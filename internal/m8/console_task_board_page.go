package m8

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const consoleTaskBoardPageHTML = `<!doctype html>
<html lang=zh-CN><head><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1">
<title>任务管理</title><style>
:root{color-scheme:light;--bg:#fff;--panel:#f6f8fb;--line:#d8dee8;--text:#17202a;--muted:#667085;--selected:#e8f1ff}
html[data-theme="dark"]{color-scheme:dark;--bg:#111316;--panel:#1b1f24;--line:#38414c;--text:#eef2f6;--muted:#a8b0bb;--selected:#1b3048}
*{box-sizing:border-box}body{margin:0;padding:16px;background:var(--bg);color:var(--text);font:14px/1.45 system-ui,sans-serif}
h1,h2{margin:0 0 10px}.board{display:grid;grid-template-columns:minmax(280px,35%) minmax(0,65%);gap:16px}.panel{min-width:0;padding:12px;border-radius:8px;background:var(--panel)}
table{width:100%;border-collapse:collapse;table-layout:fixed}th,td{padding:8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top;font-weight:400;overflow-wrap:anywhere}th{font-weight:700}
#spec-list tr{cursor:pointer}#spec-list tr.selected{background:var(--selected)}.pager{display:flex;align-items:center;justify-content:center;gap:10px;margin-top:10px}.muted{color:var(--muted)}
.task-toolbar{display:flex;justify-content:space-between;align-items:center}.offline-dot{color:#98a2b3;margin-left:5px}.assignee-select{max-width:100%}
#task-table th:nth-child(1){width:32%}#task-table th:nth-child(2),#task-table th:nth-child(3){width:13%}#task-table th:nth-child(4){width:18%}#task-table th:nth-child(5){width:24%}
@media(max-width:800px){.board{grid-template-columns:1fr}}
</style></head><body><h1>任务管理</h1><div class=board>
<section id=spec-panel class=panel><h2>spec 列表</h2><table><thead><tr><th>spec</th><th>状态</th></tr></thead><tbody id=spec-list></tbody></table><div class=pager><button id=spec-prev type=button>上一页</button><span id=spec-page></span><button id=spec-next type=button>下一页</button></div></section>
<section id=task-panel class=panel><div class=task-toolbar><h2>任务分解列表</h2><span><button id=edit-tasks type=button>编辑</button><button id=finish-tasks type=button hidden>编辑完成</button></span></div><div id=task-status role=status></div><table id=task-table><thead><tr><th>任务文本</th><th>研发</th><th>验收</th><th>状态</th><th>验收标准</th></tr></thead><tbody id=task-list></tbody></table><div class=pager><button id=task-prev type=button>上一页</button><span id=task-page></span><button id=task-next type=button>下一页</button></div></section>
</div><script>
let specPage=1,taskPage=1,selectedSpec='',editing=false,onlineAgents=[],currentTasks=[];
function acceptanceDisplay(raw){try{const value=JSON.parse(raw||'[]'),list=Array.isArray(value)?value:(Array.isArray(value.criteria)?value.criteria:[]),ids=[],texts=[];list.forEach(function(v){const id=String(v.id||'').trim(),text=String(v.text||'').trim();if(id)ids.push(id);if(text)texts.push((id?id+' ':'')+text)});return {label:ids.join(', '),title:texts.join('\n')}}catch(_){return {label:'',title:''}}}
function setPager(prefix,page,pages){document.getElementById(prefix+'-page').textContent=page+' / '+pages;document.getElementById(prefix+'-prev').disabled=page<=1;document.getElementById(prefix+'-next').disabled=page>=pages}
function taskCell(value,title){const td=document.createElement('td');td.textContent=value||'';if(title)td.title=title;return td}
function assigneeCell(task,role){const td=document.createElement('td'),idKey=role+'_agent_id',nameKey=role+'_agent',onlineKey=role+'_online';if(!editing||!String(task.review_id||'').startsWith('manual:')){td.textContent=task[nameKey]||'';if(task[idKey]&&!task[onlineKey]){const dot=document.createElement('span');dot.className='offline-dot';dot.title='当前离线';dot.textContent='●';td.append(dot)}return td}const select=document.createElement('select');select.className='assignee-select '+role+'-agent';const blank=document.createElement('option');blank.value='';blank.textContent='';select.append(blank);if(task[idKey]&&!task[onlineKey]){const assigned=document.createElement('option');assigned.value=task[idKey];assigned.textContent=task[nameKey]||task[idKey];assigned.disabled=true;assigned.selected=true;assigned.dataset.assignedOffline='true';select.append(assigned)}onlineAgents.forEach(function(agent){const option=document.createElement('option');option.value=agent.id;option.textContent=agent.session_name;option.selected=agent.id===task[idKey];select.append(option)});select.onchange=function(){task[idKey]=select.value;saveAssignment(task)};td.append(select);return td}
async function saveAssignment(task){const status=document.getElementById('task-status'),r=await fetch('/api/console/task-board/tasks/'+encodeURIComponent(task.proposal_id),{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({developer_agent_id:task.developer_agent_id||'',reviewer_agent_id:task.reviewer_agent_id||''})});status.textContent=r.ok?'已保存':(await r.text()).trim();if(r.ok)await loadBoard()}
function renderTasks(tasks){currentTasks=tasks||[];const body=document.getElementById('task-list');body.replaceChildren();currentTasks.forEach(function(task){const tr=document.createElement('tr'),acceptance=acceptanceDisplay(task.acceptance_json);tr.dataset.taskId=task.proposal_id;tr.append(taskCell(task.content||task.title),assigneeCell(task,'developer'),assigneeCell(task,'reviewer'),taskCell(String(task.passed_cases||0)+'/'+String(task.total_cases||1)),taskCell(acceptance.label,acceptance.title));body.append(tr)})}
function renderSpecs(specs){const body=document.getElementById('spec-list');body.replaceChildren();(specs||[]).forEach(function(spec){const tr=document.createElement('tr');tr.dataset.specId=spec.id;tr.classList.toggle('selected',spec.id===selectedSpec);tr.append(taskCell(spec.title),taskCell(spec.state));tr.onclick=function(){selectedSpec=selectedSpec===spec.id?'':spec.id;taskPage=1;loadBoard()};body.append(tr)})}
async function loadBoard(){const q=new URLSearchParams({spec_page:String(specPage),task_page:String(taskPage)});if(selectedSpec)q.set('spec_id',selectedSpec);const r=await fetch('/api/console/task-board?'+q,{cache:'no-store'});if(!r.ok)throw new Error('task board unavailable');const data=await r.json();specPage=data.spec_page;taskPage=data.task_page;onlineAgents=data.agents||[];renderSpecs(data.specs);renderTasks(data.tasks);setPager('spec',data.spec_page,data.spec_total_pages);setPager('task',data.task_page,data.task_total_pages)}
document.getElementById('edit-tasks').onclick=function(){editing=true;this.hidden=true;document.getElementById('finish-tasks').hidden=false;renderTasks(currentTasks)};document.getElementById('finish-tasks').onclick=function(){editing=false;this.hidden=true;document.getElementById('edit-tasks').hidden=false;renderTasks(currentTasks)};
document.getElementById('spec-prev').onclick=function(){if(specPage>1){specPage--;loadBoard()}};document.getElementById('spec-next').onclick=function(){specPage++;loadBoard()};document.getElementById('task-prev').onclick=function(){if(taskPage>1){taskPage--;loadBoard()}};document.getElementById('task-next').onclick=function(){taskPage++;loadBoard()};
loadBoard();
</script></body></html>`

