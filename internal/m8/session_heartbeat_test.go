package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func heartbeatBody(status, task, eta, blocker string) string {
	raw, _ := json.Marshal(map[string]string{"schema": sessionHeartbeatSchema, "status": status,
		"task": task, "eta": eta, "blocker": blocker})
	return string(raw)
}

func heartbeatEnvelope(nonce, body string) model.Envelope {
	return model.Envelope{Body: body, Meta: map[string]any{"type": "session_heartbeat_v1", "nonce": nonce, "no_mirror": true}}
}

func seedHeartbeat(t *testing.T, h *Hub, c *sessionClient, sample sessionHeartbeatSample, received time.Time) {
	t.Helper()
	h.mu.Lock()
	c.heartbeat = &sessionHeartbeatCache{sample: sample, received: received}
	h.mu.Unlock()
}

func resetHeartbeatThrottle(h *Hub, c *sessionClient) {
	h.mu.Lock()
	c.lastHeartbeatAttempt = time.Time{}
	h.mu.Unlock()
}

func oversizeHeartbeatBody() string {
	return `{"schema":"session-heartbeat/v1","status":"active","task":"` + strings.Repeat("x", sessionHeartbeatMaxBytes) + `"}`
}

func waitHeartbeatClient(t *testing.T, e *agentEnv, keyID string) *sessionClient {
	t.Helper()
	var c *sessionClient
	waitFor(t, 2*time.Second, "session client", func() bool {
		e.hub.mu.Lock()
		c = e.hub.sessionClients[keyID]["opus50ts"]
		e.hub.mu.Unlock()
		return c != nil
	})
	return c
}

// The registry moves to a replacement while the superseded socket keeps being
// served by its own read loop. The replacement owns a real websocket so an
// accidental reply is observable instead of a nil-conn panic (same fixture shape
// as attachAMRNoticeClient). Returns the replacement's peer side.
func publishHeartbeatReplacement(t *testing.T, e *agentEnv, ctx context.Context, old *sessionClient) (*websocket.Conn, *sessionClient) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-release
	}))
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		close(release)
		server.Close()
		t.Fatal(err)
	}
	serverConn := <-accepted
	replacement := &sessionClient{conn: serverConn, keyID: old.keyID, sessionName: old.sessionName,
		metricsAgentID: old.metricsAgentID, metricsGeneration: old.metricsGeneration, connSeq: old.connSeq + 1}
	e.hub.mu.Lock()
	e.hub.sessionClients[old.keyID][old.sessionName] = replacement
	e.hub.mu.Unlock()
	t.Cleanup(func() {
		peer.CloseNow()
		serverConn.CloseNow()
		close(release)
		server.Close()
	})
	return peer, replacement
}

// replyFrames reads a connection in the background and forwards only frames that
// could answer a self-report: heartbeat results and error envelopes. Two details
// force this shape. assertNoEnvelope stops at the first ignorable frame, and a
// freshly admitted connection always gets the onboarding push first, so a heartbeat
// reply queued right behind it would stay hidden. And reading with a deadline
// context closes the connection, which would break every later send in the same
// case, so silence is asserted with a timer over an ongoing read instead.
func replyFrames(ctx context.Context, conn *websocket.Conn) <-chan model.Envelope {
	replies := make(chan model.Envelope, 16)
	go func() {
		defer close(replies)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var env model.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}
			if metaString(env.Meta, "type") == "session_heartbeat_result_v1" || metaBool(env.Meta, "error") {
				replies <- env
			}
		}
	}()
	return replies
}

func assertNoHeartbeatReply(t *testing.T, replies <-chan model.Envelope, wait time.Duration, what string) {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case env, open := <-replies:
		if open {
			t.Fatalf("%s must be dropped silently, but the connection answered: %+v", what, env)
		}
	case <-timer.C:
	}
}

