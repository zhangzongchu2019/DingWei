package m8

// Durable helper-update catalog and job primitives.  The Console surface and
// legacy public provision retirement live in the separately assigned branch;
// this file deliberately contains only Hub-owned state and wire validation.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
