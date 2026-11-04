package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type r9PMStore interface {
	GetR9PMResponsibility(context.Context, store.R9PMKey) (*store.R9PMResponsibility, error)
	SetR9PMResponsibilityAndReissue(context.Context, store.R9PMKey, string, string, string, string) (int64, error)
	DeleteR9PMResponsibility(context.Context, store.R9PMKey, string) (int64, error)
}

func R9PMDecisionRequestEnabled() bool { return os.Getenv("DW_PM_DECISION_REQUEST_ENABLED") == "1" }

type r9PMMappingInput struct {
	Provider        string `json:"provider"`
	Instance        string `json:"instance"`
	RepositoryID    string `json:"repository_id"`
	ChangeDirectory string `json:"change_directory"`
	PointID         string `json:"point_id"`
	PMUserIDLC      string `json:"pm_user_id_lc"`
}

func (in r9PMMappingInput) key() (store.R9PMKey, bool) {
	vals := []string{in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID}
	for _, v := range vals {
		if strings.TrimSpace(v) == "" || strings.TrimSpace(v) != v || len(v) > 512 {
			return store.R9PMKey{}, false
		}
	}
	return store.R9PMKey{Provider: in.Provider, Instance: in.Instance, RepositoryID: in.RepositoryID, ChangeDirectory: in.ChangeDirectory, PointID: in.PointID}, true
}
func (h *Hub) HandleR9PMResponsibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !R9PMDecisionRequestEnabled() {
		http.Error(w, "R9 PM decision request disabled", http.StatusNotFound)
		return
	}
	repo, ok := h.Repo.(r9PMStore)
	if !ok {
		http.Error(w, "R9 PM store unavailable", http.StatusServiceUnavailable)
		return
	}
	actor := platformPrincipal(r)
	if actor == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		in := r9PMMappingInput{Provider: q.Get("provider"), Instance: q.Get("instance"), RepositoryID: q.Get("repository_id"), ChangeDirectory: q.Get("change_directory"), PointID: q.Get("point_id")}
		k, valid := in.key()
		if !valid {
			http.Error(w, "invalid key", http.StatusBadRequest)
			return
		}
		v, err := repo.GetR9PMResponsibility(r.Context(), k)
		if errors.Is(err, store.ErrR9PMNotFound) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"configured": false, "status": "未配置 PM"})
			return
		}
		if err != nil {
			http.Error(w, "R9 mapping unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	origin, e := url.Parse(r.Header.Get("Origin"))
	if e != nil || origin.Host != r.Host || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "https" && origin.Scheme != "http") || (h.PublicOrigin != "" && r.Header.Get("Origin") != h.PublicOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in r9PMMappingInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	k, valid := in.key()
	if !valid {
		http.Error(w, "invalid key", http.StatusBadRequest)
		return
	}
	var gen int64
	var err error
	if r.Method == http.MethodPut {
		in.PMUserIDLC = identity.Lower(in.PMUserIDLC)
		if in.PMUserIDLC == "" || h.Identity == nil || h.Identity.Store == nil {
			http.Error(w, "active PM user required", http.StatusBadRequest)
			return
		}
		u, e := h.Identity.Store.GetUser(r.Context(), in.PMUserIDLC)
		if e != nil || u == nil || u.State != identity.UserActive {
			http.Error(w, "active PM user required", http.StatusBadRequest)
			return
		}
		issuer, subject := "", ""
		if ident, e := h.Identity.Store.GetIdentity(r.Context(), in.PMUserIDLC, "feishu"); e == nil && ident != nil {
			issuer, subject = ident.Issuer, ident.Subject
		}
		gen, err = repo.SetR9PMResponsibilityAndReissue(r.Context(), k, in.PMUserIDLC, actor, issuer, subject)
	} else {
		gen, err = repo.DeleteR9PMResponsibility(r.Context(), k, actor)
	}
	if err != nil {
		http.Error(w, "R9 mapping update failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"mapping_generation": gen})
}

type r9PMConsoleResponseStore interface {
	AcceptR9PMResponse(context.Context, store.R9PMResponseInput) (bool, error)
	GetR9PMRequestAuthority(context.Context, string) (*store.R9PMReplyTarget, error)
}
type r9PMConsoleResponseInput struct {
	Generation int64  `json:"generation"`
	Decision   string `json:"decision"`
	Comment    string `json:"comment"`
}

func (h *Hub) HandleR9PMConsoleResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !R9PMDecisionRequestEnabled() {
		http.Error(w, "R9 PM decision request disabled", http.StatusNotFound)
		return
	}
	repo, ok := h.Repo.(r9PMConsoleResponseStore)
	if !ok {
		http.Error(w, "R9 PM store unavailable", http.StatusServiceUnavailable)
		return
	}
	actor := platformPrincipal(r)
	if actor == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	origin, e := url.Parse(r.Header.Get("Origin"))
	if e != nil || origin.Host != r.Host || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "https" && origin.Scheme != "http") || (h.PublicOrigin != "" && r.Header.Get("Origin") != h.PublicOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in r9PMConsoleResponseInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF || in.Generation < 1 || len(in.Comment) > 4096 {
		http.Error(w, "invalid response", http.StatusBadRequest)
		return
	}
	decision := map[string]string{"同意": "agree", "不同意": "disagree", "需修改": "revise"}[in.Decision]
	if decision == "" {
		http.Error(w, "decision must be 同意、不同意或需修改", http.StatusBadRequest)
		return
	}
	if h.Identity == nil || h.Identity.Store == nil {
		http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		return
	}
	u, e := h.Identity.Store.GetUser(r.Context(), actor)
	if e != nil || u == nil || u.State != identity.UserActive {
		http.Error(w, "active PM required", http.StatusForbidden)
		return
	}
	requestID := r.PathValue("requestID")
	if requestID == "" {
		http.Error(w, "request required", http.StatusBadRequest)
		return
	}
	authority, e := repo.GetR9PMRequestAuthority(r.Context(), requestID)
	if e != nil || authority.PMUserIDLC != actor {
		http.Error(w, "request authority rejected", http.StatusForbidden)
		return
	}
	if h.SpecReader == nil {
		http.Error(w, "current repository read unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, e = h.SpecReader.Read(r.Context(), actor, specread.Coordinate{Provider: authority.Provider, Instance: authority.Instance, Repository: authority.RepositoryID, Commit: authority.CommitSHA, Path: "openspec/changes/" + authority.ChangeDirectory + "/proposal.md"}, ""); e != nil {
		http.Error(w, "current repository read unavailable", http.StatusForbidden)
		return
	}
	sum := sha256.Sum256([]byte(requestID + "\x00" + actor + "\x00" + strconv.FormatInt(in.Generation, 10) + "\x00" + decision + "\x00" + in.Comment))
	id := hex.EncodeToString(sum[:])
	accepted, e := repo.AcceptR9PMResponse(r.Context(), store.R9PMResponseInput{ResponseID: id, RequestID: requestID, Generation: in.Generation, PrincipalUserIDLC: actor, Channel: "console", Decision: decision, Comment: in.Comment, OperationID: id})
	if e != nil {
		http.Error(w, "response rejected", http.StatusConflict)
		return
	}
	if !accepted {
		http.Error(w, "response already recorded or stale", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type r9PMConsoleTaskStore interface {
	ListR9PMConsoleTasks(context.Context, string) ([]store.R9PMConsoleTask, error)
}

func (h *Hub) HandleR9PMConsoleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !R9PMDecisionRequestEnabled() {
		http.Error(w, "R9 PM decision request disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	actor := platformPrincipal(r)
	if actor == "" || h.Identity == nil || h.Identity.Store == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	u, err := h.Identity.Store.GetUser(r.Context(), actor)
	if err != nil || u == nil || u.State != identity.UserActive {
		http.Error(w, "active user required", http.StatusForbidden)
		return
	}
	repo, ok := h.Repo.(r9PMConsoleTaskStore)
	if !ok {
		http.Error(w, "R9 PM store unavailable", http.StatusServiceUnavailable)
		return
	}
	tasks, err := repo.ListR9PMConsoleTasks(r.Context(), actor)
	if err != nil {
		http.Error(w, "R9 PM tasks unavailable", http.StatusServiceUnavailable)
		return
	}
	if h.SpecReader == nil {
		tasks = []store.R9PMConsoleTask{}
	} else {
		visible := make([]store.R9PMConsoleTask, 0, len(tasks))
		for _, task := range tasks {
			_, readErr := h.SpecReader.Read(r.Context(), actor, specread.Coordinate{Provider: task.Provider, Instance: task.Instance, Repository: task.RepositoryID, Commit: task.CommitSHA, Path: "openspec/changes/" + task.ChangeDirectory + "/proposal.md"}, "")
			if readErr == nil {
				visible = append(visible, task)
			}
		}
		tasks = visible
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}