func waitHeartbeatReply(t *testing.T, replies <-chan model.Envelope, wait time.Duration) model.Envelope {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case env, open := <-replies:
		if !open {
			t.Fatal("the connection closed before the reply arrived")
		}
		return env
	case <-timer.C:
		t.Fatal("no reply within the window")
	}
	return model.Envelope{}
}

// Real authenticated socket: capability header, correlated reply, throttle,
// stored self-report and the rule that a control subtype never reaches a peer.
func TestSessionHeartbeatWireRoundTripThrottleAndPeerIsolation(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	peerCred, peerID := e.enroll("hb-wire-peer", "HBPeer")
	group, err := e.svc.CreateGroup(ctx, "zzc", e.agID, "hbwire")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(ctx, "zzc", group.Code, peerID); err != nil {
		t.Fatal(err)
	}
	source, headers := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	if headers.Get("X-DW-Session-Heartbeat") != SessionHeartbeatCapability {
		t.Fatal("capability header missing", headers)
	}
	peer, _, _ := e.dial(peerCred, "")
	defer peer.CloseNow()
	key := AgentKeyID("zzc")
	send := func(conn *websocket.Conn, name string, env model.Envelope) {
		t.Helper()
		env.From = name + "#enrolled"
		if env.ID == "" {
			env.ID = randomHex(16)
		}
		if env.To == "" {
			env.To = "workpulse#" + key
		}
		if err := writeEnvelope(ctx, conn, env); err != nil {
			t.Fatal(err)
		}
	}
	waitClient := func() *sessionClient {
		var c *sessionClient
		waitFor(t, 2*time.Second, "session client", func() bool {
			e.hub.mu.Lock()
			c = e.hub.sessionClients[key]["opus50ts"]
			e.hub.mu.Unlock()
			return c != nil
		})
		return c
	}
	client := waitClient()

	send(source, "opus50ts", heartbeatEnvelope(strings.Repeat("a", 32), heartbeatBody("busy", "R60-③", "2026-11-18T12:00Z", "")))
	reply := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
	result, _ := reply.Meta["result"].(map[string]any)
	if result["ok"] != true || result["status"] != "recorded" || result["recorded_at_ms"] == nil {
		t.Fatal("no recorded reply", reply.Meta)
	}
	if metaString(reply.Meta, "nonce") != strings.Repeat("a", 32) || reply.To != "opus50ts#"+key || reply.From != "workpulse#"+key {
		t.Fatal("reply is not correlated to the reporter", reply)
	}
	if reply.Meta["no_mirror"] != true || reply.Meta["system"] != true {
		t.Fatal("control reply is not marked reserved", reply.Meta)
	}
	got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID)
	report, _ := got["report"].(map[string]any)
	if got["status"] != "ok" || report["status"] != "busy" || report["task"] != "R60-③" || report["eta"] != "2026-11-18T12:00Z" {
		t.Fatal("stored self-report", got)
	}
	if _, leaked := report["blocker"]; leaked {
		t.Fatal("empty optional text was materialised", report)
	}
	// The reserved subtype is consumed in place: a group peer sees nothing.
	assertNoEnvelope(t, ctx, peer, 250*time.Millisecond)

	// A second report inside the interval is refused with a definite answer and
	// cannot overwrite the stored sample.
	send(source, "opus50ts", heartbeatEnvelope(strings.Repeat("b", 32), heartbeatBody("idle", "second", "", "")))
	throttled := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
	throttledResult, _ := throttled.Meta["result"].(map[string]any)
	if throttledResult["status"] != "throttled" || throttledResult["ok"] != false {
		t.Fatal("expected throttled reply", throttled.Meta)
	}
	// The reply crossed JSON, so the bounded wait arrives as a float.
	if retryAfter, ok := throttledResult["retry_after_ms"].(float64); !ok || retryAfter <= 0 || retryAfter > 1000 {
		t.Fatal("retry_after_ms", throttledResult)
	}
	if stored := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID); stored["report"].(map[string]any)["status"] != "busy" {
		t.Fatal("throttled report overwrote the sample", stored)
	}

	// Advancing the ingress clock avoids a timing-based sleep (inbox_lag 同款).
	resetHeartbeatThrottle(e.hub, client)
	send(source, "opus50ts", heartbeatEnvelope(strings.Repeat("c", 32), heartbeatBody("blocked", "", "", "waiting for review")))
	readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
	updated := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID)
	updatedReport, _ := updated["report"].(map[string]any)
	if updatedReport["status"] != "blocked" || updatedReport["blocker"] != "waiting for review" {
		t.Fatal("accepted follow-up report", updated)
	}
	if updated["age_ms"] == nil || updated["stale"] != false || updated["source"] != sessionHeartbeatSource {
		t.Fatal("read metadata", updated)
	}
}

