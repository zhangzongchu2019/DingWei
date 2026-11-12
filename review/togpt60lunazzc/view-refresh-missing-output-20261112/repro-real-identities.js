// Focused history/realtime/pagination replay using prodops-confirmed payload
// identity shapes. Bodies are synthetic; source kind/seq order and IDs are
// documented in the adjacent raw fixture manifest.
const assert=require('assert');
const {execFileSync}=require('child_process');
const fs=require('fs');
const http=require('http');
const path=require('path');
const {chromium}=require('playwright');
const repo=process.env.REPO_ROOT||process.cwd();
const revisions=[
  ['r57-prod-329eb9f9','329eb9f9c45d2d785a0429b01eaa0fa5264fe15d'],
  ['r59-candidate-8528ed8c','8528ed8cf7e43c1bec97102927c92975ed714609'],
];
const renderers=Object.fromEntries(revisions.map(([n,r])=>[n,execFileSync('git',['-C',repo,'show',`${r}:view-service/renderer-canary-sse.html`])]));
const currentKinds=`3907 user_input
3908 user_input
3909 tool_call
3910 assistant_text
3911 tool_call
3912 assistant_text
3913 state_change
3914 user_input
3915 user_input
3916 tool_call
3917 assistant_text
3918 state_change
3919 user_input
3920 user_input
3921 assistant_text
3922 tool_call
3923 tool_call
3924 assistant_text
3925 state_change
3926 user_input
3927 user_input
3928 tool_call
3929 assistant_text
3930 state_change
3931 user_input
3932 user_input
3933 tool_call
3934 assistant_text
3935 state_change
3936 user_input
3937 user_input
3938 tool_call
3939 assistant_text
3940 tool_call
3941 assistant_text
3942 tool_call
3943 assistant_text
3944 state_change
3945 tool_call
3946 tool_call
3947 assistant_text
3948 state_change
3949 user_input
3950 user_input
3951 tool_call
3952 assistant_text
3953 state_change`.split('\n').map(s=>{const [seq,kind]=s.trim().split(/\s+/);return{seq:Number(seq),kind}});
const realTurns=[
  '8d0b99cd394a4e6dbd3c6e87e09594c3','fb86893a830448d7bd5450c307fe8d2e',
  '123f55eecf9a487e976eb3dd540697e6','synthetic-turn-4','synthetic-turn-5','synthetic-turn-6','synthetic-turn-7'];
const roundForSeq=seq=>seq<=3913?1:seq<=3918?2:seq<=3925?3:seq<=3930?4:seq<=3935?5:seq<=3948?6:7;
function eventFor(seq,kind,turn,identity='current'){
  const turnId=turn==='queued'?'':realTurns[turn-1];
  if(kind==='user_input')return{kind,text:`FIXTURE_USER_${seq}`,session_id:'',turn_id:turnId,
    delivery_id:`input-${turnId||`queued-${seq}`}`,data:{lifecycle:turn==='queued'?'queued':'admitted'}};
  const sessionId=identity==='bc19'?'bc19e89c-41f7-4109-ae86-660ac1ef39dc':
    identity==='1be22'?'1be22a87-c900-4378-87b7-2c862ffc7c1d':'83e9a02a-e451-488e-a699-c62fa95ae27e';
  if(kind==='assistant_text')return{kind,text:`FIXTURE_ASSISTANT_${seq}`,session_id:sessionId,turn_id:realTurns[turn-1],delivery_id:`reply-${turn}`};
  if(kind==='tool_call')return{kind,text:`FIXTURE_TOOL_${seq}`,session_id:sessionId,turn_id:realTurns[turn-1],delivery_id:`reply-${turn}`};
  return{kind,text:'',session_id:sessionId,turn_id:realTurns[turn-1],delivery_id:`reply-${turn}`,data:{change:'turn_completed',subtype:'success'}};
}
let inputOrdinal=0;
const currentRows=currentKinds.map(({seq,kind})=>{
  const round=roundForSeq(seq);let event;
  if(kind==='user_input'){inputOrdinal++;event=eventFor(seq,kind,inputOrdinal%2?'queued':round);}
  else event=eventFor(seq,kind,round);
  return{cursor:{generation:'0fda970aaff3905c61684736ec808452',seq},event_id:`current-${seq}`,
    received_at:new Date(Date.UTC(2026,10,12,12,40)+seq*1000).toISOString(),event:JSON.stringify(event)};
});
const switchedRows=currentRows.map(row=>{
  const event=JSON.parse(row.event),round=roundForSeq(row.cursor.seq);
  if(event.kind!=='user_input')event.session_id=round<=2
    ?'bc19e89c-41f7-4109-ae86-660ac1ef39dc'
    :round<=4?'1be22a87-c900-4378-87b7-2c862ffc7c1d'
    :'83e9a02a-e451-488e-a699-c62fa95ae27e';
  return{...row,event:JSON.stringify(event)};
});
// Simulated older pages straddle the real session_id transitions. The Hub
// storage key and generation remain unchanged; only payload.session_id changes.
const olderRows=[
  [3881,'user_input',1,'queued','current'],[3882,'user_input',1,1,'current'],
  [3883,'tool_call',1,1,'bc19'],[3884,'assistant_text',1,1,'bc19'],[3885,'state_change',1,1,'bc19'],
  [3886,'user_input',2,'queued','current'],[3887,'user_input',2,2,'current'],
  [3888,'tool_call',2,2,'1be22'],[3889,'assistant_text',2,2,'1be22'],[3890,'state_change',2,2,'1be22'],
].map(([seq,kind,turn,role,identity])=>({cursor:{generation:'0fda970aaff3905c61684736ec808452',seq},event_id:`older-${seq}`,
  received_at:new Date(Date.UTC(2026,10,11,12,40)+seq*1000).toISOString(),event:JSON.stringify(eventFor(seq,kind,role==='queued'?'queued':turn,identity))}));
