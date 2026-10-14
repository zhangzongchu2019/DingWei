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
 const box={TextEncoder,Uint8Array,BigInt,AbortController,Date,document,location:{origin:'https://console.invalid',replace(){throw Error('unexpected navigation');}},confirm:()=>false,queueMicrotask,EventSource,crypto:{getRandomValues(a){a.fill(++nonce);return a;}},setTimeout(fn,ms){timers.set(++clock,{fn,ms});return clock;},clearTimeout(id){timers.delete(id);},setInterval(){return 0;},clearInterval(){},fetch:async(path,opts)=>{const req={path,method:opts.method,body:opts.body?JSON.parse(opts.body):null};calls.push(req);const out=await handler(req,calls,opts.signal);return {ok:out.code>=200&&out.code<300,status:out.code,json:async()=>{if(out.bodyWait)return out.bodyWait(opts.signal);return out.body;}};}};
 box.window={addEventListener(name,fn){listeners[name]=fn;}};
 // Test-only VM export; product code is unchanged and exposes no internals.
 vm.runInNewContext(source.replace(/\}\)\(\);\s*$/,"window.__inspect=()=>({seats,feed,source,totalCount,totalBytes});})();"),box,{filename:file});
 function register(name){const messages=[],pane={dataset:{},src:'preserved',draft:'draft-'+name,scrollTop:42,contentWindow:{postMessage(d){messages.push(d);}}};const instance=box.window.DWConsoleMux.register(name,pane);return {name,pane,instance,messages};}
 const state=()=>box.window.__inspect();
 function frame(seat,type,extra={}){const s=state().seats.get(seat.name);const p={type,full_name:s.name,agent_id:s.agent,iframe_instance:s.instance,generation:s.generation,replay_round:s.round,...extra};state().source.onmessage({data:JSON.stringify(p)});}
 function ready(seat){listeners.message({origin:box.location.origin,source:seat.pane.contentWindow,data:{protocol:'dw-console-mux/v1',type:'ready',iframe_instance:seat.instance}});}
 function applied(seat){const s=state().seats.get(seat.name),p=s.inflight.p;listeners.message({origin:box.location.origin,source:seat.pane.contentWindow,data:{protocol:'dw-console-mux/v1',type:'applied',iframe_instance:s.instance,agent_id:s.agent,generation:s.generation,delivery_id:p.epoch+'-'+p.seq,ok:true}});}
 return {register,state,calls,streams,frame,ready,applied,timers,window:box.window,pagehide:()=>listeners.pagehide(),fire(ms){const hit=[...timers].find(([id,t])=>t.ms===ms);assert.ok(hit,'missing timer '+ms);timers.delete(hit[0]);hit[1].fn();}};
}
function service(extra=()=>null){let revision=0;return (r,calls,signal)=>{const fail=extra(r,calls,signal);if(fail)return fail;if(r.path.endsWith('/event-context'))return {code:200,body:{auth_context:'synthetic-context'}};if(r.method==='GET')return {code:200,body:{feed_id:'feed',revision,subscriptions:[]}};return {code:r.method==='POST'?201:200,body:{feed_id:'feed',revision:++revision,subscriptions:(r.body.subscriptions||[]).map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:'1'}))}};};}
const err=(name,cause='agent_offline',code=404)=>({code,body:{error:cause,...(name?{full_name:name}:{})}});
const controls=h=>h.calls.filter(c=>['POST','PUT'].includes(c.method));