// Every rejection is a definite reply (never silence, never a stored sample).
func TestSessionHeartbeatInvalidPayloadsAreRejectedNotStored(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	source, _ := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	key := AgentKeyID("zzc")
	waitFor(t, 2*time.Second, "session client", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		return e.hub.sessionClients[key]["opus50ts"] != nil
	})
	client := func() *sessionClient {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		return e.hub.sessionClients[key]["opus50ts"]
	}
	cases := map[string]string{
		"unknown_status":   heartbeatBody("paused", "", "", ""),
		"empty_status":     heartbeatBody("", "", "", ""),
		"wrong_schema":     `{"schema":"session-heartbeat/v2","status":"active"}`,
		"unknown_field":    `{"schema":"session-heartbeat/v1","status":"active","agent_id":"other"}`,
		"trailing_garbage": `{"schema":"session-heartbeat/v1","status":"active"} {"schema":"session-heartbeat/v1","status":"idle"}`,
		"oversize_text":    heartbeatBody("active", strings.Repeat("心", sessionHeartbeatTextRunes+1), "", ""),
		"control_char":     heartbeatBody("active", "task\nwith\nnewlines", "", ""),
		"empty_body":       "",
		"not_json":         "heartbeat",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resetHeartbeatThrottle(e.hub, client())
			env := heartbeatEnvelope(strings.Repeat("d", 32), body)
			env.From, env.To, env.ID = "opus50ts#enrolled", "workpulse#"+key, randomHex(16)
			if err := writeEnvelope(ctx, source, env); err != nil {
				t.Fatal(err)
			}
			reply := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
			result, _ := reply.Meta["result"].(map[string]any)
			if result["ok"] != false || result["status"] != "invalid" {
				t.Fatal("expected invalid", reply.Meta)
			}
			if got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID); got["status"] != "unsupported" {
				t.Fatal("rejected payload populated the cache", got)
			}
		})
	}
	t.Run("maximal_text_is_accepted_and_bounded", func(t *testing.T) {
		resetHeartbeatThrottle(e.hub, client())
		body := heartbeatBody("active", strings.Repeat("心", sessionHeartbeatTextRunes),
			strings.Repeat("跳", sessionHeartbeatTextRunes), strings.Repeat("阻", sessionHeartbeatTextRunes))
		env := heartbeatEnvelope(strings.Repeat("e", 32), body)
		env.From, env.To, env.ID = "opus50ts#enrolled", "workpulse#"+key, randomHex(16)
		if err := writeEnvelope(ctx, source, env); err != nil {
			t.Fatal(err)
		}
		reply := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
		if result, _ := reply.Meta["result"].(map[string]any); result["status"] != "recorded" {
			t.Fatal(reply.Meta)
		}
		got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID)
		if len(got["report"].(map[string]any)["task"].(string)) != sessionHeartbeatTextRunes*3 {
			t.Fatal("text was not preserved byte for byte", got)
		}
	})
}

