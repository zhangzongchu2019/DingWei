package nodeagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine 内存假 Docker Engine：记录调用序列、回放各端点、可注入失败（反向用例）。
type fakeEngine struct {
	mu           sync.Mutex
	calls        []string
	specs        []map[string]any // containerCreate 收到的 spec（安全参数清单断言用）
	ids          map[string]string
	next         int
	failAt       string
	unhealthyNew bool // D2 反向：新容器不健康（upgrade 须回滚保留旧）
	srv          *httptest.Server
}

func newFakeDocker(t *testing.T) *fakeEngine {
	f := &fakeEngine{ids: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		key := r.Method + " " + r.URL.Path
		f.calls = append(f.calls, key)
		if f.failAt != "" && strings.Contains(key, f.failAt) {
			http.Error(w, `{"message":"injected failure"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(key, "POST /images/create"):
			w.Write([]byte(`{"status":"ok"}`))
		case strings.HasPrefix(key, "POST /containers/create"):
			var spec map[string]any
			json.NewDecoder(r.Body).Decode(&spec)
			f.specs = append(f.specs, spec)
			f.next++
			id := strings.Repeat("ab", 16) + string(rune('a'+f.next%26))
			name := r.URL.Query().Get("name")
			f.ids[name] = id
			json.NewEncoder(w).Encode(map[string]string{"Id": id})
		case strings.Contains(key, "/start") || strings.Contains(key, "/stop") || strings.Contains(key, "/restart") || strings.Contains(key, "/rename"):
			w.Write([]byte(`{}`))
		case key == "GET /containers/json":
			json.NewEncoder(w).Encode([]map[string]any{
				{"Id": "c1", "Names": []string{"/seat-a"}, "State": "running"},
				{"Id": "c2", "Names": []string{"/seat-b-new"}, "State": "exited"},
				{"Id": "c3", "Names": []string{"/other-x"}, "State": "running"},
			})
		case key == "GET /images/json":
			json.NewEncoder(w).Encode([]map[string]any{{"Size": 104857600}, {"Size": 52428800}})
		case strings.HasPrefix(key, "GET /containers/"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "GET /containers/"), "/json")
			id := name
			for n, i := range f.ids {
				if i == name || n == name || n == strings.TrimSuffix(name, "-new") {
					id = i
				}
			}
			running := true
			if f.unhealthyNew && name == f.ids[func() string {
				for n := range f.ids {
					if strings.HasSuffix(n, "-new") {
						return n
					}
				}
				return ""
			}()] {
				running = false // D2 反向：新容器不健康（以 ID 寻址的 inspect）
			}
			json.NewEncoder(w).Encode(map[string]any{
				"Id": id, "Name": name,
				"State": map[string]any{"Status": map[bool]string{true: "running", false: "created"}[running], "Running": running},
				"Image": "x",
			})
		case strings.HasPrefix(key, "POST /networks/create") || strings.HasPrefix(key, "DELETE /networks/"):
			w.Write([]byte(`{}`))
		case strings.HasPrefix(key, "DELETE /containers/"):
			w.Write([]byte(`{}`))
		default:
			http.Error(w, `{"message":"no route"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEngine) ops(root string) *dockerOps {
	return &dockerOps{
		eng:          newFakeEngine(f.srv.Client(), f.srv.URL),
		quota:        &fakeQuota{},
		seatRoot:     root,
		registry:     "hub.local/dingwei",
		cgroupParent: "dingwei-hosted.slice",
		seatProxy:    "http://172.30.68.1:11080",
		waitMax:      2 * time.Second,
	}
}

const goodImage = "hub.local/dingwei/base@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestValidateImageRef(t *testing.T) {
	if err := validateImageRef(goodImage, "hub.local/dingwei"); err != nil {
		t.Fatalf("合法 ref 应通过: %v", err)
	}
	bad := []string{
		"evil.registry/x/base@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // 仓库不符
		"hub.local/dingwei/base:latest",     // 无 digest
		"hub.local/dingwei/base@sha256:xyz", // digest 非法
		"hub.local/dingwei@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // 无路径
	}
	for _, ref := range bad {
		if err := validateImageRef(ref, "hub.local/dingwei"); err == nil {
			t.Fatalf("应拒: %q", ref)
		}
	}
}

func TestStrictDecodeRejectsArbitraryFields(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	for _, raw := range []string{
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"env":["KEY=x"]}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"mounts":["/:/host"]}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"network":"host"}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"capabilities":["SYS_ADMIN"]}`,
	} {
		if _, err := d.create(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("任意字段必须拒（白名单）: %s", raw)
		}
	}
}

func TestCreateSeatSecuritySpecAndTokenFile(t *testing.T) {
	f := newFakeDocker(t)
	root := t.TempDir()
	d := f.ops(root)
	raw := `{"seat_id":"ag_t1","image":"` + goodImage + `",
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384},
		"token_file":{"application_id":"app1","poll_token":"tok","expires_at":"2026-10-01T12:00:00Z"}}`
	out, err := d.create(context.Background(), json.RawMessage(raw))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var res map[string]string
	json.Unmarshal(out, &res)
	if res["state"] != "running" {
		t.Fatalf("应 running: %s", out)
	}
	if len(f.specs) != 1 {
		t.Fatalf("应有一次 create: %d", len(f.specs))
	}
	spec := f.specs[0]
	hc := spec["HostConfig"].(map[string]any)
	if spec["User"] != "10001:10001" {
		t.Fatal("必须非 root UID")
	}
	// B7 语义精确化（第 12 条②后）：env 里只许代理三键，凭据类（key/token/secret/auth）零出现
	if env, _ := spec["Env"].([]any); len(env) > 0 {
		for _, e := range env {
			s, _ := e.(string)
			if !strings.HasPrefix(s, "HTTP_PROXY=") && !strings.HasPrefix(s, "HTTPS_PROXY=") && !strings.HasPrefix(s, "NO_PROXY=") {
				t.Fatalf("凭据永不经 env（B7），实得: %v", env)
			}
			if strings.Contains(strings.ToLower(s), "key=") || strings.Contains(strings.ToLower(s), "token=") || strings.Contains(strings.ToLower(s), "secret") {
				t.Fatalf("env 不得含凭据形态: %v", env)
			}
		}
	}
	if hc["ReadonlyRootfs"] != true {
		t.Fatal("只读 rootfs")
	}
	mem := hc["Memory"].(float64)
	if int64(mem) != 4096*1024*1024 || hc["MemorySwap"].(float64) != mem {
		t.Fatal("memory＝memory-swap（内存硬限）")
	}
	if hc["CpuShares"].(float64) != 2048 {
		t.Fatal("cpu-shares 映射")
	}
	if capDrop, _ := hc["CapDrop"].([]any); len(capDrop) != 1 || capDrop[0] != "ALL" {
		t.Fatal("capability drop ALL")
	}
	secOpt, _ := hc["SecurityOpt"].([]any)
	if len(secOpt) != 1 || secOpt[0] != "no-new-privileges" {
		t.Fatal("no-new-privileges")
	}
	if hc["Privileged"] != false {
		t.Fatal("非特权")
	}
	if hc["NetworkMode"] != "seatnet-ag_t1" {
		t.Fatal("每席独占网络")
	}
	if binds, _ := hc["Binds"].([]any); len(binds) != 1 || binds[0] != filepath.Join(root, "ag_t1")+":/work" {
		t.Fatalf("bind 只许席位目录: %v", binds)
	}
	// 令牌文件：0600、在席位目录、内容三字段
	tok := filepath.Join(root, "ag_t1", "enroll-token.json")
	fi, err := os.Stat(tok)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("令牌文件 0600: %v", err)
	}
	rawTok, _ := os.ReadFile(tok)
	var tf tokenFile
	if json.Unmarshal(rawTok, &tf) != nil || tf.PollToken != "tok" {
		t.Fatal("令牌文件内容")
	}
}

func TestCreateFailureLeavesNoPartial(t *testing.T) {
	f := newFakeDocker(t)
	f.failAt = "/start"
	root := t.TempDir()
	d := f.ops(root)
	raw := `{"seat_id":"ag_t2","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err == nil {
		t.Fatal("注入启动失败应报错")
	}
	// B3 反向：容器已删、网络已删、配额目录已清
	var sawRemove, sawNetRm bool
	for _, c := range f.calls {
		if strings.HasPrefix(c, "DELETE /containers/") {
			sawRemove = true
		}
		if strings.HasPrefix(c, "DELETE /networks/") {
			sawNetRm = true
		}
	}
	if !sawRemove || !sawNetRm {
		t.Fatalf("失败必须清理（容器+网络）: %v", f.calls)
	}
	q := d.quota.(*fakeQuota)
	if q.dirs[filepath.Join(root, "ag_t2")] {
		t.Fatal("失败必须清配额目录（不留半成品）")
	}
}

