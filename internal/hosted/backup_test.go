package hosted

// T-G4-53 备份管线用例：全链（pending→export_seat→回传落盘→complete→Destroy 拿 ref）、
// 鉴权（别节点凭据/未知 id 拒）、半截文件不标记、TTL 到期清扫；
// 返修（台账 lead-g4-t53-review，opus50tc2k）：完整性两道（sha256 trailer、gzip＋tar 解码）、失败分支一律清 pending。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// mkTarGz 造一个真 tar.gz（条目名→内容）。
func mkTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// uploadBackup 扮演节点回传：流式 POST，sha256 走请求 trailer（与 nodeagent 同一协议）。
// sha 为 "-" 表示不带 trailer。
func uploadBackup(ctx context.Context, url, secret string, body []byte, sha string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return 0, err
	}
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write(body); _ = pw.Close() }()
	if sha != "-" {
		req.Trailer = http.Header{BackupSHA256Trailer: nil}
		req.Body = io.NopCloser(&eofHook{r: pr, onEOF: func() { req.Trailer.Set(BackupSHA256Trailer, sha) }})
	} else {
		req.Body = pr
	}
	req.ContentLength = -1
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

type eofHook struct {
	r     io.Reader
	onEOF func()
	done  bool
}

func (e *eofHook) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF && !e.done {
		e.done = true
		e.onEOF()
	}
	return n, err
}

// gwBackupNode 假网关：收到 export_seat 就扮演节点——把 body 回传到 upload_url（带真凭据与 sha256 trailer）。
// sha 为空 ⇒ 用 body 的真 sha256；"-" ⇒ 不带 trailer；其它 ⇒ 原样（造校验和不符）。
// reportSHA 控制【控制面补报】（丙案，结果载荷里的 sha256）：空 ⇒ 补报真值；"-" ⇒ 不补报；其它 ⇒ 谎报。
type gwBackupNode struct {
	secret    string
	body      []byte
	sha       string
	reportSHA string
	sendErr   error // 非空 ⇒ 扮演网关发不出去
	fail      bool  // 扮演节点回失败
	lie       bool  // 扮演节点不回传却报成功
	called    []string
}

func (g *gwBackupNode) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	g.called = append(g.called, cmd.Type)
	if cmd.Type != "export_seat" {
		return NodeResult{OK: true}, nil
	}
	if g.sendErr != nil {
		return NodeResult{}, g.sendErr
	}
	if g.fail {
		return NodeResult{OK: false, Error: "injected"}, nil
	}
	if g.lie {
		return NodeResult{OK: true}, nil
	}
	var p struct {
		UploadURL string `json:"upload_url"`
	}
	_ = json.Unmarshal(cmd.Params, &p)
	sha := g.sha
	if sha == "" {
		sha = sha256Hex(g.body)
	}
	code, err := uploadBackup(ctx, p.UploadURL, g.secret, g.body, sha)
	if err != nil {
		return NodeResult{OK: false, Error: err.Error()}, nil
	}
	if code/100 != 2 {
		return NodeResult{OK: false, Error: "upload rejected"}, nil // 节点见 Hub 拒收即报失败（nodeagent 同口径）
	}
	// 控制面补报（与 nodeagent 结果载荷同形；丙案后这是权威通道）
	report := g.reportSHA
	if report == "" {
		report = sha256Hex(g.body)
	}
	payload, _ := json.Marshal(map[string]string{"seat_id": "ag", "state": "exported", "sha256": report})
	if report == "-" {
		payload, _ = json.Marshal(map[string]string{"seat_id": "ag", "state": "exported"}) // 不补报
	}
	return NodeResult{OK: true, Payload: payload}, nil
}