// The write path is bound to the credential-admitted identity: once that binding
// no longer resolves to an active owner, the report is refused with a definite
// "unavailable" and nothing is stored. The owner is disabled through the identity
// store rather than by poking sessionClient fields, which a live connection reads
// from background goroutines.
func TestSessionHeartbeatUnboundConnectionGetsDefiniteUnavailable(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	source, _ := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	key := AgentKeyID("zzc")
	var client *sessionClient
	waitFor(t, 2*time.Second, "session client", func() bool {
		e.hub.mu.Lock()
		client = e.hub.sessionClients[key]["opus50ts"]
		e.hub.mu.Unlock()
		return client != nil
	})
	mem, ok := e.svc.Store.(*identity.MemStore)
	if !ok {
		t.Fatal("fixture identity store is not the in-memory store")
	}
	if err := mem.SetUserState(ctx, "zzc", identity.UserDisabled, time.Now()); err != nil {
		t.Fatal(err)
	}
	resetHeartbeatThrottle(e.hub, client)
	env := heartbeatEnvelope(strings.Repeat("a", 32), heartbeatBody("active", "task", "", ""))
	env.From, env.To, env.ID = "opus50ts#enrolled", "workpulse#"+key, randomHex(16)
	if err := writeEnvelope(ctx, source, env); err != nil {
		t.Fatal(err)
	}
	reply := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
	result, _ := reply.Meta["result"].(map[string]any)
	if result["ok"] != false || result["status"] != "unavailable" {
		t.Fatal("a connection whose owner is not active was allowed to self-report", reply.Meta)
	}
	e.hub.mu.Lock()
	stored := client.heartbeat
	e.hub.mu.Unlock()
	if stored != nil {
		t.Fatal("refused report was stored", stored)
	}
}

// Silent consumption: a superseded connection, a bad nonce or an oversized body
// gets no reply at all, so nothing can probe or amplify through this subtype.
func TestSessionHeartbeatSilentDrops(t *testing.T) {
	for _, mode := range []string{"bad_nonce", "short_nonce", "oversize_body", "old_connection", "nil_client"} {
		t.Run(mode, func(t *testing.T) {
			h, c, _ := diskEnv(t)
			ctx := context.Background()
			env := heartbeatEnvelope(strings.Repeat("f", 32), heartbeatBody("active", "task", "", ""))
			target := c
			switch mode {
			case "bad_nonce":
				env.Meta["nonce"] = "not-hex"
			case "short_nonce":
				env.Meta["nonce"] = strings.Repeat("f", 31)
			case "oversize_body":
				env.Body = oversizeHeartbeatBody()
			case "old_connection":
				replacement := &sessionClient{keyID: c.keyID, sessionName: c.sessionName, metricsAgentID: c.metricsAgentID, connSeq: c.connSeq + 1}
				h.sessionClients[c.keyID][c.sessionName] = replacement
			case "nil_client":
				target = nil
			}
			if !h.handleSessionHeartbeat(ctx, target, env) {
				t.Fatal("reserved subtype was not consumed")
			}
			h.mu.Lock()
			stored := c.heartbeat
			h.mu.Unlock()
			if stored != nil {
				t.Fatal("silent drop stored a sample", stored)
			}
			if got := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID); got["status"] == "ok" {
				t.Fatal("silent drop became readable", got)
			}
		})
	}
}