func TestUpgradeNewHealthyBeforeOldRemoved(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	raw := `{"seat_id":"ag_t3","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.upgrade(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var idxCreateNew, idxStopOld, idxRemoveOld, idxRename = -1, -1, -1, -1
	for i, c := range f.calls {
		switch {
		case strings.HasPrefix(c, "POST /containers/create") && idxCreateNew == -1:
			idxCreateNew = i
		case strings.Contains(c, "/stop") && idxStopOld == -1:
			idxStopOld = i
		case strings.HasPrefix(c, "DELETE /containers/") && idxRemoveOld == -1:
			idxRemoveOld = i
		case strings.Contains(c, "/rename") && idxRename == -1:
			idxRename = i
		}
	}
	if !(idxCreateNew >= 0 && idxCreateNew < idxStopOld && idxStopOld < idxRemoveOld && idxRemoveOld < idxRename) {
		t.Fatalf("升级顺序必须 起新→停旧→删旧→改名: %v", f.calls)
	}
}

func TestDestroyClearsQuota(t *testing.T) {
	f := newFakeDocker(t)
	root := t.TempDir()
	d := f.ops(root)
	if _, err := d.destroy(context.Background(), json.RawMessage(`{"seat_id":"ag_t4"}`)); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	q := d.quota.(*fakeQuota)
	var sawClear bool
	for _, c := range q.calls {
		if c.Op == "clear" && c.SeatID == "ag_t4" {
			sawClear = true
		}
	}
	if !sawClear {
		t.Fatalf("销毁必须清配额项目: %+v", q.calls)
	}
}

// TestCgroupParentRequiredAndSet 反向用例（组长 eccc54bc）：create 的 --cgroup-parent
// 必须指向配置父切片；漏设（空值）create 直接拒。
func TestCgroupParentRequiredAndSet(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	raw := `{"seat_id":"ag_cg","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	hc := f.specs[0]["HostConfig"].(map[string]any)
	if hc["CgroupParent"] != "dingwei-hosted.slice" {
		t.Fatalf("--cgroup-parent 必须指向配置父切片: %v", hc["CgroupParent"])
	}

	// 漏设父切片 ⇒ create 直接拒（不留下任何引擎调用）
	d2 := f.ops(t.TempDir())
	d2.cgroupParent = ""
	callsBefore := len(f.calls)
	if _, err := d2.create(context.Background(), json.RawMessage(raw)); err == nil {
		t.Fatal("漏设父切片必须拒")
	}
	if len(f.calls) != callsBefore {
		t.Fatal("漏设拒时不得产生任何引擎调用")
	}
}

// ---------- M1 返修 阻断5（B3/D2 判别力）＋席位代理 ----------

// B3 补强：清理自身失败必须上抛（清理都失败而报"已清理"是比残留更糟的假象）。
func TestCreateCleanupFailureIsSurfaced(t *testing.T) {
	f := newFakeDocker(t)
	f.failAt = "/start" // 启动失败触发清理
	root := t.TempDir()
	d := f.ops(root)
	raw := `{"seat_id":"ag_b3","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err == nil {
		t.Fatal("启动失败应报错")
	}
	// 再制造：清理闸失效（quota clear 失败）
	d.quota.(*fakeQuota).failAt = "clear"
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err == nil {
		t.Fatal("应报错")
	} else if !strings.Contains(err.Error(), "清理失败") {
		t.Fatalf("清理失败必须上抛（B3）: %v", err)
	}
}

// D2 补强：完整顺序 start-new → healthy-new(inspect) → stop-old → remove-old → rename；
// 反向：新容器不健康 ⇒ 旧容器全程不动（无 stop/remove 旧）。
func TestUpgradeFullOrderAndRollbackPreservesOld(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	raw := `{"seat_id":"ag_d2","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.upgrade(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	newID := f.ids["seat-ag_d2-new"]
	if newID == "" {
		t.Fatalf("假引擎未建 -new 容器: %v", f.ids)
	}
	var iStartNew, iHealthyNew, iStopOld, iRemoveOld, iRename = -1, -1, -1, -1, -1
	for i, c := range f.calls {
		switch {
		case c == "POST /containers/"+newID+"/start" && iStartNew == -1:
			iStartNew = i
		case c == "GET /containers/"+newID+"/json" && iHealthyNew == -1 && iStartNew >= 0:
			iHealthyNew = i // 新容器健康检查（waitRunning 的 inspect，以新容器 ID 寻址）
		case strings.Contains(c, "/stop") && !strings.Contains(c, newID) && iStopOld == -1 && iHealthyNew >= 0:
			iStopOld = i
		case strings.HasPrefix(c, "DELETE /containers/") && !strings.Contains(c, newID) && iRemoveOld == -1 && iStopOld >= 0:
			iRemoveOld = i
		case strings.Contains(c, "/rename") && iRename == -1 && iRemoveOld >= 0:
			iRename = i
		}
	}
	if !(iStartNew >= 0 && iHealthyNew > iStartNew && iStopOld > iHealthyNew && iRemoveOld > iStopOld && iRename > iRemoveOld) {
		t.Fatalf("顺序必须 起新→健康新→停旧→删旧→改名: %v", f.calls)
	}

	// 反向：新容器不健康 ⇒ 回滚，旧容器全程不动
	f2 := newFakeDocker(t)
	f2.unhealthyNew = true
	d2 := f2.ops(t.TempDir())
	if _, err := d2.upgrade(context.Background(), json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), "rolled_back") {
		t.Fatalf("新不健康应回滚, 得 %v", err)
	}
	newID2 := f2.ids["seat-ag_d2-new"]
	if newID2 == "" {
		t.Fatalf("假引擎未建 -new 容器: %v", f2.ids)
	}
	for _, c := range f2.calls {
		// 旧容器按 seat 名寻址（seat-ag_d2，不含新容器 ID）；新容器的停/删（按 newID2）是回滚的正当事
		if (strings.Contains(c, "/stop") || strings.HasPrefix(c, "DELETE /containers/")) &&
			!strings.Contains(c, newID2) && strings.Contains(c, "seat-ag_d2") {
			t.Fatalf("回滚路径不得动旧容器: %s", c)
		}
	}
}

// 席位代理（第 12 条②）：注入容器 env；漏设 ⇒ create 必拒（与 cgroup 父切片同型的反向）。
func TestSeatProxyInjection(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatProxy = "http://172.30.68.1:11080"
	raw := `{"seat_id":"ag_px","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	env, _ := f.specs[0]["Env"].([]any)
	var sawHTTP, sawHTTPS bool
	for _, e := range env {
		s, _ := e.(string)
		if s == "HTTP_PROXY=http://172.30.68.1:11080" {
			sawHTTP = true
		}
		if s == "HTTPS_PROXY=http://172.30.68.1:11080" {
			sawHTTPS = true
		}
	}
	if !sawHTTP || !sawHTTPS {
		t.Fatalf("代理环境变量须注入容器: %v", env)
	}
	// 反向：漏设 ⇒ 必拒且零引擎调用
	d2 := f.ops(t.TempDir())
	d2.seatProxy = ""
	callsBefore := len(f.calls)
	if _, err := d2.create(context.Background(), json.RawMessage(raw)); err == nil {
		t.Fatal("漏设代理必须拒")
	}
	if len(f.calls) != callsBefore {
		t.Fatal("漏设拒时不得产生任何引擎调用")
	}
}

// T-G4-32：credential_file 写席位目录固定名 account-credential（0600、内容逐字、不经 env）。
func TestCreateWritesCredentialFile(t *testing.T) {
	f := newFakeDocker(t)
	root := t.TempDir()
	d := f.ops(root)
	out, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_cr1","image":"`+goodImage+`",
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384},
		"token_file":{"application_id":"app1","poll_token":"tok","expires_at":"2026-10-01T12:00:00Z"},
		"credential_file":{"content":"sk-test-credential-逐字"}}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var res map[string]string
	_ = json.Unmarshal(out, &res)
	if res["state"] != "running" {
		t.Fatalf("应 running: %s", out)
	}
	raw, err := os.ReadFile(filepath.Join(root, "ag_cr1", "account-credential"))
	if err != nil {
		t.Fatalf("凭据件必须落盘: %v", err)
	}
	if string(raw) != "sk-test-credential-逐字" {
		t.Fatalf("内容须逐字: %q", raw)
	}
	fi, _ := os.Stat(filepath.Join(root, "ag_cr1", "account-credential"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("权限须 0600: %o", fi.Mode().Perm())
	}
	// env 闸不受影响：凭据类零出现（spec 的 Env 只许代理三键——已由 TestCreateSeatSecuritySpecAndTokenFile 钉死）
	if _, err := os.Stat(filepath.Join(root, "ag_cr1", "enroll-token.json")); err != nil {
		t.Fatalf("令牌件应同在: %v", err)
	}
	// 缺省 credential_file ⇒ 不写文件、不报错（旧指令兼容）
	f2 := newFakeDocker(t)
	root2 := t.TempDir()
	d2 := f2.ops(root2)
	if _, err := d2.create(context.Background(), json.RawMessage(`{"seat_id":"ag_cr2","image":"`+goodImage+`",
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384}}`)); err != nil {
		t.Fatalf("无 credential_file 应照常: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root2, "ag_cr2", "account-credential")); !os.IsNotExist(err) {
		t.Fatalf("未下发不得落凭据件: %v", err)
	}
}
