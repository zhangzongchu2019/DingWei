package evidence

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type fakeProvider struct {
	out string
	err error
}

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Complete(ctx context.Context, system, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

type sequenceProvider struct {
	outs  []string
	calls int
}

func (s *sequenceProvider) Name() string { return "sequence" }
func (s *sequenceProvider) Complete(ctx context.Context, system, user string) (string, error) {
	if s.calls >= len(s.outs) {
		return s.outs[len(s.outs)-1], nil
	}
	out := s.outs[s.calls]
	s.calls++
	return out, nil
}

func newEvidenceTest(t *testing.T) (*store.SQLite, context.Context, *clock.Fake) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "evidence.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	clk := &clock.Fake{T: time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)}
	return db, ctx, clk
}

func TestRunOnceExtractsEvidenceAndReconcilesFiveStates(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", false)
	seedSchedule(t, db, ctx, "alice", "开发网关")
	seedSchedule(t, db, ctx, "alice", "写测试")
	seedSchedule(t, db, ctx, "alice", "发布上线")
	seedSchedule(t, db, ctx, "alice", "整理需求")
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "开发网关", Note: "完成", Percent: 100, ReportedAt: clk.Now(), Source: "self"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "写测试", Note: "进行中", Percent: 40, ReportedAt: clk.Now(), Source: "self"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "发布上线", Note: "完成", Percent: 100, ReportedAt: clk.Now(), Source: "self"}); err != nil {
		t.Fatal(err)
	}
	adapter := &FakeAdapter{
		Sessions: []Session{{ID: "s1", Source: "fake", OwnerKey: "alice", OwnerConfidence: 0.9}},
		Chunks: map[string][]Chunk{"s1": {{
			SessionID:  "s1",
			Source:     "fake",
			Text:       "完成开发网关并提交文件 internal/gateway.go",
			OccurredAt: clk.Now(),
		}}},
	}
	provider := fakeProvider{out: `{"items":[{"work_item":"开发网关","action_type":"code","artifact":"internal/gateway.go","files":["internal/gateway.go"],"occurred_at":"2026-06-29T10:00:00Z","confidence":0.91,"raw_excerpt":"完成开发网关"}]}`}
	svc := New(db, adapter, provider, clk)
	result, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.EvidenceWritten != 1 || result.Reconciled != 4 {
		t.Fatalf("result = %+v", result)
	}
	evs, err := db.ListAIEvidence(ctx, "alice", "开发网关")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].WorkItem != "开发网关" || evs[0].RawExcerptHash == "" || strings.Contains(evs[0].RawExcerptHash, "完成开发网关") {
		t.Fatalf("evidence = %+v", evs)
	}
	recs, err := db.ListReconciliation(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	got := verdicts(recs)
	if got["开发网关"] != "confirmed" || got["写测试"] != "partial" || got["发布上线"] != "suspected_lag" || got["整理需求"] != "no_evidence" {
		t.Fatalf("verdicts = %+v recs=%+v", got, recs)
	}
}

func TestRunOnceSkipsOptOutWithoutStoringEvidence(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", true)
	adapter := &FakeAdapter{
		Sessions: []Session{{ID: "s1", Source: "fake", OwnerKey: "alice"}},
		Chunks:   map[string][]Chunk{"s1": {{SessionID: "s1", Text: "完成开发网关", OccurredAt: clk.Now()}}},
	}
	svc := New(db, adapter, fakeProvider{out: `{"items":[{"work_item":"开发网关","confidence":0.8}]}`}, clk)
	result, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedOptOut != 1 || result.EvidenceWritten != 0 {
		t.Fatalf("result = %+v", result)
	}
	evs, err := db.ListAIEvidence(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("opt-out evidence stored: %+v", evs)
	}
}

