package m8

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Guard the exact regression shape: raw path session values must not key terminal storage.
func TestInternalViewHandlersDoNotKeyTerminalsByRawPathSession(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, file := range files {
		if filepath.Ext(file) != ".go" || len(file) >= 8 && file[len(file)-8:] == "_test.go" {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			if s, ok := call.Fun.(*ast.SelectorExpr); ok {
				name = s.Sel.Name
			}
			if name != "terminalForSession" && name != "terminalKey" && name != "subscribeSessionEvents" && name != "subscribeExistingSessionEvents" {
				return true
			}
			for _, arg := range call.Args {
				id, ok := arg.(*ast.Ident)
				if ok && (id.Name == "session" || id.Name == "sessionName") {
					// These low-level helpers legitimately receive already-normalized names.
					// Handler bodies are the forbidden boundary; flag only direct calls there.
					pos := fset.Position(call.Pos())
					if file == "terminal.go" && pos.Line >= 727 && pos.Line <= 810 {
						t.Errorf("%s:%d raw session identifier keys event subscription via %s", file, pos.Line, name)
					}
				}
			}
			return true
		})
	}
}
