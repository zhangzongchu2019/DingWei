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
// 页面隐藏即暂停、恢复可见立即取一次；401／403 停止轮询并显示；取样失败或超过 75 s 没有新样本标「陈旧」，
// 但保留上一次的值并写明它的取样时刻；null 显示 UNKNOWN（附原因），不显示成 0。
// 响应字节与渲染行数都有上限；只用 textContent／createElement，不用 innerHTML；没有外部资源、不写任何数据、
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
  var MAX_BYTES = 1048576, MAX_HTTP_ROWS = 64, MAX_STREAM_ROWS = 32, MAX_WS_ROWS = 8, MAX_REASONS = 16, MAX_NI_ROWS = 16, MAX_TEXT = 200;
  var inflight = false, timer = null, stopped = false, lastOk = null, lastOkAt = null, lastErr = '';

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
  // null／缺失 ⇒ UNKNOWN（附原因），绝不当 0。
  function cellValue(v, reason) {
    if (v === null || v === undefined) return el('span', reason ? 'UNKNOWN（' + reason + '）' : 'UNKNOWN', 'unknown');
    return el('span', v);
  }
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
    if (!h || !isNum(h.count) || h.count === 0 || !Array.isArray(h.counts) || !Array.isArray(h.le)) return '—（无样本）';
    var target = Math.ceil(h.count * 0.95), acc = 0;
    for (var i = 0; i < h.counts.length; i++) {
      acc += h.counts[i];
      if (acc >= target) return h.le[i] === null ? '> ' + h.le[i - 1] : '≤ ' + h.le[i];
    }
    return '—';
  }
  function mean(h) {
    return h && isNum(h.count) && h.count > 0 && isNum(h.sum) ? (h.sum / h.count).toFixed(1) : '—（无样本）';
  }
  function truncNote(box, total, cap) {
    if (total > cap) box.appendChild(el('div', '只显示前 ' + cap + ' 行，共 ' + total + ' 行', 'note'));
  }

  function render(s) {
    var root = document.getElementById('data');
    clear(root);
    var id = s.identity || {}, unk = id.unknown || {};
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
    row(t, ['uptime_seconds（进程存续，非请求时长）', isNum(s.uptime_seconds) ? s.uptime_seconds.toFixed(0) : cellValue(null)]);
    root.appendChild(t);

    var http = s.http || {}, series = Array.isArray(http.series) ? http.series : [];
    root.appendChild(el('h2', '短请求（按注册路由；不含 SSE 与升级连接）'));
    root.appendChild(el('div', http.note || '', 'note'));
    t = table(['server', 'route', 'method', 'class', 'count', 'mean ms', 'p95（桶上界）']);
    series.slice(0, MAX_HTTP_ROWS).forEach(function (r) {
      var h = r.latency_ms || {};
      row(t, [r.server, r.route, r.method, r['class'], isNum(h.count) ? h.count : null, mean(h), p95(h)]);
    });
    root.appendChild(t);
    truncNote(root, series.length, MAX_HTTP_ROWS);
    var ov = Array.isArray(http.overflow) ? http.overflow : [];
    root.appendChild(el('div', '序列上限 ' + (isNum(http.max_series) ? http.max_series : 'UNKNOWN') + '；溢出样本 ' +
      (isNum(http.dropped_samples) ? http.dropped_samples : 'UNKNOWN') + '；溢出桶 ' + ov.length + ' 个', 'note'));

    var st = s.streams || {}, ss = Array.isArray(st.series) ? st.series : [];
    root.appendChild(el('h2', '长连接（SSE／升级；时长在连接结束时计入）'));
    t = table(['server', 'route', 'kind', 'opened', 'closed', 'active', 'bytes（已结束）', 'p95 存续 s（桶上界）']);
    ss.slice(0, MAX_STREAM_ROWS).forEach(function (r) {
      row(t, [r.server, r.route, r.kind, r.opened, r.closed, r.active, r.bytes_written_closed, p95(r.duration_s)]);
    });
    root.appendChild(t);
    truncNote(root, ss.length, MAX_STREAM_ROWS);
    root.appendChild(el('div', '溢出样本 ' + (isNum(st.dropped_samples) ? st.dropped_samples : 'UNKNOWN'), 'note'));

    var ws = Array.isArray(s.ws) ? s.ws : [];
    root.appendChild(el('h2', 'WebSocket（仅已接 hook 的种类；0 为实测）'));
    t = table(['kind', 'opened', 'active', 'closed_by_reason', 'ping ok', 'ping fail', 'ping p95 ms（桶上界）']);
    ws.slice(0, MAX_WS_ROWS).forEach(function (r) {
      var reasons = r.closed_by_reason && typeof r.closed_by_reason === 'object' ? Object.keys(r.closed_by_reason).sort() : [];
      var parts = reasons.slice(0, MAX_REASONS).map(function (k) { return k + '=' + r.closed_by_reason[k]; });
      if (reasons.length > MAX_REASONS) parts.push('…');
      row(t, [r.kind, r.opened, r.active, parts.length ? parts.join(' ') : '（无关闭）', r.ping_ok, r.ping_fail, p95(r.ping_rtt_ms)]);
    });
    root.appendChild(t);
    truncNote(root, ws.length, MAX_WS_ROWS);

    var ni = Array.isArray(s.not_instrumented) ? s.not_instrumented : [];
    root.appendChild(el('h2', '未接埋点（NOT_INSTRUMENTED，不是 0，也不是 PASS）'));
    t = table(['area', 'status', 'reason']);
    ni.slice(0, MAX_NI_ROWS).forEach(function (r) {
      var c = el('span', r.status, 'ni');
      row(t, [r.area, c, r.reason]);
    });
    root.appendChild(t);
    truncNote(root, ni.length, MAX_NI_ROWS);
  }

  function refreshBanner() {
    if (stopped) return;
    if (document.hidden) { setStatus('paused', '页面已隐藏：轮询暂停，回到页面会立即取样'); return; }
    if (!lastOkAt) { if (lastErr) setStatus('error', '取样失败：' + lastErr + '；还没有任何有效样本'); return; }
    var age = Date.now() - lastOkAt.getTime(), at = lastOkAt.toISOString();
    var sAt = lastOk && lastOk.snapshot_at ? fmtTime(lastOk.snapshot_at) : 'UNKNOWN';
    if (lastErr) setStatus('error', '最近一次取样失败：' + lastErr + '；下面是上一次成功样本（服务端取样 ' + sAt + '，页面收到 ' + at + '）');
    else if (age > STALE_MS) setStatus('stale', '陈旧：' + Math.round(age / 1000) + ' s 没有新样本；下面是服务端 ' + sAt + ' 的值');
    else setStatus('ok', '服务端取样 ' + sAt + '；页面收到 ' + at + '；每 30 s 刷新');
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
          setStatus('denied', (res.status === 403 ? '无权限（403）：只有当前有效的平台管理员可以查看' : '未登录或会话失效（' + (res.status || '重定向') + '）') + '；已停止轮询，请重新登录后刷新页面');
          return null;
        }
        if (!res.ok) throw new Error('HTTP ' + res.status);
        var len = Number(res.headers.get('Content-Length') || 0);
        if (len > MAX_BYTES) throw new Error('响应过大（' + len + ' 字节）');
        return res.text().then(function (text) {
          if (text.length > MAX_BYTES) throw new Error('响应过大');
          var snap = JSON.parse(text);
          if (!snap || snap.schema !== 'dw-obs/v1') throw new Error('快照格式不认识');
          lastOk = snap; lastOkAt = new Date(); lastErr = '';
          render(snap);
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
