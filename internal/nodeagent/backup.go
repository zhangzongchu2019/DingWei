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
	"strings"
	"time"
)

// BackupSHA256Trailer 回传流的 sha256（hex）随 HTTP 请求 trailer 送出：边打边算，流结束才知道，
// 故不能是请求头（放头里就得先把整卷打包落在节点盘上再算，违背「不落节点盘」）。Hub 落盘时同步算、不符即拒。
const BackupSHA256Trailer = "X-Backup-Sha256"

// backupExcluded 席位根目录下不得进备份的凭据文件（复验 REVIEW-G4-T53 缺陷一；台账 lead-g4-t53-review 🔴1）：
// account-credential 是账号凭据、enroll-token.json 含一次性 poll_token（T-G4-32 写在同一目录）。
// 备份在 Hub /data 留 14 天，带着它们等于把凭据明文存 14 天（违背 B7）。
var backupExcluded = []string{"account-credential", "enroll-token.json"}

// tarArgs 导出命令参数：--anchored 让排除只认「席位根/文件名」这一处，数据目录里同名的用户文件不受影响。
func tarArgs(seatRoot, seatID string) []string {
	args := []string{"-C", seatRoot, "--anchored"}
	for _, f := range backupExcluded {
		args = append(args, "--exclude="+seatID+"/"+f)
	}
	return append(args, "-czf", "-", seatID)
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
	req, err := http.NewRequestWithContext(ctx, "POST", p.UploadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Trailer = http.Header{BackupSHA256Trailer: nil}
	req.Body = io.NopCloser(&trailerHashReader{r: io.TeeReader(pr, h), onEOF: func() {
		req.Trailer.Set(BackupSHA256Trailer, hex.EncodeToString(h.Sum(nil)))
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
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "state": "exported"})
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
