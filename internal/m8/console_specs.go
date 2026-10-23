package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type consoleSpecRepository interface {
	CreateConsoleSpec(context.Context, model.ConsoleSpec, model.ConsoleSpecRevision, time.Time) error
	ListConsoleSpecs(context.Context, string, bool) ([]model.ConsoleSpec, error)
	ConsoleSpec(context.Context, string, string, bool) (*model.ConsoleSpec, error)
	ConsoleSpecRevision(context.Context, string, string, int, bool) (*model.ConsoleSpecRevision, error)
	AppendConsoleSpecRevision(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	AppendConsoleSpecRevisionByAgent(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	AppendConsoleSpecRevisionWithCapability(context.Context, string, int, string, model.ConsoleSpecRevision, time.Time) (int, error)
	OpenConsoleSpecRevision(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	OpenConsoleSpecRevisionWithCapability(context.Context, string, int, string, model.ConsoleSpecRevision, time.Time) (int, error)
	CloseConsoleSpecEditing(context.Context, string, string, time.Time) error
	ReopenConsoleSpecEditing(context.Context, string, string, time.Time) error
	ArchiveConsoleSpec(context.Context, string, string, time.Time) error
	SetConsoleSpecsArchived(context.Context, []string, string, bool, bool, time.Time) error
	ListReadyConsoleTaskProposals(context.Context) ([]model.ConsoleTaskProposal, error)
	ListConsoleTaskProposals(context.Context, string) ([]model.ConsoleTaskProposal, error)
	ListAllConsoleTaskProposals(context.Context) ([]model.ConsoleTaskProposal, error)
	CreateConsoleTaskProposal(context.Context, model.ConsoleTaskProposal, string, bool) error
	UpdateConsoleTaskProposalPlan(context.Context, string, string, bool, string, string, float64, []string, string) error
	UpdateConsoleSpecTaskAssignees(context.Context, string, string, string, string) error
	UpdateConsoleSpecTaskMetadata(context.Context, string, string, *time.Time, *time.Time, *string, *string) error
	StartConsoleTaskDriver(context.Context, model.ConsoleTaskDriverStart) error
	ListConsoleTasksForAgent(context.Context, string) ([]model.ConsoleTaskProposal, error)
	RecordConsoleTaskPause(context.Context, string, int, string, time.Time) error
	ConsoleTaskPauseReported(context.Context, string, int, string) (bool, error)
	ListConsoleTaskPauseReports(context.Context, string, int) ([]model.ConsoleTaskPauseReport, error)
	ReviewConsoleTaskDriver(context.Context, string, int, string, time.Time) error
	RequestConsoleTaskDeployment(context.Context, string, int, string, time.Time) error
	ConsoleTaskDriverStart(context.Context, string, int) (*model.ConsoleTaskDriverStart, error)
	ConsoleSpecTaskCaseProgress(context.Context, string) (int, int, error)
	DeleteConsoleSpecTask(context.Context, string, string) error
	ListActiveConsolePrincipals(context.Context) ([]model.ConsolePrincipal, error)
	TransferConsoleSpec(context.Context, string, string, string, string, time.Time) error
	SubmitConsoleSpecReview(context.Context, string, string, time.Time) error
	FinalizeConsoleSpec(context.Context, string, string, time.Time) error
	SaveConsoleSpecGateReview(context.Context, string, string, model.ConsoleSpecGateReview, time.Time) error
	ConsoleSpecGateReview(context.Context, string, int) (*model.ConsoleSpecGateReview, error)
	CloseConsoleSpecTaskSplit(context.Context, string, string, time.Time) error
	ListOwnedConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	ListConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	ResolveConsoleAgentByEndpoint(context.Context, string, string) (*model.ConsoleAgent, error)
	StartConsoleSpecPresplit(context.Context, model.ConsoleSpecPresplitRequest, string) (*model.ConsoleSpecRevision, *model.ConsoleAgent, error)
	ConsoleSpecPresplit(context.Context, string, string, time.Time, time.Time) (*model.ConsoleSpecPresplitRequest, error)
	FailConsoleSpecPresplitDispatch(context.Context, string, string, time.Time) error
	RejectConsoleSpecPresplitResult(context.Context, string, model.ConsoleAgent, string, time.Time) error
	CompleteConsoleSpecPresplit(context.Context, string, model.ConsoleAgent, []store.ConsoleSpecPresplitTask, time.Time, ...func() error) error
	ConsoleSpecAttribution(context.Context, string) (model.ConsoleSpecAttribution, error)
	DeriveConsoleSpec(context.Context, string, string, model.ConsoleSpec, model.ConsoleSpecRevision, time.Time) error
}

type consoleSpecDirectContextKey struct{}
type consoleSpecOpenNewContextKey struct{}
type consoleSpecActorContextKey struct{}

type consoleSpecActor struct {
	ID   string
	Kind string
}

func orderConsoleTasks(tasks []model.ConsoleTaskProposal, all []model.ConsoleTaskProposal) []model.ConsoleTaskProposal {
	byID := make(map[string]*model.ConsoleTaskProposal, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = &tasks[i]
	}
	deps := make(map[string][]string, len(tasks))
	for i := range tasks {
		var taskDeps []string
		_ = json.Unmarshal([]byte(tasks[i].DependsOnJSON), &taskDeps)
		deps[tasks[i].ID] = taskDeps
	}
	for _, other := range all {
		var otherDeps []string
		_ = json.Unmarshal([]byte(other.DependsOnJSON), &otherDeps)
		for _, dep := range otherDeps {
			if task := byID[dep]; task != nil {
				task.DependedBy = append(task.DependedBy, other.ID)
			}
		}
	}
	memo, visiting := map[string]int{}, map[string]bool{}
	var level func(string) int
	level = func(id string) int {
		if n, ok := memo[id]; ok {
			return n
		}
		if visiting[id] {
			return 0
		}
		visiting[id] = true
		n := 0
		for _, dep := range deps[id] {
			if byID[dep] != nil {
				if candidate := level(dep) + 1; candidate > n {
					n = candidate
				}
			}
		}
		visiting[id] = false
		memo[id] = n
		return n
	}
	counts := map[int]int{}
	for i := range tasks {
		tasks[i].DependencyLevel = level(tasks[i].ID)
		counts[tasks[i].DependencyLevel]++
	}
	for i := range tasks {
		tasks[i].Parallel = counts[tasks[i].DependencyLevel] > 1
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].DependencyLevel != tasks[j].DependencyLevel {
			return tasks[i].DependencyLevel < tasks[j].DependencyLevel
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

func specCapabilityHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var specMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\((https://[^)\s]+)\)`)

func (h *Hub) persistSpecImages(ctx context.Context, markdown, actorPrincipal, ownerPrincipal string) (string, error) {
	matches := specMarkdownImage.FindAllStringSubmatch(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil
	}
	scopeRepo, ok := h.Repo.(consoleOwnerScopeRepository)
	if !ok || h.filePermanent == nil {
		return "", errors.New("spec permanent image service unavailable")
	}
	ownerKey, err := scopeRepo.ResolveConsoleOwnerScope(ctx, ownerPrincipal)
	if err != nil || ownerKey == "" {
		return "", errors.New("spec image owner scope unavailable")
	}
	rewritten := markdown
	for _, match := range matches {
		raw := match[1]
		u, parseErr := url.Parse(raw)
		base := ""
		if parseErr == nil && strings.Contains(u.Path, "/f/") {
			name := u.Path[strings.LastIndex(u.Path, "/")+1:]
			if dot := strings.IndexByte(name, '.'); dot > 0 {
				base = name[:dot]
			}
		}
		if !fileIDPattern.MatchString(base) {
			return "", errors.New("spec image must use an uploaded HTTPS /f/ URL")
		}
		stable, persistErr := h.filePermanent.PersistSpecFile(ctx, FilePermanentRequest{FileID: base, ActorPrincipal: actorPrincipal, OwnerKey: ownerKey})
		if persistErr != nil || !strings.HasPrefix(stable, "https://") {
			return "", errors.New("persist spec image")
		}
		rewritten = strings.ReplaceAll(rewritten, raw, stable)
	}
	return rewritten, nil
}

type specAcceptance struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	OwnerRef    string   `json:"owner_ref"`
	Source      string   `json:"source,omitempty"`
	GeneratedBy string   `json:"generated_by,omitempty"`
	CheckedBy   string   `json:"checked_by,omitempty"`
	CheckedAt   string   `json:"checked_at,omitempty"`
	Cases       []string `json:"cases,omitempty"`
}

type specInference struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	GeneratedBy string `json:"generated_by"`
	Status      string `json:"status"` // pending_owner_confirmation
}

type specAcceptanceBundle struct {
	Criteria   []specAcceptance `json:"criteria"`
	Inferences []specInference  `json:"inferences"`
}

type specGateIssue struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Response string `json:"response"`
	Outcome  string `json:"outcome"` // true_problem | false_positive | pending
}

type specGateReviewRequest struct {
	ReviewAgentID string          `json:"review_agent_id"`
	Issues        []specGateIssue `json:"issues"`
}

type specInputAnchor struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Digest string `json:"digest,omitempty"`
}

type consoleSpecWriteRequest struct {
	Title            string            `json:"title"`
	ContentMarkdown  string            `json:"content_markdown"`
	Acceptance       []specAcceptance  `json:"acceptance"`
	Inferences       []specInference   `json:"inferences,omitempty"`
	InputAnchors     []specInputAnchor `json:"input_anchors"`
	ExpectedRevision int               `json:"expected_revision,omitempty"`
	ClaimedCreatedBy string            `json:"created_by,omitempty"`
}

func authenticatedConsoleSpecActor(r *http.Request) consoleSpecActor {
	if actor, ok := r.Context().Value(consoleSpecActorContextKey{}).(consoleSpecActor); ok && actor.ID != "" {
		return actor
	}
	return consoleSpecActor{ID: platformPrincipal(r), Kind: "principal"}
}

func normalizeConsoleSpecGeneratedBy(req *consoleSpecWriteRequest, actor consoleSpecActor) {
	for i := range req.Acceptance {
		req.Acceptance[i].GeneratedBy = ""
		if actor.Kind == "agent" {
			req.Acceptance[i].GeneratedBy = actor.ID
		}
	}
}

func validateConsoleSpecWrite(req consoleSpecWriteRequest) error {
	if strings.TrimSpace(req.Title) == "" || len(req.Title) > 200 || strings.TrimSpace(req.ContentMarkdown) == "" || len(req.Acceptance) == 0 || len(req.InputAnchors) == 0 {
		return errors.New("title, content, acceptance and input anchors are required")
	}
	seen := map[string]bool{}
	for i, item := range req.Acceptance {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.OwnerRef) == "" || seen[item.ID] {
			return errors.New("acceptance ids, text and owner_ref must be non-empty and unique")
		}
		if !strings.Contains(req.ContentMarkdown, item.OwnerRef) {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				id = fmt.Sprintf("acceptance[%d]", i)
			}
			quote := []rune(strings.TrimSpace(item.OwnerRef))
			if len(quote) > 30 {
				quote = append(quote[:30], '…')
			}
			return fmt.Errorf("acceptance original quote must occur in current spec content: %s「%s」", id, string(quote))
		}
		seen[item.ID] = true
	}
	for _, item := range req.Inferences {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.GeneratedBy) == "" || item.Status != "pending_owner_confirmation" || seen[item.ID] {
			return errors.New("invalid pending Agent inference")
		}
		seen[item.ID] = true
	}
	for _, anchor := range req.InputAnchors {
		if strings.TrimSpace(anchor.Ref) == "" {
			return errors.New("input anchor ref is required")
		}
		switch anchor.Kind {
		case "requirement_pool_item", "owner_message":
		case "upstream_spec":
			if strings.TrimSpace(anchor.Digest) == "" {
				return errors.New("upstream spec anchor requires digest")
			}
		default:
			return errors.New("invalid input anchor kind")
		}
	}
	return nil
}

