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
	managerExtensions := append(managerMapExtensions(t, managerPath, "exts", "downloadExts", "filenameTextExts", "filenameAudioExts"), "bin")
	assertForbiddenExtensionsAbsent(t, "nginx file_ext", nginxExtensions)
	assertForbiddenExtensionsAbsent(t, "directObjectKey", storeExtensions)
	assertForbiddenExtensionsAbsent(t, "manager media maps", managerExtensions)
	assertExtensionSetsEqual(t, "manager", managerExtensions, "nginx", nginxExtensions)
	assertExtensionSetsEqual(t, "manager", managerExtensions, "directObjectKey", storeExtensions)
	assertNginxServesTextExtensionsAsPlain(t, nginx, managerMapExtensions(t, managerPath, "filenameTextExts"))
	assertNginxInlineTypesRejectExecutableMedia(t, nginx)
}

var executableInlineTypeCorpus = []string{
	"text/html", "application/xhtml+xml", "text/xml", "application/xml", "image/svg+xml",
	"text/javascript", "application/javascript", "application/x-javascript", "text/ecmascript", "application/ecmascript",
}

var forbiddenNginxInlineTypes = map[string]bool{
	"text/html": true, "application/xhtml+xml": true, "text/xml": true, "application/xml": true, "image/svg+xml": true,
	"text/javascript": true, "application/javascript": true, "application/x-javascript": true, "text/ecmascript": true, "application/ecmascript": true,
}

func assertNginxInlineTypesRejectExecutableMedia(t *testing.T, nginx string) {
	t.Helper()
	if forbidden, ok := firstForbiddenNginxInlineType(nginx); ok {
		t.Fatalf("nginx inline types contains forbidden media type %q", forbidden)
	}
	const safeDirective = "video/mp4 mp4;"
	if !strings.Contains(nginx, safeDirective) {
		t.Fatalf("mutation anchor %q missing", safeDirective)
	}
	for _, forbidden := range executableInlineTypeCorpus {
		mutated := strings.Replace(nginx, safeDirective, forbidden+" mp4;", 1)
		if got, rejected := firstForbiddenNginxInlineType(mutated); !rejected || got != forbidden {
			t.Fatalf("forbidden inline type mutation accepted: want=%q got=%q rejected=%t", forbidden, got, rejected)
		}
	}
}

// 🔴 /f/ location 的正则【必须带引号】(否则 nginx 把 {1,64} 当块起始,pcre2 报
// "missing closing parenthesis" —— 0.2.0.6 v2 曾因此回滚,修复见 dcfa01ee)。
// 这里按【内容】切分而不写死引号形式:带不带引号都能定位,
// 免得下次有人调整书写方式时,是这道闸先红而不是 nginx 先红。
var inlineFLocationSplit = regexp.MustCompile(`location ~ "?\^/f/`)

// 🔴 只测一件事:含 {} 量词的 /f/ location 正则【必须被引号包住】。
// 不带引号时 nginx 会把 {1,64} 的 { 当作块起始,pcre2 报
//
//	missing closing parenthesis in "^/f/(?<file_rel>[a-z0-9_-]"
//
// 0.2.0.6 v2 因此回滚过一次(RELEASE-0.2.0.6-v2-NGINX-SYNTAX-ROLLBACK-20260918),
// 修复见 dcfa01ee —— 而该修复只在发布线上,从未回流工作分支,
// 于是 0.2.0.7 的发版锚又带着坏形态出来,被 prodops 在部署第 2 步拦下。
//
// 🔴 本用例【不能】与按内容定位的那几道合并:
// 那几道为了兼容书写形式而对引号不敏感,去掉引号它们仍全绿。
// 引号这件事必须有一条只盯它的用例,否则坏形态只会在部署当天被 nginx 拒绝。
func TestInlineFLocationRegexesAreQuoted(t *testing.T) {
	nginx := mustRead(t, "../../deploy/nginx-dingwei.conf")
	unquoted := regexp.MustCompile(`(?m)^location ~ \^/f/`).FindAllString(nginx, -1)
	if len(unquoted) != 0 {
		t.Fatalf("发现 %d 条未加引号的 /f/ location 正则;含 {} 量词时 nginx 会解析失败(见 dcfa01ee)", len(unquoted))
	}
	quoted := regexp.MustCompile(`(?m)^location ~ "\^/f/[^"]*\{[0-9]`).FindAllString(nginx, -1)
	if len(quoted) < 2 {
		t.Fatalf("带引号且含 {} 量词的 /f/ location 应至少 2 条,实得 %d —— 是不是正则形态被改了?", len(quoted))
	}
}

func firstForbiddenNginxInlineType(nginx string) (string, bool) {
	locations := inlineFLocationSplit.Split(nginx, -1)
	if len(locations) < 2 {
		return "", false
	}
	types := regexp.MustCompile(`(?s)types\s*\{(.*?)\}`).FindStringSubmatch(locations[1])
	if len(types) != 2 {
		return "", false
	}
	for _, directive := range regexp.MustCompile(`(?m)^\s*([^\s;]+)\s+`).FindAllStringSubmatch(types[1], -1) {
		if forbiddenNginxInlineTypes[directive[1]] {
			return directive[1], true
		}
	}
	return "", false
}

func assertNginxServesTextExtensionsAsPlain(t *testing.T, nginx string, textExtensions []string) {
	t.Helper()
	locations := inlineFLocationSplit.Split(nginx, -1)
	if len(locations) < 2 {
		t.Fatal("nginx inline /f/ location not found")
	}
	types := regexp.MustCompile(`(?s)types\s*\{(.*?)\}`).FindStringSubmatch(locations[1])
	if len(types) != 2 {
		t.Fatal("nginx inline types block not found")
	}
	extensionType := map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^\s*([^\s;]+)\s+([^;]+);`).FindAllStringSubmatch(types[1], -1) {
		for _, extension := range strings.Fields(match[2]) {
			extensionType[extension] = match[1]
		}
	}
	for _, extension := range textExtensions {
		if got := extensionType[extension]; got != "text/plain" {
			t.Fatalf("nginx text extension %q has type %q, want text/plain", extension, got)
		}
	}
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
