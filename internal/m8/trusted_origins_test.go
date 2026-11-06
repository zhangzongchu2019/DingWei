package m8

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// This guard-site inventory makes accidental bypasses visible: every active
// category-甲 handler must call the shared policy at its decision point.
func TestTrustedOriginAllWriteGuardSitesUseSharedPolicy(t *testing.T) {
	want := map[string][]string{
		"newplat_view.go":                  {"newPlatformViewAuth", "newPlatformPrincipal"},
		"r9_pm_request.go":                 {"HandleR9PMRequest"},
		"task_dispatch_lock.go":            {"handleTaskDispatchLock"},
		"console_change_evidence.go":       {"HandleChangeEvidence"},
		"console_helper_updates.go":        {"helperConsoleActor"},
		"task_dispatch_batch.go":           {"handleTaskDispatchBatch"},
		"r9_pm_mapping.go":                 {"HandleR9PMResponsibility", "HandleR9PMConsoleResponse"},
		"coordinate_review.go":             {"coordinateMutation"},
		"console_openspec_confirmation.go": {"HandleOpenSpecConfirmations"},
		"console_openspec_read.go":         {"HandleOpenSpecBinding"},
		"console_mux.go":                   {"actor"},
		"terminal.go":                      {"HandleInternalViewEvents"},
	}
	fset := token.NewFileSet()
	for file, functions := range want {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join(".", file)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range functions {
				found := false
				for _, decl := range f.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Name.Name != name || fn.Body == nil {
						continue
					}
					ast.Inspect(fn.Body, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						selector, ok := call.Fun.(*ast.SelectorExpr)
						if ok && selector.Sel.Name == "originAllowed" {
							found = true
						}
						return true
					})
				}
				if !found {
					t.Errorf("%s must call the shared Hub.originAllowed decision", name)
				}
			}
		})
	}
}
