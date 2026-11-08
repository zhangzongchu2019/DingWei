// tenant-schema reports tenant columns declared by literal CREATE/ALTER TABLE DDL.
// It deliberately does not treat JSON fields or SQL query predicates as schema.
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

// Tokenize comments and SQL strings before identifiers so their contents cannot
// become columns. Quoted identifiers remain identifiers; SQL literals do not.
var tokens = regexp.MustCompile(`(?s)--[^\n]*(?:\n|$)|/\*.*?\*/|'(?:''|[^'])*'|"(?:""|[^"])*"|` + "`(?:``|[^`])*`" + `|\[[^\]]*\]|[A-Za-z_][A-Za-z_0-9]*|[(),.;]`)

func lex(s string) []string {
	var out []string
	for _, t := range tokens.FindAllString(s, -1) {
		if strings.HasPrefix(t, "--") || strings.HasPrefix(t, "/*") {
			continue
		}
		if strings.HasPrefix(t, "'") {
			out = append(out, "<literal>")
			continue
		}
		out = append(out, strings.ToLower(strings.Trim(t, "\"`[]")))
	}
	return out
}

type column struct{ table, name string }

func tenant(s string) bool {
	return s == "tenant" || s == "tenant_id" || s == "tenant_key" || s == "tenant_uuid"
}
func columns(s string) []column {
	ts := lex(s)
	var out []column
	for i := 0; i < len(ts); i++ {
		j := i
		alter := false
		if ts[j] == "create" {
			j++
			if j < len(ts) && (ts[j] == "temporary" || ts[j] == "temp" || ts[j] == "unlogged") {
				j++
			}
		} else if ts[j] == "alter" {
			alter = true
			j++
		} else {
			continue
		}
		if j >= len(ts) || ts[j] != "table" {
			continue
		}
		j++
		if j+2 < len(ts) && ts[j] == "if" && ts[j+1] == "not" && ts[j+2] == "exists" {
			j += 3
		}
		if j+1 < len(ts) && ts[j] == "if" && ts[j+1] == "exists" {
			j += 2
		}
		if j >= len(ts) {
			continue
		}
		table := ts[j]
		j++
		if j+1 < len(ts) && ts[j] == "." {
			table = ts[j+1]
			j += 2
		}
		if alter {
			// A statement may add more than one column. Only ADD [COLUMN] declares one.
			for ; j < len(ts) && ts[j] != ";"; j++ {
				if ts[j] != "add" {
					continue
				}
				j++
				if j < len(ts) && ts[j] == "column" {
					j++
				}
				if j+2 < len(ts) && ts[j] == "if" && ts[j+1] == "not" && ts[j+2] == "exists" {
					j += 3
				}
				if j < len(ts) && tenant(ts[j]) {
					out = append(out, column{table, ts[j]})
				}
			}
		} else if j < len(ts) && ts[j] == "(" {
			depth := 1
			start := true
			for j++; j < len(ts) && depth > 0; j++ {
				t := ts[j]
				if depth == 1 && start {
					if tenant(t) {
						out = append(out, column{table, t})
					}
					start = false
				}
				if t == "(" {
					depth++
				}
				if t == ")" {
					depth--
				}
				if depth == 1 && t == "," {
					start = true
				}
			}
		}
	}
	return out
}
func literal(x ast.Expr) (string, bool) {
	switch v := x.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, e := strconv.Unquote(v.Value)
			return s, e == nil
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			a, ao := literal(v.X)
			b, bo := literal(v.Y)
			return a + b, ao && bo
		}
	case *ast.ParenExpr:
		return literal(v.X)
	}
	return "", false
}
func scan(root string) ([]string, error) {
	owners := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, "specs/gates/tenant-schema-domains.txt"))
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || owners[f[0]] != "" {
			return nil, fmt.Errorf("invalid or duplicate schema owner: %s", line)
		}
		owners[f[0]] = f[1]
	}
	var out []string
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) < 3 {
			return fmt.Errorf("unknown schema domain: %s", rel)
		}
		report := func(sql string, line int) {
			for _, c := range columns(sql) {
				domain := parts[1]
				if owner := owners[c.table]; owner != "" {
					domain = owner
				}
				out = append(out, fmt.Sprintf("%s\t%s:%d:%s.%s", domain, rel, line, c.table, c.name))
			}
		}
		if strings.HasSuffix(path, ".sql") {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			report(string(b), 1)
		} else if strings.HasSuffix(path, ".go") {
			fs := token.NewFileSet()
			f, e := parser.ParseFile(fs, path, nil, 0)
			if e != nil {
				return e
			}
			ast.Inspect(f, func(n ast.Node) bool {
				x, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				if s, ok := literal(x); ok {
					report(s, fs.Position(x.Pos()).Line)
					return false
				}
				return true
			})
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}
func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	hits, err := scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, h := range hits {
		fmt.Println(h)
	}
}
