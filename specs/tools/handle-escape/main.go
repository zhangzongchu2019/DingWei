// handle-escape：用 Go AST 判「裸库句柄是否逃出包外」（SpecDoc §22.2c #1 后半句）。
//
// 🔴 立此工具的直接原因（甲域 sol CR v2 #5）：
//
//	上一版判据二是【逐行正则】，导出函数签名一换行就绕过去了 ——
//	实测 `func (b *B) DB(\n) *sql.DB` 报 escape=0 且打印「两条判据均恒零 ✓ 达标」。
//	SpecDoc 写的是「**任何**函数不返回它」，而逐行正则的作用域远小于这句话。
//	⇒ 这是「用局部证据冒充全称结论」在【实现手段】上的又一次：正则能看的只有一行。
//
// 覆盖：导出函数/方法的返回值 · 导出字段（含匿名嵌入）· 接口方法返回值 ·
//
//	指向句柄类型的本地类型别名/定义（`type DB = *sql.DB`）· import alias（按导入路径解析，不按包名字面）。
//
// ⚠️ 已知不覆盖（写明而非假装闭合，E.4 #58）：
//   - 跨包类型别名（本工具逐包做语法分析，不加载依赖包的类型信息）；
//   - 经 interface{} / 反射 / unsafe 传出的句柄；
//   - 通过导出的函数【入参】把句柄交出去（§22.2c #1 只写「不返回」，此处不擅自扩严）。
//     要覆盖前两项须上 go/types 并加载整个包图，属另一件事。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// 句柄类型：按【导入路径】判定，不按包名字面 —— 否则 import alias 即可绕。
var handles = map[string]map[string]bool{
	"database/sql":                    {"DB": true, "Tx": true, "Conn": true},
	"github.com/jackc/pgx/v5":         {"Conn": true, "Tx": true},
	"github.com/jackc/pgx/v5/pgxpool": {"Pool": true, "Conn": true},
	"github.com/jmoiron/sqlx":         {"DB": true, "Tx": true, "Conn": true},
}

type finding struct {
	file string
	line int
	kind string
	name string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	var files []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(2)
	}
	sort.Strings(files)

	fset := token.NewFileSet()
	var out []finding
	for _, f := range files {
		af, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			// 🔴 解析失败是【闸坏了】，不是「这个文件本次不检」。
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, perr)
			os.Exit(2)
		}
		rel, _ := filepath.Rel(root, f)
		out = append(out, scan(fset, af, rel)...)
	}
	for _, x := range out {
		fmt.Printf("%s:%d:%s %s\n", x.file, x.line, x.kind, x.name)
	}
}

func scan(fset *token.FileSet, af *ast.File, rel string) []finding {
	// 1) import alias -> 导入路径
	alias := map[string]string{}
	for _, im := range af.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		alias[name] = path
	}
	isHandleSel := func(e ast.Expr) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		if !ok {
			return false
		}
		set, ok := handles[alias[id.Name]]
		return ok && set[s.Sel.Name]
	}
	// 2) 本地别名/定义：type DB = *sql.DB / type myDB *sql.DB
	local := map[string]bool{}
	// 迭代至不动点，不用固定遍数：Go 同包类型可前向引用，别名链的声明顺序任意，
	// 固定 N 遍只覆盖「顺序声明且链长 <= N」的链，逆序声明的三级链会漏
	// （乙域 terra SA-11）。local 只增不减、上界为本文件 TypeSpec 数，故必然收敛。
	for {
		before := len(local)
		ast.Inspect(af, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if isHandle(ts.Type, isHandleSel, local) {
				local[ts.Name.Name] = true
			}
			return true
		})
		if len(local) == before {
			break
		}
	}
	handle := func(e ast.Expr) bool { return isHandle(e, isHandleSel, local) }

	var res []finding
	pos := func(n ast.Node) int { return fset.Position(n.Pos()).Line }

	ast.Inspect(af, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() || d.Type.Results == nil {
				return true
			}
			for _, r := range d.Type.Results.List {
				if handle(r.Type) {
					res = append(res, finding{rel, pos(d), "func-returns-handle", d.Name.Name})
					break
				}
			}
		case *ast.StructType:
			for _, f := range d.Fields.List {
				if !handle(f.Type) {
					continue
				}
				if len(f.Names) == 0 { // 匿名嵌入：字段名即类型名，句柄类型均导出 ⇒ 可从包外访问
					res = append(res, finding{rel, pos(f), "embedded-handle-field", "<embedded>"})
					continue
				}
				for _, nm := range f.Names {
					if nm.IsExported() {
						res = append(res, finding{rel, pos(f), "exported-handle-field", nm.Name})
					}
				}
			}
		case *ast.InterfaceType:
			for _, m := range d.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || ft.Results == nil {
					continue
				}
				for _, nm := range m.Names {
					if !nm.IsExported() {
						continue
					}
					for _, r := range ft.Results.List {
						if handle(r.Type) {
							res = append(res, finding{rel, pos(m), "iface-method-returns-handle", nm.Name})
							break
						}
					}
				}
			}
		}
		return true
	})
	return res
}

func isHandle(e ast.Expr, sel func(ast.Expr) bool, local map[string]bool) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return isHandle(t.X, sel, local)
	case *ast.SelectorExpr:
		return sel(t)
	case *ast.Ident:
		return local[t.Name]
	}
	return false
}
