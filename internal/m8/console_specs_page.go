package m8

import (
	"fmt"
	"net/http"
	"strings"

	viewservice "github.com/zhangzongchu2019/dingwei/view-service"
)

const consoleSpecsPageHTML = `<!doctype html><meta charset=utf-8><title>DingWei Specs</title>
<link rel=stylesheet href="/console/spec-assets/easymde.min.css">
<style>
body{font:14px sans-serif;margin:20px;max-width:1100px}h1,h2{margin:8px 0}.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
section{border:1px solid #ccd4df;border-radius:8px;padding:14px}label{display:block;margin:8px 0 3px}input,textarea{box-sizing:border-box;width:100%;padding:7px}
textarea{min-height:110px}button{margin:8px 6px 0 0;padding:6px 10px}.spec-row{border:1px solid #dde3ea;border-radius:6px;padding:9px;margin:7px 0}
.meta{color:#667085;font-size:12px}.error{color:#b42318}.ok{color:#067647}.required{color:#b42318;font-weight:700}@media(max-width:760px){.grid{grid-template-columns:1fr}}
.spec-rendered{margin:10px 0 18px;padding:12px;border:1px solid #dde3ea;border-radius:6px;--accent:#175cd3;--panel:#fff;--panel-2:#f2f4f7;--line:#d0d5dd;--muted:#667085;--code-bg:#101828;--code-text:#f2f4f7}
.md{min-width:0;max-width:100%;overflow-wrap:anywhere;word-break:break-word}.md>*:first-child{margin-top:0}.md>*:last-child{margin-bottom:0}.md h1,.md h2,.md h3{margin:18px 0 8px;line-height:1.25}.md h1{font-size:22px}.md h2{font-size:18px}.md h3{font-size:16px}.md p{margin:0 0 11px}.md a{color:var(--accent);text-decoration-thickness:1px}.md img.md-img{max-width:100%;max-height:420px;border-radius:6px;display:block;margin:6px 0}.md blockquote{margin:10px 0;padding:8px 12px;border-left:3px solid var(--accent);background:var(--panel-2);color:var(--muted)}.md hr{border:0;border-top:1px solid var(--line);margin:16px 0}.md ul,.md ol{padding-left:24px;margin:8px 0 12px}.md li{margin:4px 0}.md code:not(pre code){font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--panel-2);border:1px solid var(--line);border-radius:4px;padding:1px 4px;white-space:normal;overflow-wrap:anywhere;word-break:break-word}
.table-wrap,.code-wrap{width:100%;max-width:100%;min-width:0;overflow-x:auto;margin:12px 0;border-radius:8px}.table-wrap table{width:max-content;min-width:100%;border-collapse:collapse;background:var(--panel);border:1px solid var(--line)}.table-wrap th,.table-wrap td{padding:8px 10px;border:1px solid var(--line);text-align:left;vertical-align:top;white-space:nowrap}.table-wrap th{background:var(--panel-2);font-weight:700}.code-wrap{position:relative;background:var(--code-bg);border:1px solid var(--line)}.code-toolbar{display:flex;justify-content:space-between;align-items:center;gap:8px;padding:7px 8px;color:#aab4c0;border-bottom:1px solid rgba(255,255,255,.12);font-size:12px}.copy-code{height:26px;padding:0 8px;border-radius:5px;border:1px solid rgba(255,255,255,.18);background:rgba(255,255,255,.06);color:#e6edf3;cursor:pointer}.code-wrap pre{margin:0;padding:12px;overflow-x:auto;max-width:100%;color:var(--code-text);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;line-height:1.55;white-space:pre}.tok-kw{color:#ff7b72}.tok-str{color:#a5d6ff}.tok-num{color:#79c0ff}.tok-com{color:#8b949e}.tok-fn{color:#d2a8ff}.md-image-fallback{display:inline-block;padding:7px 9px;border:1px solid var(--line);border-radius:6px;background:var(--panel-2)}
.editor-toolbar button.dw-icon{color:#344054;background:#fff}.editor-toolbar button.dw-icon:hover,.editor-toolbar button.dw-icon.active{color:#101828;background:#d0d5dd}
.dw-icon:before{font:700 13px/1 sans-serif}.dw-bold:before{content:'B'}.dw-italic:before{content:'I';font-style:italic}.dw-heading:before{content:'H'}.dw-link:before{content:'↗'}.dw-image:before{content:'▧'}.dw-list:before{content:'•'}
</style>
<h1>Spec management</h1><p>一个账号可同时管理多份 spec。认领后的版本不可原地覆盖。</p>
<nav><button id=show-spec-list type=button>我的 spec</button><button id=show-new-spec type=button>新建 spec</button></nav>
<section id=new-screen hidden><h2>New spec</h2>
<label>Title <span class=required>*</span></label><input id=new-title required>
<label>Markdown <span class=required>*</span></label><textarea id=new-content required></textarea>
<label>Import Markdown</label><input id=new-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance <span class=required>*</span></label><input id=new-acceptance required>
<label>Owner message anchor <span class=required>*</span></label><input id=new-anchor required>
<button id=create-spec type=button>Create</button><span id=create-status></span>
</section>
<section id=list-screen><h2>我的 spec</h2><div id=spec-list></div><button id=prev-page type=button>上一页</button><span id=page-status></span><button id=next-page type=button>下一页</button></section>
<section id=detail-screen hidden><h2>Spec 详情</h2><div id=detail-meta class=meta><span id=spec-state></span> · 所属人 <span id=spec-owner-short></span> · 当前修改人 <span id=spec-current-editor-short></span></div><div id=spec-rendered class=spec-rendered></div>
<div id=spec-workflow hidden><label>转给 <select id=spec-next-editor></select></label><button id=transfer-editing type=button>转修改</button><button id=transfer-review type=button>转审核</button><button id=finalize-spec type=button>审结</button><span id=workflow-status></span></div>
<h3>任务拆解</h3><div id=task-split hidden><label>任务名 <input id=task-title></label><label>内容 <textarea id=task-content></textarea></label><label>完成标准 (DoD) <textarea id=task-dod></textarea></label><label>预期工时 <input id=task-hours type=number min=0 step=.5 value=0></label><span id=task-hours-hint></span><label>依赖项 <select id=task-deps multiple></select></label><button id=create-task type=button>拆解任务</button><button id=close-task-split type=button>任务结案</button><span id=task-create-status></span></div>
<h3>任务列表</h3><div id=task-list></div></section>
<section id=editor hidden><h2>修改 spec</h2>
<h3>Rendered spec</h3><div id=edit-rendered class=spec-rendered></div>
<label>Title <span class=required>*</span></label><input id=edit-title readonly required>
<label>Markdown <span class=required>*</span></label><textarea id=edit-content required></textarea>
<label>Import Markdown</label><input id=edit-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance <span class=required>*</span></label><input id=edit-acceptance required>
<label>Owner message anchor <span class=required>*</span></label><input id=edit-anchor required>
<button id=save-spec type=button>Save new revision</button><span id=edit-status></span>
</section>
<script src="/console/spec-assets/easymde.min.js"></script><script id=dw-md-core></script><script>
var currentSpec=null,platformRole='',currentPrincipal='',currentPage=1,totalPages=1;
var sharedMatch=location.pathname.match(/^\/console\/specs\/([^/]+)$/);
var sharedSpecID=sharedMatch?decodeURIComponent(sharedMatch[1]):'';
var sharedEditToken=new URLSearchParams(location.hash.slice(1)).get('edit')||'';
function editorOptions(element){return {element:element,lineWrapping:true,spellChecker:false,autoDownloadFontAwesome:false,autosave:{enabled:false,uniqueId:'disabled-'+element.id},status:false,forceSync:true,toolbar:[
 {name:'bold',action:EasyMDE.toggleBold,className:'dw-icon dw-bold',title:'Bold'},
 {name:'italic',action:EasyMDE.toggleItalic,className:'dw-icon dw-italic',title:'Italic'},
 {name:'heading',action:EasyMDE.toggleHeadingSmaller,className:'dw-icon dw-heading',title:'Heading'},
 {name:'unordered-list',action:EasyMDE.toggleUnorderedList,className:'dw-icon dw-list',title:'List'},
 {name:'link',action:EasyMDE.drawLink,className:'dw-icon dw-link',title:'Link'},
 {name:'image',action:EasyMDE.drawImage,className:'dw-icon dw-image',title:'Image'}]};}
var specEditors={new:new EasyMDE(editorOptions(document.getElementById('new-content'))),edit:new EasyMDE(editorOptions(document.getElementById('edit-content')))};
function bindImport(prefix){document.getElementById(prefix+'-import').onchange=async function(){var f=this.files&&this.files[0];if(f)specEditors[prefix].value(await f.text());};}bindImport('new');bindImport('edit');
function payload(prefix){return {title:document.getElementById(prefix+'-title').value,content_markdown:specEditors[prefix].value(),
 acceptance:[{id:'A1',text:document.getElementById(prefix+'-acceptance').value}],input_anchors:[{kind:'owner_message',ref:document.getElementById(prefix+'-anchor').value}]};}
function missingRequired(body){for(const item of [['Title',body.title],['Markdown',body.content_markdown],['Acceptance',body.acceptance[0].text],['Owner message anchor',body.input_anchors[0].ref]])if(!String(item[1]||'').trim())return item[0];return '';}
async function responseFailure(prefix,r){var detail=(await r.text()).trim();return prefix+' '+r.status+(detail?':'+detail:'');}
function wireSpecRender(root){root.querySelectorAll('.copy-code').forEach(function(btn){btn.onclick=async function(){try{await navigator.clipboard.writeText(decodeURIComponent(btn.dataset.code||''));btn.textContent='Copied';}catch(_){btn.textContent='Copy failed';}};});root.querySelectorAll('img[data-md-img]').forEach(function(img){img.onerror=function(){var link=document.createElement('a');link.className='md-image-fallback';link.href=img.currentSrc||img.src;link.target='_blank';link.rel='noreferrer';link.download='';link.textContent='⬇ 下载图片'+(img.alt?'：'+img.alt:'');img.replaceWith(link);};});}
function renderSpec(markdown,rootID){var root=document.getElementById(rootID||'spec-rendered');root.innerHTML=DwMdCore.renderMarkdown(markdown||'');wireSpecRender(root);}
function showScreen(id){var detail=id==='detail-screen';document.getElementById('list-screen').hidden=!(id==='list-screen'||detail);document.getElementById('detail-screen').hidden=!detail;document.getElementById('new-screen').hidden=id!=='new-screen';document.getElementById('editor').hidden=id!=='editor';}
async function specAction(id,path,body,status){status.textContent='';var opts={method:'POST'};if(body){opts.headers={'Content-Type':'application/json'};opts.body=JSON.stringify(body);}var r=await fetch('/api/console/specs/'+encodeURIComponent(id)+'/'+path,opts);if(!r.ok){status.className='spec-action-status error';status.textContent=await responseFailure('操作失败',r);return false;}await refreshSpecs();return true;}
async function refreshSpecs(){var endpoint=sharedSpecID?'/api/specs/shared/'+encodeURIComponent(sharedSpecID):'/api/console/specs?page='+currentPage;var r=await fetch(endpoint,{cache:'no-store'});if(!r.ok)throw new Error('list failed');var raw=await r.json();var data=sharedSpecID?{role:'shared',principal:'',specs:[raw.spec],page:1,total_pages:1}:raw;platformRole=data.role||'';currentPrincipal=data.principal||'';currentPage=data.page||1;totalPages=data.total_pages||1;document.getElementById('page-status').textContent=currentPage+' / '+totalPages;document.getElementById('prev-page').disabled=currentPage<=1;document.getElementById('next-page').disabled=currentPage>=totalPages;var list=document.getElementById('spec-list');list.replaceChildren();
 (Array.isArray(data.specs)?data.specs:[]).forEach(function(spec){var row=document.createElement('div');row.className='spec-row';row.dataset.specId=spec.id;row.dataset.title=spec.title;
  var title=document.createElement('strong');title.textContent=spec.title;var meta=document.createElement('div');meta.className='meta';meta.textContent=spec.state+' · revision '+spec.current_revision;
  var status=document.createElement('span');status.className='spec-action-status';var canEdit=!!spec.can_edit;
  meta.textContent=spec.state+' · revision '+spec.current_revision+' · 当前 '+(spec.current_editor_short_name||'无');row.append(title,meta);var view=document.createElement('button');view.type='button';view.className='view-spec';view.textContent='View';view.onclick=function(){openSpec(spec.id,true)};row.append(view);if(canEdit){var edit=document.createElement('button');edit.type='button';edit.className='edit-spec';edit.textContent='Edit';edit.onclick=function(){openSpec(spec.id,false)};row.append(edit);}
  row.append(status);
  list.append(row);});}
function taskDoD(task){try{var items=JSON.parse(task.acceptance_json||'[]');return (items||[]).map(function(x){return x.text||''}).filter(Boolean).join('\n');}catch(_){return '';}}
function hoursHint(input,node){node.textContent=Number(input.value)>8?'建议再拆':'';}
function renderTasks(tasks,candidates){var root=document.getElementById('task-list');root.replaceChildren();var createDeps=document.getElementById('task-deps');createDeps.replaceChildren();(candidates||[]).forEach(function(c){var o=document.createElement('option');o.value=c.proposal_id;o.textContent=c.title+' ('+c.spec_id+' r'+c.revision+')';createDeps.append(o)});(tasks||[]).forEach(function(task){var row=document.createElement('article');row.className='spec-row task-row';row.dataset.taskId=task.proposal_id;row.dataset.level=String(task.dependency_level||0);row.id='task-row-'+String(task.proposal_id).replace(/[^a-zA-Z0-9_-]/g,'-');var deps=JSON.parse(task.depends_on_json||'[]');row.innerHTML='<strong></strong><div class=task-content></div><div class=meta></div><label>完成标准 (DoD) <textarea class=task-dod></textarea></label><label>预期工时 <input class=task-hours type=number min=0 step=.5></label><span class=task-hours-hint></span><label>依赖项 <select class=task-deps multiple></select></label><button class=save-task-plan type=button>保存拆解</button><span class=task-status></span>';row.querySelector('.task-dod').id=row.id+'-dod';row.querySelector('strong').textContent=task.title;row.querySelector('.task-content').textContent=task.content||'';row.querySelector('.meta').textContent='层级 '+(task.dependency_level||0)+' · '+(task.parallel?'可并行':'顺序执行')+' · 状态 '+task.status+' · 被依赖 '+((task.depended_by||[]).join(', ')||'无');row.querySelector('.task-dod').value=taskDoD(task);var hours=row.querySelector('.task-hours'),hint=row.querySelector('.task-hours-hint');hours.value=task.estimated_hours||0;hours.oninput=function(){hoursHint(hours,hint)};hoursHint(hours,hint);var select=row.querySelector('.task-deps');(candidates||[]).forEach(function(c){var o=document.createElement('option');o.value=c.proposal_id;o.textContent=c.title+' ('+c.spec_id+' r'+c.revision+')';o.selected=deps.includes(c.proposal_id);select.append(o)});row.querySelector('.save-task-plan').onclick=async function(){var body={content:row.querySelector('.task-content').textContent,status:task.status||'proposed',estimated_hours:Number(hours.value),depends_on:Array.from(select.selectedOptions).map(function(o){return o.value}),definition_of_done:row.querySelector('.task-dod').value};var resp=await fetch('/api/console/task-proposals/'+encodeURIComponent(task.proposal_id)+'/plan',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});var status=row.querySelector('.task-status');status.textContent=resp.ok?'已保存':(await resp.text()).trim();if(resp.ok)openSpec(currentSpec.id,true);};root.append(row);});if(!(tasks||[]).length)root.textContent='尚未拆出任务';}
async function openSpec(id,readOnly){var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var r=await fetch(endpoint+encodeURIComponent(id),{cache:'no-store'});if(!r.ok)throw new Error('open failed');var data=await r.json();currentSpec=data.spec;
 if(readOnly){document.getElementById('spec-state').textContent=data.spec.state;document.getElementById('spec-owner-short').textContent=data.spec.owner_short_name;document.getElementById('spec-current-editor-short').textContent=data.spec.current_editor_short_name||'无';renderSpec(data.revision.content_markdown,'spec-rendered');renderTasks(data.tasks||[],data.dependency_candidates||[]);document.getElementById('spec-workflow').hidden=!data.spec.can_edit;document.getElementById('finalize-spec').hidden=!(data.spec.can_edit&&data.spec.state==='review');document.getElementById('task-split').hidden=!data.spec.can_split;await loadSpecEditors();showScreen('detail-screen');return;}
 document.getElementById('edit-title').value=data.spec.title;specEditors.edit.value(data.revision.content_markdown);renderSpec(data.revision.content_markdown,'edit-rendered');
 var acceptance=JSON.parse(data.revision.acceptance_json);var anchors=JSON.parse(data.revision.input_anchors_json);
 document.getElementById('edit-acceptance').value=acceptance[0].text;document.getElementById('edit-anchor').value=anchors[0].ref;specEditors.edit.codemirror.setOption('readOnly',false);['edit-import','edit-acceptance','edit-anchor'].forEach(function(id){document.getElementById(id).disabled=false;});document.getElementById('save-spec').hidden=false;showScreen('editor');document.getElementById('edit-status').textContent='';}
document.getElementById('create-spec').onclick=async function(){var status=document.getElementById('create-status');status.textContent='';var body=payload('new');var missing=missingRequired(body);if(missing){status.className='error';status.textContent='缺少必填项: '+missing;return;}var r=await fetch('/api/console/specs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
 if(!r.ok){status.className='error';status.textContent=await responseFailure('创建失败',r);return;}status.className='ok';status.textContent='created';showScreen('list-screen');await refreshSpecs();};
document.getElementById('save-spec').onclick=async function(){var status=document.getElementById('edit-status');var body=payload('edit');body.expected_revision=currentSpec.current_revision;var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var headers={'Content-Type':'application/json'};if(sharedSpecID)headers['X-DW-Spec-Edit-Capability']=sharedEditToken;
 var missing=missingRequired(body);if(missing){status.className='error';status.textContent='缺少必填项: '+missing;return;}var r=await fetch(endpoint+encodeURIComponent(currentSpec.id),{method:'PATCH',headers:headers,body:JSON.stringify(body)});if(!r.ok){status.className='error';status.textContent=await responseFailure('保存失败',r);return;}
 var data=await r.json();status.className='ok';status.textContent='saved revision '+data.revision;renderSpec(body.content_markdown,'edit-rendered');await refreshSpecs();};
document.getElementById('task-hours').oninput=function(){hoursHint(this,document.getElementById('task-hours-hint'))};
document.getElementById('create-task').onclick=async function(){var status=document.getElementById('task-create-status');var body={title:document.getElementById('task-title').value.trim(),content:document.getElementById('task-content').value.trim(),definition_of_done:document.getElementById('task-dod').value.trim(),estimated_hours:Number(document.getElementById('task-hours').value),depends_on:Array.from(document.getElementById('task-deps').selectedOptions).map(function(o){return o.value})};if(!body.title||!body.content||body.estimated_hours<0){status.textContent='任务名、内容与非负工时必填';return;}var r=await fetch('/api/console/specs/'+encodeURIComponent(currentSpec.id)+'/tasks',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});status.textContent=r.ok?'已拆解':(await r.text()).trim();if(r.ok)await openSpec(currentSpec.id,true);};
async function loadSpecEditors(){var r=await fetch('/api/console/spec-editors',{cache:'no-store'});if(!r.ok)return;var data=await r.json(),select=document.getElementById('spec-next-editor');select.replaceChildren();(data.editors||[]).forEach(function(item){var o=document.createElement('option');o.value=item.short_name;o.textContent=item.short_name;select.append(o)});}
async function workflowAction(action,body){var status=document.getElementById('workflow-status');var r=await fetch('/api/console/specs/'+encodeURIComponent(currentSpec.id)+'/workflow/'+action,{method:'POST',headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});status.textContent=r.ok?'已完成':(await r.text()).trim();if(r.ok){await refreshSpecs();await openSpec(currentSpec.id,true);}}
document.getElementById('transfer-editing').onclick=function(){workflowAction('transfer',{target_short_name:document.getElementById('spec-next-editor').value,state:'editing'})};document.getElementById('transfer-review').onclick=function(){workflowAction('transfer',{target_short_name:document.getElementById('spec-next-editor').value,state:'review'})};document.getElementById('finalize-spec').onclick=function(){workflowAction('finalize')};document.getElementById('close-task-split').onclick=function(){workflowAction('close-tasks')};
document.getElementById('show-spec-list').onclick=function(){showScreen('list-screen');refreshSpecs();};document.getElementById('show-new-spec').onclick=function(){showScreen('new-screen');};document.getElementById('prev-page').onclick=function(){if(currentPage>1){currentPage--;refreshSpecs();}};document.getElementById('next-page').onclick=function(){if(currentPage<totalPages){currentPage++;refreshSpecs();}};
refreshSpecs().then(function(){if(sharedSpecID)return openSpec(sharedSpecID,true)}).catch(function(){document.getElementById('spec-list').textContent='Unable to load specs';});
</script>`

func (h *Hub) HandleConsoleSpecsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, renderedConsoleSpecsPage())
}

func (h *Hub) HandleSharedConsoleSpecPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, renderedConsoleSpecsPage())
}

func renderedConsoleSpecsPage() string {
	return strings.Replace(consoleSpecsPageHTML, "<script id=dw-md-core></script>", "<script>"+viewservice.MarkdownRenderCore+"</script>", 1)
}
