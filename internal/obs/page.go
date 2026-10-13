package obs

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// 独立监控页 GET /console/monitor（DW123-PER-RELEASE-OBSERVABILITY，组长 2026-10-13T08:59:04Z）。
//
// 只读 GET /api/console/monitor/snapshot 的真实快照：30 s 有限轮询、同一时刻只有一个请求在飞、
// 页面隐藏即暂停、恢复可见立即取一次；401／403 停止轮询并显示。
// 响应按实际收到的字节流式累计，超过 1 MiB 立即中止（不信 Content-Length、不先缓存全文）；
// 快照全量校验并离屏构建成功后才整体替换页面与「上一次成功样本」，失败保留旧值与旧取样时刻；
// 陈旧（单调时钟 75 s 无新有效样本）与最近一次错误可以同时显示；null 显示 UNKNOWN（附原因），不显示成 0。
// 渲染行数有上限；只用 textContent／createElement，不用 innerHTML；没有外部资源、不写任何数据、
// 不带除同源 cookie 以外的凭据。鉴权与快照接口同一套（路由上套 RequireConsolePrincipal＋RequireConsolePlatformAdmin）。
// 页面源码不写版本号（版本在快照的 identity 里）。

const monitorPageStyle = `
body{font:14px/1.45 system-ui,sans-serif;margin:16px;color:#1d2433;background:#f7f8fa}
h1{font-size:18px;margin:0 0 8px}
h2{font-size:15px;margin:18px 0 6px}
.status{padding:8px 10px;border-radius:6px;margin:8px 0;border:1px solid #c9ced8;background:#fff}
.status.ok{border-color:#7bb88a;background:#eef8f0}
.status.stale,.status.error{border-color:#d8a54a;background:#fdf6e8}
.status.denied{border-color:#d0645e;background:#fbeceb}
.status.paused{border-color:#9aa3b2;background:#f0f2f5}
table{border-collapse:collapse;background:#fff;margin:4px 0;font-size:13px}
th,td{border:1px solid #d9dde5;padding:3px 7px;text-align:left;vertical-align:top}
th{background:#eef0f4}
td.num{text-align:right;font-variant-numeric:tabular-nums}
.unknown{color:#a15c00}
.note{color:#5b6475;font-size:12px}
.ni{color:#7a3e9d}
`

