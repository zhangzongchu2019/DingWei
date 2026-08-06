package datad

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAssignSoleWritePath:契约 61e5938 §5① N-A3 代码层守卫——assignment 表唯一写路径。
//
// DB 层(deploy/datad-grants.sql)挡 datad_user 之外的角色直写;但 datad_user 进程内若某
// 新端点加一处 INSERT/UPDATE assignment,就用同一 datad_user 角色绕过了 assign 事务,破坏
// §5① "=1"(当代双角色齐)——GRANT 不辨代码路径,挡不住。此测试扫描 datad 包非测试源,
// 断言 assignment 表的写语句(INSERT INTO assignment / UPDATE assignment)只出现在唯一写
// 路径文件 assignment_pg.go。新增写路径=测试红,强制走评审并同步更新 allow 名单+契约。
func TestAssignSoleWritePath(t *testing.T) {
	// 词界 \b 防误伤 assign_first_result(首果账,允许写)与 assignment_active_uniq(索引名)。
	writeRe := regexp.MustCompile(`(?i)(INSERT\s+INTO\s+assignment|UPDATE\s+assignment)\b`)
	allowed := map[string]bool{"assignment_pg.go": true}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if writeRe.Match(src) && !allowed[f] {
			t.Fatalf("assignment 表写语句出现在非唯一写路径文件 %s——§5① 唯一写路径被绕过。"+
				"若确需新写路径:先评审契约 §5①,再把该文件加入 allowed 并说明其如何维持双角色齐+代一致。", f)
		}
	}
	// 反向哨兵:确保唯一写路径文件确含写语句(防重构把写移走却没更新本测试=守卫空转)。
	src, err := os.ReadFile("assignment_pg.go")
	if err != nil {
		t.Fatal(err)
	}
	if !writeRe.Match(src) {
		t.Fatal("assignment_pg.go 未见 assignment 表写语句——唯一写路径已迁移?请更新本测试 allowed 名单。")
	}
}
