package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Agent pre-splitting includes one LLM call; 180 seconds avoids classifying a
// normal minute-scale response as failure. Tests override Hub.SpecPresplitTimeout.
const defaultSpecPresplitTimeout = 180 * time.Second

const (
	specPresplitRequestType = "console_spec_presplit_request"
	specPresplitResultType  = "console_spec_presplit_result"
)

func (h *Hub) effectiveSpecPresplitTimeout() time.Duration {
	if h.SpecPresplitTimeout > 0 {
		return h.SpecPresplitTimeout
	}
	return defaultSpecPresplitTimeout
}

func (h *Hub) HandleConsoleSpecPresplit(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	if r.Method == http.MethodGet {
		state, err := repo.ConsoleSpecPresplit(r.Context(), r.PathValue("specID"), principal, h.now(), h.startedAt)
		if err != nil {
			http.Error(w, "presplit unavailable", http.StatusServiceUnavailable)
			return
		}
		agents, err := repo.ListOwnedConsoleAgents(r.Context(), principal)
		if err != nil {
			http.Error(w, "presplit agents unavailable", http.StatusServiceUnavailable)
			return
		}
		online := make([]model.ConsoleAgent, 0, len(agents))
		for _, agent := range agents {
			if h.consoleAgentOnline(r.Context(), agent.ID, principal) {
				online = append(online, agent)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"request": state, "agents": online})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AgentID string `json:"agent_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || strings.TrimSpace(body.AgentID) == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}
	now := h.now()
	req := model.ConsoleSpecPresplitRequest{ID: "presplit:" + randomHex(16), SpecID: r.PathValue("specID"), AgentID: body.AgentID,
		Status: "pending", CreatedAt: now, DeadlineAt: now.Add(h.effectiveSpecPresplitTimeout())}
	revision, agent, err := repo.StartConsoleSpecPresplit(r.Context(), req, principal)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecPermission) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	if !h.consoleAgentOnline(r.Context(), agent.ID, principal) {
		_ = repo.FailConsoleSpecPresplitDispatch(r.Context(), req.ID, "agent offline", now)
		http.Error(w, "Agent 不在线，预拆失败，可手工添加", http.StatusConflict)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"request_id": req.ID, "spec_id": req.SpecID, "revision": revision.Revision,
		"content_markdown": revision.ContentMarkdown,
		"respond_to":       sessionAddress(systemAddressName, agent.KeyID), "response_type": specPresplitResultType,
		"response_schema": map[string]any{"tasks": []string{"title", "content", "definition_of_done", "estimated_hours", "depends_on_indexes"}},
		"instruction":     "拆成候选任务后，用当前会话向 respond_to 发送系统信封：Meta.type=response_type、Meta.request_id=request_id，Body 使用 response_schema JSON。",
	})
	env := model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, agent.KeyID), To: sessionAddress(agent.SessionName, agent.KeyID), TS: now.Unix(),
		Body: string(payload), Meta: map[string]any{"type": specPresplitRequestType, "request_id": req.ID, "system": true, "no_mirror": true}}
	dispatch := h.specPresplitDispatch
	if dispatch == nil {
		dispatch = func(ctx context.Context, _ model.ConsoleAgent, env model.Envelope) error {
			return h.RouteEnvelope(ctx, env)
		}
	}
	if err = dispatch(r.Context(), *agent, env); err != nil {
		_ = repo.FailConsoleSpecPresplitDispatch(r.Context(), req.ID, "agent did not accept request", now)
		http.Error(w, "预拆失败，可手工添加", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"request_id": req.ID, "status": "pending"})
}

type presplitResultBody struct {
	RequestID   string                 `json:"request_id"`
	Tasks       []presplitResultTask   `json:"tasks"`
	KeyID       string                 `json:"key_id,omitempty"`
	SessionName string                 `json:"session_name,omitempty"`
	Extra       map[string]interface{} `json:"-"`
}

type presplitResultTask struct {
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	DefinitionOfDone string  `json:"definition_of_done"`
	EstimatedHours   float64 `json:"estimated_hours"`
	DependsOnIndexes []int   `json:"depends_on_indexes"`
}

func (h *Hub) handleConsoleSpecPresplitResult(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != specPresplitResultType {
		return false
	}
	repo, ok := h.consoleSpecs()
	if !ok {
		return true
	}
	agent, err := repo.ResolveConsoleAgentByEndpoint(ctx, c.keyID, c.sessionName)
	if err != nil || agent == nil {
		log.Printf("spec presplit callback rejected request_id=%q reason=unknown_agent key_id=%s session=%s", metaString(env.Meta, "request_id"), c.keyID, c.sessionName)
		return true
	}
	var raw map[string]json.RawMessage
	var body presplitResultBody
	if json.Unmarshal([]byte(env.Body), &raw) != nil || json.Unmarshal([]byte(env.Body), &body) != nil {
		h.rejectMalformedPresplit(ctx, repo, *agent, metaString(env.Meta, "request_id"), "invalid json")
		return true
	}
	for key := range raw {
		switch key {
		case "request_id", "tasks", "key_id", "session_name":
		default:
			log.Printf("spec presplit callback ignored unknown field=%q request_id=%q", key, body.RequestID)
		}
	}
	var rawTasks []map[string]json.RawMessage
	if json.Unmarshal(raw["tasks"], &rawTasks) == nil {
		for index, task := range rawTasks {
			for key := range task {
				switch key {
				case "title", "content", "definition_of_done", "estimated_hours", "depends_on_indexes":
				default:
					log.Printf("spec presplit callback ignored unknown task field=%q request_id=%q task_index=%d", key, body.RequestID, index)
				}
			}
		}
	}
	if body.KeyID != "" || body.SessionName != "" {
		log.Printf("spec presplit callback ignored claimed identity request_id=%q key_id=%q session_name=%q", body.RequestID, body.KeyID, body.SessionName)
	}
	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" {
		requestID = metaString(env.Meta, "request_id")
	}
	tasks, valid := validatePresplitTasks(body.Tasks)
	if !valid {
		h.rejectMalformedPresplit(ctx, repo, *agent, requestID, "invalid task format")
		return true
	}
	if err = repo.CompleteConsoleSpecPresplit(ctx, requestID, *agent, tasks, h.now()); err != nil {
		log.Printf("spec presplit callback rejected request_id=%q agent=%s reason=%v", requestID, agent.ID, err)
		return true
	}
	log.Printf("spec presplit callback completed request_id=%q agent=%s tasks=%d", requestID, agent.ID, len(tasks))
	return true
}

func (h *Hub) rejectMalformedPresplit(ctx context.Context, repo consoleSpecRepository, agent model.ConsoleAgent, requestID, reason string) {
	if err := repo.RejectConsoleSpecPresplitResult(ctx, requestID, agent, reason, h.now()); err != nil {
		log.Printf("spec presplit malformed callback rejected request_id=%q agent=%s reason=%v", requestID, agent.ID, err)
	}
}

func validatePresplitTasks(raw []presplitResultTask) ([]store.ConsoleSpecPresplitTask, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	ids := make([]string, len(raw))
	for i := range ids {
		ids[i] = "task:" + randomHex(16)
	}
	graph := make([][]int, len(raw))
	out := make([]store.ConsoleSpecPresplitTask, len(raw))
	for i, task := range raw {
		if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.Content) == "" || task.EstimatedHours < 0 {
			return nil, false
		}
		seen := map[int]bool{}
		out[i].DependsOn = []string{}
		for _, dep := range task.DependsOnIndexes {
			if dep < 0 || dep >= len(raw) || dep == i || seen[dep] {
				return nil, false
			}
			seen[dep] = true
			graph[i] = append(graph[i], dep)
			out[i].DependsOn = append(out[i].DependsOn, ids[dep])
		}
		out[i].ID, out[i].Title, out[i].Content = ids[i], strings.TrimSpace(task.Title), strings.TrimSpace(task.Content)
		out[i].DefinitionOfDone, out[i].EstimatedHours = strings.TrimSpace(task.DefinitionOfDone), task.EstimatedHours
	}
	visiting, done := make([]bool, len(raw)), make([]bool, len(raw))
	var visit func(int) bool
	visit = func(i int) bool {
		if visiting[i] {
			return true
		}
		if done[i] {
			return false
		}
		visiting[i] = true
		for _, dep := range graph[i] {
			if visit(dep) {
				return true
			}
		}
		visiting[i], done[i] = false, true
		return false
	}
	for i := range raw {
		if visit(i) {
			return nil, false
		}
	}
	return out, true
}

func presplitFailureText(req *model.ConsoleSpecPresplitRequest) string {
	if req == nil || req.Status != "failed" {
		return ""
	}
	return fmt.Sprintf("预拆失败，可手工添加（%s）", req.FailureReason)
}
