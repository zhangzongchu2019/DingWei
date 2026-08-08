package datad

// B1 防漂移交叉测试(claudek3 T18 审 CHANGES/B1 裁定转正):真 Server×真 Client 同测,
// 钉死线格式=writeJSON 裸数组。fake 自喂形态一旦与 server 漂移,此处即红。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAgainstRealServerWireFormat(t *testing.T) {
	s, _ := testServer()
	httpSrv := httptest.NewServer(s)
	t.Cleanup(httpSrv.Close)
	ctx := context.Background()

	// 空库:server 对 nil 切片 Encode 出 null→client 须安全返回空集且 err=nil。
	c := NewClient(httpSrv.URL, "tok-devo")
	tasks, err := c.ListTasks(ctx)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("empty tasks: n=%d err=%v", len(tasks), err)
	}
	sessions, err := c.ListSessions(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("empty sessions: n=%d err=%v", len(sessions), err)
	}

	// 非空:经真 server 写入后,client 解裸数组必须取回同数据。
	if w := call(t, s, "PUT", "/v1/tasks/T-cross", "tok-coord",
		`{"title":"cross","owner_session":"zzc-devo48-2642","status":"in_progress"}`, nil); w.Code != 200 {
		t.Fatalf("seed task: %d %s", w.Code, w.Body.String())
	}
	if w := call(t, s, "PUT", "/v1/sessions/me/state", "tok-devo",
		`{"state":"active","current_task":"T-cross"}`, nil); w.Code != 200 {
		t.Fatalf("seed session: %d %s", w.Code, w.Body.String())
	}
	tasks, err = c.ListTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].ID != "T-cross" || tasks[0].Status != "in_progress" {
		t.Fatalf("tasks roundtrip: %+v err=%v", tasks, err)
	}
	sessions, err = c.ListSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].Session != "zzc-devo48-2642" {
		t.Fatalf("sessions roundtrip: %+v err=%v", sessions, err)
	}

	// 鉴权面顺核:错 token 经 client=非 200 错误,不静默。
	bad := NewClient(httpSrv.URL, "wrong")
	if _, err := bad.ListTasks(ctx); err == nil {
		t.Fatal("bad token must error")
	}
}

// claudek3 r2 nit 转正:钉死 base 带 /datad 子路径前缀时的拼接(生产 URL 形态
// https://dingwei.wegoab.com/datad → /datad/v1/tasks,防 JoinPath 回归吞前缀)。
func TestClientBaseWithSubpathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL+"/datad/", "tok") // 带尾斜杠+子路径,双坑同测
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatalf("subpath base: %v", err)
	}
	if gotPath != "/datad/v1/tasks" {
		t.Fatalf("path=%q want /datad/v1/tasks", gotPath)
	}
}
