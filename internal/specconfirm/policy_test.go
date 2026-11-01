package specconfirm_test

import (
	"context"
	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"strings"
	"testing"
)

func TestR7ApprovedTaskExemptions(t *testing.T) {
	cases := []struct {
		name, old, next string
		want            sc.Difference
	}{
		{"check", "- [ ] T-01 task\n", "- [x] T-01 task\n", sc.NoMaterial},
		{"uppercase-uncheck", "* [X] T-01 task\n", "* [ ] T-01 task\n", sc.NoMaterial},
		{"handoff", "# Tasks\n- [ ] T-01 task\n## 交接记录\nold\n", "# Tasks\n- [ ] T-01 task\n## 交接记录\nnew\n### detail\nmore\n", sc.NoMaterial},
		{"combined", "- [ ] T-01 task\n## 交接记录\nold\n", "- [x] T-01 task\n## 交接记录\nnew\n", sc.NoMaterial},
		{"task-text", "- [ ] T-01 task\n", "- [x] T-01 changed\n", sc.Material},
		{"task-added", "- [ ] T-01 task\n", "- [x] T-01 task\n- [ ] T-02 added\n", sc.Material},
		{"task-deleted", "- [ ] T-01 task\n- [ ] T-02 other\n", "- [x] T-01 task\n", sc.Material},
		{"new-section-after-handoff", "## 交接记录\nold\n## Tasks\n- [ ] T-01 task\n", "## 交接记录\nnew\n## Tasks\n- [x] T-01 changed\n", sc.Material},
		{"parent-after-handoff", "## 交接记录\nold\n# Scope\nA\n", "## 交接记录\nnew\n# Scope\nB\n", sc.Material},
		{"fenced-checkbox", "```md\n- [ ] task\n```\n", "```md\n- [x] task\n```\n", sc.Material},
		{"tilde-fenced-checkbox", "~~~\n- [ ] task\n~~~\n", "~~~\n- [x] task\n~~~\n", sc.Material},
		{"indented-checkbox", "    - [ ] task\n", "    - [x] task\n", sc.Material},
		{"fake-handoff-in-fence", "```\n## 交接记录\nA\n```\n", "```\n## 交接记录\nB\n```\n", sc.Material},
		{"nested-fence-in-handoff", "## 交接记录\n```\n## Tasks\nold\n```\n## Real\nA\n", "## 交接记录\n```\n## Tasks\nnew\n```\n## Real\nB\n", sc.Material},
		{"unclosed-fence", "- [ ] task\n```\n", "- [x] task\n```\n", sc.Material},
		{"duplicate-handoff", "## 交接记录\nA\n## 交接记录\nB\n", "## 交接记录\nC\n## 交接记录\nB\n", sc.Material},
		{"setext-boundary", "## 交接记录\nold\nTasks\n------\nA\n", "## 交接记录\nnew\nTasks\n------\nB\n", sc.Material},
		{"html-ambiguity", "<pre>\n- [ ] task\n</pre>\n", "<pre>\n- [x] task\n</pre>\n", sc.Material},
		{"inline-checkbox", "Example - [ ] task\n", "Example - [x] task\n", sc.Material},
		{"other-heading", "## Notes\nA\n", "## Notes\nB\n", sc.Material},
		{"format-not-exempt", "- [ ] task\n", "- [x]  task\n", sc.Material},
	}
	a, b := coord(), coord()
	b.Commit = strings.Repeat("b", 40)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (sc.ConservativePolicyA{}).Compare(snap(a, map[string]string{"tasks.md": tc.old}), snap(b, map[string]string{"tasks.md": tc.next}))
			if err != nil || got != tc.want {
				t.Fatalf("got %v %v want %v", got, err, tc.want)
			}
		})
	}
	// A task-looking block in another document is not exempt.
	for _, file := range []string{"proposal.md", "design.md", "specs/cap/spec.md", "nested/tasks.md"} {
		t.Run(file, func(t *testing.T) {
			got, err := (sc.ConservativePolicyA{}).Compare(snap(a, map[string]string{file: "- [ ] T-01\n## 交接记录\nold"}), snap(b, map[string]string{file: "- [x] T-01\n## 交接记录\nnew"}))
			if err != nil || got != sc.Material {
				t.Fatalf("non-task exemption: %v %v", got, err)
			}
		})
	}
}

