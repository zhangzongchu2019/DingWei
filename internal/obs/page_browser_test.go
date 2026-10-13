package obs

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 真 Chrome 跑独立监控页（与仓内 MUX_PUPPETEER 同一做法：没装 puppeteer-core 就 Skip 并写明）。
// 页面与快照都用本包的真实 handler（鉴权另由 cmd/workpulse 的用例钉）；快照端可切换成慢、500、401、403。
// 钉住：CSP 下脚本真的执行、注入内容只当文本、UNKNOWN 不显示成 0、单一在飞、隐藏暂停／恢复立即取样、
// 失败保留旧值、401／403 停止轮询。
func TestMonitorPageInRealChrome(t *testing.T) {
	module := os.Getenv("OBS_PUPPETEER")
	if module == "" {
		t.Skip("set OBS_PUPPETEER to an installed puppeteer-core directory for the real Chrome page check")
	}
	chrome := os.Getenv("OBS_CHROME")
	if chrome == "" {
		chrome = "/usr/bin/google-chrome"
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}

	reg := New(nil)
	reg.SetIdentity(Identity{Service: "workpulse", Version: `<img src=x onerror="window.__pwn=1">`, Commit: "unknown", ConfigScope: "多字节边界：观测配置范围说明"})
	op := reg.OpenWS("session")
	op.Closed(PeerClose(1000))

	var mode atomic.Value // ok | slow | 500 | 401 | 403
	mode.Store("ok")
	var total, cur, maxCur, bigSent atomic.Int64
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("GET /console/monitor", MonitorPageHandler)
	mux.HandleFunc("GET /api/console/monitor/snapshot", func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)
		n := cur.Add(1)
		mu.Lock()
		if n > maxCur.Load() {
			maxCur.Store(n)
		}
		mu.Unlock()
		defer cur.Add(-1)
		switch mode.Load().(string) {
		case "slow":
			time.Sleep(1500 * time.Millisecond)
		case "500":
			http.Error(w, "boom", 500)
			return
		case "401":
			http.Error(w, `{"error":"unauthenticated"}`, 401)
			return
		case "403":
			http.Error(w, `{"error":"platform_admin_required"}`, 403)
			return
		case "big-chunked":
			// 无 Content-Length、分块：400000 个中文字（约 1.2 MB UTF-8），字符数低于 1 MiB、字节数超过。
			w.Header().Set("Content-Type", "application/json")
			fl := w.(http.Flusher)
			_, _ = io.WriteString(w, `{"schema":"dw-obs/v1","x":"`)
			chunk := strings.Repeat("中", 20000)
			var sent int64
			for i := 0; i < 20; i++ {
				n, err := io.WriteString(w, chunk)
				sent += int64(n)
				if err != nil {
					break
				}
				fl.Flush()
				time.Sleep(5 * time.Millisecond)
			}
			_, _ = io.WriteString(w, `"}`)
			bigSent.Store(sent)
			return
		case "multibyte-split":
			// 合法快照，按会把多字节字符切开的位置分块发送。
			rec := httptest.NewRecorder()
			reg.SnapshotHandler(rec, r)
			b := rec.Body.Bytes()
			i := bytes.Index(b, []byte("多字节"))
			if i < 0 {
				http.Error(w, "fixture", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fl := w.(http.Flusher)
			for _, cut := range [][2]int{{0, i + 1}, {i + 1, i + 4}, {i + 4, i + 8}, {i + 8, len(b)}} {
				_, _ = w.Write(b[cut[0]:cut[1]])
				fl.Flush()
				time.Sleep(30 * time.Millisecond)
			}
			return
		case "malformed-null":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, malformedSnapshot(reg, func(m map[string]any) { m["http"].(map[string]any)["series"] = []any{nil} }))
			return
		case "malformed-type":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, malformedSnapshot(reg, func(m map[string]any) {
				m["ws"].([]any)[0].(map[string]any)["opened"] = "many"
			}))
			return
		}
		reg.SnapshotHandler(w, r)
	})
	mux.HandleFunc("POST /ctl/{mode}", func(w http.ResponseWriter, r *http.Request) {
		mode.Store(r.PathValue("mode"))
		_ = json.NewEncoder(w).Encode(map[string]int64{"total": total.Load(), "max": maxCur.Load()})
	})
	mux.HandleFunc("GET /ctl/count", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"total": total.Load(), "max": maxCur.Load(), "bigSent": bigSent.Load()})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	script := filepath.Join(t.TempDir(), "monitor-chrome.cjs")
	if err := os.WriteFile(script, []byte(monitorChromeScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, srv.URL, chrome)
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Dir(module))
	out, err := cmd.CombinedOutput()
	t.Logf("chrome run:\n%s", out)
	if err != nil {
		t.Fatalf("browser checks failed: %v", err)
	}
	if !strings.Contains(string(out), "ALL_BROWSER_CHECKS_OK") {
		t.Fatal("browser script did not finish")
	}
}

