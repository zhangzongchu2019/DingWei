package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// S-SPEC-GATE-ERROR-DETAIL: the owner saw only "generation gate incomplete" and
// could not tell what to fix. The shape below is the live V2.3 revision 1
// (2026-10-30): AI criteria with generated_by/checked_at but no checked_by.
func TestSpecGateErrorNamesMissingField(t *testing.T) {
	live := `{"criteria":[{"id":"A1","text":"x","owner_ref":"T01","source":"ai","generated_by":"ag_x","checked_at":"2026-10-30T09:36:42Z","cases":["c1"]},` +
		`{"id":"A2","text":"y","owner_ref":"T02","source":"ai","generated_by":"ag_x","checked_at":"2026-10-30T09:36:42Z","cases":["c2"]}]}`
	got := specFinalAcceptanceProblem(live)
	if !strings.Contains(got, "核对者") || !strings.Contains(got, "A1、A2") {
		t.Fatalf("GATE_DETAIL_CHECKER_NOT_NAMED got=%q", got)
	}
	if validSpecFinalAcceptance(live) {
		t.Fatal("GATE_DETAIL_LIVE_SHAPE_ACCEPTED")
	}
	fixed := strings.ReplaceAll(live, `"generated_by":"ag_x"`, `"generated_by":"ag_x","checked_by":"zhangzongchu"`)
	if got := specFinalAcceptanceProblem(fixed); got != "" || !validSpecFinalAcceptance(fixed) {
		t.Fatalf("GATE_DETAIL_COMPLETE_BUNDLE_REJECTED got=%q", got)
	}
	cases := map[string]string{
		`{"criteria":[]}`: "没有验收标准",
		`[]`:              "没有验收标准",
		`{"criteria":[{"id":"A3","owner_ref":"","source":"ai","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}]}`:      "A3 缺「原文引用」",
		`{"criteria":[{"id":"A4","owner_ref":"T","source":"bot","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}]}`:    "A4 来源无效",
		`{"criteria":[{"id":"A5","owner_ref":"T","source":"ai","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":[]}]}`:        "A5 缺「证据」",
		`{"criteria":[{"id":"A6","owner_ref":"T","source":"ai","checked_by":"u","checked_at":"","cases":["c"]}]}`:                         "A6 缺「核对时刻」",
		`{"criteria":[{"id":"A7","owner_ref":"T","source":"ai","checked_by":"u","checked_at":"yesterday","cases":["c"]}]}`:                "A7 的「核对时刻」格式无效",
		`{"criteria":[{"id":"A8","owner_ref":"T","source":"ai","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":["c"," "]}]}`: "A8 有空的证据行",
	}
	for raw, want := range cases {
		if got := specFinalAcceptanceProblem(raw); !strings.Contains(got, want) {
			t.Errorf("GATE_DETAIL_WRONG_REASON raw=%s got=%q want %q", raw, got, want)
		}
	}
}

func TestSpecGateErrorTruncatesLongCheckerList(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"criteria":[`)
	for i := 1; i <= 24; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"A` + string(rune('0'+i/10)) + string(rune('0'+i%10)) + `","owner_ref":"T","source":"ai","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}`)
	}
	b.WriteString(`]}`)
	got := specFinalAcceptanceProblem(b.String())
	if !strings.Contains(got, "等共 24 条") || strings.Contains(got, "A11") {
		t.Fatalf("GATE_DETAIL_LIST_NOT_BOUNDED got=%q", got)
	}
}

func TestSpecGateErrorKeepsSentinelAndLegacyText(t *testing.T) {
	err := specGate("验收标准 A1 缺「核对者」")
	if !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatal("GATE_DETAIL_SENTINEL_LOST")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "审结未通过：验收标准 A1") || !strings.Contains(msg, ErrConsoleSpecGate.Error()) {
		t.Fatalf("GATE_DETAIL_MESSAGE got=%q", msg)
	}
}

