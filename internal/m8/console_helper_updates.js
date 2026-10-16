// Catalog selection and presentation only. Authorization, target ownership,
// offer creation and lifecycle transitions remain server-side.
(()=>{
'use strict';
const API='/api/console/helper-updates';
const stages={offered:'已下发',downloaded:'已取包',installed:'已安装，等待重启确认',restarted:'已重启，等待版本确认',confirmed:'已上线新版本',rolled_back:'已回滚',failed:'失败',expired:'已过期',agent_inactive:'Agent 已停用',owner_inactive:'业主已停用',release_unavailable:'版本不可用'};
const reasons={manual_required:'需要先手动升级 helper',offline:'离线，等待连接',not_selfhost:'仅支持自备 Agent',inactive:'Agent 已停用',unknown_version:'当前版本未知',lock_changed:'依赖锁已变化',sha_mismatch:'升级包校验失败',sha256_mismatch:'升级包校验失败',package_invalid:'升级包与该 Agent 布局不匹配或包校验失败',origin_denied:'升级包来源不允许',offer_expired:'升级请求已过期',health_timeout:'升级后健康检查超时',rolled_back:'升级已回滚',agent_inactive:'Agent 已停用',owner_inactive:'业主已停用',release_unavailable:'版本不可用',job_conflict:'已有升级作业',forbidden:'无权操作该 Agent',unavailable:'暂不可用',unknown:'升级失败，原因未知',failed:'下发失败'};
const reasonText=(code,fallback=reasons.unknown)=>Object.prototype.hasOwnProperty.call(reasons,code)&&typeof reasons[code]==='string'?reasons[code]:fallback;
let dispose=null;
function node(tag,text){const e=document.createElement(tag);if(text!==undefined)e.textContent=text;return e;}
function mount(){
 const anchor=document.getElementById('agentRoster');
 if(!anchor){if(dispose){dispose();dispose=null;}return;}
 if(document.getElementById('helperUpdatePanel'))return;
 if(dispose)dispose();
 const section=node('section');section.id='helperUpdatePanel';section.className='group-block';
 const open=node('button','helper 升级');open.type='button';open.id='helperUpdateOpen';section.append(open);
 const panel=node('div');panel.hidden=true;panel.id='helperUpdateBody';section.append(panel);
 const note=node('p','只可选择运维登记的 Linux helper 包。已下发或已安装不代表升级成功；以重连后的新版本确认结果为准。');panel.append(note);
 const label=node('label','升级到 '),select=node('select');select.id='helperUpdateRelease';label.append(select);panel.append(label);
 const batch=node('button','升级选中 Agent');batch.type='button';batch.id='helperUpdateBatch';panel.append(batch);
 const refresh=node('button','刷新状态');refresh.type='button';refresh.id='helperUpdateRefresh';panel.append(refresh);
 const status=node('div');status.id='helperUpdateStatus';status.setAttribute('role','status');panel.append(status);
 const table=node('table');table.className='group-table';const head=node('thead'),hr=node('tr');['选择','Agent','当前 helper 版本','升级状态','操作'].forEach(t=>hr.append(node('th',t)));head.append(hr);table.append(head);const rows=node('tbody');rows.id='helperUpdateRows';table.append(rows);panel.append(table);
 anchor.after(section);
 let agents=[],selected=new Set(),generation=0,timer=null,controller=null,busy=false,dead=false;
 function controls(){open.disabled=busy;batch.disabled=busy||!select.value||!agents.some(a=>a.eligible&&selected.has(a.agent_id));select.disabled=busy||!select.options.length;refresh.disabled=busy;rows.querySelectorAll('input,button').forEach(b=>{b.disabled=busy||b.dataset.eligible!=='true'||(b.tagName==='BUTTON'&&!select.value);});}
 function render(){
  rows.replaceChildren();const valid=new Set(agents.filter(a=>a.eligible).map(a=>a.agent_id));selected=new Set([...selected].filter(id=>valid.has(id)));
  agents.forEach(a=>{
   const row=node('tr');row.dataset.agentId=a.agent_id;
   const cell=node('td'),check=node('input');check.type='checkbox';check.setAttribute('aria-label','选择 '+a.name);check.dataset.eligible=String(a.eligible===true);check.checked=selected.has(a.agent_id);check.onchange=()=>{if(check.checked)selected.add(a.agent_id);else selected.delete(a.agent_id);controls();};cell.append(check);
   const state=node('td');const job=a.job;state.textContent=job?(stages[job.stage]||'状态未知')+(job.version?'（目标 '+job.version+'）':'')+(job.reason?' · '+reasonText(job.reason):''):(a.eligible?'尚未下发':reasonText(a.reason,'暂不可升级'));
   const op=node('td'),one=node('button','升级');one.type='button';one.dataset.eligible=String(a.eligible===true);one.onclick=()=>issue([a.agent_id]);op.append(one);row.append(cell,node('td',a.name),node('td',a.version||'未知'),state,op);rows.append(row);
  });controls();
 }
 async function request(method,body){
  const c=new AbortController();controller=c;const timeout=setTimeout(()=>c.abort(),10000);
  try{const r=await fetch(API,{method,cache:'no-store',headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined,signal:c.signal});const data=await r.json();if(!r.ok)throw Error(reasonText(data.error,'请求失败（HTTP '+r.status+'）'));return data;}
  finally{clearTimeout(timeout);if(controller===c)controller=null;}
 }
 async function load(){
  if(dead||busy||panel.hidden)return;const g=++generation;if(controller)controller.abort();status.textContent='升级状态加载中…';
  try{const data=await request('GET');if(dead||g!==generation)return;if(!Array.isArray(data.releases)||!Array.isArray(data.agents))throw Error('响应格式无效');
   const previous=select.value;select.replaceChildren();data.releases.forEach(r=>{const opt=node('option',r.version+' · '+r.platform+' · '+r.sha256.slice(0,12));opt.value=r.sha256;select.append(opt);});if([...select.options].some(o=>o.value===previous))select.value=previous;
   agents=data.agents;render();status.textContent=data.releases.length?(agents.length?'':'没有自己的 Agent。'):'暂无运维登记的升级包。';
  }catch(e){if(!dead&&g===generation){status.textContent='升级状态加载失败：'+(e.name==='AbortError'?'请求超时':e.message);agents=[];render();select.replaceChildren();controls();}}
 }
 async function issue(ids){
  if(busy||!select.value||!ids.length)return;
  if(!confirm('确认向 '+ids.length+' 个 Agent 下发所选 helper 升级？'))return;
  busy=true;++generation;if(controller)controller.abort();controls();status.textContent='正在下发…';
  try{const data=await request('POST',{release_sha256:select.value,agent_ids:ids});if(dead)return;if(!Array.isArray(data.results))throw Error('响应格式无效');
   const results=data.results.map(r=>{const a=agents.find(a=>a.agent_id===r.agent_id);return (a?a.name:'Agent')+'：'+(r.ok?'已下发，等待升级确认':reasonText(r.error,'下发失败'));});
   status.textContent=results.join('；');data.results.forEach(r=>{const a=agents.find(a=>a.agent_id===r.agent_id);if(a&&r.job)a.job=r.job;});render();
  }catch(e){if(!dead)status.textContent='下发未确认：'+(e.name==='AbortError'?'请求超时，请刷新状态核对后再试':e.message);}
  finally{busy=false;if(!dead)controls();}
 }
 select.onchange=controls;batch.onclick=()=>issue(agents.filter(a=>a.eligible&&selected.has(a.agent_id)).map(a=>a.agent_id));refresh.onclick=load;
 open.onclick=()=>{panel.hidden=!panel.hidden;open.textContent=panel.hidden?'helper 升级':'收起 helper 升级';if(!panel.hidden){load();if(!timer)timer=setInterval(load,15000);}else{++generation;if(controller)controller.abort();clearInterval(timer);timer=null;}};
 dispose=()=>{dead=true;++generation;if(controller)controller.abort();clearInterval(timer);};controls();
}
const observer=new MutationObserver(mount);observer.observe(document.body,{childList:true,subtree:true});mount();
window.addEventListener('pagehide',()=>{observer.disconnect();if(dispose)dispose();});
})();
