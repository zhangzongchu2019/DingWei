package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		case strings.HasPrefix(key, "GET /images/") && strings.HasSuffix(key, "/json") && key != "GET /images/json":
			id := strings.TrimSuffix(strings.TrimPrefix(key, "GET /images/"), "/json")
			if strings.Contains(key, "beefbeef") { // 登记过但本机不存在的探针形状
				http.Error(w, "no such image", 404)
				return
			}
			if strings.Contains(key, "cafecafe") { // 引擎读出的 ID 与 ref 逐字不等（探针形状）
				json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:" + strings.Repeat("00", 32)})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:" + id}) // 引擎惯例带前缀
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
		hubWSBase:    "wss://hub.test",
		localImages:  map[string]bool{strings.Repeat("ab", 32): true},
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
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex","env":["KEY=x"]}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex","mounts":["/:/host"]}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex","network":"host"}`,
		`{"seat_id":"s1","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex","capabilities":["SYS_ADMIN"]}`,
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
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384},"cred_kind":"api_key","cli":"codex",
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
	// B7＋容器启动契约：env 只许代理三键＋契约四键（路径/形态，非凭据）；凭据类零出现
	if env, _ := spec["Env"].([]any); len(env) > 0 {
		allowed := map[string]bool{"HTTP_PROXY=": true, "HTTPS_PROXY=": true, "NO_PROXY=": true,
			"SH_WS_BASE=": true, "SH_ENROLL_TOKEN_FILE=": true, "SH_CRED_FILE=": true, "SH_CRED_KIND=": true,
			"SH_MODE=": true, "SH_CLI=": true}
		seen := map[string]bool{}
		for _, e := range env {
			s, _ := e.(string)
			ok := false
			for p := range allowed {
				if strings.HasPrefix(s, p) {
					ok = true
					seen[p] = true
				}
			}
			if !ok {
				t.Fatalf("凭据永不经 env（B7），实得: %v", env)
			}
		}
		for _, p := range []string{"SH_WS_BASE=", "SH_ENROLL_TOKEN_FILE=", "SH_CRED_FILE=", "SH_CRED_KIND=", "SH_MODE=", "SH_CLI="} {
			if !seen[p] {
				t.Fatalf("契约四键缺 %s: %v", p, env)
			}
		}
		// 契约验收反向：env 任何值不得出现本次下发的令牌内容（值级断言——poll_token 是 "tok"，
		// 若哪天上屏写错把令牌值塞进 env，这格红）
		if joined := fmt.Sprintf("%v", env); strings.Contains(joined, `"tok"`) || strings.Contains(joined, "=tok ") || strings.Contains(joined, "=tok]") {
			t.Fatalf("env 出现令牌内容: %v", env)
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
	raw := `{"seat_id":"ag_t2","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
	raw := `{"seat_id":"ag_t3","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
	raw := `{"seat_id":"ag_cg","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
	raw := `{"seat_id":"ag_b3","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
	raw := `{"seat_id":"ag_d2","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
	raw := `{"seat_id":"ag_px","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`
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
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384},"cred_kind":"api_key","cli":"codex",
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
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":16384},"cred_kind":"api_key","cli":"codex"}`)); err != nil {
		t.Fatalf("无 credential_file 应照常: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root2, "ag_cr2", "account-credential")); !os.IsNotExist(err) {
		t.Fatalf("未下发不得落凭据件: %v", err)
	}
}

// 容器启动契约反向：缺 cred_kind ⇒ create 拒（缺任一键红），零引擎调用。
func TestCreateRejectsMissingCredKind(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	before := len(f.calls)
	_, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_nok","image":"`+goodImage+`",
		"resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},
		"token_file":{"application_id":"a","poll_token":"t","expires_at":"2026-10-02T00:00:00Z"}}`))
	if err == nil || !strings.Contains(err.Error(), "cred_kind") {
		t.Fatalf("缺 cred_kind 必拒: %v", err)
	}
	if len(f.calls) != before {
		t.Fatal("拒绝在引擎调用之前")
	}
}

// local 形态（组长 a26a825f528a：local@sha256:<64 位>，白名单＋inspect 逐字相等，免 pull）。
func TestLocalImageForm(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	registered := strings.Repeat("ab", 32)
	out, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:`+registered+`"}`))
	if err != nil {
		t.Fatalf("登记过的 local 形态应免拉通过: %v", err)
	}
	if !strings.Contains(string(out), "present") {
		t.Fatalf("应 present: %s", out)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "images/create") {
			t.Fatalf("local 形态不得拉取: %v", f.calls)
		}
	}
	// 反向①：未登记的 ID ⇒ 拒
	if _, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:`+strings.Repeat("cd", 32)+`"}`)); err == nil {
		t.Fatal("未登记 ID 必须拒")
	}
	// 反向②：带 tag／短 ID ⇒ 拒
	if _, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:abcd"}`)); err == nil {
		t.Fatal("短 ID 必须拒")
	}
	// 反向③：登记过但本机不存在（inspect 404）⇒ 拒
	missing := strings.Repeat("beef", 16)
	d.localImages[missing] = true
	if _, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:`+missing+`"}`)); err == nil {
		t.Fatal("本机不存在必须拒")
	}
	// 反向④：inspect 读出的 ID 与 ref 逐字不等 ⇒ 拒
	mismatch := strings.Repeat("cafe", 16)
	d.localImages[mismatch] = true
	if _, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:`+mismatch+`"}`)); err == nil {
		t.Fatal("ID 逐字不等必须拒")
	}
}

