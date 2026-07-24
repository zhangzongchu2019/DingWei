package demo

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/mcsd"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/route"
)

// 鉴权 mock:未登记会话必须拒(fail-closed,不默认放行)。
func TestMockAuth_FailClosed(t *testing.T) {
	a := NewMockAuth()
	a.Bind("s1", "zzc-x-1", false)
	if _, err := a.StampSession(context.Background(), "unknown"); err == nil {
		t.Fatal("未登记会话应拒绝盖章")
	}
	if s, err := a.StampSession(context.Background(), "s1"); err != nil || s.Principal != "zzc-x-1" {
		t.Fatalf("已登记会话应盖章,got %+v/%v", s, err)
	}
}

// 面板只读:POST/PUT→405,GET→200。
func TestPanel_ReadOnly(t *testing.T) {
	b, err := board.Open(filepath.Join(t.TempDir(), "p.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	h := PanelHandler(b)
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(m, "/", nil))
		if rr.Code != 405 {
			t.Fatalf("%s 应 405,got %d", m, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 200 {
		t.Fatalf("GET 应 200,got %d", rr.Code)
	}
}

// 回放:未登记会话被盖章闸拦(不进收件面);已登记 status 走引擎 op。
func TestReplay_StampGate(t *testing.T) {
	ctx := context.Background()
	b, err := board.Open(filepath.Join(t.TempDir(), "r.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	epoch, err := b.Takeover(ctx, "board1", "zzc-orch-1", 3600)
	if err != nil {
		t.Fatal(err)
	}
	rb := func(w string) board.RoleBinding { return board.RoleBinding{Who: w, CLI: "claude", LLM: "m"} }
	ra := board.RoleAssignments{Designer: rb("zzc-d-1"), Implementer: rb("zzc-i-1"), Tester: rb("zzc-t-1"), QAOwner: rb("zzc-q-1")}
	if err := b.CreateTask(ctx, "t1", "board1", "t", ra, board.Identity{Owner: "zzc-orch-1", Epoch: epoch}); err != nil {
		t.Fatal(err)
	}
	rc := mcsd.NewReceiver(b, map[string]bool{"zzc-orch-1": true}, nil, true)
	auth := NewMockAuth()
	auth.Bind("s-impl", "zzc-i-1", false)

	in := strings.NewReader(`{"session":"ghost","meta_op":"status","task_id":"t1","body":"x"}
{"session":"s-impl","meta_op":"status","task_id":"t1","body":"进行中"}`)
	var got []ReplayResult
	n, err := Replay(ctx, in, auth, rc, func(r ReplayResult) { got = append(got, r) })
	if err != nil || n != 2 {
		t.Fatalf("回放 n=%d err=%v", n, err)
	}
	if got[0].Stamped {
		t.Fatal("未登记会话不应盖章通过")
	}
	if !got[1].Stamped || got[1].Decision.Verdict != route.VerdictEngineOp {
		t.Fatalf("已登记 status 应 engine_op,got %+v", got[1].Decision)
	}
}
