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

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type consoleSpecRepository interface {
	CreateConsoleSpec(context.Context, model.ConsoleSpec, model.ConsoleSpecRevision, time.Time) error
	ListConsoleSpecs(context.Context, string, bool) ([]model.ConsoleSpec, error)
	ConsoleSpec(context.Context, string, string, bool) (*model.ConsoleSpec, error)
	ConsoleSpecRevision(context.Context, string, string, int, bool) (*model.ConsoleSpecRevision, error)
	AppendConsoleSpecRevision(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	AppendConsoleSpecRevisionWithCapability(context.Context, string, int, string, model.ConsoleSpecRevision, time.Time) (int, error)
	OpenConsoleSpecRevision(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	OpenConsoleSpecRevisionWithCapability(context.Context, string, int, string, model.ConsoleSpecRevision, time.Time) (int, error)
	CloseConsoleSpecEditing(context.Context, string, string, time.Time) error
	ReopenConsoleSpecEditing(context.Context, string, string, time.Time) error
	ArchiveConsoleSpec(context.Context, string, string, time.Time) error
	ListReadyConsoleTaskProposals(context.Context) ([]model.ConsoleTaskProposal, error)
	ListConsoleTaskProposals(context.Context, string) ([]model.ConsoleTaskProposal, error)
	ListAllConsoleTaskProposals(context.Context) ([]model.ConsoleTaskProposal, error)
	CreateConsoleTaskProposal(context.Context, model.ConsoleTaskProposal, string, bool) error
	UpdateConsoleTaskProposalPlan(context.Context, string, string, bool, string, string, float64, []string, string) error
	DeleteConsoleSpecTask(context.Context, string, string) error
	ListActiveConsolePrincipals(context.Context) ([]model.ConsolePrincipal, error)
	TransferConsoleSpec(context.Context, string, string, string, string, time.Time) error
	FinalizeConsoleSpec(context.Context, string, string, time.Time) error
	SaveConsoleSpecGateReview(context.Context, string, string, model.ConsoleSpecGateReview, time.Time) error
	ConsoleSpecGateReview(context.Context, string, int) (*model.ConsoleSpecGateReview, error)
	CloseConsoleSpecTaskSplit(context.Context, string, string, time.Time) error
	ListOwnedConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	ResolveConsoleAgentByEndpoint(context.Context, string, string) (*model.ConsoleAgent, error)
	StartConsoleSpecPresplit(context.Context, model.ConsoleSpecPresplitRequest, string) (*model.ConsoleSpecRevision, *model.ConsoleAgent, error)
	ConsoleSpecPresplit(context.Context, string, string, time.Time, time.Time) (*model.ConsoleSpecPresplitRequest, error)
	FailConsoleSpecPresplitDispatch(context.Context, string, string, time.Time) error
	RejectConsoleSpecPresplitResult(context.Context, string, model.ConsoleAgent, string, time.Time) error
	CompleteConsoleSpecPresplit(context.Context, string, model.ConsoleAgent, []store.ConsoleSpecPresplitTask, time.Time) error
}

type consoleSpecOpenNewContextKey struct{}

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
}

func validateConsoleSpecWrite(req consoleSpecWriteRequest) error {
	if strings.TrimSpace(req.Title) == "" || len(req.Title) > 200 || strings.TrimSpace(req.ContentMarkdown) == "" || len(req.Acceptance) == 0 || len(req.InputAnchors) == 0 {
		return errors.New("title, content, acceptance and input anchors are required")
	}
	seen := map[string]bool{}
	for _, item := range req.Acceptance {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.OwnerRef) == "" || seen[item.ID] {
			return errors.New("acceptance ids, text and owner_ref must be non-empty and unique")
		}
		if !strings.Contains(req.ContentMarkdown, item.OwnerRef) {
			return errors.New("acceptance original quote must occur in current spec content")
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
	if decoder.Decode(&req) != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return consoleSpecWriteRequest{}, false
	}
	if validateConsoleSpecWrite(req) != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return consoleSpecWriteRequest{}, false
	}
	return req, true
}

