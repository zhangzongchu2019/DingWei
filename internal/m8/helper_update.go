package m8

// Durable helper-update catalog and job primitives.  The Console surface and
// legacy public provision retirement live in the separately assigned branch;
// this file deliberately contains only Hub-owned state and wire validation.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	HelperUpdateCatalogPrefix = "helper_update.catalog."
	helperUpdateJobPrefix     = "helper_update.job."
	helperUpdateWireVersion   = "helper-update/v1"
)

type HelperUpdateCatalog struct {
	SHA256     string    `json:"sha256"`
	Version    string    `json:"version"`
	URL        string    `json:"url"`
	LockSHA256 string    `json:"lock_sha256"`
	Platform   string    `json:"platform"`
	Registered time.Time `json:"registered_at"`
}

// helperUpdateJob is persisted under helper_update.job.<agent_id>.  Revision
// is part of the document so CAS transitions have an explicit audit value in
// addition to app_config's physical compare-and-swap predicate.
type helperUpdateJob struct {
	JobID       string    `json:"job_id"`
	ReleaseID   string    `json:"release_id"`
	AgentID     string    `json:"agent_id"`
	OwnerID     string    `json:"owner_id"`
	AgentName   string    `json:"agent_name"`
	Version     string    `json:"version"`
	SHA256      string    `json:"sha256"`
	LockSHA256  string    `json:"lock_sha256"`
	URL         string    `json:"url"`
	Platform    string    `json:"platform"`
	Stage       string    `json:"stage"`
	ExpiresAt   time.Time `json:"expires_at"`
	HealthNonce string    `json:"health_nonce"`
	Revision    int       `json:"revision"`
}

func HelperUpdateCatalogKey(sha string) string {
	return HelperUpdateCatalogPrefix + strings.ToLower(strings.TrimSpace(sha))
}
func helperUpdateJobKey(agentID string) string {
	return helperUpdateJobPrefix + strings.TrimSpace(agentID)
}

func validHelperUpdateSHA256(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func validateHelperUpdateCatalog(v HelperUpdateCatalog) error {
	if !validHelperUpdateSHA256(v.SHA256) {
		return fmt.Errorf("helper update sha256 invalid")
	}
	if !validHelperUpdateSHA256(v.LockSHA256) {
		return fmt.Errorf("helper update lock_sha256 invalid")
	}
	if strings.TrimSpace(v.Version) == "" || strings.TrimSpace(v.URL) == "" {
		return fmt.Errorf("helper update version/url required")
	}
	if strings.TrimSpace(v.Platform) != "linux" {
		return fmt.Errorf("helper update platform must be linux")
	}
	return nil
}

func marshalHelperUpdate(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *Hub) HelperUpdateCatalog(ctx context.Context, sha string) (*HelperUpdateCatalog, error) {
	if h.Repo == nil {
		return nil, fmt.Errorf("helper update repository unavailable")
	}
	key := HelperUpdateCatalogKey(sha)
	rows, err := h.Repo.ListAppConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("list helper update catalog: %w", err)
	}
	for _, row := range rows {
		if row.Key != key {
			continue
		}
		var catalog HelperUpdateCatalog
		if err := json.Unmarshal([]byte(row.ValueJSON), &catalog); err != nil {
			return nil, fmt.Errorf("decode helper update catalog: %w", err)
		}
		if err := validateHelperUpdateCatalog(catalog); err != nil {
			return nil, err
		}
		return &catalog, nil
	}
	return nil, nil
}

func (h *Hub) helperUpdateJob(ctx context.Context, agentID string) (*helperUpdateJob, string, error) {
	if h.Repo == nil {
		return nil, "", fmt.Errorf("helper update repository unavailable")
	}
	key := helperUpdateJobKey(agentID)
	rows, err := h.Repo.ListAppConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list helper update jobs: %w", err)
	}
	for _, row := range rows {
		if row.Key != key {
			continue
		}
		var job helperUpdateJob
		if err := json.Unmarshal([]byte(row.ValueJSON), &job); err != nil {
			return nil, "", fmt.Errorf("decode helper update job: %w", err)
		}
		return &job, row.ValueJSON, nil
	}
	return nil, "", nil
}

