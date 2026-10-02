package main

// FPG-3「提交即落盘」机械正例 ＋ FPG-4 多轮稳定性专项（派工 R-G2-FANOUT-SPLIT ①②③）。
// 判据原文见板件 runbooks/SPEC-NEWPLAT-G2-FANOUT-PG-DRAFT.md（FPG-3／FPG-4）。
//
// 判据 ↔ 用例（一一对应，反向对照刀的读数见板件交付说明）：
//
//	FPG-3 半一「真实测试连接的 synchronous_commit 必须为 on」
//	  → TestPGDurabilityProductSessionSyncCommitOn        （反向刀 M4）
//	FPG-3 半二「接管入口成功返回后，从独立连接立即读取业务键必须可见完整提交」· 出站
//	  → TestPGDurabilityOutboundCommitVisibleOnIndependentConn （反向刀 M1／M2）
//	FPG-3 半二 · 入站（WAL 提交之前不得 ack）
//	  → TestPGDurabilityInboundCommitVisibleOnIndependentConn  （反向刀 M3）
//
// 🔴 为什么这三条用例落在 cmd/feishu-fanout 而不是 internal/fanout —— 两条硬理由，都不是图方便：
//
//	① 判据半一要断言的是【装配层真的会建出来的那个会话】。产品池由本目录 main.go 的
//	   openSpoolPool 建；本包用例直接调它，于是「被测的池」与「产品用的池」出自同一处代码。
//	   写进 internal/fanout 就只能由测试自己另建一个池 —— 那证明的是夹具，不是装配
//	   （交接件坑#3「只检查 DSN 或工具默认值不足以证明调用连接未漂移」指的正是这类冒充）。
//	   入站／出站的接管入口（POST /outbound、POST /_test/inject）也全部经 fanout 的
//	   【导出面】驱动，不借包内私有捷径，故这三条用例同时是黑盒证据。
//	② -count=50 -timeout 60m 的预算。internal/fanout 在 e6642c85 上单轮实测 69.9s
//	   （17 个用例各起一个内嵌 PG，每个约 3s），50 轮＝58.2 分钟，已占满 60m 上限的 97%；
//	   在那包里再加一个起 PG 的用例即破线（+3s/轮 → 60.75 分钟 > 60m）。
//	   本包单轮 7.3s，加这三条后约 16s/轮，50 轮≈14 分钟 ⇒ FPG-4 的 60m 上限可原样满足。
//
// 🔴「立即读取」在实现上是【单次】查询：本文件不得出现轮询、重试或 sleep 之后再读。
//
//	一旦 waitFor，"返回先于提交" 就会被等待掩盖成绿 —— 那恰恰是 FPG-3 反向变异要抓的东西
//	（交接件坑#1「不要用日志顺序冒充提交顺序」、建议判据形状第 3 条「避免自证」）。
//
// 🔴 本文件【永不调用 t.Skip】：FPG-4 要求 0 SKIP，而 pgtest 默认已把「环境在却起不来」
//
//	判成 Fail（裁定 ruling-2026-10-01T130835Z），只有环境本就缺失才 Skip。
//	故读数里的 SKIP 计数必须由 -v 输出实数，不得由本文件"保证"。
//
// 🔴 观测查询用双轴上下文（repo.NewAccessCtx）而不是系统上下文：观测读到的是租户数据，
//
//	SP7a 阶段过滤尚未注入（repo 包首注已写明"现在拦不住跨租户读"），用双轴上下文如实
//	表达"这是一次带租户轴的读"，不拿"与租户无关"冒充任何隔离结论。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/fanout"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

const (
	durCallerToken = "tok-dur"
	durCallerID    = "dur-hub"
	// durRequestID 合法 UUIDv5 形（第 13 位=5、第 17 位=8）：custody.go 的 reUUIDv5 只验形，
	// 派生规则在 Hub 侧（设计 §C1），代理不代生成 —— 故此处用固定合法形即可。
	durRequestID = "9b2f8a4e-1c3d-5e6f-8a9b-0c1d2e3f4a5b"
	durBot       = "unifiedrobot"
	durToID      = "oc_dur_1"
	durOutText   = "durability-outbound-commit-visible"

	durBotChan = "bot-dur"
	durMsgID   = "om_dur_inbound_1"
	durChatID  = "oc_dur_in_1"
	durOpenID  = "ou_dur_1"
	durInText  = "durability-inbound-commit-visible"

	durSinkAuth = "sink-auth"
	durSinkBest = "sink-best"
)

