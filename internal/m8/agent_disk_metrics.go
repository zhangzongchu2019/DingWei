package m8

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/agentmetrics"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// metricsBinding re-reads stable credential identity, including active owner and
// origin. Hosted allocations need their own namespace authority and stay unknown.
func (h *Hub) metricsBinding(ctx context.Context, c *sessionClient) (agentmetrics.Binding, bool) {
	if h.Identity == nil || c == nil || c.metricsAgentID == "" {
		return agentmetrics.Binding{}, false
	}
	ag, err := h.Identity.Store.GetAgent(ctx, c.metricsAgentID)
	if err != nil || ag.State != identity.AgentActive || ag.Origin != identity.OriginSelfhost || AgentKeyID(ag.OwnerLC) != c.keyID || ag.NameLC() != c.sessionName {
		return agentmetrics.Binding{}, false
	}
	user, err := h.Identity.Store.GetUser(ctx, ag.OwnerLC)
	if err != nil || user.State != identity.UserActive {
		return agentmetrics.Binding{}, false
	}
	return agentmetrics.Binding{Agent: ag.ID, Connection: c.metricsGeneration, Current: true}, true
}

// Reserved control subtype is consumed even on rejection; it never enters the
// retained conversation ring, AMR, cost, inbox or applied/ACK sequence pipeline.
func (h *Hub) handleAgentDiskMetrics(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "agent_environment_metrics" {
		return false
	}
	// Cheap admission precedes identity I/O. Charge attempts, including invalid
	// payloads/identities, so a connection cannot turn rejects into unbounded DB reads.
	if c == nil || len(env.Body) > agentmetrics.MaxBytes {
		return true
	}
	now := h.now()
	h.mu.Lock()
	if h.sessionClients[c.keyID][c.sessionName] != c ||
		(!c.lastMetricsAttempt.IsZero() && now.Sub(c.lastMetricsAttempt) < agentmetrics.SampleInterval) {
		h.mu.Unlock()
		return true
	}
	c.lastMetricsAttempt = now
	h.mu.Unlock()
	binding, ok := h.metricsBinding(ctx, c)
	if !ok {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return true
	}
	if c.diskMetrics == nil {
		c.diskMetrics = agentmetrics.New(binding)
	}
	_, _ = c.diskMetrics.Accept([]byte(env.Body), binding, h.now())
	return true
}

func (h *Hub) currentAgentDisk(ctx context.Context, fullName, agentID string) agentmetrics.Snapshot {
	out := agentmetrics.Snapshot{Agent: agentID, Status: "unavailable", Source: "helper_self_report"}
	if h.Identity == nil {
		return out
	}
	ag, _, err := h.Identity.ResolveFullName(ctx, fullName)
	if err != nil || ag.ID != agentID {
		return out
	}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()]
	h.mu.Unlock()
	binding, ok := h.metricsBinding(ctx, c)
	if !ok || binding.Agent != agentID {
		return out
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c || c.diskMetrics == nil {
		return out
	}
	return c.diskMetrics.Read(binding, h.now())
}

// Read-only service route. Authentication authorizes only this disk snapshot;
// it never mints a page/capability or touches cost or manual write leases.
func (h *Hub) HandleInternalAgentDiskResources(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("session")
	name, target := loadViewRouting().resolve(session, ownerFromSessionName(session))
	if target.Builtin || !target.ViewUnlockV2 || target.VSToHubSecretFile == "" || r.Header.Get("X-DW-VS-Target") != name {
		http.Error(w, "resources unavailable", 503)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2049))
	if err != nil || len(raw) == 0 || len(raw) > 2048 {
		http.Error(w, "invalid request", 400)
		return
	}
	file, err := os.Open(target.VSToHubSecretFile)
	if err != nil {
		http.Error(w, "service auth unavailable", 503)
		return
	}
	secret, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(secret) > 4096 || len(bytes.TrimSpace(secret)) < 32 {
		http.Error(w, "service auth unavailable", 503)
		return
	}
	stamp, nonce := r.Header.Get("X-DW-Timestamp"), r.Header.Get("X-DW-Nonce")
	ts, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || !viewV2NoncePattern.MatchString(nonce) || time.Since(time.Unix(ts, 0)).Abs() > time.Minute {
		http.Error(w, "invalid authentication", 401)
		return
	}
	want, _ := hex.DecodeString(signViewServiceRequest(bytes.TrimSpace(secret), vsToHubDirection, r.Method, r.URL.Path, stamp, nonce, raw))
	got, err := hex.DecodeString(r.Header.Get("X-DW-Signature"))
	if err != nil || !hmac.Equal(got, want) {
		http.Error(w, "invalid authentication", 401)
		return
	}
	if !h.claimViewV2Nonce(name, nonce, time.Now()) {
		http.Error(w, "replayed request", 409)
		return
	}
	var req struct {
		Agent    string `json:"agent_id"`
		FullName string `json:"full_name"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || req.FullName != session || req.Agent == "" {
		http.Error(w, "invalid request", 400)
		return
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		http.Error(w, "invalid request", 400)
		return
	}
	// A valid service signature cannot relabel an existing different stable Agent.
	if h.Identity == nil {
		http.Error(w, "identity unavailable", 503)
		return
	}
	ag, _, err := h.Identity.ResolveFullName(r.Context(), session)
	if err != nil || ag.ID != req.Agent {
		http.Error(w, "subject mismatch", 403)
		return
	}
	body, err := json.Marshal(map[string]any{"agent_id": req.Agent, "full_name": session, "agent_disk": h.currentAgentDisk(r.Context(), session, req.Agent), "inbox_lag": h.currentInboxLag(r.Context(), session, req.Agent)})
	if err != nil || len(body) > 4096 {
		http.Error(w, "resources unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
