package hubdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []struct {
		To   string
		Body []byte
	}
	fail error
}

func (f *fakeSender) Send(_ context.Context, to string, body []byte) error {
	if f.fail != nil {
		return f.fail
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, struct {
		To   string
		Body []byte
	}{to, body})
	return nil
}

func writeList(t *testing.T, names ...string) *OnlineList {
	t.Helper()
	p := filepath.Join(t.TempDir(), "online.list")
	body := "# 在线清单\n"
	for _, n := range names {
		body += "1. … 全名:" + n + "\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &OnlineList{Path: p}
}

// 正例:活会话绑定→Deliver 出线格式信封。
func TestDeliverWireFormat(t *testing.T) {
	fs := &fakeSender{}
	ol := writeList(t, "zzc-w1-2642")
	a := &Adapter{Registry: []string{"zzc-w1-2642"}, Send: fs, Online: ol, From: "zzc-devo48-2642"}
	w, err := a.Spawn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Deliver(context.Background(), pool.Envelope{NodeID: "n1", Generation: 2, RID: "r-1", Payload: []byte("p")}); err != nil {
		t.Fatal(err)
	}
	var we WireEnvelope
	if err := json.Unmarshal(fs.sent[0].Body, &we); err != nil {
		t.Fatal(err)
	}
	if we.Kind != "mcs_dispatch_v1" || we.NodeID != "n1" || we.Generation != 2 || we.RID != "r-1" || we.From != "zzc-devo48-2642" {
		t.Fatalf("线格式错: %+v", we)
	}
}

// 负例(M1-N1):目标会话不在清单→Deliver 拒,不外呼。
func TestDeliverDeadWorkerRefused(t *testing.T) {
	fs := &fakeSender{}
	ol := writeList(t, "zzc-w1-2642")
	w := &Worker{full: "zzc-dead-2642", send: fs, online: ol, from: "me"}
	if err := w.Deliver(context.Background(), pool.Envelope{NodeID: "n1", RID: "r"}); err == nil {
		t.Fatal("死会话应拒投")
	}
	if len(fs.sent) != 0 {
		t.Fatal("拒投后不得外呼")
	}
}

// 负例(M1-N2):清单过期→Health 报本机 sessionHelper 疑异常(fail-closed,不假活)。
func TestStaleListFailClosed(t *testing.T) {
	ol := writeList(t, "zzc-w1-2642")
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(ol.Path, old, old); err != nil {
		t.Fatal(err)
	}
	w := &Worker{full: "zzc-w1-2642", online: ol}
	if err := w.Health(context.Background()); err == nil {
		t.Fatal("过期清单应 fail-closed")
	}
}

// 负例(M1-N3):registry 全死→Spawn 显式报错,不静默给死绑定(S3 同款:不静默丢)。
func TestSpawnAllDeadExplicit(t *testing.T) {
	ol := writeList(t, "zzc-other-2642")
	a := &Adapter{Registry: []string{"zzc-d1-2642", "zzc-d2-2642"}, Send: &fakeSender{}, Online: ol}
	if _, err := a.Spawn(context.Background()); err == nil {
		t.Fatal("全死 registry 应显式报错")
	}
}

// 负例(M1-N4):Stop 后 Deliver 拒(不向已停用绑定投递)。
func TestStoppedWorkerRefuses(t *testing.T) {
	fs := &fakeSender{}
	ol := writeList(t, "zzc-w1-2642")
	w := &Worker{full: "zzc-w1-2642", send: fs, online: ol}
	_ = w.Stop(context.Background())
	if err := w.Deliver(context.Background(), pool.Envelope{NodeID: "n1", RID: "r"}); err == nil {
		t.Fatal("已停用 worker 应拒投")
	}
}

// 负例(M1-N5):send 外呼失败→Deliver 透传错误(dispatch 层按 at-least-once 语义处置,不吞)。
func TestSendFailurePropagates(t *testing.T) {
	fs := &fakeSender{fail: fmt.Errorf("hub down")}
	ol := writeList(t, "zzc-w1-2642")
	w := &Worker{full: "zzc-w1-2642", send: fs, online: ol}
	if err := w.Deliver(context.Background(), pool.Envelope{NodeID: "n1", RID: "r"}); err == nil {
		t.Fatal("外呼失败必须透传")
	}
}

// 正例:Spawn 轮转跳过死会话绑到活会话。
func TestSpawnSkipsDead(t *testing.T) {
	ol := writeList(t, "zzc-alive-2642")
	a := &Adapter{Registry: []string{"zzc-dead-2642", "zzc-alive-2642"}, Send: &fakeSender{}, Online: ol}
	w, err := a.Spawn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.ID() != "zzc-alive-2642" {
		t.Fatalf("应绑活会话,得 %s", w.ID())
	}
}