// durRig 一条用例的全部现场：一个内嵌 PG（只起一次）＋两个池（产品池／独立观测池）＋真 Server。
type durRig struct {
	dsn   string
	prod  *repo.Pool // 产品池：装配层同一入口建，MaxOpenConns(1)
	watch *repo.Pool // 观测池：与产品池不同的连接，用来证明"独立连接立即可见"
	ac    repo.AccessCtx
	srv   *fanout.Server

	prodPID  int64 // 产品池会话后端 pid
	watchPID int64 // 观测池会话后端 pid（必须与 prodPID 不同）
}

// newDurRig 起一个内嵌 PG 并装配真 Server（不调 Start：不开 worker，
// 于是落盘后的行必然还停在 queued／pending —— 状态断言因此是"提交瞬间"的读数，
// 而不是被后台投递改过之后的读数）。
func newDurRig(t *testing.T) *durRig {
	t.Helper()
	// 🔴 pgtest.DSN 只许调【一次】：它每调一次就起一个全新的内嵌 PG（实测约 3s）。
	//   两个池必须连同一个库，否则"从独立连接读到已提交"根本无从谈起。
	dsn := pgtest.DSN(t)
	ctx := context.Background()

	prod, err := openSpoolPool(ctx, dsn) // ← 装配层同一入口（理由见文件头注 ①）
	if err != nil {
		t.Fatalf("装配层建产品池失败（openSpoolPool）: %v", err)
	}
	t.Cleanup(func() { _ = prod.Close() })

	watch, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("建独立观测池失败: %v", err)
	}
	t.Cleanup(func() { _ = watch.Close() })

	ac, err := repo.NewAccessCtx("org-durability", "principal-durability")
	if err != nil {
		t.Fatalf("观测用双轴上下文不可用: %v", err)
	}

	srv := fanout.New(fanout.Config{
		ProdURL: "http://127.0.0.1:1", // 不用：本用例不转发生产路
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sender:  fakeSender{}, // 出站选路要求 bot 已知；Bots 空 ⇒ 退化用它
		// 托管出站（§C）：caller token → 身份；后缀策略关，落盘字节＝所投字节。
		Callers:     map[string]fanout.CallerConfig{durCallerToken: {Name: durCallerID}},
		SpoolPool:   prod,
		MaxAttempts: 8,
		// 入站（§A/§B）：两个 sink（authoritative＋besteffort），filter=all ⇒ 必然建两行。
		Sinks: []fanout.SinkConfig{
			{Name: durSinkAuth, Role: "authoritative", HubURL: "http://127.0.0.1:1", Channel: "dur", Filter: "all"},
			{Name: durSinkBest, Role: "besteffort", HubURL: "http://127.0.0.1:1", Channel: "dur", Filter: "all"},
		},
		TestInject: true, // 打开 /_test/inject：用真 mux 驱动 ws 入站接管路（生产恒关）
	})

	r := &durRig{dsn: dsn, prod: prod, watch: watch, ac: ac, srv: srv}
	r.prodPID = r.backendPID(t, prod)
	r.watchPID = r.backendPID(t, watch)
	// 🔴 "独立连接" 不靠声明，靠 pid 实测：产品池上限 1 条连接，观测池必然是另一条会话。
	//   若两池共用一条会话，读到的可能是同一事务内的未提交数据 —— 判据会自证（交接件坑#3）。
	if r.prodPID == r.watchPID {
		t.Fatalf("观测池与产品池落在同一后端会话（pid=%d）：独立连接读取不成立，判据会自证", r.prodPID)
	}
	return r
}

// backendPID 取该池当前会话的后端 pid。
func (r *durRig) backendPID(t *testing.T, p *repo.Pool) int64 {
	t.Helper()
	row, err := p.QueryRow(context.Background(), r.ac, `SELECT pg_backend_pid()`)
	if err != nil {
		t.Fatalf("取后端 pid 失败: %v", err)
	}
	var pid int64
	if err := row.Scan(&pid); err != nil {
		t.Fatalf("读后端 pid 失败: %v", err)
	}
	return pid
}

// showSetting 在【该池的实际会话】上读一个会话级参数。
//
// name 只来自本文件的字面常量（不接受外部输入），故直接拼进 SHOW；
// 用 SHOW 而不是 current_setting($1)，是为了走会话参数的规范取值路径。
func (r *durRig) showSetting(t *testing.T, p *repo.Pool, name string) string {
	t.Helper()
	row, err := p.QueryRow(context.Background(), r.ac, "SHOW "+name)
	if err != nil {
		t.Fatalf("SHOW %s 失败: %v", name, err)
	}
	var v string
	if err := row.Scan(&v); err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	return v
}