// RegisterHelperUpdateCatalog is intentionally an operations-only primitive.
// The command-line caller added by the Hub slice is its sole production entry;
// Console callers can select only a pre-registered SHA.
func (h *Hub) RegisterHelperUpdateCatalog(ctx context.Context, actor string, catalog HelperUpdateCatalog) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("catalog operator required")
	}
	if err := validateHelperUpdateCatalog(catalog); err != nil {
		return err
	}
	catalog.SHA256 = strings.ToLower(strings.TrimSpace(catalog.SHA256))
	catalog.LockSHA256 = strings.ToLower(strings.TrimSpace(catalog.LockSHA256))
	if catalog.Registered.IsZero() {
		catalog.Registered = h.now().UTC()
	}
	payload, err := marshalHelperUpdate(catalog)
	if err != nil {
		return err
	}
	created, err := h.Repo.CompareAndSwapAppConfig(ctx, HelperUpdateCatalogKey(catalog.SHA256), false, "", payload)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("helper update catalog already registered")
	}
	return h.Repo.WriteAudit(ctx, actor, "helper_update_catalog_registered", HelperUpdateCatalogKey(catalog.SHA256))
}

func (h *Hub) createHelperUpdateJob(ctx context.Context, catalog HelperUpdateCatalog, agentID, ownerID, agentName, issuer string) (*helperUpdateJob, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(agentName) == "" {
		return nil, fmt.Errorf("helper update agent identity required")
	}
	now := h.now().UTC()
	job := helperUpdateJob{
		JobID: randomHex(16), ReleaseID: catalog.SHA256, AgentID: agentID, OwnerID: ownerID, AgentName: agentName,
		Version: catalog.Version, SHA256: catalog.SHA256, LockSHA256: catalog.LockSHA256, URL: catalog.URL,
		Platform: catalog.Platform, Stage: "offered", ExpiresAt: now.Add(30 * time.Minute), HealthNonce: randomHex(16), Revision: 1,
	}
	payload, err := marshalHelperUpdate(job)
	if err != nil {
		return nil, err
	}
	previous, previousRaw, err := h.helperUpdateJob(ctx, agentID)
	if err != nil {
		return nil, err
	}
	expectedExists := previous != nil
	expectedRaw := previousRaw
	if previous != nil && !helperUpdateTerminal(previous.Stage) {
		return nil, fmt.Errorf("helper update job already exists")
	}
	created, err := h.Repo.CompareAndSwapAppConfig(ctx, helperUpdateJobKey(agentID), expectedExists, expectedRaw, payload)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, fmt.Errorf("helper update job changed concurrently")
	}
	if err := h.Repo.WriteAudit(ctx, issuer, "helper_update_offered", job.JobID); err != nil {
		return nil, err
	}
	return &job, nil
}

// IssueHelperUpdate creates one durable, target-bound offer and delivers it to
// the authenticated Agent's current session.  The Console authorization check
// is intentionally outside this primitive; it supplies issuer only after
// proving owner/manager scope.  We still re-check the target Agent and owner
// here so a stale Console page cannot offer to disabled identities.
func (h *Hub) IssueHelperUpdate(ctx context.Context, agentID, releaseSHA, issuer string) (*helperUpdateJob, error) {
	if h.Identity == nil {
		return nil, fmt.Errorf("helper update identity unavailable")
	}
	ag, err := h.Identity.Store.GetAgent(ctx, strings.TrimSpace(agentID))
	if err != nil || ag == nil || ag.State != identity.AgentActive {
		return nil, fmt.Errorf("helper update agent inactive")
	}
	owner, err := h.Identity.Store.GetUser(ctx, ag.OwnerLC)
	if err != nil || owner == nil || owner.State != identity.UserActive {
		return nil, fmt.Errorf("helper update owner inactive")
	}
	catalog, err := h.HelperUpdateCatalog(ctx, releaseSHA)
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, fmt.Errorf("helper update release unavailable")
	}
	job, err := h.createHelperUpdateJob(ctx, *catalog, ag.ID, ag.OwnerLC, ag.NameLC(), issuer)
	if err != nil {
		return nil, err
	}
	keyID := AgentKeyID(ag.OwnerLC)
	h.mu.Lock()
	c := h.sessionClients[keyID][ag.NameLC()]
	h.mu.Unlock()
	if c == nil {
		return job, nil // durable offered state is recovered on the next session.
	}
	if err := h.sendHelperUpdateOffer(ctx, c, job); err != nil {
		return job, err
	}
	return job, nil
}

