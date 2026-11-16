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
				env.Body = `{"schema":"session-heartbeat/v1","status":"active","task":"` + strings.Repeat("x", sessionHeartbeatMaxBytes) + `"}`
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
