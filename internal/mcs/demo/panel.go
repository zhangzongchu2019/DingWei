// panel.go —— 极简只读任务面板页(演示版)。
//
// 只读铁律:仅 GET/HEAD(其余 405);零写路由;数据全部来自 Board 只读查询;
// 由 cmd 侧强制 bind 127.0.0.1。正式内嵌 UI 属 P4,本页不替代它。
package demo

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
)

// tsSpan 时间戳占位:后端只给 UTC epoch(data-ts),前端 JS 按【查看者本地时区】渲染
// (manager 07-24 20:52 要求:客户端 toLocaleString,非固定服务器时区;各时区的人各看各的本地时间)。
// 无 JS 兜底文本=UTC 带 Z(不含糊)。mode:full=月-日 时:分:秒 / clock=时:分:秒。
func tsSpan(ts int64, mode string) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(ts, 0).UTC()
	fb := t.Format("15:04:05") + "Z"
	if mode == "full" {
		fb = t.Format("01-02 15:04:05") + "Z"
	}
	return fmt.Sprintf(`<span class="ts" data-m="%s" data-ts="%d">%s</span>`, mode, ts, fb)
}

// tsScript 页尾本地化脚本(只读展示,无任何请求/写)。
const tsScript = `<script>
document.querySelectorAll('.ts').forEach(function(e){
  var d=new Date(e.dataset.ts*1000);
  e.textContent = e.dataset.m==='full'
    ? d.toLocaleString([],{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false})
    : d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false});
});
var tz=document.getElementById('tzname'); if(tz){tz.textContent=Intl.DateTimeFormat().resolvedOptions().timeZone;}
</script>`

// nodeTimes 节点操作时间线:从事件账取 派发/开始/完成 时刻(★张宗楚:每一步的操作时间线)。
func nodeTimes(b *board.Board, nodeID string) (disp, start, done int64) {
	rows, err := b.DB().Query(`
SELECT kind, MAX(ts) FROM board_event
 WHERE node_id=? AND kind IN ('node_dispatched','node_started','node_done','node_failed')
 GROUP BY kind`, nodeID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var ts int64
		if rows.Scan(&kind, &ts) != nil {
			return
		}
		switch kind {
		case "node_dispatched":
			disp = ts
		case "node_started":
			start = ts
		case "node_done", "node_failed":
			done = ts
		}
	}
	return
}