// Owner-side read: identity bound, connection bound, and an old self-report is
// never presented as fresh (reads cannot renew it either).
func TestSessionHeartbeatReadAuthorizationAndFreshness(t *testing.T) {
	h, c, mem := diskEnv(t)
	ctx := context.Background()
	now := time.Now()
	h.NowFunc = func() time.Time { return now }
	agentID := c.metricsAgentID
	sample := sessionHeartbeatSample{Schema: sessionHeartbeatSchema, Status: "blocked", Task: "R60-③", Blocker: "waiting"}
	if got := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID); got["status"] != "unsupported" {
		t.Fatal("absent report must not read as a fresh zero", got)
	}
	seedHeartbeat(t, h, c, sample, now)
	got := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID)
	if got["status"] != "ok" || got["report"].(map[string]any)["blocker"] != "waiting" || got["age_ms"] != int64(0) || got["stale"] != false {
		t.Fatal(got)
	}
	// The caller-supplied agent id must match the identity resolution of the
	// full name even while the connection itself stays legitimately bound.
	if mismatched := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", "another-agent-id"); mismatched["status"] != "unknown" {
		t.Fatal("a mismatched caller agent id read the bound report", mismatched)
	}
	now = now.Add(time.Minute)
	if renewed := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID); renewed["age_ms"] != int64(60000) || renewed["observed_at_ms"] != got["observed_at_ms"] {
		t.Fatal("read renewed the sample", renewed)
	}
	now = now.Add(sessionHeartbeatStaleAfter)
	if stale := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID); stale["stale"] != true || stale["status"] != "ok" {
		t.Fatal("old report must stay visible but be marked stale", stale)
	}
	c.metricsAgentID = ""
	if unbound := h.sessionHeartbeatForClient(ctx, c); unbound["status"] != "unknown" {
		t.Fatal("unbound connection reported a heartbeat", unbound)
	}
	c.metricsAgentID = "other"
	if relabelled := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", "other"); relabelled["status"] != "unknown" {
		t.Fatal("relabelled agent id read the cache", relabelled)
	}
	c.metricsAgentID = ""
	seedHeartbeat(t, h, c, sample, now)
	if wrong := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", "does-not-exist"); wrong["status"] != "unknown" {
		t.Fatal(wrong)
	}
	c.metricsAgentID = agentID
	replacement := &sessionClient{keyID: c.keyID, sessionName: c.sessionName, metricsAgentID: c.metricsAgentID, connSeq: c.connSeq + 1}
	h.mu.Lock()
	h.sessionClients[c.keyID][c.sessionName] = replacement
	h.mu.Unlock()
	if offline := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", c.metricsAgentID); offline["status"] != "unsupported" {
		t.Fatal("a new connection inherited the old report", offline)
	}
	// Same race on the direct read path: the sample belongs to the superseded
	// connection, so it must read as offline rather than as a live report.
	if inherited := h.sessionHeartbeatForClient(ctx, c); inherited["status"] != "offline" {
		t.Fatal("a superseded connection still exposed its report", inherited)
	}
	if err := mem.SetUserState(ctx, "zzc", identity.UserDisabled, now); err != nil {
		t.Fatal(err)
	}
	seedHeartbeat(t, h, replacement, sample, now)
	if disabled := h.currentSessionHeartbeat(ctx, "DiskAgent.zzc", replacement.metricsAgentID); disabled["status"] != "unknown" {
		t.Fatal("disabled owner still exposed the report", disabled)
	}
}

// The owner-side signed route stays within its 4096 byte bound even with a
// maximal CJK self-report; the field is additive to agent_disk / inbox_lag.
func TestSessionHeartbeatOwnerRouteStaysWithinBound(t *testing.T) {
	h, c, _ := diskEnv(t)
	ctx := context.Background()
	h.handleAgentDiskMetrics(ctx, c, diskEnvelope(time.Now()))
	seedHeartbeat(t, h, c, sessionHeartbeatSample{Schema: sessionHeartbeatSchema, Status: "blocked",
		Task: strings.Repeat("心", sessionHeartbeatTextRunes), ETA: strings.Repeat("跳", sessionHeartbeatTextRunes),
		Blocker: strings.Repeat("阻", sessionHeartbeatTextRunes)}, time.Now())
	dir := t.TempDir()
	secret := []byte(strings.Repeat("s", 32))
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, secret, 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"sessions": map[string]string{"DiskAgent.zzc": "fixture"}, "targets": map[string]any{"fixture": map[string]any{"url": "http://127.0.0.1:19321", "view_unlock_v2": true, "vs_to_hub_secret_file": key}}})
	file := filepath.Join(dir, "routes")
	if err := os.WriteFile(file, config, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", file)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/view-v2/{session}/resources", h.HandleInternalAgentDiskResources)
	path := "/internal/view-v2/DiskAgent.zzc/resources"
	body := fmt.Sprintf(`{"agent_id":%q,"full_name":"DiskAgent.zzc"}`, c.metricsAgentID)
	nonce := strings.Repeat("a", 32)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("X-DW-VS-Target", "fixture")
	req.Header.Set("X-DW-Timestamp", stamp)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, vsToHubDirection, "POST", path, stamp, nonce, []byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(w.Body.Bytes()) > 4096 {
		t.Fatal("response exceeded the documented bound", len(w.Body.Bytes()))
	}
	var decoded map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	heartbeat, ok := decoded["session_heartbeat"].(map[string]any)
	if !ok || heartbeat["status"] != "ok" {
		t.Fatal("session_heartbeat missing", decoded)
	}
	report := heartbeat["report"].(map[string]any)
	if report["status"] != "blocked" || len(report["blocker"].(string)) != sessionHeartbeatTextRunes*3 {
		t.Fatal(report)
	}
	if decoded["agent_disk"] == nil || decoded["inbox_lag"] == nil {
		t.Fatal("additive field displaced an existing one", decoded)
	}
}