function begin(h,s){h.frame(s,'replay_begin',{epoch:'epoch',floor:'1',H:'0',empty:true});}
const refillCalls=h=>h.calls.filter(r=>r.method==='PUT'&&(r.body.refill||[]).length);
(async()=>{
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();h.ready(a);begin(h,a);
  for(let n=1;n<=8;n++){h.frame(a,'watermark',{epoch:'epoch',H:String(n)});h.frame(a,'event',{epoch:'epoch',seq:String(n),payload:{kind:'synthetic'}});h.applied(a);h.fire(2000);await settle();}
  assert.equal(refillCalls(h).length,0);assert.equal(h.state().seats.get(a.name).refills.length,0);assert.equal(h.state().seats.get(a.name).stopped,false);assert.equal(h.state().seats.get(a.name).applied,'8');assert.equal(a.pane.draft,'draft-recv.owner');console.log('PASS eight caught-up watermarks spend zero refill budget');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();begin(h,a);
  for(let n=1;n<=4;n++){h.frame(a,'watermark',{epoch:'epoch',H:String(n)});h.fire(2000);await settle();}
  assert.equal(refillCalls(h).length,3);assert.equal(h.state().seats.get(a.name).stopped,true);assert.match(a.pane.dataset.streamError,/补收次数已达上限/);assert.equal(h.state().seats.get(a.name).applied,'0');console.log('PASS persistent watermark gap retains three-refill cap and cursor');
 }
 for(const mode of ['inflight','not-ready']){
  const h=harness(service());const a=h.register('recv.owner');await settle();if(mode==='inflight')h.ready(a);begin(h,a);
  h.frame(a,'watermark',{epoch:'epoch',H:'1'});h.frame(a,'event',{epoch:'epoch',seq:'1',payload:{kind:'synthetic'}});
  const s=h.state().seats.get(a.name),q=s.queue[0],inflight=s.inflight;
  h.fire(2000);await settle();assert.equal(refillCalls(h).length,0);assert.equal(s.queue[0],q);assert.equal(s.inflight,inflight);assert.equal(s.refills.length,0);assert.equal(s.applied,'0');console.log('PASS '+mode+' next event preserved without refill/drop/ACK advance');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();h.ready(a);begin(h,a);h.frame(a,'event',{epoch:'epoch',seq:'2',payload:{kind:'synthetic'}});h.fire(2000);await settle();assert.equal(refillCalls(h).length,1);assert.equal(h.state().seats.get(a.name).applied,'0');console.log('PASS genuine sequence hole still requests refill without advancing cursor');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();begin(h,a);h.frame(a,'replay_end',{epoch:'epoch',H:'1'});h.frame(a,'event',{epoch:'epoch',seq:'1',payload:{kind:'synthetic'}});h.ready(a);h.applied(a);h.fire(2000);await settle();assert.equal(refillCalls(h).length,0);assert.equal(h.state().seats.get(a.name).refills.length,0);console.log('PASS replay_end gap resolved during delay cancels refill');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();begin(h,a);h.frame(a,'watermark',{epoch:'epoch',H:'1'});h.window.DWConsoleMux.remove(a.name);const fresh=h.register(a.name);await settle();h.fire(2000);await settle();assert.equal(refillCalls(h).length,0);assert.equal(h.state().seats.get(fresh.name).stopped,false);assert.equal(h.state().seats.get(fresh.name).refills.length,0);console.log('PASS old seat timer cannot mutate replacement seat');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();begin(h,a);h.frame(a,'watermark',{epoch:'epoch',H:'1'});h.pagehide();h.fire(2000);await settle();assert.equal(refillCalls(h).length,0);console.log('PASS pagehide prevents queued refill control');
 }
 {
  let release;const h=harness(service(r=>r.method==='PUT'&&!r.body.refill.length?new Promise(resolve=>{release=()=>resolve({code:200,body:{feed_id:'feed',revision:2,subscriptions:r.body.subscriptions.map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:'1'}))}});}):null));
  const a=h.register('recv.owner');await settle();h.ready(a);begin(h,a);h.register('other.owner');await settle();assert.ok(release);
  h.frame(a,'watermark',{epoch:'epoch',H:'1'});h.fire(2000);await settle();h.frame(a,'event',{epoch:'epoch',seq:'1',payload:{kind:'synthetic'}});h.applied(a);release();await settle();assert.equal(refillCalls(h).length,0);assert.equal(h.state().seats.get(a.name).refills.length,0);console.log('PASS serial backlog rechecks applied cursor when control finally runs');
 }

 {
  let round='1';const h=harness(service(r=>r.method==='PUT'?{code:200,body:{feed_id:'feed',revision:2,subscriptions:r.body.subscriptions.map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:round}))}}:null));
  const a=h.register('recv.owner');await settle();begin(h,a);h.frame(a,'watermark',{epoch:'epoch',H:'1'});round='2';h.register('other.owner');await settle();assert.equal(h.state().seats.get(a.name).round,'2');h.fire(2000);await settle();assert.equal(refillCalls(h).length,0);console.log('PASS stale replay-round timer cannot issue refill');
 }
 {
  const h=harness(service());const a=h.register('recv.owner');await settle();h.ready(a);begin(h,a);h.frame(a,'watermark',{epoch:'epoch',H:'1'});h.frame(a,'event',{epoch:'epoch',seq:'1',payload:{kind:'synthetic'}});
  h.fire(2000);await settle();h.fire(2000);await settle();h.fire(2000);await settle();const seat=h.state().seats.get(a.name);assert.equal(seat.stopped,true);assert.match(a.pane.dataset.streamError,/ACK 超时/);assert.equal(seat.applied,'0');assert.equal(refillCalls(h).length,0);console.log('PASS missing applied ACK retains two-delivery stop and unchanged cursor');
 }
 console.log('ALL refill recheck cases PASS (VM/mock only)');
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
