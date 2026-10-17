package nodeagent

// T-G4-53 · export_seat 指令：席位卷备份回传（销毁先备份——业主 8971bddf）。
// 席位目录打成 tar.gz 流式 POST 到 Hub 给的 upload_url（不落节点盘——组长 eccc54bc：
// 备份统一送 dw 的 /data）。🔴 防外泄闸：upload_url 必须以本代理配置的 Hub 地址为源
// （wss→https 换算），指向别处的指令一律拒——否则这条指令就是把席位数据送到任意地址。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// BackupSHA256Trailer 回传流的 sha256（hex）随 HTTP 请求 trailer 送出：边打边算，流结束才知道，
// 故不能是请求头（放头里就得先把整卷打包落在节点盘上再算，违背「不落节点盘」）。
// 🔴 R-G4-BACKUP-CHECKSUM-CTRL（106 实测 nginx 1.24.0 丢弃请求 trailer ⇒ 丙案）：
// trailer 只是第三方一致性信号；权威通道是 export_seat 结果载荷里的 sha256（控制面补报），
// Hub 落盘自算、两值相等才认。trailer 该发还发（到达即多一道旁证），但不再是唯一依据。
const BackupSHA256Trailer = "X-Backup-Sha256"

// backupExcluded is rooted at the hosted /work bind, never a recursive basename filter.
// CLI private trees are omitted as a whole: auth copies, refresh caches, config secrets,
// and helper journals/credentials must not be restored as a new live identity.
// Work files remain; restoring a backup requires fresh enrollment and LLM login.
var backupExcluded = []string{
	"account-credential", ".account-credential.*", "enroll-token.json",
	".dingwei", "inbox.sqlite3", "inbox.sqlite3-wal", "inbox.sqlite3-shm",
	".codex", ".claude", ".claude.json", ".claude.json.*",
	".gemini", ".cline", ".opencode", ".config",
	".local/share/opencode", ".local/share/cline",
	".aider.conf.yml", ".aider.model.settings.yml", ".env", ".env.*",
}

// tarArgs 导出命令参数：--anchored 让排除只认「席位根/文件名」这一处，数据目录里同名的用户文件不受影响。
func tarArgs(seatRoot, seatID string) []string {
	args := []string{"-C", seatRoot, "--anchored"}
	for _, f := range backupExcluded {
		args = append(args, "--exclude="+seatID+"/"+f)
	}
	return append(args, "-czf", "-", seatID)
}

