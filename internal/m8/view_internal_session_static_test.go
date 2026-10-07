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
		// R-V2-0105：原按写死行号区间（terminal.go 727–810）圈定处理器，文件前部加代码即错位；
		// 改为按函数名圈定：只检查 HandleInternalView* 处理器函数体内的直接调用（边界语义不变）。
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || len(fn.Name.Name) < 18 || fn.Name.Name[:18] != "HandleInternalView" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				if s, ok := call.Fun.(*ast.SelectorExpr); ok {
					name = s.Sel.Name
				}
				switch name {
				case "terminalForSession", "terminalKey", "subscribeSessionEvents", "subscribeExistingSessionEvents", "subscribeExistingSessionEventsSeq":
				default:
					return true
				}
				for _, arg := range call.Args {
					id, ok := arg.(*ast.Ident)
					if ok && (id.Name == "session" || id.Name == "sessionName") {
						// These low-level helpers legitimately receive already-normalized names.
						// Handler bodies are the forbidden boundary; flag only direct calls there.
						pos := fset.Position(call.Pos())
						t.Errorf("%s:%d raw session identifier keys event subscription via %s in %s", file, pos.Line, name, fn.Name.Name)
					}
				}
				return true
			})
		}
	}
}