// PanelHandler 只读面板(自动 5s 刷新)。
func PanelHandler(b *board.Board) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "只读面板:仅 GET", http.StatusMethodNotAllowed)
			return
		}
		var sb strings.Builder
		sb.WriteString(`<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="5">
<title>多CLI调度·任务面板(演示)</title><style>
body{font:14px/1.5 sans-serif;margin:24px;color:#222}h1{font-size:18px}h2{font-size:15px;margin-top:20px}
table{border-collapse:collapse;margin:6px 0}td,th{border:1px solid #ccc;padding:3px 8px;text-align:left}
th{background:#f2f2f2}.done{color:#0a7a2f}.failed,.rej{color:#b00020}.running,.dispatched{color:#8a5a00}
.blocked{color:#b00020;font-weight:bold}small{color:#777}</style>
<h1>多CLI调度·任务面板 <small>(演示版·只读·5s 自刷新·`)
		sb.WriteString(time.Now().UTC().Format("15:04:05 UTC"))
		sb.WriteString(`)</small></h1>`)

		ctx := r.Context()
		rows, err := b.DB().QueryContext(ctx, `SELECT task_id, title, status FROM task ORDER BY created_ts`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type trow struct{ id, title, status string }
		var tasks []trow
		for rows.Next() {
			var t trow
			if err := rows.Scan(&t.id, &t.title, &t.status); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tasks = append(tasks, t)
		}
		rows.Close()

		for _, t := range tasks {
			led, err := b.TaskLedger(ctx, t.id)
			if err != nil {
				continue
			}
			sb.WriteString(fmt.Sprintf(`<h2>任务 %s — %s <small>进度 %d/%d · 调度者 %s</small></h2>`,
				html.EscapeString(t.id), html.EscapeString(t.title), led.NodesDone, led.NodesTotal, html.EscapeString(led.Dispatcher)))
			sb.WriteString(`<table><tr><th>角色</th><th>谁</th><th>CLI</th><th>模型</th></tr>`)
			for _, role := range []string{"designer", "implementer", "tester", "qa_owner"} {
				rb := led.Roles[role]
				sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
					role, html.EscapeString(rb.Who), html.EscapeString(rb.CLI), html.EscapeString(rb.LLM)))
			}
			sb.WriteString(`</table><table><tr><th>节点</th><th>状态</th><th>代</th><th>执行者</th><th>派发</th><th>开始</th><th>完成</th><th>用时</th><th>卡点</th></tr>`)
			nrows, err := b.DB().QueryContext(ctx, `
SELECT node_id, status, generation, COALESCE(assigned_agent,''), COALESCE(blocker_reason,'')
  FROM node WHERE task_id=? ORDER BY seq`, t.id)
			if err == nil {
				// ⚠先收集再嵌套查:Board 是单连接池(MaxOpenConns=1),游标未关时再 Query=自死锁
				type nrow struct {
					id, st, ag, br string
					gen            int64
				}
				var ns []nrow
				for nrows.Next() {
					var x nrow
					if nrows.Scan(&x.id, &x.st, &x.gen, &x.ag, &x.br) != nil {
						break
					}
					ns = append(ns, x)
				}
				nrows.Close()
				now := time.Now().Unix()
				for _, x := range ns {
					blk := ""
					if x.br != "" {
						blk = `<span class="blocked">` + html.EscapeString(x.br) + `</span>`
					}
					// ★操作时间线(张宗楚 07-24):派发/开始/完成时刻+用时=派发→完成全程
					// (模拟工人的开始/完成回执几乎同刻,按全程算才反映"这一步花了多久";未完=已进行,斜体)
					disp, start, done := nodeTimes(b, x.id)
					dur := ""
					switch {
					case disp > 0 && done > 0:
						dur = fmt.Sprintf("%ds", done-disp)
					case disp > 0:
						dur = fmt.Sprintf("<i>%ds…</i>", now-disp)
					}
					sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="%s">%s</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
						html.EscapeString(x.id), x.st, x.st, x.gen, html.EscapeString(x.ag),
						tsSpan(disp, "clock"), tsSpan(start, "clock"), tsSpan(done, "clock"), dur, blk))
				}
			}
			sb.WriteString(`</table>`)
		}

		sb.WriteString(`<h2>最近操作账(30 条)<small> 时间列按您的本地时区(<span id="tzname">UTC</span>)显示</small></h2><table><tr><th>#</th><th>时间</th><th>事件</th><th>任务/节点</th><th>谁做的</th><th>详情</th></tr>`)
		erows, err := b.DB().QueryContext(ctx, `
SELECT cursor, ts, kind, COALESCE(task_id,''), COALESCE(node_id,''), COALESCE(agent_id,''), data
  FROM board_event ORDER BY cursor DESC LIMIT 30`)
		if err == nil {
			for erows.Next() {
				var cur, ts int64
				var kind, tid, nid, actor, data string
				if erows.Scan(&cur, &ts, &kind, &tid, &nid, &actor, &data) != nil {
					break
				}
				cls := ""
				if strings.Contains(kind, "rejected") {
					cls = ` class="rej"`
				}
				loc := tid
				if nid != "" {
					loc = nid
				}
				sb.WriteString(fmt.Sprintf(`<tr><td>%d</td><td>%s</td><td%s>%s</td><td>%s</td><td>%s</td><td><small>%s</small></td></tr>`,
					cur, tsSpan(ts, "full"), cls, html.EscapeString(kind), html.EscapeString(loc), html.EscapeString(actor), html.EscapeString(data)))
			}
			erows.Close()
		}
		sb.WriteString(`</table>`)
		sb.WriteString(tsScript) // ★时间本地化:后端只给 UTC epoch,查看者各按自己时区看
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sb.String()))
	})
}