// A second hard-link name outside an excluded tree could otherwise export the same
// credential inode. Refuse before opening the HTTP body; never read credential contents.
// Lifecycle destruction stops writers first; this is not a sandbox against a hostile host.
func rejectBackupCredentialLinks(dir string) error {
	for _, pattern := range backupExcluded {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return fmt.Errorf("backup exclusion validation failed")
		}
		for _, root := range matches {
			err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return fmt.Errorf("backup private metadata unavailable")
				}
				info, err := entry.Info()
				if err != nil {
					return fmt.Errorf("backup private metadata unavailable")
				}
				if info.Mode().IsRegular() {
					st, ok := info.Sys().(*syscall.Stat_t)
					if !ok || st.Nlink > 1 {
						return fmt.Errorf("backup private hardlink refused")
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type exportSeatDeps struct {
	seatRoot  string
	hubOrigin string // https 源（由 wss 配置换算）
	secretFn  func() (string, error)
	hc        *http.Client
	tarCmd    func(ctx context.Context, args ...string) *exec.Cmd // 默认 exec.CommandContext(ctx, "tar", ...)；用例注入失败的 tar
}

// NewExportSeatHandler 构造 export_seat 处理器。hubWSS＝配置里的 HubURL（wss://…）；
// certPin 非空则钉证（与主连接同一层），空则系统 CA。
func NewExportSeatHandler(seatRoot, hubWSS string, secretFn func() (string, error), certPin string) (Handler, error) {
	if !strings.HasPrefix(hubWSS, "wss://") {
		return nil, fmt.Errorf("nodeagent: HubURL 必须 wss://")
	}
	origin := "https://" + strings.TrimPrefix(hubWSS, "wss://")
	// 剥掉路径（配置形如 wss://host/api/node/connect）
	if i := strings.Index(origin[len("https://"):], "/"); i >= 0 {
		origin = origin[:len("https://")+i]
	}
	hc := &http.Client{Timeout: 15 * time.Minute}
	if certPin != "" {
		tr, err := TLSPinnedTransport(certPin)
		if err != nil {
			return nil, err
		}
		hc.Transport = tr
	}
	d := &exportSeatDeps{seatRoot: seatRoot, hubOrigin: origin, secretFn: secretFn, hc: hc, tarCmd: defaultTarCmd}
	return d.handle, nil
}

func (d *exportSeatDeps) handle(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SeatID    string `json:"seat_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	// 🔴 防外泄：回传地址必须是以配置 Hub 为源的 /api/node/backup/
	if !strings.HasPrefix(p.UploadURL, d.hubOrigin+"/api/node/backup/") {
		return nil, fmt.Errorf("%w: upload_url 非本 Hub 备份端点", ErrParams)
	}
	dir := seatDir(d.seatRoot, p.SeatID)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("席位目录不在: %v", err)
	}
	if err := rejectBackupCredentialLinks(dir); err != nil {
		return nil, err
	}
	secret, err := d.secretFn()
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	tarCmd := d.tarCmd
	if tarCmd == nil {
		tarCmd = defaultTarCmd
	}
	cmd := tarCmd(ctx, tarArgs(d.seatRoot, p.SeatID)...)
	cmd.Stdout = pw
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tar 起不来: %v", err)
	}
	tarDone := make(chan error, 1)
	go func() { // tar 失败经 pipe 错误传给 Upload 读侧，退出码另交给本函数核
		werr := cmd.Wait()
		tarDone <- werr
		pw.CloseWithError(werr)
	}()

	// 边打边算 sha256：读侧每读一段就喂哈希；读到 EOF（流结束）时把结果写进请求 trailer。
	h := sha256.New()
	sumCh := make(chan string, 1) // 补报值：trailer（可丢）与结果载荷（控制面）同出一路
	req, err := http.NewRequestWithContext(ctx, "POST", p.UploadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Trailer = http.Header{BackupSHA256Trailer: nil}
	req.Body = io.NopCloser(&trailerHashReader{r: io.TeeReader(pr, h), onEOF: func() {
		sum := hex.EncodeToString(h.Sum(nil))
		req.Trailer.Set(BackupSHA256Trailer, sum)
		sumCh <- sum
	}})
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("回传失败: %v", err)
	}
	defer resp.Body.Close()
	// 🔴 两道都得过（复验存活刀 B11／B12）：Hub 拒收 ⇒ 失败；tar 非零退出 ⇒ 失败（哪怕 HTTP 那边看着正常结束）。
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("回传被拒: HTTP %d", resp.StatusCode)
	}
	if werr := <-tarDone; werr != nil {
		return nil, fmt.Errorf("tar 失败: %v", werr)
	}
	// 🔴 控制面补报（丙案）：sha256 随结果载荷走 WSS 回 Hub——trailer 会被反代丢弃，这条才是权威通道。
	sum := <-sumCh
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "state": "exported", "sha256": sum})
}

func defaultTarCmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "tar", args...)
}

// trailerHashReader 读到 EOF 时回调一次（此刻 sha256 已覆盖全部字节），供写请求 trailer。
type trailerHashReader struct {
	r     io.Reader
	onEOF func()
	done  bool
}

func (t *trailerHashReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err == io.EOF && !t.done {
		t.done = true
		t.onEOF()
	}
	return n, err
}