func TestFinalizeReportsMissingGateReview(t *testing.T) {
	db, now, _, _ := r8Fixture(t, "review")
	err := db.FinalizeConsoleSpec(context.Background(), "s", "owner-a", now)
	if !errors.Is(err, ErrConsoleSpecGate) || !strings.HasPrefix(err.Error(), "审结未通过：") || !strings.Contains(err.Error(), "预审进行中，请等它返回") {
		t.Fatalf("GATE_DETAIL_FINALIZE_GENERIC err=%v", err)
	}
	t.Logf("finalize reason: %v", err)
}

// Live V2.3 shape: fresh Agent spec, AI criteria without checked_by, latest
// review timed out. Both problems must be named in one message.
func TestFinalizeReportsAllProblemsForLiveShape(t *testing.T) {
	db, now, q, _ := r8Fixture(t, "review")
	ctx := context.Background()
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='timed_out',failure='deadline expired' WHERE id=?`, q.ID); err != nil {
		t.Fatal(err)
	}
	live := `{"criteria":[{"id":"A1","text":"x","owner_ref":"T01","source":"ai","generated_by":"ag_x","checked_at":"2026-10-30T09:36:42Z","cases":["c1"]}]}`
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_revision SET acceptance_json=? WHERE spec_id='s' AND revision=1`, live); err != nil {
		t.Fatal(err)
	}
	err := db.FinalizeConsoleSpec(ctx, "s", "owner-a", now)
	if !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("GATE_DETAIL_LIVE_NOT_GATE err=%v", err)
	}
	for _, want := range []string{"A1", "核对者", "预审超时未返回", "重新发起预审"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("GATE_DETAIL_LIVE_MISSING %q err=%v", want, err)
		}
	}
	t.Logf("live-shape reason: %v", err)
}

// coder CR on e26e01fd: the parent ignored criterion ids, so a non-string id
// must not turn a finalizable bundle into "没有验收标准".
func TestSpecGateNonStringIDKeepsParentVerdict(t *testing.T) {
	ok := `{"criteria":[{"id":123,"owner_ref":"T","source":"ai","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}]}`
	if got := specFinalAcceptanceProblem(ok); got != "" || !validSpecFinalAcceptance(ok) {
		t.Fatalf("GATE_DETAIL_NUMERIC_ID_REJECTED got=%q", got)
	}
	for _, id := range []string{`123`, `null`, `{"x":1}`, `["A1"]`} {
		raw := `{"criteria":[{"id":` + id + `,"owner_ref":"T","source":"ai","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}]}`
		if got := specFinalAcceptanceProblem(raw); !strings.Contains(got, "第 1 条") {
			t.Errorf("GATE_DETAIL_NUMERIC_ID_LABEL id=%s got=%q", id, got)
		}
	}
}

// coder CR on e26e01fd: a 100k-rune id must not make the reason 100k long.
func TestSpecGateReasonLengthBounded(t *testing.T) {
	long := strings.Repeat("X", 100000)
	var b strings.Builder
	b.WriteString(`{"criteria":[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"` + long + `","owner_ref":"T","source":"ai","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}`)
	}
	b.WriteString(`]}`)
	got := specFinalAcceptanceProblem(b.String())
	if n := len([]rune(got)); n > 10*(maxSpecGateLabelRunes+2)+80 {
		t.Fatalf("GATE_DETAIL_LABEL_UNBOUNDED runes=%d", n)
	}
	single := specFinalAcceptanceProblem(`{"criteria":[{"id":"` + long + `","owner_ref":"","source":"ai","checked_by":"u","checked_at":"2026-10-30T09:36:42Z","cases":["c"]}]}`)
	if n := len([]rune(single)); n > maxSpecGateLabelRunes+40 {
		t.Fatalf("GATE_DETAIL_SINGLE_LABEL_UNBOUNDED runes=%d", n)
	}
	msg := specGate(long).Error()
	if n := len([]rune(msg)); n > maxSpecGateReasonRunes+len([]rune(ErrConsoleSpecGate.Error()))+10 {
		t.Fatalf("GATE_DETAIL_REASON_UNBOUNDED runes=%d", n)
	}
	if !errors.Is(specGate(long), ErrConsoleSpecGate) {
		t.Fatal("GATE_DETAIL_SENTINEL_LOST")
	}
}
