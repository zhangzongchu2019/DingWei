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
<div class=grid>
<section><h2>New spec</h2>
<label>Title <span class=required>*</span></label><input id=new-title required>
<label>Markdown <span class=required>*</span></label><textarea id=new-content required></textarea>
<label>Import Markdown</label><input id=new-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance <span class=required>*</span></label><input id=new-acceptance required>
<label>Owner message anchor <span class=required>*</span></label><input id=new-anchor required>
<button id=create-spec type=button>Create</button><span id=create-status></span>
</section>
<section><h2>My specs</h2><div id=spec-list></div></section>
</div>
<section id=editor hidden><h2>Edit spec</h2>
<h3>Rendered spec</h3><div id=spec-rendered class=spec-rendered></div>
<label>Title <span class=required>*</span></label><input id=edit-title readonly required>
<label>Markdown <span class=required>*</span></label><textarea id=edit-content required></textarea>
<label>Import Markdown</label><input id=edit-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance <span class=required>*</span></label><input id=edit-acceptance required>
<label>Owner message anchor <span class=required>*</span></label><input id=edit-anchor required>
<button id=save-spec type=button>Save new revision</button><span id=edit-status></span>
</section>
<script src="/console/spec-assets/easymde.min.js"></script><script id=dw-md-core></script><script>
var currentSpec=null,platformRole='',currentPrincipal='';
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
function renderSpec(markdown){var root=document.getElementById('spec-rendered');root.innerHTML=DwMdCore.renderMarkdown(markdown||'');wireSpecRender(root);}
async function specAction(id,path,body,status){status.textContent='';var opts={method:'POST'};if(body){opts.headers={'Content-Type':'application/json'};opts.body=JSON.stringify(body);}var r=await fetch('/api/console/specs/'+encodeURIComponent(id)+'/'+path,opts);if(!r.ok){status.className='spec-action-status error';status.textContent=await responseFailure('操作失败',r);return false;}await refreshSpecs();return true;}
async function refreshSpecs(){var r=await fetch(sharedSpecID?'/api/specs/shared/'+encodeURIComponent(sharedSpecID):'/api/console/specs',{cache:'no-store'});if(!r.ok)throw new Error('list failed');var raw=await r.json();var data=sharedSpecID?{role:'shared',principal:'',specs:[raw.spec]}:raw;platformRole=data.role||'';currentPrincipal=data.principal||'';var list=document.getElementById('spec-list');list.replaceChildren();
 (Array.isArray(data.specs)?data.specs:[]).forEach(function(spec){var row=document.createElement('div');row.className='spec-row';row.dataset.specId=spec.id;row.dataset.title=spec.title;
  var title=document.createElement('strong');title.textContent=spec.title;var meta=document.createElement('div');meta.className='meta';meta.textContent=spec.state+' · revision '+spec.current_revision;
  var status=document.createElement('span');status.className='spec-action-status';var isOwner=!!currentPrincipal&&spec.principal_id===currentPrincipal;var canEdit=(isOwner||!!(sharedSpecID&&sharedEditToken))&&!spec.editing_closed;
  row.append(title,meta);var view=document.createElement('button');view.type='button';view.className='view-spec';view.textContent='View';view.onclick=function(){openSpec(spec.id,true)};row.append(view);if(canEdit){var edit=document.createElement('button');edit.type='button';edit.className='edit-spec';edit.textContent='Edit';edit.onclick=function(){openSpec(spec.id,false)};row.append(edit);
   if(isOwner&&(spec.state==='draft'||spec.state==='needs_improvement')){var submit=document.createElement('button');submit.textContent='Submit';submit.onclick=function(){specAction(spec.id,'submit',null,status)};row.append(submit);}}
  if(platformRole==='management'){if(spec.state==='hard_gate'||spec.state==='human_review'){let stage=spec.state==='hard_gate'?'hard_gate':'human_review';var pass=document.createElement('button');pass.textContent=stage==='hard_gate'?'Pass hard gate':'Pass human review';pass.onclick=function(){specAction(spec.id,'reviews',{revision:spec.current_revision,stage:stage,verdict:'pass',reason_codes:[]},status)};row.append(pass);
   var fail=document.createElement('button');fail.textContent='Needs improvement';fail.onclick=function(){var reason=window.prompt('Reason code');if(!reason||!reason.trim())return;specAction(spec.id,'reviews',{revision:spec.current_revision,stage:stage,verdict:'fail',reason_codes:[reason.trim()]},status)};row.append(fail);}
   if(spec.state==='claimable'){var claim=document.createElement('button');claim.textContent='Claim';claim.onclick=function(){specAction(spec.id,'claim',null,status)};row.append(claim);}var archive=document.createElement('button');archive.textContent='Archive';archive.onclick=function(){specAction(spec.id,'archive',null,status)};row.append(archive);}
  row.append(status);
  list.append(row);});}
async function openSpec(id,readOnly){var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var r=await fetch(endpoint+encodeURIComponent(id),{cache:'no-store'});if(!r.ok)throw new Error('open failed');var data=await r.json();currentSpec=data.spec;
 document.getElementById('edit-title').value=data.spec.title;specEditors.edit.value(data.revision.content_markdown);renderSpec(data.revision.content_markdown);
 var acceptance=JSON.parse(data.revision.acceptance_json);var anchors=JSON.parse(data.revision.input_anchors_json);
 document.getElementById('edit-acceptance').value=acceptance[0].text;document.getElementById('edit-anchor').value=anchors[0].ref;specEditors.edit.codemirror.setOption('readOnly',!!readOnly);['edit-import','edit-acceptance','edit-anchor'].forEach(function(id){document.getElementById(id).disabled=!!readOnly;});document.getElementById('save-spec').hidden=!!readOnly;document.getElementById('editor').hidden=false;document.getElementById('edit-status').textContent=readOnly?'只读':'';}
document.getElementById('create-spec').onclick=async function(){var status=document.getElementById('create-status');status.textContent='';var body=payload('new');var missing=missingRequired(body);if(missing){status.className='error';status.textContent='缺少必填项: '+missing;return;}var r=await fetch('/api/console/specs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
 if(!r.ok){status.className='error';status.textContent=await responseFailure('创建失败',r);return;}status.className='ok';status.textContent='created';await refreshSpecs();};
document.getElementById('save-spec').onclick=async function(){var status=document.getElementById('edit-status');var body=payload('edit');body.expected_revision=currentSpec.current_revision;var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var headers={'Content-Type':'application/json'};if(sharedSpecID)headers['X-DW-Spec-Edit-Capability']=sharedEditToken;
 var missing=missingRequired(body);if(missing){status.className='error';status.textContent='缺少必填项: '+missing;return;}var r=await fetch(endpoint+encodeURIComponent(currentSpec.id),{method:'PATCH',headers:headers,body:JSON.stringify(body)});if(!r.ok){status.className='error';status.textContent=await responseFailure('保存失败',r);return;}
 var data=await r.json();status.className='ok';status.textContent='saved revision '+data.revision;renderSpec(body.content_markdown);await refreshSpecs();};
refreshSpecs().then(function(){if(sharedSpecID)return openSpec(sharedSpecID,!sharedEditToken)}).catch(function(){document.getElementById('spec-list').textContent='Unable to load specs';});
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
