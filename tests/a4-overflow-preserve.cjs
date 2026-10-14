// Actual renderer callbacks and original integrity fixture; no browser/network.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert/strict');
const {createRequire}=require('module');
const base=path.resolve(__dirname,'../internal/m8/testdata/renderer_integrity_recovery.cjs');
const renderer=path.resolve(process.argv[2]||path.join(__dirname,'../view-service/renderer-canary-sse.html'));
const original=fs.readFileSync(base,'utf8'),end=original.indexOf('let checks=0;');assert.ok(end>0);
const ctx={URLSearchParams,require:createRequire(base),__dirname:path.dirname(base),console,process:{argv:['node',base,renderer]}};
vm.createContext(ctx);vm.runInContext(original.slice(0,end),ctx);const fixture=vm.runInContext('fixture',ctx);
let tests=0;
function setup(){const h=fixture();let resets=0;const body=[];h.c.resetConversation=()=>{resets++;body.length=0;};h.c.handleIncomingEvent=p=>body.push(p.text);h.run('connectEventStream()');h.es.at(-1).onopen();h.es.at(-1).onmessage({lastEventId:'',data:JSON.stringify({text:'already displayed'})});return {h,body,resets:()=>resets};}
function open(h){h.run('streamEverOpened=true;connectEventStream()');h.es.at(-1).onopen();}
function overflow(h){h.run('handleReplayGap({data:\'{"reason":"overflow"}\'})');}
function deliver(h){h.es.at(-1).onmessage({lastEventId:'',data:'{"text":"retained suffix"}'});}
function check(name,fn){fn();tests++;console.log('PASS '+name);}
check('pending legacy clear then overflow then retained event preserves displayed body',()=>{const {h,body,resets}=setup();open(h);assert.equal(h.run('pendingReconnectClear'),true);overflow(h);deliver(h);assert.equal(resets(),0);assert.deepEqual(body,['already displayed','retained suffix']);assert.equal(h.state().issue.kind,'history');assert.equal(h.alert.hidden,false);assert.equal(h.run('pendingReconnectClear'),false);});
check('history before another reconnect cannot arm a destructive clear',()=>{const {h,body,resets}=setup();overflow(h);open(h);assert.equal(h.run('pendingReconnectClear'),false);deliver(h);assert.equal(resets(),0);assert.equal(body[0],'already displayed');assert.equal(h.state().issue.kind,'history');});
check('later reconnect still preserves content after first overflow refill',()=>{const {h,body,resets}=setup();open(h);overflow(h);deliver(h);open(h);deliver(h);assert.equal(resets(),0);assert.equal(body[0],'already displayed');assert.equal(h.state().issue.kind,'history');});
check('normal unsequenced replay still clears once without history loss',()=>{const {h,body,resets}=setup();open(h);assert.equal(h.run('pendingReconnectClear'),true);deliver(h);assert.equal(resets(),1);assert.deepEqual(body,['retained suffix']);assert.equal(h.state().issue,null);deliver(h);assert.equal(resets(),1);});
console.log('TOTAL '+tests+' overflow/legacy composition checks');