// 复验关注面：create 直达路径上的 local 形态闸——未登记的本机 ID 不得起容器（不经 pull）。
func TestCreateRejectsUnregisteredLocalImage(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	before := len(f.calls)
	_, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_lk","image":"local@sha256:`+
		strings.Repeat("cd", 32)+`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`))
	if err == nil {
		t.Fatal("未登记本机 ID 不得建席")
	}
	for _, c := range f.calls[before:] {
		if strings.Contains(c, "containers/create") {
			t.Fatalf("未登记镜像不得建容器: %v", f.calls)
		}
	}
}

// 复验 G3：local 形态给引擎的是裸 sha256 ID（不是 local@ 引用——引擎会当仓库摘要 404）。
// 复验 G5：upgrade 缺 cred_kind 即拒（与 create 同口径）。删闸矩阵：upgrade 的 local 白名单格。
func TestLocalFormEngineTranslationAndUpgradeGuards(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	registered := strings.Repeat("ab", 32)
	// G3：create 用 local 形态 ⇒ 引擎 spec 的 Image 是裸 sha256 ID
	if _, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_g3","image":"local@sha256:`+registered+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err != nil {
		t.Fatalf("登记过的 local 形态 create: %v", err)
	}
	if got := f.specs[len(f.specs)-1]["Image"]; got != "sha256:"+registered {
		t.Fatalf("引擎拿到的必须是裸 image ID: %v", got)
	}
	// G5：upgrade 缺 cred_kind ⇒ 拒
	f2 := newFakeDocker(t)
	d2 := f2.ops(t.TempDir())
	if _, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_g5","image":"`+goodImage+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.upgrade(context.Background(), json.RawMessage(`{"seat_id":"ag_g5","image":"`+goodImage+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024}}`)); err == nil || !strings.Contains(err.Error(), "cred_kind") {
		t.Fatalf("upgrade 缺 cred_kind 必拒: %v", err)
	}
	// 删闸格：upgrade 收未登记 local ID ⇒ 拒且旧容器零动作（复验补：拒在引擎动作之前）
	before := len(f2.calls)
	if _, err := d2.upgrade(context.Background(), json.RawMessage(`{"seat_id":"ag_g5","image":"local@sha256:`+
		strings.Repeat("cd", 32)+`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err == nil {
		t.Fatal("upgrade 未登记 local ID 必拒")
	}
	for _, c := range f2.calls[before:] {
		if strings.Contains(c, "/stop") || strings.Contains(c, "/start") || strings.Contains(c, "create") {
			t.Fatalf("被拒的 upgrade 不得动容器: %v", f2.calls[before:])
		}
	}
}

// 复验 RV2 缺口（tosonnet55tc2k fc604d8b）：单路径去 inspect 也必须红——
// 白名单在而 inspect 不在 ⇒ 本机没有该镜像也照起。两格：create 与 upgrade 各自的 inspect 404 必拒。
func TestLocalFormInspectPerPath(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	missing := strings.Repeat("beef", 16) // 白名单里登记、引擎里没有（fake 对 beefbeef 形 404）
	d.localImages[missing] = true
	// create 路径：拒因文本＋零变更调用（复验口径）
	before := len(f.calls)
	err := func() error {
		_, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_ip1","image":"local@sha256:`+missing+
			`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`))
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "本机镜像不存在") {
		t.Fatalf("create inspect 404 必拒，拒因文本：%v", err)
	}
	for _, c := range f.calls[before:] {
		if strings.Contains(c, "containers/create") || strings.Contains(c, "/start") || strings.Contains(c, "/stop") {
			t.Fatalf("被拒不得有变更类引擎调用: %v", f.calls[before:])
		}
	}
	// upgrade 路径：先有一个正常席位，再用不存在的 local ID 升级
	if _, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_ip2","image":"`+goodImage+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err != nil {
		t.Fatal(err)
	}
	before = len(f.calls)
	err = func() error {
		_, err := d.upgrade(context.Background(), json.RawMessage(`{"seat_id":"ag_ip2","image":"local@sha256:`+missing+
			`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`))
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "本机镜像不存在") {
		t.Fatalf("upgrade inspect 404 必拒，拒因文本：%v", err)
	}
	for _, c := range f.calls[before:] {
		if strings.Contains(c, "containers/create") || strings.Contains(c, "/start") || strings.Contains(c, "/stop") {
			t.Fatalf("被拒不得有变更类引擎调用: %v", f.calls[before:])
		}
	}
}

