package m8

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R60-③: heartbeat_report 的旧通路是 helper→datad 的 vsapi 管道
// (mcp_shim._ipc_vsapi → app._make_vsapi_handler → aiohttp)。新平台不部署 datad
// (docker-compose.yml 只有 workpulse)，且 requirements-runtime.lock 里没有 aiohttp，
// 于是 helper 抛错、shim 把确定失败折叠成 helper_rejected/outcome unknown。
// 自报心跳改走既有已认证会话 WS 的保留控制子类型，沿用 inbox_progress_v1 与
// delivery_status_query_v1 的形状：nonce 关联一次应答、不自动重发、无应答只报 unknown。
const (
	sessionHeartbeatSchema      = "session-heartbeat/v1"
	SessionHeartbeatCapability  = "session-heartbeat/v1"
	sessionHeartbeatMaxBytes    = 4096
	sessionHeartbeatTextRunes   = 128
	sessionHeartbeatMinInterval = time.Second
	sessionHeartbeatStaleAfter  = 30 * time.Minute
	sessionHeartbeatSource      = "helper_self_report"
)

var sessionHeartbeatStatuses = map[string]bool{"active": true, "busy": true, "blocked": true, "idle": true}

type sessionHeartbeatSample struct {
	Schema  string `json:"schema"`
	Status  string `json:"status"`
	Task    string `json:"task,omitempty"`
	ETA     string `json:"eta,omitempty"`
	Blocker string `json:"blocker,omitempty"`
}

type sessionHeartbeatCache struct {
	sample   sessionHeartbeatSample
	received time.Time
}

func sessionHeartbeatText(text string) bool {
	if utf8.RuneCountInString(text) > sessionHeartbeatTextRunes {
		return false
	}
	return !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) })
}

func validSessionHeartbeat(body string) (sessionHeartbeatSample, bool) {
	var sample sessionHeartbeatSample
	if len(body) == 0 || len(body) > sessionHeartbeatMaxBytes {
		return sample, false
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&sample) != nil {
		return sessionHeartbeatSample{}, false
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return sessionHeartbeatSample{}, false
	}
	if sample.Schema != sessionHeartbeatSchema || !sessionHeartbeatStatuses[sample.Status] {
		return sessionHeartbeatSample{}, false
	}
	if !sessionHeartbeatText(sample.Task) || !sessionHeartbeatText(sample.ETA) || !sessionHeartbeatText(sample.Blocker) {
		return sessionHeartbeatSample{}, false
	}
	return sample, true
}

// Reserved telemetry/control subtype: consumed even on rejection, and it never
// enters the conversation ring, AMR, inbox, delivery ACK or cost pipeline. The
// reporter is always the authenticated connection; a claimed name in the body is
// never read (datad 的自报域同理：session != 目标行即拒)。
func (h *Hub) handleSessionHeartbeat(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "session_heartbeat_v1" {
		return false
	}
	if c == nil || len(env.Body) > sessionHeartbeatMaxBytes {
		return true
	}
	nonce := metaString(env.Meta, "nonce")
	now := h.now()
	h.mu.Lock()
	current := h.sessionClients[c.keyID][c.sessionName] == c
	lastAttempt := c.lastHeartbeatAttempt
	h.mu.Unlock()
	if !current || !inboxInstance.MatchString(nonce) {
		// 被顶替的连接与无法关联的报告都不应答：对端无从消费，也不给它探测面。
		return true
	}
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < sessionHeartbeatMinInterval {
		h.replySessionHeartbeat(ctx, c, nonce, map[string]any{"ok": false, "status": "throttled",
			"retry_after_ms": (sessionHeartbeatMinInterval - now.Sub(lastAttempt)).Milliseconds()})
		return true
	}
	// Charge attempts before validation so a connection cannot turn rejects into
	// unbounded identity lookups (same discipline as agent_environment_metrics).
	h.mu.Lock()
	c.lastHeartbeatAttempt = now
	h.mu.Unlock()
	sample, ok := validSessionHeartbeat(env.Body)
	if !ok {
		h.replySessionHeartbeat(ctx, c, nonce, map[string]any{"ok": false, "status": "invalid"})
		return true
	}
	if _, bound := h.metricsBinding(ctx, c); !bound {
		h.replySessionHeartbeat(ctx, c, nonce, map[string]any{"ok": false, "status": "unavailable"})
		return true
	}
	h.mu.Lock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		h.mu.Unlock()
		return true
	}
	c.heartbeat = &sessionHeartbeatCache{sample: sample, received: now}
	h.mu.Unlock()
	log.Printf("session heartbeat recorded key_id=%s session=%s conn_seq=%d status=%s task_runes=%d blocker_runes=%d",
		c.keyID, c.sessionName, c.connSeq, sample.Status, utf8.RuneCountInString(sample.Task), utf8.RuneCountInString(sample.Blocker))
	h.replySessionHeartbeat(ctx, c, nonce, map[string]any{"ok": true, "status": "recorded", "recorded_at_ms": now.UnixMilli()})
	return true
}

func (h *Hub) replySessionHeartbeat(ctx context.Context, c *sessionClient, nonce string, result map[string]any) {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = c.write(bounded, model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: h.now().Unix(), Meta: map[string]any{"type": "session_heartbeat_result_v1", "nonce": nonce, "result": result, "no_mirror": true, "system": true}})
}

// Owner-side read. An absent or old self-report is never rendered as a fresh
// zero, and reads cannot renew it (same contract as inboxLagForClient).
func (h *Hub) sessionHeartbeatForClient(ctx context.Context, c *sessionClient) map[string]any {
	result := map[string]any{"status": "offline", "source": sessionHeartbeatSource}
	if c == nil {
		return result
	}
	if _, ok := h.metricsBinding(ctx, c); !ok {
		result["status"] = "unknown"
		return result
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return result
	}
	if c.heartbeat == nil {
		result["status"] = "unsupported"
		return result
	}
	age := h.now().Sub(c.heartbeat.received)
	if age < 0 {
		age = 0
	}
	sample := c.heartbeat.sample
	report := map[string]any{"status": sample.Status}
	if sample.Task != "" {
		report["task"] = sample.Task
	}
	if sample.ETA != "" {
		report["eta"] = sample.ETA
	}
	if sample.Blocker != "" {
		report["blocker"] = sample.Blocker
	}
	result["status"] = "ok"
	result["report"] = report
	result["observed_at_ms"] = c.heartbeat.received.UnixMilli()
	result["age_ms"] = age.Milliseconds()
	result["stale"] = age >= sessionHeartbeatStaleAfter
	result["connection_seq"] = c.connSeq
	return result
}

func (h *Hub) currentSessionHeartbeat(ctx context.Context, fullName, agentID string) map[string]any {
	unavailable := map[string]any{"status": "unknown", "source": sessionHeartbeatSource}
	if h.Identity == nil {
		return unavailable
	}
	ag, _, err := h.Identity.ResolveFullName(ctx, fullName)
	if err != nil || ag.ID != agentID {
		return unavailable
	}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()]
	h.mu.Unlock()
	return h.sessionHeartbeatForClient(ctx, c)
}
