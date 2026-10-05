package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"testing"
)

// TestLifecycleRecoveryIsWiredAtHostedStartup guards the startup assembly,
// not only Lifecycle.RecoverJobs in isolation.  A persisted queued/running job
// must be reconciled before hosted routes start serving requests.
func TestLifecycleRecoveryIsWiredAtHostedStartup(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "newplat_hosted.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "RecoverJobs" {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if ok && id.Name == "life" && len(call.Args) == 1 {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("wireHosted must call life.RecoverJobs before serving hosted lifecycle requests")
	}
}

// TestRootTasksRouteIsRegistered guards the renderer's real polling path.
// HandleViewTasks has handler-level tests, but those cannot detect a missing
// ServeMux registration (the production symptom is then a 404 every 5s).
func TestRootTasksRouteIsRegistered(t *testing.T) {
	hub := newplatHub(t)
	rec := &routeRecorder{mux: http.NewServeMux()}
	registerConsoleRoutes(rec, hub, true)
	for _, pattern := range rec.patterns {
		if pattern == "GET /tasks" {
			return
		}
	}
	t.Fatal("GET /tasks is not registered in registerConsoleRoutes")
}
