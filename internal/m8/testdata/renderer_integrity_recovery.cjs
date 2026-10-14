// Offline exact-source fixture, no HTTP/browser/credentials/real timers.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert/strict');
const source=process.argv[2]||path.join(__dirname,'../../..','view-service/renderer-canary-sse.html');
const html=fs.readFileSync(source,'utf8');
const slice=(a,b)=>{const x=html.indexOf(a),z=html.indexOf(b,x);assert.ok(x>=0&&z>x,a);return html.slice(x,z);};
if(process.argv.includes('--parent')){
 const h=slice("    streamRetryBtn.addEventListener('click',",'    async function doSend()');let reloads=0;
 const c={streamRetryBtn:{addEventListener:(k,f)=>c.click=f},streamAlertMode:'gap',hideStreamAlert:()=>{},window:{location:{reload:()=>reloads++}},connectEventStream:()=>{}};
 vm.runInNewContext(h,c);c.click();assert.equal(reloads,0,'permanent/unclassified gap action must not repeat page reload');process.exit(0);
}
function fixture({managed=false,fail=false,instance="a".repeat(32),holdManaged=false}={}){
 let now=100000,tid=0,connections=0,resets=0,rendered=0;
 const timers=new Map(),handlers={},alert={hidden:true,setAttribute(k,v){this[k]=v;}},text={textContent:'connection'},button={};
 const parent={postMessage:d=>replies.push(d)},replies=[];
 const doc={hidden:false,getElementById:()=>text};
 const es=[];class EventSource{static OPEN=1;constructor(url){this.url=url;this.readyState=1;this.listeners={};es.push(this);connections++;}addEventListener(n,f){this.listeners[n]=f;}close(){this.closed=true;}}
 const window={addEventListener:(k,f)=>handlers[k]=f};
 const c={console,Math,Set,Map,Promise,BigInt,Error,URLSearchParams,Date:class extends Date{static now(){return now;}},
  setTimeout:(f,delay)=>{const id=++tid;timers.set(id,{f,delay});return id;},clearTimeout:id=>timers.delete(id),
  streamAlert:alert,streamAlertText:text,streamRetryBtn:button,document:doc,window,parent,location:{origin:'https://synthetic.invalid',search:managed?'?console_mux=1&iframe_instance='+instance:''},
  clockStatus:null,localTime:{},updateLocalTime:()=>{},reconnectTimer:null,reconnectAttempt:0,RECONNECT_BACKOFF_MS:[1000,2000,5000,10000,30000],
  eventSource:null,eventSourceGeneration:0,streamConnectTimer:null,streamEverOpened:false,streamHasAcceptedEvent:false,streamTransportOpen:false,STREAM_CONNECT_GRACE_MS:5000,REPLAY_HARD_LIMIT_MS:1000,
  c1MergeBuf:new Map(),pendingRenders:new Set(),stats:{readonly:true},beginTimelineReplay:()=>{},finishTimelineReplay:()=>{},setStatus:()=>{},apiEp:()=>'/synthetic-events',
  EventSource,resetConversation:()=>resets++,handleIncomingEvent:()=>{if(fail)throw Error('SYNTHETIC');if(holdManaged)vm.runInContext('managedApplyPending.records.add({timer:0})',c);rendered++;},flushPendingRenders:()=>{}};
 vm.createContext(c);
 const code=slice('    function clearStreamConnectTimer()', '    const STREAM_ALERT_TEXT =')+slice('    const STREAM_ALERT_TEXT =','    const managedParams =')+slice('    const managedParams =','    async function refreshStatus()');
 vm.runInContext(code,c);
 const run=x=>vm.runInContext(x,c);
 const timer=delay=>{const hit=[...timers].find(([id,v])=>v.delay===delay);assert.ok(hit,'timer '+delay);timers.delete(hit[0]);hit[1].f();};
 const event=(seq,data='{}')=>{if(!es.length)run('connectEventStream()');es.at(-1).onmessage({lastEventId:'aa-'+seq,data});};
 const installRendering=()=>{
  Object.assign(c,{C2_MERGE_HOLD_MS:3000,cancelRenderFlush:()=>{},replayInProgress:false,timelineAtBottom:false,
   scrollTimelineToBottom:()=>{},renderEvent:()=>rendered++,admitWaitingDelivery:()=>{},
   renderPendingEntry:entry=>{if(entry.fail)throw Error('SYNTHETIC_RENDER');entry.done=true;rendered++;}});
  vm.runInContext(slice('    function bufferMergeCopy(', '    function c1Footnote(')+slice('    function flushPendingRenders()', '    function scheduleRenderFlush()'),c);
 };
 return {c,run,event,installRendering,es,alert,text,button,timer,timers,replies,parent,handlers,advance:ms=>now+=ms,
  state:()=>run('({issue:integrityIssue&&{...integrityIssue},mode:streamAlertMode,after:lastContiguousSeq,above:seenAboveSeq.size,charged:gapRefillTimes.length,stopped:gapRefillStopped,applied:managedApplied.toString()})'),
  counts:()=>({connections,resets,rendered})};
}
let checks=0;function test(name,fn){fn();console.log('PASS '+name);checks++;}
(async()=>{
 test('server overflow persists without reload or body reset; new events still applied',()=>{
  const h=fixture();h.event(10);h.run(`handleReplayGap({data:'{"reason":"overflow"}'})`);
  assert.equal(h.state().issue.kind,'history');h.run('requestStreamRecovery()');h.event(11);
  assert.equal(h.state().issue.kind,'history');assert.equal(h.counts().resets,0);assert.equal(h.counts().rendered,2);
  assert.match(h.text.textContent,/更早的历史已超出服务端保留范围，无法加载/);assert.match(h.text.textContent,/当前内容完整保留/);
 });
 test('temporary missing seq closes only after actual contiguous render evidence',()=>{
  const h=fixture();h.event(1);h.event(3);assert.equal(h.state().issue.kind,'recoverable');h.run('hideStreamAlert()');assert.equal(h.alert.hidden,false);
  h.event(2);assert.equal(h.state().issue,null);assert.equal(h.state().mode,'result');assert.equal(h.alert.role,'status');assert.equal(h.state().after,3);assert.equal(h.counts().resets,0);
 });
 test('queued renders or merge records prevent premature completeness',()=>{
  for(const key of ['pendingRenders','c1MergeBuf']){const h=fixture();h.event(1);h.event(3);h.run(key+(key==='c1MergeBuf'?'.set("synthetic",[])':'.add("synthetic")'));h.event(2);
   assert.equal(h.state().issue.kind,'recoverable');h.run(key+'.clear();maybeClearRecoveredGap()');assert.equal(h.state().issue,null);}
 });
 test('render failure never clears issue by empty seenAbove alone',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('markIntegrityApplyFailed()');h.event(2);assert.equal(h.state().issue.kind,'apply');assert.equal(h.state().mode,'gap');
 });
 test('invalid JSON does not advance event cursor',()=>{const h=fixture();h.event(1);h.event(2,'{');assert.equal(h.state().after,1);assert.equal(h.state().issue.kind,'apply');});
 test('tracking capacity loss cannot masquerade as repaired contiguous history',()=>{
  const h=fixture();h.event(1);h.run('for(let n=3;n<=5003;n++)admitEventId("aa-"+n)');assert.equal(h.state().issue.kind,'tracking');
  h.run('maybeClearRecoveredGap()');assert.equal(h.state().issue.kind,'tracking');
 });
 test('epoch change does not count a different baseline as healed old gap',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('admitEventId("bb-1");maybeClearRecoveredGap()');assert.equal(h.state().issue.kind,'tracking');
 });
 test('naturally healed gap before actual reconnect consumes no refill',()=>{
  const h=fixture();h.event(1);h.event(3);h.timer(2000);const n=h.counts().connections;h.event(2);h.timer(1000);
  assert.equal(h.state().charged,0);assert.equal(h.counts().connections,n);
 });
 test('real automatic reconnect charged once at execution',()=>{
  const h=fixture();h.event(1);h.event(3);h.timer(2000);assert.equal(h.state().charged,0);h.timer(1000);assert.equal(h.state().charged,1);assert.equal(h.counts().connections,2);
 });
 test('manual recovery retains text/cursor and reports verified success',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('requestStreamRecovery()');assert.equal(h.counts().connections,2);assert.equal(h.state().issue.retrying,true);assert.equal(h.state().after,1);
  h.event(2);assert.equal(h.state().mode,'result');assert.match(h.text.textContent,/已补齐并完成渲染/);assert.equal(h.counts().resets,0);assert.equal(h.button.disabled,false);
 });
 test('manual timeout reports lack of proof without discarding current body',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('requestStreamRecovery()');h.timer(8000);assert.match(h.text.textContent,/8 秒内尚未确认/);assert.equal(h.state().issue.retrying,false);assert.equal(h.counts().resets,0);h.event(4);assert.equal(h.counts().rendered,3);
 });
 test('new out-of-order event cannot erase in-flight result/timeout state',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('requestStreamRecovery()');h.event(5);assert.equal(h.state().issue.retrying,true);h.timer(8000);assert.match(h.text.textContent,/尚未确认/);
 });
 test('three attempts stay bounded and show an honest exhausted result',()=>{
  const h=fixture();h.event(1);h.event(3);h.run('gapRefillTimes=[Date.now(),Date.now(),Date.now()];requestStreamRecovery()');assert.equal(h.counts().connections,1);assert.match(h.text.textContent,/次数已达上限/);assert.equal(h.state().charged,3);
 });
 test('managed stopped reason do not promise historical recovery',()=>{
  const h=fixture({managed:true});h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:'a'.repeat(32),type:'stopped',reason:'FIXED_STOP_REASON'}});
  assert.equal(h.state().issue.kind,'managed');assert.match(h.text.textContent,/FIXED_STOP_REASON/);h.run('requestStreamRecovery()');assert.equal(h.counts().connections,0);assert.match(h.text.textContent,/不能用页面刷新保证恢复/);
 });
 test('legacy replay uncertainty shows scope instead of infinite reload',()=>{const h=fixture();h.run(`showStreamAlert('replay');requestStreamRecovery()`);assert.match(h.text.textContent,/没有逐事件完整性证据/);assert.equal(h.counts().connections,0);});
 test('success is explicitly dismissible and does not mark global history complete',()=>{const h=fixture();h.event(1);h.event(3);h.event(2);assert.match(h.text.textContent,/不代表更早历史完整/);h.run('requestStreamRecovery()');assert.equal(h.alert.hidden,true);});
 test('actual deferred merge completion closes only after its final record renders',()=>{
  const h=fixture();h.installRendering();h.event(1);h.event(3);h.run(`bufferMergeCopy('synthetic',{kind:'synthetic'},{holdMs:1234})`);h.event(2);
  assert.equal(h.state().issue.kind,'recoverable');h.timer(1234);assert.equal(h.state().issue,null);assert.equal(h.state().mode,'result');
 });
 test('actual pending render flush closes gap; failed render keeps warning and other entries',()=>{
  for(const fail of [false,true]){const h=fixture();h.installRendering();h.event(1);h.event(3);h.c.pendingRenders.add({fail});h.c.pendingRenders.add({fail:false});h.event(2);
   assert.equal(h.state().issue.kind,'recoverable');const old=h.c.console;h.c.console={...console,error:()=>{}};h.run('flushPendingRenders()');h.c.console=old;
   assert.equal(h.state().issue?.kind,fail?'apply':undefined);if(!fail)assert.equal(h.state().mode,'result');assert.ok(h.counts().rendered>=4);}
 });
 test('nested flush cannot announce success before the enclosing event returns',()=>{
  const h=fixture();h.installRendering();h.event(1);h.event(3);let first=true,mode;
  h.c.handleIncomingEvent=()=>{if(first){first=false;h.c.pendingRenders.add({});h.run('flushPendingRenders()');mode=h.state().mode;throw Error('SYNTHETIC_LATE_HANDLER_FAILURE');}};
  h.event(2);assert.equal(mode,'gap');assert.equal(h.state().issue.kind,'apply');assert.equal(h.state().mode,'gap');
 });
 test('invalid managed instance exposes context problem and does not open child stream',()=>{const h=fixture({managed:true,instance:'invalid'});assert.equal(h.state().issue.kind,'managed');assert.match(h.text.textContent,/上下文无效/);assert.equal(h.counts().connections,0);});
 {
  const h=fixture({managed:true,fail:true}),p={seq:'1',agent_id:'synthetic-agent',generation:'b'.repeat(32),epoch:'aa',iframe_instance:'a'.repeat(32),payload:{kind:'synthetic'}};
  await h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:p.iframe_instance,type:'event',agent_id:p.agent_id,generation:p.generation,event:p}});
  assert.equal(h.state().applied,'0');assert.equal(h.replies.at(-1).ok,false);assert.equal(h.replies.at(-1).retryable,false);assert.equal(h.state().issue.kind,'managed');
  console.log('PASS managed render failure retains negative applied and cursor guard');checks++;
 }
 {
  const h=fixture({managed:true,holdManaged:true}),p={seq:'1',agent_id:'synthetic-agent',generation:'b'.repeat(32),epoch:'aa',iframe_instance:'a'.repeat(32),payload:{kind:'synthetic'}};
  const first=h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:p.iframe_instance,type:'event',agent_id:p.agent_id,generation:p.generation,event:p}});
  await h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:p.iframe_instance,type:'stopped',reason:'FIXED_BROKER_REASON'}});
  h.run(`managedApplyPending.reject(Error('SYNTHETIC_LATE_FAILURE'))`);await first;
  assert.match(h.text.textContent,/FIXED_BROKER_REASON/);assert.equal(h.state().applied,'0');assert.equal(h.replies.at(-1).ok,false);
  console.log('PASS late apply failure cannot erase the trusted stopped reason');checks++;
 }
 console.log('ALL '+checks+' integrity recovery checks PASS (offline only)');
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