// RV2 余项（tosonnet55tc2k）：ID 不等的单路径格——inspect 返回的 ID 与 ref 逐字不等 ⇒ 必拒，
// create 与 upgrade 各一格（断言拒因文本＋零变更调用）。
func TestLocalFormIDMismatchPerPath(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	mismatch := strings.Repeat("cafe", 16) // fake 引擎对 cafe 形返回全零 ID（逐字不等）
	d.localImages[mismatch] = true
	before := len(f.calls)
	err := func() error {
		_, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_mm1","image":"local@sha256:`+mismatch+
			`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`))
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "逐字不等") {
		t.Fatalf("create ID 不等必拒: %v", err)
	}
	for _, c := range f.calls[before:] {
		if strings.Contains(c, "containers/create") {
			t.Fatalf("被拒不得建容器: %v", f.calls[before:])
		}
	}
	// upgrade 同形
	if _, err := d.create(context.Background(), json.RawMessage(`{"seat_id":"ag_mm2","image":"`+goodImage+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err != nil {
		t.Fatal(err)
	}
	before = len(f.calls)
	if _, err := d.upgrade(context.Background(), json.RawMessage(`{"seat_id":"ag_mm2","image":"local@sha256:`+mismatch+
		`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"codex"}`)); err == nil ||
		!strings.Contains(err.Error(), "逐字不等") {
		t.Fatalf("upgrade ID 不等必拒: %v", err)
	}
	for _, c := range f.calls[before:] {
		if strings.Contains(c, "containers/create") || strings.Contains(c, "/start") || strings.Contains(c, "/stop") {
			t.Fatalf("被拒不得动容器: %v", f.calls[before:])
		}
	}
}

// 组长 R-B12-NOPROXY：席位 NO_PROXY 须含 Hub 主机（精确 IP 或域名，不含端口），回环两项保留。
// 反向：不追加 ⇒ 入网 HTTP 被送进 relay（B12 独立网络里 relay 够不到网关 IP）。
func TestSeatEnvNoProxyIncludesHubHost(t *testing.T) {
	for _, c := range []struct{ base, host string }{
		{"wss://172.31.250.1:8443", "172.31.250.1"},
		{"wss://hub.test", "hub.test"},
		{"wss://[fd00::1]:8443", "fd00::1"},
	} {
		t.Run(c.base, func(t *testing.T) {
			f := newFakeDocker(t)
			d := f.ops(t.TempDir())
			d.hubWSBase = c.base
			var noProxy string
			for _, e := range d.seatEnv("api_key", "codex", "") {
				if v, ok := strings.CutPrefix(e, "NO_PROXY="); ok {
					noProxy = v
				}
			}
			entries := map[string]bool{}
			for _, x := range strings.Split(noProxy, ",") {
				entries[x] = true
			}
			if !entries[c.host] || !entries["localhost"] || !entries["127.0.0.1"] {
				t.Fatalf("NO_PROXY=%q must contain the Hub host %q plus localhost and 127.0.0.1", noProxy, c.host)
			}
			for x := range entries {
				if strings.Contains(x, ":8443") {
					t.Fatalf("NO_PROXY entry %q must not carry a port", x)
				}
			}
		})
	}
}

// R-B12-WHITELIST-FIX 附派：NODEAGENT_LOCAL_IMAGES 启动期归一 sha256: 前缀、格式不对即拒（报键名）。
// 反向：去掉归一 ⇒ 带前缀格红；去掉格式校验 ⇒ 畸形格红。
func TestParseLocalImagesNormalizeAndReject(t *testing.T) {
	id := strings.Repeat("ab", 32)
	// 裸 hex 与 sha256: 前缀都归一到裸 hex 键（后者即 106 image_pull_failed 的根因形态）
	allow, err := ParseLocalImages("sha256:" + id + " , " + id + ",,")
	if err != nil {
		t.Fatalf("合法输入应过: %v", err)
	}
	if len(allow) != 1 || !allow[id] {
		t.Fatalf("归一后应只剩裸 hex 键: %v", allow)
	}
	// 空串＝空白名单（合法——不设 local 形态）
	if allow, err := ParseLocalImages(""); err != nil || len(allow) != 0 {
		t.Fatalf("空串应得空白名单: %v %v", allow, err)
	}
	// 畸形逐项拒，错误须点名键名
	for _, bad := range []string{
		"abcd",                    // 短
		strings.Repeat("zz", 32),  // 非 hex
		strings.Repeat("AB", 32),  // 大写（engine ID 是小写，fail-closed）
		"local@sha256:" + id,      // 镜像引用形态不是白名单形态
		"SHA256:" + id,            // 大写前缀不归一（大小写敏感，fail-closed）
		"sha256:" + id + "@extra", // 尾巴脏
	} {
		if _, err := ParseLocalImages(bad); err == nil || !strings.Contains(err.Error(), "NODEAGENT_LOCAL_IMAGES") {
			t.Fatalf("%q 必须拒且报键名: %v", bad, err)
		}
	}
}

// 长度闸 fail-closed：64 位以外的都拒，重点钉 65 位上界
// （复验存活刀 P3：长度判断改成「< 64」时 65 位会漏过，本格必须红）。
func TestParseLocalImagesRejectsWrongLength(t *testing.T) {
	id := strings.Repeat("ab", 32) // 64 位基准
	for _, bad := range []string{
		id[:63],              // 63 位
		id + "f",             // 65 位（P3 存活刀的漏口）
		"sha256:" + id + "f", // 带前缀的 65 位（剥前缀后仍须拒）
	} {
		if _, err := ParseLocalImages(bad); err == nil || !strings.Contains(err.Error(), "NODEAGENT_LOCAL_IMAGES") {
			t.Fatalf("%q（剥前缀后 %d 位）必须拒且报键名: %v", bad, len(strings.TrimPrefix(bad, "sha256:")), err)
		}
	}
}

// 坏白名单必须向上报错让调用方拒启
// （复验存活刀 P5：RegisterDockerHandlers 若吞掉 ParseLocalImages 的错误返回 nil，本格必须红）。
func TestRegisterDockerHandlersRejectsBadAllowlist(t *testing.T) {
	f := newFakeDocker(t)
	register := func(csv string) error {
		return (&Agent{handlers: map[string]Handler{}}).RegisterDockerHandlers(
			newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
			t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
			"http://172.30.68.1:11080", "wss://hub.test", csv, false, "", "", nil,
		)
	}
	if err := register(strings.Repeat("ab", 32) + "f"); err == nil || !strings.Contains(err.Error(), "NODEAGENT_LOCAL_IMAGES") {
		t.Fatalf("坏白名单（65 位）必须返回错误并点名键名: %v", err)
	}
	// 对照：好白名单注册成功——错误路径不是「逢调必错」
	if err := register(strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("好白名单应注册成功: %v", err)
	}
}

// R-SEAT-HUB-VIA-PROXY 案②：开关开 ⇒ 席位 env 含 SH_PROXY＝seatProxy 且 NO_PROXY 不含 Hub 主机
// （两个动作缺一不可——a 管入网 HTTP、b 管 WS；helper WS 只认 SH_PROXY）。
// 反向：去掉 SH_PROXY 注入 ⇒ 红；恢复 NO_PROXY 追加 ⇒ 红。
func TestSeatEnvHubViaProxyOn(t *testing.T) {
	for _, hubWSBase := range []string{
		"wss://dw.cmjrt.com:55055",  // 域名
		"wss://58.251.94.69:55055",  // IPv4
		"wss://[2001:db8::1]:55055", // IPv6（hubHost 须剥方括号）
	} {
		t.Run(hubWSBase, func(t *testing.T) {
			f := newFakeDocker(t)
			d := f.ops(t.TempDir())
			d.seatProxy = "http://172.30.68.1:11080"
			d.hubWSBase = hubWSBase
			d.seatHubViaProxy = true
			env := d.seatEnv("api_key", "codex", "")
			joined := "\n" + strings.Join(env, "\n") + "\n"
			for _, want := range []string{
				"\nSH_PROXY=http://172.30.68.1:11080\n",
				"\nHTTP_PROXY=http://172.30.68.1:11080\n",
				"\nHTTPS_PROXY=http://172.30.68.1:11080\n",
				"\nNO_PROXY=localhost,127.0.0.1\n", // 精确：恰这两个，不含 Hub
			} {
				if !strings.Contains(joined, want) {
					t.Fatalf("开 ⇒ 缺精确项 %q: %v", want, env)
				}
			}
		})
	}
}

// 复验预审第三层：走真 create，Docker create spec 的 Env 必须真含开态结果（不只内部函数）。
func TestSeatEnvHubViaProxyInCreateSpec(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatProxy = "http://172.30.68.1:11080"
	d.hubWSBase = "wss://dw.cmjrt.com:55055"
	d.seatHubViaProxy = true
	raw := `{"seat_id":"ag_vp","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(f.specs) != 1 {
		t.Fatalf("应有一次 create: %d", len(f.specs))
	}
	envs, _ := f.specs[0]["Env"].([]any)
	joined := "\n"
	for _, e := range envs {
		v, _ := e.(string)
		joined += v + "\n"
	}
	if !strings.Contains(joined, "\nSH_PROXY=http://172.30.68.1:11080\n") {
		t.Fatalf("create spec 的 Env 必须含 SH_PROXY: %v", envs)
	}
	if !strings.Contains(joined, "\nNO_PROXY=localhost,127.0.0.1\n") {
		t.Fatalf("create spec 的 NO_PROXY 必须恰为 localhost,127.0.0.1: %v", envs)
	}
}

// 开关关 ⇒ 与 ebd17f43 现行为逐字相同（B12／本机 Hub 场景不受冲击）。
func TestSeatEnvHubViaProxyOffUnchanged(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatProxy = "http://172.30.68.1:11080"
	d.hubWSBase = "wss://dw.cmjrt.com:55055"
	d.seatHubViaProxy = false
	env := d.seatEnv("api_key", "codex", "")
	want := []string{
		"SH_WS_BASE=wss://dw.cmjrt.com:55055",
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=api_key",
		"SH_MODE=cli",
		"SH_CLI=codex",
		"HTTP_PROXY=http://172.30.68.1:11080",
		"HTTPS_PROXY=http://172.30.68.1:11080",
		"NO_PROXY=localhost,127.0.0.1,dw.cmjrt.com",
	}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Fatalf("关 ⇒ 与 ebd17f43 逐字不同:\n得 %v\n要 %v", env, want)
	}
}

// 带前缀的白名单配置（106 的失误形态）经归一后，local 形态闸照常放行登记镜像。
func TestLocalImageGateWithPrefixedAllowlist(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	registered := strings.Repeat("ab", 32)
	allow, err := ParseLocalImages("sha256:" + registered)
	if err != nil {
		t.Fatal(err)
	}
	d.localImages = allow
	out, err := d.pull(context.Background(), json.RawMessage(`{"image":"local@sha256:`+registered+`"}`))
	if err != nil || !strings.Contains(string(out), "present") {
		t.Fatalf("前缀归一后登记镜像应免拉通过: %v %s", err, out)
	}
}

// 组长 R-B12-SEATMODE 第 2 条（取乙）：席位 env 按 create／upgrade 参数 cli 注入 SH_MODE=cli、SH_CLI=<cli>——
// sessionHelper 未设 SH_MODE 默认 echo（不起任何 CLI）。反向：不注入 ⇒ 红；cli 缺或不在白名单 ⇒ create／upgrade 拒且零引擎调用。
func TestSeatEnvInjectsCLIMode(t *testing.T) {
	envOf := func(t *testing.T, f *fakeEngine) map[string]string {
		t.Helper()
		got := map[string]string{}
		env, _ := f.specs[len(f.specs)-1]["Env"].([]any)
		for _, e := range env {
			s, _ := e.(string)
			if k, v, ok := strings.Cut(s, "="); ok {
				got[k] = v
			}
		}
		return got
	}
	for _, cli := range []string{"codex", "claude"} {
		t.Run("create "+cli, func(t *testing.T) {
			f := newFakeDocker(t)
			d := f.ops(t.TempDir())
			raw := `{"seat_id":"ag_cm","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"` + cli + `"}`
			if _, err := d.create(context.Background(), json.RawMessage(raw)); err != nil {
				t.Fatalf("create: %v", err)
			}
			if got := envOf(t, f); got["SH_MODE"] != "cli" || got["SH_CLI"] != cli {
				t.Fatalf("seat env SH_MODE=%q SH_CLI=%q, want cli/%s", got["SH_MODE"], got["SH_CLI"], cli)
			}
		})
	}
	for name, tail := range map[string]string{"missing": ``, "unknown": `,"cli":"bash"`, "empty": `,"cli":""`} {
		t.Run("create rejects "+name, func(t *testing.T) {
			f := newFakeDocker(t)
			d := f.ops(t.TempDir())
			raw := `{"seat_id":"ag_cm","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key"` + tail + `}`
			if _, err := d.create(context.Background(), json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), "cli") {
				t.Fatalf("cli %s must be rejected: %v", name, err)
			}
			if len(f.specs) != 0 {
				t.Fatalf("rejected create must not reach the engine: %d specs", len(f.specs))
			}
		})
	}
	t.Run("upgrade rejects missing cli", func(t *testing.T) {
		f := newFakeDocker(t)
		d := f.ops(t.TempDir())
		raw := `{"seat_id":"ag_cm","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key"}`
		if _, err := d.upgrade(context.Background(), json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), "cli") {
			t.Fatalf("upgrade without cli must be rejected: %v", err)
		}
	})
}

