// pending-debt 按【效果】清点「尚未租户化」的调用点（乙域 terra SA-12b）。
//
// 🔴 它取代的旧做法：闸里硬编码 `grep -E '\b(dtx|ftx)\(\)'`。
//
//	dtx / ftx 是 datad / fanout 各自的局部 helper 名，没有任何注册表约束。
//	SP7a 收编 board 时谁写一个 btx()，闸认不得它 —— 债凭空消失而计数不变，
//	主闸照打「持平 ✓」。那正是本仓已归纳的「按名字判非按效果判」，
//	而且发生在专门用来防「安全属性互相冒充」的那道闸自己身上。
//
// 本工具的判据是效果：**任何函数，只要它返回的是 repo.PendingTenantCtx 造出来的上下文，
// 它就是一个债源**；它的全部调用点都计入债。helper 叫什么与判据无关。
//
// ⚠️ 保证范围（不夸大，与判据二同款免责）：
//
//	· 只解析【同包内】直接返回构造器结果的 helper，以及直接调用构造器的点。
//	· 不含跨包间接（A 包 helper 调 B 包 helper）、不含把上下文存进结构体字段后再取出、
//	  不含 reflect / 接口动态派发。这些形态目前仓内不存在；出现了要扩这里，
//	  **而不是把断言写宽**。
//	· 语法错文件一律判【闸坏】(exit 2)，不当作「没有债」。
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

const repoPkgPath = "internal/repo"

type site struct {
	file   string
	line   int
	domain string
	kind   string // "origin"(构造点) | "call"(helper 调用点)
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	sites, err := scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pending-debt: "+err.Error())
		os.Exit(2)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	for _, s := range sites {
		fmt.Printf("%s:%d\t%s\t%s\n", s.file, s.line, s.domain, s.kind)
	}
}

// pkgFiles 把一个目录下的 .go 按包聚合（同目录即同包，本仓无 xxx_test 外部包混排的情形）。
func scan(root string) ([]site, error) {
	byDir := map[string][]string{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			// 仓储包本身是【定义处】，不是债；与旧闸的 `grep -v /repo/` 同口径。
			if filepath.Base(p) == "repo" && strings.Contains(filepath.ToSlash(p), "/internal/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		d := filepath.Dir(p)
		byDir[d] = append(byDir[d], p)
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []site
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, d := range dirs {
		s, err := scanPkg(byDir[d])
		if err != nil {
			return nil, err
		}
		out = append(out, s...)
	}
	return out, nil
}

func scanPkg(files []string) ([]site, error) {
	fset := token.NewFileSet()
	type parsed struct {
		path string
		af   *ast.File
		// repo 包在本文件里的名字（可能是 import alias）
		repoName string
	}
	var ps []parsed
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			// 🔴 语法错 = 闸坏，不是「本文件没有债」。
			return nil, fmt.Errorf("解析失败（判为闸坏，非通过）：%v", err)
		}
		name := ""
		for _, im := range af.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if !strings.HasSuffix(p, repoPkgPath) {
				continue
			}
			name = "repo"
			if im.Name != nil {
				name = im.Name.Name
			}
		}
		ps = append(ps, parsed{path: f, af: af, repoName: name})
	}

	// ① 找债源：直接调用 repo.PendingTenantCtx 的地方；并记下【包裹它的函数名】。
	//    按导入路径识别，不按包名字面 —— alias 绕不过。
	origins := map[string]string{} // helper 函数名 -> domain
	var out []site

	isCtor := func(repoName string, e ast.Expr) bool {
		if repoName == "" {
			return false
		}
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "PendingTenantCtx" {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == repoName
	}
	domainOf := func(c *ast.CallExpr) string {
		if len(c.Args) == 0 {
			return "?"
		}
		lit, ok := c.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "?" // 非字面量 ⇒ 判不出领域，交给闸 fail-closed
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil || v == "" {
			return "?"
		}
		return v
	}

	for _, p := range ps {
		var cur *ast.FuncDecl
		ast.Inspect(p.af, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok {
				cur = fd
				return true
			}
			c, ok := n.(*ast.CallExpr)
			if !ok || !isCtor(p.repoName, c.Fun) {
				return true
			}
			dom := domainOf(c)
			out = append(out, site{file: p.path, line: fset.Position(c.Pos()).Line, domain: dom, kind: "origin"})
			if cur != nil && cur.Recv == nil {
				origins[cur.Name.Name] = dom
			}
			return true
		})
	}

	if len(origins) == 0 {
		return out, nil
	}

	// ② 数债源 helper 的调用点。**按效果**：helper 叫什么无所谓，
	//    它是不是债源由 ① 决定。
	for _, p := range ps {
		ast.Inspect(p.af, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			dom, ok := origins[id.Name]
			if !ok {
				return true
			}
			out = append(out, site{file: p.path, line: fset.Position(c.Pos()).Line, domain: dom, kind: "call"})
			return true
		})
	}
	return out, nil
}
