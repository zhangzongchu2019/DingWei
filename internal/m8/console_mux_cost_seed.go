package m8

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Only a configured target service can obtain this read-only migration cut.
// It exposes identity/cursor metadata, never payload, cookies or credentials.
// Ops must separately stop publishers and drain/stop both cost consumers.
func (h *Hub) HandleConsoleMuxCostSeedAuthority(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil || h.Identity == nil {
		writeAgentWSError(w, 503, "seed_authority_unavailable")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		writeAgentWSError(w, 400, "invalid_seed_request")
		return
	}
	var request struct {
		Target string   `json:"target"`
		Names  []string `json:"full_names"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Names) == 0 || len(request.Names) > 8 {
		writeAgentWSError(w, 400, "invalid_seed_request")
		return
	}
	target, ok := loadViewRouting().Targets[request.Target]
	if !ok || !target.ViewUnlockV2 {
		writeAgentWSError(w, 404, "not_found")
		return
	}
	key, err := os.ReadFile(target.VSToHubSecretFile)
	ts, nonce := r.Header.Get("X-DW-Timestamp"), r.Header.Get("X-DW-Nonce")
	n, e := strconv.ParseInt(ts, 10, 64)
	signature := signViewServiceRequest(bytes.TrimSpace(key), vsToHubDirection, r.Method, r.URL.Path, ts, nonce, body)
	if err != nil || len(bytes.TrimSpace(key)) < 32 || e != nil || time.Since(time.Unix(n, 0)).Abs() > time.Minute || !muxValidID(nonce) || r.Header.Get("X-DW-VS-Target") != request.Target || !hmac.Equal([]byte(signature), []byte(r.Header.Get("X-DW-Signature"))) {
		writeAgentWSError(w, 401, "unauthorized")
		return
	}
	if !h.claimViewV2Nonce(request.Target, nonce, time.Now()) {
		writeAgentWSError(w, 409, "replayed")
		return
	}
	m.mu.Lock()
	feeds := []*consoleMuxFeed{}
	for _, f := range m.feeds {
		feeds = append(feeds, f)
	}
	m.mu.Unlock()
	for _, f := range feeds {
		f.mu.Lock()
		busy := false
		for _, sc := range f.Scopes {
			if sc.TargetName == request.Target && f.Cancel != nil {
				busy = true
			}
		}
		f.mu.Unlock()
		if busy {
			writeAgentWSError(w, 409, "seed_consumers_active")
			return
		}
	}
	entries := []map[string]string{}
	err = m.budget.Do(r.Context(), func(ctx context.Context) error {
		seen := map[string]bool{}
		for _, name := range request.Names {
			canon, a, ok := h.newPlatformCanonicalName(ctx, name)
			if !ok || canon != name || seen[a.ID] {
				return consolemux.ErrDenied
			}
			seen[a.ID] = true
			user, err := h.Identity.Store.GetUser(ctx, a.OwnerLC)
			if err != nil || user.State != identity.UserActive {
				return consolemux.ErrDenied
			}
			routed, _ := loadViewRouting().resolve(canon, a.OwnerLC)
			if routed != request.Target {
				return consolemux.ErrDenied
			}
			snap, ok := h.consoleMuxSnapshot(consolemux.Agent{ID: a.ID, Owner: a.OwnerLC, Session: a.NameLC(), FullName: canon})
			if !ok {
				return consolemux.ErrDenied
			}
			entries = append(entries, map[string]string{"agent_id": a.ID, "owner_lc": a.OwnerLC, "full_name": canon, "epoch": snap.Epoch, "H": muxSeq(snap.H)})
		}
		return nil
	})
	if err != nil {
		writeAgentWSError(w, 503, "seed_authority_unavailable")
		return
	}
	authority := sha256.Sum256(m.secret[:])
	muxJSON(w, 200, map[string]any{"target": request.Target, "authority_id": hex.EncodeToString(authority[:]), "agents": entries})
}
