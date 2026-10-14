// Exact full renderer in an isolated Chromium page. All HTTP is intercepted;
// synthetic /f/ objects only, no live Hub, upload, credentials or external GET.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('playwright');
const source=path.resolve(process.argv[2]||'view-service/renderer-canary-sse.html');
const html=fs.readFileSync(source,'utf8');
const origin='https://file-card.invalid';
const url=origin+'/f/u_owner/agent/26/1014/'+'a6a7f17b'.repeat(8)+'.md';
const pause=ms=>new Promise(r=>setTimeout(r,ms));
let browser,passed=0,failed=0;
async function fixture(){
 const context=await browser.newContext();const page=await context.newPage();
 const calls=[],held=new Map(),pending=new Map();
 await page.route('**/*',async route=>{
  const request=route.request(),u=new URL(request.url());
  if(u.origin!==origin)return route.abort();
  if(u.pathname.startsWith('/f/')){
   calls.push({url:request.url(),method:request.method()});
   if(request.method()==='HEAD'&&held.has(request.url())){
    await new Promise(resolve=>pending.set(request.url(),resolve));
   }
   return route.fulfill({status:u.pathname.includes('/missing/')?404:200,
    contentType:'application/octet-stream',headers:{'Content-Length':'12'},body:request.method()==='HEAD'?'':'fixture body'});
  }
  if(u.pathname==='/view/test')return route.fulfill({status:200,contentType:'text/html',body:html});
  if(u.pathname.endsWith('/status'))return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({session:'test',state:'idle',readonly:false})});
  return route.fulfill({status:404,body:''});
 });
 await page.goto(origin+'/view/test',{waitUntil:'domcontentloaded'});
 await page.waitForFunction(()=>typeof attachFileRenders==='function');
 await page.evaluate(()=>{closeEventStream();clearReconnectTimer();replayInProgress=false;});
 async function card(id){await page.evaluate(id=>{
  const c=document.createElement('article');c.className='message-card';c.id=id;
  const body=document.createElement('span');body.className='event-content';c.append(body);timeline.append(c);
 },id);}
 async function attach(id,text,streaming){await page.evaluate(({id,text,streaming})=>{
  const c=document.getElementById(id);c.querySelector('.event-content').textContent=text;
  attachFileRenders(c,text,{streaming});
 },{id,text,streaming});}
 async function cards(id,n){await page.waitForFunction(({id,n})=>document.getElementById(id).querySelectorAll('.attachment').length===n,{id,n});}
 async function state(id){return page.evaluate(id=>{
  const c=document.getElementById(id);return {slots:c._fileHost?.children.length||0,
   urls:[...c.querySelectorAll('.attachment')].map(x=>x.dataset.fileUrl),
   body:c.querySelector('.event-content')?.textContent,visible:c.querySelector('.event-content')?.innerText,
   note:c.querySelector('.attachment-note')?.textContent};
 },id);}
 return {page,calls,card,attach,cards,state,hold:u=>held.set(u,true),async release(u){
  const end=Date.now()+3000;while(!pending.has(u)&&Date.now()<end)await pause(5);
  assert(pending.has(u),'deferred HEAD was not observed');pending.get(u)();pending.delete(u);
 },close:()=>context.close()};
}
async function test(name,fn){const h=await fixture();try{await fn(h);console.log('PASS '+name);passed++;}catch(e){console.error('FAIL '+name+': '+e.message);failed++;}finally{await h.close();}}
(async()=>{
 browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 await test('actual attach: character-by-character live URL creates no prefix probes; delimiter produces exactly one complete card',async h=>{
  await h.card('stream');
  const interim=await h.page.evaluate(u=>{
   const c=document.getElementById('stream');let text='report: ';
   for(const ch of u){text+=ch;c.querySelector('.event-content').textContent=text;attachFileRenders(c,text,{streaming:true});}
   return {slots:c._fileHost?.children.length||0,body:c.querySelector('.event-content').innerText};
  },url);
  assert.equal(interim.slots,0,'a growing URL must have no pending or resolved cards');
  assert(interim.body.endsWith(url),'deferred URL must remain visible/copyable');
  await pause(30);assert.equal(h.calls.length,0,'growing URL must not issue HEAD/GET');
  await h.attach('stream','report: '+url+'\n',true);await h.cards('stream',1);
  assert.deepEqual((await h.state('stream')).urls,[url]);
  assert.equal(h.calls.filter(x=>x.method==='HEAD').length,1);
 });
 await test('actual live turn lifecycle: no trailing-prefix card until turn_completed',async h=>{
  const interim=await h.page.evaluate(u=>{
   let seq=0;for(const text of ['report: ',...u]){handleIncomingEvent({kind:'assistant_text',turn_id:'file-turn',cursor:String(++seq),text});flushPendingRenders();}
   const c=turnGroups.get('file-turn').node;c.id='turn';return c._fileHost?.children.length||0;
  },url);
  assert.equal(interim,0);assert.equal(h.calls.length,0);
  await h.page.evaluate(()=>handleIncomingEvent({kind:'state_change',turn_id:'file-turn',cursor:'999',data:{change:'turn_completed'}}));
  await h.cards('turn',1);assert.deepEqual((await h.state('turn')).urls,[url]);
 });
 await test('pending prefix subscriber is removed and late HEAD cannot resurrect it while suffix grows',async h=>{
  const prefix=url.slice(0,-3);h.hold(prefix);await h.card('pending');
  await h.attach('pending',prefix,false);assert.equal((await h.state('pending')).slots,1);
  await h.attach('pending',url,true);assert.equal((await h.state('pending')).slots,0);
  await h.release(prefix);await pause(50);assert.equal((await h.state('pending')).slots,0,'obsolete probe resurrected its card');
  await h.attach('pending',url,false);await h.cards('pending',1);assert.deepEqual((await h.state('pending')).urls,[url]);
 });
 await test('resolved strict prefix cards collapse to longest URL within one card',async h=>{
  const prefix=url.slice(0,-3);await h.card('resolved');await h.attach('resolved',prefix,false);await h.cards('resolved',1);
  await h.attach('resolved',prefix+'\n'+url,false);await h.cards('resolved',1);
  assert.deepEqual((await h.state('resolved')).urls,[url]);
 });
 await test('removing a successful prefix restores its hidden body URL until the complete card succeeds',async h=>{
  const prefix=url.slice(0,-3);await h.card('marks');await h.attach('marks',prefix,false);await h.cards('marks',1);
  assert.equal(await h.page.locator('#marks .dw-link-rendered').count(),1,'existing successful card must retain R5-2');
  const pending=await h.page.evaluate(({prefix,url})=>{
   const c=document.getElementById('marks'),body=c.querySelector('.event-content');
   body.append(document.createTextNode(url.slice(prefix.length)));
   attachFileRenders(c,url,{streaming:true});
   return {slots:c._fileHost.children.length,marks:c.querySelectorAll('.dw-link-rendered').length,visible:body.innerText,copyable:body.textContent};
  },{prefix,url});
  assert.equal(pending.slots,0);assert.equal(pending.marks,0,'removed prefix must leave no hidden marker');
  assert.equal(pending.visible,url);assert.equal(pending.copyable,url);
  await h.page.evaluate(u=>attachFileRenders(document.getElementById('marks'),u,{streaming:false}),url);
  await h.cards('marks',1);assert.deepEqual((await h.state('marks')).urls,[url]);
  assert.equal(await h.page.locator('#marks .dw-link-rendered').count(),1,'successful complete card reapplies unchanged R5-2');
  assert.equal(await h.page.locator('#marks .dw-link-rendered').getAttribute('data-file-url'),url);
 });
 await test('history replay and complete one-shot user message still render a tail URL immediately',async h=>{
  await h.card('history');await h.attach('history',url,false);await h.cards('history',1);
  assert.deepEqual((await h.state('history')).urls,[url]);
  await h.page.evaluate(u=>{handleIncomingEvent({kind:'user_input',cursor:'file-once',text:u});timeline.lastElementChild.id='user-once';},url);
  await h.cards('user-once',1);assert.deepEqual((await h.state('user-once')).urls,[url]);
  await h.page.evaluate(u=>{replayInProgress=true;handleIncomingEvent({kind:'assistant_text',turn_id:'history-turn',cursor:'1',text:u});turnGroups.get('history-turn').node.id='history-turn';},url);
  await h.cards('history-turn',1);assert.deepEqual((await h.state('history-turn')).urls,[url]);
 });
 await test('standalone assistant card finalization creates the complete tail attachment',async h=>{
  await h.page.evaluate(u=>{renderAssistant({kind:'assistant_text',cursor:'single',text:u});flushPendingRenders();messageNodes.get('cursor-single').closest('.message-card').id='single';},url);
  assert.equal((await h.state('single')).slots,0);
  await h.page.evaluate(()=>renderState({kind:'state_change',data:{change:'turn_completed'}}));
  await h.cards('single',1);assert.deepEqual((await h.state('single')).urls,[url]);
 });
 await test('404 describes missing/invalid link without asserting expiry; original body stays visible',async h=>{
  const missing=origin+'/f/missing/report.md';await h.card('missing');await h.attach('missing',missing,false);await h.cards('missing',1);
  const s=await h.state('missing');assert(s.note.startsWith('文件不存在或链接无效（服务端 404）'));
  assert(!s.note.includes('已不可取'));assert(s.note.includes('若是 7 天前的链接'));
  assert(s.visible.includes(missing));assert(s.body.includes(missing));
 });
 await test('cancel one card subscriber without cancelling another card sharing its HEAD',async h=>{
  const prefix=url.slice(0,-3);h.hold(prefix);await h.card('a');await h.card('b');
  await h.attach('a',prefix,false);await h.attach('b',prefix,false);
  await h.attach('a',url,false);await h.release(prefix);
  await h.cards('a',1);await h.cards('b',1);
  assert.deepEqual((await h.state('a')).urls,[url]);assert.deepEqual((await h.state('b')).urls,[prefix]);
  assert.equal(h.calls.filter(x=>x.url===prefix&&x.method==='HEAD').length,1,'shared HEAD cache must remain effective');
 });
 await test('closing markdown delimiter admits a complete live URL and repeated rerender does not duplicate',async h=>{
  await h.card('closed');await h.attach('closed','[report]('+url+')',true);await h.cards('closed',1);
  await h.attach('closed','[report]('+url+')',true);await pause(20);
  assert.deepEqual((await h.state('closed')).urls,[url]);
 });
 console.log(`TOTAL ${passed} passed, ${failed} failed (Chromium, exact renderer, intercepted HTTP only)`);
 await browser.close();process.exitCode=failed?1:0;
})().catch(async e=>{console.error(e.stack);if(browser)await browser.close();process.exitCode=1;});