const monitorPageScript = `
(function () {
  'use strict';
  var API = '/api/console/monitor/snapshot';
  var INTERVAL_MS = 30000, STALE_MS = 75000, TIMEOUT_MS = 10000;
  var MAX_BYTES = 1048576, MAX_ARRAY = 4096, MAX_HIST = 64;
  var MAX_HTTP_ROWS = 64, MAX_STREAM_ROWS = 32, MAX_WS_ROWS = 8, MAX_REASONS = 16, MAX_NI_ROWS = 16, MAX_TEXT = 200;
  var inflight = false, timer = null, stopped = false;
  // 只有完整校验并渲染成功的样本才提交到这三个变量。年龄用单调时钟；UTC 只作展示。
  var lastOk = null, lastOkMono = null, lastOkWall = null, lastErr = '';

  function mono() { return performance.now(); }
  function txt(v) {
    var s = String(v);
    return s.length > MAX_TEXT ? s.slice(0, MAX_TEXT) + '…' : s;
  }
  function el(tag, text, cls) {
    var e = document.createElement(tag);
    if (text !== undefined && text !== null) e.textContent = txt(text);
    if (cls) e.className = cls;
    return e;
  }
  function clear(n) { while (n.firstChild) n.removeChild(n.firstChild); }
  function isNum(v) { return typeof v === 'number' && isFinite(v); }
  function fmtTime(s) {
    if (!s) return 'UNKNOWN';
    var d = new Date(s);
    return isNaN(d.getTime()) ? 'UNKNOWN' : d.toISOString();
  }
  function setStatus(kind, msg) {
    var s = document.getElementById('status');
    s.className = 'status ' + kind;
    s.textContent = txt(msg);
  }

  // ---------------------------------------------------------------- 有界读取
  // 按实际收到的字节累计（chunk.byteLength），超过上限立即 cancel 并 abort；不依赖 Content-Length，也不先缓存全文。
  function readBounded(res, ctl) {
    if (!res.body || typeof res.body.getReader !== 'function') return Promise.reject(new Error('浏览器不支持流式读取'));
    var reader = res.body.getReader();
    var dec = new TextDecoder('utf-8', { fatal: true });
    var total = 0, parts = [];
    function pump() {
      return reader.read().then(function (r) {
        if (r.done) {
          parts.push(dec.decode());
          return parts.join('');
        }
        total += r.value.byteLength;
        if (total > MAX_BYTES) {
          try { reader.cancel(); } catch (e) { /* 已在取消 */ }
          ctl.abort();
          throw new Error('响应过大（超过 ' + MAX_BYTES + ' 字节，已中止）');
        }
        parts.push(dec.decode(r.value, { stream: true }));
        return pump();
      });
    }
    return pump();
  }

  // ---------------------------------------------------------------- 校验（有界、全量；任何一处不符就整体拒收）
  function bad(path) { throw new Error('快照字段不合法：' + path); }
  function obj(v, path) { if (!v || typeof v !== 'object' || Array.isArray(v)) bad(path); return v; }
  function arr(v, path, max) { if (!Array.isArray(v) || v.length > max) bad(path); return v; }
  function str(v, path) { if (typeof v !== 'string') bad(path); return v; }
  function strOrNull(v, path) { if (v !== null && v !== undefined && typeof v !== 'string') bad(path); }
  function cnt(v, path) { if (!isNum(v) || v < 0 || Math.floor(v) !== v) bad(path); return v; }
  function int(v, path) { if (!isNum(v) || Math.floor(v) !== v) bad(path); return v; }
  function num(v, path) { if (!isNum(v)) bad(path); return v; }
  function hist(h, path) {
    obj(h, path);
    var le = arr(h.le, path + '.le', MAX_HIST), counts = arr(h.counts, path + '.counts', MAX_HIST);
    if (le.length !== counts.length || le.length === 0) bad(path + '.counts');
    le.forEach(function (b, i) {
      if (i === le.length - 1) { if (b !== null) bad(path + '.le'); } else num(b, path + '.le');
    });
    var sum = 0;
    counts.forEach(function (c) { sum += cnt(c, path + '.counts'); });
    if (cnt(h.count, path + '.count') !== sum) bad(path + '.count');
    num(h.sum, path + '.sum');
  }
  function validate(s) {
    obj(s, 'snapshot');
    if (s.schema !== 'dw-obs/v1') bad('schema');
    str(s.snapshot_at, 'snapshot_at');
    str(s.process_started_at, 'process_started_at');
    num(s.uptime_seconds, 'uptime_seconds');
    var id = obj(s.identity, 'identity');
    ['service', 'config_scope'].forEach(function (k) { strOrNull(id[k], 'identity.' + k); });
    ['version', 'commit', 'artifact_sha256', 'config_sha256', 'deploy_id'].forEach(function (k) { strOrNull(id[k], 'identity.' + k); });
    if (id.unknown !== undefined) {
      obj(id.unknown, 'identity.unknown');
      Object.keys(id.unknown).forEach(function (k) { str(id.unknown[k], 'identity.unknown.' + k); });
    }
    var http = obj(s.http, 'http');
    cnt(http.max_series, 'http.max_series');
    cnt(http.dropped_samples, 'http.dropped_samples');
    strOrNull(http.note, 'http.note');
    arr(http.series, 'http.series', MAX_ARRAY).forEach(function (r, i) {
      var p = 'http.series[' + i + ']';
      obj(r, p);
      ['server', 'route', 'method', 'class'].forEach(function (k) { str(r[k], p + '.' + k); });
      hist(r.latency_ms, p + '.latency_ms');
    });
    arr(http.overflow, 'http.overflow', MAX_ARRAY).forEach(function (r, i) {
      obj(r, 'http.overflow[' + i + ']');
      str(r.server, 'http.overflow[' + i + '].server');
      hist(r.latency_ms, 'http.overflow[' + i + '].latency_ms');
    });
    var st = obj(s.streams, 'streams');
    cnt(st.dropped_samples, 'streams.dropped_samples');
    function stream(r, p) {
      obj(r, p);
      ['server', 'route', 'kind'].forEach(function (k) { str(r[k], p + '.' + k); });
      ['opened', 'closed', 'bytes_written_closed'].forEach(function (k) { cnt(r[k], p + '.' + k); });
      int(r.active, p + '.active');
      hist(r.duration_s, p + '.duration_s');
    }
    arr(st.series, 'streams.series', MAX_ARRAY).forEach(function (r, i) { stream(r, 'streams.series[' + i + ']'); });
    if (st.overflow !== null && st.overflow !== undefined) stream(st.overflow, 'streams.overflow');
    arr(s.ws, 'ws', MAX_ARRAY).forEach(function (r, i) {
      var p = 'ws[' + i + ']';
      obj(r, p);
      str(r.kind, p + '.kind');
      ['opened', 'ping_ok', 'ping_fail'].forEach(function (k) { cnt(r[k], p + '.' + k); });
      int(r.active, p + '.active');
      var c = obj(r.closed_by_reason, p + '.closed_by_reason');
      var keys = Object.keys(c);
      if (keys.length > MAX_ARRAY) bad(p + '.closed_by_reason');
      keys.forEach(function (k) { cnt(c[k], p + '.closed_by_reason.' + k); });
      hist(r.ping_rtt_ms, p + '.ping_rtt_ms');
    });
    arr(s.not_instrumented, 'not_instrumented', MAX_ARRAY).forEach(function (r, i) {
      var p = 'not_instrumented[' + i + ']';
      obj(r, p);
      ['area', 'status', 'reason'].forEach(function (k) { str(r[k], p + '.' + k); });
    });
    return s;
  }

  // ---------------------------------------------------------------- 渲染（离屏构建，成功后再整体替换）
  // null／缺失 ⇒ UNKNOWN（附原因），绝不当 0。
  function cellValue(v, reason) {
    if (v === null || v === undefined) return el('span', reason ? 'UNKNOWN（' + reason + '）' : 'UNKNOWN', 'unknown');
    return el('span', v);
  }
  function table(headers) {
    var t = el('table'), tr = el('tr');
    headers.forEach(function (h) { tr.appendChild(el('th', h)); });
    t.appendChild(tr);
    return t;
  }
  function row(t, cells) {
    var tr = el('tr');
    cells.forEach(function (c) {
      var td = el('td');
      if (c && c.nodeType) td.appendChild(c);
      else if (isNum(c)) { td.textContent = String(c); td.className = 'num'; }
      else td.textContent = txt(c === null || c === undefined ? 'UNKNOWN' : c);
      tr.appendChild(td);
    });
    t.appendChild(tr);
  }
  // 直方图只给桶计数：按桶上界给出「p95 ≤ X」，不插值。
  function p95(h) {
    if (h.count === 0) return '—（无样本）';
    var target = Math.ceil(h.count * 0.95), acc = 0;
    for (var i = 0; i < h.counts.length; i++) {
      acc += h.counts[i];
      if (acc >= target) return h.le[i] === null ? '> ' + h.le[i - 1] : '≤ ' + h.le[i];
    }
    return '—';
  }
  function mean(h) { return h.count > 0 ? (h.sum / h.count).toFixed(1) : '—（无样本）'; }
  function truncNote(box, total, cap) {
    if (total > cap) box.appendChild(el('div', '只显示前 ' + cap + ' 行，共 ' + total + ' 行', 'note'));
  }

  function build(s) {
    var root = el('div');
    var id = s.identity, unk = id.unknown || {};
    root.appendChild(el('h2', '运行身份与时刻'));
    var t = table(['项', '值']);
    row(t, ['service', cellValue(id.service, unk.service)]);
    row(t, ['version', cellValue(id.version, unk.version)]);
    row(t, ['commit', cellValue(id.commit, unk.commit)]);
    row(t, ['artifact_sha256', cellValue(id.artifact_sha256, unk.artifact_sha256)]);
    row(t, ['config_sha256', cellValue(id.config_sha256, unk.config_sha256)]);
    row(t, ['config_scope', cellValue(id.config_scope || null, null)]);
    row(t, ['deploy_id', cellValue(id.deploy_id, unk.deploy_id)]);
    row(t, ['snapshot_at（服务端取样时刻）', fmtTime(s.snapshot_at)]);
    row(t, ['process_started_at', fmtTime(s.process_started_at)]);
    row(t, ['uptime_seconds（进程存续，非请求时长）', s.uptime_seconds.toFixed(0)]);
    root.appendChild(t);

    var http = s.http, series = http.series;
    root.appendChild(el('h2', '短请求（按注册路由；不含 SSE 与升级连接）'));
    root.appendChild(el('div', http.note || '', 'note'));
    t = table(['server', 'route', 'method', 'class', 'count', 'mean ms', 'p95（桶上界）']);
    series.slice(0, MAX_HTTP_ROWS).forEach(function (r) {
      var h = r.latency_ms;
      row(t, [r.server, r.route, r.method, r['class'], h.count, mean(h), p95(h)]);
    });
    root.appendChild(t);
    truncNote(root, series.length, MAX_HTTP_ROWS);
    root.appendChild(el('div', '序列上限 ' + http.max_series + '；溢出样本 ' + http.dropped_samples + '；溢出桶 ' + http.overflow.length + ' 个', 'note'));

    var st = s.streams, ss = st.series;
    root.appendChild(el('h2', '长连接（SSE／升级；时长在连接结束时计入）'));
    t = table(['server', 'route', 'kind', 'opened', 'closed', 'active', 'bytes（已结束）', 'p95 存续 s（桶上界）']);
    ss.slice(0, MAX_STREAM_ROWS).forEach(function (r) {
      row(t, [r.server, r.route, r.kind, r.opened, r.closed, r.active, r.bytes_written_closed, p95(r.duration_s)]);
    });
    root.appendChild(t);
    truncNote(root, ss.length, MAX_STREAM_ROWS);
    root.appendChild(el('div', '溢出样本 ' + st.dropped_samples, 'note'));

    var ws = s.ws;
    root.appendChild(el('h2', 'WebSocket（仅已接 hook 的种类；0 为实测）'));
    t = table(['kind', 'opened', 'active', 'closed_by_reason', 'ping ok', 'ping fail', 'ping p95 ms（桶上界）']);
    ws.slice(0, MAX_WS_ROWS).forEach(function (r) {
      var reasons = Object.keys(r.closed_by_reason).sort();
      var parts = reasons.slice(0, MAX_REASONS).map(function (k) { return k + '=' + r.closed_by_reason[k]; });
      if (reasons.length > MAX_REASONS) parts.push('…');
      row(t, [r.kind, r.opened, r.active, parts.length ? parts.join(' ') : '（无关闭）', r.ping_ok, r.ping_fail, p95(r.ping_rtt_ms)]);
    });
    root.appendChild(t);
    truncNote(root, ws.length, MAX_WS_ROWS);

    var ni = s.not_instrumented;
    root.appendChild(el('h2', '未接埋点（NOT_INSTRUMENTED，不是 0，也不是 PASS）'));
    t = table(['area', 'status', 'reason']);
    ni.slice(0, MAX_NI_ROWS).forEach(function (r) { row(t, [r.area, el('span', r.status, 'ni'), r.reason]); });
    root.appendChild(t);
    truncNote(root, ni.length, MAX_NI_ROWS);
    return root;
  }

  // 校验与离屏构建都成功之后才动页面与 lastOk；替换这一步本身不会抛错。
  function commit(snap) {
    var built = build(validate(snap));
    var box = document.getElementById('data');
    clear(box);
    box.appendChild(built);
    lastOk = snap; lastOkMono = mono(); lastOkWall = new Date(); lastErr = '';
  }

  // ---------------------------------------------------------------- 状态条：陈旧与最近错误可以同时显示
  function refreshBanner() {
    if (stopped) return;
    if (document.hidden) { setStatus('paused', '页面已隐藏：轮询暂停，回到页面会立即取样'); return; }
    if (lastOkMono === null) {
      if (lastErr) setStatus('error', '取样失败：' + lastErr + '；还没有任何有效样本');
      return;
    }
    var age = mono() - lastOkMono;
    var sAt = fmtTime(lastOk.snapshot_at), gotAt = lastOkWall.toISOString();
    var stale = age > STALE_MS;
    var parts = [];
    if (stale) parts.push('陈旧：' + Math.round(age / 1000) + ' s 没有新的有效样本');
    if (lastErr) parts.push('最近一次取样失败：' + lastErr);
    if (parts.length) {
      setStatus(stale ? 'stale' : 'error', parts.join('；') + '；下面是上一次成功样本（服务端取样 ' + sAt + '，页面收到 ' + gotAt + '）');
    } else {
      setStatus('ok', '服务端取样 ' + sAt + '；页面收到 ' + gotAt + '；每 30 s 刷新');
    }
  }

  function schedule() {
    clearTimeout(timer);
    timer = null;
    if (stopped || document.hidden) return;
    timer = setTimeout(poll, INTERVAL_MS);
  }

  function poll() {
    if (stopped || inflight || document.hidden) return;
    inflight = true;
    clearTimeout(timer);
    timer = null;
    var ctl = new AbortController();
    var to = setTimeout(function () { ctl.abort(); }, TIMEOUT_MS);
    fetch(API, { credentials: 'same-origin', cache: 'no-store', redirect: 'manual', headers: { 'Accept': 'application/json' }, signal: ctl.signal })
      .then(function (res) {
        if (res.status === 401 || res.status === 403 || res.type === 'opaqueredirect') {
          stopped = true;
          try { ctl.abort(); } catch (e) { /* 忽略 */ }
          setStatus('denied', (res.status === 403 ? '无权限（403）：只有当前有效的平台管理员可以查看' : '未登录或会话失效（' + (res.status || '重定向') + '）') + '；已停止轮询，请重新登录后刷新页面');
          return null;
        }
        if (!res.ok) { ctl.abort(); throw new Error('HTTP ' + res.status); }
        var len = Number(res.headers.get('Content-Length') || 0);
        if (len > MAX_BYTES) { ctl.abort(); throw new Error('响应过大（声明 ' + len + ' 字节）'); }
        return readBounded(res, ctl).then(function (text) {
          var snap;
          try { snap = JSON.parse(text); } catch (e) { throw new Error('快照不是合法 JSON'); }
          commit(snap);
          return null;
        });
      })
      .catch(function (e) {
        lastErr = e && e.name === 'AbortError' ? '超时（' + TIMEOUT_MS / 1000 + ' s）' : (e && e.message ? e.message : '未知错误');
      })
      .then(function () {
        clearTimeout(to);
        inflight = false;
        refreshBanner();
        schedule();
      });
  }

  document.addEventListener('visibilitychange', function () {
    if (stopped) return;
    if (document.hidden) { clearTimeout(timer); timer = null; refreshBanner(); }
    else { refreshBanner(); poll(); }
  });
  setInterval(refreshBanner, 5000);
  window.__dwMonitorState = function () { return { inflight: inflight, stopped: stopped, hasTimer: timer !== null }; };
  poll();
})();
`

var monitorPageHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>DingWei 运维监控</title>
<style>` + monitorPageStyle + `</style></head>
<body>
<h1>DingWei 运维监控（只读）</h1>
<div class="note">数据来自本 Hub 进程的内存快照（自进程启动起，未持久化）；View、浏览器上报、飞书、node 等未接埋点的面在下方明确列出。</div>
<div id="status" class="status">正在取样…</div>
<div id="data"></div>
<script>` + monitorPageScript + `</script>
</body></html>
`

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// monitorPageCSP 只放行本页这一段内联脚本与样式（按内容哈希），只许同源 fetch，禁止任何外部资源、表单与被嵌入。
var monitorPageCSP = strings.Join([]string{
	"default-src 'none'",
	"script-src " + cspHash(monitorPageScript),
	"style-src " + cspHash(monitorPageStyle),
	"connect-src 'self'",
	"img-src 'none'",
	"base-uri 'none'",
	"form-action 'none'",
	"frame-ancestors 'none'",
}, "; ")

// MonitorPageHandler 输出独立监控页。本函数不鉴权：路由上必须套 RequireConsolePrincipal＋RequireConsolePlatformAdmin。
func MonitorPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", monitorPageCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	_, _ = w.Write([]byte(monitorPageHTML))
}
