package hosted

// T-G4-53 · 销毁备份管线（Hub 侧）：节点代理出卷内容（tar.gz 流式回传，不落节点盘——
// 组长 eccc54bc：备份统一送 dw 的 /data）→ Hub 落盘登记 → 留 14 天到期删（业主 8971bddf）。
//
// 链路：Lifecycle.Destroy → HubBackuper.BackupSeatVolume（建 pending 记录→经网关发
// export_seat（带 upload_url）→节点 tar 流式 POST /api/node/backup/{id}（Bearer 节点凭据）
// →BackupHandler 落盘＋complete）→ Destroy 拿到 backupRef。任一环失败 ⇒ backup_failed
// （销毁中止——不跳过备份）。

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupTTL 备份保留期（业主 8971bddf：14 天到期删除）。
const BackupTTL = 14 * 24 * time.Hour

// BackupSHA256Trailer 节点回传流的 sha256（hex），随 HTTP 请求 trailer 送来（与 nodeagent 同名）。
const BackupSHA256Trailer = "X-Backup-Sha256"

// HubBackuper 实现 VolumeBackuper：指挥节点回传席位卷并落 Hub 本地（dw 的 /data 下）。
type HubBackuper struct {
	st     *Store
	gw     NodeGateway
	origin string // Hub 公网地址（节点回传的 upload_url 基底；DW_PUBLIC_ORIGIN）
	dir    string // 备份落盘目录（/data/dingwei-newplat/backups/hosted）
	now    func() time.Time
}

func NewHubBackuper(st *Store, gw NodeGateway, publicOrigin, dir string) *HubBackuper {
	return &HubBackuper{st: st, gw: gw, origin: strings.TrimRight(publicOrigin, "/"), dir: dir, now: time.Now}
}

// BackupSeatVolume 备份席位卷：pending 记录 → export_seat → 等回传完成 → 落盘路径即 backupRef。
func (b *HubBackuper) BackupSeatVolume(ctx context.Context, nodeID, seatID string) (string, error) {
	id, err := newBackupID()
	if err != nil {
		return "", err
	}
	if err := b.st.CreateBackup(ctx, Backup{
		ID: id, NodeID: nodeID, SeatID: seatID, State: "pending",
		Path: filepath.Join(b.dir, id+".tar.gz"), ExpiresAt: b.now().Add(BackupTTL),
	}); err != nil {
		return "", err
	}
	cmd := NodeCommand{Type: "export_seat", Params: json.RawMessage(
		`{"seat_id":` + jsonStr(seatID) + `,"upload_url":` + jsonStr(b.origin+"/api/node/backup/"+id) + `}`)}
	var res NodeResult
	if gw, ok := b.gw.(interface {
		SendWithTimeout(context.Context, string, NodeCommand, time.Duration) (NodeResult, error)
	}); ok {
		res, err = gw.SendWithTimeout(ctx, nodeID, cmd, 15*time.Minute) // 大卷回传
	} else {
		res, err = b.gw.Send(ctx, nodeID, cmd)
	}
	if err != nil {
		_ = b.st.FailBackup(ctx, id)
		return "", err
	}
	if !res.OK {
		_ = b.st.FailBackup(ctx, id)
		return "", fmt.Errorf("export_seat: %s", res.Error)
	}
	bk, err := b.st.GetBackup(ctx, id)
	if err != nil {
		_ = b.st.FailBackup(ctx, id)
		return "", err
	}
	if bk.State != "complete" || bk.SizeBytes <= 0 {
		// 异常分支也清 pending（复验第四节）：不清的话这条记录之后仍会被 BackupHandler 当可写目标收一次迟到的上传。
		_ = b.st.FailBackup(ctx, id)
		return "", fmt.Errorf("备份回传未完成（state=%s size=%d）", bk.State, bk.SizeBytes)
	}
	// 落盘件在、非空；内容的完整性已在 BackupHandler 落盘时核过（sha256 对上、gzip＋tar 头到尾解得开）才会 complete。
	fi, err := os.Stat(bk.Path)
	if err != nil || fi.Size() == 0 {
		return "", fmt.Errorf("备份件缺失或为空: %v", err)
	}
	return bk.Path, nil
}

