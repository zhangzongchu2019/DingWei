package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
)

func oldClarificationKindSchema(t *testing.T, db *SQLite) string {
	t.Helper()
	body, err := migrationFS.ReadFile("migrations/0066_spec_clarification_supplement.sql")
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(body), "'supplement',", "", 1)
	if err := db.applyForeignKeyDisabledMigration(context.Background(), "fixture_old_clarification_kind", old, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("DELETE FROM schema_migrations WHERE version='0066_spec_clarification_supplement.sql'"); err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestClarificationSupplementKindKeepsExistingAuthorityBoundary(t *testing.T) {
	db, now, q := clarificationFixture(t)
	u := clarificationUpdate("typed-supplement")
	u.Kind = "supplement"
	out, err := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, u)
	if err != nil || out.Post.Kind != "supplement" || out.Post.Role != "pm" || out.ExecutionAuthorized {
		t.Fatalf("补充类型/权限不正确: %+v %v", out, err)
	}
	u.RequestID = "technical-cannot-supplement"
	if _, err := clarificationApply(t, db, now, BlockerActor{AgentID: "dev"}, q, u); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatalf("补充不得扩展技术席权威: %v", err)
	}
	u.RequestID = "supplement-cannot-correct"
	u.Corrects = out.Post.Floor
	if _, err := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, u); !errors.Is(err, ErrTaskFlowInvalid) {
		t.Fatalf("补充不能暗带更正关系: %v", err)
	}
}

func TestClarificationSupplementMigrationPreservesFloorsRequestsImagesAndGuards(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor := BlockerActor{UserID: "pm"}
	check := func() error { return nil }
	image := clarificationimage.File{ID: strings.Repeat("e", 64), SHA: strings.Repeat("a", 64), Media: "image/png", Size: 24}
	if err := db.RegisterClarificationImage(ctx, c, actor, q.ID, image, check, now); err != nil {
		t.Fatal(err)
	}
	u := clarificationUpdate("before-kind-migration")
	u.Files = []string{image.ID}
	before, err := clarificationApply(t, db, now, actor, q, u)
	if err != nil {
		t.Fatal(err)
	}
	oldClarificationKindSchema(t, db)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("migration replay", err)
	}
	after, err := readClarificationPost(ctx, db.db, q.SpecID, before.Post.Floor)
	if err != nil || !reflect.DeepEqual(before.Post, after) {
		t.Fatalf("迁移改变历史楼层/附图: before=%+v after=%+v err=%v", before.Post, after, err)
	}
	replay, err := clarificationApply(t, db, now, actor, q, u)
	if err != nil || !replay.Replayed || replay.Post.Floor != before.Post.Floor {
		t.Fatalf("迁移丢失幂等请求: %+v %v", replay, err)
	}
	for _, statement := range []string{"UPDATE spec_clarification_post SET kind='supplement'", "DELETE FROM spec_clarification_post", "UPDATE pilot_question_answer SET body='rewritten'", "DELETE FROM spec_clarification_post_image"} {
		if _, err := db.db.Exec(statement); err == nil {
			t.Fatalf("迁移丢失不可变约束: %s", statement)
		}
	}
	violations, err := consoleForeignKeyViolations(ctx, db.db)
	if err != nil || len(violations) != 0 {
		t.Fatal("迁移后外键异常", violations, err)
	}
	u = clarificationUpdate("after-kind-migration")
	u.Kind = "supplement"
	added, err := clarificationApply(t, db, now, actor, q, u)
	if err != nil || added.Post.Floor != before.Post.Floor+1 {
		t.Fatal("迁移后补充不能按原楼号追加", added, err)
	}
}

func TestClarificationSupplementMigrationFailureRollsBackAndRestoresFK(t *testing.T) {
	db, _, _ := clarificationFixture(t)
	ctx := context.Background()
	body := oldClarificationKindSchema(t, db)
	err := db.applyForeignKeyDisabledMigration(ctx, "0066_spec_clarification_supplement.sql", body+"\nSELECT nonexistent_column FROM missing_table;", "fixture")
	if err == nil {
		t.Fatal("故障迁移必须失败")
	}
	var schema string
	if err := db.db.QueryRow("SELECT sql FROM sqlite_master WHERE name='spec_clarification_post'").Scan(&schema); err != nil || strings.Contains(schema, "'supplement'") {
		t.Fatal("迁移故障未回滚旧约束", schema, err)
	}
	var enabled, count int
	if err := db.db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("外键未恢复", enabled, err)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version='0066_spec_clarification_supplement.sql'").Scan(&count); err != nil || count != 0 {
		t.Fatal("失败迁移错误记账", count, err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("回滚后不可重试", err)
	}
}
