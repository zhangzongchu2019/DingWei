package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func TestConsolePlatformRoutesAreAssembledWithServerGuards(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GET /api/console/platform":                                      "HandleConsolePlatform",
		"POST /api/console/platform/submissions":                         "HandleConsolePlatformSubmit",
		"POST /api/console/platform/items":                               "HandleConsolePlatformAdd",
		"PATCH /api/console/platform/items/{itemID}":                     "HandleConsolePlatformEdit",
		"DELETE /api/console/platform/items/{itemID}":                    "HandleConsolePlatformDelete",
		"POST /api/console/platform/items/{itemID}/approve":              "HandleConsolePlatformApprove",
		"GET /console/specs":                                             "HandleConsoleSpecsPage",
		"GET /api/console/specs":                                         "HandleConsoleSpecs",
		"POST /api/console/specs":                                        "HandleConsoleSpecs",
		"GET /api/console/specs/{specID}":                                "HandleConsoleSpec",
		"PATCH /api/console/specs/{specID}":                              "HandleConsoleSpec",
		"POST /api/console/specs/{specID}/workflow/{action}":             "HandleConsoleSpecWorkflow",
		"POST /api/console/specs/{specID}/tasks":                         "HandleConsoleSpecTasks",
		"GET /api/console/specs/{specID}/presplit":                       "HandleConsoleSpecPresplit",
		"POST /api/console/specs/{specID}/presplit":                      "HandleConsoleSpecPresplit",
		"POST /api/console/specs/{specID}/archive":                       "HandleConsoleSpecArchive",
		"GET /api/console/task-proposals/ready":                          "HandleConsoleTaskProposalsReady",
		"PATCH /api/console/task-proposals/{proposalID}/plan":            "HandleConsoleTaskProposalPlan",
		"DELETE /api/console/task-proposals/{proposalID}":                "HandleConsoleSpecTaskDelete",
		"POST /api/console/task-driver/specs/{specID}/start":             "HandleConsoleTaskDriverStart",
		"POST /api/console/task-driver/specs/{specID}/{revision}/review": "HandleConsoleTaskDriverReview",
		"POST /api/console/task-driver/specs/{specID}/{revision}/deploy": "HandleConsoleTaskDriverDeploy",
		"GET /api/console/task-driver/specs/{specID}/{revision}":         "HandleConsoleTaskDriverStatus",
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

func agentCapabilityPageID(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.SelectorExpr:
		pkg, ok := value.X.(*ast.Ident)
		if !ok || pkg.Name != "m8" {
			return "", false
		}
		switch value.Sel.Name {
		case "ConsoleAgentCapabilitySpecsNew":
			return m8.ConsoleAgentCapabilitySpecsNew, true
		case "ConsoleAgentCapabilityTasks":
			return m8.ConsoleAgentCapabilityTasks, true
		}
	case *ast.CallExpr:
		selector, ok := value.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		pkg, pkgOK := selector.X.(*ast.Ident)
		if !pkgOK || pkg.Name != "m8" || selector.Sel.Name != "ConsoleAgentSpecCapabilityPage" || len(value.Args) != 1 {
			return "", false
		}
		pathCall, ok := value.Args[0].(*ast.CallExpr)
		if !ok || len(pathCall.Args) != 1 {
			return "", false
		}
		pathSelector, ok := pathCall.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		receiver, receiverOK := pathSelector.X.(*ast.Ident)
		if !receiverOK || receiver.Name != "r" || pathSelector.Sel.Name != "PathValue" {
			return "", false
		}
		literal, ok := pathCall.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return "", false
		}
		pathKey, err := strconv.Unquote(literal.Value)
		if err != nil || pathKey == "" {
			return "", false
		}
		return m8.ConsoleAgentSpecCapabilityPage("contract-" + pathKey), true
	}
	return "", false
}

func TestEveryAgentCapabilityRoutePageIsAcceptedBySigningRegistry(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "RequireConsoleSpecAgentCapability" || len(call.Args) < 1 {
			return true
		}
		count++
		fn, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			t.Errorf("Agent capability route at %s has non-function page id", fset.Position(call.Pos()))
			return true
		}
		var pageID string
		var resolved bool
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			ret, ok := inner.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			pageID, resolved = agentCapabilityPageID(ret.Results[0])
			return false
		})
		if !resolved {
			t.Errorf("Agent capability route at %s uses an unregistered page-id expression", fset.Position(call.Pos()))
			return true
		}
		if !m8.ConsoleAgentCapabilityPageAllowed(pageID) {
			t.Errorf("Agent capability route at %s returns page_id=%q rejected by route registry", fset.Position(call.Pos()), pageID)
		}
		return true
	})
	if count < 4 {
		t.Fatalf("Agent capability route scan matched %d, want at least current four routes", count)
	}
	// This hand-built node controls the evaluator + route registry only. The
	// count floor above independently guards the main.go scan.
	known, ok := agentCapabilityPageID(&ast.SelectorExpr{X: ast.NewIdent("m8"), Sel: ast.NewIdent("ConsoleAgentCapabilityTasks")})
	if !ok || !m8.ConsoleAgentCapabilityPageAllowed(known) {
		t.Fatal("registered Agent page evaluator positive control failed")
	}
	// Even a runtime-valid value is forbidden when the route shape itself is a
	// literal: consumers must derive ids from their route, never caller input.
	if _, ok := agentCapabilityPageID(&ast.BasicLit{Kind: token.STRING, Value: `"console-tasks"`}); ok {
		t.Fatal("literal Agent page-id expression escaped route derivation contract")
	}
}

func TestAgentTaskDriverRouteRequiresAgentCapability(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		pattern, _ := strconv.Unquote(lit.Value)
		if pattern != "GET /api/agent/console/task-driver/specs/{specID}/{revision}/tasks" {
			return true
		}
		hasGuard, hasHandler := false, false
		ast.Inspect(call.Args[1], func(inner ast.Node) bool {
			if sel, ok := inner.(*ast.SelectorExpr); ok {
				hasGuard = hasGuard || sel.Sel.Name == "RequireConsoleSpecAgentCapability"
				hasHandler = hasHandler || sel.Sel.Name == "HandleAgentConsoleTasks"
			}
			return true
		})
		if !hasGuard || !hasHandler {
			t.Fatalf("agent task route capability_guard=%t handler=%t", hasGuard, hasHandler)
		}
		found = true
		return true
	})
	if !found {
		t.Fatal("agent task driver route is not assembled")
	}
}