let scenario='refresh';const streams=new Set();let origin;
const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://127.0.0.1'),m=url.pathname.match(/^\/view\/([^/]+)(?:\/(.*))?$/);
  if(m&&!m[2]){const html=renderers[url.searchParams.get('renderer')];if(!html){res.writeHead(404);res.end();return;}scenario=url.searchParams.get('scenario');res.writeHead(200,{'content-type':'text/html'});res.end(html);return;}
  if(m&&m[2]==='history'){
    const before=Number(url.searchParams.get('before')||0);let events;
    if(before)events=olderRows.filter(r=>r.cursor.seq<before);
    else if(scenario==='realtime')events=currentRows.filter(r=>JSON.parse(r.event).kind==='user_input');
    else events=scenario==='session-switch'?switchedRows:currentRows;
    const page={generation:'0fda970aaff3905c61684736ec808452',high_water:3953,events,gaps:[],
      has_older:!before&&scenario==='pagination',before_seq:before?null:(events[0]?.cursor.seq||null)};
    if(process.env.RAW_DIR){fs.mkdirSync(process.env.RAW_DIR,{recursive:true});fs.writeFileSync(path.join(process.env.RAW_DIR,`${scenario}-page-${before||'latest'}.json`),JSON.stringify(page,null,2)+'\n');}
    res.writeHead(200,{'content-type':'application/json','cache-control':'no-store'});res.end(JSON.stringify(page));return;
  }
  if(m&&m[2]==='events'){
    res.writeHead(200,{'content-type':'text/event-stream','cache-control':'no-cache',connection:'keep-alive'});streams.add(res);
    if(scenario==='realtime')setTimeout(()=>currentRows.filter(r=>JSON.parse(r.event).kind!=='user_input').forEach((row,i)=>{
      const ev=JSON.parse(row.event);for(const s of streams)if(!s.destroyed)s.write(`id: a11ce-${i+1}\ndata: ${JSON.stringify(ev)}\n\n`);
    }),1300);
    req.on('close',()=>{streams.delete(res);try{res.end()}catch(_){}});return;
  }
  if(m&&['status','tasks','page'].includes(m[2])){res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({session:'opus50tc2k.zzc',state:'idle',readonly:true,can_write:false,auth_mode:'legacy',owner:'owner',model:'Claude'}));return;}
  if(req.method==='POST'){req.resume();res.writeHead(200,{'content-type':'application/json'});res.end('{"ok":true}');return;}
  res.writeHead(404);res.end();
});
async function inspect(browser,renderer,which){
  const page=await browser.newPage({viewport:{width:1280,height:800}});
  await page.goto(`${origin}/view/opus50tc2k.zzc?${new URLSearchParams({renderer,scenario:which})}`,{waitUntil:'domcontentloaded'});
  await page.getByText('FIXTURE_USER_3950').waitFor({timeout:10000});
  await page.getByText('FIXTURE_ASSISTANT_3952').waitFor({timeout:12000});
  if(which==='pagination'){
    await page.locator('#historyOlder').waitFor({state:'visible',timeout:5000});
    await page.locator('#historyOlder').click();
    await page.getByText('FIXTURE_ASSISTANT_3884').waitFor({timeout:10000});
    await page.getByText('FIXTURE_ASSISTANT_3889').waitFor({timeout:10000});
  }
  await page.waitForFunction(()=>pendingRenders.size===0,null,{timeout:5000});
  const body=await page.locator('#timeline').innerText();
  const assistantMarkers=currentKinds.filter(r=>r.kind==='assistant_text').map(r=>`FIXTURE_ASSISTANT_${r.seq}`);
  const visibleAssistantRows=assistantMarkers.filter(marker=>body.includes(marker)).length;
  const result={renderer,scenario:which,currentHistoryRows:which==='realtime'?14:47,
    latestAssistantVisible:body.includes('FIXTURE_ASSISTANT_3952'),
    visibleAssistantRows,
    olderBc19Visible:body.includes('FIXTURE_ASSISTANT_3884'),older1be22Visible:body.includes('FIXTURE_ASSISTANT_3889'),
    userInputSessionId:'',assistantSessionId:'83e9a02a-e451-488e-a699-c62fa95ae27e',
    queuedTurnId:'',admittedAndAssistantTurnIntersection:7,
    sessionIdsInCurrentWindow:which==='session-switch'?['bc19e89c-41f7-4109-ae86-660ac1ef39dc','1be22a87-c900-4378-87b7-2c862ffc7c1d','83e9a02a-e451-488e-a699-c62fa95ae27e']:['83e9a02a-e451-488e-a699-c62fa95ae27e']};
  assert(result.latestAssistantVisible&&visibleAssistantRows===12,JSON.stringify(result));
  if(which==='pagination')assert(result.olderBc19Visible&&result.older1be22Visible,JSON.stringify(result));
  await page.close();return result;
}
async function main(){
  await new Promise((ok,bad)=>{server.once('error',bad);server.listen(0,'127.0.0.1',ok)});origin=`http://127.0.0.1:${server.address().port}`;
  const browser=await chromium.launch(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{});
  try{
    if(process.env.RAW_DIR){fs.mkdirSync(process.env.RAW_DIR,{recursive:true});fs.writeFileSync(path.join(process.env.RAW_DIR,'real-identities-manifest.json'),JSON.stringify({source:'prodops supplement d48c6fd6595a551bd351078f6cc8b5b0d7ea0817',storageKey:'same owner/agent/session_name and generation',generation:'0fda970aaff3905c61684736ec808452',seq:[3907,3953],kindCounts:{user_input:14,assistant_text:12,tool_call:13,state_change:8},userInputSessionId:'',claudeSessionId:'83e9a02a-e451-488e-a699-c62fa95ae27e',queuedInputTurnId:'',knownTurnIds:['8d0b99cd394a4e6dbd3c6e87e09594c3','fb86893a830448d7bd5450c307fe8d2e','123f55eecf9a487e976eb3dd540697e6'],turnOverlapCount:7,remainingTurnIds:'synthetic placeholders because supplement only supplies first three',switchPageIds:['bc19e89c-41f7-4109-ae86-660ac1ef39dc','1be22a87-c900-4378-87b7-2c862ffc7c1d','83e9a02a-e451-488e-a699-c62fa95ae27e'],scenarios:['refresh all current 47','realtime inputs from history then Claude-side events by SSE','upward pagination into two older session-id values then current id','same latest page with session_id transitions bc19 -> 1be22 -> 83e9'],notes:'Response bodies synthetic; current window kind/seq order follows raw. Older pagination records are diagnostic transition fixtures, not claimed as captured production seq.'},null,2)+'\n');}
    const out=[];for(const [name] of revisions)for(const scenario of ['refresh','realtime','pagination','session-switch'])out.push(await inspect(browser,name,scenario));
    for(const r of out)console.log(`RESULT ${JSON.stringify(r)}`);console.log('REAL_IDENTITIES_REPRO_OK renderers=2 paths=4 all_assistant_visible=true');
  }finally{await browser.close();await new Promise(ok=>server.close(ok));}
}
main().catch(e=>{console.error(e.stack||e);process.exitCode=1;try{server.close()}catch(_){}});
