package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 反测的判据沿用本目录既有手法（reap_loop_test.go 头注）：
// 【修复前的形状必须被这些用例判红】。修复前的形状是 noRetry —— 起一次、失败即失败
// （pickFreePort 原注释「不做重试」那个决定）。每格涉及"重试救回来了"的，都在同一序列上
// 跑一次 noRetry 并断言它失败；否则本文件只是把旧代码换了个写法。

// noRetry 是修复前的形状。
func noRetry(port uint, start func(uint) error) (uint, error) {
	return port, start(port)
}

// 两种真实签名（出处逐字见 portretry.go 的 portTakenSignatures 注）。
// 🔴 errPortTakenPrecheck 是抢占窗口里最常命中的那条：库在 Start() 第一行就自己 Listen 一次，
// 别人已经绑上了 ⇒ 连 pg_ctl 都不会去调。
var errPortTakenPrecheck = errors.New("process already listening on port 46399")

// errPortTakenPGBind 是②那条：pg_ctl 真的起了、PG 自己 bind 失败，:224 把日志原文包进错误。
// 故意保留 PG 日志里的【大写】Address —— isPortTaken 统一小写后比，这条同时钉住那个小写化。
var errPortTakenPGBind = fmt.Errorf("could not start postgres using %s:\n%s",
	`"/tmp/dingwei-pgtest-x/binaries/bin/pg_ctl start -w -D /tmp/dingwei-pgtest-x/data -o -p 46399"`,
	`pg_ctl: could not start server
LOG:  could not bind IPv4 address "127.0.0.1": Address already in use
	Is the server already running on port 46399?
`)

// errNotPort 是一个【真失败】的代表：运行目录删不掉。它含 "port" 吗？不含 ——
// 但下面 TestIsPortTakenIsNarrow 里另有一条含 "port" 却不是抢占的，专门钉"窄"这半句。
var errNotPort = errors.New("unable to clean up runtime directory /tmp/dingwei-pgtest-x with error: permission denied")

// recorder 收集 start/pick/notify 的调用，供各格断言"重试了几次、报了什么"。
type recorder struct {
	ports   []uint // start 被调用时用的端口，按顺序
	picks   int
	notices [][2]uint
}

func (r *recorder) start(results []error) func(uint) error {
	return func(p uint) error {
		r.ports = append(r.ports, p)
		i := len(r.ports) - 1
		if i < len(results) {
			return results[i]
		}
		return nil
	}
}

func TestStartWithPortRetryRetriesOnPrecheckSignature(t *testing.T) {
	r := &recorder{}
	next := uint(52001)
	got, err := startWithPortRetry(46399, portAttempts,
		r.start([]error{errPortTakenPrecheck, nil}),
		func() (uint, error) { r.picks++; next++; return next, nil },
		func(from, to uint) { r.notices = append(r.notices, [2]uint{from, to}) })
	if err != nil {
		t.Fatalf("端口被抢后换端口重试仍然失败: %v", err)
	}
	if len(r.ports) != 2 {
		t.Fatalf("start 被调了 %d 次（%v），要 2 次（首次撞抢占、第二次成功）", len(r.ports), r.ports)
	}
	if r.picks != 1 {
		t.Errorf("pick 被调了 %d 次，要 1 次（只有重试才需要再挑端口）", r.picks)
	}
	// 🔴 返回值必须是【最终那个】端口：调用方拿它拼 DSN。返回原端口＝"重试了但没用上"，
	// 那比不重试更坏（会去连一个别人的库），故单独钉一句。
	if got != r.ports[1] {
		t.Errorf("返回端口 %d，要最终成功那次的 %d —— 返回原端口会让 DSN 指着别人的库", got, r.ports[1])
	}
	if got == 46399 {
		t.Errorf("返回的还是原来那个被抢的端口 %d", got)
	}
	if len(r.notices) != 1 || r.notices[0] != [2]uint{46399, r.ports[1]} {
		t.Errorf("notify = %v，要一次 {46399 %d} —— 重试必须【出声】，"+
			"否则就是 pickFreePort 原注释担心的那种静默重试", r.notices, r.ports[1])
	}

	// 反向对照：修复前的形状在同一序列上必然失败。
	if _, err := noRetry(46399, (&recorder{}).start([]error{errPortTakenPrecheck, nil})); err == nil {
		t.Errorf("修复前的形状（noRetry）在这个序列上竟然成功 —— 那本格证明不了重试有用")
	}
}

