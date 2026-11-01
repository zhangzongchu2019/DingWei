package m8

import (
	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"net/http"
)

func (h *Hub) HandleCoordinateReviewPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.coordinateReviewEntry(r.Context(), platformPrincipal(r)) {
		coordinateError(w, errCoordinatePageUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(coordinateReviewPage))
}

var errCoordinatePageUnavailable = cr.ErrUnavailable

const coordinateReviewPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>坐标派审</title><style>body{font:16px system-ui;max-width:1100px;margin:2rem auto;padding:1rem;background:#f5f7fa;color:#142333}label{display:block;margin:.5rem 0}input{width:90%;padding:.5rem}button{padding:.5rem;margin:.4rem}article{background:white;border:1px solid #ccd4df;border-radius:8px;padding:1rem;margin:1rem 0}pre{white-space:pre-wrap;overflow-wrap:anywhere}.error{color:#ac2626}</style><h1>坐标派审</h1><p>逐人记录仅绑定完整 commit。传输受理、打开与审阅完成分别记录；审阅结果不代表委员会确认、验收或发布。</p><form id="create"><label>GitHub 稳定仓库 ID<input name="repository" required pattern="[1-9][0-9]*"></label><label>变更目录<input name="change_dir" required placeholder="openspec/changes/change-name"></label><label>完整 commit SHA<input name="commit_sha" required pattern="[a-f0-9]{40}"></label><label>Agent ID（每个 Agent 单独派审）<input name="agent_id" required></label><label>跟踪 ref（可选；仅用于比较，不替换受审 SHA）<input name="track_ref"></label><button>派审</button></form><button id="reload">刷新</button><p id="notice" role="status"></p><main id="records"></main><button id="more" hidden>下一页</button><script>
'use strict';const $=s=>document.querySelector(s);let cursor='',lastOperation=null,generation=0,pending=null;
async function api(path,body,signal){const options={signal,cache:'no-store'};if(body!==undefined){options.method='POST';options.headers={'Content-Type':'application/json'};options.body=JSON.stringify(body)}const r=await fetch('/api/console/coordinate-reviews'+path,options);const v=await r.json();if(!r.ok)throw Error(v.error||('HTTP '+r.status));return v}
function node(tag,text,parent){const e=document.createElement(tag);e.textContent=text;parent.append(e);return e}
function date(v){return v&&!v.startsWith('0001-')?v:'未发生'}
function render(q){const e=node('article','',$('#records'));e.dataset.request=q.id;node('h2',q.agent_id,e);node('pre',JSON.stringify(q.coordinate,null,2),e);const last=q.comparisons?.at(-1);let state='待审';if(date(q.completed_at)!=='未发生')state=q.results?.at(-1)?.verdict==='UNABLE'?'未能审阅':'已审';if(date(q.superseded_at)!=='未发生')state='针对旧版本';if(date(q.unable_at)!=='未发生'&&q.results?.at(-1)?.verdict==='UNABLE')state='未能审阅';if(last?.assessment.status==='unknown')state='待核对';if(date(q.cancelled_at)!=='未发生')state='已取消';const status=node('p',state+'；传输：'+q.dispatch+'；打开：'+date(q.opened_at),e);status.className='review-state';if(last)node('pre','当前坐标 '+JSON.stringify(last.current)+'\n'+last.assessment.reason+'\n策略 '+last.assessment.policy_version,e);node('h3','审阅历史',e);for(const x of q.results||[])node('pre',x.reviewer+' / '+x.commit_sha+' / '+x.verdict+' / '+x.received_at+'\n证据：'+x.evidence,e);node('h3','提醒',e);node('p',date(q.opened_at)==='未发生'&&date(q.cancelled_at)==='未发生'&&date(q.completed_at)==='未发生'&&date(q.superseded_at)==='未发生'&&date(q.unable_at)==='未发生'?'Console 待办：尚未打开':'Console 待办已停止',e);for(const n of q.notices||[])node('p',n.slot+' / '+n.attempted_at+' / '+n.state,e);for(const a of q.alerts||[])node('p','重审提示：'+a.current.commit_sha+' / '+a.dispatch,e);const compare=node('button','比较当前版本',e);compare.onclick=async()=>{const sha=prompt('当前完整 commit SHA');if(!sha)return;await run(async()=>{await api('/'+q.id+'/compare',{...q.coordinate,commit_sha:sha});await load(false)})};const cancel=node('button','取消派审',e);cancel.onclick=()=>run(async()=>{await api('/'+q.id+'/cancel',{});await load(false)})}
async function run(fn){try{$('#notice').textContent='';await fn()}catch(e){if(e.name==='AbortError')return;$('#notice').textContent=e.message;$('#notice').className='error'}}
async function load(append){const mine=++generation;if(pending)pending.abort();pending=new AbortController();if(!append){cursor='';$('#records').replaceChildren()}const v=await api(cursor?'?after='+encodeURIComponent(cursor):'',undefined,pending.signal);if(mine!==generation)return;for(const q of v.requests)render(q);cursor=v.next||'';$('#more').hidden=!cursor}
$('#create').onsubmit=e=>{e.preventDefault();run(async()=>{const d=Object.fromEntries(new FormData(e.target));const payload={coordinate:{provider:'github',git_instance:'github.com',repository:d.repository,change_dir:d.change_dir,commit_sha:d.commit_sha},agent_id:d.agent_id,track_ref:d.track_ref};const key=JSON.stringify(payload);if(!lastOperation||lastOperation.key!==key)lastOperation={key,id:crypto.randomUUID()};await api('',{...payload,operation_id:lastOperation.id});lastOperation=null;await load(false)})};$('#reload').onclick=()=>run(()=>load(false));$('#more').onclick=()=>run(()=>load(true));run(()=>load(false));
</script></html>`
