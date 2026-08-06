package datad

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAssignSoleWritePath:契约 §5① N-A3 代码层守卫——assignment/assign_first_result 唯一写路径。
//
// R2-7 修:从**仓根 WalkDir** 扫描所有受版本控制的 Go/SQL/迁移脚本(非只 Glob 本包)——新增
// 目录/组件里的写语句同样命中;allowlist 按**类别显式区分**(实现文件/测试/GRANT 部署件),
// 不用宽泛目录排除;新增命中默认失败。保留反向哨兵:唯一写路径文件必须确含预期写语句
// (防重构把写移走守卫空转)。DB 层(deploy/datad-grants.sql 三角色 ACL)与本守卫两层合力;
// 代码层守卫是纵深,不替代 PG ACL 原证(addendum ⑤)。
func TestAssignSoleWritePath(t *testing.T) {
	root := repoRoot(t)
	// 写语句(词界防误伤 assignment_active_uniq 索引名/GRANT ... ON assignment 授权句)。
	writeRe := regexp.MustCompile(`(?i)(INSERT\s+INTO\s+(assignment|assign_first_result)|UPDATE\s+(assignment|assign_first_result)|DELETE\s+FROM\s+(assignment|assign_first_result))\b`)

	// 类别显式 allowlist(仓根相对路径;宽泛目录排除禁用)。
	type klass string
	const (
		kImpl  klass = "impl"  // 唯一写路径实现(反向哨兵要求确含写语句)
		kGrant klass = "grant" // 部署授权件(GRANT/REVOKE 语句面,不含 DML;类别显式登记)
		kTest  klass = "test"  // 测试(直写=fixture/ACL 负例面,不经生产路径;逐文件登记非通配)
	)
	allowed := map[string]klass{
		"internal/datad/assignment_pg.go":                  kImpl, // AssignGeneration 事务+首果+GC(唯一 DML 面)
		"deploy/datad-grants.sql":                          kGrant,
		"internal/datad/assignment_pg_integration_test.go": kTest, // ACL 负例须以各角色直写(证 denied)
	}

	hits := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".sql" {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if !writeRe.Match(src) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		hits[rel] = true
		if _, ok := allowed[rel]; !ok {
			t.Errorf("assignment/assign_first_result 写语句出现在未登记文件 %s——§5① 唯一写路径被绕过。"+
				"若确需:先评审契约 §5①,再按类别登记 allowlist 并说明如何维持双角色齐+代一致。", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 反向哨兵:唯一写路径实现必须在命中集(防写路径迁移后守卫空转)。
	if !hits["internal/datad/assignment_pg.go"] {
		t.Fatal("assignment_pg.go 未见 assignment 写语句——唯一写路径已迁移?更新 allowlist 与本哨兵。")
	}
}

// repoRoot 从测试工作目录向上找 go.mod(=仓根)。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到仓根(go.mod)")
		}
		dir = parent
	}
}

// TestGrantFileCoversThreeRoles:部署件结构自检(纵深,不替代真 PG 证据)——grants.sql 必须
// 含三角色模型的关键语句;缺失=部署件回退旧单角色版。
func TestGrantFileCoversThreeRoles(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "datad-grants.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, must := range []string{
		"REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON assignment FROM datad_user",
		"REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON assign_first_result FROM datad_user",
		"GRANT SELECT, INSERT, UPDATE ON assignment TO datad_assign",
		"GRANT SELECT, INSERT ON assign_first_result TO datad_assign",
		"GRANT SELECT, DELETE ON assign_first_result TO datad_assign_gc",
		"REVOKE ALL ON assignment FROM datad_assign_gc",
		"GRANT USAGE ON SEQUENCE audit_id_seq TO datad_assign",
	} {
		if !strings.Contains(s, must) {
			t.Errorf("grants.sql 缺关键语句: %s", must)
		}
	}
}
