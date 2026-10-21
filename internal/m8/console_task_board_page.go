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
</div><section id="blocker-panel" class="panel" hidden><h2>阻塞卡</h2><p class="muted">1 看卡点　2 答复或转交　3 看重核结果。答复到件不等于允许恢复任务。</p><div id="blocker-status" role="status"></div><div id="blocker-list"></div><button type="button" id="blocker-next" hidden>下一页阻塞卡</button><form id="blocker-create"><h3>卡住了</h3><label>卡在哪 <input id="blocker-where" required maxlength="4096"></label><label>类型 <select id="blocker-kind"><option value="technical">技术受阻</option><option value="requirement_unclear">需求不清</option><option value="change">需求变更</option><option value="resource">缺资源</option></select></label><label>等谁 <select id="blocker-waiter"></select></label><label>等什么 <input id="blocker-expected" required maxlength="4096"></label><label><input type="checkbox" id="blocker-risk">涉及真实生产或安全（自动用完整档）</label><button type="submit">登记阻塞</button></form><button type="button" id="blocker-close">收起</button></section><section class="panel active-panel"><h2>进行中任务</h2><table><thead><tr><th>任务</th><th>状态</th><th>开始时间</th><th>执行人</th><th>验收人</th><th>计划完成</th><th>堵塞点</th><th>资源协调</th></tr></thead><tbody id="active-task-list"></tbody></table></section><script>
function addClarificationSummary(root,specID,taskID,revision){const box=document.createElement('details'),summary=document.createElement('summary'),content=document.createElement('div');summary.textContent='Spec 解释与补充（只读数据）';box.append(summary,content);let loading=false;box.ontoggle=async()=>{if(!box.open||loading)return;loading=true;content.textContent='正在核对当前权限…';try{const query=taskID?'task_id='+encodeURIComponent(taskID):'spec_id='+encodeURIComponent(specID);let r=await fetch('/api/console/questions?'+query+'&limit=20',{credentials:'same-origin',cache:'no-store'});if(!r.ok)throw Error('当前问题权限不可用，不显示私有旧答复。');const list=await r.json();content.replaceChildren();for(const q of list.questions||[]){r=await fetch('/api/console/questions/'+encodeURIComponent(q.id)+'/clarifications?limit=3',{credentials:'same-origin',cache:'no-store'});if(!r.ok)continue;const b=await r.json();if(revision&&b.context.revision!==revision)continue;const row=document.createElement('article'),link=document.createElement('a');link.href='/console/spec-clarifications?question_id='+encodeURIComponent(q.id);link.textContent=q.title+' · 修订 '+b.context.revision+' · 查看完整楼层';row.append(link);for(const p of b.posts||[]){const text=document.createElement('p');text.style.whiteSpace='pre-wrap';text.style.overflowWrap='anywhere';text.textContent='第'+p.floor+'条 · '+(p.role==='pm'?'PM':p.role==='deputy'?'代答（待PM追认）':'技术说明，非权威')+' · '+p.kind+'：'+p.body.slice(0,500)+(p.body.length>500?'…':'');row.append(text)}content.append(row)}if(!content.childNodes.length)content.textContent='本次权限及修订下无可见楼层。'}catch(e){content.textContent=e.message}finally{loading=false}};root.append(box)}

var DW_THEME_KEY='dw-theme',dwTaskThemeChannel=null;
function dwTaskApplyTheme(v){if(v!=='dark'&&v!=='light')return;document.documentElement.dataset.theme=v;if(document.body)document.body.dataset.theme=v;}
function dwTaskInitTheme(){var initial='light';try{var stored=localStorage.getItem(DW_THEME_KEY);if(stored==='dark'||stored==='light')initial=stored;}catch(_){}dwTaskApplyTheme(initial);
 try{if('BroadcastChannel' in window){dwTaskThemeChannel=new BroadcastChannel(DW_THEME_KEY);dwTaskThemeChannel.onmessage=function(e){dwTaskApplyTheme(e&&e.data&&e.data.theme);};}}catch(_){dwTaskThemeChannel=null;}
 try{window.addEventListener('storage',function(e){if(e&&e.key===DW_THEME_KEY)dwTaskApplyTheme(e.newValue);});}catch(_){} }
