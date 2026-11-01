// Synthetic field-coordinate comparison; no production connection or message.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process');
const {launchChromium,startRendererServer}=require('../../../view-service/renderer-regression-selfhost');
const ids=[['dw-dlv_a073022f53991cacfa695447539a0d75','57141700617d6e3cdf06a24bbb99eb1e'],['dw-dlv_cce9cd0461277a978369e65853196aae','dcb08183123b29b98571393ee46053b1'],['dw-dlv_2ff330b5ed10f5de7b931c2ffc4860f1','3a1ea10189dc6000ea8fe203a4e02f5f']];
const claude=JSON.parse(fs.readFileSync(path.join(__dirname,'raw/claude-014-fixture.json'),'utf8')).event;
const baseline=cp.execFileSync('git',['show','3fd26ba67ebc90db4f35449e6968c4f22f89666b:view-service/renderer-canary-sse.html'],{encoding:'utf8'});
(async()=>{const server=await startRendererServer();let browser;let assertions=0;
function check(v,msg){assertions++;assert.ok(v,msg);}
try{browser=await launchChromium();
for(const fixed of [false,true]){
 const pages=[]; const context=await browser.newContext();
 for(let i=0;i<2;i++){
  const page=await context.newPage();pages.push(page);
  if(!fixed)await page.route(server.url+'*',r=>r.fulfill({contentType:'text/html',body:baseline}));
  await page.route('**/status',r=>r.fulfill({contentType:'application/json',body:JSON.stringify({session:'ts3opus55.zzc',state:'idle',readonly:true,can_write:false})}));
  await page.route('**/events',r=>r.fulfill({contentType:'text/event-stream',body:''}));
  await page.goto(server.url+'?session=ts3opus55.zzc');
  await page.evaluate(()=>{closeEventStream();resetConversation();finishTimelineReplay(false);
   window.fieldFeed=(ev,id)=>{if(!admitEventId(id))return false;handleIncomingEvent(ev);return true;};
   window.fieldInput=(id,message)=>({kind:'input_posted',schema_version:'delivery.v2',message_id:message,delivery_id:id,payload:'synthetic owner input',stage:'hub_received',delivery:{role:'primary'},cancellable:true});
   window.fieldUpdate=(id,message,stage)=>({kind:'delivery_update',schema_version:'delivery.v2',message_id:message,observed_delivery_id:id,delivery_stage:stage,ack_protocol:2,processing_mode:'driver',cancellable:false,turn_outcome:stage==='turn_processed'?'completed':''});
   window.fieldState=id=>{const key=c1DeliveryCards.get(deliveryMergeKey(id)),e=c1InputCards.get(key);return {status:e?.stageEl?.textContent||'',key:key||'',pending:[...c1PendingUpdates.keys()],waiting:waitingDeliveries.size,cursor:lastEventEpoch+'-'+lastContiguousSeq};};
  });
 }
 // Same session in two independent renderer documents; only event order differs.
 const [id,message]=ids[0];
 const first=await pages[0].evaluate(({id,message,claude})=>{fieldFeed(fieldInput(id,message),'aa-1');fieldFeed(claude,'aa-2');const url=eventsStreamUrl();const duplicate=fieldFeed(claude,'aa-2');fieldFeed(fieldUpdate(id,message,'cli_injected'),'aa-3');fieldFeed(fieldUpdate(id,message,'turn_processed'),'aa-4');return {...fieldState(id),url,duplicate};},{id,message,claude});
 const second=await pages[1].evaluate(({id,message,claude})=>{fieldFeed(fieldInput(id,message),'aa-1');fieldFeed(fieldUpdate(id,message,'cli_injected'),'aa-2');fieldFeed(claude,'aa-3');fieldFeed(fieldUpdate(id,message,'turn_processed'),'aa-4');return fieldState(id);},{id,message,claude});
 console.log(JSON.stringify({fixed,scenario:'same-session-two-documents-and-resume-cursor',first,second}));
 check(first.status===(fixed?'回合已结束（完成）':''),'early-output comparison');
 check(second.status==='回合已结束（完成）','ACK-first unaffected control');
 check(first.url.includes('after=aa-2'),'per-document explicit after cursor preserves contiguous sequence');
 check(first.duplicate===false,'duplicate sequenced event is rejected');
 check(second.cursor==='aa-4','second document advances its own cursor');
 for(const [id,message] of ids.slice(1)){
  const state=await pages[0].evaluate(({id,message,claude})=>{resetConversation();finishTimelineReplay(false);handleIncomingEvent(fieldInput(id,message));handleIncomingEvent({...claude,delivery_id:id});handleIncomingEvent(fieldUpdate(id,message,'cli_injected'));handleIncomingEvent(fieldUpdate(id,message,'turn_processed'));return fieldState(id);},{id,message,claude});
  console.log(JSON.stringify({fixed,scenario:'consistent-envelope-and-source-message-coordinate',id,message,state}));
  check(state.status===(fixed?'回合已结束（完成）':''),'exact field-coordinate ordering reproduction');
 }
 // A retained suffix with no input event cannot prove input identity/body.
 const missing=await pages[1].evaluate(({id,message,claude})=>{resetConversation();finishTimelineReplay(false);handleIncomingEvent(claude);handleIncomingEvent(fieldUpdate(id,message,'turn_processed'));return fieldState(id);},{id,message,claude});
 console.log(JSON.stringify({fixed,scenario:'suffix-without-input-posted',missing}));
 check(missing.status==='','do not invent missing canonical primary from output or ledger-like update');
 for(const page of pages)await page.close(); await context.close();
}
console.log(JSON.stringify({assertions,failures:0,scope:'Synthetic Chrome/cursor-functions/two-documents; no actual network reconnect, original SSE trace, or field attribution'}));
}finally{if(browser)await browser.close();await server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
