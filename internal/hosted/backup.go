package hosted

// T-G4-53 · 销毁备份管线（Hub 侧）：节点代理出卷内容（tar.gz 流式回传，不落节点盘——
// 组长 eccc54bc：备份统一送 dw 的 /data）→ Hub 落盘登记 → 留 14 天到期删（业主 8971bddf）。
//
// 链路：Lifecycle.Destroy → HubBackuper.BackupSeatVolume（建 pending 记录→经网关发
// export_seat（带 upload_url）→节点 tar 流式 POST /api/node/backup/{id}（Bearer 节点凭据）
// →BackupHandler 落盘＋自算 sha256）→ 节点在 export_seat 结果载荷里经控制面补报 sha256
// →BackupSeatVolume 比对相等才转 complete → Destroy 拿到 backupRef。任一环失败 ⇒ backup_failed
// （销毁中止——不跳过备份）。
//
// 🔴 R-G4-BACKUP-CHECKSUM-CTRL（丙案；106 实测 nginx 1.24.0 丢弃请求 trailer）：
// 校验的权威通道是控制面补报（WSS 结果载荷），trailer 到达只作第三方一致性旁证，
// 不得作为唯一依据；未补报或不等 ⇒ 不销毁。不得降级为只靠 verifyTarGz。

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
	"sync"
	"time"
)

// BackupTTL 备份保留期（业主 8971bddf：14 天到期删除）。
const BackupTTL = 14 * 24 * time.Hour

// PendingBackupTTL is deliberately longer than the 15 minute export command
// timeout.  A pending row older than this cannot belong to a healthy in-flight
// export and is safe for the periodic sweeper to reclaim.
const PendingBackupTTL = time.Hour

// BackupSHA256Trailer 节点回传流的 sha256（hex），随 HTTP 请求 trailer 送来（与 nodeagent 同名）。
// 🔴 丙案后它只是旁证：到了就与落盘自算对一遍（不符即拒），不到不挡路（反代会丢）。
const BackupSHA256Trailer = "X-Backup-Sha256"

// HubBackuper 实现 VolumeBackuper：指挥节点回传席位卷并落 Hub 本地（dw 的 /data 下）。
type HubBackuper struct {
	st     *Store
	gw     NodeGateway
	origin string // Hub 公网地址（节点回传的 upload_url 基底；DW_PUBLIC_ORIGIN）
	dir    string // 备份落盘目录（/data/dingwei-newplat/backups/hosted）
	now    func() time.Time
	// landed 已落盘备份的自算 sha256 与大小（id→…）：BackupHandler 写入，
	// BackupSeatVolume 拿它与控制面补报比对，相等才转 complete。
	// 进程内存即可——比对在同一次同步流里完成；Hub 中途重启则 pending 记录留待重试（与旧口径同）。
	mu     sync.Mutex
	landed map[string]struct {
		sha  string
		size int64
	}
}

func NewHubBackuper(st *Store, gw NodeGateway, publicOrigin, dir string) *HubBackuper {
	return &HubBackuper{st: st, gw: gw, origin: strings.TrimRight(publicOrigin, "/"), dir: dir, now: time.Now,
		landed: map[string]struct {
			sha  string
			size int64
		}{}}
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
		// 结果没回来（掉线／超时／ctx 取消）：落盘登记也要摘——否则 landed[id] 永驻内存
		//（复验二轮 qwen38maxkhovvob4：形状① 落地前大席位必走这条）。
		_ = b.st.FailBackup(ctx, id)
		b.forget(id)
		return "", err
	}
	if !res.OK {
		_ = b.st.FailBackup(ctx, id)
		b.forget(id)
		return "", fmt.Errorf("export_seat: %s", res.Error)
	}
	// 🔴 丙案校验闸（权威通道）：节点在结果载荷里补报的 sha256 必须等于落盘自算值——
	// 未补报、未落盘或两值不等 ⇒ 不销毁（销毁侧拿到 error 即中止，席位 backup_failed）。
	var rep struct {
		SHA256 string `json:"sha256"`
	}
	_ = json.Unmarshal(res.Payload, &rep)
	b.mu.Lock()
	land, landed := b.landed[id]
	b.mu.Unlock()
	if rep.SHA256 == "" {
		b.rejectLanded(ctx, id)
		return "", fmt.Errorf("节点未补报 sha256（控制面）")
	}
	if !landed {
		b.rejectLanded(ctx, id)
		return "", fmt.Errorf("备份回传未完成（未落盘）")
	}
	if !strings.EqualFold(rep.SHA256, land.sha) {
		b.rejectLanded(ctx, id)
		return "", fmt.Errorf("补报 sha256 与落盘自算不符")
	}
	if err := b.st.CompleteBackup(ctx, id, land.size); err != nil {
		b.rejectLanded(ctx, id)
		return "", err
	}
	b.forget(id)
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
	// 落盘件在、非空；完整性两道：gzip＋tar 头到尾解得开（落盘时已核），sha256 控制面比对（上面已核）。
	fi, err := os.Stat(bk.Path)
	if err != nil || fi.Size() == 0 {
		return "", fmt.Errorf("备份件缺失或为空: %v", err)
	}
	return bk.Path, nil
}

// forget 摘掉落盘登记（终态已定后）。
func (b *HubBackuper) forget(id string) {
	b.mu.Lock()
	delete(b.landed, id)
	b.mu.Unlock()
}

// rejectLanded 校验闸不过的清理：作废 pending 记录（FailBackup 连落盘件一起删）、摘登记。
func (b *HubBackuper) rejectLanded(ctx context.Context, id string) {
	_ = b.st.FailBackup(ctx, id)
	b.forget(id)
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
		b.mu.Lock()
		_, already := b.landed[id]
		b.mu.Unlock()
		if already { // 已落盘的同一 id 再传 ⇒ 不覆写（记录尚在 pending，等控制面比对）
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
		// 🔴 完整性（复验缺陷二；台账 lead-g4-t53-review 🔴2）＋ 丙案（R-G4-BACKUP-CHECKSUM-CTRL）：
		//   ① 落盘件 gzip＋tar 从头到尾解得开 —— 非 gzip、截断、tar 坏块都在这里现形；
		//   ② trailer 的 sha256 到达即与落盘自算对一遍（不符即拒）；🔴 不到不挡路——
		//      106 实测 nginx 丢 trailer，权威校验在控制面补报（BackupSeatVolume 比对后才转 complete）。
		if tr := strings.ToLower(strings.TrimSpace(r.Trailer.Get(BackupSHA256Trailer))); tr != "" &&
			!strings.EqualFold(hex.EncodeToString(h.Sum(nil)), tr) {
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
		// 落盘成功：登记自算 sha256 与大小，等控制面补报比对（BackupSeatVolume）后才转 complete。
		b.mu.Lock()
		b.landed[id] = struct {
			sha  string
			size int64
		}{sha: hex.EncodeToString(h.Sum(nil)), size: n}
		b.mu.Unlock()
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
	stale, err := b.st.ListStalePendingBackups(ctx, now.Add(-PendingBackupTTL))
	if err != nil {
		return err
	}
	for _, bk := range stale {
		if err := b.st.FailBackup(ctx, bk.ID); err != nil {
			return err
		}
		b.forget(bk.ID)
	}
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
	_, err = s.exec(ctx, `DELETE FROM g4_backup WHERE id=$1 AND state='pending'`, id)
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

func (s *Store) ListStalePendingBackups(ctx context.Context, before time.Time) ([]Backup, error) {
	rows, err := s.query(ctx, `SELECT id, path FROM g4_backup WHERE state='pending' AND created_at<$1`, before)
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