func decodeConsoleSpecWrite(w http.ResponseWriter, r *http.Request) (consoleSpecWriteRequest, bool) {
	var req consoleSpecWriteRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid spec JSON: "+err.Error(), http.StatusBadRequest)
		return consoleSpecWriteRequest{}, false
	}
	normalizeConsoleSpecGeneratedBy(&req, authenticatedConsoleSpecActor(r))
	if err := validateConsoleSpecWrite(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return consoleSpecWriteRequest{}, false
	}
	return req, true
}

func revisionFromRequest(specID string, actor consoleSpecActor, revision int, req consoleSpecWriteRequest, now time.Time) (model.ConsoleSpecRevision, error) {
	for i := range req.Acceptance {
		req.Acceptance[i].ID = fmt.Sprintf("A%d", i+1)
		req.Acceptance[i].Source = "owner"
		if actor.Kind == "agent" {
			req.Acceptance[i].Source = "ai"
		}
		req.Acceptance[i].CheckedAt = now.UTC().Format(time.RFC3339)
	}
	acceptance, err := json.Marshal(specAcceptanceBundle{Criteria: req.Acceptance, Inferences: req.Inferences})
	if err != nil {
		return model.ConsoleSpecRevision{}, err
	}
	anchors, err := json.Marshal(req.InputAnchors)
	if err != nil {
		return model.ConsoleSpecRevision{}, err
	}
	sum := sha256.Sum256([]byte(req.ContentMarkdown))
	return model.ConsoleSpecRevision{SpecID: specID, Revision: revision, ContentMarkdown: req.ContentMarkdown,
		AcceptanceJSON: string(acceptance), InputAnchorsJSON: string(anchors), ContentSHA256: hex.EncodeToString(sum[:]), CreatedBy: actor.ID, CreatedByKind: actor.Kind}, nil
}