// mkBackupRig 节点登记＋Hub 接收端点＋Backuper（origin 指向测试服务）。
func mkBackupRig(t *testing.T, gw *gwBackupNode) (*Store, *HubBackuper, string) {
	t.Helper()
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_bk", Name: "bk", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	gw.secret = secret
	dir := t.TempDir()
	bk := NewHubBackuper(s, gw, "", dir)
	mux := http.NewServeMux()
	mux.Handle("POST /api/node/backup/{id}", bk.BackupHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	bk.origin = srv.URL
	return s, bk, dir
}

func backupCount(t *testing.T, s *Store) int {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), htx(), `SELECT COUNT(*) FROM g4_backup`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	rows.Next()
	_ = rows.Scan(&n)
	return n
}

func TestBackupPipelineEndToEnd(t *testing.T) {
	body := mkTarGz(t, map[string]string{"ag_bks/work.log": "卷内容"})
	s, bk, dir := mkBackupRig(t, &gwBackupNode{body: body})
	ctx := context.Background()

	ref, err := bk.BackupSeatVolume(ctx, "nd_bk", "ag_bks")
	if err != nil {
		t.Fatalf("备份: %v", err)
	}
	if !strings.HasSuffix(ref, ".tar.gz") {
		t.Fatalf("backupRef: %s", ref)
	}
	raw, err := os.ReadFile(ref)
	if err != nil || !bytes.Equal(raw, body) {
		t.Fatalf("落盘内容应与回传逐字节相同: %v", err)
	}
	fi, _ := os.Stat(ref)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("备份件权限 0600: %o", fi.Mode())
	}
	// 记录 complete＋expires 约 14 天
	rec, _ := s.GetBackup(ctx, strings.TrimSuffix(strings.TrimPrefix(ref, dir+"/"), ".tar.gz"))
	if rec.State != "complete" || rec.SizeBytes != int64(len(body)) {
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

func TestBackupHandlerAuthAndStates(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_b1", Name: "b1", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// 第二个节点凭据（串用必拒）
	secret2, err := s.RegisterNode(ctx, Node{ID: "nd_b2", Name: "b2", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bk := NewHubBackuper(s, &gwBackupNode{}, "", dir)
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
	good := mkTarGz(t, map[string]string{"ag_x/f": "data"})
	post := func(id, secret string) int {
		code, err := uploadBackup(ctx, srv.URL+"/api/node/backup/"+id, secret, good, sha256Hex(good))
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	if c := post("bk_nope", secret); c != 404 {
		t.Fatalf("未知 id 应 404: %d", c)
	}
	if c := post(mk(), "wrong"); c != 401 {
		t.Fatalf("坏凭据应 401: %d", c)
	}
	if c := post(mk(), ""); c != 401 {
		t.Fatalf("无凭据应 401: %d", c)
	}
	if c := post(mk(), secret2); c != 401 {
		t.Fatalf("别的节点的凭据应 401: %d", c)
	}
	// 正常：pending＋对节点凭据＋sha 对上＋解得开 ⇒ 200＋complete
	id := mk()
	if c := post(id, secret); c != 200 {
		t.Fatalf("正常回传: %d", c)
	}
	// 已 complete 的再传 ⇒ 404（不覆写）
	if c := post(id, secret); c != 404 {
		t.Fatalf("重复回传应 404: %d", c)
	}
	// 完整性不过 ⇒ 422，且这条记录当场作废：之后哪怕补传好的也 404（不留可写目标）
	bad := mk()
	if code, err := uploadBackup(ctx, srv.URL+"/api/node/backup/"+bad, secret, []byte("garbage"), sha256Hex([]byte("garbage"))); err != nil || code != 422 {
		t.Fatalf("corrupt upload: %d %v, want 422", code, err)
	}
	if c := post(bad, secret); c != 404 {
		t.Fatalf("rejected record must be void, later upload got %d", c)
	}
}

// 🔴 完整性（台账 lead-g4-t53-review 🔴2；复验缺陷二）：非 gzip、截断 gzip、trailer 校验和与落盘不符
// ⇒ Hub 422 拒收、不落盘、记录作废；BackupSeatVolume 报错；Destroy 不继续（不吊销、不删、不改状态）。
// 丙案（R-G4-BACKUP-CHECKSUM-CTRL）：trailer 缺失不再拒收（反代会丢）——校验移到控制面补报，
// 由 TestBackupChecksumControlPlane 钉。
func TestBackupIntegrityRejected(t *testing.T) {
	good := mkTarGz(t, map[string]string{"ag_i/work.log": strings.Repeat("卷内容", 2000)})
	cases := []struct {
		name string
		body []byte
		sha  string // "" ⇒ 用 body 的真 sha256
	}{
		{"not a gzip", []byte("THIS IS NOT A GZIP"), ""},
		{"truncated gzip", good[:len(good)/2], ""},
		{"checksum mismatch", good, sha256Hex([]byte("something else"))},
		// 复验遗留 X4（tosonnet50ts）：合法 gzip＋合法 tar、sha256 也对，但一个条目都没有 ⇒ 仍须拒收（空卷备份不可用于恢复）。
		{"empty archive", mkTarGz(t, map[string]string{}), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gw := &gwBackupNode{body: c.body, sha: c.sha}
			s, bk, dir := mkBackupRig(t, gw)
			ctx := context.Background()
			ref, err := bk.BackupSeatVolume(ctx, "nd_bk", "ag_i")
			if err == nil || ref != "" {
				t.Fatalf("must fail: ref=%q err=%v", ref, err)
			}
			if n := backupCount(t, s); n != 0 {
				t.Fatalf("rejected backup record must be gone: %d", n)
			}
			left, _ := os.ReadDir(dir)
			if len(left) != 0 {
				t.Fatalf("nothing may stay on disk: %v", left)
			}
		})
	}
	t.Run("destroy does not continue", func(t *testing.T) {
		lc, log, _, _, _, s, id := mkLifecycle(t)
		gw := &gwBackupNode{body: []byte("THIS IS NOT A GZIP")}
		_, bk, _ := mkBackupRigOn(t, s, gw)
		lc.backups = bk
		log.reset()
		err := lc.Destroy(context.Background(), id, "zzc", false, true)
		var be *BackupFailedError
		if !errors.As(err, &be) {
			t.Fatalf("%v, want backup_failed", err)
		}
		eqSeq(t, "corrupt backup stops destroy", log.get(), []string{"node:stop_seat"}) // 已停容器；不吊销、不发 destroy_seat
		if seat, _ := s.GetSeat(context.Background(), id); seat.State != "error" || seat.StateDetail != "backup_failed" {
			t.Fatalf("seat must show backup_failed (container stopped, nothing deleted): %+v", seat)
		}
	})
}

// 🔴 丙案（R-G4-BACKUP-CHECKSUM-CTRL；106 实测 nginx 1.24.0 丢弃请求 trailer）：
// 校验值经控制面补报（export_seat 结果载荷）、Hub 落盘自算、相等才转 complete；
// 未补报或不等 ⇒ 不销毁、backup_failed。反向：去掉 BackupSeatVolume 的补报比对 ⇒ 本格红。
func TestBackupChecksumControlPlane(t *testing.T) {
	good := mkTarGz(t, map[string]string{"ag_c/work.log": "卷内容"})

	// 丢 trailer 的反代形态：上传不带 trailer 也须走通全链（补报正确 ⇒ complete）
	t.Run("no trailer full chain ok", func(t *testing.T) {
		s, bk, dir := mkBackupRig(t, &gwBackupNode{body: good, sha: "-"})
		ref, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_c")
		if err != nil {
			t.Fatalf("丢 trailer 不应挡路: %v", err)
		}
		raw, err := os.ReadFile(ref)
		if err != nil || !bytes.Equal(raw, good) {
			t.Fatalf("落盘内容应逐字节相同: %v", err)
		}
		rec, _ := s.GetBackup(context.Background(), strings.TrimSuffix(strings.TrimPrefix(ref, dir+"/"), ".tar.gz"))
		if rec.State != "complete" {
			t.Fatalf("补报相等应转 complete: %+v", rec)
		}
	})

	// 未补报 ⇒ 不转 complete、报错（不销毁）；记录与文件清掉
	t.Run("report missing blocks", func(t *testing.T) {
		s, bk, dir := mkBackupRig(t, &gwBackupNode{body: good, reportSHA: "-"})
		if ref, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_c"); err == nil || ref != "" {
			t.Fatalf("未补报必须失败: ref=%q err=%v", ref, err)
		}
		if n := backupCount(t, s); n != 0 {
			t.Fatalf("记录应作废: %d", n)
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Fatalf("落盘件不得留: %v", left)
		}
	})

	// 补报与落盘自算不等 ⇒ 不转 complete、报错
	t.Run("report mismatch blocks", func(t *testing.T) {
		s, bk, dir := mkBackupRig(t, &gwBackupNode{body: good, reportSHA: sha256Hex([]byte("lying"))})
		if ref, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_c"); err == nil || ref != "" {
			t.Fatalf("补报不等必须失败: ref=%q err=%v", ref, err)
		}
		if n := backupCount(t, s); n != 0 {
			t.Fatalf("记录应作废: %d", n)
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Fatalf("落盘件不得留: %v", left)
		}
	})

	// 完整销毁链过丢 trailer 反代：destroy 走到底（补报是唯一校验依据）
	t.Run("destroy chain via trailer-dropping proxy", func(t *testing.T) {
		lc, log, _, _, _, s, id := mkLifecycle(t)
		gw := &gwBackupNode{body: good, sha: "-"}
		_, bk, _ := mkBackupRigOn(t, s, gw)
		lc.backups = bk
		log.reset()
		if err := lc.Destroy(context.Background(), id, "zzc", false, true); err != nil {
			t.Fatalf("丢 trailer 下销毁链应走通: %v", err)
		}
		seat, _ := s.GetSeat(context.Background(), id)
		if seat.State != "destroyed" {
			t.Fatalf("席位应 destroyed: %+v", seat)
		}
		if n := backupCount(t, s); n != 1 {
			t.Fatalf("备份记录应留存（complete 待 TTL）: %d", n)
		}
	})

	// 补报缺失 ⇒ 销毁链中止在 backup_failed（不发 destroy_seat）
	t.Run("missing report stops destroy", func(t *testing.T) {
		lc, log, _, _, _, s, id := mkLifecycle(t)
		gw := &gwBackupNode{body: good, reportSHA: "-"}
		_, bk, _ := mkBackupRigOn(t, s, gw)
		lc.backups = bk
		log.reset()
		err := lc.Destroy(context.Background(), id, "zzc", false, true)
		var be *BackupFailedError
		if !errors.As(err, &be) {
			t.Fatalf("%v, want backup_failed", err)
		}
		eqSeq(t, "missing report stops destroy", log.get(), []string{"node:stop_seat"})
		if seat, _ := s.GetSeat(context.Background(), id); seat.State != "error" || seat.StateDetail != "backup_failed" {
			t.Fatalf("seat must show backup_failed: %+v", seat)
		}
	})

	// 甲形状的安全方向（复验预审 G9）：Hub 重启 ⇒ landed 内存丢失 ⇒ 补报到了也比对无门
	// ⇒ 必须失败朝安全方向（不销毁）。扮演：上传落盘后、结果返回前清掉 landed（等价重启丢内存）。
	t.Run("hub restart loses landed, report blocked", func(t *testing.T) {
		inner := &gwBackupNode{body: good}
		s, bk, dir := mkBackupRig(t, inner)
		bk.gw = gwFunc(func(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
			res, err := inner.Send(ctx, nodeID, cmd)
			if cmd.Type == "export_seat" && err == nil && res.OK {
				bk.mu.Lock() // 等价 Hub 重启：落盘登记（内存）丢光
				bk.landed = map[string]struct {
					sha  string
					size int64
				}{}
				bk.mu.Unlock()
			}
			return res, err
		})
		if ref, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_c"); err == nil || ref != "" {
			t.Fatalf("重启丢内存后补报必须失败（不销毁）: ref=%q err=%v", ref, err)
		}
		if n := backupCount(t, s); n != 0 {
			t.Fatalf("记录应作废: %d", n)
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Fatalf("落盘件不得留: %v", left)
		}
	})
}

