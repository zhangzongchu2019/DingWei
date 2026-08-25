// 旧模型名计数器 —— 按【对象】分类逐项计数，不按行。
//
// 🔴 立此件的直接原因（甲域 sol，delta-b）：
//
//	前一版 ㉑ 用 `grep -c` 逐文件计数 —— 那数的是【含词行数】，不是【出现数】：
//	  · 同一行出现两次只记 1 ⇒ **在已有行上再加一个旧名不涨数**；
//	  · 注释 / 声明 / 测试混计 ⇒ **删一行注释可以抵消新增一行生产用法**。
//	⚠️ 这正是本仓已归纳的「按行计数却断言出现数」，与 pending 债那条同族 ——
//	  而 ㉑ 本身就是为防这类而立的，实现却又犯了一次。
//
// 判据对象（分三类，各自棘轮，**不得互相抵消**）：
//
//	prod    生产代码中的标识符出现（非 _test.go，AST Ident，天然不含注释）
//	test    测试代码中的标识符出现（_test.go）
//	comment 注释中的字面出现（词法扫描注释组）
//
// ⚠️ 能力边界：判「出现数」，判不了「改对了没有」——
//
//	把 TenantCtx 改名成 AccessCtx 而语义仍是单层，本件看不出来，那归人审。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var targets = []string{"TenantCtx", "PendingTenantCtx", "NewTenantCtx"}

// identWords 按 Go 标识符规则切词：连续的 [A-Za-z0-9_] 为一个词。
// 只有整词等于目标才计 —— 子串匹配会让重叠名称互相串味（甲域 sol）。
func identWords(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		isIdent := r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if isIdent && start < 0 {
			start = i
		} else if !isIdent && start >= 0 {
			out = append(out, s[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func hit(name string) bool {
	for _, t := range targets {
		if name == t {
			return true
		}
	}
	// 复合标识符（如 tenantCtxKey）不计：判据对象是【受控符号集合】，不是子串
	return false
}

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"internal", "cmd"}
	}
	counts := map[string]int{"prod": 0, "test": 0, "comment": 0}
	perFile := map[string]map[string]int{}

	for _, root := range roots {
		// 🔴 甲域 sol：原实现 `_ = filepath.Walk(...)` 丢弃返回错误，
		//   且 callback 遇 err 也 `return nil` ⇒ **根目录不存在时计 0 并放行**。
		//   ⚠️ 这是本仓已归纳的「取不到就判空」在本件里的又一次：
		//     「走不到」被当成「走过了，没有」。**缺失与不适用必须分开。**
		if fi, err := os.Stat(root); err != nil {
			fmt.Fprintf(os.Stderr, "✗ 扫描根不可用 %s: %v —— 走不到就不能报数\n", root, err)
			os.Exit(2)
		} else if !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "✗ 扫描根不是目录 %s —— 走不到就不能报数\n", root)
			os.Exit(2)
		}
		if werr := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("遍历失败 %s: %w", p, err) // 🔴 向上传播，不吞
			}
			if fi.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, p, nil, parser.ParseComments)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "✗ 解析失败 %s: %v —— 解析不了就不能报数\n", p, perr)
				os.Exit(2)
			}
			kind := "prod"
			if strings.HasSuffix(p, "_test.go") {
				kind = "test"
			}
			if perFile[p] == nil {
				perFile[p] = map[string]int{}
			}
			// 标识符：AST 逐个 Ident，同一行多个各记一次
			ast.Inspect(f, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && hit(id.Name) {
					counts[kind]++
					perFile[p][kind]++
				}
				return true
			})
			// 注释：逐 comment 行内所有出现，全部计入
			// 🔴 甲域 sol：原实现用 strings.Count 逐目标做**子串**计数 ——
			//   `PendingTenantCtx` / `NewTenantCtx` 内含 `TenantCtx`，于是同一处被计两次，
			//   违反本件头注自称的「受控符号**精确**集合」。
			//   ⇒ 按 Go 标识符边界切词后**精确相等**匹配，一处只归一个目标。
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					for _, w := range identWords(c.Text) {
						if hit(w) {
							counts["comment"]++
							perFile[p]["comment"]++
						}
					}
				}
			}
			return nil
		}); werr != nil {
			fmt.Fprintf(os.Stderr, "✗ %v —— 走不到就不能报数\n", werr)
			os.Exit(2)
		}
	}

	fmt.Printf("prod=%d\ntest=%d\ncomment=%d\n", counts["prod"], counts["test"], counts["comment"])
	files := make([]string, 0, len(perFile))
	for k := range perFile {
		files = append(files, k)
	}
	sort.Strings(files)
	for _, p := range files {
		m := perFile[p]
		if m["prod"]+m["test"]+m["comment"] > 0 {
			fmt.Printf("  %s prod=%d test=%d comment=%d\n", p, m["prod"], m["test"], m["comment"])
		}
	}
}