// 组长 R-SEAT-CLI-STRICT（tosonnet55tc2k 复验 716ee4c8 存活一刀：校验改成小写＋去空白后比对，原用例全绿）：
// cli 必须与白名单逐字相等——大小写不同、带首尾空白一律拒，create 与 upgrade 同口径，且拒在任何引擎调用之前。
// 镜像 start.py（prodops/hosted-image-20261002）对 SH_CLI 逐字比对 codex，节点若放过「Codex」「codex 」，要到容器里才拒启。
func TestSeatCLIMustMatchAllowlistExactly(t *testing.T) {
	for name, cli := range map[string]string{"upper": "Codex", "trailing space": "codex ", "leading space": " codex", "all caps": "CODEX"} {
		for _, op := range []string{"create", "upgrade"} {
			t.Run(op+" rejects "+name, func(t *testing.T) {
				f := newFakeDocker(t)
				d := f.ops(t.TempDir())
				raw := `{"seat_id":"ag_cs","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":1024},"cred_kind":"api_key","cli":"` + cli + `"}`
				var err error
				if op == "create" {
					_, err = d.create(context.Background(), json.RawMessage(raw))
				} else {
					_, err = d.upgrade(context.Background(), json.RawMessage(raw))
				}
				if err == nil || !strings.Contains(err.Error(), "cli") {
					t.Fatalf("%s cli=%q must be rejected: %v", op, cli, err)
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.calls) != 0 || len(f.specs) != 0 {
					t.Fatalf("%s cli=%q rejected but reached the engine: calls=%v", op, cli, f.calls)
				}
			})
		}
	}
}

