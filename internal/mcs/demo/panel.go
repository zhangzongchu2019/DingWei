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
			sb.WriteString(`</table><table><tr><th>节点</th><th>状态</th><th>代</th><th>执行者</th><th>卡点</th></tr>`)
			nrows, err := b.DB().QueryContext(ctx, `
SELECT node_id, status, generation, COALESCE(assigned_agent,''), COALESCE(blocker_reason,'')
  FROM node WHERE task_id=? ORDER BY seq`, t.id)
			if err == nil {
				for nrows.Next() {
					var id, st, ag, br string
					var gen int64
					if nrows.Scan(&id, &st, &gen, &ag, &br) != nil {
						break
					}
					blk := ""
					if br != "" {
						blk = `<span class="blocked">` + html.EscapeString(br) + `</span>`
					}
					sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="%s">%s</td><td>%d</td><td>%s</td><td>%s</td></tr>`,
						html.EscapeString(id), st, st, gen, html.EscapeString(ag), blk))
				}
				nrows.Close()
			}
			sb.WriteString(`</table>`)
		}

		sb.WriteString(`<h2>最近操作账(30 条)</h2><table><tr><th>#</th><th>事件</th><th>任务/节点</th><th>谁做的</th><th>详情</th></tr>`)
		erows, err := b.DB().QueryContext(ctx, `
SELECT cursor, kind, COALESCE(task_id,''), COALESCE(node_id,''), COALESCE(agent_id,''), data
  FROM board_event ORDER BY cursor DESC LIMIT 30`)
		if err == nil {
			for erows.Next() {
				var cur int64
				var kind, tid, nid, actor, data string
				if erows.Scan(&cur, &kind, &tid, &nid, &actor, &data) != nil {
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
				sb.WriteString(fmt.Sprintf(`<tr><td>%d</td><td%s>%s</td><td>%s</td><td>%s</td><td><small>%s</small></td></tr>`,
					cur, cls, html.EscapeString(kind), html.EscapeString(loc), html.EscapeString(actor), html.EscapeString(data)))
			}
			erows.Close()
		}
		sb.WriteString(`</table>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sb.String()))
	})
}
