package m8

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestConsoleSpecsDoNotWriteTaskOrScheduleSystems(t *testing.T) {
	path := filepath.Join("console_specs.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"EnqueueControlTask": true, "UpsertSchedule": true, "AppendScheduleDoc": true,
		"PutTask": true, "UpdateProgress": true, "OwnerTransition": true,
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && forbidden[sel.Sel.Name] {
			t.Errorf("I/K boundary violated: console_specs.go calls %s", sel.Sel.Name)
		}
		return true
	})
}
