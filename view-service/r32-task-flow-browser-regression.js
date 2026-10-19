// Real production HTML; HTTP fixtures model the already separately tested Hub.
const assert=require('assert');
const fs=require('fs');
const http=require('http');
const path=require('path');
const {chromium}=require('playwright');
const root=path.join(__dirname,'..');
function embedded(file,name,end){const s=fs.readFileSync(path.join(root,file),'utf8');const start='const '+name+' = `';const a=s.indexOf(start);assert(a>=0);const b=s.indexOf(end,a+start.length);assert(b>a);return s.slice(a+start.length,b)}
const board=embedded('internal/m8/console_task_board_page.go','consoleTaskBoardPageHTML','`\n\nconst (');
let specs=embedded('internal/m8/console_specs_page.go','consoleSpecsPageHTML','`\n\nfunc (h *Hub) HandleConsoleSpecsPage');
specs=specs.replace('<script id=dw-md-core></script>',()=>'<script>'+fs.readFileSync(path.join(__dirname,'md-render-core.js'),'utf8')+'</script>');
const task={proposal_id:'task-one',task_id:'task-one',spec_id:'spec-one',revision:1,title:'Deliver',content:'Work',status:'planned',definition_of_done:'Pass',depends_on_json:'[]',acceptance_json:'[]',review_id:'manual:task-one',developer_agent_id:'dev',reviewer_agent_id:'review',developer_agent:'Dev.owner',reviewer_agent:'Review.owner',developer_online:false,reviewer_online:true,passed_cases:0,total_cases:1,version:2,group_code:'group'};
const spec={id:'spec-one',title:'R32 candidate',state:'final',current_revision:1,owner_short_name:'Owner',current_editor_short_name:'',task_state:'splitting',can_edit:false,can_split:true,archived:false};
const candidate={request_id:'presplit-one',version:1,revision:1,confirmed:false,tasks:[{id:'candidate-one',title:'Candidate',content:'Implement safely',definition_of_done:'Verified',estimated_hours:1,depends_on:[],developer_agent:'Dev.owner',reviewer_agent:'Review.owner',group_code:'group'}]};
const agents=[{id:'dev',session_name:'Dev.owner',group_codes:['group'],online:false},{id:'review',session_name:'Review.owner',group_codes:['group'],online:true}];
let boardReads=0,confirmed=0,formalTasks=[],assigned=[];
const json=(res,status,body)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body))};
const server=http.createServer(async(req,res)=>{
 const u=new URL(req.url,'http://fixture');
 if(u.pathname==='/console/task-board'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(board)}
 if(u.pathname==='/console/specs'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(specs)}
 if(u.pathname.startsWith('/console/spec-assets/')){const name=path.basename(u.pathname);const p=path.join(root,'internal/m8/static/easymde',name);if(fs.existsSync(p)){res.writeHead(200,{'Content-Type':name.endsWith('.js')?'text/javascript':'text/css'});return res.end(fs.readFileSync(p))}}
 if(u.pathname==='/api/console/task-board'){
  boardReads++;return json(res,200,{viewer_key:'owner',specs:[spec],tasks:[task],active_tasks:task.status==='planned'?[]:[task],task_total:1,agents:[],task_agents:agents,task_groups:[{code:'group',name:'Team'}],spec_page:1,spec_total_pages:1,task_page:1,task_total_pages:1,updated_at:new Date().toISOString()})
 }
 if(u.pathname==='/api/console/task-board/tasks/task-one'&&req.method==='PATCH'){
  let raw='';for await(const c of req)raw+=c;const value=JSON.parse(raw);assigned.push(value);task.version++;if(value.reopen){task.status='planned';task.passed_cases=0}return json(res,200,{version:task.version})
 }
 if(u.pathname==='/api/console/specs')return json(res,200,{role:'owner',specs:[spec],page:1,total_pages:1,total:1,page_size:20});
 if(u.pathname==='/api/console/spec-editors')return json(res,200,{editors:[{short_name:'Owner',display_name:'Owner',value:'Owner'}]});
 if(u.pathname==='/api/console/specs/spec-one')return json(res,200,{spec,revision:{spec_id:spec.id,revision:1,content_markdown:'# Requirements',acceptance_json:'[]',input_anchors_json:'[]'},tasks:formalTasks,dependency_candidates:[]});
 if(u.pathname==='/api/console/specs/spec-one/presplit')return json(res,200,{request:{request_id:candidate.request_id,status:'completed'},candidate,agents:[{id:'split',session_name:'Splitter.owner'}],task_groups:[{code:'group',name:'Team'}],task_agents:agents});
 if(u.pathname==='/api/console/specs/spec-one/presplit/presplit-one/confirm'){
  let raw='';for await(const c of req)raw+=c;const b=JSON.parse(raw);assert.equal(b.expected_revision,1);assert.equal(b.expected_version,1);assert.equal(b.group_code,'group');assert.equal(b.tasks[0].developer_agent,'Dev.owner');assert.equal(b.tasks[0].reviewer_agent,'Review.owner');assert.equal(b.tasks[0].content,'Edited by owner');confirmed++;candidate.confirmed=true;formalTasks=[{...task,proposal_id:'candidate-one',title:'Candidate',content:b.tasks[0].content}];return json(res,200,{confirmed:true})
 }
 return json(res,404,{error:'not_found'});
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch(process.env.PW_CHANNEL?{headless:true,channel:process.env.PW_CHANNEL}:{headless:true});let checks=0;const errors=[];
 try{
  const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(origin+'/console/task-board');await page.waitForSelector('#task-list tr');
  assert.equal(await page.locator('#task-table th').count(),5);checks++;
  const reads=boardReads;task.status='blocked';task.blocker='Waiting for review';
  await page.waitForFunction(()=>document.querySelector('#active-task-list').textContent.includes('Waiting for review'),null,{timeout:8000}).catch(()=>{throw new Error('R32_BOARD_REFRESH_STALLED')});assert(boardReads>reads);checks++;
  task.needs_reassignment=true;await page.click('#refresh-tasks');await page.waitForFunction(()=>document.querySelector('#task-list').textContent.includes('请改派'));checks++;
  task.needs_reassignment=false;await page.click('#refresh-tasks');await page.click('#edit-tasks');
  const devOptions=await page.locator('select.developer-agent option').allTextContents();assert(devOptions.some(x=>x.includes('Dev.owner')&&x.includes('离线')));checks++;
  await page.selectOption('select.developer-agent','dev');await page.dispatchEvent('select.developer-agent','change');
  await page.click('#finish-tasks');await page.waitForFunction(()=>document.querySelector('#task-status').textContent==='已保存'||document.querySelector('#finish-tasks').hidden);
  assert.equal(assigned.length,1);assert.equal(assigned[0].expected_version,2);assert.equal(assigned[0].group_code,'group');checks++;
  task.status='done';await page.click('#refresh-tasks');await page.waitForFunction(()=>document.querySelector('#task-list').textContent.includes('done'));
  await page.click('#edit-tasks');await page.selectOption('select.developer-agent','review');await page.selectOption('select.reviewer-agent','dev');
  const reopenDialog=new Promise(resolve=>page.once('dialog',async d=>{await d.dismiss();resolve(true)}));const bypassWrite=page.waitForResponse(r=>r.url().endsWith('/api/console/task-board/tasks/task-one')&&r.request().method()==='PATCH').then(()=>false).catch(()=>false);await page.click('#finish-tasks');
  const sawReopen=await Promise.race([reopenDialog,bypassWrite,new Promise(resolve=>setTimeout(()=>resolve(false),6000))]);assert(sawReopen,'DONE_ASSIGNMENT_CONFIRMATION_BYPASSED');assert.equal(assigned.length,1);checks++;
  page.once('dialog',d=>d.accept());await page.click('#finish-tasks');await page.waitForFunction(()=>document.querySelector('#finish-tasks').hidden);
  assert.equal(assigned.length,2);assert.equal(assigned[1].reopen,true);assert.equal(task.status,'planned');checks++;
  await page.goto(origin+'/console/specs');await page.click('.spec-title');await page.waitForSelector('#presplit-candidates:not([hidden])');
  assert.equal(formalTasks.length,0);assert.equal(confirmed,0);checks++;
  const textareas=page.locator('#candidate-list textarea');await textareas.nth(0).fill('Edited by owner');
  await page.click('#confirm-presplit');await page.waitForFunction(()=>document.querySelector('#presplit-candidates').hidden);
  assert.equal(confirmed,1);assert.equal(formalTasks.length,1);checks++;
  await page.waitForFunction(()=>document.querySelector('#task-list').textContent.includes('Edited by owner'));checks++;
  assert.deepEqual(errors,[]);checks++;
  console.log(JSON.stringify({suite:'r32-task-flow',passed:checks,failed:0}));
 }finally{await browser.close();await new Promise(r=>server.close(r))}
})().catch(e=>{console.error(e.stack);server.close();process.exitCode=1});