func (h *Hub) consoleSpecs() (consoleSpecRepository, bool) {
	repo, ok := h.Repo.(consoleSpecRepository)
	return repo, ok
}

func (h *Hub) HandleConsoleSpecs(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	actor := authenticatedConsoleSpecActor(r)
	management := platformRole(r) == consolePlatformRoleManagement
	if r.Method == http.MethodGet {
		items, err := repo.ListConsoleSpecs(r.Context(), principal, false) // “我的 spec”始终只列当前 principal
		if err != nil {
			http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
			return
		}
		includeArchived := r.URL.Query().Get("include_archived") == "1"
		visible := make([]model.ConsoleSpec, 0, len(items))
		for _, item := range items {
			if !item.Archived || includeArchived {
				visible = append(visible, item)
			}
		}
		items = visible
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			if _, scanErr := fmt.Sscanf(raw, "%d", &page); scanErr != nil || page < 1 {
				http.Error(w, "invalid page", 400)
				return
			}
		}
		const pageSize = 20
		for i := range items {
			items[i].CanDelete = items[i].PrincipalID == principal || management
			items[i].CanEdit = items[i].CurrentEditorID == principal && items[i].State != "final" && !items[i].Archived
			items[i].CanSplit = items[i].PrincipalID == principal && items[i].State == "final" && items[i].TaskState == "splitting" && !items[i].Archived
		}
		total := len(items)
		pages := (total + pageSize - 1) / pageSize
		if pages == 0 {
			pages = 1
		}
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		agents, err := repo.ListOwnedConsoleAgents(r.Context(), principal)
		if err != nil {
			http.Error(w, "spec Agent list unavailable", http.StatusServiceUnavailable)
			return
		}
		type assistAgent struct {
			ID          string `json:"id"`
			SessionName string `json:"session_name"`
			Online      bool   `json:"online"`
		}
		assistAgents := make([]assistAgent, 0, len(agents))
		for _, agent := range agents {
			assistAgents = append(assistAgents, assistAgent{ID: agent.ID, SessionName: agent.SessionName, Online: h.consoleAgentOnline(r.Context(), agent.ID, principal)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"role": platformRole(r), "specs": items[start:end], "agents": assistAgents, "page": page, "page_size": pageSize, "total": total, "total_pages": pages})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, ok := decodeConsoleSpecWrite(w, r)
	if !ok {
		return
	}
	id := "spec-" + randomHex(12)
	editToken := randomHex(32)
	persistedMarkdown, err := h.persistSpecImages(r.Context(), req.ContentMarkdown, principal, principal)
	if err != nil {
		http.Error(w, "spec image persistence failed", http.StatusServiceUnavailable)
		return
	}
	req.ContentMarkdown = persistedMarkdown
	rev, err := revisionFromRequest(id, actor, 1, req, h.now())
	if err != nil {
		http.Error(w, "invalid spec encoding: "+err.Error(), http.StatusBadRequest)
		return
	}
	spec := model.ConsoleSpec{ID: id, PrincipalID: principal, Title: strings.TrimSpace(req.Title), EditCapabilityHash: specCapabilityHash(editToken)}
	if err = repo.CreateConsoleSpec(r.Context(), spec, rev, h.now()); err != nil {
		http.Error(w, "console spec unavailable", http.StatusConflict)
		return
	}
	created, _ := repo.ConsoleSpec(r.Context(), principal, id, management)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		*model.ConsoleSpec
		EditURL string `json:"edit_url"`
	}{created, "/console/specs/" + id + "#edit=" + editToken})
}