// BackupHandler 回传接收端点（POST /api/node/backup/{id}；Bearer 节点凭据，且必须是该备份
// 记录的属主节点）。流式落盘：先写 .tmp 再原子改名（半截文件不得标记 complete）。
func (b *HubBackuper) BackupHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		bk, err := b.st.GetBackup(r.Context(), id)
		if err != nil || bk.State != "pending" {
			http.Error(w, `{"error":"backup_not_pending"}`, http.StatusNotFound)
			return
		}
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if secret == "" || b.st.AuthenticateNode(r.Context(), bk.NodeID, secret) != nil {
			http.Error(w, `{"error":"node_auth_failed"}`, http.StatusUnauthorized)
			return
		}
		if err := os.MkdirAll(b.dir, 0o700); err != nil {
			http.Error(w, `{"error":"backup_store_unavailable"}`, http.StatusInternalServerError)
			return
		}
		tmp := bk.Path + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			http.Error(w, `{"error":"backup_store_unavailable"}`, http.StatusInternalServerError)
			return
		}
		h := sha256.New()
		n, werr := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, 32<<30)) // 上限 32GiB 防爆；落盘同步算 sha256
		cerr := f.Close()
		if werr != nil || cerr != nil {
			os.Remove(tmp)
			http.Error(w, `{"error":"backup_write_failed"}`, http.StatusInternalServerError)
			return
		}
		// 🔴 完整性两道（复验缺陷二；台账 lead-g4-t53-review 🔴2），任一不过 ⇒ 删临时件、记录作废、422 拒收：
		//   ① 节点边打边算的 sha256（请求 trailer，读完 body 才有）必须与 Hub 落盘同步算的一致 —— 缺了也拒；
		//   ② 落盘件 gzip＋tar 从头到尾解得开 —— 非 gzip、截断、tar 坏块都在这里现形。
		// 原先只 os.Stat 看非空：非 gzip、截断 gzip 都被当成功，随后销毁容器与卷，数据就丢了。
		if got, want := hex.EncodeToString(h.Sum(nil)), strings.ToLower(strings.TrimSpace(r.Trailer.Get(BackupSHA256Trailer))); want == "" || got != want {
			b.rejectBackup(w, r.Context(), id, tmp, "backup_checksum_mismatch")
			return
		}
		if err := verifyTarGz(tmp); err != nil {
			b.rejectBackup(w, r.Context(), id, tmp, "backup_corrupt")
			return
		}
		if err := os.Rename(tmp, bk.Path); err != nil {
			os.Remove(tmp)
			http.Error(w, `{"error":"backup_write_failed"}`, http.StatusInternalServerError)
			return
		}
		if err := b.st.CompleteBackup(r.Context(), id, n); err != nil {
			http.Error(w, `{"error":"backup_write_failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

// rejectBackup 完整性不过：删临时件、作废记录（迟到的重传也进不来），回 422。
func (b *HubBackuper) rejectBackup(w http.ResponseWriter, ctx context.Context, id, tmp, code string) {
	_ = os.Remove(tmp)
	_ = b.st.FailBackup(ctx, id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

// verifyTarGz 落盘件头到尾解一遍：gzip 流完整（含尾部校验）、tar 每个条目读得完、正常结束。
func verifyTarGz(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := 0
	for {
		_, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return err
		}
		entries++
	}
	if entries == 0 {
		return errors.New("备份件里一个条目都没有")
	}
	// tar 结束之后 gzip 也必须读到干净的结尾（含 CRC 与长度校验）
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return err
	}
	return nil
}

// SweepExpiredBackups 到期删除（14 天）：删文件＋置 expired。由 Hub 侧周期任务驱动。
func (b *HubBackuper) SweepExpiredBackups(ctx context.Context, now time.Time) error {
	expired, err := b.st.ListExpiredBackups(ctx, now)
	if err != nil {
		return err
	}
	for _, bk := range expired {
		if bk.Path != "" {
			if err := os.Remove(bk.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("删备份件 %s: %v", bk.ID, err)
			}
		}
		if err := b.st.MarkBackupExpired(ctx, bk.ID); err != nil {
			return err
		}
	}
	return nil
}

func newBackupID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "bk_" + hex.EncodeToString(b[:]), nil
}

// ---------- 存储 ----------

type Backup struct {
	ID, NodeID, SeatID, State, Path string
	SizeBytes                       int64
	ExpiresAt                       time.Time
}

func (s *Store) CreateBackup(ctx context.Context, bk Backup) error {
	_, err := s.pool.Exec(ctx, htx(), `
INSERT INTO g4_backup (id, node_id, seat_id, state, path, expires_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		bk.ID, bk.NodeID, bk.SeatID, bk.State, bk.Path, bk.ExpiresAt)
	return err
}

func (s *Store) GetBackup(ctx context.Context, id string) (Backup, error) {
	var bk Backup
	row, err := s.qrow(ctx, `SELECT id, node_id, seat_id, state, path, size_bytes, expires_at FROM g4_backup WHERE id=$1`, id)
	if err != nil {
		return bk, err
	}
	err = row.Scan(&bk.ID, &bk.NodeID, &bk.SeatID, &bk.State, &bk.Path, &bk.SizeBytes, &bk.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return bk, ErrNotFound
	}
	return bk, err
}

// FailBackup 回传失败：记录删除（pending 留着会被 Handler 当可写目标）。文件残留清掉。
func (s *Store) FailBackup(ctx context.Context, id string) error {
	var path string
	row, err := s.qrow(ctx, `SELECT path FROM g4_backup WHERE id=$1 AND state='pending'`, id)
	if err == nil && row.Scan(&path) == nil && path != "" {
		_ = os.Remove(path)
		_ = os.Remove(path + ".tmp")
	}
	_, err = s.pool.Exec(ctx, htx(), `DELETE FROM g4_backup WHERE id=$1 AND state='pending'`, id)
	return err
}

func (s *Store) CompleteBackup(ctx context.Context, id string, size int64) error {
	_, err := s.pool.Exec(ctx, htx(), `UPDATE g4_backup SET state='complete', size_bytes=$2 WHERE id=$1 AND state='pending'`, id, size)
	return err
}

func (s *Store) ListExpiredBackups(ctx context.Context, now time.Time) ([]Backup, error) {
	rows, err := s.pool.Query(ctx, htx(), `SELECT id, path FROM g4_backup WHERE state='complete' AND expires_at<$1`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Backup
	for rows.Next() {
		var bk Backup
		if err := rows.Scan(&bk.ID, &bk.Path); err != nil {
			return nil, err
		}
		out = append(out, bk)
	}
	return out, rows.Err()
}

func (s *Store) MarkBackupExpired(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, htx(), `UPDATE g4_backup SET state='expired' WHERE id=$1`, id)
	return err
}