const (
	consoleTaskBoardSpecPageSize = 8
	consoleTaskBoardTaskPageSize = 20
)

func (h *Hub) HandleConsoleTaskBoardPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, consoleTaskBoardPageHTML)
}

func positivePage(raw string) (int, bool) {
	if raw == "" {
		return 1, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n > 0
}

func pageConsoleSpecs(items []model.ConsoleSpec, page, size int) ([]model.ConsoleSpec, int) {
	pages := (len(items) + size - 1) / size
	if pages == 0 {
		pages = 1
	}
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], pages
}

func pageConsoleTasks(items []model.ConsoleTaskProposal, page, size int) ([]model.ConsoleTaskProposal, int) {
	pages := (len(items) + size - 1) / size
	if pages == 0 {
		pages = 1
	}
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], pages
}

func (h *Hub) HandleConsoleTaskBoard(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	specPage, specOK := positivePage(r.URL.Query().Get("spec_page"))
	taskPage, taskOK := positivePage(r.URL.Query().Get("task_page"))
	if !specOK || !taskOK {
		http.Error(w, "invalid page", http.StatusBadRequest)
		return
	}
	principal := platformPrincipal(r)
	specs, err := repo.ListConsoleSpecs(r.Context(), principal, false)
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	selectedID := strings.TrimSpace(r.URL.Query().Get("spec_id"))
	selected := false
	for i := range specs {
		if specs[i].ID == selectedID {
			selected = true
			break
		}
	}
	var tasks []model.ConsoleTaskProposal
	if selected {
		tasks, err = repo.ListConsoleTaskProposals(r.Context(), selectedID)
		if err != nil {
			http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	agents, err := repo.ListConsoleAgents(r.Context(), principal)
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	online := make([]model.ConsoleAgent, 0, len(agents))
	onlineIDs := make(map[string]bool, len(agents))
	for _, agent := range agents {
		if h.consoleTaskAgentOnline(agent) {
			online = append(online, agent)
			onlineIDs[agent.ID] = true
		}
	}
	for i := range tasks {
		tasks[i].DeveloperOnline = tasks[i].DeveloperAgentID != "" && onlineIDs[tasks[i].DeveloperAgentID]
		tasks[i].ReviewerOnline = tasks[i].ReviewerAgentID != "" && onlineIDs[tasks[i].ReviewerAgentID]
		tasks[i].TotalCases = 1
		if strings.HasPrefix(tasks[i].ReviewID, "manual:") {
			tasks[i].PassedCases, tasks[i].TotalCases, err = repo.ConsoleSpecTaskCaseProgress(r.Context(), tasks[i].ID)
			if err != nil {
				http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
				return
			}
		}
	}
	visibleSpecs, specPages := pageConsoleSpecs(specs, specPage, consoleTaskBoardSpecPageSize)
	visibleTasks, taskPages := pageConsoleTasks(tasks, taskPage, consoleTaskBoardTaskPageSize)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"specs": visibleSpecs, "spec_page": specPage, "spec_page_size": consoleTaskBoardSpecPageSize, "spec_total_pages": specPages,
		"tasks": visibleTasks, "agents": online, "task_page": taskPage, "task_page_size": consoleTaskBoardTaskPageSize, "task_total_pages": taskPages,
	})
}

