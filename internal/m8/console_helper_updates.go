package m8

// Console adapters do not write app_config. All offers and lifecycle writes go
// through the Hub core; browser inputs cannot register a URL or alter a job.
import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

type consoleHelperJob struct {
	JobID   string `json:"job_id"`
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Reason  string `json:"reason,omitempty"`
}
type consoleHelperAgent struct {
	AgentID  string            `json:"agent_id"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Online   bool              `json:"online"`
	Eligible bool              `json:"eligible"`
	Reason   string            `json:"reason,omitempty"`
	Job      *consoleHelperJob `json:"job,omitempty"`
}

func helperJobView(j *HelperUpdateJob) *consoleHelperJob {
	if j == nil {
		return nil
	}
	reason := j.Reason
	switch reason {
	case "", "lock_changed", "sha_mismatch", "origin_denied", "expired", "agent_inactive", "owner_inactive", "release_unavailable", "start_failed", "health_timeout", "rolled_back":
	default:
		reason = "failed"
	}
	return &consoleHelperJob{JobID: j.JobID, Version: j.Version, Stage: j.Stage, Reason: reason}
}
func helperConsoleJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func helperConsoleError(w http.ResponseWriter, status int, code string) {
	helperConsoleJSON(w, status, map[string]string{"error": code})
}
func (h *Hub) helperConsoleActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.Onboarding == nil || h.Identity == nil {
		http.NotFound(w, r)
		return "", false
	}
	actor := platformPrincipal(r)
	if actor == "" {
		helperConsoleError(w, 401, "unauthenticated")
		return "", false
	}
	user, err := h.Identity.Store.GetUser(r.Context(), actor)
	if err != nil || user == nil || user.State != identity.UserActive {
		helperConsoleError(w, 403, "forbidden")
		return "", false
	}
	if r.Method != http.MethodGet && (h.PublicOrigin == "" || r.Header.Get("Origin") != h.PublicOrigin) {
		helperConsoleError(w, 403, "origin_mismatch")
		return "", false
	}
	// Every active user is owner of their own Agents. Manager/platform_admin
	// status confers no right to target another user's Agent here.
	return user.UserIDLC(), true
}
func helperUpdaterVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 4 {
		return false
	}
	var nums [4]int
	for i, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 {
			return false
		}
		nums[i] = n
	}
	return nums[0] == 0 && nums[1] == 2 && (nums[2] > 3 || (nums[2] == 3 && nums[3] >= 1))
}
func (h *Hub) helperAgentView(ag identity.Agent) consoleHelperAgent {
	out := consoleHelperAgent{AgentID: ag.ID, Name: ag.Name, Reason: "offline"}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()]
	osName := ""
	if c != nil && c.metricsAgentID == ag.ID {
		out.Online = true
		out.Version = c.version
		osName = c.osName
	}
	h.mu.Unlock()
	switch {
	case ag.OwnerLC == "" || ag.State != identity.AgentActive:
		out.Reason = "inactive"
	case ag.Origin != identity.OriginSelfhost:
		out.Reason = "not_selfhost"
	case !out.Online:
		out.Reason = "offline"
	case !helperUpdaterVersion(out.Version):
		out.Reason = "manual_required"
	case osName != "linux":
		out.Reason = "manual_required"
	default:
		out.Eligible = true
		out.Reason = ""
	}
	return out
}

func (h *Hub) HandleConsoleHelperUpdates(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.helperConsoleActor(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.listConsoleHelperUpdates(w, r, actor)
	case http.MethodPost:
		h.issueConsoleHelperUpdates(w, r, actor)
	default:
		w.Header().Set("Allow", "GET, POST")
		helperConsoleError(w, 405, "method_not_allowed")
	}
}
func (h *Hub) listConsoleHelperUpdates(w http.ResponseWriter, r *http.Request, actor string) {
	catalogs, err := h.ListHelperUpdateCatalogs(r.Context())
	if err != nil {
		helperConsoleError(w, 503, "unavailable")
		return
	}
	jobs, err := h.ListHelperUpdateJobsForOwner(r.Context(), actor)
	if err != nil {
		helperConsoleError(w, 503, "unavailable")
		return
	}
	agents, err := h.Identity.Store.ListAgents(r.Context(), actor)
	if err != nil {
		helperConsoleError(w, 503, "unavailable")
		return
	}
	endpoints, err := h.Repo.ListSessionEndpoints(r.Context())
	if err != nil {
		helperConsoleError(w, 503, "unavailable")
		return
	}
	type release struct {
		SHA256   string `json:"sha256"`
		Version  string `json:"version"`
		Platform string `json:"platform"`
	}
	releases := make([]release, 0, len(catalogs))
	for _, c := range catalogs {
		releases = append(releases, release{c.SHA256, c.Version, c.Platform})
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].SHA256 < releases[j].SHA256 })
	views := make([]consoleHelperAgent, 0, len(agents))
	for _, ag := range agents {
		if ag.OwnerLC != actor {
			continue
		}
		v := h.helperAgentView(ag)
		if !v.Online {
			for _, ep := range endpoints {
				if ep.KeyID == AgentKeyID(actor) && ep.SessionName == ag.NameLC() {
					v.Version = ep.Version
					break
				}
			}
		}
		for i := range jobs {
			j := &jobs[i]
			if j.AgentID == ag.ID && j.OwnerID == actor {
				v.Job = helperJobView(j)
				break
			}
		}
		views = append(views, v)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].AgentID < views[j].AgentID })
	helperConsoleJSON(w, 200, map[string]any{"releases": releases, "agents": views})
}
func (h *Hub) issueConsoleHelperUpdates(w http.ResponseWriter, r *http.Request, actor string) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		helperConsoleError(w, 415, "invalid_request")
		return
	}
	var req struct {
		ReleaseSHA string   `json:"release_sha256"`
		AgentIDs   []string `json:"agent_ids"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || !validHelperUpdateSHA256(req.ReleaseSHA) || len(req.AgentIDs) < 1 || len(req.AgentIDs) > 50 {
		helperConsoleError(w, 400, "invalid_request")
		return
	}
	req.ReleaseSHA = strings.ToLower(req.ReleaseSHA)
	type result struct {
		AgentID string            `json:"agent_id"`
		OK      bool              `json:"ok"`
		Error   string            `json:"error,omitempty"`
		Job     *consoleHelperJob `json:"job,omitempty"`
	}
	results := make([]result, 0, len(req.AgentIDs))
	seen := map[string]bool{}
	for _, id := range req.AgentIDs {
		if id == "" || len(id) > 256 {
			helperConsoleError(w, 400, "invalid_request")
			return
		}
	}
	for _, id := range req.AgentIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		out := result{AgentID: id}
		ag, err := h.Identity.Store.GetAgent(r.Context(), id)
		if err != nil || ag == nil || ag.OwnerLC != actor {
			out.Error = "forbidden"
			results = append(results, out)
			continue
		}
		state := h.helperAgentView(*ag)
		if !state.Eligible {
			out.Error = state.Reason
			results = append(results, out)
			continue
		}
		// Mandatory second authorization in core: never pass a browser owner field.
		job, err := h.IssueHelperUpdateForActor(r.Context(), actor, id, req.ReleaseSHA)
		out.Job = helperJobView(job)
		if err == nil {
			out.OK = true
		} else {
			out.Error = "unavailable"
			if strings.Contains(err.Error(), "job already exists") || strings.Contains(err.Error(), "changed concurrently") {
				out.Error = "job_conflict"
			}
		}
		results = append(results, out)
	}
	helperConsoleJSON(w, 200, map[string]any{"results": results})
}