// gwFunc 适配器：把函数包装成 NodeGateway（G9 包层用）。
type gwFunc func(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error)

func (f gwFunc) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	return f(ctx, nodeID, cmd)
}

// mkBackupRigOn 在已有 Store 上搭接收端点（席位所在节点 nd_free 登记凭据）。
func mkBackupRigOn(t *testing.T, s *Store, gw *gwBackupNode) (*Store, *HubBackuper, string) {
	t.Helper()
	ctx := context.Background()
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_free", Name: "free", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	gw.secret = secret
	dir := t.TempDir()
	bk := NewHubBackuper(s, gw, "", dir)
	mux := http.NewServeMux()
	mux.Handle("POST /api/node/backup/{id}", bk.BackupHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	bk.origin = srv.URL
	return s, bk, dir
}

// 失败分支一律清 pending（复验存活刀 B4 与第四节）：网关发不出去、节点回失败两路。
func TestBackupFailureClearsPending(t *testing.T) {
	for name, gw := range map[string]*gwBackupNode{
		"gateway error":                    {sendErr: errors.New("injected: node unreachable")},
		"node failure":                     {fail: true},
		"node claims ok without uploading": {lie: true},
	} {
		t.Run(name, func(t *testing.T) {
			s, bk, _ := mkBackupRig(t, gw)
			if _, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_bf"); err == nil {
				t.Fatal("回传失败必须报错")
			}
			if n := backupCount(t, s); n != 0 {
				t.Fatalf("失败的 pending 记录应清掉: %d", n)
			}
		})
	}
}

