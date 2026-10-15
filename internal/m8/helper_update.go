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
	"net"
	"net/url"
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
	SHA256            string    `json:"sha256"`
	Version           string    `json:"version"`
	URL               string    `json:"url"`
	FileServiceOrigin string    `json:"file_service_origin"`
	LockSHA256        string    `json:"lock_sha256"`
	Platform          string    `json:"platform"`
	Registered        time.Time `json:"registered_at"`
}

// helperUpdateJob is persisted under helper_update.job.<agent_id>.  Revision
// is part of the document so CAS transitions have an explicit audit value in
// addition to app_config's physical compare-and-swap predicate.
type helperUpdateJob struct {
	JobID             string    `json:"job_id"`
	OfferID           string    `json:"offer_id"`
	ReleaseID         string    `json:"release_id"`
	AgentID           string    `json:"agent_id"`
	OwnerID           string    `json:"owner_id"`
	AgentName         string    `json:"agent_name"`
	Version           string    `json:"version"`
	SHA256            string    `json:"sha256"`
	LockSHA256        string    `json:"lock_sha256"`
	URL               string    `json:"url"`
	FileServiceOrigin string    `json:"file_service_origin"`
	Platform          string    `json:"platform"`
	Issuer            string    `json:"issuer"`
	Stage             string    `json:"stage"`
	Reason            string    `json:"reason,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
	// OfferConnSeq is the authenticated connection that first received this
	// offer. Health may only confirm from a later connection, after the helper
	// has restarted into the staged slot; it is never client supplied.
	OfferConnSeq int `json:"offer_conn_seq"`
	// ActivationConnSeq is recorded when the authenticated helper reports the
	// installed/restarted lifecycle boundary. Health confirmation must arrive
	// on a later connection, proving the candidate restart rather than merely
	// a reconnect after an offer was delivered.
	ActivationConnSeq int `json:"activation_conn_seq"`
	Revision          int `json:"revision"`
}

// HelperUpdateJob is the read-only Console projection of a durable job. The
// only state-changing public entry remains IssueHelperUpdateForActor.
type HelperUpdateJob = helperUpdateJob

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
	origin, err := canonicalHelperUpdateOrigin(v.FileServiceOrigin)
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(v.URL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("helper update catalog url invalid")
	}
	urlOrigin, err := canonicalHelperUpdateOrigin(u.Scheme + "://" + u.Host)
	if err != nil || urlOrigin != origin {
		return fmt.Errorf("helper update catalog url/origin mismatch")
	}
	if strings.TrimSpace(v.Platform) != "linux" {
		return fmt.Errorf("helper update platform must be linux")
	}
	return nil
}

// canonicalHelperUpdateOrigin limits an artifact to the one operations-
// registered file-service origin. Paths, credentials, queries and fragments
// are never part of the trusted origin.
func canonicalHelperUpdateOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("helper update file_service_origin invalid")
	}
	loopback := strings.EqualFold(u.Hostname(), "localhost")
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", fmt.Errorf("helper update file_service_origin requires https")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func marshalHelperUpdate(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *Hub) writeHelperUpdateAudit(ctx context.Context, actor, action string, job *helperUpdateJob, reason string) {
	if h.Repo == nil {
		return
	}
	detail := map[string]any{"reason": reason}
	if job != nil {
		detail["job_id"] = job.JobID
		detail["agent_id"] = job.AgentID
		detail["release_id"] = job.ReleaseID
		detail["revision"] = job.Revision
	}
	payload, err := json.Marshal(detail)
	if err == nil {
		_ = h.Repo.WriteAudit(ctx, actor, action, string(payload))
	}
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

// ListHelperUpdateCatalogs exposes only valid, operator-registered releases;
// callers never receive a route for creating arbitrary catalog URLs.
func (h *Hub) ListHelperUpdateCatalogs(ctx context.Context) ([]HelperUpdateCatalog, error) {
	if h.Repo == nil {
		return nil, fmt.Errorf("helper update repository unavailable")
	}
	rows, err := h.Repo.ListAppConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("list helper update catalogs: %w", err)
	}
	items := make([]HelperUpdateCatalog, 0)
	for _, row := range rows {
		if !strings.HasPrefix(row.Key, HelperUpdateCatalogPrefix) {
			continue
		}
		var catalog HelperUpdateCatalog
		if err := json.Unmarshal([]byte(row.ValueJSON), &catalog); err != nil {
			continue
		}
		if validateHelperUpdateCatalog(catalog) == nil {
			items = append(items, catalog)
		}
	}
	return items, nil
}

// ListHelperUpdateCatalog is the frozen, read-only Console catalog API.
func (h *Hub) ListHelperUpdateCatalog(ctx context.Context) ([]HelperUpdateCatalog, error) {
	return h.ListHelperUpdateCatalogs(ctx)
}

// ListHelperUpdateJobsForOwner is a server-side ownership filter for Console
// status pages. A caller still authenticates the actor before invoking it.
func (h *Hub) ListHelperUpdateJobsForOwner(ctx context.Context, ownerID string) ([]HelperUpdateJob, error) {
	if h.Repo == nil {
		return nil, fmt.Errorf("helper update repository unavailable")
	}
	rows, err := h.Repo.ListAppConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("list helper update jobs: %w", err)
	}
	items := make([]HelperUpdateJob, 0)
	for _, row := range rows {
		if !strings.HasPrefix(row.Key, helperUpdateJobPrefix) {
			continue
		}
		var job helperUpdateJob
		if err := json.Unmarshal([]byte(row.ValueJSON), &job); err == nil && job.OwnerID == strings.TrimSpace(ownerID) {
			items = append(items, job)
		}
	}
	return items, nil
}

// GetHelperUpdateJob requires the authenticated principal, then rechecks the
// stored owner before returning a target-specific projection.
func (h *Hub) GetHelperUpdateJob(ctx context.Context, principalID, agentID string) (*HelperUpdateJob, error) {
	if h.Identity == nil {
		return nil, fmt.Errorf("helper update identity unavailable")
	}
	principal, err := h.Identity.Store.GetUser(ctx, strings.TrimSpace(principalID))
	if err != nil || principal == nil || principal.State != identity.UserActive {
		return nil, fmt.Errorf("helper update principal inactive")
	}
	job, _, err := h.helperUpdateJob(ctx, agentID)
	if err != nil || job == nil {
		return job, err
	}
	if job.OwnerID != principal.UserIDLC() {
		return nil, fmt.Errorf("helper update cross-owner denied")
	}
	return job, nil
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
	detail, _ := json.Marshal(map[string]string{"sha256": catalog.SHA256, "version": catalog.Version})
	return h.Repo.WriteAudit(ctx, actor, "helper_update_catalog_registered", string(detail))
}

func (h *Hub) createHelperUpdateJob(ctx context.Context, catalog HelperUpdateCatalog, agentID, ownerID, agentName, issuer string) (*helperUpdateJob, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(agentName) == "" {
		return nil, fmt.Errorf("helper update agent identity required")
	}
	now := h.now().UTC()
	job := helperUpdateJob{
		JobID: randomHex(16), ReleaseID: catalog.SHA256, AgentID: agentID, OwnerID: ownerID, AgentName: agentName,
		Version: catalog.Version, SHA256: catalog.SHA256, LockSHA256: catalog.LockSHA256, URL: catalog.URL, FileServiceOrigin: catalog.FileServiceOrigin,
		Platform: catalog.Platform, Issuer: issuer, Stage: "offered", ExpiresAt: now.Add(30 * time.Minute), Revision: 1,
	}
	job.OfferID = job.JobID
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
	h.writeHelperUpdateAudit(ctx, issuer, "helper_update_offered", &job, "")
	return &job, nil
}

// IssueHelperUpdateForActor creates one durable, target-bound offer after
// enforcing the Console contract: an active owner or manager may only target
// an active Agent belonging to that same owner. The Hub repeats these checks
// rather than relying on a stale browser list or client supplied owner field.
func (h *Hub) IssueHelperUpdate(ctx context.Context, principalID, agentID, releaseSHA string) (*helperUpdateJob, error) {
	if h.Identity == nil {
		return nil, fmt.Errorf("helper update identity unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	actor, err := h.Identity.Store.GetUser(ctx, principalID)
	if err != nil || actor == nil || actor.State != identity.UserActive {
		return nil, fmt.Errorf("helper update actor inactive")
	}
	ag, err := h.Identity.Store.GetAgent(ctx, strings.TrimSpace(agentID))
	if err != nil || ag == nil || ag.State != identity.AgentActive {
		return nil, fmt.Errorf("helper update agent inactive")
	}
	if ag.OwnerLC != actor.UserIDLC() {
		return nil, fmt.Errorf("helper update cross-owner denied")
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
	keyID := AgentKeyID(ag.OwnerLC)
	h.mu.Lock()
	c := h.sessionClients[keyID][ag.NameLC()]
	h.mu.Unlock()
	// A connected legacy helper cannot safely consume this offer. It must first
	// be bootstrapped manually; do not leave an offer for it to replay later.
	if c != nil && !h.sessionSupportsHelperUpdate(c) {
		return nil, fmt.Errorf("helper_update_manual_required")
	}
	job, err := h.createHelperUpdateJob(ctx, *catalog, ag.ID, ag.OwnerLC, ag.NameLC(), actor.UserIDLC())
	if err != nil {
		return nil, err
	}
	if c == nil {
		return job, nil // durable offered state is recovered on the next session.
	}
	job, err = h.recordHelperUpdateOfferConnection(ctx, c, job)
	if err != nil {
		return job, err
	}
	if err := h.sendHelperUpdateOffer(ctx, c, job); err != nil {
		return job, err
	}
	return job, nil
}

// recordHelperUpdateOfferConnection freezes the connection which received the
// first offer. A reconnect may receive a replay, but must not overwrite this
// boundary: confirmation must prove a later authenticated connection.
func (h *Hub) recordHelperUpdateOfferConnection(ctx context.Context, c *sessionClient, job *helperUpdateJob) (*helperUpdateJob, error) {
	if c == nil || job == nil || job.OfferConnSeq > 0 {
		return job, nil
	}
	stored, raw, err := h.helperUpdateJob(ctx, job.AgentID)
	if err != nil || stored == nil {
		return nil, fmt.Errorf("helper update offer job unavailable: %w", err)
	}
	if stored.JobID != job.JobID || helperUpdateTerminal(stored.Stage) {
		return stored, fmt.Errorf("helper update offer no longer active")
	}
	updated := *stored
	updated.OfferConnSeq = c.connSeq
	ok, err := h.updateHelperUpdateJob(ctx, raw, updated)
	if err != nil {
		return stored, err
	}
	if !ok {
		return stored, fmt.Errorf("helper update offer changed concurrently")
	}
	return &updated, nil
}

// IssueHelperUpdateForActor is retained while callers move to the frozen
// principal-bearing IssueHelperUpdate name.
func (h *Hub) IssueHelperUpdateForActor(ctx context.Context, actorID, agentID, releaseSHA string) (*helperUpdateJob, error) {
	return h.IssueHelperUpdate(ctx, actorID, agentID, releaseSHA)
}

func (h *Hub) sendHelperUpdateOffer(ctx context.Context, c *sessionClient, job *helperUpdateJob) error {
	meta := map[string]any{
		"type":                "provision",
		"system":              true,
		"no_mirror":           true,
		"action":              "update_self",
		"target":              "self",
		"job_id":              job.JobID,
		"offer_id":            job.OfferID,
		"release_id":          job.ReleaseID,
		"agent_id":            job.AgentID,
		"owner_id":            job.OwnerID,
		"agent_name":          job.AgentName,
		"version":             job.Version,
		"sha256":              job.SHA256,
		"lock_sha256":         job.LockSHA256,
		"url":                 job.URL,
		"file_service_origin": job.FileServiceOrigin,
		"platform":            job.Platform,
		"issuer":              job.Issuer,
		"expires_at":          job.ExpiresAt.UTC().Format(time.RFC3339),
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
	if c == nil || c.metricsAgentID == "" || !h.sessionSupportsHelperUpdate(c) {
		return true // This control is credential-agent-only; never fall through to routing.
	}
	job, raw, err := h.helperUpdateJob(ctx, c.metricsAgentID)
	if err != nil || job == nil {
		return true
	}
	// All fields are echoed by the helper, but all identity-sensitive values are
	// compared with the durable job and authenticated connection, never trusted
	// because they appeared in the incoming envelope.
	if job.Stage == "failed" || job.Stage == "rolled_back" ||
		(job.Stage != "confirmed" && h.now().UTC().After(job.ExpiresAt)) ||
		(job.Stage != "confirmed" && job.Stage != "installed" && job.Stage != "restarted") ||
		job.ActivationConnSeq <= 0 || c.connSeq <= job.ActivationConnSeq ||
		job.AgentID != c.metricsAgentID || job.OwnerID != strings.TrimPrefix(c.keyID, "u_") ||
		job.AgentName != c.sessionName || c.version != job.Version ||
		metaString(env.Meta, "job_id") != job.JobID || metaString(env.Meta, "release_id") != job.ReleaseID ||
		metaString(env.Meta, "agent_id") != job.AgentID || metaString(env.Meta, "owner_id") != job.OwnerID ||
		metaString(env.Meta, "version") != job.Version ||
		metaString(env.Meta, "sha256") != job.SHA256 || metaString(env.Meta, "lock_sha256") != job.LockSHA256 ||
		!validHelperUpdateHealthNonce(metaString(env.Meta, "health_nonce")) {
		return true
	}
	if job.Stage == "confirmed" {
		// The helper can miss a confirmation during a reconnect. Re-echo a
		// valid current nonce for the same canonical job without changing any
		// durable state, audit record, revision, or activation.
		h.sendHelperUpdateConfirm(ctx, c, job, metaString(env.Meta, "health_nonce"))
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
	h.writeHelperUpdateAudit(ctx, c.keyID, "helper_update_confirmed", &updated, "")
	h.sendHelperUpdateConfirm(ctx, c, &updated, metaString(env.Meta, "health_nonce"))
	return true
}

func (h *Hub) sendHelperUpdateConfirm(ctx context.Context, c *sessionClient, job *helperUpdateJob, nonce string) {
	if c == nil || job == nil {
		return
	}
	_ = c.write(ctx, model.Envelope{
		ID: randomHex(16), To: sessionAddress(c.sessionName, c.keyID), From: sessionAddress("workpulse", c.keyID), TS: time.Now().Unix(),
		Meta: map[string]any{"type": "helper_update_confirm", "system": true, "no_mirror": true,
			"job_id": job.JobID, "release_id": job.ReleaseID, "agent_id": job.AgentID, "owner_id": job.OwnerID,
			"version": job.Version, "sha256": job.SHA256, "lock_sha256": job.LockSHA256, "health_nonce": nonce},
	})
}

// The helper creates a fresh UUID-hex nonce after its post-restart health
// checks. It is an echo-binding value for this confirmation, not offer state.
func validHelperUpdateHealthNonce(v string) bool {
	if len(v) != 32 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func helperUpdateReceiptStage(stage string) bool {
	switch stage {
	case "downloaded", "installed", "restarted", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

func helperUpdateStageRank(stage string) int {
	switch stage {
	case "offered":
		return 0
	case "downloaded":
		return 1
	case "installed":
		return 2
	case "restarted":
		return 3
	default:
		return -1
	}
}

func helperUpdateAuditReason(v string) string {
	switch v {
	case "", "sha_mismatch", "lock_changed", "download_failed", "install_failed", "health_timeout", "rollback", "busy", "offer_expired":
		return v
	default:
		return "unknown"
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
	if stage != "failed" && stage != "rolled_back" && helperUpdateStageRank(stage) <= helperUpdateStageRank(job.Stage) {
		return true // duplicate or stale receipt: no state regression and no audit effect.
	}
	updated := *job
	updated.Stage = stage
	if stage == "installed" || stage == "restarted" {
		updated.ActivationConnSeq = c.connSeq
	}
	if ok, err := h.updateHelperUpdateJob(ctx, raw, updated); err == nil && ok {
		h.writeHelperUpdateAudit(ctx, c.keyID, "helper_update_"+stage, &updated, helperUpdateAuditReason(metaString(env.Meta, "reason")))
	}
	return true
}

func helperUpdateTerminal(stage string) bool {
	switch stage {
	case "confirmed", "failed", "rolled_back":
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
		reason := ""
		if h.now().UTC().After(job.ExpiresAt) {
			next, reason = "failed", "offer_expired"
		} else if ag, err := h.Identity.Store.GetAgent(ctx, job.AgentID); err != nil {
			continue
		} else if ag == nil || ag.State != identity.AgentActive {
			next, reason = "failed", "agent_inactive"
		} else if ag.OwnerLC != job.OwnerID {
			next, reason = "failed", "owner_changed"
		} else if owner, err := h.Identity.Store.GetUser(ctx, job.OwnerID); err != nil {
			continue
		} else if owner == nil || owner.State != identity.UserActive {
			next, reason = "failed", "owner_inactive"
		} else if catalog, err := h.HelperUpdateCatalog(ctx, job.SHA256); err == nil && catalog == nil {
			next, reason = "failed", "release_unavailable"
		} else if err != nil {
			// A transient config-store read must not terminalize a valid durable
			// offer. The next startup/reconnect retries the same job.
			continue
		}
		if next == "" {
			continue
		}
		job.Stage = next
		job.Reason = reason
		if ok, err := h.updateHelperUpdateJob(ctx, row.ValueJSON, job); err == nil && ok {
			h.writeHelperUpdateAudit(ctx, "workpulse", "helper_update_"+next, &job, reason)
		}
	}
	return nil
}

func (h *Hub) resumeHelperUpdateOffer(ctx context.Context, c *sessionClient) {
	if c == nil || c.metricsAgentID == "" || !h.sessionSupportsHelperUpdate(c) {
		// An offline-issued job stays durable, but it must never be replayed to a
		// legacy connection merely because registration completed. A later
		// authenticated capability frame invokes this function again.
		return
	}
	job, raw, err := h.helperUpdateJob(ctx, c.metricsAgentID)
	if err != nil || job == nil || job.Stage != "offered" {
		return
	}
	if h.now().UTC().After(job.ExpiresAt) {
		job.Stage, job.Reason = "failed", "offer_expired"
		if ok, err := h.updateHelperUpdateJob(ctx, raw, *job); err == nil && ok {
			h.writeHelperUpdateAudit(ctx, "workpulse", "helper_update_failed", job, "offer_expired")
		}
		return
	}
	// The persisted first-offer connection is deliberately retained: this is a
	// replay for recovery, not evidence that a candidate restarted on c.
	if updated, err := h.recordHelperUpdateOfferConnection(ctx, c, job); err == nil {
		job = updated
	}
	_ = h.sendHelperUpdateOffer(ctx, c, job)
}
