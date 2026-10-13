// Executes the unmodified broker program in a VM with mock fetch/DOM/timers.
// No listener, browser, credentials, or actual HTTP. Optional source selects
// the exact parent for retained pre-fix counterexamples.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const file=process.env.BROKER_SOURCE||require('node:path').join(__dirname,'../console_mux_broker.js');
const source=fs.readFileSync(file,'utf8');
const settle=async()=>{for(let i=0;i<20;i++)await new Promise(r=>setImmediate(r));};
function harness(handler){
 const listeners={},calls=[],streams=[],nodes=new Map(),timers=new Map();let nonce=0,clock=0;
 const document={body:{append(el){nodes.set(el.id,el);}},getElementById:id=>nodes.get(id),createElement:()=>({setAttribute(){},style:{},append(){}})};
 class EventSource {constructor(url){this.url=url;this.closed=false;streams.push(this);}close(){this.closed=true;}}
 const box={TextEncoder,Uint8Array,BigInt,AbortController,Date,document,location:{origin:'https://console.invalid',replace(){throw Error('unexpected navigation');}},confirm:()=>false,queueMicrotask,EventSource,crypto:{getRandomValues(a){a.fill(++nonce);return a;}},setTimeout(fn,ms){timers.set(++clock,{fn,ms});return clock;},clearTimeout(id){timers.delete(id);},setInterval(){return 0;},clearInterval(){},fetch:async(path,opts)=>{const req={path,method:opts.method,body:opts.body?JSON.parse(opts.body):null};calls.push(req);const out=handler(req,calls);return {ok:out.code>=200&&out.code<300,status:out.code,json:async()=>out.body};}};
 box.window={addEventListener(name,fn){listeners[name]=fn;}};
 // Test-only VM export; product code is unchanged and exposes no internals.
 vm.runInNewContext(source.replace(/\}\)\(\);\s*$/,"window.__inspect=()=>({seats,feed,source,totalCount,totalBytes});})();"),box,{filename:file});
 function register(name){const messages=[],pane={dataset:{},src:'preserved',draft:'draft-'+name,scrollTop:42,contentWindow:{postMessage(d){messages.push(d);}}};const instance=box.window.DWConsoleMux.register(name,pane);return {name,pane,instance,messages};}
 const state=()=>box.window.__inspect();
 function frame(seat,type,extra={}){const s=state().seats.get(seat.name);const p={type,full_name:s.name,agent_id:s.agent,iframe_instance:s.instance,generation:s.generation,replay_round:s.round,...extra};state().source.onmessage({data:JSON.stringify(p)});}
 function ready(seat){listeners.message({origin:box.location.origin,source:seat.pane.contentWindow,data:{protocol:'dw-console-mux/v1',type:'ready',iframe_instance:seat.instance}});}
 function applied(seat){const s=state().seats.get(seat.name),p=s.inflight.p;listeners.message({origin:box.location.origin,source:seat.pane.contentWindow,data:{protocol:'dw-console-mux/v1',type:'applied',iframe_instance:s.instance,agent_id:s.agent,generation:s.generation,delivery_id:p.epoch+'-'+p.seq,ok:true}});}
 return {register,state,calls,streams,frame,ready,applied,timers,window:box.window};
}
function service(extra=()=>null){let revision=0;return (r,calls)=>{const fail=extra(r,calls);if(fail)return fail;if(r.path.endsWith('/event-context'))return {code:200,body:{auth_context:'synthetic-context'}};if(r.method==='GET')return {code:200,body:{feed_id:'feed',revision,subscriptions:[]}};return {code:r.method==='POST'?201:200,body:{feed_id:'feed',revision:++revision,subscriptions:(r.body.subscriptions||[]).map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:'1'}))}};};}
const err=(name,cause='agent_offline',code=404)=>({code,body:{error:cause,...(name?{full_name:name}:{})}});
const controls=h=>h.calls.filter(c=>['POST','PUT'].includes(c.method));
(async()=>{
 for(const method of ['POST','PUT'])for(const cause of ['agent_offline','not_found']){
  let unavailable=method==='POST';const h=harness(service(r=>r.method===method&&unavailable&&r.body.subscriptions.some(s=>s.full_name==='bad.owner')?err('bad.owner',cause):null));
  const good=h.register('good.owner');let bad;
  if(method==='PUT'){await settle();unavailable=true;bad=h.register('bad.owner');}else bad=h.register('bad.owner');
  await settle();
  assert.equal(h.state().seats.get(good.name).stopped,false,'unrelated seat stopped');assert.equal(h.state().seats.get(bad.name).stopped,true);
  assert.equal(controls(h).filter(c=>c.method==='POST').length,method==='PUT'?1:2,'scoped PUT must not recreate feed');
  assert.deepEqual(Array.from(controls(h).at(-1).body.subscriptions,s=>s.full_name),['good.owner']);
  assert.equal(bad.pane.draft,'draft-bad.owner');assert.equal(bad.pane.src,'preserved');assert.match(bad.pane.dataset.streamError,/事件连接暂不可用/);
  assert.doesNotMatch(bad.pane.dataset.streamError,/所有|模型/);
  h.ready(good);h.frame(good,'replay_begin',{epoch:'epoch',floor:'1',H:'1',empty:false});h.frame(good,'event',{epoch:'epoch',seq:'1',payload:{kind:'test'}});
  assert.equal(good.messages.at(-1).type,'event');h.applied(good);assert.equal(h.state().seats.get(good.name).applied,'1');
  console.log('PASS '+method+' '+cause+' exact seat isolation + live continuation + draft');
 }
 // A scoped PUT failure must preserve a different seat's queued payload/cursor
 // and its existing transport; not a feed recreation / replay restart.
 {
  let offline=false;const h=harness(service(r=>r.method==='PUT'&&offline&&r.body.subscriptions.some(s=>s.full_name==='bad.owner')?err('bad.owner'):null));
  const good=h.register('good.owner');await settle();h.ready(good);
  h.frame(good,'replay_begin',{epoch:'epoch',floor:'1',H:'2',empty:false});h.frame(good,'event',{epoch:'epoch',seq:'1',payload:{kind:'first'}});h.applied(good);
  h.frame(good,'event',{epoch:'epoch',seq:'2',payload:{kind:'queued'}});const before=h.state(),seat=before.seats.get(good.name),item=seat.inflight;
  offline=true;h.register('bad.owner');await settle();
  assert.equal(h.state().source,before.source);assert.equal(seat.inflight,item);assert.equal(seat.queue[0],item);assert.equal(seat.applied,'1');assert.equal(seat.count,1);assert.equal(h.state().totalBytes,item.bytes);assert.equal(good.pane.scrollTop,42);assert.equal(good.pane.draft,'draft-good.owner');
  h.applied(good);assert.equal(seat.applied,'2');assert.equal(h.state().totalCount,0);console.log('PASS other-seat inflight/queue/credits/cursor/scroll/draft preserved');
 }
 for(const method of ['POST','PUT']){
  let offline=method==='POST';const h=harness(service(r=>offline&&r.method===method&&r.body.subscriptions.length?err(r.body.subscriptions[0].full_name):null));
  if(method==='PUT'){h.register('preexisting.owner');await settle();h.window.DWConsoleMux.remove('preexisting.owner');await settle();offline=true;}
  const start=controls(h).length;for(let i=0;i<8;i++)h.register('bad'+i+'.owner');await settle();
  assert.equal([...h.state().seats.values()].filter(s=>s.stopped).length,8);
  const n=controls(h).length-start;assert.equal(n,method==='POST'?8:9);await settle();assert.equal(controls(h).length-start,n);assert.ok(h.streams.every(s=>s.closed));
  console.log('PASS '+method+' 8 unavailable seats converge, max 8 failures + optional empty PUT, no loop');
 }
 for(const code of [404,410]){
  let lost=false,failed=false;const h=harness(service(r=>r.method==='PUT'&&lost&&!failed?(failed=true,err(null,'not_found',code)):null));h.register('one.owner');await settle();lost=true;h.register('two.owner');await settle();
  assert.equal(controls(h).filter(c=>c.method==='POST').length,2);assert.ok([...h.state().seats.values()].every(s=>!s.stopped));console.log('PASS unscoped '+code+' retains feed recreation');
 }
 for(const method of ['POST','PUT'])for(const code of [401,403,409]){
  let fail=method==='POST';const h=harness(service(r=>r.method===method&&fail?err(null,code===409?'slot_occupied':'authorization_unavailable',code):null));h.register('one.owner');h.register('two.owner');if(method==='PUT'){h.register('remove.owner');await settle();fail=true;h.window.DWConsoleMux.remove('remove.owner');}await settle();assert.ok([...h.state().seats.values()].every(s=>s.stopped));assert.ok(h.streams.every(s=>s.closed));console.log('PASS '+method+' '+code+' remains global fail-closed');
 }
 for(const name of ['unknown.owner','good.owner.extra']){
  const h=harness(service(r=>r.method==='POST'?err(name):null));const good=h.register('good.owner');await settle();assert.equal(h.state().seats.get(good.name).stopped,false);assert.equal(controls(h).length,1);console.log('PASS unrecognized scope '+name+' cannot stop another seat or retry');
 }
 {
  let conflict=false,hit=false;const h=harness(service(r=>r.method==='PUT'&&conflict&&!hit?(hit=true,err(null,'revision_conflict',409)):null));h.register('one.owner');await settle();conflict=true;h.register('two.owner');await settle();assert.equal(h.calls.filter(r=>r.method==='GET'&&r.path.includes('/event-feeds/')).length,1);assert.ok([...h.state().seats.values()].every(s=>!s.stopped));console.log('PASS revision conflict metadata reconciliation unchanged');
 }

 for(const code of [401,403]){
  const h=harness(service(r=>r.method==='POST'?err('one.owner','agent_offline',code):null));h.register('one.owner');h.register('two.owner');await settle();assert.ok([...h.state().seats.values()].every(s=>s.stopped));console.log('PASS scoped '+code+' cannot enter per-seat 404 branch');
 }
 {
  let fail=false;const h=harness(service(r=>r.method==='PUT'&&fail&&r.body.subscriptions.some(s=>s.full_name==='bad.owner')?err('bad.owner','unrecognized_reason'):null));h.register('one.owner');await settle();fail=true;h.register('bad.owner');await settle();assert.equal(controls(h).filter(c=>c.method==='POST').length,1);assert.equal(h.state().seats.get('one.owner').stopped,false);console.log('PASS other scoped PUT 404 is thrown, not feed-missing recreation');
 }
 console.log('ALL scoped-unavailable broker checks PASS (mock only)');
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