func TestStartWithPortRetryRetriesOnPGBindSignature(t *testing.T) {
	// 🔴 这一格钉的是【第二种签名】：只认 ensurePortAvailable 那条的话，
	// "端口在库自检之后、PG 真 bind 之前才被抢走" 这个形状仍然会让无关用例红。
	r := &recorder{}
	got, err := startWithPortRetry(46399, portAttempts,
		r.start([]error{errPortTakenPGBind, nil}),
		func() (uint, error) { return 52999, nil },
		func(from, to uint) { r.notices = append(r.notices, [2]uint{from, to}) })
	if err != nil {
		t.Fatalf("PG bind 签名（②）没被认成端口被抢: %v", err)
	}
	if got != 52999 || len(r.ports) != 2 {
		t.Errorf("端口序列 %v、最终 %d，要 [46399 52999]、52999", r.ports, got)
	}
	if _, err := noRetry(46399, (&recorder{}).start([]error{errPortTakenPGBind, nil})); err == nil {
		t.Errorf("修复前的形状在②这个序列上竟然成功 —— 本格的反向对照失效")
	}
}

func TestStartWithPortRetryDoesNotRetryOtherErrors(t *testing.T) {
	// 🔴 这是"窄"那一半的判据：真失败必须【立刻】失败、原样交回，不许重试。
	// 宽了之后，「运行目录删不掉」这种真因会被重试三轮、最后报一个含 "连续 3 次都撞上端口被占"
	// 的假文案 —— 排障的人会被指到完全错误的方向。
	r := &recorder{}
	got, err := startWithPortRetry(46399, portAttempts,
		r.start([]error{errNotPort}),
		func() (uint, error) { r.picks++; return 52001, nil },
		func(from, to uint) { r.notices = append(r.notices, [2]uint{from, to}) })
	if err == nil {
		t.Fatalf("真失败（%v）竟然没报错", errNotPort)
	}
	if !errors.Is(err, errNotPort) {
		t.Errorf("错误没有原样交回：got %v，要 errors.Is 认得出 %v（包装可以，改写不行）", err, errNotPort)
	}
	if len(r.ports) != 1 {
		t.Errorf("start 被调了 %d 次（%v），要 1 次 —— 非端口失败不得重试", len(r.ports), r.ports)
	}
	if r.picks != 0 {
		t.Errorf("pick 被调了 %d 次，要 0 次", r.picks)
	}
	if len(r.notices) != 0 {
		t.Errorf("notify 被调了 %d 次（%v），要 0 次 —— 没重试就不该报 PORT_RETRY", len(r.notices), r.notices)
	}
	if got != 46399 {
		t.Errorf("返回端口 %d，要原来那个 %d（没换过）", got, 46399)
	}
	// 反向对照在这一格是【同形】的：修复前后都立刻失败。写出来是为了说明本格不是
	// "新形状特有的行为"，而是"新形状没有把旧的正确行为弄丢"。
	if _, errOld := noRetry(46399, (&recorder{}).start([]error{errNotPort})); !errors.Is(errOld, errNotPort) {
		t.Errorf("修复前的形状对真失败的处理也应是原样交回，实得 %v", errOld)
	}
}

func TestStartWithPortRetryGivesUpAfterAttempts(t *testing.T) {
	// 一直撞抢占 ⇒ 到上限就停，不无限重试（无限重试会把真失败伪装成慢）。
	r := &recorder{}
	always := func(p uint) error { r.ports = append(r.ports, p); return errPortTakenPrecheck }
	seq := func() (uint, error) { return uint(52000 + len(r.ports)), nil }
	got, err := startWithPortRetry(46399, portAttempts, always, seq, func(from, to uint) {})
	if err == nil {
		t.Fatal("连续撞抢占却没报错")
	}
	if len(r.ports) != portAttempts {
		t.Errorf("start 被调了 %d 次，要恰好 %d 次（上限含首次）", len(r.ports), portAttempts)
	}
	if !errors.Is(err, errPortTakenPrecheck) {
		t.Errorf("放弃时的错误没有包住最后一次的真因: %v", err)
	}
	for _, want := range []string{fmt.Sprint(portAttempts), fmt.Sprint(got), "端口被占"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("放弃文案缺 %q（要说清试了几次、最后一次用的哪个号、是什么性质）: %v", want, err)
		}
	}
	if got != r.ports[len(r.ports)-1] {
		t.Errorf("返回端口 %d，要最后一次尝试的 %d（排障要能对上 PORT_RETRY 那几条）", got, r.ports[len(r.ports)-1])
	}
}

