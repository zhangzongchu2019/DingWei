package m8

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These are the only production functions allowed to apply legacy short-name
// matching. Both first call resolveNewPlatformVisibleSession; the legacy
// comparisons are only the documented fallback for a non-full_name input.
var rawSessionComparisonWhitelist = map[string]string{
	"resolveNewPlatformFeishuSession": "Feishu short-name and prefix compatibility after the full_name gate",
	"resolveOwnerSessionByShort":      "Agent/AMR short-name and prefix compatibility after the full_name gate",
}

func TestNewPlatformUserVisibleNamesUseIdentityBoundary(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	fset := token.NewFileSet()
	required := map[string]bool{
		"resolveNewPlatformFeishuSession": false,
		"resolveOwnerSessionByShort":      false,
		"RouteEnvelope":                   false,
	}
	for _, name := range []string{"newplat_feishu_inbound.go", "hub.go"} {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			hasBoundary := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				called := ""
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					called = fun.Name
				case *ast.SelectorExpr:
					called = fun.Sel.Name
				}
				if called == "resolveNewPlatformVisibleSession" {
					hasBoundary = true
				}
				if called == "ownerSessionExactMatch" || called == "ownerSessionFuzzyMatch" {
					if _, allowed := rawSessionComparisonWhitelist[fn.Name.Name]; !allowed {
						t.Errorf("%s directly applies %s without a documented whitelist reason", fn.Name.Name, called)
					}
				}
				return true
			})
			if _, wanted := required[fn.Name.Name]; wanted && hasBoundary {
				required[fn.Name.Name] = true
			}
		}
	}
	for fn, found := range required {
		if !found {
			t.Errorf("%s no longer calls the shared full_name identity boundary", fn)
		}
	}
	for fn, reason := range rawSessionComparisonWhitelist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("raw comparison whitelist %s has no reason", fn)
		}
	}
}