// gwWithTimeout 实现 SendWithTimeout 可选接口的网关：记录是否被用、给的上限是多少。
// （丙案并入项：export_seat 的 15 分钟上限此前是死代码——全仓无实现。）
type gwWithTimeout struct {
	inner  *gwBackupNode
	usedD  time.Duration
	called bool
}

func (g *gwWithTimeout) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	return g.inner.Send(ctx, nodeID, cmd)
}

func (g *gwWithTimeout) SendWithTimeout(ctx context.Context, nodeID string, cmd NodeCommand, d time.Duration) (NodeResult, error) {
	g.called = true
	g.usedD = d
	return g.inner.Send(ctx, nodeID, cmd)
}

// BackupSeatVolume 对 export_seat 必须走 SendWithTimeout(15min)（长指令上限）；
// 反向：探测分支被去掉（只用 Send）⇒ 本格红。
func TestBackupUsesLongTimeout(t *testing.T) {
	body := mkTarGz(t, map[string]string{"ag_lt/work.log": "卷内容"})
	inner := &gwBackupNode{body: body}
	_, bk, _ := mkBackupRig(t, inner) // rig 把节点凭据装到 inner 上
	gw := &gwWithTimeout{inner: inner}
	bk.gw = gw
	if _, err := bk.BackupSeatVolume(context.Background(), "nd_bk", "ag_lt"); err != nil {
		t.Fatalf("备份: %v", err)
	}
	if !gw.called || gw.usedD != 15*time.Minute {
		t.Fatalf("export_seat 必须走 SendWithTimeout(15min): called=%v d=%v", gw.called, gw.usedD)
	}
}
