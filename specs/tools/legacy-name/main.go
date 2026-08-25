// 旧模型名计数器 —— 按【对象】分类逐项计数，不按行。
//
// 🔴 立此件的直接原因（甲域 sol，delta-b）：
//   前一版 ㉑ 用 `grep -c` 逐文件计数 —— 那数的是【含词行数】，不是【出现数】：
//     · 同一行出现两次只记 1 ⇒ **在已有行上再加一个旧名不涨数**；
//     · 注释 / 声明 / 测试混计 ⇒ **删一行注释可以抵消新增一行生产用法**。
//   ⚠️ 这正是本仓已归纳的「按行计数却断言出现数」，与 pending 债那条同族 ——
//     而 ㉑ 本身就是为防这类而立的，实现却又犯了一次。
//
// 判据对象（分三类，各自棘轮，**不得互相抵消**）：
//   prod    生产代码中的标识符出现（非 _test.go，AST Ident，天然不含注释）
//   test    测试代码中的标识符出现（_test.go）
//   comment 注释中的字面出现（词法扫描注释组）
//
// ⚠️ 能力边界：判「出现数」，判不了「改对了没有」——
//   把 TenantCtx 改名成 AccessCtx 而语义仍是单层，本件看不出来，那归人审。
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
		_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") {
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
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					for _, t := range targets {
						counts["comment"] += strings.Count(c.Text, t)
						perFile[p]["comment"] += strings.Count(c.Text, t)
					}
				}
			}
			return nil
		})
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
