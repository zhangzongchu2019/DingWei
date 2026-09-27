package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
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

func TestEveryAgentCapabilityRouteUsesTheSigningRegistry(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for number, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, "RequireConsoleSpecAgentCapability") {
			continue
		}
		count++
		if !strings.Contains(line, "m8.ConsoleAgent") {
			t.Errorf("Agent capability route line %d bypasses signing registry: %s", number+1, strings.TrimSpace(line))
		}
	}
	if count < 4 {
		t.Fatalf("Agent capability route scan matched %d, want current four routes", count)
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