func TestR7ArchiveExemptionPreservesContentAndIdentity(t *testing.T) {
	a, b := coord(), coord()
	b.Commit = strings.Repeat("b", 40)
	b.ChangeDir = "openspec/changes/archive/2026-11-01-example"
	files := map[string]string{"tasks.md": "- [ ] T-01 task\n## 交接记录\nold\n", "design.md": "constraint", "specs/cap/spec.md": "requirement"}
	old, next := snap(a, files), snap(b, files)
	p := sc.ConservativePolicyA{}
	if got, err := p.Compare(old, next); err != nil || got != sc.NoMaterial {
		t.Fatalf("identical move %v %v", got, err)
	}
	for _, file := range []string{"proposal.md", "tasks.md", "design.md", "specs/cap/spec.md"} {
		t.Run("modified-"+file, func(t *testing.T) {
			changed := snap(b, files)
			changed.Files[b.ChangeDir+"/"+file] = []byte("changed")
			got, err := p.Compare(old, changed)
			if err != nil || got != sc.Material {
				t.Fatalf("changed move %v %v", got, err)
			}
		})
	}
	t.Run("move-plus-checkbox", func(t *testing.T) {
		changed := snap(b, files)
		changed.Files[b.ChangeDir+"/tasks.md"] = []byte(strings.Replace(files["tasks.md"], "[ ]", "[x]", 1))
		if got, err := p.Compare(old, changed); err != nil || got != sc.Material {
			t.Fatalf("nonidentical move exempt %v %v", got, err)
		}
	})
	for _, kind := range []string{"add", "delete", "rename"} {
		t.Run(kind, func(t *testing.T) {
			changed := snap(b, files)
			if kind != "add" {
				delete(changed.Files, b.ChangeDir+"/design.md")
			}
			if kind != "delete" {
				changed.Files[b.ChangeDir+"/other.md"] = []byte("constraint")
			}
			if got, err := p.Compare(old, changed); err != nil || got != sc.Material {
				t.Fatalf("inventory change exempt %v %v", got, err)
			}
		})
	}
	for _, kind := range []string{"different-change", "different-repo", "different-provider", "invalid-date", "archive-date-rename", "unarchive"} {
		t.Run(kind, func(t *testing.T) {
			from, to := a, b
			switch kind {
			case "different-change":
				to.ChangeDir = "openspec/changes/archive/2026-11-01-other"
			case "different-repo":
				to.Repository = "other/repo"
			case "different-provider":
				to.Instance = "github.com"
			case "invalid-date":
				to.ChangeDir = "openspec/changes/archive/2026-02-31-example"
			case "archive-date-rename":
				from = b
				to.ChangeDir = "openspec/changes/archive/2026-11-02-example"
			case "unarchive":
				from, to = b, a
			}
			if got, err := p.Compare(snap(from, files), snap(to, files)); err == nil || got != sc.Indeterminate {
				t.Fatalf("unrelated pair allowed %v %v", got, err)
			}
		})
	}
}

func TestR7ArchiveAssessmentGatedAndNeverRebinds(t *testing.T) {
	a, b := coord(), coord()
	b.ChangeDir = "openspec/changes/archive/2026-11-01-example"
	b.Commit = strings.Repeat("b", 40)
	r := &reader{snapshots: map[string]sc.Snapshot{a.Commit: snap(a, nil), b.Commit: snap(b, nil)}}
	s := &sc.Service{Reader: r, Policy: sc.ConservativePolicyA{}}
	if got := s.Assess(context.Background(), a, b); got.Status != sc.Unknown || r.calls != 0 {
		t.Fatalf("default enabled: %+v", got)
	}
	s.PolicyApproved = true
	if got := s.Assess(context.Background(), a, b); got.Status != sc.Unchanged || got.FromCommit != a.Commit || got.ToCommit != b.Commit || got.PolicyVersion != "A-conservative-exemptions-v2" {
		t.Fatalf("move: %+v", got)
	}
	if a.ChangeDir != "openspec/changes/example" {
		t.Fatal("original coordinate rewritten")
	}
	r.snapshots[b.Commit] = snap(b, map[string]string{"tasks.md": "changed"})
	if got := s.Assess(context.Background(), a, b); got.Status != sc.Stale {
		t.Fatalf("changed archive: %+v", got)
	}
	r.wrong = true
	if got := s.Assess(context.Background(), a, b); got.Status != sc.Unknown {
		t.Fatalf("wrong source accepted: %+v", got)
	}
}
