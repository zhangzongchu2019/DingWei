package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
//  1. **只挂内网 mux**(默认 127.0.0.1:8792)⇒ **不新增对外暴露面**。
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
		content, err := json.Marshal(map[string]any{"text": req.Text})
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
