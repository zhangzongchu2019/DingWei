package changeflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

func riskFixture() (RiskInput, Rules) {
	c := Coordinate{Provider: "github", ChangeID: "42", Coordinate: specconfirm.Coordinate{Instance: "github.com", Repository: "owner/repo", ChangeDir: "openspec/changes/demo", Commit: strings.Repeat("a", 40)}}
	r, e := ParseRules("", nil)
	if e != nil {
		panic(e)
	}
	return RiskInput{Coordinate: c, Base: strings.Repeat("b", 40), Head: c.Commit, Complete: true}, r
}
func patch(text string) string { return "@@ -0,0 +1 @@\n+" + text + "\n" }
func has(h []Hit, c, source string) bool {
	for _, v := range h {
		if v.Category == c && v.Source == source {
			return true
		}
	}
	return false
}
func TestR1_001_S01(t *testing.T) {
	cats := []string{"risk:funds", "risk:permission", "risk:data-delete", "risk:api-contract", "risk:sla", "risk:migration", "risk:privacy"}
	paths := []string{"billing/x.go", "internal/identity/x.go", "cleanup/x.go", "tools/sessionhelper/sessionhelper/x.py", "internal/scheduler/x.go", "db/migrations/x.sql", "internal/feishu/x.go"}
	words := []string{"金额", "权限", "TRUNCATE", "协议", "超时", "backfill", "隐私"}
	for i, c := range cats {
		for _, route := range []string{"label", "path", "added_line"} {
			t.Run(c+"/"+route, func(t *testing.T) {
				in, r := riskFixture()
				switch route {
				case "label":
					in.Labels = []string{c}
				case "path":
					in.Files = []FileDiff{{NewPath: paths[i], Complete: true, Binary: true}}
				case "added_line":
					in.Files = []FileDiff{{NewPath: "safe.txt", Complete: true, Patch: patch(words[i])}}
				}
				h, e := Evaluate(in, r)
				if e != nil || !has(h, c, route) {
					t.Fatalf("missing %s/%s: %+v %v", c, route, h, e)
				}
			})
		}
	}
	t.Run("union-and-rename", func(t *testing.T) {
		in, r := riskFixture()
		in.Labels = []string{"risk:privacy", "risk:privacy"}
		in.Files = []FileDiff{{OldPath: "internal/identity/a.go", NewPath: "safe.go", Complete: true, Patch: patch("TRUNCATE")}}
		h, e := Evaluate(in, r)
		if e != nil || !has(h, "risk:privacy", "label") || !has(h, "risk:permission", "path") || !has(h, "risk:data-delete", "added_line") {
			t.Fatalf("union %+v %v", h, e)
		}
	})
}
func TestR1_001_S02(t *testing.T) {
	paths := []string{"billingish/x.go", "internal/Identity/x.go", "cleanupish/x.go", "tools/sessionhelper/sessionhelperish/x.py", "internal/schedulerish/x.go", "schema.sqll", "internal/feishux/x.go"}
	for i, c := range []string{"risk:funds", "risk:permission", "risk:data-delete", "risk:api-contract", "risk:sla", "risk:migration", "risk:privacy"} {
		for _, route := range []string{"label", "path", "added_line"} {
			t.Run(c+"/"+route, func(t *testing.T) {
				in, r := riskFixture()
				switch route {
				case "label":
					in.Labels = []string{c + "-note"}
				case "path":
					in.Files = []FileDiff{{NewPath: paths[i], Complete: true, Binary: true}}
				case "added_line":
					in.Files = []FileDiff{{NewPath: "safe.txt", Complete: true, Patch: patch("unrelated near miss")}}
				}
				h, e := Evaluate(in, r)
				if e != nil || len(h) != 0 {
					t.Fatalf("false hit %+v %v", h, e)
				}
			})
		}
	}
}
func TestR1_002_S01(t *testing.T) {
	in, r := riskFixture()
	in.Labels = []string{"risk:funds"}
	in.Files = []FileDiff{{NewPath: "safe.txt", Complete: true, Patch: patch("price AUTHORIZATION private-source-not-for-comment")}}
	before, _ := json.Marshal(in)
	a, e := Evaluate(in, r)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		b, e := Evaluate(in, r)
		if e != nil || digest(a) != digest(b) {
			t.Fatal("not deterministic")
		}
	}
	after, _ := json.Marshal(in)
	h, _ := json.Marshal(a)
	if string(before) != string(after) || strings.Contains(string(h), "private-source") {
		t.Fatal("mutation/source disclosure")
	}
}
func TestR1_002_S02(t *testing.T) {
	t.Run("deleted-context-header", func(t *testing.T) {
		in, r := riskFixture()
		in.Files = []FileDiff{{NewPath: "safe.txt", Complete: true, Patch: "--- a/price\n+++ b/price\n@@ -1,2 +1 @@\n-price\n unchanged\n"}}
		h, e := Evaluate(in, r)
		if e != nil || len(h) != 0 {
			t.Fatalf("wrong additions %+v %v", h, e)
		}
	})
	for _, name := range []string{"incomplete", "file-incomplete", "truncated", "wrong-head", "rules-missing", "unsafe-path"} {
		t.Run(name, func(t *testing.T) {
			in, r := riskFixture()
			in.Files = []FileDiff{{NewPath: "safe.txt", Complete: true, Patch: patch("price")}}
			switch name {
			case "incomplete":
				in.Complete = false
			case "file-incomplete":
				in.Files[0].Complete = false
			case "truncated":
				in.Files[0].Patch = "@@ -0,0 +1,2 @@\n+price\n"
			case "wrong-head":
				in.Head = strings.Repeat("c", 40)
			case "rules-missing":
				r.Version = ""
			case "unsafe-path":
				in.Files[0].NewPath = "../escape"
			}
			if _, e := Evaluate(in, r); e == nil {
				t.Fatal("unknown accepted")
			}
		})
	}
}
func TestRiskRulesSchemaAndIdentity(t *testing.T) {
	raw := []byte("version: 1\nrules:\n  - id: repo-finance\n    category: risk:funds\n    paths: ['company/**/invoice?.go']\n    keywords: ['invoice']\n")
	r, e := ParseRules(GitBlob(raw), raw)
	if e != nil {
		t.Fatal(e)
	}
	in, _ := riskFixture()
	in.Files = []FileDiff{{NewPath: "company/a/invoice1.go", Complete: true, Binary: true}}
	hits, e := Evaluate(in, r)
	if e != nil || !has(hits, "risk:funds", "path") {
		t.Fatal(hits, e)
	}
	def, _ := ParseRules("", nil)
	if def.Version == r.Version {
		t.Fatal("rule identity not bound")
	}
	for _, bad := range []string{"version: 2\nrules: []", "version: 1\nunknown: true", "version: 1\nrules: &x [*x]", "version: 1\nrules:\n - id: x\n   category: risk:funds\n   paths: ['../x']"} {
		b := []byte(bad)
		if _, e := ParseRules(GitBlob(b), b); e == nil {
			t.Fatal("bad schema accepted", bad)
		}
	}
	if _, e := ParseRules(strings.Repeat("a", 40), raw); e == nil {
		t.Fatal("wrong rule blob")
	}
}

func TestRiskRuleVersionBindsEntries(t *testing.T) {
	in, r := riskFixture()
	r.Entries[0].Keywords = append(r.Entries[0].Keywords, "injected")
	if _, e := Evaluate(in, r); e == nil {
		t.Fatal("mutable rule entries retained old version")
	}
	if _, e := ParseRules(strings.Repeat("a", 40), nil); e == nil {
		t.Fatal("missing repository rule content accepted")
	}
}
