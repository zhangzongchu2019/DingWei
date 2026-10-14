package m8

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const consoleTaskBoardPageHTML = `<!doctype html>
<html lang=zh-CN><head><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1">
<title>任务管理</title><style>
:root{color-scheme:light;--bg:#fff;--panel:#f6f8fb;--line:#d8dee8;--text:#17202a;--muted:#667085;--selected:#e8f1ff}
html[data-theme="dark"]{color-scheme:dark;--bg:#111316;--panel:#1b1f24;--line:#38414c;--text:#eef2f6;--muted:#a8b0bb;--selected:#1b3048}
*{box-sizing:border-box}body{margin:0;padding:16px;background:var(--bg);color:var(--text);font:14px/1.45 system-ui,sans-serif}
h1,h2{margin:0 0 10px}.board{display:grid;grid-template-columns:minmax(280px,35%) minmax(0,65%);gap:16px}.panel{min-width:0;padding:12px;border-radius:8px;background:var(--panel)}.active-panel{grid-column:1 / -1}
table{width:100%;border-collapse:collapse;table-layout:fixed}th,td{padding:8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top;font-weight:400;overflow-wrap:anywhere}th{font-weight:700}
#spec-list tr{cursor:pointer}#spec-list tr.selected{background:var(--selected)}.pager{display:flex;align-items:center;justify-content:center;gap:10px;margin-top:10px}.muted{color:var(--muted)}
.task-toolbar{display:flex;justify-content:space-between;align-items:center}.offline-dot{color:#98a2b3;margin-left:5px}.assignee-select{max-width:100%}
#task-table th:nth-child(1){width:32%}#task-table th:nth-child(2),#task-table th:nth-child(3){width:13%}#task-table th:nth-child(4){width:18%}#task-table th:nth-child(5){width:24%}
@media(max-width:800px){.board{grid-template-columns:1fr}}
</style></head><body><h1>任务管理</h1><div class=board>
<section id=spec-panel class=panel><h2>spec 列表</h2><button id=all-owned-tasks type=button>本人全部任务</button><table><thead><tr><th>spec</th><th>状态</th></tr></thead><tbody id=spec-list></tbody></table><div class=pager><button id=spec-prev type=button>上一页</button><span id=spec-page></span><button id=spec-next type=button>下一页</button></div></section>
<section id=task-panel class=panel><div class=task-toolbar><h2>任务列表</h2><span><button id=refresh-tasks type=button>刷新</button><button id=edit-tasks type=button>编辑</button><button id=finish-tasks type=button hidden>编辑完成</button></span></div><div id=task-status role=status></div><div id=task-updated class=muted></div><table id=task-table><thead><tr><th>任务文本</th><th>研发</th><th>验收</th><th>状态</th><th>验收标准</th></tr></thead><tbody id=task-list></tbody></table><div class=pager><button id=task-prev type=button>上一页</button><span id=task-page></span><button id=task-next type=button>下一页</button></div></section>
</div><section class="panel active-panel"><h2>进行中任务</h2><table><thead><tr><th>任务</th><th>状态</th><th>开始时间</th><th>执行人</th><th>验收人</th><th>计划完成</th><th>堵塞点</th><th>资源协调</th></tr></thead><tbody id="active-task-list"></tbody></table></section><script>
var DW_THEME_KEY='dw-theme',dwTaskThemeChannel=null;
function dwTaskApplyTheme(v){if(v!=='dark'&&v!=='light')return;document.documentElement.dataset.theme=v;if(document.body)document.body.dataset.theme=v;}
function dwTaskInitTheme(){var initial='light';try{var stored=localStorage.getItem(DW_THEME_KEY);if(stored==='dark'||stored==='light')initial=stored;}catch(_){}dwTaskApplyTheme(initial);
 try{if('BroadcastChannel' in window){dwTaskThemeChannel=new BroadcastChannel(DW_THEME_KEY);dwTaskThemeChannel.onmessage=function(e){dwTaskApplyTheme(e&&e.data&&e.data.theme);};}}catch(_){dwTaskThemeChannel=null;}
 try{window.addEventListener('storage',function(e){if(e&&e.key===DW_THEME_KEY)dwTaskApplyTheme(e.newValue);});}catch(_){} }
dwTaskInitTheme();
let specPage=1,taskPage=1,selectedSpec=new URLSearchParams(location.search).get('spec_id')||'',editing=false,onlineAgents=[],currentTasks=[];
function acceptanceDisplay(raw){try{const value=JSON.parse(raw||'[]'),list=Array.isArray(value)?value:(Array.isArray(value.criteria)?value.criteria:[]),ids=[],texts=[];list.forEach(function(v){const id=String(v.id||'').trim(),text=String(v.text||'').trim();if(id)ids.push(id);if(text)texts.push((id?id+' ':'')+text)});return {label:ids.join(', '),title:texts.join('\n')}}catch(_){return {label:'',title:''}}}
function setPager(prefix,page,pages){document.getElementById(prefix+'-page').textContent=page+' / '+pages;document.getElementById(prefix+'-prev').disabled=page<=1;document.getElementById(prefix+'-next').disabled=page>=pages}
function taskCell(value,title){const td=document.createElement('td');td.textContent=value||'';if(title)td.title=title;return td}
function assigneeCell(task,role){const td=document.createElement('td'),idKey=role+'_agent_id',nameKey=role+'_agent',onlineKey=role+'_online';if(!editing||!String(task.review_id||'').startsWith('manual:')){td.textContent=task[nameKey]||'';if(task[idKey]&&!task[onlineKey]){const dot=document.createElement('span');dot.className='offline-dot';dot.title='当前离线';dot.textContent='●';td.append(dot)}return td}const select=document.createElement('select');select.className='assignee-select '+role+'-agent';const blank=document.createElement('option');blank.value='';blank.textContent='';select.append(blank);if(task[idKey]&&!task[onlineKey]){const assigned=document.createElement('option');assigned.value=task[idKey];assigned.textContent=task[nameKey]||task[idKey];assigned.disabled=true;assigned.selected=true;assigned.dataset.assignedOffline='true';select.append(assigned)}onlineAgents.forEach(function(agent){const option=document.createElement('option');option.value=agent.id;option.textContent=agent.session_name;option.selected=agent.id===task[idKey];select.append(option)});select.onchange=function(){task[idKey]=select.value;task.assignment_dirty=true};td.append(select);return td}
async function saveAssignment(task){if(boardStopped)return false;const status=document.getElementById('task-status'),r=await fetch('/api/console/task-board/tasks/'+encodeURIComponent(task.proposal_id),{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({developer_agent_id:task.developer_agent_id||'',reviewer_agent_id:task.reviewer_agent_id||''})});if(r.status===401||r.status===403){boardStop('登录或访问权限已失效，请重新打开任务管理');return false}if(boardStopped)return false;if(!r.ok){status.textContent='任务保存失败，编辑内容已保留';return false}task.assignment_dirty=false;return true}
async function saveAssignments(){if(!await loadBoard('verify')||boardStopped)return false;for(const task of currentTasks){if(task.assignment_dirty&&!await saveAssignment(task))return false}document.getElementById('task-status').textContent='已保存';return true}
function taskProgress(task){if(task.developer_agent_id&&!task.reviewer_agent_id)return {text:'缺少验收 Agent，任务无法完成',problem:true};return {text:(task.status?({'planned':'已计划'}[task.status]||task.status)+' · ':'')+String(task.passed_cases||0)+'/'+String(task.total_cases||1),problem:false}}
function renderTasks(tasks){currentTasks=tasks||[];const body=document.getElementById('task-list');body.replaceChildren();currentTasks.forEach(function(task){const tr=document.createElement('tr'),acceptance=acceptanceDisplay(task.acceptance_json),progress=taskProgress(task),progressCell=taskCell(progress.text);tr.dataset.taskId=task.proposal_id;if(progress.problem)progressCell.className='error assignment-problem';tr.append(taskCell(task.content||task.title),assigneeCell(task,'developer'),assigneeCell(task,'reviewer'),progressCell,taskCell(acceptance.label,acceptance.title));body.append(tr)})}
function taskUnknown(v){return v===null||v===undefined||v===''?'未填写/未知':String(v)}
function renderActiveTasks(tasks){const body=document.getElementById('active-task-list');body.replaceChildren();(tasks||[]).forEach(function(task){const tr=document.createElement('tr');tr.append(taskCell(task.content||task.title),taskCell(task.status),taskCell(taskUnknown(task.started_at)),taskCell(taskUnknown(task.developer_agent)),taskCell(taskUnknown(task.reviewer_agent)),taskCell(taskUnknown(task.planned_finish_at)),taskCell(taskUnknown(task.blocker)),taskCell(taskUnknown(task.resource_coordination)));body.append(tr)})}
function renderSpecs(specs){const body=document.getElementById('spec-list');body.replaceChildren();(specs||[]).forEach(function(spec){const tr=document.createElement('tr');tr.dataset.specId=spec.id;tr.classList.toggle('selected',spec.id===selectedSpec);tr.append(taskCell(spec.title),taskCell(spec.state));tr.onclick=function(){boardChangeSelection(function(){selectedSpec=selectedSpec===spec.id?'':spec.id;taskPage=1})};body.append(tr)})}
// DW-OWNED-TASK-BOARD BEGIN: single-flight reads, editing owns its draft objects.
let boardTimer=null,boardFlight=null,boardGeneration=0,boardStopped=false,boardQueued=false,boardViewer='',boardLastGoodAt='',boardSaving=false;
const boardParams=new URLSearchParams(location.search),boardEmbedded=boardParams.get('embedded')==='1';
const boardInstance=boardParams.get('board_instance')||'';
let boardParentActive=!boardEmbedded,boardVisibilityGeneration=-1;
function boardIsActive(){return !document.hidden&&(!boardEmbedded||boardParentActive)}
function boardClearTimer(){if(boardTimer!==null)clearTimeout(boardTimer);boardTimer=null}
function boardSchedule(){boardClearTimer();if(!boardStopped&&!editing&&boardIsActive())boardTimer=setTimeout(function(){boardTimer=null;loadBoard()},60000)}
function boardStop(text){boardStopped=true;boardGeneration++;boardQueued=false;boardClearTimer();if(boardFlight)boardFlight.controller.abort();boardViewer='';boardLastGoodAt='';editing=false;onlineAgents=[];renderSpecs([]);renderTasks([]);document.getElementById('task-updated').textContent='';document.getElementById('task-status').textContent=text;document.getElementById('edit-tasks').disabled=true;document.getElementById('finish-tasks').hidden=true;document.getElementById('refresh-tasks').disabled=true}
function boardQuery(){const q=new URLSearchParams({spec_page:String(specPage),task_page:String(taskPage)});if(selectedSpec)q.set('spec_id',selectedSpec);return q.toString()}
async function loadBoard(mode){
 mode=mode||'auto';boardClearTimer();
 if(boardStopped||!boardIsActive())return null;
 if(editing&&mode!=='verify'){boardQueued=true;return null}
 if(boardFlight){if(mode==='verify'){await boardFlight.promise;return loadBoard('verify')}boardQueued=true;return null}
 const generation=boardGeneration,query=boardQuery(),controller=new AbortController(),token={controller,promise:null};let timeout=null;
 if(mode!=='verify')document.getElementById('task-status').textContent='正在更新任务列表…';
 const run=(async function(){try{
  const deadline=new Promise(function(_,reject){timeout=setTimeout(function(){controller.abort();reject(new Error('deadline'))},5000)});
  const request=(async function(){const r=await fetch('/api/console/task-board?'+query,{cache:'no-store',signal:controller.signal});if(r.status===401||r.status===403){boardStop('登录或访问权限已失效，请重新打开任务管理');return null}if(!r.ok)throw new Error('unavailable');const data=await r.json();if(!data||!Array.isArray(data.specs)||!Array.isArray(data.tasks)||!Array.isArray(data.agents)||typeof data.viewer_key!=='string'||!data.viewer_key)throw new Error('shape');return data})();
  const data=await Promise.race([request,deadline]);
  if(!data||boardStopped||generation!==boardGeneration||query!==boardQuery())return null;
  if(boardViewer&&boardViewer!==data.viewer_key){boardStop('登录身份已变化，请重新打开任务管理');return null}
  boardViewer=data.viewer_key;
  if(mode==='verify')return data;
  if(editing){boardQueued=true;return null}
  specPage=data.spec_page;taskPage=data.task_page;onlineAgents=data.agents;
  renderSpecs(data.specs);renderTasks(data.tasks);renderActiveTasks(data.active_tasks||[]);setPager('spec',data.spec_page,data.spec_total_pages);setPager('task',data.task_page,data.task_total_pages);
  boardLastGoodAt=data.updated_at||new Date().toISOString();
  document.getElementById('task-updated').textContent='更新于 '+boardLastGoodAt;
  document.getElementById('task-status').textContent=selectedSpec?'':'本人名下任务 · '+String(data.task_total??data.tasks.length)+' 项';
  return data;
 }catch(_){if(!boardStopped&&generation===boardGeneration)document.getElementById('task-status').textContent=boardLastGoodAt?'更新失败，保留上次成功列表；当前筛选可能尚未生效':'任务管理暂不可用，请重试';return null}
 finally{if(timeout!==null)clearTimeout(timeout)} })();
 token.promise=run;boardFlight=token;
 try{return await run}finally{if(boardFlight===token)boardFlight=null;if(!boardStopped&&boardIsActive()&&!editing){if(boardQueued){boardQueued=false;loadBoard('manual')}else boardSchedule()}}
}
function boardChangeSelection(change){if(editing||boardSaving||boardStopped)return;change();boardGeneration++;loadBoard('manual')}
function boardVisibilityChanged(){boardClearTimer();boardGeneration++;if(boardStopped)return;if(boardIsActive())loadBoard('manual');else boardQueued=false}
window.addEventListener('message',function(ev){const d=ev.data;if(!boardEmbedded||parent===window||!/^[0-9a-f]{32}$/.test(boardInstance)||ev.source!==parent||ev.origin!==location.origin||!d||d.protocol!=='dw-task-board/v1'||d.instance!==boardInstance||!Number.isSafeInteger(d.generation)||d.generation<boardVisibilityGeneration)return;if(d.type==='invalidate'){boardStop('任务管理已关闭，请重新打开');return}if(d.type!=='visibility'||typeof d.active!=='boolean')return;boardVisibilityGeneration=d.generation;if(boardParentActive!==d.active){boardParentActive=d.active;boardVisibilityChanged()}});
document.addEventListener('visibilitychange',boardVisibilityChanged);
window.addEventListener('pagehide',function(){boardStop('任务管理已关闭')});
document.getElementById('refresh-tasks').onclick=function(){loadBoard('manual')};
document.getElementById('all-owned-tasks').onclick=function(){boardChangeSelection(function(){selectedSpec='';taskPage=1})};
document.getElementById('edit-tasks').onclick=function(){if(boardStopped||!currentTasks.length)return;editing=true;boardGeneration++;boardClearTimer();this.hidden=true;document.getElementById('finish-tasks').hidden=false;renderTasks(currentTasks);document.getElementById('task-status').textContent='编辑中，自动更新将在编辑完成后继续'};
document.getElementById('finish-tasks').onclick=async function(){if(boardSaving||boardStopped)return;boardSaving=true;this.disabled=true;try{if(!await saveAssignments())return;editing=false;this.hidden=true;document.getElementById('edit-tasks').hidden=false;boardGeneration++;boardQueued=false;await loadBoard('manual')}finally{boardSaving=false;this.disabled=false}};
document.getElementById('spec-prev').onclick=function(){boardChangeSelection(function(){if(specPage>1)specPage--})};document.getElementById('spec-next').onclick=function(){boardChangeSelection(function(){specPage++})};document.getElementById('task-prev').onclick=function(){boardChangeSelection(function(){if(taskPage>1)taskPage--})};document.getElementById('task-next').onclick=function(){boardChangeSelection(function(){taskPage++})};
if(boardEmbedded){if(parent!==window&&/^[0-9a-f]{32}$/.test(boardInstance))parent.postMessage({protocol:'dw-task-board/v1',type:'ready',instance:boardInstance},location.origin);else document.getElementById('task-status').textContent='请从工作台重新打开任务管理'}else loadBoard();
// DW-OWNED-TASK-BOARD END
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
	if page > pages {
		page = pages
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
	if page > pages {
		page = pages
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
	if principal == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	allSpecs, err := repo.ListConsoleSpecs(r.Context(), principal, false)
	if err != nil {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	selectedID := strings.TrimSpace(r.URL.Query().Get("spec_id"))
	specs := make([]model.ConsoleSpec, 0, len(allSpecs))
	var selected *model.ConsoleSpec
	for i := range allSpecs {
		spec := &allSpecs[i]
		if spec.ID == selectedID {
			selected = spec // Existing owner/editor read scope, never a query principal.
		}
		if spec.PrincipalID == principal && spec.State == "final" {
			specs = append(specs, *spec)
		}
	}
	if selectedID != "" {
		if selected == nil {
			http.NotFound(w, r)
			return
		}
		if selected.State != "final" {
			http.Error(w, fmt.Sprintf("该 spec 尚未审结(当前状态:%s),审结后才能进入任务管理", selected.State), http.StatusConflict)
			return
		}
		if selected.PrincipalID != principal {
			specs = append(specs, *selected) // Explicitly selected, already authorized.
		}
	}
	tasks := make([]model.ConsoleTaskProposal, 0)
	loadTasks := func(specID string) bool {
		items, loadErr := repo.ListConsoleTaskProposals(r.Context(), specID)
		if loadErr != nil {
			http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
			return false
		}
		tasks = append(tasks, items...)
		return true
	}
	if selected != nil {
		if !loadTasks(selected.ID) {
			return
		}
	} else {
		// All owned final specs, not merely the current eight-item spec page.
		for _, spec := range specs {
			if !loadTasks(spec.ID) {
				return
			}
		}
		sort.SliceStable(tasks, func(i, j int) bool {
			if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
				return tasks[i].ID < tasks[j].ID
			}
			return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
		})
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
	visibleSpecs, specPages := pageConsoleSpecs(specs, specPage, consoleTaskBoardSpecPageSize)
	if specPage > specPages {
		specPage = specPages
		visibleSpecs, _ = pageConsoleSpecs(specs, specPage, consoleTaskBoardSpecPageSize)
	}
	visibleTasks, taskPages := pageConsoleTasks(tasks, taskPage, consoleTaskBoardTaskPageSize)
	if taskPage > taskPages {
		taskPage = taskPages
		visibleTasks, _ = pageConsoleTasks(tasks, taskPage, consoleTaskBoardTaskPageSize)
	}
	for i := range visibleTasks {
		visibleTasks[i].DeveloperOnline = visibleTasks[i].DeveloperAgentID != "" && onlineIDs[visibleTasks[i].DeveloperAgentID]
		visibleTasks[i].ReviewerOnline = visibleTasks[i].ReviewerAgentID != "" && onlineIDs[visibleTasks[i].ReviewerAgentID]
		visibleTasks[i].TotalCases = 1
		if strings.HasPrefix(visibleTasks[i].ReviewID, "manual:") {
			visibleTasks[i].PassedCases, visibleTasks[i].TotalCases, err = repo.ConsoleSpecTaskCaseProgress(r.Context(), visibleTasks[i].ID)
			if err != nil {
				http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"viewer_key": hashConsoleToken("task-board:" + principal), "updated_at": h.now().UTC(), "task_total": len(tasks),
		"specs": visibleSpecs, "spec_page": specPage, "spec_page_size": consoleTaskBoardSpecPageSize, "spec_total_pages": specPages,
		"tasks": visibleTasks, "active_tasks": activeConsoleTasks(tasks), "agents": online, "task_page": taskPage, "task_page_size": consoleTaskBoardTaskPageSize, "task_total_pages": taskPages,
	})
}

func activeConsoleTasks(tasks []model.ConsoleTaskProposal) []model.ConsoleTaskProposal {
	out := make([]model.ConsoleTaskProposal, 0)
	for _, task := range tasks {
		if task.Status == "in_progress" || task.Status == "blocked" {
			out = append(out, task)
		}
	}
	return out
}

func (h *Hub) HandleConsoleTaskBoardAssignment(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console task board unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DeveloperAgentID     string  `json:"developer_agent_id"`
		ReviewerAgentID      string  `json:"reviewer_agent_id"`
		StartedAt            *string `json:"started_at"`
		PlannedFinishAt      *string `json:"planned_finish_at"`
		Blocker              *string `json:"blocker"`
		ResourceCoordination *string `json:"resource_coordination"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		http.Error(w, "invalid task assignment", http.StatusBadRequest)
		return
	}
	parseTaskTime := func(raw *string) (*time.Time, bool) {
		if raw == nil {
			return nil, true
		}
		v, err := time.Parse(time.RFC3339, *raw)
		if err != nil {
			return nil, false
		}
		v = v.UTC()
		return &v, true
	}
	startedAt, startedOK := parseTaskTime(req.StartedAt)
	plannedAt, plannedOK := parseTaskTime(req.PlannedFinishAt)
	if !startedOK || !plannedOK {
		http.Error(w, "invalid task time", http.StatusBadRequest)
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
	if (req.DeveloperAgentID == "") != (req.ReviewerAgentID == "") {
		http.Error(w, store.ErrConsoleTaskAssigneePair.Error(), http.StatusConflict)
		return
	}
	if req.DeveloperAgentID != "" && req.DeveloperAgentID == req.ReviewerAgentID {
		http.Error(w, store.ErrConsoleTaskAssigneeSame.Error(), http.StatusConflict)
		return
	}
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
	if req.StartedAt != nil || req.PlannedFinishAt != nil || req.Blocker != nil || req.ResourceCoordination != nil {
		if err := repo.UpdateConsoleSpecTaskMetadata(r.Context(), current.ID, principal, startedAt, plannedAt, req.Blocker, req.ResourceCoordination); err != nil {
			code := http.StatusConflict
			if errors.Is(err, store.ErrConsoleSpecPermission) {
				code = http.StatusForbidden
			}
			http.Error(w, err.Error(), code)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) consoleTaskAgentOnline(agent model.ConsoleAgent) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionClients[agent.KeyID] != nil && h.sessionClients[agent.KeyID][agent.SessionName] != nil
}
