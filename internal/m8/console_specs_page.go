package m8

import (
	"fmt"
	"net/http"
)

const consoleSpecsPageHTML = `<!doctype html><meta charset=utf-8><title>DingWei Specs</title>
<link rel=stylesheet href="/console/spec-assets/easymde.min.css">
<style>
body{font:14px sans-serif;margin:20px;max-width:1100px}h1,h2{margin:8px 0}.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
section{border:1px solid #ccd4df;border-radius:8px;padding:14px}label{display:block;margin:8px 0 3px}input,textarea{box-sizing:border-box;width:100%;padding:7px}
textarea{min-height:110px}button{margin:8px 6px 0 0;padding:6px 10px}.spec-row{border:1px solid #dde3ea;border-radius:6px;padding:9px;margin:7px 0}
.meta{color:#667085;font-size:12px}.error{color:#b42318}.ok{color:#067647}@media(max-width:760px){.grid{grid-template-columns:1fr}}
.editor-toolbar button.dw-icon{color:#344054;background:#fff}.editor-toolbar button.dw-icon:hover,.editor-toolbar button.dw-icon.active{color:#101828;background:#d0d5dd}
.dw-icon:before{font:700 13px/1 sans-serif}.dw-bold:before{content:'B'}.dw-italic:before{content:'I';font-style:italic}.dw-heading:before{content:'H'}.dw-link:before{content:'↗'}.dw-image:before{content:'▧'}.dw-list:before{content:'•'}
</style>
<h1>Spec management</h1><p>一个账号可同时管理多份 spec。认领后的版本不可原地覆盖。</p>
<div class=grid>
<section><h2>New spec</h2>
<label>Title</label><input id=new-title>
<label>Markdown</label><textarea id=new-content></textarea>
<label>Import Markdown</label><input id=new-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance</label><input id=new-acceptance>
<label>Owner message anchor</label><input id=new-anchor>
<button id=create-spec type=button>Create</button><span id=create-status></span>
</section>
<section><h2>My specs</h2><div id=spec-list></div></section>
</div>
<section id=editor hidden><h2>Edit spec</h2>
<label>Title</label><input id=edit-title readonly>
<label>Markdown</label><textarea id=edit-content></textarea>
<label>Import Markdown</label><input id=edit-import type=file accept=".md,.markdown,text/markdown,text/plain">
<label>Acceptance</label><input id=edit-acceptance>
<label>Owner message anchor</label><input id=edit-anchor>
<button id=save-spec type=button>Save new revision</button><span id=edit-status></span>
</section>
<script src="/console/spec-assets/easymde.min.js"></script><script>
var currentSpec=null,platformRole='';
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
async function specAction(id,path,body){var opts={method:'POST'};if(body){opts.headers={'Content-Type':'application/json'};opts.body=JSON.stringify(body);}var r=await fetch('/api/console/specs/'+encodeURIComponent(id)+'/'+path,opts);if(!r.ok)throw new Error(path+' failed');await refreshSpecs();}
async function refreshSpecs(){var r=await fetch(sharedSpecID?'/api/specs/shared/'+encodeURIComponent(sharedSpecID):'/api/console/specs',{cache:'no-store'});if(!r.ok)throw new Error('list failed');var raw=await r.json();var data=sharedSpecID?{role:'shared',specs:[raw.spec]}:raw;platformRole=data.role||'';var list=document.getElementById('spec-list');list.replaceChildren();
 (Array.isArray(data.specs)?data.specs:[]).forEach(function(spec){var row=document.createElement('div');row.className='spec-row';row.dataset.specId=spec.id;row.dataset.title=spec.title;
  var title=document.createElement('strong');title.textContent=spec.title;var meta=document.createElement('div');meta.className='meta';meta.textContent=spec.state+' · revision '+spec.current_revision;
  row.append(title,meta);if(platformRole!=='management'){var edit=document.createElement('button');edit.type='button';edit.className='edit-spec';edit.textContent='Edit';edit.onclick=function(){openSpec(spec.id)};row.append(edit);
   if(spec.state==='draft'||spec.state==='needs_improvement'){var submit=document.createElement('button');submit.textContent='Submit';submit.onclick=function(){specAction(spec.id,'submit')};row.append(submit);}}
  if(platformRole==='management'){if(spec.state==='hard_gate'||spec.state==='human_review'){let stage=spec.state==='hard_gate'?'hard_gate':'human_review';var pass=document.createElement('button');pass.textContent=stage==='hard_gate'?'Pass hard gate':'Pass human review';pass.onclick=function(){specAction(spec.id,'reviews',{revision:spec.current_revision,stage:stage,verdict:'pass',reason_codes:[]})};row.append(pass);
   var fail=document.createElement('button');fail.textContent='Needs improvement';fail.onclick=function(){var reason=window.prompt('Reason code');if(!reason||!reason.trim())return;specAction(spec.id,'reviews',{revision:spec.current_revision,stage:stage,verdict:'fail',reason_codes:[reason.trim()]})};row.append(fail);}
   if(spec.state==='claimable'){var claim=document.createElement('button');claim.textContent='Claim';claim.onclick=function(){specAction(spec.id,'claim')};row.append(claim);}var archive=document.createElement('button');archive.textContent='Archive';archive.onclick=function(){specAction(spec.id,'archive')};row.append(archive);}
  list.append(row);});}
async function openSpec(id){var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var r=await fetch(endpoint+encodeURIComponent(id),{cache:'no-store'});if(!r.ok)throw new Error('open failed');var data=await r.json();currentSpec=data.spec;
 document.getElementById('edit-title').value=data.spec.title;specEditors.edit.value(data.revision.content_markdown);
 var acceptance=JSON.parse(data.revision.acceptance_json);var anchors=JSON.parse(data.revision.input_anchors_json);
 document.getElementById('edit-acceptance').value=acceptance[0].text;document.getElementById('edit-anchor').value=anchors[0].ref;document.getElementById('editor').hidden=false;document.getElementById('edit-status').textContent='';}
document.getElementById('create-spec').onclick=async function(){var status=document.getElementById('create-status');status.textContent='';var r=await fetch('/api/console/specs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload('new'))});
 if(!r.ok){status.className='error';status.textContent='create failed';return;}status.className='ok';status.textContent='created';await refreshSpecs();};
document.getElementById('save-spec').onclick=async function(){var status=document.getElementById('edit-status');var body=payload('edit');body.expected_revision=currentSpec.current_revision;var endpoint=sharedSpecID?'/api/specs/shared/':'/api/console/specs/';var headers={'Content-Type':'application/json'};if(sharedSpecID)headers['X-DW-Spec-Edit-Capability']=sharedEditToken;
 var r=await fetch(endpoint+encodeURIComponent(currentSpec.id),{method:'PATCH',headers:headers,body:JSON.stringify(body)});if(!r.ok){status.className='error';status.textContent=r.status===403?'editing closed or invalid link':'save failed';return;}
 var data=await r.json();status.className='ok';status.textContent='saved revision '+data.revision;await refreshSpecs();};
refreshSpecs().then(function(){if(sharedSpecID)return openSpec(sharedSpecID)}).catch(function(){document.getElementById('spec-list').textContent='Unable to load specs';});
</script>`

func (h *Hub) HandleConsoleSpecsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, consoleSpecsPageHTML)
}

func (h *Hub) HandleSharedConsoleSpecPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, consoleSpecsPageHTML)
}