func revisionFromRequest(specID, principal string, revision int, req consoleSpecWriteRequest, now time.Time) (model.ConsoleSpecRevision, error) {
	for i := range req.Acceptance {
		req.Acceptance[i].ID = fmt.Sprintf("A%d", i+1)
		req.Acceptance[i].Source = "owner"
		if strings.TrimSpace(req.Acceptance[i].GeneratedBy) != "" {
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
		AcceptanceJSON: string(acceptance), InputAnchorsJSON: string(anchors), ContentSHA256: hex.EncodeToString(sum[:]), CreatedBy: principal}, nil
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
	management := platformRole(r) == consolePlatformRoleManagement
	if r.Method == http.MethodGet {
		items, err := repo.ListConsoleSpecs(r.Context(), principal, false) // “我的 spec”始终只列当前 principal
		if err != nil {
			http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
			return
		}
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
			items[i].CanEdit = items[i].CurrentEditorID == principal && items[i].State != "final" && !items[i].Archived
			items[i].CanSplit = items[i].PrincipalID == principal && items[i].State == "final" && items[i].TaskState == "splitting"
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
		_ = json.NewEncoder(w).Encode(map[string]any{"role": platformRole(r), "specs": items[start:end], "page": page, "page_size": pageSize, "total": total, "total_pages": pages})
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
	rev, err := revisionFromRequest(id, principal, 1, req, h.now())
	if err != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
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
		spec.CanEdit = spec.CurrentEditorID == principal && spec.State != "final" && !spec.Archived
		spec.CanSplit = spec.PrincipalID == principal && spec.State == "final" && spec.TaskState == "splitting"
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
		tasks = orderConsoleTasks(tasks, all)
		gateReview, err := repo.ConsoleSpecGateReview(r.Context(), id, spec.CurrentRevision)
		if err != nil {
			http.Error(w, "console spec review unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"spec": spec, "revision": rev, "gate_review": gateReview, "tasks": tasks, "dependency_candidates": all})
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
	actor := principal
	if actor == "" {
		actor = "shared-link"
	}
	req.ContentMarkdown, err = h.persistSpecImages(r.Context(), req.ContentMarkdown, actor, spec.PrincipalID)
	if err != nil {
		http.Error(w, "spec image persistence failed", http.StatusServiceUnavailable)
		return
	}
	rev, err := revisionFromRequest(id, principal, req.ExpectedRevision+1, req, h.now())
	if err != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return
	}
	capability := strings.TrimSpace(r.Header.Get("X-DW-Spec-Edit-Capability"))
	openNew, _ := r.Context().Value(consoleSpecOpenNewContextKey{}).(bool)
	var next int
	if capability != "" {
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
	dod, _ := json.Marshal([]specAcceptance{{ID: "DOD", Text: strings.TrimSpace(req.DefinitionOfDone)}})
	if strings.TrimSpace(req.DefinitionOfDone) == "" {
		dod = []byte("[]")
	}
	if err := repo.UpdateConsoleTaskProposalPlan(r.Context(), r.PathValue("proposalID"), platformPrincipal(r), platformRole(r) == consolePlatformRoleManagement, req.Content, req.Status, req.EstimatedHours, req.DependsOn, string(dod)); err != nil {
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
	dod, _ := json.Marshal([]specAcceptance{{ID: "DOD", Text: strings.TrimSpace(req.DefinitionOfDone)}})
	if strings.TrimSpace(req.DefinitionOfDone) == "" {
		dod = []byte("[]")
	}
	now := h.now()
	proposal := model.ConsoleTaskProposal{ID: "proposal:" + randomHex(16), SchemaVersion: 1, SpecID: r.PathValue("specID"),
		Title: strings.TrimSpace(req.Title), TaskContent: strings.TrimSpace(req.Content), Status: "planned", EstimatedHours: req.EstimatedHours,
		DependsOnJSON: string(deps), AcceptanceJSON: string(dod), ReviewID: "manual", ReviewedBy: platformPrincipal(r), ReviewedAt: now, CreatedAt: now}
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
			TargetShortName string `json:"target_short_name"`
			State           string `json:"state"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil || strings.TrimSpace(req.TargetShortName) == "" {
			http.Error(w, "target_short_name required", 400)
			return
		}
		err = repo.TransferConsoleSpec(r.Context(), r.PathValue("specID"), principal, strings.TrimSpace(req.TargetShortName), req.State, h.now())
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
		if errors.Is(err, store.ErrConsoleSpecPermission) {
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
	return map[string]string{"short_name": strings.TrimSpace(p.ShortName), "display_name": display, "value": display}, true
}

func (h *Hub) HandleConsoleSpecNewVersion(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), consoleSpecOpenNewContextKey{}, true))
	r.Method = http.MethodPatch
	h.HandleConsoleSpec(w, r)
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
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	if err := repo.ArchiveConsoleSpec(r.Context(), r.PathValue("specID"), platformPrincipal(r), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "cannot archive spec", http.StatusConflict)
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