func (h *Hub) HandleConsoleTaskBoardAssignment(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DeveloperAgentID string `json:"developer_agent_id"`
		ReviewerAgentID  string `json:"reviewer_agent_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		http.Error(w, "invalid task assignment", http.StatusBadRequest)
		return
	}
	principal := platformPrincipal(r)
	specs, err := repo.ListConsoleSpecs(r.Context(), principal, false)
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	ownedSpecs := make(map[string]bool, len(specs))
	for _, spec := range specs {
		ownedSpecs[spec.ID] = true
	}
	tasks, err := repo.ListAllConsoleTaskProposals(r.Context())
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	var current *model.ConsoleTaskProposal
	for i := range tasks {
		if tasks[i].ID == r.PathValue("taskID") && ownedSpecs[tasks[i].SpecID] {
			current = &tasks[i]
			break
		}
	}
	if current == nil {
		http.NotFound(w, r)
		return
	}
	agents, err := repo.ListConsoleAgents(r.Context(), principal)
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	online := make(map[string]bool, len(agents))
	for _, agent := range agents {
		online[agent.ID] = h.consoleTaskAgentOnline(agent)
	}
	req.DeveloperAgentID = strings.TrimSpace(req.DeveloperAgentID)
	req.ReviewerAgentID = strings.TrimSpace(req.ReviewerAgentID)
	if (req.DeveloperAgentID != "" && req.DeveloperAgentID != current.DeveloperAgentID && !online[req.DeveloperAgentID]) ||
		(req.ReviewerAgentID != "" && req.ReviewerAgentID != current.ReviewerAgentID && !online[req.ReviewerAgentID]) {
		http.Error(w, "task assignee must be online", http.StatusConflict)
		return
	}
	if err := repo.UpdateConsoleSpecTaskAssignees(r.Context(), current.ID, principal, req.DeveloperAgentID, req.ReviewerAgentID); err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecPermission) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) consoleTaskAgentOnline(agent model.ConsoleAgent) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionClients[agent.KeyID] != nil && h.sessionClients[agent.KeyID][agent.SessionName] != nil
}
