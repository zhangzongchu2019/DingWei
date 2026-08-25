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
// 🔴 二轮（terra SA-12b）：一轮只把 `cur.Recv == nil` 的 FuncDecl 注册为债源、调用统计只收
//
//	ast.Ident —— 于是 `func (x *X) btx() repo.TenantCtx` 这种**方法债源**及其 `x.btx()` 调用
//	一处都不计。断言写的是「任何函数 / 全部调用点」，实现只覆盖了普通函数，
//	**换成方法名就能把债从计数里拿掉**。与一轮那条「按名字判」是同一个病的第二种形状：
//	断言的作用域大于实现的覆盖面。本轮把方法债源与 SelectorExpr 调用一并纳入。
//
// ⚠️ 保证范围（不夸大，与判据二同款免责）：
//
//	· 只解析【同包内】直接返回构造器结果的 helper（**函数与方法都算**），
//	  以及直接调用构造器的点。
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
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const repoPkgPath = "internal/repo"

// reOwnerSP 是 ownerSP 的受控枚举形态：SP0–SP8，可带一位小写后缀（SP7a / SP7b）。
// 🔴 不接受任意字符串：一笔债「归谁还」若能写成动态值或自由文本，归责就没了（terra SA-12b）。
var reOwnerSP = regexp.MustCompile(`^SP[0-9]+[a-z]?$`)

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

	// ① 找债源：直接调用 repo.PendingTenantCtx 的地方；并记下【包裹它的函数或方法】。
	//    按导入路径识别，不按包名字面 —— alias 绕不过。
	//
	// 🔴 函数与方法**分开登记**：普通函数按 `f()` 调用（ast.Ident），
	//    方法按 `x.m()` 调用（ast.SelectorExpr）。一轮漏了后者。
	funcOrigins := map[string]string{}   // 函数名 -> domain
	methodOrigins := map[string]string{} // 方法名 -> domain（同名不同 receiver 且 domain 冲突 ⇒ "?"）
	methodRecv := map[string]string{}    // 方法名 -> receiver 类型名（仅用于报告）
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
	strLit := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
	// domainOf 同时校验 domain 与 ownerSP。
	// 🔴 terra SA-12b：一轮只校 domain 字面量，ownerSP 可以是任意表达式 ——
	//
	//	于是「这笔债归谁还」可以是动态值，归责就没了。ownerSP 须是受控枚举里的字面量。
	domainOf := func(c *ast.CallExpr) string {
		if len(c.Args) < 2 {
			return "?"
		}
		dom, ok := strLit(c.Args[0])
		if !ok {
			return "?"
		}
		owner, ok := strLit(c.Args[1])
		if !ok || !reOwnerSP.MatchString(owner) {
			return "?" // ownerSP 非受控字面量 ⇒ 判不出归责，同样 fail-closed
		}
		return dom
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
			if cur == nil {
				return true
			}
			name := cur.Name.Name
			if cur.Recv == nil {
				funcOrigins[name] = mergeDomain(funcOrigins[name], dom)
			} else {
				methodOrigins[name] = mergeDomain(methodOrigins[name], dom)
				methodRecv[name] = recvTypeName(cur)
			}
			return true
		})
	}

	if len(funcOrigins) == 0 && len(methodOrigins) == 0 {
		return out, nil
	}

	// ② 数债源的调用点。**按效果**：helper 叫什么、是函数还是方法，都不影响判据；
	//    它是不是债源由 ① 决定。
	for _, p := range ps {
		ast.Inspect(p.af, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := c.Fun.(type) {
			case *ast.Ident: // f()
				if dom, ok := funcOrigins[fn.Name]; ok {
					out = append(out, site{file: p.path, line: fset.Position(c.Pos()).Line, domain: dom, kind: "call"})
				}
			case *ast.SelectorExpr: // x.m() —— 方法债源，一轮整个漏掉
				if dom, ok := methodOrigins[fn.Sel.Name]; ok {
					out = append(out, site{file: p.path, line: fset.Position(c.Pos()).Line, domain: dom, kind: "call"})
				}
			}
			return true
		})
	}
	return out, nil
}

// mergeDomain 同名债源出现不同 domain ⇒ 判不出归属，退化为 "?" 交给闸 fail-closed。
// 🔴 不取其一、也不取先到的：两种取法都是替门方做了决定。
func mergeDomain(old, dom string) string {
	if old == "" {
		return dom
	}
	if old != dom {
		return "?"
	}
	return old
}

func recvTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
