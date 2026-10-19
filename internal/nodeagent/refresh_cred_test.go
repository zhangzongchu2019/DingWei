package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.2.1.101（R-V2-ACCOUNT-PER-USER 第十节）：refresh_cred_seat —— 原子覆盖 account-credential 后重启本席容器。

const rcSeat = "ag_rc1"

func rcSetup(t *testing.T) (*fakeEngine, *dockerOps, string) {
	t.Helper()
	f := newFakeDocker(t)
	root := t.TempDir()
	d := f.ops(root)
	dir := seatDir(root, rcSeat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cred := filepath.Join(dir, "account-credential")
	if err := os.WriteFile(cred, []byte(`{"old":"OLD-SECRET"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, d, cred
}

func rcParams(content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"seat_id": rcSeat, "credential_file": map[string]string{"content": content}})
	return b
}

func rcRestarts(f *fakeEngine) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "POST /containers/") && strings.HasSuffix(c, "/restart") {
			n++
		}
	}
	return n
}

func rcNoTemp(t *testing.T, cred string) {
	t.Helper()
	ents, err := os.ReadDir(filepath.Dir(cred))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".account-credential.") {
			t.Fatalf("不得留临时残件: %s", e.Name())
		}
	}
}

// C9：命令在白名单内且已注册（白名单外命令一律 unknown_command_type，见 agent_test）。
func TestRefreshCredWhitelisted(t *testing.T) {
	if !commandWhitelist[CmdRefreshCredSeat] {
		t.Fatalf("%s 必须在白名单内", CmdRefreshCredSeat)
	}
	f := newFakeDocker(t)
	ag := &Agent{handlers: map[string]Handler{}}
	if err := ag.registerR31LegacyFixture(t, newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://hub.test", "", false, "", "", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := ag.handlers[CmdRefreshCredSeat]; !ok {
		t.Fatalf("%s 未注册处理器", CmdRefreshCredSeat)
	}
}

// 成功：新内容、0600、重启一次、无临时残件。
func TestRefreshCredWritesAndRestarts(t *testing.T) {
	f, d, cred := rcSetup(t)
	if _, err := d.refreshCred(context.Background(), rcParams(`{"new":"NEW-SECRET"}`)); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	b, _ := os.ReadFile(cred)
	if string(b) != `{"new":"NEW-SECRET"}` {
		t.Fatalf("内容未覆盖: %q", b)
	}
	st, _ := os.Stat(cred)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("权限应 0600, 得 %v", st.Mode().Perm())
	}
	if rcRestarts(f) != 1 {
		t.Fatalf("应重启一次: %v", f.calls)
	}
	rcNoTemp(t, cred)
}

// 严格解码：多字段／缺件／空内容／坏 seat_id 一律 ErrParams，不写不重启。
func TestRefreshCredStrictParams(t *testing.T) {
	f, d, cred := rcSetup(t)
	for _, raw := range []string{
		`{"seat_id":"` + rcSeat + `","credential_file":{"content":"x"},"image":"y"}`,
		`{"seat_id":"` + rcSeat + `","credential_file":{"content":"x","path":"/etc/passwd"}}`,
		`{"seat_id":"` + rcSeat + `"}`,
		`{"seat_id":"` + rcSeat + `","credential_file":{"content":""}}`,
		`{"seat_id":"../x","credential_file":{"content":"x"}}`,
	} {
		if _, err := d.refreshCred(context.Background(), json.RawMessage(raw)); !errors.Is(err, ErrParams) {
			t.Fatalf("%s 应 ErrParams, 得 %v", raw, err)
		}
	}
	if b, _ := os.ReadFile(cred); string(b) != `{"old":"OLD-SECRET"}` {
		t.Fatalf("参数错不得改件: %q", b)
	}
	if rcRestarts(f) != 0 {
		t.Fatalf("参数错不得重启: %v", f.calls)
	}
}

// 写失败：前缀 credential_write_failed、旧件原样、无残件、不重启、错误不含内容。
func TestRefreshCredWriteFailureKeepsOld(t *testing.T) {
	f, d, cred := rcSetup(t)
	writeCredHook = func() error { return errors.New("injected write failure") }
	t.Cleanup(func() { writeCredHook = nil })
	_, err := d.refreshCred(context.Background(), rcParams(`{"new":"NEW-SECRET"}`))
	if err == nil || !strings.HasPrefix(err.Error(), "credential_write_failed") {
		t.Fatalf("应 credential_write_failed 前缀, 得 %v", err)
	}
	if strings.Contains(err.Error(), "NEW-SECRET") || strings.Contains(err.Error(), "OLD-SECRET") {
		t.Fatalf("错误不得含凭据内容: %v", err)
	}
	if b, _ := os.ReadFile(cred); string(b) != `{"old":"OLD-SECRET"}` {
		t.Fatalf("写失败旧件须原样: %q", b)
	}
	rcNoTemp(t, cred)
	if rcRestarts(f) != 0 {
		t.Fatalf("写失败不得重启: %v", f.calls)
	}
}

// 重启失败：前缀 restart_failed（新件已落，Hub 侧按 error/start_failed 处理）。
func TestRefreshCredRestartFailure(t *testing.T) {
	f, d, cred := rcSetup(t)
	f.mu.Lock()
	f.failAt = "/restart"
	f.mu.Unlock()
	_, err := d.refreshCred(context.Background(), rcParams(`{"new":"NEW-SECRET"}`))
	if err == nil || !strings.HasPrefix(err.Error(), "restart_failed") {
		t.Fatalf("应 restart_failed 前缀, 得 %v", err)
	}
	if strings.Contains(err.Error(), "NEW-SECRET") {
		t.Fatalf("错误不得含凭据内容: %v", err)
	}
	rcNoTemp(t, cred)
}

// C1 增格（节点侧）：下发内容与现有凭据件相同 ⇒ 照常原子写＋重启、不报错（无「内容未变就跳过」捷径）。
func TestRefreshCredSameContentStillRestarts(t *testing.T) {
	f, d, cred := rcSetup(t)
	if _, err := d.refreshCred(context.Background(), rcParams(`{"old":"OLD-SECRET"}`)); err != nil {
		t.Fatalf("同内容刷新不应报错: %v", err)
	}
	if b, _ := os.ReadFile(cred); string(b) != `{"old":"OLD-SECRET"}` {
		t.Fatalf("内容: %q", b)
	}
	if rcRestarts(f) != 1 {
		t.Fatalf("同内容仍应重启一次: %v", f.calls)
	}
	rcNoTemp(t, cred)
}

// 镜像 r4 摘要标记（R4-ADDENDUM 必改 1 取 A）：刷新先删 .codex 下的标记 ⇒ 下次启动 start.py 必重写 auth.json。
func TestRefreshCredClearsAppliedMarker(t *testing.T) {
	f, d, cred := rcSetup(t)
	codex := filepath.Join(filepath.Dir(cred), ".codex")
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(codex, appliedMarkerName)
	if err := os.WriteFile(marker, []byte("deadbeef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.refreshCred(context.Background(), rcParams(`{"old":"OLD-SECRET"}`)); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("刷新后标记必须已删, lstat=%v", err)
	}
	if rcRestarts(f) != 1 {
		t.Fatalf("应重启一次: %v", f.calls)
	}
}

// .codex 不存在（席位从未起过 Codex）⇒ 照常刷新。
func TestRefreshCredNoCodexHomeOK(t *testing.T) {
	_, d, _ := rcSetup(t)
	if _, err := d.refreshCred(context.Background(), rcParams(`{"new":"N"}`)); err != nil {
		t.Fatalf("无 .codex 应照常: %v", err)
	}
}

// 不跟随链接：.codex 是指向别处的链接 ⇒ 拒（credential_write_failed），别处同名文件不动、凭据件不写、不重启；
// 标记本身是链接 ⇒ 只删链接，链接目标不动。
func TestRefreshCredMarkerNoFollow(t *testing.T) {
	f, d, cred := rcSetup(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, appliedMarkerName)
	if err := os.WriteFile(victim, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(filepath.Dir(cred), ".codex")
	if err := os.Symlink(outside, codex); err != nil {
		t.Fatal(err)
	}
	_, err := d.refreshCred(context.Background(), rcParams(`{"new":"NEW-SECRET"}`))
	if err == nil || !strings.HasPrefix(err.Error(), "credential_write_failed") {
		t.Fatalf(".codex 为链接应 credential_write_failed, 得 %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("链接目标处文件不得被删: %v", err)
	}
	if b, _ := os.ReadFile(cred); string(b) != `{"old":"OLD-SECRET"}` {
		t.Fatalf("拒绝时凭据件不得改: %q", b)
	}
	if rcRestarts(f) != 0 {
		t.Fatalf("拒绝时不得重启: %v", f.calls)
	}

	_ = os.Remove(codex)
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(codex, appliedMarkerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.refreshCred(context.Background(), rcParams(`{"new":"NEW-SECRET"}`)); err != nil {
		t.Fatalf("标记为链接时应只删链接并照常: %v", err)
	}
	if b, err := os.ReadFile(victim); err != nil || string(b) != "host" {
		t.Fatalf("链接目标不得动: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(codex, appliedMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("链接本身应已删: %v", err)
	}
}
