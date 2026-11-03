package taskdispatch_test

import (
	"context"
	"errors"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
	"strings"
	"testing"
	"time"
)

func TestR10ServiceMonitorPolicySharedPollAndUnknown(t *testing.T) {
	for _, kind := range []string{"task_definition", "requirements", "proposal", "other_markdown", "scenarios", "scope", "checkbox", "handoff_section", "archive_identical", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			e := setup(t)
			ctx := context.Background()
			l := e.grant(t)
			e.r.OperationID = "grant_second_task_0001"
			e.r.Key.TaskID = "T-02"
			e.grant(t)
			switch kind {
			case "task_definition":
				e.change(c2, dir+"/tasks.md", "# Tasks\n- [ ] T-01 altered\n- [ ] T-02 other\n")
			case "requirements":
				e.change(c2, dir+"/specs/one/spec.md", "# Requirements\nchanged\n")
			case "proposal":
				e.change(c2, dir+"/proposal.md", "# Proposal\nchanged\n")
			case "other_markdown":
				e.change(c2, dir+"/context.md", "# Context\nnew contract\n")
			case "scenarios":
				e.change(c2, dir+"/specs/one/spec.md", "# Requirements\n#### Scenario: additional contract\n- WHEN new condition\n- THEN new behavior\n")
			case "scope":
				e.change(c2, dir+"/proposal.md", "# Proposal\n## Scope\nAdd a new API consumer\n")
			case "checkbox":
				e.change(c2, dir+"/tasks.md", strings.Replace(e.p.files[c1][dir+"/tasks.md"], "[ ]", "[x]", 1))
			case "handoff_section":
				e.change(c2, dir+"/tasks.md", strings.Replace(e.p.files[c1][dir+"/tasks.md"], "initial", "progress", 1))
			case "archive_identical":
				files := map[string]string{}
				for path, text := range e.p.files[c1] {
					files[strings.Replace(path, dir, "openspec/changes/archive/2026-11-02-example", 1)] = text
				}
				e.p.files[c2] = files
				e.p.head = c2
			case "unknown":
				e.p.head = c2
			}
			before := e.p.polls
			e.now = e.now.Add(time.Minute)
			if err := e.s.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if e.p.polls-before != 1 {
				t.Fatalf("R10_POLL_NOT_SHARED got=%d", e.p.polls-before)
			}
			current, _ := e.db.TaskDispatchLock(ctx, l.Key)
			if current.CommitSHA != c1 {
				t.Fatal("R10_AUTOMATIC_REBIND")
			}
			expected := "clean"
			if kind == "task_definition" || kind == "requirements" || kind == "proposal" || kind == "other_markdown" || kind == "scenarios" || kind == "scope" {
				expected = "needs_sync"
			}
			if kind == "unknown" {
				expected = "comparison_unknown"
			}
			if current.ComparisonState != expected {
				t.Fatalf("R10_POLICY_RESULT kind=%s got=%s want=%s", kind, current.ComparisonState, expected)
			}
			notices, _ := e.db.TaskDispatchNotices(ctx, "pending", 1000)
			changed := 0
			for _, n := range notices {
				if n.Type == "needs_sync" {
					changed++
					if n.CreatedAt.After(e.now.Add(5 * time.Second)) {
						t.Fatal("R10_NOTICE_TOO_LATE")
					}
				}
			}
			if expected == "needs_sync" && changed != 4 {
				t.Fatalf("R10_DRIFT_NOTICE_MISSING %d", changed)
			}
			if expected != "needs_sync" && changed != 0 {
				t.Fatal("R10_FALSE_CHANGED_NOTICE")
			}
		})
	}
}

func TestR10DefaultOffNoRepositoryAccess(t *testing.T) {
	e := setup(t)
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "true")
	e.s.Repo = nil
	e.s.Identity = nil
	e.s.Reader = nil
	if td.Enabled() {
		t.Fatal("R10_FLAG_NOT_EXACT_1")
	}
	if _, err := e.s.Execute(context.Background(), td.Actor{}, e.r); !errors.Is(err, td.ErrUnavailable) {
		t.Fatal("R10_DISABLED_EXECUTED", err)
	}
	if err := e.s.Tick(context.Background()); !errors.Is(err, td.ErrUnavailable) {
		t.Fatal("R10_DISABLED_MONITOR", err)
	}
}
