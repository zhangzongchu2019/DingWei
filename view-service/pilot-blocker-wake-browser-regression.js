// Production task-board HTML, loopback fixtures only; no live account or Hub.
const assert=require('assert');const fs=require('fs');const path=require('path');const http=require('http');const {chromium}=require('playwright');
const root=path.join(__dirname,'..'),source=fs.readFileSync(path.join(root,'internal/m8/console_task_board_page.go'),'utf8');
const marker='const consoleTaskBoardPageHTML = `',begin=source.indexOf(marker)+marker.length,end=source.indexOf('`\n\nconst (',begin);assert(begin>=marker.length&&end>begin);const html=source.slice(begin,end);
const task={proposal_id:'task-one',task_id:'task-one',spec_id:'spec-one',revision:1,title:'Original task',content:'原任务',status:'planned',acceptance_json:'[]',review_id:'manual:task-one',developer_agent_id:'dev',reviewer_agent_id:'review',developer_agent:'Dev.owner',reviewer_agent:'Review.owner',version:2,group_code:'group'};
let version=2,cards=[],inputs=[],requests=[],reject=0,denied=false,mutations=0;const cache=new Map();
function json(res,status,body){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body))}
const server=http.createServer(async(req,res)=>{const u=new URL(req.url,'http://fixture');if(u.pathname==='/console/task-board'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(html)}
if(u.pathname==='/api/console/task-board')return json(res,200,{viewer_key:'owner',blocker_current_user:'owner',specs:[{id:'spec-one',title:'Spec',state:'final'}],tasks:[task],active_tasks:[],task_total:1,agents:[],task_agents:[{id:'dev',session_name:'Dev.owner',group_codes:['group'],online:true}],task_groups:[{code:'group',name:'Team'}],spec_page:1,spec_total_pages:1,task_page:1,task_total_pages:1,updated_at:new Date().toISOString()});
if(u.pathname.startsWith('/api/console/tasks/task-one/blockers')){
 if(denied)return json(res,403,{error:'task_access_denied'});
 const bid=decodeURIComponent(u.pathname.split('/').at(-1)),single=bid!=='blockers';
 if(req.method==='GET')return json(res,200,{blockers:single?cards.filter(x=>x.id===bid):cards,inputs:single?inputs:[],inputs_complete:true,inputs_next_before:0,next_cursor:'',task_version:version,execution_authorized:false});
 let raw='';for await(const c of req)raw+=c;const b=JSON.parse(raw);requests.push(b);
 if(reject){const status=reject;reject=0;return json(res,status,{error:status===409?'conflict':'unknown'})}
 if(cache.has(b.request_id))return json(res,200,cache.get(b.request_id));mutations++;
 let card=cards.find(x=>x.id===bid);
 if(b.operation==='create'){card={id:'blk:one',task_id:'task-one',version:1,generation:1,where:b.where,kind:b.kind,expected_input:b.expected_input,waiting_principal:b.waiting_principal,state:b.waiting_principal?'waiting':'needs_owner',mode:b.production_or_security?'full':'light',resume_recheck:[{id:'identity',text:'当前身份和指派'},{id:'materials',text:'材料真实性'},{id:'authorization',text:'当前授权；唤醒不是新授权'}],input_version:0};cards=[card]}
 else if(b.operation==='input'){if(!inputs.some(x=>x.event_id===b.event_id)){card.input_version++;card.version++;card.state='ready_to_recheck';card.wake_state='queued_offline_or_upgrade';inputs.push({...b,created_at:'2026-10-20T16:00:00Z'})}}
 else if(b.operation==='assign_waiter'){card.waiting_principal=b.waiting_principal;card.version++}
 else if(b.operation==='approve_full'){card.full_approval_ref=b.full_approval_ref;card.version++}
 else if(b.operation==='cancel'){card.state='cancelled';card.version++}
 version++;const result={blocker:card,task_version:version,outcome:'updated',execution_authorized:false};cache.set(b.request_id,result);return json(res,200,result)
}
return json(res,404,{error:'not_found'})});
(async()=>{await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;const browser=await chromium.launch(process.env.PW_CHANNEL?{headless:true,channel:process.env.PW_CHANNEL}:{headless:true});let checks=0;const errors=[];
try{const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(origin+'/console/task-board');await page.waitForSelector('#task-list button');await page.click('#task-list button');await page.waitForSelector('#blocker-panel:not([hidden])');checks++;
await page.fill('#blocker-where','<script>window.INJECTED=true</script>');await page.fill('#blocker-expected','原任务材料');await page.selectOption('#blocker-waiter','user:owner');await page.check('#blocker-risk');await page.click('#blocker-create button[type=submit]');await page.waitForSelector('#blocker-list h3');assert.equal(cards.length,1);assert.equal(requests[0].operation,'create');assert.equal(requests[0].production_or_security,true);assert.equal(requests[0].expected_version,2);checks++;
assert.equal(await page.evaluate(()=>window.INJECTED),undefined);assert((await page.locator('#blocker-list').innerText()).includes('<script>'));assert.equal(task.status,'planned');checks++;
assert((await page.locator('#blocker-list').innerText()).includes('完整档'));assert((await page.locator('#blocker-list').innerText()).includes('本人'));checks++;
let card=page.locator('#blocker-list section').first();await card.locator('textarea').fill('材料答复，不是执行指令');await card.locator('input').first().fill('git:immutable-evidence');reject=409;await card.getByRole('button',{name:'答复到件',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#blocker-status').textContent.includes('版本或权限'));assert.equal(await card.locator('textarea').inputValue(),'材料答复，不是执行指令');assert.equal(inputs.length,0);checks++;
reject=503;await card.getByRole('button',{name:'答复到件',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#blocker-status').textContent.includes('结果待确认'));const firstUnknown=requests.at(-1).request_id;assert.equal(await card.locator('textarea').inputValue(),'材料答复，不是执行指令');checks++;
await card.getByRole('button',{name:'答复到件',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#blocker-list').textContent.includes('到件待重核'));assert.equal(requests.at(-1).request_id,firstUnknown);assert.equal(inputs.length,1);assert.equal(task.status,'planned');checks++;
assert((await page.locator('#blocker-list').innerText()).includes('离线或需升级'));assert(!requests.some(x=>['resume','start','complete'].includes(x.operation)));checks++;
card=page.locator('#blocker-list section').first();await card.getByRole('button',{name:'查看答复与材料'}).click();await page.waitForFunction(()=>document.querySelector('#blocker-list').textContent.includes('git:immutable-evidence'));assert((await card.innerText()).includes('材料答复，不是执行指令'));checks++;
await page.fill('#blocker-where','PRIVATE-DRAFT');await page.fill('#blocker-expected','PRIVATE-EXPECTED');denied=true;await card.getByRole('button',{name:'查看答复与材料'}).click();await page.waitForFunction(()=>document.querySelector('#blocker-panel').hidden);assert.equal(await page.locator('#blocker-where').inputValue(),'');assert.equal(await page.locator('#blocker-expected').inputValue(),'');assert.equal(await page.locator('#blocker-list').innerText(),'');checks++;
assert.deepEqual(errors,[]);checks++;console.log(JSON.stringify({suite:'pilot-blocker-wake',passed:checks,failed:0,live_access:false}))
}finally{await browser.close();await new Promise(r=>server.close(r))}})().catch(e=>{console.error(e.stack);server.close();process.exitCode=1});