// 透传层（复验中期存活刀）：RegisterDockerHandlers 的 seatHubViaProxy 参数必须真进 dockerOps——
// 用 Agent 注册（true）后走 ag.handlers[CmdCreateSeat]，从 fake Docker 的 create spec Env 断言。
// 反向：RegisterDockerHandlers 构造 dockerOps 时把该字段写 false ⇒ 本格红（此前直改 dockerOps 的格全部存活）。
func TestHubViaProxyThroughRegisterDockerHandlers(t *testing.T) {
	f := newFakeDocker(t)
	ag := &Agent{handlers: map[string]Handler{}}
	if err := ag.RegisterDockerHandlers(
		newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://dw.cmjrt.com:55055", "", true, "", "", nil,
	); err != nil {
		t.Fatalf("register: %v", err)
	}
	h, ok := ag.handlers[CmdCreateSeat]
	if !ok {
		t.Fatal("CmdCreateSeat 未注册")
	}
	raw := `{"seat_id":"ag_tp","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := h(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(f.specs) != 1 {
		t.Fatalf("应有一次 create: %d", len(f.specs))
	}
	envs, _ := f.specs[0]["Env"].([]any)
	joined := "\n"
	for _, e := range envs {
		v, _ := e.(string)
		joined += v + "\n"
	}
	for _, want := range []string{
		"\nSH_PROXY=http://172.30.68.1:11080\n",
		"\nHTTP_PROXY=http://172.30.68.1:11080\n",
		"\nHTTPS_PROXY=http://172.30.68.1:11080\n",
		"\nNO_PROXY=localhost,127.0.0.1\n",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("透传后 create spec 的 Env 缺精确项 %q: %v", want, envs)
		}
	}
}

// R-SEAT-MODEL-NODEAGENT-20261003014344（业主方案 1，托管落实 gpt-6.1-sol）：
// 开（模型值非空）⇒ 席位 env 含 CODEX_MODEL=<值>；空 ⇒ 与 597b4d93 逐字相同（不注入）。
// 校验：非空非法（字符集外／超长）⇒ RegisterDockerHandlers 报错拒启（报键名）。
// 反向：去掉注入 ⇒ 开态格红；去掉校验 ⇒ 校验格红。
func TestSeatCodexModelInject(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatCodexModel = "gpt-6.1-sol"
	env := d.seatEnv("api_key", "codex", "")
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nCODEX_MODEL=gpt-6.1-sol\n") {
		t.Fatalf("开 ⇒ 必须含 CODEX_MODEL: %v", env)
	}

	// 空 ⇒ 逐字不变（与 597b4d93 的期望切片逐字比对）
	d2 := f.ops(t.TempDir())
	env2 := d2.seatEnv("api_key", "codex", "")
	want := []string{
		"SH_WS_BASE=wss://hub.test",
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=api_key",
		"SH_MODE=cli",
		"SH_CLI=codex",
		"HTTP_PROXY=http://172.30.68.1:11080",
		"HTTPS_PROXY=http://172.30.68.1:11080",
		"NO_PROXY=localhost,127.0.0.1,hub.test",
	}
	if strings.Join(env2, "|") != strings.Join(want, "|") {
		t.Fatalf("空 ⇒ 与 597b4d93 逐字不同:\n得 %v\n要 %v", env2, want)
	}
}

// 校验格：非法值 ⇒ RegisterDockerHandlers 报错并点名键名（启动失败路径）；合法值过。
func TestSeatCodexModelValidation(t *testing.T) {
	f := newFakeDocker(t)
	register := func(model string) error {
		return (&Agent{handlers: map[string]Handler{}}).RegisterDockerHandlers(
			newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
			t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
			"http://172.30.68.1:11080", "wss://hub.test", "", false, model, "", nil,
		)
	}
	for _, bad := range []string{
		"gpt/6.1",               // 斜杠（路径注入面）
		"gpt 6.1",               // 空格
		" ",                     // 纯空白＝非法（口径对齐 01:50:12Z：与镜像侧一致拒启，不 trim 成不指定）
		"   ",                   // 纯空白（多空格）
		"\t",                    // 纯空白（tab）
		" \t ",                  // 纯空白（空格＋tab 混合）
		"gpt:",                  // 冒号
		" gpt-6.1-sol",          // 首空白（不 trim——值须逐字）
		"gpt-6.1-sol ",          // 尾空白
		"gpt-模型",                // 非 ASCII
		strings.Repeat("a", 65), // 超长
		"gpt;rm",                // 分号
		"gpt$(x)",               // shell 展开形
		"gpt-6.1-sol\nMAL=1",    // 换行（env 注入面）
	} {
		ag := &Agent{handlers: map[string]Handler{}}
		err := ag.RegisterDockerHandlers(
			newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
			t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
			"http://172.30.68.1:11080", "wss://hub.test", "", false, bad, "", nil,
		)
		if err == nil || !strings.Contains(err.Error(), "NODEAGENT_SEAT_CODEX_MODEL") {
			t.Fatalf("%q 必须拒且报键名: %v", bad, err)
		}
		if len(ag.handlers) != 0 { // 校验在注册前 ⇒ 不得部分注册（预审③）
			t.Fatalf("%q 被拒后 handlers 应为空，实 %d", bad, len(ag.handlers))
		}
	}
	for _, ok := range []string{"", "gpt-6.1-sol", "claude_opus_4.8", "a"} {
		if err := register(ok); err != nil {
			t.Fatalf("%q 应放行: %v", ok, err)
		}
	}
}

// 透传层（复验预审①②）：RegisterDockerHandlers(model) → handlers[CmdCreateSeat] → create spec。
// 开态：CODEX_MODEL 精确出现一次、值不 trim/改写（gpt-6.1-sol＋1 字符＋64 字符边界）。
// 空态：spec Env 不出现 CODEX_MODEL（与 seatEnv 空态逐字格互补，守 handler 入口）。
func TestSeatCodexModelThroughCreateSpec(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
	}{
		{"prod value", "gpt-6.1-sol"},
		{"1 char boundary", "a"},
		{"64 char boundary", strings.Repeat("g", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDocker(t)
			ag := &Agent{handlers: map[string]Handler{}}
			if err := ag.RegisterDockerHandlers(
				newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
				t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
				"http://172.30.68.1:11080", "wss://hub.test", "", false, tc.model, "", nil,
			); err != nil {
				t.Fatalf("register: %v", err)
			}
			raw := `{"seat_id":"ag_cm","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
			if _, err := ag.handlers[CmdCreateSeat](context.Background(), json.RawMessage(raw)); err != nil {
				t.Fatalf("create: %v", err)
			}
			if len(f.specs) != 1 {
				t.Fatalf("应有一次 create: %d", len(f.specs))
			}
			envs, _ := f.specs[0]["Env"].([]any)
			hits := 0
			for _, e := range envs {
				if v, _ := e.(string); strings.HasPrefix(v, "CODEX_MODEL=") {
					hits++
					if v != "CODEX_MODEL="+tc.model {
						t.Fatalf("值不 trim/改写: %q ≠ %q", v, "CODEX_MODEL="+tc.model)
					}
				}
			}
			if hits != 1 {
				t.Fatalf("CODEX_MODEL 须精确出现一次，实 %d: %v", hits, envs)
			}
		})
	}
	// 空态（预审②）：spec Env 不出现 CODEX_MODEL
	f := newFakeDocker(t)
	ag := &Agent{handlers: map[string]Handler{}}
	if err := ag.RegisterDockerHandlers(
		newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://hub.test", "", false, "", "", nil,
	); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw := `{"seat_id":"ag_cm0","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := ag.handlers[CmdCreateSeat](context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, e := range f.specs[len(f.specs)-1]["Env"].([]any) {
		if v, _ := e.(string); strings.HasPrefix(v, "CODEX_MODEL=") {
			t.Fatalf("空态不得出现 CODEX_MODEL: %v", e)
		}
	}
}

// R-V2-HOSTED-DRIVER-IMPL-20261004143130（版本 2 指标 2：cli 模式不产结构事件、view 看不到回复）：
// NODEAGENT_SEAT_MODE 未设/cli ⇒ 与现行逐字相同；driver ⇒ SH_MODE=driver＋SH_EMIT_SESSION_EVENT=1；
// 非法值 ⇒ 拒启报键名。反向：吃掉模式值（注册层恒 cli）／删校验 ⇒ 各红。
func TestSeatModeSwitch(t *testing.T) {
	// 未设（＝cli）逐字现行
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	env := d.seatEnv("api_key", "codex", "")
	want := []string{
		"SH_WS_BASE=wss://hub.test",
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=api_key",
		"SH_MODE=cli",
		"SH_CLI=codex",
		"HTTP_PROXY=http://172.30.68.1:11080",
		"HTTPS_PROXY=http://172.30.68.1:11080",
		"NO_PROXY=localhost,127.0.0.1,hub.test",
	}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Fatalf("未设 ⇒ 与现行逐字不同:\n得 %v\n要 %v", env, want)
	}
	// 显式 cli 同样逐字
	d2 := f.ops(t.TempDir())
	d2.seatMode = "cli"
	if strings.Join(d2.seatEnv("api_key", "codex", ""), "|") != strings.Join(want, "|") {
		t.Fatal("显式 cli 应同未设逐字")
	}
	// driver＋codex 席位：SH_MODE=driver＋SH_DRIVER=codex＋事件键，其余键不变
	d3 := f.ops(t.TempDir())
	d3.seatMode = "driver"
	env3 := d3.seatEnv("api_key", "codex", "")
	joined := "\n" + strings.Join(env3, "\n") + "\n"
	for _, w := range []string{"\nSH_MODE=driver\n", "\nSH_DRIVER=codex\n", "\nSH_EMIT_SESSION_EVENT=1\n", "\nSH_CLI=codex\n"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("driver ⇒ 缺 %q: %v", w, env3)
		}
	}
	if strings.Contains(joined, "SH_MODE=cli") {
		t.Fatalf("driver ⇒ 不得再含 SH_MODE=cli: %v", env3)
	}
	// 其余键逐项不变（复验 M5 存活刀：driver 丢掉 HTTPS_PROXY 之类须红）——
	// driver env 去掉 SH_MODE 与两个新键后，与未设 env 逐项相同。
	strip := func(env []string) []string {
		out := []string{}
		for _, e := range env {
			if strings.HasPrefix(e, "SH_MODE=") || strings.HasPrefix(e, "SH_DRIVER=") || strings.HasPrefix(e, "SH_EMIT_SESSION_EVENT=") {
				continue
			}
			out = append(out, e)
		}
		return out
	}
	if strings.Join(strip(env3), "|") != strings.Join(strip(env), "|") {
		t.Fatalf("driver 的其余键必须与未设逐项相同:\n得 %v\n要 %v", strip(env3), strip(env))
	}
	// 异议 1（组长采纳）：driver 对非 codex 席位不生效——claude 席位与现行逐字相同
	d4 := f.ops(t.TempDir())
	d4.seatMode = "driver"
	env4 := d4.seatEnv("api_key", "claude", "")
	wantClaude := []string{
		"SH_WS_BASE=wss://hub.test",
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=api_key",
		"SH_MODE=cli",
		"SH_CLI=claude",
		"HTTP_PROXY=http://172.30.68.1:11080",
		"HTTPS_PROXY=http://172.30.68.1:11080",
		"NO_PROXY=localhost,127.0.0.1,hub.test",
	}
	if strings.Join(env4, "|") != strings.Join(wantClaude, "|") {
		t.Fatalf("claude 席位 driver 下应与现行逐字相同:\n得 %v\n要 %v", env4, wantClaude)
	}
	// 非法值拒启报键名（注册前，不部分注册）
	for _, bad := range []string{"echo", "DRIVER", " driver", "cli2"} {
		ag := &Agent{handlers: map[string]Handler{}}
		err := ag.RegisterDockerHandlers(
			newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
			t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
			"http://172.30.68.1:11080", "wss://hub.test", "", false, "", bad, nil,
		)
		if err == nil || !strings.Contains(err.Error(), "NODEAGENT_SEAT_MODE") {
			t.Fatalf("%q 必须拒且报键名: %v", bad, err)
		}
		if len(ag.handlers) != 0 {
			t.Fatalf("%q 被拒后 handlers 应为空", bad)
		}
	}
}

// 透传层：RegisterDockerHandlers(..., "driver") → handlers[CmdCreateSeat] → spec Env 真含两键。
func TestSeatModeDriverThroughCreateSpec(t *testing.T) {
	f := newFakeDocker(t)
	ag := &Agent{handlers: map[string]Handler{}}
	if err := ag.RegisterDockerHandlers(
		newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://hub.test", "", false, "", "driver", nil,
	); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw := `{"seat_id":"ag_dm","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := ag.handlers[CmdCreateSeat](context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	envs, _ := f.specs[0]["Env"].([]any)
	joined := "\n"
	for _, e := range envs {
		v, _ := e.(string)
		joined += v + "\n"
	}
	for _, w := range []string{"\nSH_MODE=driver\n", "\nSH_EMIT_SESSION_EVENT=1\n"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("透传后 spec Env 缺 %q: %v", w, envs)
		}
	}
}

