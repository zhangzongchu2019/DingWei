package coordinatereview_test

import (
	"context"
	"errors"
	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type authority struct {
	deny  string
	final bool
	calls int
}

func (a *authority) WithReviewAccess(ctx context.Context, actor string, r cr.Record, fn func(func() error) error) error {
	a.calls++
	if actor == a.deny || (actor != r.AssignedBy && actor != cr.AgentActor(r.AgentID)) {
		return cr.ErrForbidden
	}
	return fn(func() error {
		if a.final {
			return cr.ErrForbidden
		}
		return ctx.Err()
	})
}
func (a *authority) WithReviewNotice(ctx context.Context, r cr.Record, fn func(func() error, string, string) error) error {
	return a.WithReviewAccess(ctx, r.AssignedBy, r, func(check func() error) error { return fn(check, "bot", "reviewer") })
}

type reader struct {
	files   map[string]map[string]string
	missing map[string]bool
	seen    []cr.Coordinate
	deny    string
	head    string
}

func (r *reader) Read(_ context.Context, actor string, c cr.Coordinate, p string) (specread.Result, error) {
	r.seen = append(r.seen, c)
	if r.missing[c.Commit] || actor == r.deny {
		return specread.Result{}, cr.ErrUnavailable
	}
	return specread.Result{Coordinate: specread.Coordinate{Commit: c.Commit, Path: p}, Text: strings.Repeat("x", 71680) + "END"}, nil
}
func (r *reader) Snapshot(_ context.Context, _ string, c cr.Coordinate) (sc.Snapshot, error) {
	if r.missing[c.Commit] {
		return sc.Snapshot{}, cr.ErrUnavailable
	}
	s := sc.Snapshot{Coordinate: c.Coordinate, Files: map[string][]byte{}}
	for p, b := range r.files[c.Commit] {
		s.Files[c.ChangeDir+"/"+p] = []byte(b)
	}
	return s, nil
}
func (r *reader) Resolve(_ context.Context, _ string, c cr.Coordinate, _ string) (cr.Coordinate, error) {
	if r.head == "" {
		return c, cr.ErrUnavailable
	}
	c.Commit = r.head
	return c, nil
}

type fixture struct {
	s   *cr.Service
	db  *store.SQLite
	a   *authority
	r   *reader
	now time.Time
	ctx context.Context
	c   cr.Coordinate
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, e := store.OpenSQLite(filepath.Join(t.TempDir(), "reviews.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	if e = db.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	f := &fixture{db: db, a: &authority{}, r: &reader{files: map[string]map[string]string{}, missing: map[string]bool{}}, now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), ctx: context.Background(), c: cr.Coordinate{Provider: "github", Coordinate: sc.Coordinate{Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/example", Commit: strings.Repeat("a", 40)}}}
	f.s = &cr.Service{Store: db, Access: f.a, Reader: f.r, Now: func() time.Time { return f.now }}
	f.r.files[f.c.Commit] = map[string]string{"proposal.md": "scope", "specs/cap/spec.md": "requirement\nscenario", "tasks.md": "- [ ] T-01 task\n## 交接记录\nold\n"}
	return f
}
func (f *fixture) create(t *testing.T, agent, op string) cr.Record {
	t.Helper()
	r, e := f.s.Create(f.ctx, "owner", cr.Create{Coordinate: f.c, AgentID: agent, Operation: op})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *fixture) read(t *testing.T, r cr.Record) {
	t.Helper()
	a := cr.AgentActor(r.AgentID)
	for _, p := range []string{"openspec/specs/cap/context.md", "openspec/specs/cap/spec.md", r.Coordinate.ChangeDir + "/proposal.md"} {
		out, e := f.s.Read(f.ctx, a, r.ID, p)
		if e != nil || !strings.HasSuffix(out.Text, "END") {
			t.Fatalf("R6_003_PINNED_READ %v", e)
		}
	}
	if _, e := f.s.Timeline(f.ctx, a, r.ID); e != nil {
		t.Fatal(e)
	}
}
func result(r cr.Record) cr.Result {
	return cr.Result{Operation: "result-operation-001", Commit: r.Coordinate.Commit, Verdict: "PASS", Evidence: "report:independent-review"}
}
func TestR6CoordinateValidationAndIndependentReads(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	if len(f.r.seen) != 2 || r.Coordinate != f.c {
		t.Fatal("R6_001_COORDINATES")
	}
	for _, change := range []func(*cr.Create){func(q *cr.Create) { q.Coordinate.Commit = "main" }, func(q *cr.Create) { q.Coordinate.Commit = "abcdef" }, func(q *cr.Create) { q.Coordinate.ChangeDir = "openspec/changes/../../secrets" }, func(q *cr.Create) { q.Coordinate.Repository = "https://repo.invalid/x" }, func(q *cr.Create) { q.Coordinate.Instance = "other.invalid" }} {
		q := cr.Create{Coordinate: f.c, AgentID: "agentA", Operation: "invalid-operation-001"}
		change(&q)
		if _, e := f.s.Create(f.ctx, "owner", q); e == nil {
			t.Fatal("R6_001_INVALID_ACCEPTED")
		}
	}
	f.r.deny = cr.AgentActor("agentB")
	if _, e := f.s.Create(f.ctx, "owner", cr.Create{Coordinate: f.c, AgentID: "agentB", Operation: "denied-operation-001"}); e == nil {
		t.Fatal("R6_002_AGENT_INHERITED")
	}
	f.a.deny = "owner"
	if _, e := f.s.Get(f.ctx, "owner", r.ID); e == nil {
		t.Fatal("R6_002_REVOKED_CACHE")
	}
	f.a.deny = ""
	f.a.final = true
	if _, e := f.s.Create(f.ctx, "owner", cr.Create{Coordinate: f.c, AgentID: "agentA", Operation: "final-operation-001"}); e == nil {
		t.Fatal("R6_002_FINAL_FENCE")
	}
}
func TestR6PinnedReadResultsIdempotencyAndIndependentPeople(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	other := f.create(t, "agentB", "create-operation-002")
	a := cr.AgentActor(r.AgentID)
	if _, e := f.s.Submit(f.ctx, a, r.ID, result(r)); !errors.Is(e, cr.ErrConflict) {
		t.Fatal("R6_003_UNREAD_PASS", e)
	}
	f.read(t, r)
	for _, c := range f.r.seen {
		if c.Commit != f.c.Commit {
			t.Fatal("R6_003_HEAD_FALLBACK")
		}
	}
	if _, e := f.s.Submit(f.ctx, cr.AgentActor(other.AgentID), r.ID, result(r)); e == nil {
		t.Fatal("R6_004_FORGED_REVIEWER")
	}
	wrong := result(r)
	wrong.Commit = strings.Repeat("b", 40)
	if _, e := f.s.Submit(f.ctx, a, r.ID, wrong); e == nil {
		t.Fatal("R6_004_WRONG_VERSION")
	}
	got, e := f.s.Submit(f.ctx, a, r.ID, result(r))
	if e != nil || len(got.Results) != 1 || got.Results[0].Reviewer != r.AgentID || !got.Results[0].At.Equal(f.now) {
		t.Fatal("R6_004_TRUSTED_FACT", e)
	}
	f.now = f.now.Add(time.Minute)
	retry, e := f.s.Submit(f.ctx, a, r.ID, result(r))
	if e != nil || len(retry.Results) != 1 || retry.Results[0] != got.Results[0] {
		t.Fatal("R6_004_RETRY_OVERWRITE", e)
	}
	conflict := result(r)
	conflict.Verdict = "FAIL"
	if _, e = f.s.Submit(f.ctx, a, r.ID, conflict); !errors.Is(e, cr.ErrConflict) {
		t.Fatal("R6_004_CONFLICT_OVERWRITE")
	}
	conflict.Operation = "result-operation-002"
	if q, e := f.s.Submit(f.ctx, a, r.ID, conflict); e != nil || len(q.Results) != 2 {
		t.Fatal("R6_004_APPEND_REVISION", e)
	}
	b, e := f.s.Get(f.ctx, "owner", other.ID)
	if e != nil || len(b.Results) != 0 || !b.Completed.IsZero() {
		t.Fatal("R6_008_ONE_RESULT_FILLED_OTHERS")
	}
	f.a.deny = "owner"
	if rows, _ := f.s.List(f.ctx, "owner", ""); len(rows) != 0 {
		t.Fatal("R6_008_REVOKED_VISIBLE")
	}
}
func TestR6UnreadableIsUnableNotPass(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	f.r.missing[f.c.Commit] = true
	if _, e := f.s.Read(f.ctx, cr.AgentActor(r.AgentID), r.ID, "openspec/specs/cap/context.md"); e == nil {
		t.Fatal("R6_003_MISSING_FALLBACK")
	}
	v := result(r)
	v.Verdict = "UNABLE"
	q, e := f.s.Submit(f.ctx, cr.AgentActor(r.AgentID), r.ID, v)
	if e != nil || q.Results[0].Verdict != "UNABLE" || !q.Completed.IsZero() || q.Unable.IsZero() {
		t.Fatal("R6_003_UNABLE", e)
	}
}
func TestR6ComparisonPolicyMaterialExemptionsForcePushAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, dir string
		stale                 bool
	}{
		{"requirement", "specs/cap/spec.md", "changed requirement", "", true}, {"scenario", "specs/cap/spec.md", "changed scenario", "", true}, {"scope", "proposal.md", "changed scope", "", true}, {"task", "tasks.md", "- [ ] T-01 changed", "", true},
		{"unrelated", "", "", "", false}, {"checkbox", "tasks.md", "- [x] T-01 task\n## 交接记录\nold\n", "", false}, {"handoff", "tasks.md", "- [ ] T-01 task\n## 交接记录\nnew\n", "", false}, {"archive", "", "", "openspec/changes/archive/2026-11-01-example", false}, {"exemption-plus-change", "tasks.md", "- [x] T-01 changed\n## 交接记录\nnew\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			r := f.create(t, "agentA", "create-operation-001")
			f.read(t, r)
			next := f.c
			next.Commit = strings.Repeat("b", 40)
			if tc.dir != "" {
				next.ChangeDir = tc.dir
			}
			files := map[string]string{}
			for p, v := range f.r.files[f.c.Commit] {
				files[p] = v
			}
			if tc.path != "" {
				files[tc.path] = tc.body
			}
			f.r.files[next.Commit] = files
			q, e := f.s.Compare(f.ctx, "owner", r.ID, next)
			if e != nil {
				t.Fatal(e)
			}
			a := q.Comparisons[0].Assessment
			if (a.Status == sc.Stale) != tc.stale || a.PolicyVersion != "A-conservative-exemptions-v2" || q.Coordinate != r.Coordinate {
				t.Fatal("R6_006_POLICY", a)
			}
			if tc.stale {
				if len(q.Alerts) != 1 {
					t.Fatal("R6_006_NO_REVIEW_PROMPT")
				}
				sent := 0
				for i := 0; i < 2; i++ {
					if e = f.s.DispatchAlert(f.ctx, r.ID, func(_ cr.Record, c cr.Coordinate) error {
						if c != next {
							t.Fatal("R6_006_PROMPT_WRONG_C")
						}
						sent++
						return nil
					}); e != nil {
						t.Fatal(e)
					}
				}
				if sent != 1 {
					t.Fatal("R6_006_DUP_PROMPT")
				}
			}
			q, e = f.s.Submit(f.ctx, cr.AgentActor(r.AgentID), r.ID, result(r))
			if e != nil || q.Results[0].Commit != f.c.Commit || q.Coordinate.Commit == next.Commit {
				t.Fatal("R6_007_LATE_RESULT_MIGRATED", e)
			}
			f.r.missing[f.c.Commit] = true
			q, e = f.s.Compare(f.ctx, "owner", r.ID, next)
			if e != nil || q.Comparisons[len(q.Comparisons)-1].Assessment.Status != sc.Unknown {
				t.Fatal("R6_007_UNREADABLE_GREEN", e)
			}
			next.Repository = "2"
			if _, e = f.s.Compare(f.ctx, "owner", r.ID, next); e == nil {
				t.Fatal("R6_007_CROSS_REPO")
			}
		})
	}
}
func TestR6NoticeBudgetsPersistenceSpacingAndFailures(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	start := f.now
	sent := 0
	send := func(q cr.Record, n int, issuer, subject string) (string, error) {
		sent++
		if issuer != "bot" || subject != "reviewer" {
			t.Fatal("R6_005_WRONG_CONTACT")
		}
		if n == 2 {
			return "", errors.New("transport uncertain")
		}
		return "receipt", nil
	}
	for _, minute := range []int{0, 29, 30, 30, 89, 90, 90, 149, 150, 210, 500} {
		f.now = start.Add(time.Duration(minute) * time.Minute)
		_ = f.s.Remind(f.ctx, r.ID, send)
		f.s = &cr.Service{Store: f.db, Access: f.a, Reader: f.r, Now: func() time.Time { return f.now }}
	}
	q, _ := f.s.Get(f.ctx, "owner", r.ID)
	if sent != 3 || len(q.Notices) != 3 || q.Notices[1].State != "unknown" || q.Notices[0].State != "sent" || !q.Opened.IsZero() || !q.Completed.IsZero() {
		t.Fatal("R6_005_BUDGET_OR_ACK", sent, q.Notices)
	}
	f2 := setup(t)
	r2 := f2.create(t, "agentA", "create-operation-002")
	f2.now = f2.now.Add(10 * time.Hour)
	count := 0
	send2 := func(cr.Record, int, string, string) (string, error) { count++; return "ok", nil }
	for i := 0; i < 10; i++ {
		_ = f2.s.Remind(f2.ctx, r2.ID, send2)
	}
	if count != 1 {
		t.Fatal("R6_005_RECOVERY_BURST", count)
	}
}
func TestR6OpenStopsOnlyOwnUnreadAndEndedStates(t *testing.T) {
	for _, state := range []string{"open", "completed", "cancelled", "superseded"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			r := f.create(t, "agentA", "create-operation-001")
			a := cr.AgentActor(r.AgentID)
			if _, e := f.s.Open(f.ctx, "owner", r.ID); e == nil {
				t.Fatal("R6_005_OTHER_OPEN")
			}
			if q, e := f.s.Get(f.ctx, a, r.ID); e != nil || !q.Opened.IsZero() {
				t.Fatal("R6_005_PREFETCH_OPEN")
			}
			switch state {
			case "open":
				if _, e := f.s.Open(f.ctx, a, r.ID); e != nil {
					t.Fatal(e)
				}
			case "completed":
				f.read(t, r)
				if _, e := f.s.Submit(f.ctx, a, r.ID, result(r)); e != nil {
					t.Fatal(e)
				}
			case "cancelled":
				if _, e := f.s.Cancel(f.ctx, "owner", r.ID); e != nil {
					t.Fatal(e)
				}
			case "superseded":
				if _, e := f.db.MutateCoordinateReview(f.ctx, r.ID, func(q *cr.Record) error { q.Superseded = f.now; return nil }); e != nil {
					t.Fatal(e)
				}
			}
			f.now = f.now.Add(2 * time.Hour)
			for i := 0; i < 3; i++ {
				if e := f.s.Remind(f.ctx, r.ID, func(cr.Record, int, string, string) (string, error) {
					t.Fatal("R6_005_ENDED_STILL_SENT")
					return "", nil
				}); e != nil {
					t.Fatal(e)
				}
			}
			q, _ := f.s.Get(f.ctx, "owner", r.ID)
			if state == "open" && !q.Completed.IsZero() {
				t.Fatal("R6_005_OPEN_COMPLETED")
			}
		})
	}
}
func TestR6ConcurrentOpenAndSendLinearization(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	f.now = f.now.Add(time.Hour)
	entered, release, done := make(chan bool), make(chan bool), make(chan error, 1)
	go func() {
		done <- f.s.Remind(f.ctx, r.ID, func(cr.Record, int, string, string) (string, error) {
			entered <- true
			<-release
			return "receipt", nil
		})
	}()
	<-entered
	opened := make(chan error, 1)
	go func() { _, e := f.s.Open(f.ctx, cr.AgentActor(r.AgentID), r.ID); opened <- e }()
	select {
	case <-opened:
		t.Fatal("R6_005_OPEN_RACED_SEND")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-opened; e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Hour)
	_ = f.s.Remind(f.ctx, r.ID, func(cr.Record, int, string, string) (string, error) {
		t.Fatal("R6_005_SEND_AFTER_OPEN")
		return "", nil
	})
}
func TestR6DurableEventOwnershipAndIndependentR9(t *testing.T) {
	f := setup(t)
	event := "shared-platform-event-001"
	if e := f.db.ClaimNotificationEvent(f.ctx, event, "r9", "pm-decision-001"); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.Create(f.ctx, "owner", cr.Create{Coordinate: f.c, AgentID: "agentA", Operation: "create-operation-001", EventID: event})
	if !errors.Is(e, cr.ErrConflict) {
		t.Fatal("R6_005_DOUBLE_OWNER", e)
	}
	r := f.create(t, "agentA", "create-operation-002")
	if e = f.db.ClaimNotificationEvent(f.ctx, r.EventID, "r9", "pm-decision-002"); !errors.Is(e, cr.ErrConflict) {
		t.Fatal("R6_005_R9_TAKES_R6")
	}
	if e = f.db.ClaimNotificationEvent(f.ctx, "independent-pm-event", "r9", "pm-decision-003"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Open(f.ctx, cr.AgentActor(r.AgentID), r.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.db.ClaimNotificationEvent(f.ctx, event, "r9", "pm-decision-001"); e != nil {
		t.Fatal("R6_005_OPEN_REPLACES_PM_ANSWER")
	}
}
func TestR6ConcurrentResultsAndImmutableHistory(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	f.read(t, r)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := f.s.Submit(f.ctx, cr.AgentActor(r.AgentID), r.ID, result(r)); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	q, _ := f.s.Get(f.ctx, "owner", r.ID)
	if len(q.Results) != 1 {
		t.Fatal("R6_004_CONCURRENT_DUPLICATE")
	}
	if _, e := f.db.MutateCoordinateReview(f.ctx, r.ID, func(q *cr.Record) error { q.Results[0].Verdict = "FAIL"; return nil }); e == nil {
		t.Fatal("R6_004_STORE_HISTORY_MUTABLE")
	}
}

func TestR6TrackRechecksHistoryWithoutUnboundedDuplicateFacts(t *testing.T) {
	f := setup(t)
	q, e := f.s.Create(f.ctx, "owner", cr.Create{Coordinate: f.c, AgentID: "agentA", Operation: "create-operation-001", TrackRef: "main"})
	if e != nil {
		t.Fatal(e)
	}
	f.r.head = strings.Repeat("b", 40)
	f.r.files[f.r.head] = f.r.files[f.c.Commit]
	if e = f.s.Track(f.ctx, q.ID); e != nil {
		t.Fatal(e)
	}
	first, _ := f.s.Get(f.ctx, "owner", q.ID)
	if first.Comparisons[0].Assessment.Status != sc.Status("no_material_change") {
		t.Fatal("R6_007_TRACK_BASELINE")
	}
	f.now = f.now.Add(2 * time.Minute)
	if e = f.s.Track(f.ctx, q.ID); e != nil {
		t.Fatal(e)
	}
	got, _ := f.s.Get(f.ctx, "owner", q.ID)
	if len(got.Comparisons) != 1 {
		t.Fatal("R6_007_DUPLICATE_FACTS")
	}
	f.r.missing[f.c.Commit] = true
	f.now = f.now.Add(2 * time.Minute)
	if e = f.s.Track(f.ctx, q.ID); e != nil {
		t.Fatal(e)
	}
	got, _ = f.s.Get(f.ctx, "owner", q.ID)
	if got.Comparisons[len(got.Comparisons)-1].Assessment.Status != sc.Unknown {
		t.Fatal("R6_007_HISTORY_CACHE_GREEN")
	}
	f.r.head = ""
	if e = f.s.Track(f.ctx, q.ID); e != nil {
		t.Fatal(e)
	}
	got, _ = f.s.Get(f.ctx, "owner", q.ID)
	if got.Comparisons[len(got.Comparisons)-1].Assessment.Reason != "ref_unreadable" {
		t.Fatal("R6_007_REF_FAILURE_GREEN")
	}
}

func TestR6AcrossServicesOpenAndSendUseSameDatabaseFence(t *testing.T) {
	f := setup(t)
	r := f.create(t, "agentA", "create-operation-001")
	f.now = f.now.Add(time.Hour)
	other := &cr.Service{Store: f.db, Access: f.a, Reader: f.r, Now: f.s.Now}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- f.s.Remind(f.ctx, r.ID, func(cr.Record, int, string, string) (string, error) { close(entered); <-release; return "receipt", nil })
	}()
	<-entered
	opened := make(chan error, 1)
	go func() { _, e := other.Open(f.ctx, cr.AgentActor(r.AgentID), r.ID); opened <- e }()
	premature := false
	select {
	case <-opened:
		premature = true
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if premature {
		t.Fatal("R6_005_CROSS_SERVICE_OPEN_PASSED_SEND")
	}
	if e := <-opened; e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Hour)
	if e := f.s.Remind(f.ctx, r.ID, func(cr.Record, int, string, string) (string, error) {
		t.Fatal("R6_005_CROSS_SERVICE_SEND_AFTER_OPEN")
		return "", nil
	}); e != nil {
		t.Fatal(e)
	}
}