// watchRow 从观测池取一行。err 里带上判据名，失败正文直接点在"独立连接读不到"上。
func (r *durRig) watchRow(t *testing.T, query string, args ...any) *sql.Row {
	t.Helper()
	row, err := r.watch.QueryRow(context.Background(), r.ac, query, args...)
	if err != nil {
		t.Fatalf("观测池查询被拒: %v", err)
	}
	return row
}

// assertTableRows 断言某表总行数。表名只来自本文件字面常量。
// 用途：证明"看得见的那一行"不是残留、也没有第二份（FPG-4 的隔离与无残留）。
func (r *durRig) assertTableRows(t *testing.T, table string, want int) {
	t.Helper()
	var n int
	if err := r.watchRow(t, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("数 %s 行数失败: %v", table, err)
	}
	if n != want {
		t.Fatalf("%s 有 %d 行，要求 %d 行：多行＝重复提交或残留，0 行＝提交不可见", table, n, want)
	}
}

// postJSON 经真 mux 发一次请求并返回记录器。
func (r *durRig) postJSON(t *testing.T, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("构造请求体失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.srv.ServeHTTP(w, req)
	return w
}

// ---- 用例一：FPG-3 半一（会话参数） ----

// TestPGDurabilityProductSessionSyncCommitOn 断言产品池的【实际会话】synchronous_commit=on。
//
// 🔴 取样 5 次并逐次核对 pg_backend_pid：产品池上限是 1 条连接（装配层给的 MaxOpenConns(1)），
//
//	故 5 次取样必然全部落在同一条会话上 ⇒ "取样已穷尽该池的会话"是【测出来的】，不是假设的。
//	若哪天装配层放开连接数，pid 就会出现第二个值、本用例即红 —— 那时红是对的：
//	取样不再穷尽，判据要改宽（取到覆盖全部会话为止），而不是把这条断言删掉。
func TestPGDurabilityProductSessionSyncCommitOn(t *testing.T) {
	r := newDurRig(t)

	const samples = 5
	pids := map[int64]int{}
	for i := 0; i < samples; i++ {
		if v := r.showSetting(t, r.prod, "synchronous_commit"); v != "on" {
			t.Fatalf("产品会话 synchronous_commit=%q，要求 on（FPG-3 半一）："+
				"off 时 COMMIT 返回不等于已落盘，202／ack 就成了空头承诺", v)
		}
		pids[r.backendPID(t, r.prod)]++
	}
	if len(pids) != 1 {
		t.Fatalf("产品池 %d 次取样落在 %d 条会话上（%v），MaxOpenConns(1) 下应为 1 条："+
			"取样不再穷尽，请把判据改宽（覆盖全部会话）而不是删断言", samples, len(pids), pids)
	}

	// fsync 一并读。这一条【超出 FPG-3 字面】，属加固读数，不冒充判据：
	// spool.go 头注写的是「COMMIT 返回即已 fsync」；若测试库把 fsync 关了，
	// synchronous_commit=on 就是空话（提交进 OS 缓存即算数）。
	// 内嵌 PG 由 tools/pgtest 显式把两者都钉成 on（tools/pgtest/main.go 的 StartParameters）。
	if v := r.showSetting(t, r.prod, "fsync"); v != "on" {
		t.Fatalf("产品会话 fsync=%q，要求 on：否则 synchronous_commit=on 不产生真落盘", v)
	}
	// 旁证：同一库的另一条会话也是 on ⇒ 不是某条会话被单独 SET 过。
	if v := r.showSetting(t, r.watch, "synchronous_commit"); v != "on" {
		t.Fatalf("观测会话 synchronous_commit=%q，要求 on（旁证：库级默认未漂）", v)
	}
}

// ---- 用例二：FPG-3 半二 · 出站接管 ----

// TestPGDurabilityOutboundCommitVisibleOnIndependentConn：POST /outbound 返回 202 之后，
// 【立即、单次】从独立连接读同一业务键，必须看到完整的一行。
//
// 反向刀 M1（Accept 不落盘）与 M2（提交被推迟到成功返回之后）都必须红在本用例的
// "独立连接读不到该业务键" 那句断言上（业务断言，不是编译错／panic）。
func TestPGDurabilityOutboundCommitVisibleOnIndependentConn(t *testing.T) {
	r := newDurRig(t)

	w := r.postJSON(t, "/outbound", durCallerToken, map[string]any{
		"v": 1, "request_id": durRequestID, "bot_channel": durBot,
		"to_id": durToID, "to_type": "group", "text": durOutText,
	})
	// 202＝accepted（非 delivered），且是【新键】路（200 是幂等重放，说明库里有残留）。
	if w.Code != http.StatusAccepted {
		t.Fatalf("接管入口返回 %d，要求 202（200＝幂等重放，说明本轮库不干净）: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("202 响应体不是 JSON: %v (%s)", err, w.Body.String())
	}
	wantID := fanout.OutboundIDFor(durCallerID, durRequestID)
	if resp["outbound_id"] != wantID {
		t.Fatalf("outbound_id=%q，要求确定性 id %q（重启/重放恒同）", resp["outbound_id"], wantID)
	}

	// 🔴 立即读，单次，不轮询不 sleep —— 见文件头注。
	const q = `SELECT outbound_id, body_hash, body, status, attempts
	           FROM outbound WHERE caller_id=$1 AND request_id=$2`
	var gotID, gotHash, gotStatus string
	var gotBody []byte
	var gotAttempts int
	err := r.watchRow(t, q, durCallerID, durRequestID).
		Scan(&gotID, &gotHash, &gotBody, &gotStatus, &gotAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FPG-3 半二红：202 已返回给调用方，而独立连接（pid=%d，产品会话 pid=%d）"+
			"立即读取业务键 (%s,%s) 读不到行 —— 提交未先于成功返回（未落盘／落盘被推迟）",
			r.watchPID, r.prodPID, durCallerID, durRequestID)
	}
	if err != nil {
		t.Fatalf("独立连接读取失败: %v", err)
	}

	// 完整性：确定性 id 相符 ＋ body_hash 与 body 自洽（半写行不可能自洽）＋ 状态是提交瞬间的队列态。
	if gotID != wantID {
		t.Fatalf("落盘 outbound_id=%q，要求 %q", gotID, wantID)
	}
	sum := sha256.Sum256(gotBody)
	if gotHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("落盘行 body_hash 与 body 不自洽（hash=%s，body=%d 字节）：读到的是半写行", gotHash, len(gotBody))
	}
	if gotStatus != "queued" {
		t.Fatalf("落盘 status=%q，要求 queued：本用例不开 worker，别的状态说明读到的不是提交瞬间的行", gotStatus)
	}
	if gotAttempts != 0 {
		t.Fatalf("落盘 attempts=%d，要求 0", gotAttempts)
	}
	assertOutboundBody(t, gotBody)
	r.assertTableRows(t, "outbound", 1)
}

