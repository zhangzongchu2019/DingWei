package fanout

// 出站托管路(设计 v2.3 §C/§K2/§K3):POST /outbound 带 v:1=严格托管契约
// (UUIDv5 request_id/幂等/202=accepted 仅在 WAL 落盘后);无 v=legacy 立发路
// (v1 兼容,Hub ProxyGateway 升级后弃用)。发送责任在 sender worker:
// 重试退避→DLQ,真 Lark msg_id 记 spool+审计,以 outbound_id 关联。

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

// CallerConfig 出站调用方(§C 指定来源第二维):后缀策略+审计标签按 caller。
type CallerConfig struct {
	Name       string `json:"name" yaml:"name"`
	TestSuffix bool   `json:"test_suffix" yaml:"test_suffix"` // true=追加[测试环境](幂等)
}

// reUUIDv5 §K2 冻结正则:request_id 必须为 UUIDv5(派生规则=Hub 侧 §C1,代理只验不代生成)。
var reUUIDv5 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// custodyRequest §K2 v:1 请求体。
type custodyRequest struct {
	V          int             `json:"v"`
	RequestID  string          `json:"request_id"`
	BotChannel string          `json:"bot_channel"`
	ToID       string          `json:"to_id"`
	ToName     string          `json:"to_name"`
	ToType     string          `json:"to_type"`
	Text       string          `json:"text"`
	Raw        json.RawMessage `json:"raw"`
}

// callerByToken 常数时间比对 caller token;"" = 未命中。(热载表 §E)
func (s *Server) callerByToken(tok string) (string, CallerConfig, bool) {
	s.mu.RLock()
	callers := s.dynCallers
	s.mu.RUnlock()
	for t, c := range callers {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(t)) == 1 {
			return c.Name, c, true
		}
	}
	return "", CallerConfig{}, false
}

// senderFor 多 bot 出站选路(§C 指定来源第一维):Bots 注册表按 bot_channel 选;
// 空表=退化单 Sender(v1 兼容)。(热载表 §E)
func (s *Server) senderFor(botChannel string) (Sender, bool) {
	s.mu.RLock()
	bots := s.dynBots
	s.mu.RUnlock()
	if len(bots) == 0 {
		return s.cfg.Sender, s.cfg.Sender != nil
	}
	sd, ok := bots[botChannel]
	return sd, ok
}

// handleOutboundCustody v:1 严格路。同步只做四件事(§C1):鉴权+校验+bot 已知+落盘。
func (s *Server) handleOutboundCustody(w http.ResponseWriter, r *http.Request, body []byte) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	callerID, callerCfg, ok := s.callerByToken(tok)
	if !ok {
		s.metrics.OutboundAuthFailed.Add(1)
		s.log.Warn("custody鉴权失败(审计)", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields() // §K 严格 schema:未知字段=400
	var req custodyRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "bad schema: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.V != 1 {
		http.Error(w, "unsupported version", http.StatusBadRequest)
		return
	}
	if !reUUIDv5.MatchString(req.RequestID) {
		// 缺/坏 request_id=400,代理禁止代生成(§C1)
		http.Error(w, "request_id must be UUIDv5 (derivation: design §C1)", http.StatusBadRequest)
		return
	}
	if req.Text == "" && len(req.Raw) == 0 {
		http.Error(w, "empty message", http.StatusUnprocessableEntity)
		return
	}
	if _, ok := s.senderFor(req.BotChannel); !ok {
		http.Error(w, "unknown bot_channel", http.StatusNotFound)
		return
	}
	if s.spool == nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable) // fail-closed(§B)
		return
	}
	// 后缀策略按 caller(§C 指定来源):落盘前定格,重放字节恒同。
	if callerCfg.TestSuffix {
		req.Text = AppendTestSuffix(req.Text)
	}
	canonical, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "marshal", http.StatusInternalServerError)
		return
	}
	id, replay, err := s.spool.Accept(r.Context(), callerID, req.RequestID, canonical)
	if errors.Is(err, ErrBodyConflict) {
		http.Error(w, "request_id conflict: different body", http.StatusConflict)
		return
	}
	if err != nil {
		s.log.Error("custody落盘失败", "error", err)
		http.Error(w, "spool failure", http.StatusServiceUnavailable)
		return
	}
	s.log.Info("custody审计", "caller", callerID, "outbound_id", id,
		"request_id", req.RequestID, "bot_channel", req.BotChannel, "replay", replay)
	w.Header().Set("Content-Type", "application/json")
	if replay {
		w.WriteHeader(http.StatusOK) // 幂等重放:同 outbound_id(§C1)
	} else {
		s.metrics.OutboundQueued.Add(1)
		w.WriteHeader(http.StatusAccepted) // 202=accepted 非 delivered(§C2),此刻已 fsync
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"outbound_id": id})
}

