// Real Chromium DOM + fake clock; execute exact renderer state/SSE callbacks.
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const {chromium}=require('playwright');
const source=path.resolve(process.argv[2]||path.join(__dirname,'../view-service/renderer-canary-sse.html'));
const only=process.argv.find(x=>x.startsWith('--only='))?.split('=')[1];
const html=fs.readFileSync(source,'utf8');
const cut=(a,b)=>{const x=html.indexOf(a),y=html.indexOf(b,x);assert(x>=0&&y>x);return html.slice(x,y);};
const code=cut('    function clearStreamConnectTimer()', '    const STREAM_ALERT_TEXT =')+cut('    const STREAM_ALERT_TEXT =','    const managedParams =')+cut('    const managedParams =','    // Resource snapshots');
const style=html.match(/<style>([\s\S]*?)<\/style>/)[1];
const alert=html.match(/<section class="stream-alert"[\s\S]*?<\/section>/)[0];
const notice=html.match(/<div class="toast-region integrity-notice-region"[^>]*><\/div>/)[0];
let count=0;
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 async function fixture(){
  const page=await browser.newPage();await page.clock.install({time:new Date('2026-10-17T00:00:00Z')});await page.clock.pauseAt(new Date('2026-10-17T00:00:01Z'));
  await page.setContent('<style>'+style+'</style>'+alert+notice+'<main id="body">retained body</main><textarea id="draft">retained draft</textarea>');
  await page.evaluate(code=>{
   Object.assign(window,{sources:[],clockStatus:null,localTime:{},updateLocalTime(){},reconnectTimer:null,reconnectAttempt:0,RECONNECT_BACKOFF_MS:[1000,2000,5000,10000,30000],
    eventSource:null,eventSourceGeneration:0,streamConnectTimer:null,streamEverOpened:false,streamHasAcceptedEvent:false,streamTransportOpen:false,STREAM_CONNECT_GRACE_MS:7500,REPLAY_HARD_LIMIT_MS:1000,
    c1MergeBuf:new Map(),pendingRenders:new Set(),stats:{readonly:true},beginTimelineReplay(){},finishTimelineReplay(){},setStatus(){},apiEp:()=>'/synthetic-events',
    resetConversation(){throw Error('unexpected content reset');},handleIncomingEvent(){},flushPendingRenders(){}});
   window.EventSource=class{static OPEN=1;constructor(url){this.url=url;this.readyState=1;this.listeners={};window.sources.push(this);}addEventListener(n,f){this.listeners[n]=f;}close(){this.closed=true;}};
   const pre='const streamAlert=document.getElementById("streamAlert"),streamAlertText=document.getElementById("streamAlertText"),streamRetryBtn=document.getElementById("streamRetryBtn");';
   (0,eval)(pre+code+`;window.api={note:noteIntegrityIssue,admit:admitEventId,connect:connectEventStream,show:showStreamAlert,apply:markIntegrityApplyFailed,hide:hideStreamAlert,recover:requestStreamRecovery,
    history:()=>handleReplayGap({data:JSON.stringify({reason:'overflow'})}),
    capacity:()=>{admitEventId('aa-1');for(let n=3;n<=5003;n++)admitEventId('aa-'+n);},
    state:()=>({kind:integrityIssue?.kind,warning:integrityIssue?.warningKind,after:lastContiguousSeq})};`);
   window.api.connect();window.sources.at(-1).onopen();
  },code);return page;
 }
 const test=async(name,fn)=>{const p=await fixture();try{await fn(p);count++;console.log('PASS '+name);}finally{await p.close();}};
 try{
  for(const reason of ['history','tracking']){
   if(only&&only!==reason)continue;
   await test(reason+' is nonblocking, expires at exactly 10000ms, dedupes until successful reconnect',async p=>{
    const fire=reason==='history'?'window.api.history()':'window.api.capacity()';await p.evaluate(fire);
    const bubble=p.locator('.integrity-notice');assert.equal(await bubble.count(),1);assert.equal(await bubble.getAttribute('role'),'status');
    assert.match(await bubble.textContent(),reason==='history'?/最近约 1000 段输出（或 16MB）.*Agent 自己的会话.*当前内容完整/:/部分中间输出未能补齐，已从最新内容继续显示/);
    assert.equal(await p.locator('#streamAlert').isVisible(),false);assert.equal(await bubble.evaluate(e=>getComputedStyle(e).pointerEvents),'none');
    await p.clock.runFor(9999);assert.equal(await bubble.count(),1);
    await p.evaluate(reason==='history'?'window.api.history()':"window.api.note('tracking','same reason','tracking_limit')");
    await p.clock.runFor(1);assert.equal(await bubble.count(),0,'repetition must not extend TTL');
    await p.evaluate(reason==='history'?'window.api.history()':"window.api.note('tracking','same reason','tracking_limit')");assert.equal(await bubble.count(),0);
    await p.evaluate(()=>{window.api.connect();window.sources.at(-1).onerror();});
    await p.evaluate(reason==='history'?'window.api.history()':"window.api.note('tracking','','tracking_limit')");assert.equal(await bubble.count(),0,'failed connect must not rearm');
    await p.evaluate(()=>{window.api.connect();window.sources.at(-1).onopen();});
    await p.evaluate(reason==='history'?'window.api.history()':"window.api.note('tracking','','tracking_limit')");assert.equal(await bubble.count(),1);
    await p.clock.runFor(10000);assert.equal(await bubble.count(),0);
    assert.equal(await p.locator('#body').textContent(),'retained body');assert.equal(await p.locator('#draft').inputValue(),'retained draft');
   });
  }
  if(!only){
   await test('both reasons independently notify once, stale and duplicate opens cannot rearm them',async p=>{
    await p.evaluate(()=>{window.api.history();window.api.capacity();});assert.equal(await p.locator('.integrity-notice').count(),2);assert.equal(await p.locator('#streamAlert').isVisible(),false);
    await p.clock.runFor(10000);assert.equal(await p.locator('.integrity-notice').count(),0);
    await p.evaluate(()=>{window.sources.at(-1).onopen();window.api.history();window.api.note('tracking','','tracking_limit');});assert.equal(await p.locator('.integrity-notice').count(),0);
    await p.evaluate(()=>{const old=window.sources.at(-1);window.api.connect();old.onopen();window.api.history();});assert.equal(await p.locator('.integrity-notice').count(),0);
   });
   for(const reason of ['history','tracking'])await test(reason+' cannot hijack the real connection Retry action',async p=>{
    await p.evaluate(reason==='history'?'window.api.history()':"window.api.note('tracking','','tracking_limit')");
    await p.evaluate(()=>{window.api.show('conn');window.beforeRetry=window.sources.length;window.api.recover();});
    assert.equal(await p.evaluate(()=>window.sources.length),await p.evaluate(()=>window.beforeRetry+1));
    assert.equal((await p.evaluate(()=>window.api.state())).kind,reason);
   });
   for(const fault of ['apply','managed','recoverable','tracking','conn','replay'])for(const order of ['before','after']){
    await test(fault+' remains persistent '+order+' both informational notices',async p=>{
     await p.evaluate(({fault,order})=>{
      const fail=()=>fault==='apply'?window.api.apply():['conn','replay'].includes(fault)?window.api.show(fault):window.api.note(fault,'REAL_FAILURE');
      if(order==='before')fail();window.api.history();window.api.note('tracking','','tracking_limit');if(order==='after')fail();
     },{fault,order});
     await p.clock.runFor(20000);assert.equal(await p.locator('.integrity-notice').count(),0);
     assert.equal(await p.locator('#streamAlert').isVisible(),true);assert.equal(await p.locator('#streamAlert').getAttribute('role'),'alert');
     assert.match(await p.locator('#streamAlertText').textContent(),fault==='managed'?/REAL_FAILURE/:/^⚠️/);
    });
   }
   await test('apply failure cannot be dismissed by notice timeout or duplicate history',async p=>{
    await p.evaluate(()=>{window.api.history();window.api.apply();window.api.history();});await p.clock.runFor(10000);
    assert.equal(await p.locator('#streamAlert').isVisible(),true);assert.equal((await p.evaluate(()=>window.api.state())).warning,'apply');
   });
  }
  if(!only){
   const vm=require('vm'),{createRequire}=require('module');
   const base=path.resolve(__dirname,'../internal/m8/testdata/renderer_integrity_recovery.cjs'),text=fs.readFileSync(base,'utf8');
   const ctx={URLSearchParams,require:createRequire(base),__dirname:path.dirname(base),console,process:{argv:['node',base,source]}};
   vm.createContext(ctx);vm.runInContext(text.slice(0,text.indexOf('let checks=0;')),ctx);
   const h=vm.runInContext('fixture',ctx)({managed:true}),generation='b'.repeat(32),next='c'.repeat(32),instance='a'.repeat(32);
   await h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:instance,type:'event',agent_id:'synthetic',generation,
    event:{seq:'1',agent_id:'synthetic',generation,epoch:'aa',iframe_instance:instance,payload:{kind:'synthetic'}}}});
   h.run("noteIntegrityIssue('history')");assert.equal(h.notices.length,1);h.timer(10000);assert.equal(h.notices.length,0);
   const bind={protocol:'dw-console-mux/v1',iframe_instance:instance,type:'bind',agent_id:'synthetic',generation,next_generation:next};
   await h.handlers.message({source:{},origin:h.c.location.origin,data:bind});h.run("noteIntegrityIssue('history')");assert.equal(h.notices.length,0);
   await h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{...bind,next_generation:generation}});h.run("noteIntegrityIssue('history')");assert.equal(h.notices.length,0,'idempotent bind is not a new connection');
   await h.handlers.message({source:h.parent,origin:h.c.location.origin,data:bind});h.run("noteIntegrityIssue('history')");assert.equal(h.notices.length,1);
   console.log('PASS separate VM managed rebind rearms only after trusted parent/source/origin/generation binding');
  }
  console.log('TOTAL '+count+' Chromium/fake-clock notice and persistent-failure checks');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
