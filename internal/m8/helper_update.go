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

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const (
	helperUpdateCatalogPrefix = "helper_update.catalog."
	helperUpdateJobPrefix     = "helper_update.job."
	helperUpdateWireVersion   = "helper-update/v1"
)

type helperUpdateCatalog struct {
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
	JobID      string    `json:"job_id"`
	ReleaseID  string    `json:"release_id"`
	AgentID    string    `json:"agent_id"`
	OwnerID    string    `json:"owner_id"`
	AgentName  string    `json:"agent_name"`
	Version    string    `json:"version"`
	SHA256     string    `json:"sha256"`
	LockSHA256 string    `json:"lock_sha256"`
	URL        string    `json:"url"`
	Platform   string    `json:"platform"`
	Stage      string    `json:"stage"`
	ExpiresAt  time.Time `json:"expires_at"`
	Revision   int       `json:"revision"`
}

func helperUpdateCatalogKey(sha string) string {
	return helperUpdateCatalogPrefix + strings.ToLower(strings.TrimSpace(sha))
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

func validateHelperUpdateCatalog(v helperUpdateCatalog) error {
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

func (h *Hub) helperUpdateCatalog(ctx context.Context, sha string) (*helperUpdateCatalog, error) {
	if h.Repo == nil {
		return nil, fmt.Errorf("helper update repository unavailable")
	}
	key := helperUpdateCatalogKey(sha)
	for _, row := range mustListAppConfig(ctx, h.Repo) {
		if row.Key != key {
			continue
		}
		var catalog helperUpdateCatalog
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

// mustListAppConfig is named after the expected repository capability, not
// panic behavior: it returns nil on an unavailable store so callers can turn
// the condition into a stable, non-leaking control-plane error.
func mustListAppConfig(ctx context.Context, repo store.Repository) []model.AppConfig {
	rows, err := repo.ListAppConfig(ctx)
	if err != nil {
		return nil
	}
	return rows
}

func (h *Hub) helperUpdateJob(ctx context.Context, agentID string) (*helperUpdateJob, string, error) {
	if h.Repo == nil {
		return nil, "", fmt.Errorf("helper update repository unavailable")
	}
	key := helperUpdateJobKey(agentID)
	for _, row := range mustListAppConfig(ctx, h.Repo) {
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
func (h *Hub) RegisterHelperUpdateCatalog(ctx context.Context, actor string, catalog helperUpdateCatalog) error {
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
	created, err := h.Repo.CompareAndSwapAppConfig(ctx, helperUpdateCatalogKey(catalog.SHA256), false, "", payload)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("helper update catalog already registered")
	}
	return h.Repo.WriteAudit(ctx, actor, "helper_update_catalog_registered", helperUpdateCatalogKey(catalog.SHA256))
}

func (h *Hub) createHelperUpdateJob(ctx context.Context, catalog helperUpdateCatalog, agentID, ownerID, agentName, issuer string) (*helperUpdateJob, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(agentName) == "" {
		return nil, fmt.Errorf("helper update agent identity required")
	}
	now := h.now().UTC()
	job := helperUpdateJob{
		JobID: randomHex(16), ReleaseID: catalog.SHA256, AgentID: agentID, OwnerID: ownerID, AgentName: agentName,
		Version: catalog.Version, SHA256: catalog.SHA256, LockSHA256: catalog.LockSHA256, URL: catalog.URL,
		Platform: catalog.Platform, Stage: "offered", ExpiresAt: now.Add(30 * time.Minute), Revision: 1,
	}
	payload, err := marshalHelperUpdate(job)
	if err != nil {
		return nil, err
	}
	created, err := h.Repo.CompareAndSwapAppConfig(ctx, helperUpdateJobKey(agentID), false, "", payload)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, fmt.Errorf("helper update job already exists")
	}
	if err := h.Repo.WriteAudit(ctx, issuer, "helper_update_offered", job.JobID); err != nil {
		return nil, err
	}
	return &job, nil
}