// 组长改单 a（21:59:28Z）：注册表已指向替代连接、而旧连接的读循环仍在服务时，
// 三类正文都必须静默——合法、语义不合法、超界。第二道 current 复核只挡住缓存写入，
// 挡不住应答，所以删掉 session_heartbeat.go:94 的首道 !current 必须让 invalid 格转红。
// 静默丢弃也不计费：计费发生在三道闸门之后。
func TestSessionHeartbeatSupersededConnectionIsSilentForEveryBodyClass(t *testing.T) {
	keyID := AgentKeyID("zzc")
	cases := []struct{ name, body string }{
		{"legal_body", heartbeatBody("active", "R60-③", "", "")},
		{"invalid_body", heartbeatBody("paused", "", "", "")},
		{"oversize_body", oversizeHeartbeatBody()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newAgentEnv(t)
			ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
			defer cancel()
			source, _ := e.dialWithHeaders(e.cred, "")
			defer source.CloseNow()
			old := waitHeartbeatClient(t, e, keyID)
			replacementPeer, replacement := publishHeartbeatReplacement(t, e, ctx, old)
			sourceReplies := replyFrames(ctx, source)
			replacementReplies := replyFrames(ctx, replacementPeer)
			env := heartbeatEnvelope(strings.Repeat("a", 32), tc.body)
			env.From, env.To, env.ID = "opus50ts#enrolled", "workpulse#"+keyID, randomHex(16)
			if err := writeEnvelope(ctx, source, env); err != nil {
				t.Fatal(err)
			}
			assertNoHeartbeatReply(t, sourceReplies, 400*time.Millisecond, tc.name)
			assertNoHeartbeatReply(t, replacementReplies, 200*time.Millisecond, "replacement of "+tc.name)
			e.hub.mu.Lock()
			stored, billed := old.heartbeat, old.lastHeartbeatAttempt
			inherited := replacement.heartbeat
			e.hub.mu.Unlock()
			if stored != nil || inherited != nil {
				t.Fatal("silent drop stored a sample", stored, inherited)
			}
			if !billed.IsZero() {
				t.Fatal("a dropped frame was billed to the superseded connection", billed)
			}
			if got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID); got["status"] == "ok" {
				t.Fatal("silent drop became readable", got)
			}
		})
	}
}