func (h *Hub) sendHelperUpdateOffer(ctx context.Context, c *sessionClient, job *helperUpdateJob) error {
	meta := map[string]any{
		"type":         "provision",
		"system":       true,
		"no_mirror":    true,
		"action":       "update_self",
		"target":       "self",
		"job_id":       job.JobID,
		"release_id":   job.ReleaseID,
		"agent_id":     job.AgentID,
		"owner_id":     job.OwnerID,
		"agent_name":   job.AgentName,
		"version":      job.Version,
		"sha256":       job.SHA256,
		"lock_sha256":  job.LockSHA256,
		"url":          job.URL,
		"platform":     job.Platform,
		"expires_at":   job.ExpiresAt.UTC().Format(time.RFC3339),
		"health_nonce": job.HealthNonce,
	}
	return c.write(ctx, model.Envelope{
		ID: randomHex(16), To: sessionAddress(c.sessionName, c.keyID), From: sessionAddress("workpulse", c.keyID),
		TS: time.Now().Unix(), Meta: meta,
	})
}

func (h *Hub) confirmHelperUpdateHealth(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "helper_update_health" {
		return false
	}
	if c == nil || c.metricsAgentID == "" {
		return true // This control is credential-agent-only; never fall through to routing.
	}
	job, raw, err := h.helperUpdateJob(ctx, c.metricsAgentID)
	if err != nil || job == nil {
		return true
	}
	// All fields are echoed by the helper, but all identity-sensitive values are
	// compared with the durable job and authenticated connection, never trusted
	// because they appeared in the incoming envelope.
	if h.now().UTC().After(job.ExpiresAt) || job.Stage == "confirmed" ||
		job.AgentID != c.metricsAgentID || job.OwnerID != strings.TrimPrefix(c.keyID, "u_") ||
		job.AgentName != c.sessionName || c.version != job.Version ||
		metaString(env.Meta, "job_id") != job.JobID || metaString(env.Meta, "release_id") != job.ReleaseID ||
		metaString(env.Meta, "sha256") != job.SHA256 || metaString(env.Meta, "lock_sha256") != job.LockSHA256 ||
		metaString(env.Meta, "health_nonce") != job.HealthNonce {
		return true
	}
	updated := *job
	updated.Stage = "confirmed"
	updated.Revision++
	payload, err := marshalHelperUpdate(updated)
	if err != nil {
		return true
	}
	ok, err := h.Repo.CompareAndSwapAppConfig(ctx, helperUpdateJobKey(job.AgentID), true, raw, payload)
	if err != nil || !ok {
		return true
	}
	_ = h.Repo.WriteAudit(ctx, c.keyID, "helper_update_confirmed", job.JobID)
	_ = c.write(ctx, model.Envelope{
		ID: randomHex(16), To: sessionAddress(c.sessionName, c.keyID), From: sessionAddress("workpulse", c.keyID), TS: time.Now().Unix(),
		Meta: map[string]any{"type": "helper_update_confirm", "system": true, "no_mirror": true,
			"job_id": job.JobID, "release_id": job.ReleaseID, "agent_id": job.AgentID, "owner_id": job.OwnerID,
			"version": job.Version, "sha256": job.SHA256, "lock_sha256": job.LockSHA256, "health_nonce": job.HealthNonce},
	})
	return true
}