func (h *Hub) HandleConsoleSpec(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	actor := authenticatedConsoleSpecActor(r)
	management := platformRole(r) == consolePlatformRoleManagement
	id := r.PathValue("specID")
	spec, err := repo.ConsoleSpec(r.Context(), principal, id, management)
	if err != nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if spec == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		spec.CanDelete = spec.PrincipalID == principal || management
		spec.CanEdit = spec.CurrentEditorID == principal && spec.State != "final" && !spec.Archived
		spec.CanSplit = spec.PrincipalID == principal && spec.State == "final" && spec.TaskState == "splitting" && !spec.Archived
		rev, err := repo.ConsoleSpecRevision(r.Context(), principal, id, spec.CurrentRevision, management)
		if err != nil {
			http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		tasks, err := repo.ListConsoleTaskProposals(r.Context(), id)
		if err != nil {
			http.Error(w, "console tasks unavailable", http.StatusServiceUnavailable)
			return
		}
		all, err := repo.ListAllConsoleTaskProposals(r.Context())
		if err != nil {
			http.Error(w, "console tasks unavailable", http.StatusServiceUnavailable)
			return
		}
		tasks, err = h.filterTaskFlowProjection(r, tasks)
		if err != nil {
			taskError(w, err)
			return
		}
		all, err = h.filterTaskFlowProjection(r, all)
		if err != nil {
			taskError(w, err)
			return
		}
		tasks = orderConsoleTasks(tasks, all)
		gateReview, err := repo.ConsoleSpecGateReview(r.Context(), id, spec.CurrentRevision)
		if err != nil {
			http.Error(w, "console spec review unavailable", http.StatusServiceUnavailable)
			return
		}
		attribution, err := repo.ConsoleSpecAttribution(r.Context(), id)
		if err != nil {
			http.Error(w, "console spec attribution unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"spec": spec, "revision": rev, "attribution": attribution, "gate_review": gateReview, "tasks": tasks, "dependency_candidates": all})
		return
	}
	if r.Method != http.MethodPatch {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	req, ok := decodeConsoleSpecWrite(w, r)
	if !ok {
		return
	}
	if req.ExpectedRevision <= 0 {
		http.Error(w, "expected_revision required", http.StatusBadRequest)
		return
	}
	imageActor := principal
	if imageActor == "" {
		imageActor = "shared-link"
	}
	req.ContentMarkdown, err = h.persistSpecImages(r.Context(), req.ContentMarkdown, imageActor, spec.PrincipalID)
	if err != nil {
		http.Error(w, "spec image persistence failed", http.StatusServiceUnavailable)
		return
	}
	rev, err := revisionFromRequest(id, actor, req.ExpectedRevision+1, req, h.now())
	if err != nil {
		http.Error(w, "invalid spec encoding: "+err.Error(), http.StatusBadRequest)
		return
	}
	capability := strings.TrimSpace(r.Header.Get("X-DW-Spec-Edit-Capability"))
	openNew, _ := r.Context().Value(consoleSpecOpenNewContextKey{}).(bool)
	var next int
	if direct, _ := r.Context().Value(consoleSpecDirectContextKey{}).(bool); direct {
		next, err = repo.AppendConsoleSpecRevisionByAgent(r.Context(), principal, id, req.ExpectedRevision, rev, h.now())
	} else if capability != "" {
		if openNew {
			next, err = repo.OpenConsoleSpecRevisionWithCapability(r.Context(), id, req.ExpectedRevision, specCapabilityHash(capability), rev, h.now())
		} else {
			next, err = repo.AppendConsoleSpecRevisionWithCapability(r.Context(), id, req.ExpectedRevision, specCapabilityHash(capability), rev, h.now())
		}
	} else if principal != "" {
		if openNew {
			next, err = repo.OpenConsoleSpecRevision(r.Context(), principal, id, req.ExpectedRevision, rev, h.now())
		} else {
			next, err = repo.AppendConsoleSpecRevision(r.Context(), principal, id, req.ExpectedRevision, rev, h.now())
		}
	} else {
		err = store.ErrConsoleSpecEditDenied
	}
	if errors.Is(err, store.ErrConsoleSpecAgentOwnership) {
		http.Error(w, "只能更新由当前 Agent 创建的 spec", http.StatusForbidden)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecConflict) {
		http.Error(w, "revision conflict", http.StatusConflict)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecEditDenied) {
		http.Error(w, "editing closed or invalid capability", http.StatusForbidden)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecFinal) {
		http.Error(w, "spec finalized; open a new version", http.StatusForbidden)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecRevisionLocked) {
		http.Error(w, "revision locked; open a new version", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "console spec unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"revision": next})
}

func (h *Hub) HandleConsoleTaskProposalPlan(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	var req struct {
		Content          string   `json:"content"`
		Status           string   `json:"status"`
		EstimatedHours   float64  `json:"estimated_hours"`
		DependsOn        []string `json:"depends_on"`
		DefinitionOfDone string   `json:"definition_of_done"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		http.Error(w, "invalid task plan", 400)
		return
	}
	now := h.now()
	update := func(ctx context.Context) error {
		if timed, ok := repo.(interface {
			UpdateConsoleTaskProposalPlanAt(context.Context, string, string, bool, string, string, float64, []string, string, time.Time) error
		}); ok {
			return timed.UpdateConsoleTaskProposalPlanAt(ctx, r.PathValue("proposalID"), platformPrincipal(r), platformRole(r) == consolePlatformRoleManagement, req.Content, req.Status, req.EstimatedHours, req.DependsOn, strings.TrimSpace(req.DefinitionOfDone), now)
		}
		return repo.UpdateConsoleTaskProposalPlan(ctx, r.PathValue("proposalID"), platformPrincipal(r), platformRole(r) == consolePlatformRoleManagement, req.Content, req.Status, req.EstimatedHours, req.DependsOn, strings.TrimSpace(req.DefinitionOfDone))
	}
	var updateErr error
	if flow, ok := h.taskFlowRepo(); ok {
		if state, e := flow.ConsoleTaskFlowState(r.Context(), r.PathValue("proposalID")); e == nil && state.GroupCode != "" {
			if state.Owner != platformPrincipal(r) || state.NeedsReassignment {
				taskError(w, store.ErrTaskFlowPermission)
				return
			}
			authorized, ok := h.Repo.(interface {
				UpdateConsoleTaskProposalPlanAuthorizedAt(context.Context, string, string, bool, string, string, float64, []string, string, func() error, time.Time) error
			})
			if !ok {
				taskError(w, store.ErrTaskFlowPermission)
				return
			}
			updateErr = h.withTaskIdentity(r.Context(), state, func(_ []identity.Agent, check func() error) error {
				return authorized.UpdateConsoleTaskProposalPlanAuthorizedAt(r.Context(), state.ID, platformPrincipal(r), false, req.Content, req.Status, req.EstimatedHours, req.DependsOn, strings.TrimSpace(req.DefinitionOfDone), check, now)
			})
		} else {
			updateErr = update(r.Context())
		}
	} else {
		updateErr = update(r.Context())
	}
	if err := updateErr; err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecPermission) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecTaskDelete(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := repo.DeleteConsoleSpecTask(r.Context(), r.PathValue("proposalID"), platformPrincipal(r)); err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecPermission) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecTasks(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Title            string   `json:"title"`
		Content          string   `json:"content"`
		EstimatedHours   float64  `json:"estimated_hours"`
		DependsOn        []string `json:"depends_on"`
		DefinitionOfDone string   `json:"definition_of_done"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" || req.EstimatedHours < 0 {
		http.Error(w, "task title, content and non-negative hours required", http.StatusBadRequest)
		return
	}
	deps, _ := json.Marshal(req.DependsOn)
	now := h.now()
	proposal := model.ConsoleTaskProposal{ID: "proposal:" + randomHex(16), SchemaVersion: 1, SpecID: r.PathValue("specID"),
		Title: strings.TrimSpace(req.Title), TaskContent: strings.TrimSpace(req.Content), Status: "planned", EstimatedHours: req.EstimatedHours,
		DependsOnJSON: string(deps), AcceptanceJSON: `[]`, DefinitionOfDone: strings.TrimSpace(req.DefinitionOfDone), ReviewID: "manual", ReviewedBy: platformPrincipal(r), ReviewedAt: now, CreatedAt: now}
	if err := repo.CreateConsoleTaskProposal(r.Context(), proposal, platformPrincipal(r), platformRole(r) == consolePlatformRoleManagement); err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecPermission) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(proposal)
}

func (h *Hub) HandleConsoleSpecWorkflow(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	principal := platformPrincipal(r)
	action := r.PathValue("action")
	var err error
	switch action {
	case "transfer":
		var req struct {
			TargetShortName  string `json:"target_short_name"`
			CancelRequestID  string `json:"cancel_request_id"`
			ExpectedRevision int    `json:"expected_revision"`
			ExpectedSHA      string `json:"expected_content_sha256"`
			State            string `json:"state"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil || strings.TrimSpace(req.TargetShortName) == "" {
			http.Error(w, "target_short_name required", 400)
			return
		}
		if req.CancelRequestID != "" {
			sr, ok := h.Repo.(specAgentRepository)
			if !ok {
				http.Error(w, "spec request store unavailable", 503)
				return
			}
			err = sr.CancelTransferSpecAgent(r.Context(), r.PathValue("specID"), principal, strings.TrimSpace(req.TargetShortName), req.State, req.CancelRequestID, req.ExpectedRevision, req.ExpectedSHA, h.now())
		} else {
			err = repo.TransferConsoleSpec(r.Context(), r.PathValue("specID"), principal, strings.TrimSpace(req.TargetShortName), req.State, h.now())
		}
	case "submit-review":
		err = repo.SubmitConsoleSpecReview(r.Context(), r.PathValue("specID"), principal, h.now())
	case "review":
		var req specGateReviewRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil || strings.TrimSpace(req.ReviewAgentID) == "" {
			http.Error(w, "structured review required", 400)
			return
		}
		issues, marshalErr := json.Marshal(req.Issues)
		if marshalErr != nil {
			http.Error(w, "structured review required", 400)
			return
		}
		err = repo.SaveConsoleSpecGateReview(r.Context(), r.PathValue("specID"), principal, model.ConsoleSpecGateReview{ReviewAgentID: strings.TrimSpace(req.ReviewAgentID), IssuesJSON: string(issues)}, h.now())
	case "finalize":
		err = repo.FinalizeConsoleSpec(r.Context(), r.PathValue("specID"), principal, h.now())
	case "close-tasks":
		err = repo.CloseConsoleSpecTaskSplit(r.Context(), r.PathValue("specID"), principal, h.now())
	default:
		http.Error(w, "unknown workflow action", 404)
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrConsoleSpecPermission) || errors.Is(err, store.ErrPMSpecFlowDenied) {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, err.Error(), http.StatusConflict)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecEditors(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	items, err := repo.ListActiveConsolePrincipals(r.Context())
	if err != nil {
		http.Error(w, "console principals unavailable", 503)
		return
	}
	out := make([]map[string]string, 0, len(items))
	for _, p := range items {
		if h.Identity != nil {
			// 新平台（R-V2-SPEC-TASK-SHIM-IMPL G5）：候选只取垫片投影的新平台用户；其 owner 作用域是 u_<用户>，
			// 不是给人看的简称 ⇒ 不下发 short_name，页面只显示 display_name（＝新平台简称）。
			if p.Provider != store.NewPlatformConsoleProvider {
				continue
			}
			p.ShortName = ""
		}
		if item, ok := consoleSpecEditorItem(p); ok {
			out = append(out, item)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"editors": out})
}

func consoleSpecEditorItem(p model.ConsolePrincipal) (map[string]string, bool) {
	display := strings.TrimSpace(p.DisplayName)
	if display == "" {
		return nil, false
	}
	return map[string]string{"id": p.ID, "short_name": strings.TrimSpace(p.ShortName), "display_name": display, "value": display}, true
}

func (h *Hub) HandleConsoleSpecNewVersion(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), consoleSpecOpenNewContextKey{}, true))
	r.Method = http.MethodPatch
	h.HandleConsoleSpec(w, r)
}

func (h *Hub) HandleConsoleSpecDerive(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	sourceID := r.PathValue("specID")
	source, err := repo.ConsoleSpec(r.Context(), principal, sourceID, false)
	if err != nil || source == nil {
		http.NotFound(w, r)
		return
	}
	if source.PrincipalID != principal || source.State != "final" {
		http.Error(w, "only an owned finalized spec can be derived", http.StatusConflict)
		return
	}
	sourceRevision, err := repo.ConsoleSpecRevision(r.Context(), principal, sourceID, source.CurrentRevision, false)
	if err != nil || sourceRevision == nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	id := "spec-" + randomHex(12)
	editToken := randomHex(32)
	actor := authenticatedConsoleSpecActor(r)
	revision := *sourceRevision
	revision.SpecID = id
	revision.Revision = 1
	revision.CreatedBy = actor.ID
	revision.CreatedByKind = actor.Kind
	revision.Supersedes = 0
	spec := model.ConsoleSpec{ID: id, PrincipalID: principal, Title: source.Title + "（派生）", EditCapabilityHash: specCapabilityHash(editToken), DerivedFrom: sourceID}
	if err := repo.DeriveConsoleSpec(r.Context(), principal, sourceID, spec, revision, h.now()); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	created, _ := repo.ConsoleSpec(r.Context(), principal, id, false)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"spec": created, "edit_url": "/console/specs/" + id + "#edit=" + editToken})
}

func (h *Hub) HandleConsoleSpecCloseEditing(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := repo.CloseConsoleSpecEditing(r.Context(), platformPrincipal(r), r.PathValue("specID"), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecReopenEditing(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := repo.ReopenConsoleSpecEditing(r.Context(), platformPrincipal(r), r.PathValue("specID"), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecArchive(w http.ResponseWriter, r *http.Request) {
	h.setConsoleSpecArchive(w, r, true, false)
}
func (h *Hub) HandleConsoleSpecUnarchive(w http.ResponseWriter, r *http.Request) {
	h.setConsoleSpecArchive(w, r, false, false)
}
func (h *Hub) HandleConsoleSpecsArchiveBatch(w http.ResponseWriter, r *http.Request) {
	h.setConsoleSpecArchive(w, r, true, true)
}
func (h *Hub) setConsoleSpecArchive(w http.ResponseWriter, r *http.Request, archived, batch bool) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	ids := []string{r.PathValue("specID")}
	if batch {
		var body struct {
			IDs []string `json:"spec_ids"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&body) != nil {
			http.Error(w, "invalid spec IDs", 400)
			return
		}
		ids = body.IDs
	}
	err := repo.SetConsoleSpecsArchived(r.Context(), ids, platformPrincipal(r), platformRole(r) == consolePlatformRoleManagement, archived, h.now())
	if errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecPermission) {
		http.Error(w, "forbidden", 403)
		return
	}
	if err != nil {
		http.Error(w, "cannot change archive", 409)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleTaskProposalsReady(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	items, err := repo.ListReadyConsoleTaskProposals(r.Context())
	if err != nil {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	type sourceV1 struct {
		SpecID        string `json:"spec_id"`
		Revision      int    `json:"revision"`
		ContentSHA256 string `json:"content_sha256"`
	}
	type reviewV1 struct {
		ReviewID   string    `json:"review_id"`
		ReviewedBy string    `json:"reviewed_by"`
		ReviewedAt time.Time `json:"reviewed_at"`
	}
	type proposalV1 struct {
		SchemaVersion int             `json:"schema_version"`
		ProposalID    string          `json:"proposal_id"`
		Source        sourceV1        `json:"source"`
		Title         string          `json:"title"`
		Acceptance    json.RawMessage `json:"acceptance"`
		InputAnchors  json.RawMessage `json:"input_anchors"`
		HumanReview   reviewV1        `json:"human_review"`
		CreatedAt     time.Time       `json:"created_at"`
	}
	out := make([]proposalV1, 0, len(items))
	for _, item := range items {
		out = append(out, proposalV1{SchemaVersion: item.SchemaVersion, ProposalID: item.ID, Source: sourceV1{item.SpecID, item.Revision, item.ContentSHA256}, Title: item.Title, Acceptance: json.RawMessage(item.AcceptanceJSON), InputAnchors: json.RawMessage(item.InputAnchorsJSON), HumanReview: reviewV1{item.ReviewID, item.ReviewedBy, item.ReviewedAt}, CreatedAt: item.CreatedAt})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposals": out})
}
