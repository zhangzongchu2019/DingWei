package filepolicy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUploadLimitMatchesNginx(t *testing.T) {
	if MaxUploadBytes != 52_428_800 {
		t.Fatalf("MaxUploadBytes=%d", MaxUploadBytes)
	}
	raw, err := os.ReadFile("../../deploy/nginx-dingwei.conf")
	if err != nil {
		t.Fatal(err)
	}
	const directive = "client_max_body_size 50m;"
	if count := strings.Count(string(raw), directive); count != 1 {
		t.Fatalf("%q count=%d", directive, count)
	}
}

func TestDirectLinkExtensionAllowListsExcludeActiveContent(t *testing.T) {
	nginx := mustRead(t, "../../deploy/nginx-dingwei.conf")
	store := mustRead(t, "../fileservice/local_store.go")
	managerPath := "../fileservice/manager.go"
	nginxExtensions := captureAllExtensions(t, nginx, `\(\?<file_ext>([^)]+)\)`)
	storeExtensions := captureExtensions(t, store, `directObjectKey\s*=.*?\\\.\(\?:([^)]*)\)`)
	managerExtensions := append(managerMapExtensions(t, managerPath, "exts", "downloadExts"), "bin")
	assertForbiddenExtensionsAbsent(t, "nginx file_ext", nginxExtensions)
	assertForbiddenExtensionsAbsent(t, "directObjectKey", storeExtensions)
	assertForbiddenExtensionsAbsent(t, "manager media maps", managerExtensions)
	assertExtensionSetsEqual(t, "manager", managerExtensions, "nginx", nginxExtensions)
	assertExtensionSetsEqual(t, "manager", managerExtensions, "directObjectKey", storeExtensions)
}

func managerMapExtensions(t *testing.T, path string, names ...string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	found := make(map[string]bool, len(names))
	var extensions []string
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		name, ok := assignment.Lhs[0].(*ast.Ident)
		literal, literalOK := assignment.Rhs[0].(*ast.CompositeLit)
		if !ok || !literalOK || !wanted[name.Name] {
			return true
		}
		found[name.Name] = true
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			value, valueOK := pair.Value.(*ast.BasicLit)
			if !valueOK || value.Kind != token.STRING {
				continue
			}
			decoded, decodeErr := strconv.Unquote(value.Value)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			extensions = append(extensions, decoded)
		}
		return true
	})
	for _, name := range names {
		if !found[name] {
			t.Fatalf("manager media map %q not found", name)
		}
	}
	return extensions
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func captureExtensions(t *testing.T, raw, pattern string) []string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(raw)
	if len(match) != 2 {
		t.Fatalf("extension allowlist not found with %q", pattern)
	}
	return strings.Split(match[1], "|")
}

func captureAllExtensions(t *testing.T, raw, pattern string) []string {
	t.Helper()
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		t.Fatalf("extension allowlists not found with %q", pattern)
	}
	var extensions []string
	for _, match := range matches {
		if len(match) != 2 {
			t.Fatalf("invalid extension allowlist match for %q", pattern)
		}
		extensions = append(extensions, strings.Split(match[1], "|")...)
	}
	return extensions
}

func assertExtensionSetsEqual(t *testing.T, leftName string, left []string, rightName string, right []string) {
	t.Helper()
	normalize := func(values []string) []string {
		set := make(map[string]bool, len(values))
		for _, value := range values {
			set[value] = true
		}
		result := make([]string, 0, len(set))
		for value := range set {
			result = append(result, value)
		}
		sort.Strings(result)
		return result
	}
	want, got := normalize(left), normalize(right)
	if strings.Join(want, "|") != strings.Join(got, "|") {
		t.Fatalf("extension sets differ: %s=%v %s=%v", leftName, want, rightName, got)
	}
}

func assertForbiddenExtensionsAbsent(t *testing.T, source string, extensions []string) {
	t.Helper()
	forbidden := map[string]bool{"svg": true, "svgz": true, "html": true, "htm": true, "xhtml": true, "xml": true}
	for _, extension := range extensions {
		if forbidden[extension] {
			t.Fatalf("%s permits active-content extension %q", source, extension)
		}
	}
}

func TestDefaultRetentionMatchesContract(t *testing.T) {
	if DefaultRetention != 7*24*time.Hour {
		t.Fatalf("DefaultRetention=%s", DefaultRetention)
	}
}

func TestDeploymentContractMatchesFilePolicy(t *testing.T) {
	contract := mustRead(t, "../../deploy/H-FILE-SERVICE-DEPLOYMENT.md")
	for _, setting := range []string{
		"FS_RETENTION=168h",
		"FS_MAX_FILE_BYTES=52428800",
	} {
		if count := strings.Count(contract, setting); count != 1 {
			t.Fatalf("deployment setting %q count=%d", setting, count)
		}
	}
}