func helperUpdateReceiptStage(stage string) bool {
	switch stage {
	case "downloaded", "installed", "restarted", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

// recordHelperUpdateReceipt consumes only a receipt whose frozen tuple belongs
// to this authenticated Agent connection. The stage is durable but is not
// confirmation: only a fresh post-restart health nonce may promote confirmed.
func (h *Hub) recordHelperUpdateReceipt(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "provision_ack" || metaString(env.Meta, "action") != "update_self" {
		return false
	}
	if c == nil || c.metricsAgentID == "" {
		return true
	}
	job, raw, err := h.helperUpdateJob(ctx, c.metricsAgentID)
	if err != nil || job == nil || helperUpdateTerminal(job.Stage) {
		return true
	}
	stage := metaString(env.Meta, "stage")
	if !helperUpdateReceiptStage(stage) || h.now().UTC().After(job.ExpiresAt) ||
		metaString(env.Meta, "job_id") != job.JobID || metaString(env.Meta, "release_id") != job.ReleaseID ||
		metaString(env.Meta, "sha256") != job.SHA256 || metaString(env.Meta, "lock_sha256") != job.LockSHA256 {
		return true
	}
	updated := *job
	updated.Stage = stage
	if ok, err := h.updateHelperUpdateJob(ctx, raw, updated); err == nil && ok {
		_ = h.Repo.WriteAudit(ctx, c.keyID, "helper_update_"+stage, job.JobID)
	}
	return true
}

func helperUpdateTerminal(stage string) bool {
	switch stage {
	case "confirmed", "expired", "agent_inactive", "owner_inactive", "release_unavailable", "failed":
		return true
	default:
		return false
	}
}

func (h *Hub) updateHelperUpdateJob(ctx context.Context, raw string, job helperUpdateJob) (bool, error) {
	job.Revision++
	payload, err := marshalHelperUpdate(job)
	if err != nil {
		return false, err
	}
	return h.Repo.CompareAndSwapAppConfig(ctx, helperUpdateJobKey(job.AgentID), true, raw, payload)
}

// RecoverHelperUpdates is called after the new-platform identity store is
// wired. It never resurrects terminal jobs. Nonterminal jobs whose tuple is no
// longer eligible become terminal with an auditable reason; valid offered jobs
// wait for their authenticated Agent session to reconnect.
func (h *Hub) RecoverHelperUpdates(ctx context.Context) error {
	if h.Repo == nil || h.Identity == nil {
		return nil
	}
	rows, err := h.Repo.ListAppConfig(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.Key, helperUpdateJobPrefix) {
			continue
		}
		var job helperUpdateJob
		if err := json.Unmarshal([]byte(row.ValueJSON), &job); err != nil || helperUpdateTerminal(job.Stage) {
			continue
		}
		next := ""
		if h.now().UTC().After(job.ExpiresAt) {
			next = "expired"
		} else if ag, err := h.Identity.Store.GetAgent(ctx, job.AgentID); err != nil || ag == nil || ag.State != identity.AgentActive {
			next = "agent_inactive"
		} else if owner, err := h.Identity.Store.GetUser(ctx, job.OwnerID); err != nil || owner == nil || owner.State != identity.UserActive {
			next = "owner_inactive"
		} else if catalog, err := h.HelperUpdateCatalog(ctx, job.SHA256); err != nil || catalog == nil {
			next = "release_unavailable"
		}
		if next == "" {
			continue
		}
		job.Stage = next
		if ok, err := h.updateHelperUpdateJob(ctx, row.ValueJSON, job); err == nil && ok {
			_ = h.Repo.WriteAudit(ctx, "workpulse", "helper_update_"+next, job.JobID)
		}
	}
	return nil
}

func (h *Hub) resumeHelperUpdateOffer(ctx context.Context, c *sessionClient) {
	if c == nil || c.metricsAgentID == "" {
		return
	}
	job, raw, err := h.helperUpdateJob(ctx, c.metricsAgentID)
	if err != nil || job == nil || job.Stage != "offered" {
		return
	}
	if h.now().UTC().After(job.ExpiresAt) {
		job.Stage = "expired"
		if ok, err := h.updateHelperUpdateJob(ctx, raw, *job); err == nil && ok {
			_ = h.Repo.WriteAudit(ctx, "workpulse", "helper_update_expired", job.JobID)
		}
		return
	}
	_ = h.sendHelperUpdateOffer(ctx, c, job)
}