func TestRunOnceLLMDownPausesWithoutReconciliation(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", false)
	seedSchedule(t, db, ctx, "alice", "开发网关")
	adapter := &FakeAdapter{
		Sessions: []Session{{ID: "s1", Source: "fake", OwnerKey: "alice"}},
		Chunks:   map[string][]Chunk{"s1": {{SessionID: "s1", Text: "完成开发网关", OccurredAt: clk.Now()}}},
	}
	svc := New(db, adapter, &llm.Failover{Providers: []llm.Provider{
		fakeProvider{err: errors.New("primary down")},
		fakeProvider{err: errors.New("backup down")},
	}}, clk)
	result, err := svc.RunOnce(ctx)
	if !errors.Is(err, llm.ErrAllDown) || !result.Paused {
		t.Fatalf("RunOnce err=%v result=%+v", err, result)
	}
	recs, err := db.ListReconciliation(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("paused run wrote reconciliation: %+v", recs)
	}
}

func TestRunOnceRejectsInvalidExtractionSchema(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", false)
	adapter := &FakeAdapter{
		Sessions: []Session{{ID: "s1", Source: "fake", OwnerKey: "alice"}},
		Chunks:   map[string][]Chunk{"s1": {{SessionID: "s1", Text: "完成开发网关", OccurredAt: clk.Now()}}},
	}
	svc := New(db, adapter, fakeProvider{out: `{"items":[{"confidence":0.8}]}`}, clk)
	if _, err := svc.RunOnce(ctx); err == nil {
		t.Fatalf("invalid extraction schema accepted")
	}
	evs, err := db.ListAIEvidence(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("invalid schema wrote evidence: %+v", evs)
	}
}

func TestRunOnceRetriesInvalidExtractionSchemaOnce(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", false)
	adapter := &FakeAdapter{
		Sessions: []Session{{ID: "s1", Source: "fake", OwnerKey: "alice"}},
		Chunks:   map[string][]Chunk{"s1": {{SessionID: "s1", Text: "完成开发网关", OccurredAt: clk.Now()}}},
	}
	provider := &sequenceProvider{outs: []string{
		`{"items":[{"confidence":0.8}]}`,
		`{"items":[{"work_item":"开发网关","confidence":0.8}]}`,
	}}
	svc := New(db, adapter, provider, clk)
	result, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || result.EvidenceWritten != 1 {
		t.Fatalf("retry result=%+v calls=%d", result, provider.calls)
	}
}

func TestAssociateEvidenceManualLinkReconcilesAhead(t *testing.T) {
	db, ctx, clk := newEvidenceTest(t)
	seedMember(t, db, ctx, "alice", false)
	seedSchedule(t, db, ctx, "alice", "补文档")
	if err := db.AddAIEvidence(ctx, model.AIEvidence{ID: "ev1", OwnerKey: "alice", WorkItem: "整理说明", Confidence: 0.9, OccurredAt: clk.Now(), RawExcerptHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	svc := New(db, &FakeAdapter{}, fakeProvider{}, clk)
	if err := svc.AssociateEvidence(ctx, "alice", "ev1", "补文档"); err != nil {
		t.Fatal(err)
	}
	evs, err := db.ListAIEvidence(ctx, "alice", "补文档")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ID != "ev1" {
		t.Fatalf("manual link evidence = %+v", evs)
	}
	recs, err := db.ListReconciliation(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if verdicts(recs)["补文档"] != "ahead" {
		t.Fatalf("manual link recs = %+v", recs)
	}
}

func seedMember(t *testing.T, db *store.SQLite, ctx context.Context, owner string, optOut bool) {
	t.Helper()
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: owner, Role: model.RoleMember, EvidenceOptOut: optOut, Active: true}); err != nil {
		t.Fatal(err)
	}
}

func seedSchedule(t *testing.T, db *store.SQLite, ctx context.Context, owner, task string) {
	t.Helper()
	if err := db.UpsertSchedule(ctx, model.Schedule{ID: owner + ":" + task, OwnerKey: owner, StartDate: "2026-06-29", EndDate: "2026-06-30", Task: task, Status: "planned", Priority: 100}); err != nil {
		t.Fatal(err)
	}
}

func verdicts(recs []model.Reconciliation) map[string]string {
	out := map[string]string{}
	for _, r := range recs {
		out[r.TaskKey] = r.Verdict
	}
	return out
}