// malformedSnapshot 取一份真实快照，按 mutate 改坏一处（schema 仍是 dw-obs/v1）。
func malformedSnapshot(reg *Registry, mutate func(map[string]any)) string {
	b, _ := json.Marshal(reg.Snapshot())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	mutate(m)
	out, _ := json.Marshal(m)
	return string(out)
}

const monitorChromeScript = `
const puppeteer = require('puppeteer-core');
const [,, base, chrome] = process.argv;
const sleep = ms => new Promise(r => setTimeout(r, ms));
function check(c, msg) { if (!c) { console.log('FAIL: ' + msg); process.exitCode = 1; throw new Error(msg); } console.log('ok: ' + msg); }
(async () => {
  const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox', '--no-proxy-server'] });
  try {
    const page = await browser.newPage();
    const consoleMsgs = [];
    page.on('console', m => consoleMsgs.push(m.text()));
    page.on('pageerror', e => consoleMsgs.push('pageerror ' + e.message));
    await page.evaluateOnNewDocument(() => {
      // 受控单调时钟：页面年龄只用 performance.now；测试可整体前拨，不用真等 75 s。
      const realNow = performance.now.bind(performance);
      window.__monoOffset = 0;
      performance.now = () => realNow() + window.__monoOffset;
      window.__hid = false;
      Object.defineProperty(document, 'hidden', { configurable: true, get() { return window.__hid; } });
    });
    const ctl = async m => (await fetch(base + '/ctl/' + m, { method: 'POST' })).json();
    const count = async () => (await fetch(base + '/ctl/count')).json();
    const status = () => page.$eval('#status', e => ({ cls: e.className, text: e.textContent }));
    const state = () => page.evaluate(() => window.__dwMonitorState());
    const setHidden = h => page.evaluate(h => { window.__hid = h; document.dispatchEvent(new Event('visibilitychange')); }, h);

    await page.goto(base + '/console/monitor', { waitUntil: 'load' });
    await page.waitForFunction(() => document.getElementById('status').className.includes('ok'), { timeout: 5000 });
    check(!consoleMsgs.some(m => /Content Security Policy|pageerror/.test(m)), 'no CSP violation or page error: ' + JSON.stringify(consoleMsgs));
    check(await page.evaluate(() => window.__pwn === undefined), 'injected markup did not execute');
    const body = await page.evaluate(() => document.body.innerText);
    check(body.includes('<img src=x onerror='), 'injected markup shown as literal text');
    check(await page.evaluate(() => document.querySelectorAll('#data img').length === 0), 'no element created from data');
    check(/deploy_id\s+UNKNOWN（UNKNOWN/.test(body) && /commit\s+UNKNOWN（UNKNOWN/.test(body), 'null identity fields shown as UNKNOWN with reason');
    check(/session\s+1\s+0\s+peer_normal=1/.test(body), 'measured ws row rendered');
    check(body.includes('node_ws') && body.includes('NOT_INSTRUMENTED'), 'NOT_INSTRUMENTED areas listed');
    check(body.includes('多字节边界：观测配置范围说明'), 'multibyte text decoded on normal load');
    let c = await count();
    check(c.total === 1, 'exactly one request on load (got ' + c.total + ')');
    await sleep(1500);
    check((await count()).total === 1 && (await state()).hasTimer, 'no tight loop; next poll scheduled');

    await ctl('slow');
    await setHidden(true); await setHidden(false);
    await setHidden(true); await setHidden(false);
    await setHidden(true); await setHidden(false);
    check((await state()).inflight, 'request in flight after resume');
    await sleep(2200);
    c = await count();
    check(c.max === 1, 'never more than one request in flight (max ' + c.max + ')');
    check(c.total === 2, 'three rapid resumes produced one request (total ' + c.total + ')');

    await ctl('ok');
    await setHidden(true);
    let st = await state();
    check(!st.hasTimer && (await status()).cls.includes('paused'), 'hidden: timer cleared, status paused');
    await sleep(800);
    check((await count()).total === 2, 'no requests while hidden');
    await setHidden(false);
    await page.waitForFunction(() => document.getElementById('status').className.includes('ok'), { timeout: 5000 });
    check((await count()).total === 3, 'visible again: immediate fresh request');

    await ctl('slow');
    await setHidden(true); await setHidden(false);
    check((await state()).inflight, 'slow request in flight');
    await setHidden(true);
    await sleep(2200);
    st = await state();
    check(!st.inflight && !st.hasTimer, 'hidden while in flight: no timer armed after the response');
    check((await count()).total === 4, 'no extra request while hidden (total ' + (await count()).total + ')');
    await ctl('ok');
    await setHidden(false);
    await page.waitForFunction(() => document.getElementById('status').className.includes('ok'), { timeout: 5000 });
    check((await count()).total === 5, 'resume after hidden-in-flight polls once');

    // ---- R2 页面 findings：有界流式读取、失败保留旧值、陈旧与错误共存
    const pollNow = async () => { await setHidden(true); await setHidden(false); };
    const waitDone = () => page.waitForFunction(() => !window.__dwMonitorState().inflight, { timeout: 8000 });
    const goodText = await page.evaluate(() => document.getElementById('data').innerText);
    const goodStatus = (await status()).text;
    const goodAt = (goodStatus.match(/服务端取样 (\S+?)；/) || [])[1];
    check(!!goodAt, 'good sample time visible: ' + goodStatus);

    await ctl('multibyte-split');
    await pollNow(); await waitDone();
    let s1 = await status();
    check(s1.cls.includes('ok'), 'multibyte split across chunks accepted: ' + s1.text);
    check((await page.evaluate(() => document.body.innerText)).includes('多字节边界：观测配置范围说明'), 'multibyte boundary decoded intact');

    const keptText = await page.evaluate(() => document.getElementById('data').innerText);
    const keptAt = ((await status()).text.match(/服务端取样 (\S+?)；/) || [])[1];
    await ctl('big-chunked');
    await pollNow(); await waitDone();
    s1 = await status();
    check(s1.text.includes('响应过大'), 'chunked response without Content-Length over 1 MiB (bytes, not chars) rejected: ' + s1.text);
    check(await page.evaluate(() => document.getElementById('data').innerText) === keptText, 'old data unchanged after oversize');
    check(s1.text.includes(keptAt), 'old sample time kept after oversize');

    for (const m of ['malformed-null', 'malformed-type']) {
      await ctl(m);
      await pollNow(); await waitDone();
      s1 = await status();
      check(s1.text.includes('快照字段不合法'), m + ' rejected by validation: ' + s1.text);
      check(await page.evaluate(() => document.getElementById('data').innerText) === keptText, m + ': DOM unchanged (no partial render)');
      check(s1.text.includes(keptAt), m + ': old sample time kept');
    }

    await ctl('ok');
    await pollNow(); await waitDone();
    check((await status()).cls.includes('ok'), 'recovers after malformed');
    await page.evaluate(() => { window.__monoOffset += 80000; });
    await page.waitForFunction(() => document.getElementById('status').textContent.includes('陈旧'), { timeout: 7000 });
    s1 = await status();
    check(s1.cls.includes('stale') && !s1.text.includes('取样失败'), 'stale after 75 s on the monotonic clock: ' + s1.text);
    await ctl('500');
    await pollNow(); await waitDone();
    s1 = await status();
    check(s1.cls.includes('stale') && s1.text.includes('陈旧') && s1.text.includes('最近一次取样失败：HTTP 500'), 'stale and latest error shown together: ' + s1.text);
    await ctl('ok');
    await pollNow(); await waitDone();
    s1 = await status();
    check(s1.cls.includes('ok') && !s1.text.includes('陈旧'), 'fresh sample clears stale: ' + s1.text);

    await ctl('500');
    await setHidden(true); await setHidden(false);
    await page.waitForFunction(() => document.getElementById('status').className.includes('error'), { timeout: 5000 });
    const errText = (await status()).text;
    check(errText.includes('HTTP 500') && errText.includes('上一次成功样本'), 'failure shown, previous sample kept: ' + errText);
    check((await page.evaluate(() => document.body.innerText)).includes('artifact_sha256'), 'previous data still rendered after failure');

    await ctl('403');
    await setHidden(true); await setHidden(false);
    await page.waitForFunction(() => document.getElementById('status').className.includes('denied'), { timeout: 5000 });
    st = await state();
    check(st.stopped && !st.hasTimer && (await status()).text.includes('403'), '403 stops polling and is shown');
    const before = (await count()).total;
    await setHidden(true); await setHidden(false);
    await sleep(500);
    check((await count()).total === before, 'no request after denial');

    await ctl('401');
    await page.reload({ waitUntil: 'load' });
    await page.waitForFunction(() => document.getElementById('status').className.includes('denied'), { timeout: 5000 });
    st = await state();
    check(st.stopped && !st.hasTimer && (await status()).text.includes('401'), '401 on first load stops polling and is shown');
    check(await page.evaluate(() => document.getElementById('data').childElementCount === 0), 'no data rendered without a successful sample');
    console.log('ALL_BROWSER_CHECKS_OK');
  } finally {
    await browser.close();
  }
})().catch(e => { console.log('ERROR ' + (e && e.stack || e)); process.exitCode = 1; });
`
