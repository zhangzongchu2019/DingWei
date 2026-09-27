package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func generationGateBody(criteria string) string {
	return `{"title":"S-GEN","content_markdown":"# gate owner:q1 owner:q2 owner:q owner:先挡住","acceptance":` + criteria + `,"inferences":[{"id":"I1","text":"可能需要提示","generated_by":"agent-writer","status":"pending_owner_confirmation"}],"input_anchors":[{"kind":"owner_message","ref":"owner:quote"}]}`
}

func TestConsoleSpecGenerationGatePersistsMultipleCriteriaAndUsesAuthenticatedSource(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	criteria := `[{"id":"A1","text":"owner criterion","owner_ref":"owner:q1","source":"owner","checked_by":"checker","checked_at":"2026-09-25T01:02:03Z","cases":["case-a","case-b"]},{"id":"A2","text":"AI criterion","owner_ref":"owner:q2","source":"ai","generated_by":"agent-writer","checked_by":"checker","checked_at":"2026-09-25T01:02:04Z","cases":["case-c"]}]`
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(criteria), hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rev, err := hub.Repo.(*store.SQLite).ConsoleSpecRevision(context.Background(), "owner-a", created.ID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	var bundle specAcceptanceBundle
	if err := json.Unmarshal([]byte(rev.AcceptanceJSON), &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Criteria) != 2 || len(bundle.Inferences) != 1 || bundle.Criteria[0].Source != "owner" || bundle.Criteria[0].GeneratedBy != "" || bundle.Criteria[1].Source != "owner" || bundle.Criteria[1].GeneratedBy != "" || len(bundle.Criteria[0].Cases) != 2 {
		t.Fatalf("bundle=%+v", bundle)
	}
}

func TestConsoleSpecGenerationGateRejectsMissingOriginalQuoteButIgnoresSource(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(`[{"id":"A1","text":"no quote","source":"invalid"}]`), hub.HandleConsoleSpecs)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing owner_ref code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(`[{"id":"client-id","text":"source ignored","owner_ref":"owner:q","source":"invalid","generated_by":"person-or-agent"}]`), hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("removed source field still constrained code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestConsoleSpecFinalizeRequiresVerificationIndependentReviewerAndCompleteIssues(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	criteria := `[{"id":"A1","text":"gate","owner_ref":"owner:q","source":"owner","checked_by":"checker","checked_at":"2026-09-25T01:02:03Z","cases":["case-a","case-b"]}]`
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(criteria), hub.HandleConsoleSpecs)
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := db.TransferConsoleSpec(context.Background(), created.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != store.ErrConsoleSpecGate {
		t.Fatalf("finalize without review err=%v", err)
	}
	for _, agent := range []model.ConsoleAgent{
		{ID: "self-reviewer", KeyID: "self-key", SessionName: "self", OwnerPrincipal: "manager", Active: true},
		{ID: "independent-reviewer", KeyID: "other-key", SessionName: "other", OwnerPrincipal: "owner-a", Active: true},
		{ID: "inactive-reviewer", KeyID: "inactive-key", SessionName: "inactive", OwnerPrincipal: "owner-a", Active: false},
	} {
		if err := db.UpsertConsoleAgent(context.Background(), agent); err != nil {
			t.Fatal(err)
		}
	}
	for _, reviewer := range []string{"", "missing-reviewer", "inactive-reviewer"} {
		err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: reviewer, IssuesJSON: `[{"id":"Q","text":"疑点"}]`}, hub.now())
		if err != store.ErrConsoleSpecReviewerInvalid {
			t.Fatalf("reviewer=%q err=%v want ErrConsoleSpecReviewerInvalid", reviewer, err)
		}
	}
	pending := `[{
		"id":"Q1","text":"owner_ref 是否逐条成立","response":"","outcome":"pending"
	}]`
	// 🔴 业主 2026-09-25 裁定「删」：不再按 Agent 归属人拒绝 ⇒ 审结者名下的 Agent **现在应当放行**。
	// 🔵 Q7 的真语义（复核者 ≠ 当前环节操作者）另开一项；本批只保证「记录 ＋ 不卡死」。
	if err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: "self-reviewer", IssuesJSON: pending}, hub.now()); err != nil {
		t.Fatalf("self-owned reviewer must now be accepted (owner ruling 2026-09-25): err=%v", err)
	}
	if err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: "independent-reviewer", IssuesJSON: pending}, hub.now()); err != nil {
		t.Fatal(err)
	}
	for _, empty := range []string{`[]`, `null`} {
		if err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: "independent-reviewer", IssuesJSON: empty}, hub.now()); err != nil {
			t.Fatal(err)
		}
		if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != store.ErrConsoleSpecGate {
			t.Fatalf("empty issues=%s finalize err=%v", empty, err)
		}
	}
	if err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: "independent-reviewer", IssuesJSON: pending}, hub.now()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != store.ErrConsoleSpecGate {
		t.Fatalf("incomplete response err=%v", err)
	}
	complete := `[{"id":"Q1","text":"owner_ref 是否逐条成立","response":"已补逐条引用","outcome":"true_problem"},{"id":"Q2","text":"第二疑点","response":"证据表明无需改","outcome":"false_positive"}]`
	if err := db.SaveConsoleSpecGateReview(context.Background(), created.ID, "manager", model.ConsoleSpecGateReview{ReviewAgentID: "independent-reviewer", IssuesJSON: complete}, hub.now()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	review, err := db.ConsoleSpecGateReview(context.Background(), created.ID, 1)
	if err != nil || review == nil || !strings.Contains(review.IssuesJSON, "owner_ref 是否逐条成立") || !strings.Contains(review.IssuesJSON, "已补逐条引用") || !strings.Contains(review.IssuesJSON, "false_positive") {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	patch := strings.TrimSuffix(generationGateBody(criteria), "}") + `,"expected_revision":1}`
	req := httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+created.ID, bytes.NewBufferString(patch))
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("post-final write code=%d body=%s", rr.Code, rr.Body.String())
	}
}