dwTaskInitTheme();
let specPage=1,taskPage=1,selectedSpec=new URLSearchParams(location.search).get('spec_id')||'',editing=false,onlineAgents=[],currentTasks=[],taskGroups=[];
function acceptanceDisplay(raw){try{const value=JSON.parse(raw||'[]'),list=Array.isArray(value)?value:(Array.isArray(value.criteria)?value.criteria:[]),ids=[],texts=[];list.forEach(function(v){const id=String(v.id||'').trim(),text=String(v.text||'').trim();if(id)ids.push(id);if(text)texts.push((id?id+' ':'')+text)});return {label:ids.join(', '),title:texts.join('\n')}}catch(_){return {label:'',title:''}}}
function setPager(prefix,page,pages){document.getElementById(prefix+'-page').textContent=page+' / '+pages;document.getElementById(prefix+'-prev').disabled=page<=1;document.getElementById(prefix+'-next').disabled=page>=pages}
function taskCell(value,title){const td=document.createElement('td');td.textContent=value||'';if(title)td.title=title;return td}
function assigneeCell(task,role){const td=document.createElement('td'),idKey=role+'_agent_id',nameKey=role+'_agent',onlineKey=role+'_online';if(!editing||!String(task.review_id||'').startsWith('manual:')){td.textContent=task[nameKey]||'';if(task[idKey]&&!task[onlineKey]){const dot=document.createElement('span');dot.className='offline-dot';dot.title='当前离线';dot.textContent='●';td.append(dot)}return td}const select=document.createElement('select');select.className='assignee-select '+role+'-agent';const blank=document.createElement('option');blank.value='';blank.textContent='';select.append(blank);if(task[idKey]&&!task[onlineKey]&&!onlineAgents.some(function(a){return a.id===task[idKey]&&(!task.group_code||!a.group_codes||a.group_codes.includes(task.group_code))})){const assigned=document.createElement('option');assigned.value=task[idKey];assigned.textContent=task[nameKey]||task[idKey];assigned.disabled=true;assigned.selected=true;assigned.dataset.assignedOffline='true';select.append(assigned)}onlineAgents.filter(function(agent){return !task.group_code||!agent.group_codes||agent.group_codes.includes(task.group_code)}).forEach(function(agent){const option=document.createElement('option');option.value=agent.id;option.textContent=agent.session_name+(agent.online===false?"（离线）":"");option.selected=agent.id===task[idKey];select.append(option)});select.onchange=function(){task[idKey]=select.value;task.assignment_dirty=true};td.append(select);return td}
async function saveAssignment(task){if(boardStopped)return false;if(task.status==='done'&&!confirm('重新指派会重新计划此任务并清除原用例确认，是否继续？'))return false;const status=document.getElementById('task-status'),r=await fetch('/api/console/task-board/tasks/'+encodeURIComponent(task.proposal_id),{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.assign({developer_agent_id:task.developer_agent_id||'',reviewer_agent_id:task.reviewer_agent_id||''},task.version?{expected_version:task.version,group_code:task.group_code||'',reopen:task.status==='done'}:{}))});if(r.status===401||r.status===403){boardStop('登录或访问权限已失效，请重新打开任务管理');return false}if(boardStopped)return false;if(!r.ok){status.textContent='任务保存失败，编辑内容已保留';return false}task.assignment_dirty=false;return true}
async function saveAssignments(){if(!await loadBoard('verify')||boardStopped)return false;for(const task of currentTasks){if(task.assignment_dirty&&!await saveAssignment(task))return false}document.getElementById('task-status').textContent='已保存';return true}
function taskProgress(task){if(task.version&&!task.group_code)return {text:'请选择编组以启用 Agent 任务通道',problem:true};if(task.needs_reassignment)return {text:'执行人权限已失效，请改派',problem:true};if(task.developer_agent_id&&!task.reviewer_agent_id)return {text:'缺少验收 Agent，任务无法完成',problem:true};return {text:(task.status?({'planned':'已计划'}[task.status]||task.status)+' · ':'')+String(task.passed_cases||0)+'/'+String(task.total_cases||1)+(task.assignment_notification_pending?' · 通知排队中（离线或需升级helper）':''),problem:false}}
function flowGroupCell(task){const td=document.createElement("div");if(!task.version)return td;const select=document.createElement("select");const blank=document.createElement("option");blank.value="";blank.textContent="请选择编组";select.append(blank);taskGroups.forEach(function(g){const o=document.createElement("option");o.value=g.code;o.textContent=g.name;o.selected=g.code===task.group_code;select.append(o)});select.disabled=!editing;select.onchange=function(){task.group_code=select.value;task.assignment_dirty=true;renderTasks(currentTasks)};td.append(select);return td}
let questionsAvailable=false;
function renderTasks(tasks){currentTasks=tasks||[];const body=document.getElementById('task-list');body.replaceChildren();currentTasks.forEach(function(task){const tr=document.createElement('tr'),acceptance=acceptanceDisplay(task.acceptance_json),progress=taskProgress(task),progressCell=taskCell(progress.text);tr.dataset.taskId=task.proposal_id;if(progress.problem)progressCell.className='error assignment-problem';const contentCell=taskCell(task.content||task.title);if(editing)contentCell.append(flowGroupCell(task));if(String(task.review_id||'').startsWith('manual:')){const card=document.createElement('button');card.type='button';card.textContent='阻塞卡';card.onclick=function(){openBlockers(task)};contentCell.append(card)}if(questionsAvailable){const link=document.createElement('a');link.textContent='提研发问题';link.href='/console/questions?spec_id='+encodeURIComponent(task.spec_id)+'&task_id='+encodeURIComponent(task.proposal_id);link.target='_top';contentCell.append(link);addClarificationSummary(contentCell,task.spec_id,task.proposal_id,task.revision)}tr.append(contentCell,assigneeCell(task,'developer'),assigneeCell(task,'reviewer'),progressCell,taskCell(acceptance.label,acceptance.title));body.append(tr)})}
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
function boardSchedule(){boardClearTimer();if(!boardStopped&&!editing&&boardIsActive())boardTimer=setTimeout(function(){boardTimer=null;loadBoard()},5000)}
function boardStop(text){if(typeof blockerTask!=='undefined')blockerTask=null;if(typeof blockerPending!=='undefined')blockerPending.clear();['blocker-where','blocker-expected'].forEach(function(id){const n=document.getElementById(id);if(n)n.value=''});const waiter=document.getElementById('blocker-waiter');if(waiter)waiter.replaceChildren();const cardList=document.getElementById('blocker-list');if(cardList)cardList.replaceChildren();const cardPanel=document.getElementById('blocker-panel');if(cardPanel)cardPanel.hidden=true;boardStopped=true;boardGeneration++;boardQueued=false;boardClearTimer();if(boardFlight)boardFlight.controller.abort();boardViewer='';boardLastGoodAt='';editing=false;onlineAgents=[];renderSpecs([]);renderTasks([]);document.getElementById('task-updated').textContent='';document.getElementById('task-status').textContent=text;document.getElementById('edit-tasks').disabled=true;document.getElementById('finish-tasks').hidden=true;document.getElementById('refresh-tasks').disabled=true}
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
  specPage=data.spec_page;taskPage=data.task_page;onlineAgents=data.task_agents&&data.task_agents.length?data.task_agents:data.agents;taskGroups=data.task_groups||[];questionsAvailable=data.questions_available===true;blockerViewer=data.blocker_current_user||'';
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
// One card ledger; UI actions never start, resume or complete a task.
let blockerTask=null,blockerViewer='',blockerTaskVersion=0,blockerNextCursor='',blockerFlight=false,blockerPending=new Map();
function blockerNode(tag,text){const n=document.createElement(tag);n.textContent=text||'';return n}
function blockerWaitOptions(){const select=document.getElementById('blocker-waiter');select.replaceChildren();let option=blockerNode('option','待分派');option.value='';select.append(option);if(blockerViewer){option=blockerNode('option','我来答复');option.value='user:'+blockerViewer;select.append(option)}(taskGroups.length?onlineAgents:[]).forEach(function(a){option=blockerNode('option',a.session_name||a.id);option.value='agent:'+a.id;select.append(option)})}
async function openBlockers(task){if(boardStopped)return;blockerTask=task;document.getElementById('blocker-panel').hidden=false;blockerWaitOptions();await refreshBlockers()}
async function refreshBlockers(cursor){cursor=cursor||'';if(!blockerTask||boardStopped)return;const id=blockerTask.proposal_id;try{const r=await fetch('/api/console/tasks/'+encodeURIComponent(id)+'/blockers?limit=100'+(cursor?'&cursor='+encodeURIComponent(cursor):''),{cache:'no-store'});if(r.status===401||r.status===403){boardStop('登录或任务权限已失效');return}if(!r.ok)throw new Error();const data=await r.json();if(!blockerTask||blockerTask.proposal_id!==id||boardStopped)return;blockerTaskVersion=data.task_version;blockerNextCursor=data.next_cursor||'';document.getElementById('blocker-next').hidden=!blockerNextCursor;renderBlockers(data.blockers||[]);document.getElementById('blocker-status').textContent=data.next_cursor?'还有后续阻塞卡，可点下一页继续查看':'只做恢复前重核，当前没有新增执行授权'}catch(_){document.getElementById('blocker-status').textContent='阻塞卡读取失败，保留原输入；请刷新后重核'}}
async function blockerMutation(bid,body){if(blockerFlight||!blockerTask||boardStopped)return false;blockerFlight=true;const target=blockerTask.proposal_id,cache=target+'|'+bid+'|'+JSON.stringify(Object.fromEntries(Object.entries(body).filter(function(v){return v[0]!=='expected_version'})));if(!blockerPending.has(cache))blockerPending.set(cache,Object.assign({request_id:crypto.randomUUID()},body));const request=blockerPending.get(cache);try{const r=await fetch('/api/console/tasks/'+encodeURIComponent(target)+'/blockers'+(bid?'/'+encodeURIComponent(bid):''),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(r.status===401||r.status===403){boardStop('登录或任务权限已失效');return false}if(!r.ok){document.getElementById('blocker-status').textContent=r.status>=500?'结果待确认：先刷新查看，不要重复发起':'版本或权限已变化，刷新后重核；输入保留';if(r.status<500)blockerPending.delete(cache);return false}await refreshBlockers();return true}catch(_){document.getElementById('blocker-status').textContent='结果待确认：先刷新查看；原输入已保留';return false}finally{blockerFlight=false}}
async function blockerEventID(b,body,evidence){const raw=new TextEncoder().encode(b.id+'|'+b.generation+'|'+body+'|'+evidence),sum=await crypto.subtle.digest('SHA-256',raw);return 'web:'+Array.from(new Uint8Array(sum),function(v){return v.toString(16).padStart(2,'0')}).join('')}
function renderBlockers(cards){const list=document.getElementById('blocker-list');list.replaceChildren();cards.forEach(function(b){const card=blockerNode('section'),state=({waiting:'等新输入',needs_owner:'待分派',ready_to_recheck:'到件待重核',rechecking:'重核中',resolved:'重核通过（未自动恢复）',cancelled:'所有者已取消',needs_reassignment:'指派已变，待转交'})[b.state]||b.state;card.append(blockerNode('h3',b.where),blockerNode('p','状态：'+state+'；处理人：'+blockerWaitingName(b.waiting_principal)+'；'+(b.mode==='full'?'完整档':'轻量档')),blockerNode('p','等什么：'+b.expected_input));const conditions=blockerNode('ul');(b.resume_recheck||[]).forEach(function(c){conditions.append(blockerNode('li',c.text))});card.append(conditions);if(b.wake_state)card.append(blockerNode('p',({'queued_offline_or_upgrade':'通知排队中（对方离线或需升级helper）','durable_delivery_queued':'已交可靠投递，等待执行人重核','pending_reconciliation':'通知待对账恢复'})[b.wake_state]||'通知已停止，须核对当前指派'));const history=blockerNode('div'),historyButton=blockerNode('button','查看答复与材料');historyButton.type='button';historyButton.onclick=function(){blockerHistory(b,history,0)};card.append(historyButton,history);if(!['resolved','cancelled'].includes(b.state)){const body=blockerNode('textarea');body.placeholder='答复或材料说明';body.maxLength=8192;const evidence=blockerNode('input');evidence.placeholder='真实证据引用（Git坐标、材料或授权记录；不填凭据）';evidence.maxLength=2048;const reply=blockerNode('button','答复到件');reply.type='button';reply.onclick=async function(){if(!body.value.trim()||!evidence.value.trim()){document.getElementById('blocker-status').textContent='请填答复与真实材料引用；未知不能当解除';return}const event=await blockerEventID(b,body.value,evidence.value);if(await blockerMutation(b.id,{operation:'input',expected_version:b.version,event_id:event,body:body.value,evidence_ref:evidence.value})){body.value='';evidence.value=''}};card.append(body,evidence,reply);const waiter=document.getElementById('blocker-waiter').cloneNode(true);waiter.removeAttribute('id');const assign=blockerNode('button','转给此人答复');assign.type='button';assign.onclick=function(){if(waiter.value)blockerMutation(b.id,{operation:'assign_waiter',expected_version:b.version,waiting_principal:waiter.value})};card.append(waiter,assign);if(b.mode==='full'&&!b.full_approval_ref){const approve=blockerNode('button','完整档已核，补入记录');approve.type='button';approve.onclick=function(){if(evidence.value.trim())blockerMutation(b.id,{operation:'approve_full',expected_version:b.version,full_approval_ref:evidence.value})};card.append(approve)}if(b.state==='needs_reassignment'){const adopt=blockerNode('button','转交当前执行人重新核对');adopt.type='button';adopt.onclick=function(){blockerMutation(b.id,{operation:'adopt',expected_version:b.version})};card.append(adopt)}const cancel=blockerNode('button','按记录取消此阻塞');cancel.type='button';cancel.onclick=function(){if(evidence.value.trim())blockerMutation(b.id,{operation:'cancel',expected_version:b.version,evidence_ref:evidence.value})};card.append(cancel)}list.append(card)})}
function blockerWaitingName(principal){if(!principal)return '待分派';if(principal==='user:'+blockerViewer)return '本人';const agent=onlineAgents.find(function(a){return 'agent:'+a.id===principal});return agent?(agent.session_name||agent.id):'已指定平台人员'}
async function blockerHistory(b,container,before){if(!blockerTask||boardStopped)return;const target=blockerTask.proposal_id;try{const r=await fetch('/api/console/tasks/'+encodeURIComponent(target)+'/blockers/'+encodeURIComponent(b.id)+(before?'?input_before='+before:''),{cache:'no-store'});if(r.status===401||r.status===403){boardStop('登录或任务权限已失效');return}if(!r.ok)throw new Error();const data=await r.json();if(boardStopped||!blockerTask||target!==blockerTask.proposal_id)return;if(!before)container.replaceChildren();(data.inputs||[]).forEach(function(v){container.append(blockerNode('p',v.created_at+' · '+v.body),blockerNode('p','材料：'+v.evidence_ref))});if(data.inputs_next_before){const earlier=blockerNode('button','更早答复');earlier.type='button';earlier.onclick=function(){earlier.remove();blockerHistory(b,container,data.inputs_next_before)};container.append(earlier)}}catch(_){container.append(blockerNode('p','答复暂不可用，原记录未改动'))}}
document.getElementById('blocker-next').onclick=function(){refreshBlockers(blockerNextCursor)};
document.getElementById('blocker-close').onclick=function(){document.getElementById('blocker-panel').hidden=true;blockerTask=null};
document.getElementById('blocker-create').onsubmit=async function(event){event.preventDefault();if(!blockerTask)return;await blockerMutation('',{operation:'create',expected_version:blockerTaskVersion,kind:document.getElementById('blocker-kind').value,where:document.getElementById('blocker-where').value,waiting_principal:document.getElementById('blocker-waiter').value,expected_input:document.getElementById('blocker-expected').value,production_or_security:document.getElementById('blocker-risk').checked})};

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
		if spec.Archived {
			continue
		}
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
	notificationPending := map[string]bool{}
	if flow, ok := h.taskFlowRepo(); ok {
		notifications, e := flow.PendingConsoleTaskNotifications(r.Context())
		if e != nil {
			taskError(w, e)
			return
		}
		for _, n := range notifications {
			notificationPending[n.TaskID] = true
		}
	}
	groups, taskAgents, optionErr := h.taskFlowOptions(r.Context(), principal)
	if optionErr != nil {
		taskError(w, optionErr)
		return
	}
	if h.Identity != nil {
		for i := range tasks {
			tasks[i].AssignmentNotificationPending = notificationPending[tasks[i].ID]
			if flow, ok := h.taskFlowRepo(); ok {
				if st, e := flow.ConsoleTaskFlowState(r.Context(), tasks[i].ID); e == nil {
					tasks[i].Version = st.Version
					tasks[i].GroupCode = st.GroupCode
					tasks[i].NeedsReassignment = st.NeedsReassignment
				}
			}
			for _, a := range taskAgents {
				onlineIDs[a.ID] = a.Online
				if tasks[i].DeveloperAgentID == a.ID {
					tasks[i].DeveloperAgent = a.SessionName
				}
				if tasks[i].ReviewerAgentID == a.ID {
					tasks[i].ReviewerAgent = a.SessionName
				}
			}
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
		"questions_available": h.Identity != nil, "blocker_current_user": principal, "viewer_key": hashConsoleToken("task-board:" + principal), "updated_at": h.now().UTC(), "task_total": len(tasks),
		"specs": visibleSpecs, "spec_page": specPage, "spec_page_size": consoleTaskBoardSpecPageSize, "spec_total_pages": specPages,
		"tasks": visibleTasks, "active_tasks": activeConsoleTasks(tasks), "agents": online, "task_agents": taskAgents, "task_groups": groups, "task_page": taskPage, "task_page_size": consoleTaskBoardTaskPageSize, "task_total_pages": taskPages,
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
		GroupCode            string  `json:"group_code"`
		ExpectedVersion      int     `json:"expected_version"`
		Reopen               bool    `json:"reopen"`
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
		if !spec.Archived {
			ownedSpecs[spec.ID] = true
		}
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
	if h.Identity != nil && (req.GroupCode != "" || req.ExpectedVersion > 0) {
		if req.StartedAt != nil || req.PlannedFinishAt != nil || req.Blocker != nil || req.ResourceCoordination != nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		h.assignTaskFlow(w, r, current.ID, principal, req.GroupCode, req.DeveloperAgentID, req.ReviewerAgentID, req.ExpectedVersion, req.Reopen)
		return
	}
	if flow, ok := h.taskFlowRepo(); ok {
		if t, e := flow.ConsoleTaskFlowState(r.Context(), current.ID); e == nil && t.GroupCode != "" {
			taskError(w, store.ErrTaskFlowConflict)
			return
		}
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
