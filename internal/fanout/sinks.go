package fanout

// N 路 sink 投递+ws 接入装配(设计 v2.3 §A/§B/§K1)。
// ws 接入=复用 internal/feishu LarkGateway(生产在跑组件),onInbound 契约:
// AcceptInbound WAL 提交后才返回 nil(§A WAL-先于-SDK-ack);spool 失败=返回 err,不假装接管。
// 投递=K1 明文归一 payload(v:1+顶层 token,Hub fake 分支+verifyWebhookToken)。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// SinkConfig 投递目标(§B)。Suffix 字段保留于配置模型(冻结设计字段);
// 入站投递不改字节,后缀语义由出站 caller 策略承担(§C)。
type SinkConfig struct {
	Name    string `json:"name"`
	HubURL  string `json:"hub_url"`
	Channel string `json:"channel"` // POST {hub_url}/webhook/{channel}
	Role    string `json:"role"`    // authoritative|besteffort
	Filter  string `json:"filter"`  // all|whitelist
	Suffix  string `json:"suffix"`
	Token   string `json:"token"` // 内部共享秘密→payload 顶层 token(§K1)
}

// SinkTarget 落账用轻量对(名字+角色)。
type SinkTarget struct{ Name, Role string }

// AccessConfig 接入实例(§A)。段2 实现 ws;https=v1 回调路保持原样。
type AccessConfig struct {
	BotChannel string `json:"bot_channel"`
	Mode       string `json:"mode"` // ws|https
	AppID      string `json:"app_id"`
	AppSecret  string `json:"app_secret"`
}

// ReceiverFactory 可注入(测试用 fake;默认=真 LarkGateway)。
type ReceiverFactory func(botChannel, appID, appSecret string) (feishu.Receiver, error)

func defaultReceiverFactory(botChannel, appID, appSecret string) (feishu.Receiver, error) {
	return feishu.NewLarkGateway(botChannel, appID, appSecret, nil)
}

// inboundPayloadV1 §K1:fake 归一形+v/ts;token 发送时按 sink 注入(秘密不落 WAL)。
func inboundPayloadV1(m model.Message) map[string]any {
	chatID := ""
	openID := ""
	// ChatEntityID=bot:chatType:feishuID(feishu.go:543)
	if parts := strings.SplitN(m.ChatEntityID, ":", 3); len(parts) == 3 {
		if m.ChatType == model.ChatPersonal {
			openID = parts[2]
		} else {
			chatID = parts[2]
		}
	}
	return map[string]any{
		"v": 1, "msg_id": m.FeishuMsgID,
		"chat_type": string(m.ChatType), "chat_id": chatID, "open_id": openID,
		"sender_open_id": m.SenderOpenID,
		"raw":            json.RawMessage(m.Content),
		"ts":             m.CreatedAt.UTC().Unix(),
	}
}

// matchWhitelist ws 路白名单(§A:已归一,直读 sender/chat,无需解密)。
func (s *Server) matchWhitelist(m model.Message) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.wlOK { // fail-closed:白名单不可用=不进 whitelist 过滤的 sink
		return false
	}
	if contains(s.whitelist.OpenIDs, m.SenderOpenID) {
		return true
	}
	if parts := strings.SplitN(m.ChatEntityID, ":", 3); len(parts) == 3 &&
		contains(s.whitelist.ChatIDs, parts[2]) {
		return true
	}
	return false
}

// onWSInbound ws 接入 handler:构 payload→选 sink(filter)→WAL 落盘→才返回 nil。
func (s *Server) onWSInbound(ctx context.Context, m model.Message) error {
	if s.spool == nil {
		return fmt.Errorf("spool unavailable (fail-closed)") // §B:不假装接管
	}
	payload, err := json.Marshal(inboundPayloadV1(m))
	if err != nil {
		return err
	}
	s.mu.RLock()
	sinks := s.dynSinks
	s.mu.RUnlock()
	var targets []SinkTarget
	for _, sk := range sinks {
		if sk.Filter == "whitelist" && !s.matchWhitelist(m) {
			s.metrics.WhitelistMiss.Add(1)
			continue
		}
		targets = append(targets, SinkTarget{Name: sk.Name, Role: sk.Role})
	}
	msgKey := m.BotChannelID + "\x1f" + m.FeishuMsgID
	fresh, err := s.spool.AcceptInbound(ctx, msgKey, payload, targets)
	if err != nil {
		s.log.Error("入站落盘失败(不ack)", "msg_key", msgKey, "error", err)
		return err
	}
	if fresh {
		s.metrics.InboundAccepted.Add(1)
	}
	return nil
}

