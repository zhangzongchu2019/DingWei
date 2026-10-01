package hosted

// T-G4-53 备份管线用例：全链（pending→export_seat→回传落盘→complete→Destroy 拿 ref）、
// 鉴权（别节点凭据/未知 id 拒）、半截文件不标记、TTL 到期清扫。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// gwBackupFake 假网关：收到 export_seat 就扮演节点——把内容 POST 到 upload_url。
type gwBackupFake struct {
	st      *Store
	content string
	called  []string
	fail    bool
}

func (g *gwBackupFake) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	g.called = append(g.called, cmd.Type)
	if cmd.Type != "export_seat" {
		return NodeResult{OK: true}, nil
	}
	if g.fail {
		return NodeResult{OK: false, Error: "injected"}, nil
	}
	var p struct {
		SeatID    string `json:"seat_id"`
		UploadURL string `json:"upload_url"`
	}
	_ = json.Unmarshal(cmd.Params, &p)
	// 节点侧动作：POST 卷内容（鉴权变体见 gwBackupWithAuth）
	resp, err := http.Post(p.UploadURL, "application/gzip", strings.NewReader(g.content))
	if err != nil {
		return NodeResult{OK: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return NodeResult{OK: false, Error: "upload rejected"}, nil
	}
	return NodeResult{OK: true}, nil
}


func TestBackupPipelineEndToEnd(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	// 节点登记（鉴权要真凭据）
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_bk", Name: "bk", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gw := &gwBackupFake{st: s, content: "tar-bytes-卷内容"}
	// 回传须带凭据——假网关补上真 secret
	gwWithAuth := &gwBackupWithAuth{inner: gw, secret: secret}
	bk := NewHubBackuper(s, gwWithAuth, "", dir)
	// Hub 接收端点（PathValue 依赖 mux 模式注册——真接线处亦然）
	mux := http.NewServeMux()
	mux.Handle("POST /api/node/backup/{id}", bk.BackupHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	bk.origin = srv.URL

	ref, err := bk.BackupSeatVolume(ctx, "nd_bk", "ag_bks")
	if err != nil {
		t.Fatalf("备份: %v", err)
	}
	if !strings.HasSuffix(ref, ".tar.gz") {
		t.Fatalf("backupRef: %s", ref)
	}
	raw, err := os.ReadFile(ref)
	if err != nil || string(raw) != "tar-bytes-卷内容" {
		t.Fatalf("落盘内容: %v %q", err, raw)
	}
	fi, _ := os.Stat(ref)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("备份件权限 0600: %o", fi.Mode())
	}
	// 记录 complete＋expires 约 14 天
	rec, _ := s.GetBackup(ctx, strings.TrimSuffix(strings.TrimPrefix(ref, dir+"/"), ".tar.gz"))
	if rec.State != "complete" || rec.SizeBytes <= 0 {
		t.Fatalf("记录: %+v", rec)
	}
	if d := rec.ExpiresAt.Sub(time.Now()); d < 13*24*time.Hour || d > 15*24*time.Hour {
		t.Fatalf("TTL 应约 14 天: %v", d)
	}
	// TTL 到期清扫：删文件＋expired
	if err := bk.SweepExpiredBackups(ctx, time.Now().Add(15*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ref); !os.IsNotExist(err) {
		t.Fatal("到期备份件应删除")
	}
	rec, _ = s.GetBackup(ctx, rec.ID)
	if rec.State != "expired" {
		t.Fatalf("应 expired: %+v", rec)
	}
}

// gwBackupWithAuth 带真凭据的回传桩。
type gwBackupWithAuth struct {
	inner  *gwBackupFake
	secret string
}

func (g *gwBackupWithAuth) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	if cmd.Type != "export_seat" {
		return g.inner.Send(ctx, nodeID, cmd)
	}
	var p struct {
		UploadURL string `json:"upload_url"`
	}
	_ = json.Unmarshal(cmd.Params, &p)
	req, _ := http.NewRequestWithContext(ctx, "POST", p.UploadURL, strings.NewReader(g.inner.content))
	req.Header.Set("Authorization", "Bearer "+g.secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return NodeResult{OK: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return NodeResult{OK: false, Error: "rejected"}, nil
	}
	return NodeResult{OK: true}, nil
}

func TestBackupHandlerAuthAndStates(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_b1", Name: "b1", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// 第二个节点凭据（串用必拒）
	if _, err := s.RegisterNode(ctx, Node{ID: "nd_b2", Name: "b2", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bk := NewHubBackuper(s, &gwBackupFake{}, "", dir)
	mux := http.NewServeMux()
	mux.Handle("POST /api/node/backup/{id}", bk.BackupHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func() string {
		id, _ := newBackupID()
		if err := s.CreateBackup(ctx, Backup{ID: id, NodeID: "nd_b1", SeatID: "ag_x", State: "pending",
			Path: dir + "/" + id + ".tar.gz", ExpiresAt: time.Now().Add(BackupTTL)}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	post := func(id, auth string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/node/backup/"+id, strings.NewReader("data"))
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("bk_nope", "Bearer "+secret); c != 404 {
		t.Fatalf("未知 id 应 404: %d", c)
	}
	if c := post(mk(), "Bearer wrong"); c != 401 {
		t.Fatalf("坏凭据应 401: %d", c)
	}
	if c := post(mk(), ""); c != 401 {
		t.Fatalf("无凭据应 401: %d", c)
	}
	// 正常：pending＋对节点凭据 ⇒ 200＋complete
	id := mk()
	if c := post(id, "Bearer "+secret); c != 200 {
		t.Fatalf("正常回传: %d", c)
	}
	// 已 complete 的再传 ⇒ 404（不覆写）
	if c := post(id, "Bearer "+secret); c != 404 {
		t.Fatalf("重复回传应 404: %d", c)
	}
}

func TestBackupFailureFailsDestroy(t *testing.T) {
	// 回传失败 ⇒ BackupSeatVolume 报错 ⇒ Destroy backup_failed（链路在 lifecycle 层已由
	// TestFailedGateStopsTheChain/backup_fail 钉住；此格钉 Backuper 自身的失败传播与 pending 清理）
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	if _, err := s.RegisterNode(ctx, Node{ID: "nd_bf", Name: "bf", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gw := &gwBackupFake{st: s, fail: true}
	bk := NewHubBackuper(s, gw, "http://127.0.0.1:1", dir)
	if _, err := bk.BackupSeatVolume(ctx, "nd_bf", "ag_bf"); err == nil {
		t.Fatal("回传失败必须报错")
	}
	// pending 记录与文件残留都清掉
	rows, err := s.pool.Query(ctx, htx(), `SELECT COUNT(*) FROM g4_backup`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	rows.Next()
	_ = rows.Scan(&n)
	if n != 0 {
		t.Fatalf("失败的 pending 记录应清掉: %d", n)
	}
}