// assertOutboundBody 逐字段核对落盘 body ＝ 所投内容（冻结 schema：字段一个不多一个不少）。
func assertOutboundBody(t *testing.T, gotBody []byte) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(gotBody, &got); err != nil {
		t.Fatalf("落盘 body 不是 JSON（%d 字节）: %v", len(gotBody), err)
	}
	// custodyRequest 无 omitempty ⇒ 未投的字段以零值落盘（to_name=""、raw=null），一并核对。
	want := map[string]any{
		"v": float64(1), "request_id": durRequestID, "bot_channel": durBot,
		"to_id": durToID, "to_name": "", "to_type": "group", "text": durOutText, "raw": nil,
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("落盘 body 缺字段 %q（完整提交要求整行可见）: %s", k, gotBody)
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Fatalf("落盘 body 字段 %q=%#v，要求 %#v", k, gv, wv)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("落盘 body 有 %d 个字段，要求 %d 个（多余字段＝冻结 schema 已漂）: %s",
			len(got), len(want), gotBody)
	}
}

// ---- 用例三：FPG-3 半二 · 入站接管（WAL 先于 ack） ----

// TestPGDurabilityInboundCommitVisibleOnIndependentConn：/_test/inject 走真 ws 入站接管路
// （handleTestInject → onWSInbound → Spool.AcceptInbound），返回 202 之后立即从独立连接读，
// 主记录与两个 sink 行必须【全部】可见 —— 这是一次多行单事务，缺任一行都算未完整提交。
//
// 反向刀 M3（事务回滚而外层照样报成功）必须红在本用例的"读不到主记录"那句断言上。
func TestPGDurabilityInboundCommitVisibleOnIndependentConn(t *testing.T) {
	r := newDurRig(t)

	inject := map[string]any{
		"bot_channel": durBotChan, "msg_id": durMsgID, "chat_type": "group",
		"chat_id": durChatID, "sender_open_id": durOpenID, "text": durInText,
	}
	w := r.postJSON(t, "/_test/inject", "", inject)
	// 503 是"WAL 失败＝不 ack"的路；202 才表示接管成功（sinks.go:255）。
	if w.Code != http.StatusAccepted {
		t.Fatalf("入站接管返回 %d，要求 202: %s", w.Code, w.Body.String())
	}

	msgKey := durBotChan + "\x1f" + durMsgID // onWSInbound 的业务键构成
	// 🔴 立即读，单次，不轮询。
	var body []byte
	var status string
	err := r.watchRow(t, `SELECT body, status FROM inbound_msg WHERE msg_key=$1`, msgKey).
		Scan(&body, &status)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FPG-3 半二（入站）红：已向上游 ack 202，而独立连接（pid=%d，产品会话 pid=%d）"+
			"立即读取业务键 %q 读不到主记录 —— 提交未先于成功返回", r.watchPID, r.prodPID, msgKey)
	}
	if err != nil {
		t.Fatalf("独立连接读 inbound_msg 失败: %v", err)
	}
	if status != "pending" {
		t.Fatalf("inbound_msg.status=%q，要求 pending：两个 sink 均未投递（本用例不开 worker）", status)
	}
	assertInboundBody(t, body)

	// 多行单事务的另一半：两个 sink 行必须同时可见，且都停在 queued／attempts=0。
	rows, qErr := r.watch.Query(context.Background(), r.ac,
		`SELECT sink_name, role, status, attempts FROM inbound_sink WHERE msg_key=$1 ORDER BY sink_name`, msgKey)
	if qErr != nil {
		t.Fatalf("观测池查 inbound_sink 被拒: %v", qErr)
	}
	type sinkRow struct {
		name, role, status string
		attempts           int
	}
	var got []sinkRow
	for rows.Next() {
		var sr sinkRow
		if err := rows.Scan(&sr.name, &sr.role, &sr.status, &sr.attempts); err != nil {
			rows.Close()
			t.Fatalf("读 inbound_sink 失败: %v", err)
		}
		got = append(got, sr)
	}
	rows.Close()
	want := []sinkRow{
		{name: durSinkAuth, role: "authoritative", status: "queued"},
		{name: durSinkBest, role: "besteffort", status: "queued"},
	}
	if len(got) != len(want) {
		t.Fatalf("独立连接看到 %d 个 sink 行，要求 %d 行（%v）：主记录可见而 sink 行缺＝部分提交",
			len(got), len(want), got)
	}
	for i := range want {
		if got[i].name != want[i].name || got[i].role != want[i].role || got[i].status != want[i].status {
			t.Fatalf("sink 行[%d]=%+v，要求 %+v", i, got[i], want[i])
		}
		if got[i].attempts != 0 {
			t.Fatalf("sink 行 %s attempts=%d，要求 0", got[i].name, got[i].attempts)
		}
	}

	// 幂等吸收（at-least-once）：同 msg 二投仍 202，且账上仍恰一份 —— 二投不得留下部分提交。
	w2 := r.postJSON(t, "/_test/inject", "", inject)
	if w2.Code != http.StatusAccepted {
		t.Fatalf("同 msg 二投返回 %d，要求 202（已在账＝幂等吸收）: %s", w2.Code, w2.Body.String())
	}
	r.assertTableRows(t, "inbound_msg", 1)
	r.assertTableRows(t, "inbound_sink", 2)
}

// assertInboundBody 核对落盘的归一 payload（sinks.go inboundPayloadV1 的 §K1 形）。
func assertInboundBody(t *testing.T, body []byte) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("inbound_msg.body 不是 JSON（%d 字节）: %v", len(body), err)
	}
	want := map[string]any{
		"v": float64(1), "msg_id": durMsgID, "chat_type": "group",
		"chat_id": durChatID, "open_id": "", "sender_open_id": durOpenID,
		"raw": map[string]any{"text": durInText},
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("落盘 payload 缺字段 %q（完整提交要求整行可见）: %s", k, body)
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Fatalf("落盘 payload 字段 %q=%#v，要求 %#v", k, gv, wv)
		}
	}
	if ts, ok := got["ts"].(float64); !ok || ts <= 0 {
		t.Fatalf("落盘 payload ts=%#v，要求正的 unix 秒", got["ts"])
	}
	if len(got) != len(want)+1 { // +1 = ts
		t.Fatalf("落盘 payload 有 %d 个字段，要求 %d 个: %s", len(got), len(want)+1, body)
	}
}
