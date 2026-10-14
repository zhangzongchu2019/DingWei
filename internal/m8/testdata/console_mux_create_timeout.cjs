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

function aborted(signal){return new Promise((resolve,reject)=>{if(signal.aborted)return reject(signal.reason);signal.addEventListener('abort',()=>reject(signal.reason),{once:true});});}
(async()=>{
 for(const body of [false,true]){
  let committed=null,attempts=0;
  const h=harness(service((r,calls,signal)=>{
   if(r.method!=='POST')return null;
   attempts++;
   if(!committed){committed=JSON.stringify(r.body);const meta={feed_id:'created',revision:1,subscriptions:r.body.subscriptions.map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:'1'}))};
    if(body)return {code:201,bodyWait:()=>aborted(signal)};
    return aborted(signal);
   }
   if(JSON.stringify(r.body)!==committed)return err(null,'console_instance_exists',409);
   return {code:201,body:{feed_id:'created',revision:1,subscriptions:r.body.subscriptions.map(s=>({...s,agent_id:'id-'+s.full_name,generation:'g-'+s.full_name,replay_round:'1'}))}};
  }));
  const a=h.register('one.owner');await settle();h.fire(1500);await settle();
  // Register during the uncertain creation; the frozen replay must not expand.
  h.register('two.owner');h.fire(500);await settle();
  assert.equal(attempts,2);const posts=h.calls.filter(c=>c.method==='POST');assert.equal(JSON.stringify(posts[0].body),JSON.stringify(posts[1].body));
  assert.equal(h.state().seats.get(a.name).stopped,false);assert.equal(h.streams.length,1);
  assert.deepEqual(Array.from(h.calls.filter(c=>c.method==='PUT').at(-1).body.subscriptions,s=>s.full_name),['one.owner','two.owner']);
  assert.equal(a.pane.draft,'draft-one.owner');assert.equal(h.state().seats.get(a.name).applied,'');
  console.log('PASS committed create with lost '+(body?'JSON body':'response')+' replays exact request; changed seats reconciled by PUT');
 }
 {
  let fail=true;
  const h=harness(service((r,calls,signal)=>r.method==='POST'&&fail?aborted(signal):null));const a=h.register('one.owner');await settle();
  for(let i=0;i<3;i++){h.fire(1500);await settle();if(i<2){h.fire(500);await settle();}}
  const posts=h.calls.filter(c=>c.method==='POST');assert.equal(posts.length,3);assert.equal(new Set(posts.map(r=>JSON.stringify(r.body))).size,1);
  assert.equal(h.state().seats.get(a.name).stopped,true);assert.match(a.pane.dataset.streamError,/请求超时/);assert.doesNotMatch(a.pane.dataset.streamError,/aborted without reason/);assert.equal(h.streams.length,0);
  fail=false;h.window.DWConsoleMux.remove(a.name);h.register(a.name);await settle();
  assert.equal(JSON.stringify(h.calls.filter(c=>c.method==='POST').at(-1).body),JSON.stringify(posts[0].body));assert.equal(h.state().seats.get(a.name).stopped,false);assert.equal(h.streams.length,1);
  console.log('PASS three-attempt bound, explicit timeout, exact pending request retained across explicit seat reopen');
 }
 for(const code of [401,403,409]){
  const reason=code===409?'console_in_use':'authorization_unavailable';const h=harness(service(r=>r.method==='POST'?err(null,reason,code):null));const a=h.register('one.owner');await settle();
  assert.equal(h.calls.filter(c=>c.method==='POST').length,1);assert.equal(h.state().seats.get(a.name).stopped,true);assert.equal(h.streams.length,0);
  if(code===409)assert.match(a.pane.dataset.streamError,/另一个 Console/);
  console.log('PASS definitive '+code+' no uncertain-create retry, authority fail-closed');
 }
 {
  const h=harness(service(r=>r.method==='POST'?err(null,'console_instance_exists',409):null));const a=h.register('one.owner');await settle();
  assert.match(a.pane.dataset.streamError,/当前 Console 实例/);assert.doesNotMatch(a.pane.dataset.streamError,/另一个 Console/);assert.equal(h.calls.filter(c=>c.method==='POST').length,1);console.log('PASS instance conflict distinguished from account slot');
 }
 {
  const h=harness(service((r,calls,signal)=>r.method==='POST'?aborted(signal):null));h.register('one.owner');await settle();h.fire(1500);await settle();h.pagehide();h.fire(500);await settle();assert.equal(h.calls.filter(c=>c.method==='POST').length,1);assert.equal(h.streams.length,0);console.log('PASS pagehide cancels pending retry without opening transport');
 }
 console.log('ALL create-response uncertainty checks PASS (VM/mock only; no HTTP/browser/PG)');
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
