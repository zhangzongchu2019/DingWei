package m8

// 阴性对照(复验席 opus50tc2k 补,组长 2026-09-25T16:40Z 裁定的两条之二)
//
// 钉的是:GET spec 详情【无条件依赖 console_spec_gate_review 表】——
// 迁移 0046 若晚于新二进制生效,详情页不是"复核功能不可用",而是【整页 503 打不开】。
// 🔴 排他性:503 在本 handler 里有两个来源(ConsoleSpec 失败也 503),
//    所以只断状态码是不排他的 —— 必须连【那一句正文】一起断。

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 与 newConsoleSpecTestHub 同构,但把库路径交出来,便于按真实错误路径制造"表不存在"。
func newConsoleSpecTestHubAtPath(t *testing.T) (*Hub, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "console-spec-gate-missing.db")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.ConsolePrincipal{
		{ID: "owner-a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oa", DisplayName: "甲", Active: true},
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "om", DisplayName: "丙", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertConsolePrincipalRole(context.Background(), model.ConsolePrincipalRole{PrincipalID: p.ID, Role: "owner", OwnerKey: "tenant-a", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	return New(db, ConsoleAuthMode{enabled: true}), path
}

func getConsoleSpecDetail(t *testing.T, hub *Hub, specID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/console/specs/"+specID, bytes.NewBufferString(""))
	req.SetPathValue("specID", specID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionRead, hub.HandleConsoleSpec)(rr, req)
	return rr
}

func TestConsoleSpecDetailFailsLoudlyWhenGateReviewTableIsMissing(t *testing.T) {
	hub, path := newConsoleSpecTestHubAtPath(t)
	criteria := `[{"id":"A1","text":"闸要挡住写错的 spec","owner_ref":"owner:先挡住","source":"owner","checked_by":"opus50tc2k","checked_at":"2026-09-25T16:25:00Z","cases":["console_spec_gate_review_unavailable_test.go"]}]`
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(criteria), hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// 🔵 阳性对照:表在 ⇒ 详情正常。缺了这一格,下面那条可能只是"详情本来就打不开"。
	if ok := getConsoleSpecDetail(t, hub, created.ID); ok.Code != http.StatusOK {
		t.Fatalf("表在时详情就打不开:code=%d body=%s", ok.Code, ok.Body.String())
	}

	// 制造"迁移 0046 尚未生效"这一个事实,其余一切不动。
	raw, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(context.Background(), `DROP TABLE console_spec_gate_review`); err != nil {
		t.Fatal(err)
	}

	missing := getConsoleSpecDetail(t, hub, created.ID)
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("表缺失时详情未 503:code=%d body=%s —— 失败被吞掉了,巡检看不见", missing.Code, missing.Body.String())
	}
	// 🔴 排他:本 handler 另一处 503 的正文是 "console specs unavailable";
	// 若断不到下面这一句,说明 503 来自别的依赖,这条用例就没在看 gate_review。
	if !strings.Contains(missing.Body.String(), "console spec review unavailable") {
		t.Fatalf("503 了但不是 gate_review 这一条造成的:body=%s", missing.Body.String())
	}
}
