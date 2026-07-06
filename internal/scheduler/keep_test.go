package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

type fnRunner struct{ fn func(prompt string) string }

func (r fnRunner) Run(_ context.Context, prompt string) (string, error) { return r.fn(prompt), nil }

const keepOriginal = "# 标题\n\n<!-- WP:KEEP 甘特 -->\n```mermaid\ngantt\n  title X\n```\n<!-- /WP:KEEP -->\n\n## 正文\n旧内容\n"

// ②③ 良性 runner：LLM 改正文、保留占位符 → 保护块字节级缝回、变更应用、占位符不入盘。
func TestCoordinateKeepBlocksPreserved(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "AI-研究工作内容清单.md")
	if err := os.WriteFile(team, []byte(keepOriginal), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "工作计划-a.md"), []byte("个人\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	good := fnRunner{fn: func(p string) string {
		if strings.Contains(p, "gantt") || strings.Contains(p, "title X") {
			t.Fatalf("保护块内容泄漏进了 LLM prompt")
		}
		if !strings.Contains(p, "WP:KEEP:0") {
			t.Fatalf("占位符未出现在喂给 LLM 的快照里")
		}
		return "# 标题\n\n<!-- WP:KEEP:0 -->\n\n## 正文\n新内容\n"
	}}
	svc := New(Config{TeamFile: team, PersonalDir: dir, BackupDir: filepath.Join(dir, "backup"), Command: "x"},
		good, &clock.Fake{T: time.Date(2026, 7, 4, 8, 0, 0, 0, time.UTC)}, nil)
	if _, err := svc.Coordinate(context.Background(), "把正文改一下"); err != nil {
		t.Fatalf("coordinate: %v", err)
	}
	s := readFile(t, team)
	if !strings.Contains(s, "新内容") {
		t.Fatalf("变更未应用: %s", s)
	}
	if !strings.Contains(s, "```mermaid\ngantt\n  title X\n```") {
		t.Fatalf("保护的甘特未原样缝回: %s", s)
	}
	if strings.Contains(s, "WP:KEEP:0") {
		t.Fatalf("占位符泄漏进最终文件: %s", s)
	}
}

// ③ 恶意 runner：LLM 丢了占位符 → 断言拦截、拒绝写入、原文件原样不动。
func TestCoordinateRejectsWhenPlaceholderDropped(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "AI-研究工作内容清单.md")
	os.WriteFile(team, []byte(keepOriginal), 0o640)
	os.WriteFile(filepath.Join(dir, "工作计划-a.md"), []byte("个人\n"), 0o640)

	bad := fnRunner{fn: func(p string) string { return "# 标题\n\n## 正文\n新内容(丢了占位符)\n" }}
	svc := New(Config{TeamFile: team, PersonalDir: dir, BackupDir: filepath.Join(dir, "backup"), Command: "x"},
		bad, &clock.Fake{T: time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)}, nil)
	if _, err := svc.Coordinate(context.Background(), "改"); err == nil {
		t.Fatalf("占位符丢失时应报错拒绝写入")
	}
	if got := readFile(t, team); got != keepOriginal {
		t.Fatalf("拒绝写入时原文件必须原样不动, got: %s", got)
	}
}

// maskKeepBlocks/restoreKeepBlocks 往返一致。
func TestMaskRestoreRoundTrip(t *testing.T) {
	masked, blocks := maskKeepBlocks(keepOriginal)
	if len(blocks) != 1 {
		t.Fatalf("want 1 keep block, got %d", len(blocks))
	}
	if strings.Contains(masked, "gantt") {
		t.Fatalf("masked 仍含保护内容")
	}
	restored, missing := restoreKeepBlocks(masked, blocks)
	if len(missing) != 0 || restored != keepOriginal {
		t.Fatalf("round trip mismatch: missing=%v\n%q", missing, restored)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