// 包 B2 节点侧：按席 model 校验（格式＋清单纵深）与注入优先级（席＞节点默认＞不注）。
// 反向：去清单校验 ⇒ 红；优先级颠倒 ⇒ 红。
func TestSeatModelParamValidation(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatModels = []string{"gpt-6.1-sol", "gpt-5.5"}
	mk := func(model string) json.RawMessage {
		return json.RawMessage(`{"seat_id":"ag_m","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex","model":"` + model + `"}`)
	}
	if _, err := d.create(context.Background(), mk("gpt-6.1-sol")); err != nil {
		t.Fatalf("清单内应过: %v", err)
	}
	if _, err := d.create(context.Background(), mk("gpt/6")); err == nil {
		t.Fatal("格式坏应拒")
	}
	if _, err := d.create(context.Background(), mk("gpt-9")); err == nil {
		t.Fatal("清单外应拒")
	}
	// 口径 3（复验预审）：清单空＝不支持选型——带 model 一律拒（fail-closed，Hub 歪斜时兜住）
	d2 := f.ops(t.TempDir())
	d2.seatModels = nil
	if _, err := d2.create(context.Background(), mk("gpt-9")); err == nil {
		t.Fatal("无清单节点带 model 必须拒")
	}
	// 老形态请求（无 model 字段）仍成功（滚动发版：老 Hub 先发、新节点须兼容；strictDecode 不拒缺省）
	d3 := f.ops(t.TempDir())
	d3.seatModels = []string{"gpt-6.1-sol"}
	oldShape := json.RawMessage(`{"seat_id":"ag_m","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`)
	if _, err := d3.create(context.Background(), oldShape); err != nil {
		t.Fatalf("老形态（无 model）必须成功: %v", err)
	}
}