// StartAccesses 起 ws 接入实例:ConfigPath 有=文件真源(load+validate+apply);
// 无=注入 Config(测试/最小部署),经同一 reconcile 生命周期(§E)。
func (s *Server) StartAccesses(ctx context.Context) error {
	if s.cfg.ConfigPath != "" {
		return s.ReloadFromFile(ctx)
	}
	return s.reconcileAccesses(ctx, s.cfg.Accesses)
}

func (s *Server) sinkByName(name string) (SinkConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sk := range s.dynSinks {
		if sk.Name == name {
			return sk, true
		}
	}
	return SinkConfig{}, false
}

// sinkWorker 投递循环:独立游标取 (消息,sink) 对→POST K1→delivered|退避|终态。
func (s *Server) sinkWorker(ctx context.Context) {
	if s.spool == nil {
		return
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		for {
			job, err := s.spool.NextDueInbound(ctx, time.Now())
			if err != nil || job == nil {
				break
			}
			s.deliverSink(ctx, job)
		}
	}
}

func (s *Server) deliverSink(ctx context.Context, job *InboundSinkJob) {
	sk, ok := s.sinkByName(job.SinkName)
	if !ok {
		_, _ = s.spool.MarkSinkFailed(ctx, job, "sink gone from config",
			s.maxAttempts(), custodyBackoff(job.Attempts), false)
		return
	}
	// token 发送时注入(§K1;秘密不落 WAL)
	var payload map[string]any
	if err := json.Unmarshal(job.Body, &payload); err != nil {
		_, _ = s.spool.MarkSinkFailed(ctx, job, "corrupt body", 1, 1, true)
		return
	}
	payload["token"] = sk.Token
	body, _ := json.Marshal(payload)
	url := strings.TrimRight(sk.HubURL, "/") + "/webhook/" + sk.Channel
	sctx, cancel := context.WithTimeout(ctx, s.cfg.ForwardTO)
	req, _ := http.NewRequestWithContext(sctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	cancel()
	status := 0
	if resp != nil {
		status = resp.StatusCode
		resp.Body.Close()
	}
	switch {
	case err == nil && status >= 200 && status < 300:
		s.metrics.SinkDelivered.Add(1)
		if e := s.spool.MarkSinkDelivered(ctx, job.MsgKey, job.SinkName); e != nil {
			s.log.Error("MarkSinkDelivered失败", "error", e)
		}
		return
	case err == nil && status == http.StatusUnauthorized:
		// K1:401=token 错=配置事故;authoritative 恒重试+告警,绝不丢
		s.log.Error("sink投递401(token配置事故,告警)", "sink", job.SinkName, "msg_key", job.MsgKey)
		_, _ = s.spool.MarkSinkFailed(ctx, job, "401 unauthorized",
			1<<30, s.cfg.ForwardTO, false) // 实际上限极大=永不自动 dlq
		return
	case err == nil && status >= 400 && status < 500:
		// K1:其余 4xx=毒件,立即终态(auth→dlq[blocked],besteffort→dropped),不 HOL
		term, _ := s.spool.MarkSinkFailed(ctx, job,
			fmt.Sprintf("poison %d", status), 1, 0, true)
		if term == "dlq" {
			s.metrics.SinkDLQ.Add(1)
			s.log.Error("sink毒件入DLQ(告警)", "sink", job.SinkName, "msg_key", job.MsgKey, "status", status)
		} else {
			s.metrics.BesteffortDropped.Add(1)
		}
		return
	default: // 网络/5xx=瞬态,退避重试;besteffort 有界
		max := s.maxAttempts()
		if sk.Role != "authoritative" {
			max = s.besteffortMax()
		}
		errStr := fmt.Sprintf("status=%d err=%v", status, err)
		term, _ := s.spool.MarkSinkFailed(ctx, job, errStr, max, custodyBackoff(job.Attempts), false)
		switch term {
		case "dlq":
			s.metrics.SinkDLQ.Add(1)
			s.log.Error("sink终败入DLQ(告警,global=blocked)", "sink", job.SinkName,
				"msg_key", job.MsgKey, "error", errStr)
		case "dropped":
			s.metrics.BesteffortDropped.Add(1)
		}
	}
}

func (s *Server) besteffortMax() int {
	if s.cfg.BesteffortMaxAttempts > 0 {
		return s.cfg.BesteffortMaxAttempts
	}
	return 5
}