// handleOutboundStatus K3:GET /outbound/{id},仅本 caller 可查。
func (s *Server) handleOutboundStatus(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	callerID, _, ok := s.callerByToken(tok)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.spool == nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	j, err := s.spool.Status(r.Context(), callerID, r.PathValue("id"))
	if err != nil {
		http.Error(w, "lookup", http.StatusInternalServerError)
		return
	}
	if j == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	status := j.Status
	if status == "sending" || status == "queued" { // 对外二态化归 §C3 状态机口径
		status = map[string]string{"queued": "queued", "sending": "sending"}[status]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status, "attempts": j.Attempts,
		"lark_msg_id": nullable(j.LarkMsgID), "error": nullable(j.LastError),
	})
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// custodyBackoff 重试退避表:1s/5s/30s/2m/10m 封顶。
func custodyBackoff(attempts int) time.Duration {
	steps := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	if attempts >= len(steps) {
		return steps[len(steps)-1]
	}
	return steps[attempts]
}

// custodyWorker 托管发送循环:取到期件→按 bot 选路发 Lark→sent|退避|DLQ。
// 崩溃恢复:启动时 RecoverSending 把残留 sending 归队(at-least-once)。
func (s *Server) custodyWorker(ctx context.Context) {
	if s.spool == nil {
		return
	}
	if n, err := s.spool.RecoverSending(ctx); err != nil {
		s.log.Error("spool恢复失败", "error", err)
	} else if n > 0 {
		s.log.Info("spool恢复", "resent_queued", n)
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
			job, err := s.spool.NextDue(ctx, time.Now())
			if err != nil || job == nil {
				break
			}
			s.deliverCustody(ctx, job)
		}
	}
}

func (s *Server) deliverCustody(ctx context.Context, job *OutboundJob) {
	var req custodyRequest
	if err := json.Unmarshal(job.Body, &req); err != nil { // 理论不可达(入队前已 marshal)
		_, _ = s.spool.MarkRetry(ctx, job.OutboundID, "corrupt body: "+err.Error(),
			s.maxAttempts()-1, s.maxAttempts(), 0)
		return
	}
	sender, ok := s.senderFor(req.BotChannel)
	if !ok {
		_, _ = s.spool.MarkRetry(ctx, job.OutboundID, "bot_channel gone",
			job.Attempts, s.maxAttempts(), custodyBackoff(job.Attempts))
		return
	}
	sctx, cancel := context.WithTimeout(ctx, s.cfg.ForwardTO)
	msgID, err := sender.Send(sctx, feishu.OutMessage{
		BotChannelID: req.BotChannel, ToID: req.ToID, ToName: req.ToName,
		ToType: req.ToType, Text: req.Text,
	})
	cancel()
	// 审计:凭据每次使用留痕+outbound_id 三点关联(§C5)
	s.log.Info("custody发送审计", "outbound_id", job.OutboundID, "caller", job.CallerID,
		"bot_channel", req.BotChannel, "to_id", req.ToID, "attempt", job.Attempts+1,
		"msg_id", msgID, "error", errStr(err))
	if err == nil {
		s.metrics.OutboundSent.Add(1)
		if e := s.spool.MarkSent(ctx, job.OutboundID, msgID); e != nil {
			s.log.Error("MarkSent失败", "outbound_id", job.OutboundID, "error", e)
		}
		return
	}
	dlq, e := s.spool.MarkRetry(ctx, job.OutboundID, err.Error(),
		job.Attempts, s.maxAttempts(), custodyBackoff(job.Attempts))
	if e != nil {
		s.log.Error("MarkRetry失败", "outbound_id", job.OutboundID, "error", e)
	}
	if dlq {
		s.metrics.OutboundDLQ.Add(1)
		s.log.Error("custody终败入DLQ(告警)", "outbound_id", job.OutboundID,
			"caller", job.CallerID, "error", err.Error())
	}
}

func (s *Server) maxAttempts() int {
	if s.cfg.MaxAttempts > 0 {
		return s.cfg.MaxAttempts
	}
	return 8
}
