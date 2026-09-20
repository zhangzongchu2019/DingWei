package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestConsolePlatformRoutesAreAssembledWithServerGuards(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GET /api/console/platform":                         "HandleConsolePlatform",
		"POST /api/console/platform/submissions":            "HandleConsolePlatformSubmit",
		"POST /api/console/platform/items":                  "HandleConsolePlatformAdd",
		"PATCH /api/console/platform/items/{itemID}":        "HandleConsolePlatformEdit",
		"DELETE /api/console/platform/items/{itemID}":       "HandleConsolePlatformDelete",
		"POST /api/console/platform/items/{itemID}/approve": "HandleConsolePlatformApprove",
		"GET /console/specs":                                "HandleConsoleSpecsPage",
		"GET /api/console/specs":                            "HandleConsoleSpecs",
		"POST /api/console/specs":                           "HandleConsoleSpecs",
		"GET /api/console/specs/{specID}":                   "HandleConsoleSpec",
		"PATCH /api/console/specs/{specID}":                 "HandleConsoleSpec",
		"POST /api/console/specs/{specID}/submit":           "HandleConsoleSpecSubmit",
		"POST /api/console/specs/{specID}/reviews":          "HandleConsoleSpecReview",
		"POST /api/console/specs/{specID}/claim":            "HandleConsoleSpecClaim",
		"POST /api/console/specs/{specID}/archive":          "HandleConsoleSpecArchive",
		"GET /api/console/task-proposals/ready":             "HandleConsoleTaskProposalsReady",
	}
	seen := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		pattern, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		handler, needed := want[pattern]
		if !needed {
			return true
		}
		hasHandler, hasPrincipalGuard, hasRoleGuard := false, false, false
		ast.Inspect(call.Args[1], func(inner ast.Node) bool {
			selector, ok := inner.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case handler:
				hasHandler = true
			case "RequireConsolePrincipal":
				hasPrincipalGuard = true
			case "RequireConsolePlatformAction":
				hasRoleGuard = true
			}
			return true
		})
		if !hasHandler || !hasPrincipalGuard || !hasRoleGuard {
			t.Fatalf("route %s handler=%t principal_guard=%t role_guard=%t", pattern, hasHandler, hasPrincipalGuard, hasRoleGuard)
		}
		seen[pattern] = true
		return true
	})
	for pattern := range want {
		if !seen[pattern] {
			t.Errorf("console platform route not assembled: %s", pattern)
		}
	}
}
