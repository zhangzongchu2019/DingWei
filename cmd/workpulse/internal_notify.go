package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// makeInternalNotifyHandler 提供 D(`0.2.0.5`) 缺的那一半:**告警出口**。
//
// 为什么是这个形状(2026-09-16,业主确认 D 指标后补完):
//   - 巡检器已在容器内跑起来并实测「它自己死了能被发现」,但**容器内没有任何发消息的出口**
//     ⇒ 业主确认的指标「故意制造一次失败 ⇒ 当天有人收到告警」**无法成立**。
//   - 备选是把客户端 send.py 放进容器 —— 那会**新增一个凭据面**。
//     本实现复用 hub 既有出站队列,**不新增凭据面**
//     (与 T-113 登录设计同一条原则:**能不新增就不新增**)。
//
// 🔴 三条硬线:
//  1. **只挂 Unix domain socket**,不挂任何 TCP 端口。
//     ⚠️ **首版挂在内网 TCP mux(127.0.0.1:8792)上,理由是「内网口不可达」—— 那是错的。**
//     prodops 实测:**从宿主 POST 该端口得到 HTTP 400,即端口宿主可达**(容器共享网络栈)。
//     🔴 更值得记的是:**本仓所有既有内网端点都不依赖「端口不可达」,它们全都验 HMAC 签名**
//     (见 view_input_v2.go 的 X-DW-Signature)⇒ **代码库早已假定内网口可能可达,
//     而只有我那个端点靠网络拓扑当安全边界。** 我没看既有做法就自己发明了一个假设。
//     改用 Unix socket:**文件权限兜底,不依赖任何网络拓扑假设,也不新增凭据。**
//  2. **未配置目标 ⇒ 404「notify disabled」,绝不静默无操作** ——
//     静默成功是本项目当日最贵的那一族错误(失败没有声音)。
//  3. **正文长度设上限**,且用 DisallowUnknownFields —— 不接受未知字段。
func makeInternalNotifyHandler(outbound bus.Queue, logger *slog.Logger) http.HandlerFunc {
	target := strings.TrimSpace(os.Getenv("WP_ALERT_CHAT_ENTITY"))
	channel := strings.TrimSpace(os.Getenv("WP_ALERT_BOT_CHANNEL"))
	chatType := model.ChatPersonal
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WP_ALERT_CHAT_TYPE")), "group") {
		chatType = model.ChatGroup
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// 🔴 fail-closed:缺任何一项即明确拒绝,并说明缺什么 —— 不留"看起来成功了"的路径。
		if outbound == nil || target == "" || channel == "" {
			http.Error(w, "notify disabled: WP_ALERT_CHAT_ENTITY / WP_ALERT_BOT_CHANNEL required",
				http.StatusNotFound)
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
			http.Error(w, "text required", http.StatusBadRequest)
			return
		}
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "notify unavailable", http.StatusServiceUnavailable)
			return
		}
		content, err := json.Marshal(map[string]any{"text": formatInternalAlertText(req.Text)})
		if err != nil {
			http.Error(w, "notify unavailable", http.StatusServiceUnavailable)
			return
		}
		msg := model.Message{
			ID:           "alert-" + hex.EncodeToString(b),
			ChatEntityID: target,
			BotChannelID: channel,
			ChatType:     chatType,
			Content:      string(content),
			CreatedAt:    time.Now().UTC(),
		}
		// Direction 与 Status 由队列自己填(DBQueue.Enqueue:Direction=out, Status=queued)。
		if err := outbound.Enqueue(r.Context(), msg); err != nil {
			// 🔴 入队失败必须让调用方知道 —— 调用方(巡检 runner)据此在日志里留痕。
			logger.Warn("internal notify enqueue failed", "error", err)
			http.Error(w, "enqueue failed", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"queued":true}`))
	}
}

func formatInternalAlertText(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			lines[i] = " " + line
		}
	}
	return "【DingWei 巡检告警】\n" + strings.Join(lines, "\n") +
		"\n\n回复说明：此消息为系统单向告警，不可直接回复。"
}

// serveNotifySocket 在 Unix domain socket 上提供告警出口。
//
// 🔴 为什么是 socket 而不是 TCP:见 makeInternalNotifyHandler 顶部注释 ——
// 「内网 TCP 端口不可达」在本部署形态下**被实测证伪**,
// 而**文件权限是不依赖网络拓扑的边界**。
func serveNotifySocket(path string, handler http.Handler, logger *slog.Logger) (func(), error) {
	return serveNotifySocketWith(path, handler, logger, net.Listen, os.Chmod)
}

func serveNotifySocketWith(path string, handler http.Handler, logger *slog.Logger, listen func(string, string) (net.Listener, error), chmod func(string, os.FileMode) error) (func(), error) {
	// 陈旧 socket 文件会让 Listen 失败;先清掉(它不含任何状态)。
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Own unlinking explicitly. net.UnixListener's default unlink-on-close can
	// otherwise delete a newer listener that has already rebound this path.
	if unix, ok := ln.(*net.UnixListener); ok {
		unix.SetUnlinkOnClose(false)
	}
	// 🔴 0600:只有服务运行身份可用。**这是本出口唯一的边界,不可放宽。**
	if err := chmod(path, 0o600); err != nil {
		_ = ln.Close()
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Listen 之后 socket 文件被删除（疑为上一个 listener 的 unlink 迟到）: %w", err)
		}
		return nil, err
	}
	owned, err := os.Stat(path)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Warn("notify socket serve stopped", "error", err)
		}
	}()
	logger.Info("notify socket listening", "path", path)
	var once sync.Once
	return func() {
			once.Do(func() {
				_ = srv.Close()
				current, err := os.Stat(path)
				// SameFile can misidentify an unlinked-and-recreated socket after inode reuse;
				// this guard reduces, but does not eliminate, the risk of deleting a successor.
				if err == nil && os.SameFile(owned, current) {
					_ = os.Remove(path)
				}
		})
	}, nil
}