func TestSeatModelPriority(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	d.seatCodexModel = "gpt-6.1-sol" // 节点默认
	// 席位值覆盖节点默认
	env := d.seatEnv("api_key", "codex", "gpt-5.5")
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nCODEX_MODEL=gpt-5.5\n") {
		t.Fatalf("席位值应覆盖节点默认: %v", env)
	}
	// 席位空 ⇒ 节点默认
	env2 := d.seatEnv("api_key", "codex", "")
	if !strings.Contains("\n"+strings.Join(env2, "\n")+"\n", "\nCODEX_MODEL=gpt-6.1-sol\n") {
		t.Fatalf("席位空应落节点默认: %v", env2)
	}
}

// 口径 3 启动期：清单非空时节点默认（NODEAGENT_SEAT_CODEX_MODEL）必须是清单成员，否则拒启报键名。
func TestNodeDefaultMustBeInModelsList(t *testing.T) {
	f := newFakeDocker(t)
	err := (&Agent{handlers: map[string]Handler{}}).RegisterDockerHandlers(
		newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://hub.test", "", false, "gpt-6.1-sol", "", []string{"gpt-5.5"},
	)
	if err == nil || !strings.Contains(err.Error(), "NODEAGENT_SEAT_CODEX_MODEL") {
		t.Fatalf("默认不在清单必须拒启报键名: %v", err)
	}
	// 在清单 ⇒ 过
	if err := (&Agent{handlers: map[string]Handler{}}).RegisterDockerHandlers(
		newFakeEngine(f.srv.Client(), f.srv.URL), &fakeQuota{},
		t.TempDir(), "hub.local/dingwei", "dingwei-hosted.slice",
		"http://172.30.68.1:11080", "wss://hub.test", "", false, "gpt-6.1-sol", "", []string{"gpt-6.1-sol"},
	); err != nil {
		t.Fatalf("在清单应过: %v", err)
	}
}

