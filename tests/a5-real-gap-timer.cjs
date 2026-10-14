// Execute the integrated production connection, integrity and sequence/gap paths.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert/strict');
const {createRequire}=require('module');
const base=path.resolve(__dirname,'../internal/m8/testdata/renderer_integrity_recovery.cjs');
const renderer=path.resolve(process.env.A5_SOURCE||path.join(__dirname,'../view-service/renderer-canary-sse.html'));
const text=fs.readFileSync(base,'utf8'),end=text.indexOf('let checks=0;');assert(end>0);
const c={URLSearchParams,require:createRequire(base),__dirname:path.dirname(base),console,process:{argv:['node',base,renderer]}};
vm.createContext(c);vm.runInContext(text.slice(0,end),c);const fixture=vm.runInContext('fixture',c);
let count=0;function test(name,fn){fn();console.log('PASS '+name);count++;}
function queued(){const h=fixture();h.run('connectEventStream()');h.es[0].onopen();h.event(1);h.event(3);h.timer(2000);assert(h.c.reconnectTimer);assert.equal(h.state().charged,0);return h;}
test('1,3,queued refill,4 preserves unresolved-gap reconnect',()=>{const h=queued(),timer=h.c.reconnectTimer;h.event(4);assert.equal(h.c.reconnectTimer,timer);assert(h.timers.has(timer));assert.equal(h.state().above,2);assert.equal(h.state().after,1);assert.equal(h.state().charged,0);h.timer(1000);assert.equal(h.counts().connections,2);assert.equal(h.state().charged,1);assert.match(h.es[1].url,/after=aa-1/);});
test('continuous jumps preserve one queued refill and charge only at execution',()=>{const h=queued(),timer=h.c.reconnectTimer;for(const n of [4,5,6,7])h.event(n);assert.equal(h.c.reconnectTimer,timer);assert.equal(h.state().charged,0);h.timer(1000);assert.equal(h.counts().connections,2);assert.equal(h.state().charged,1);});
test('actual missing event preserves original final check and consumes no refill',()=>{const h=queued(),timer=h.c.reconnectTimer;h.event(2);assert.equal(h.state().above,0);assert.equal(h.state().after,3);assert.equal(h.c.reconnectTimer,timer);h.timer(1000);assert.equal(h.counts().connections,1);assert.equal(h.state().charged,0);assert.equal(h.c.reconnectTimer,null);});
test('transport open cannot cancel queued gap refill',()=>{const h=queued(),timer=h.c.reconnectTimer;h.es[0].onopen();assert.equal(h.c.reconnectTimer,timer);assert(h.timers.has(timer));});
test('duplicate event does not falsely close the gap or cancel recovery',()=>{const h=queued(),timer=h.c.reconnectTimer,rendered=h.counts().rendered;h.event(3);assert.equal(h.c.reconnectTimer,timer);assert.equal(h.counts().rendered,rendered);assert.equal(h.state().above,1);});
test('ordinary legacy success still clears ordinary reconnect and retry count',()=>{const h=fixture();h.run('connectEventStream();scheduleReconnect()');h.es[0].onmessage({lastEventId:'',data:'{}'});assert.equal(h.c.reconnectTimer,null);assert.equal(h.c.reconnectAttempt,0);assert.equal(h.c.streamHasAcceptedEvent,true);});
test('connected transport with pending gap shows waiting then clock, never stuck reconnect',()=>{
 const h=queued(),timer=h.c.reconnectTimer,classes=new Set();
 h.c.clockStatus={title:'',classList:{add:x=>classes.add(x),remove:x=>classes.delete(x)}};
 h.c.stats.statusDatetimeText='fixture-clock';h.c.stats.statusDatetimeMs=0;
 const source=fs.readFileSync(renderer,'utf8'),a=source.indexOf('    function updateLocalTime()'),b=source.indexOf('    function formatMem(',a);assert(a>=0&&b>a);
 vm.runInContext(source.slice(a,b),h.c);h.run('renderConnectionState()');
 assert.equal(h.c.localTime.textContent,'已连接，等待事件');assert(!classes.has('offline'));assert(!h.c.localTime.textContent.includes('断线重连'));
 h.event(4);assert.equal(h.c.reconnectTimer,timer);assert.equal(h.c.reconnectAttempt,0);assert.equal(h.c.localTime.textContent,'fixture-clock');assert(!classes.has('offline'));
 h.run('updateLocalTime()');assert.equal(h.c.localTime.textContent,'fixture-clock');assert.equal(h.state().issue.kind,'recoverable');assert(h.timers.has(timer));
});
console.log('TOTAL '+count+' real integrated gap-timer observations');
