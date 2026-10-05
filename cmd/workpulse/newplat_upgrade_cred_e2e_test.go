//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 端到端（R-V2-UPGRADE-CRED-FIX 必加格①②）：真 Hub（wireNewPlatform，含生产那一行 OnCredentialIssued 接线）
// ＋两个真 sessionhelper 进程共用同一个 /work（同一状态目录、同一令牌文件路径），复现托管升级的凭据轮换：
//
//	A（旧容器）在线 → 冻住 A（SIGSTOP）→ Reissue＋写令牌文件 → B（新容器）入网、取走新凭据并落盘
//	→ 放开 A（SIGCONT）：A 此时才处理 Hub 发来的 4401 → A 退出 → 新凭据文件必须仍在
//	→ 停 B、按同一环境再起 B′（无令牌文件）→ 必须凭盘上凭据入网，不得 exit 2。
//
// 冻住 A 是为了把生产上那一次的先后（新凭据先写盘、旧 helper 后处理断开，见 DOWN-CAUSE）钉成确定顺序；
// 不冻则两边谁先谁后看调度，撤修法时不一定红。撤修法（OnCredentialIssued 改回 DisconnectAgent）⇒ A 收
// credential_revoked、revoke_local 删掉 B 的凭据 ⇒ 本格必红（反向②）。
//
// 需要一个装有 websockets>=15 的 python（helper 依赖）。DW_HELPER_TEST_PYTHON 指定解释器，默认 python3；
// 环境没有就 Skip 并说明原因（只许环境缺失时 Skip）。子进程环境一律显式给，不继承本进程的 SH_*/DINGWEI_*，
// 各自独立 IPC socket——不碰本机在线 helper。
func TestUpgradeRotationKeepsNewCredentialOnSharedWork(t *testing.T) {
	py := os.Getenv("DW_HELPER_TEST_PYTHON")
	if py == "" {
		py = "python3"
	}
	if out, err := exec.Command(py, "-c", "import websockets,sys;assert int(websockets.__version__.split('.')[0])>=15").CombinedOutput(); err != nil {
		t.Skipf("helper 端到端需要装有 websockets>=15 的 python（DW_HELPER_TEST_PYTHON=%q 不满足：%v %s）", py, err, bytes.TrimSpace(out))
	}
	helperDir, err := filepath.Abs(filepath.Join("..", "..", "tools", "sessionhelper"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	hub := newplatHub(t)
	mux := http.NewServeMux()
	idStore := identity.NewMemStore()
	if err := idStore.CreateUser(ctx, identity.User{UserID: "zzc", ShortName: "zzc", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: "https://x"}, idStore, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := hub.Identity

	hi, err := svc.IssueHostedAgentWithNewGroup(ctx, "zzc", "Seat1", "upg", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	agentID := hi.Agent.ID

	tmp := t.TempDir()
	work := filepath.Join(tmp, "work") // 新旧「容器」共用的 /work
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(work, "enroll-token.json")
	stateDir := filepath.Join(work, ".dingwei")
	credFile := filepath.Join(stateDir, "credential.json")
	writeToken := func(tok identity.EnrollToken) {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{
			"application_id": tok.ApplicationID, "poll_token": tok.PollToken,
			"expires_at": tok.ExpiresAt.UTC().Format(time.RFC3339),
		})
		if err := os.WriteFile(tokenFile, raw, 0o600); err != nil { // 同节点 writeTokenFile：先写令牌再起容器
			t.Fatal(err)
		}
	}
	readSecret := func() string {
		raw, err := os.ReadFile(credFile)
		if err != nil {
			return ""
		}
		var c struct {
			Secret string `json:"secret"`
		}
		_ = json.Unmarshal(raw, &c)
		return c.Secret
	}

	type helperProc struct {
		cmd  *exec.Cmd
		log  string
		done chan struct{}
	}
	start := func(tag string) *helperProc {
		t.Helper()
		logPath := filepath.Join(tmp, tag+".log")
		lf, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(py, "-m", "sessionhelper")
		cmd.Dir = helperDir
		cmd.Env = []string{ // 显式环境：不继承 SH_*／DINGWEI_*
			"PATH=" + os.Getenv("PATH"), "HOME=" + work, "PYTHONPATH=" + helperDir, "PYTHONUNBUFFERED=1",
			"SH_WS_BASE=ws" + strings.TrimPrefix(srv.URL, "http"),
			"SH_ENROLL=1", "SH_ENROLL_TOKEN_FILE=" + tokenFile, "SH_STATE_DIR=" + stateDir,
			"SH_MCP_IPC_SOCK=" + filepath.Join(tmp, tag+".sock"),
			"SH_INBOX_DB=" + filepath.Join(tmp, tag+"-inbox.sqlite3"),
			"SH_MODE=echo", "SH_COLLECT=0",
		}
		cmd.Stdout, cmd.Stderr = lf, lf
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		p := &helperProc{cmd: cmd, log: logPath, done: make(chan struct{})}
		go func() { _ = cmd.Wait(); _ = lf.Close(); close(p.done) }()
		t.Cleanup(func() {
			_ = cmd.Process.Signal(syscall.SIGCONT)
			_ = cmd.Process.Kill()
			<-p.done
		})
		return p
	}
	logOf := func(p *helperProc) string { b, _ := os.ReadFile(p.log); return string(b) }
	waitFor := func(what string, d time.Duration, cond func() bool, ps ...*helperProc) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !cond() {
			if time.Now().After(deadline) {
				for _, p := range ps {
					t.Logf("%s log:\n%s", filepath.Base(p.log), logOf(p))
				}
				t.Fatalf("timed out after %v waiting for %s", d, what)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	exited := func(p *helperProc) bool {
		select {
		case <-p.done:
			return true
		default:
			return false
		}
	}

	// ① 首建：A 用令牌入网、凭据落盘、在线
	writeToken(hi.Token)
	a := start("A")
	waitFor("A online with credential on disk", 60*time.Second, func() bool {
		return hub.AgentOnline(agentID) && readSecret() != ""
	}, a)
	secretA := readSecret()
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("token file should be consumed by A: %v", err)
	}

	// ② 升级：冻住 A（旧容器还连着、但来不及处理断开）→ Reissue＋写令牌文件 → 起 B
	if err := a.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	tok, err := svc.ReissueHostedToken(ctx, agentID, "g4")
	if err != nil {
		t.Fatal(err)
	}
	writeToken(*tok)
	b := start("B")
	waitFor("B took the new credential and wrote it to disk", 60*time.Second, func() bool {
		s := readSecret()
		return s != "" && s != secretA
	}, b)
	secretB := readSecret()

	// ③ 放开 A：它现在才处理 Hub 早已发来的 4401 ⇒ 必须只退出、不删共用目录里的（B 的）新凭据
	if err := a.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	waitFor("A exits after the rotation kick", 30*time.Second, func() bool { return exited(a) }, a)
	if code := a.cmd.ProcessState.ExitCode(); code != 3 {
		t.Fatalf("A exit code %d, want 3 (fatal connect error)\n%s", code, logOf(a))
	}
	if got := readSecret(); got != secretB {
		t.Fatalf("new credential on shared /work after old helper exit: present=%v sameAsB=%v (old helper deleted it)\n%s", got != "", got == secretB, logOf(a))
	}
	if strings.Contains(logOf(a), "local credential was removed") {
		t.Fatalf("A ran revoke_local on a rotation kick (credential_revoked)\n%s", logOf(a))
	}
	waitFor("B online", 30*time.Second, func() bool { return hub.AgentOnline(agentID) }, b)

	// ④ 新容器重启（同环境、无令牌文件）：必须凭盘上的凭据入网
	if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitFor("B stops", 30*time.Second, func() bool { return exited(b) }, b)
	waitFor("Hub drops B", 30*time.Second, func() bool { return !hub.AgentOnline(agentID) }, b)
	b2 := start("B2")
	waitFor("B restarted online from the on-disk credential", 60*time.Second, func() bool {
		return hub.AgentOnline(agentID) || exited(b2)
	}, b2)
	if exited(b2) {
		t.Fatalf("B restart exited with %d (want: stays online)\n%s", b2.cmd.ProcessState.ExitCode(), logOf(b2))
	}
	if strings.Contains(logOf(b2), "enroll stopped") {
		t.Fatalf("B restart hit enroll\n%s", logOf(b2))
	}
	if got := readSecret(); got != secretB {
		t.Fatal("credential changed across a plain restart")
	}
}