// B1（包 B1）：席位容器 HostConfig.Init=true（tini 式 init——停席不再白等 10s 强杀）。
// 反向：去掉该键 ⇒ 本格红。真机读数（docker stop<2s、退出码非 137）属部署验证格。
func TestCreateSeatInitEnabled(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	raw := `{"seat_id":"ag_init","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := d.create(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("create: %v", err)
	}
	hc, _ := f.specs[0]["HostConfig"].(map[string]any)
	if hc["Init"] != true {
		t.Fatalf("HostConfig.Init 必须 true: %v", hc["Init"])
	}
}

// 复验缺口（tosonnet55tc2k）：upgrade 带 model ⇒ spec Env 含 CODEX_MODEL＝该值；坏格式 ⇒ ErrParams。
// 反向：去注入／去校验各红。
func TestUpgradeModelInjectAndValidate(t *testing.T) {
	f := newFakeDocker(t)
	// 好值：upgrade 后新容器 spec 带 CODEX_MODEL
	d := f.ops(t.TempDir())
	// 先建一席（upgrade 需要旧容器存在）
	create := `{"seat_id":"ag_up","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex"}`
	if _, err := d.create(context.Background(), json.RawMessage(create)); err != nil {
		t.Fatalf("create: %v", err)
	}
	up := `{"seat_id":"ag_up","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex","model":"gpt-6.1-sol"}`
	if _, err := d.upgrade(context.Background(), json.RawMessage(up)); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	last := f.specs[len(f.specs)-1]
	envs, _ := last["Env"].([]any)
	joined := "\n"
	for _, e := range envs {
		v, _ := e.(string)
		joined += v + "\n"
	}
	if !strings.Contains(joined, "\nCODEX_MODEL=gpt-6.1-sol\n") {
		t.Fatalf("upgrade 后 spec Env 须含 CODEX_MODEL＝记录值: %v", envs)
	}
	// 坏格式 ⇒ ErrParams
	bad := `{"seat_id":"ag_up","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"api_key","cli":"codex","model":"gpt/6"}`
	if _, err := d.upgrade(context.Background(), json.RawMessage(bad)); !errors.Is(err, ErrParams) {
		t.Fatalf("坏格式应 ErrParams: %v", err)
	}
}
