package admin

// G/0.2.1.0:渠道保留字不得用作业主名/成员显示名(业主裁定,与 Agent 短名并列)。
//
// 🔴 组长限定范围:只拦【新增/改名】,存量同名成员不动(不迁移、不批量改名、不启动期校验)。
//    ⇒ N4 专门钉这一条:存量同名成员更新其它字段时**不得被拒**,
//      否则等于对存量做了一次隐式迁移 —— 那是被明确排除的。

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// N1:新增时用保留字 ⇒ 拒绝。
func TestMemberUpsertRejectsReservedChannelNames(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	for _, name := range []string{"飞书", "feishu", "fs", "企微", "slack", "google", "FS", "Lark"} {
		rec := postFormRaw(mux, "/admin/members", url.Values{
			"owner_key": {name}, "display_name": {"X"}, "role": {"member"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("N1:owner_key=%q 应被拒,实测 status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	// display_name 同样要拦 —— 业主原话是「业主名/Agent名的短名」,
	// 而 resolveMemberByName 同时匹配 OwnerKey 与 DisplayName,只拦一边等于没拦。
	rec := postFormRaw(mux, "/admin/members", url.Values{
		"owner_key": {"dev:personal:x"}, "display_name": {"飞书"}, "role": {"member"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("N1:display_name=飞书 应被拒,实测 status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// N2 反向:普通名字必须成功 —— 只测 N1 是半条,「一律拒绝」能让 N1 全过。
func TestMemberUpsertAcceptsOrdinaryNames(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	for _, name := range []string{"dev:personal:zzc", "opus50tc2k", "张三", "feishuman", "fsx"} {
		rec := postFormRaw(mux, "/admin/members", url.Values{
			"owner_key": {name}, "display_name": {"Ordinary"}, "role": {"member"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("N2:普通名 %q 必须能存,实测 status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
}

// N4:存量同名成员更新其它字段时不得被拒(组长明确排除存量迁移)。
func TestMemberUpsertLeavesExistingReservedNameAlone(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	// 绕过 HTTP 入口直接落库,模拟「本版之前就已存在的同名成员」
	if err := db.UpsertMember(ctx, model.Member{
		OwnerKey: "飞书", DisplayName: "飞书", Role: model.RoleMember, Active: true, FeishuOpenID: "ou_legacy",
	}); err != nil {
		t.Fatal(err)
	}

	// 只改角色,名字与 open_id 一个字没动 ⇒ 必须放行
	// (刻意不改 open_id:allocateUniqueOwnerKey 会按 owner_key+open_id 去重,
	//  改 open_id 会被它判成另一个人并分配新 owner_key —— 那是既有行为,与本守卫无关,
	//  用它做素材会把两件事混在一条用例里。)
	rec := postFormRaw(mux, "/admin/members", url.Values{
		"owner_key": {"飞书"}, "display_name": {"飞书"}, "feishu_open_id": {"ou_legacy"}, "role": {"manager"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("N4:存量同名成员改其它字段必须放行(本版不做存量迁移),实测 status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, err := db.GetMemberByOwnerKey(ctx, "飞书")
	if err != nil || got == nil {
		t.Fatalf("取回存量成员失败 err=%v got=%v", err, got)
	}
	if got.Role != model.RoleManager {
		t.Fatalf("N4:更新必须真的生效,实测 role=%q", got.Role)
	}

	// 反向:把另一个【普通】成员改名成保留字 ⇒ 仍要拒(这是「改名」,不是存量)
	if err := db.UpsertMember(ctx, model.Member{
		OwnerKey: "dev:personal:norm", DisplayName: "Normal", Role: model.RoleMember, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec = postFormRaw(mux, "/admin/members", url.Values{
		"owner_key": {"dev:personal:norm"}, "display_name": {"slack"}, "role": {"member"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("N4 反向:把普通成员改名成保留字属于「改名」,必须拒,实测 status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// 拒绝时必须说清楚为什么 —— 「保留字」三个字对管理员没有信息量。
func TestMemberUpsertRejectionExplainsWhy(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	rec := postFormRaw(mux, "/admin/members", url.Values{
		"owner_key": {"飞书"}, "display_name": {"X"}, "role": {"member"},
	})
	body := rec.Body.String()
	if !strings.Contains(body, "渠道保留字") || !strings.Contains(body, "两种合法解读") {
		t.Fatalf("拒绝文案必须说明歧义从何而来,实测 %q", body)
	}
}