func TestStartWithPortRetryPickFailure(t *testing.T) {
	// 换端口时连"再挑一个号"都失败 ⇒ 报的是挑端口失败，且不再 start。
	r := &recorder{}
	pickErr := errors.New("挑空闲端口: socket: too many open files")
	got, err := startWithPortRetry(46399, portAttempts,
		r.start([]error{errPortTakenPrecheck}),
		func() (uint, error) { return 0, pickErr },
		func(from, to uint) { r.notices = append(r.notices, [2]uint{from, to}) })
	if !errors.Is(err, pickErr) {
		t.Fatalf("挑端口失败没有原样交回: %v", err)
	}
	if len(r.ports) != 1 {
		t.Errorf("start 被调了 %d 次，要 1 次（挑不到新端口就不该再起一次）", len(r.ports))
	}
	if len(r.notices) != 0 {
		t.Errorf("notify 被调了 %v，要 0 次 —— 端口没换成就不该报 PORT_RETRY", r.notices)
	}
	if got != 46399 {
		t.Errorf("返回端口 %d，要原来那个 %d", got, 46399)
	}
}

func TestStartWithPortRetryRejectsBadAttempts(t *testing.T) {
	// 上限小于 1 是配置错误，必须 fail-closed：否则循环一次都不跑、
	// 返回一个 nil 错误，调用方会以为库起来了（那正是"没做却说做了"）。
	r := &recorder{}
	for _, attempts := range []int{0, -1} {
		got, err := startWithPortRetry(46399, attempts, r.start(nil),
			func() (uint, error) { return 52001, nil }, func(from, to uint) {})
		if err == nil {
			t.Errorf("attempts=%d 却没报错 —— 一次都没试就报成功，调用方会当库起来了", attempts)
		}
		if got != 46399 {
			t.Errorf("attempts=%d 返回端口 %d，要原样交回 %d", attempts, got, 46399)
		}
	}
	if len(r.ports) != 0 {
		t.Errorf("start 被调了 %d 次，要 0 次", len(r.ports))
	}
}

func TestIsPortTakenIsNarrow(t *testing.T) {
	// 两半都要钉：① 两种真签名都认得（含大小写与包装形态）；② 像但不是的都不认。
	// 🔴 只有①的话，把判据放宽成"含 port 就算"也能全绿 —— 那就是拿宽松冒充覆盖。
	yes := []struct {
		name string
		err  error
	}{
		{"库自检①", errPortTakenPrecheck},
		{"PG bind ②（日志原文，大写 Address）", errPortTakenPGBind},
		{"包装过一层", fmt.Errorf("起 PG 失败(端口 46399,运行目录 /tmp/x): %w", errPortTakenPrecheck)},
		{"全小写", errors.New("address already in use")},
		{"全大写", errors.New("ADDRESS ALREADY IN USE")},
	}
	for _, c := range yes {
		if !isPortTaken(c.err) {
			t.Errorf("%s：isPortTaken 认不出（%v）—— 这种形状会让无关用例红", c.name, c.err)
		}
	}
	no := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"运行目录删不掉", errNotPort},
		{"含 port 但不是抢占", errors.New("port must be between 1 and 65535")},
		{"含 listening 但不是抢占", errors.New("stopped listening for signals")},
		{"二进制解压失败", errors.New("unable to extract binaries: unexpected EOF")},
		{"起库超时", errors.New("timed out waiting for database to start")},
		{"已在别处启动过", errors.New("server is already started")},
	}
	for _, c := range no {
		if isPortTaken(c.err) {
			t.Errorf("%s：isPortTaken 误认成端口被抢（%v）—— 真因会被重试三轮并换成假文案", c.name, c.err)
		}
	}
}