// 组长改单 b：尝试先计费、后校验。语义不合法的一帧消耗掉该连接 1 秒额度，
// 紧随其后的合法帧拿到确定的 throttled 而不是 recorded。把计费移到
// validSessionHeartbeat 之后必须让本用例转红。
func TestSessionHeartbeatInvalidAttemptIsBilledBeforeValidation(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	source, _ := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	keyID := AgentKeyID("zzc")
	client := waitHeartbeatClient(t, e, keyID)
	report := func(nonce, body string) map[string]any {
		t.Helper()
		env := heartbeatEnvelope(nonce, body)
		env.From, env.To, env.ID = "opus50ts#enrolled", "workpulse#"+keyID, randomHex(16)
		if err := writeEnvelope(ctx, source, env); err != nil {
			t.Fatal(err)
		}
		reply := readEnvelopeOfType(t, ctx, source, "session_heartbeat_result_v1")
		if metaString(reply.Meta, "nonce") != nonce {
			t.Fatal("reply is not correlated to the request", reply.Meta)
		}
		result, _ := reply.Meta["result"].(map[string]any)
		return result
	}
	if result := report(strings.Repeat("b", 32), heartbeatBody("paused", "", "", "")); result["ok"] != false || result["status"] != "invalid" {
		t.Fatal("expected a definite invalid", result)
	}
	result := report(strings.Repeat("c", 32), heartbeatBody("active", "legal right after invalid", "", ""))
	if result["ok"] != false || result["status"] != "throttled" {
		t.Fatal("the invalid attempt was not billed, so the legal frame escaped the throttle", result)
	}
	if retryAfter, ok := result["retry_after_ms"].(float64); !ok || retryAfter <= 0 || retryAfter > 1000 {
		t.Fatal("retry_after_ms", result)
	}
	if got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID); got["status"] != "unsupported" {
		t.Fatal("neither frame may store a sample", got)
	}
	e.hub.mu.Lock()
	billed := client.lastHeartbeatAttempt
	e.hub.mu.Unlock()
	if billed.IsZero() {
		t.Fatal("the rejected attempt never reached the billing point")
	}
}

// 组长改单 c：4096 字节上界在计费之前判定，所以超界帧既不应答也不占额度，
// 紧随其后的合法帧仍被 recorded。把超界判断挪到计费之后必须让本用例转红。
func TestSessionHeartbeatOversizeFrameIsNotBilled(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	source, _ := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	keyID := AgentKeyID("zzc")
	client := waitHeartbeatClient(t, e, keyID)
	replies := replyFrames(ctx, source)
	oversize := heartbeatEnvelope(strings.Repeat("d", 32), oversizeHeartbeatBody())
	oversize.From, oversize.To, oversize.ID = "opus50ts#enrolled", "workpulse#"+keyID, randomHex(16)
	if err := writeEnvelope(ctx, source, oversize); err != nil {
		t.Fatal(err)
	}
	assertNoHeartbeatReply(t, replies, 400*time.Millisecond, "the oversize frame")
	legal := heartbeatEnvelope(strings.Repeat("e", 32), heartbeatBody("busy", "legal right after oversize", "", ""))
	legal.From, legal.To, legal.ID = "opus50ts#enrolled", "workpulse#"+keyID, randomHex(16)
	if err := writeEnvelope(ctx, source, legal); err != nil {
		t.Fatal(err)
	}
	reply := waitHeartbeatReply(t, replies, envelopeReadTimeout)
	if metaString(reply.Meta, "nonce") != strings.Repeat("e", 32) {
		t.Fatal("the oversize frame answered, or the reply is not correlated", reply.Meta)
	}
	if result, _ := reply.Meta["result"].(map[string]any); result["ok"] != true || result["status"] != "recorded" {
		t.Fatal("an unbilled oversize frame must not throttle the next legal report", reply.Meta)
	}
	got := e.hub.currentSessionHeartbeat(ctx, "Opus50TS.zzc", e.agID)
	report, _ := got["report"].(map[string]any)
	if got["status"] != "ok" || report["status"] != "busy" || report["task"] != "legal right after oversize" {
		t.Fatal("stored self-report", got)
	}
	e.hub.mu.Lock()
	billed := client.lastHeartbeatAttempt
	e.hub.mu.Unlock()
	if billed.IsZero() {
		t.Fatal("the legal frame was never billed")
	}
}
