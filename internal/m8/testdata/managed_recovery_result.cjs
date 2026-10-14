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
 {const h=harness(service());const seat=h.register('one.owner');await settle();h.ready(seat);h.frame(seat,'replay_begin',{epoch:'epoch',floor:'1',H:'3',empty:false});h.frame(seat,'event',{epoch:'epoch',seq:'1',payload:{kind:'one'}});h.applied(seat);
  h.frame(seat,'event',{epoch:'epoch',seq:'3',payload:{kind:'three'}});h.frame(seat,'event',{epoch:'epoch',seq:'2',payload:{kind:'two'}});
  assert.equal(h.state().seats.get(seat.name).applied,'1');
  const timer=[...h.timers].find(([id,t])=>t.ms===2000);assert(timer);h.timers.delete(timer[0]);timer[1].fn();await settle();
  const req=controls(h).at(-1).body;assert.deepEqual(Array.from(req.refill),['one.owner']);assert.equal(req.subscriptions[0].after,'epoch-1');
  h.frame(seat,'replay_begin',{epoch:'epoch',floor:'1',H:'3',empty:false});
  for(const seq of ['2','3']){h.frame(seat,'event',{epoch:'epoch',seq,payload:{kind:'replayed'}});h.applied(seat);}
  h.frame(seat,'replay_end',{epoch:'epoch',H:'3'});const state=h.state().seats.get(seat.name);
  assert.equal(state.applied,'3');assert.equal(state.count,0);assert.equal(state.stopped,false);assert.equal(seat.pane.draft,'draft-one.owner');assert.equal(seat.pane.scrollTop,42);console.log('PASS managed actual refill from contiguous cursor, replay+positive applied reaches H without body/draft reset');
 }
 {const h=harness(service());const seat=h.register('one.owner');await settle();h.ready(seat);h.frame(seat,'replay_begin',{epoch:'epoch',floor:'1',H:'2',empty:false});h.frame(seat,'event',{epoch:'epoch',seq:'1',payload:{kind:'one'}});
  assert.equal(h.state().seats.get(seat.name).applied,'0');h.applied(seat);h.frame(seat,'replay_end',{epoch:'epoch',H:'2'});assert.equal(h.state().seats.get(seat.name).applied,'1');assert.equal(h.state().seats.get(seat.name).stopped,false);console.log('PASS replay_end without missing event does not fabricate applied H or recovery');
 }
 {const h=harness(service());const bad=h.register('bad.owner'),good=h.register('good.owner');await settle();h.ready(bad);h.ready(good);h.frame(bad,'gap',{cause:'history_unavailable'});await settle();const s=h.state().seats.get(bad.name);
  assert.equal(s.stopped,true);assert.equal(bad.messages.at(-1).type,'stopped');assert.equal(bad.pane.draft,'draft-bad.owner');const n=h.calls.length;await settle();assert.equal(h.calls.length,n);assert.equal(h.state().seats.get(good.name).stopped,false);console.log('PASS managed history_unavailable remains stopped with explicit retained-suffix-only reset, no auto-reopen or other-seat stop');
 }
 console.log('TOTAL 3 new managed recovery observations; no live HTTP/browser');
})().catch(e=>{console.error(e);process.exitCode=1;});
