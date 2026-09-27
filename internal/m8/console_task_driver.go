package m8

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

var taskStartSecret = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/-]{8,}|(?:token|capability|secret|api[_-]?key)\s*[:=]\s*[^\s,;]+)`)

type consoleTaskStartRequest struct {
	RepositoryName    string `json:"repository_name"`
	BranchName        string `json:"branch_name"`
	Schedule          string `json:"schedule"`
	TaskEndpoint      string `json:"task_endpoint"`
	TokenInstructions string `json:"token_instructions"`
}

func taskStartContainsSecret(req consoleTaskStartRequest) bool {
	return taskStartSecret.MatchString(strings.Join([]string{req.RepositoryName, req.BranchName, req.Schedule, req.TaskEndpoint, req.TokenInstructions}, "\n"))
}

func (h *Hub) HandleConsoleTaskDriverStart(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "task driver unavailable", http.StatusServiceUnavailable)
		return
	}
	var req consoleTaskStartRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || strings.TrimSpace(req.RepositoryName) == "" || strings.TrimSpace(req.BranchName) == "" || strings.TrimSpace(req.Schedule) == "" || strings.TrimSpace(req.TaskEndpoint) == "" || strings.TrimSpace(req.TokenInstructions) == "" {
		http.Error(w, store.ErrConsoleTaskStartIncomplete.Error(), http.StatusBadRequest)
		return
	}
	if taskStartContainsSecret(req) {
		http.Error(w, "开工信息不得包含令牌明文", http.StatusBadRequest)
		return
	}
	principal := platformPrincipal(r)
	specID := r.PathValue("specID")
	spec, err := repo.ConsoleSpec(r.Context(), principal, specID, false)
	if err != nil {
		http.Error(w, "spec unavailable", http.StatusNotFound)
		return
	}
	rev, err := repo.ConsoleSpecRevision(r.Context(), principal, specID, spec.CurrentRevision, false)
	if err != nil {
		http.Error(w, "spec revision unavailable", http.StatusNotFound)
		return
	}
	start := model.ConsoleTaskDriverStart{SpecID: specID, Revision: spec.CurrentRevision, ContentSHA256: rev.ContentSHA256, RepositoryName: strings.TrimSpace(req.RepositoryName), BranchName: strings.TrimSpace(req.BranchName), Schedule: strings.TrimSpace(req.Schedule), TaskEndpoint: strings.TrimSpace(req.TaskEndpoint), TokenInstructions: strings.TrimSpace(req.TokenInstructions), AnnouncedBy: principal, AnnouncedAt: h.now()}
	if err := repo.StartConsoleTaskDriver(r.Context(), start); err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleTaskStartIncomplete) {
			code = http.StatusBadRequest
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(start)
}

func (h *Hub) HandleAgentConsoleTasks(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "取不到任务清单", http.StatusServiceUnavailable)
		return
	}
	actor := authenticatedConsoleSpecActor(r)
	if actor.Kind != "agent" {
		http.Error(w, "agent capability required", http.StatusForbidden)
		return
	}
	tasks, err := repo.ListConsoleTasksForAgent(r.Context(), actor.ID)
	if err != nil {
		http.Error(w, "取不到任务清单", http.StatusServiceUnavailable)
		return
	}
	specID := r.PathValue("specID")
	revision, valid := positivePage(r.PathValue("revision"))
	if !valid {
		http.Error(w, "invalid revision", http.StatusBadRequest)
		return
	}
	start, err := repo.ConsoleTaskDriverStart(r.Context(), specID, revision)
	if err != nil || start.AnnouncedBy != platformPrincipal(r) {
		http.Error(w, "项目尚未开工，取不到任务清单", http.StatusConflict)
		return
	}
	filtered := tasks[:0]
	for _, task := range tasks {
		if task.SpecID == specID && task.Revision == revision {
			filtered = append(filtered, task)
		}
	}
	status := "active"
	message := ""
	if len(filtered) == 0 {
		status = "paused"
		message = "本席暂停"
		if err := repo.RecordConsoleTaskPause(r.Context(), specID, revision, actor.ID, h.now()); err != nil {
			http.Error(w, "取不到任务清单", http.StatusServiceUnavailable)
			return
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"start": start, "status": status, "message": message, "tasks": filtered})
}

func (h *Hub) HandleConsoleTaskDriverReview(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "task driver unavailable", 503)
		return
	}
	revision, valid := positivePage(r.PathValue("revision"))
	if !valid {
		http.Error(w, "invalid revision", 400)
		return
	}
	if err := repo.ReviewConsoleTaskDriver(r.Context(), r.PathValue("specID"), revision, platformPrincipal(r), h.now()); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleTaskDriverDeploy(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "task driver unavailable", 503)
		return
	}
	revision, valid := positivePage(r.PathValue("revision"))
	if !valid {
		http.Error(w, "invalid revision", 400)
		return
	}
	if err := repo.RequestConsoleTaskDeployment(r.Context(), r.PathValue("specID"), revision, platformPrincipal(r), h.now()); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleTaskDriverStatus(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "task driver unavailable", 503)
		return
	}
	revision, valid := positivePage(r.PathValue("revision"))
	if !valid {
		http.Error(w, "invalid revision", 400)
		return
	}
	start, err := repo.ConsoleTaskDriverStart(r.Context(), r.PathValue("specID"), revision)
	if err != nil || start.AnnouncedBy != platformPrincipal(r) {
		http.NotFound(w, r)
		return
	}
	reports, err := repo.ListConsoleTaskPauseReports(r.Context(), start.SpecID, start.Revision)
	if err != nil {
		http.Error(w, "task driver unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"start": start, "pause_reports": reports})
}
