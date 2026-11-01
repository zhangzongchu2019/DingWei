'use strict';
// Independent, deliberately small DOM Markdown renderer. Repository text is
// never assigned to HTML; images are text and only explicit HTTPS links exist.
(function () {
 const form=document.getElementById('coordinates'), message=document.getElementById('message'), coordinate=document.getElementById('coordinate'), docs=document.getElementById('documents'), article=document.getElementById('document');
 let generation=0, loaded=null, pending=null;
 function clear(){generation++;loaded=null;if(pending)pending.abort();article.replaceChildren();docs.replaceChildren();coordinate.replaceChildren();message.textContent='';}
 function allowedPath(p){return p.startsWith('openspec/')&&/\.md$/i.test(p)&&!/[\\%\x00-\x20?#:]/.test(p)&&!p.includes('//')&&!p.split('/').some(x=>x==='.'||x==='..');}
 function link(raw,c){
  if(!raw||/[\x00-\x20\\%<>"'`]/.test(raw)||raw.startsWith('//'))return null;
  if(raw.startsWith('https://')){try{const u=new URL(raw);if(u.protocol!=='https:'||u.username||u.password)return null;
   const source=new URL(c.source);if(u.hostname===source.hostname&&u.pathname.includes('/blob/')){const prefix=source.pathname.split('/blob/')[0]+'/blob/'+c.commit+'/';if(!u.pathname.startsWith(prefix))return null;}
   return {href:u.href};}catch(_){return null;}}
  if(raw.includes(':')||raw.startsWith('/'))return null;
  try{const base=new URL('https://document.invalid/'+c.path),u=new URL(raw,base);const p=u.pathname.slice(1);if(u.origin!==base.origin||!allowedPath(p)||u.search)return null;return {path:p};}catch(_){return null;}
 }
 function inline(parent,text,c){
  const re=/(!?)\[([^\]\n]*)\]\(([^)\n]*)\)/g;let start=0,m;
  while((m=re.exec(text))){parent.append(document.createTextNode(text.slice(start,m.index)));if(m[1]){parent.append(document.createTextNode('[图片：'+m[2]+']'));}else{const target=link(m[3],c);if(!target)parent.append(document.createTextNode(m[2]));else{const a=document.createElement('a');a.textContent=m[2];if(target.path){a.href='#document';a.addEventListener('click',e=>{e.preventDefault();load({...c,path:target.path});});}else{a.href=target.href;a.target='_blank';a.rel='noopener noreferrer';a.referrerPolicy='no-referrer';}parent.append(a);}}start=re.lastIndex;}
  parent.append(document.createTextNode(text.slice(start)));
 }
 function render(text,c){
  article.replaceChildren();let code=null,fence='';
  for(const line of text.split('\n')){
   const f=line.match(/^\s*(`{3,}|~{3,})/);
   if(f){if(code&&f[1][0]===fence[0]&&f[1].length>=fence.length){code=null;fence='';}else if(!code){fence=f[1];const pre=document.createElement('pre');code=document.createElement('code');pre.append(code);article.append(pre);}else code.append(document.createTextNode(line+'\n'));continue;}
   if(code){code.append(document.createTextNode(line+'\n'));continue;}
   const heading=line.match(/^(#{1,6})\s+(.*)$/);const node=document.createElement(heading?'h'+heading[1].length:'p');inline(node,heading?heading[2]:line,c);article.append(node);
  }
 }
 async function load(input){
  clear();const mine=generation;pending=new AbortController();message.textContent='读取中…';
  const c={provider:input.provider,instance:input.instance,repository:input.repository};if(input.commit)c.commit=input.commit;else c.ref=input.ref;if(input.path)c.path=input.path;
  coordinate.textContent=JSON.stringify(c);
  try{const response=await fetch('/api/console/openspec-read?'+new URLSearchParams(c),{credentials:'same-origin',cache:'no-store',signal:pending.signal});const data=await response.json();if(mine!==generation)return;
   if(!response.ok){message.textContent=data.error||'读取失败';if(response.status===413&&data.source){const target=link(data.source,{...data.coordinate,source:data.source});if(target&&target.href){const a=document.createElement('a');a.textContent='在仓库托管平台查看';a.href=target.href;a.target='_blank';a.rel='noopener noreferrer';a.referrerPolicy='no-referrer';coordinate.append(a);}}return;}
   loaded=data;coordinate.textContent=[data.provider,data.instance,data.repository,data.commit,data.path||'openspec/'].join(' / ');
   if(data.source){const a=document.createElement('a');a.textContent='不可变来源';a.href=data.source;a.target='_blank';a.rel='noopener noreferrer';a.referrerPolicy='no-referrer';coordinate.append(document.createTextNode(' '),a);}
   for(const entry of data.entries||[]){const a=document.createElement('a');a.textContent=entry.path;a.href='#document';a.addEventListener('click',e=>{e.preventDefault();load({...data,path:entry.path});});const p=document.createElement('p');p.append(a);docs.append(p);}
   render(data.text||'',data);message.textContent='只读 · '+new TextEncoder().encode(data.text||'').length+' 字节';
  }catch(e){if(mine===generation&&e.name!=='AbortError')message.textContent='读取服务不可用或超时';}
 }
 form.addEventListener('input',clear);form.addEventListener('submit',e=>{e.preventDefault();const v=Object.fromEntries(new FormData(form));const version=v.version;delete v.version;if(/^[0-9a-f]{40}$/.test(version))v.commit=version;else v.ref=version;load(v);});
 // Export pure policy + renderer only for the local browser security harness.
 window.DwOpenSpecRead={link,allowedPath,render};
})();
