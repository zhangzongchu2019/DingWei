// Offline exact renderer functions; no network, browser session or user payload.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const path=require('node:path');
const html=fs.readFileSync(process.argv[2]||path.join(__dirname,'renderer-canary-sse.html'),'utf8');
const part=(a,b)=>{const start=html.indexOf(a),end=html.indexOf(b,start);assert.ok(start>=0&&end>start);return html.slice(start,end);};
function fixture(stage='hub_received'){
 let renders=0,removed=0,moved=0;const waiting=new Map();
 function node(){let button=null;const text={};return {dataset:{},classList:{add:()=>{}},appendChild:b=>{if(b.className==='waiting-cancel-btn')button=b},addEventListener:()=>{},querySelector:s=>s==='.waiting-text'?text:s==='.waiting-cancel-btn'?button:null,remove:()=>removed++,set innerHTML(v){this.markup=v;button=null;renders++},get innerHTML(){return this.markup}};}
 let now=1000;
 const c={Map,Set,Date:{now:()=>now},window:{PROVENANCE_ICONS_V1:true},document:{createElement:node},waitingDeliveries:waiting,waitingList:{appendChild:()=>{}},waitingArea:{hidden:true},escapeHtml:s=>s,requestAnimationFrame:()=>{},c2TrackTurn:()=>{},replayInProgress:false,timelineAtBottom:false,scheduleTimelineReplayEnd:()=>{},rendered:0,updateMetrics:()=>{},console:{warn:()=>{}},renderEvent:()=>moved++};
 vm.createContext(c);vm.runInContext(part('    const C1_STAGE_ORDER','    // ── B/C/D:')+part('    function dispatchIncomingEvent','    function admitWaitingDelivery'),c);
 c.ev={schema_version:'delivery.v2',kind:'input_posted',message_id:'synthetic-m',delivery_id:'synthetic-d',stage,cancellable:true,payload:'synthetic-input'};
 vm.runInContext('renderWaitingDelivery(normalizeEvent(ev))',c);
 return {waiting,get:()=>({renders,removed,moved}),send:(stage,status='',extra={})=>{now+=1000;c.ev={schema_version:'delivery.v2',kind:'delivery_update',message_id:'synthetic-m',observed_delivery_id:'synthetic-d',delivery_stage:stage,delivery_status:status,...extra};vm.runInContext('dispatchIncomingEvent(ev)',c);}};
}
// Original demonstrated late-update counterexample must become green.
let f=fixture('helper_received'),before=f.get().renders,touched=f.waiting.get('synthetic-d').touched;
f.send('hub_received');assert.equal(f.waiting.get('synthetic-d').event._norm.stage,'helper_received');assert.equal(f.waiting.get('synthetic-d').event._norm.stageIdx,2);assert.equal(f.get().renders,before);assert.equal(f.waiting.get('synthetic-d').touched,touched);console.log('LATE_LOWER: label/index retained; no false progress heartbeat');
f=fixture('generated');f.send('hub_received');f.send('helper_received');let e=f.waiting.get('synthetic-d');assert.equal(e.event._norm.stage,'helper_received');assert.equal(e.event._norm.stageIdx,2);assert.equal(f.get().renders,3);assert.equal(f.waiting.size,1);console.log('FORWARD: generated -> hub -> helper; stage/index synchronized; still waiting');
before=f.get().renders;touched=e.touched;f.send('helper_received');assert.equal(e.event._norm.stageIdx,2);assert.equal(f.get().renders,before);assert.equal(e.touched,touched);console.log('SAME_STAGE: no invented progress or re-render');
f.send('unrecognized_stage');assert.equal(e.event._norm.stage,'helper_received');assert.equal(e.event._norm.stageIdx,2);assert.equal(f.get().renders,before);console.log('UNKNOWN_STAGE: no replacement of confirmed C1 stage');
f=fixture('hub_received');f.send('helper_received','',{reason:'desktop_inbox_saved',cancellable:false});e=f.waiting.get('synthetic-d');assert.equal(e.event._norm.stage,'helper_received');assert.equal(e.event._norm.stageIdx,2);assert.equal(e.event._norm.cancellable,true);assert.equal(e.event._norm.reason,'');assert.equal(f.get().removed,0);assert.doesNotMatch(e.node.innerHTML,/已送达|已注入/);console.log('DESKTOP_CONTRACT_UNCHANGED: no injection, no forced move, metadata gap remains out of scope');
for(const [stage,status] of [['helper_received','cancelled'],['helper_received','failed'],['cli_injected',''],['pty_write',''],['expired',''],['helper_received','dead'],['helper_received','dropped']]){
 f=fixture('hub_received');f.send(stage,status);assert.equal(f.waiting.size,0);assert.deepEqual(f.get(),{renders:1,removed:1,moved:1});console.log('EXISTING_MOVE: '+stage+'/'+(status||'NONE'));
}
console.log('COMPLETE: 12 targeted observations; original renderer functions, synthetic DOM only');
