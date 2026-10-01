package nodeagent

// T-G4-53 · export_seat 指令：席位卷备份回传（销毁先备份——业主 8971bddf）。
// 席位目录打成 tar.gz 流式 POST 到 Hub 给的 upload_url（不落节点盘——组长 eccc54bc：
// 备份统一送 dw 的 /data）。🔴 防外泄闸：upload_url 必须以本代理配置的 Hub 地址为源
// （wss→https 换算），指向别处的指令一律拒——否则这条指令就是把席位数据送到任意地址。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type exportSeatDeps struct {
	seatRoot  string
	hubOrigin string // https 源（由 wss 配置换算）
	secretFn  func() (string, error)
	hc        *http.Client
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
	d := &exportSeatDeps{seatRoot: seatRoot, hubOrigin: origin, secretFn: secretFn, hc: hc}
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
	cmd := exec.CommandContext(ctx, "tar", "-C", d.seatRoot, "-czf", "-", p.SeatID)
	cmd.Stdout = pw
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tar 起不来: %v", err)
	}
	go func() { pw.CloseWithError(cmd.Wait()) }() // tar 失败经 pipe 错误传给 Upload 读侧

	req, err := http.NewRequestWithContext(ctx, "POST", p.UploadURL, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("回传失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("回传被拒: HTTP %d", resp.StatusCode)
	}
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "state": "exported"})
}
