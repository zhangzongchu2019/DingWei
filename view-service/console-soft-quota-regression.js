// Real Console DOM against loopback-only HTTP fixtures; no live Hub or node.
const assert = require('assert');
const {execFileSync} = require('child_process');
const http = require('http');
const path = require('path');
const {chromium} = require('playwright');
let html = execFileSync('go',['run','./view-service/console-hosted-page-fixture'],{
  cwd:path.join(__dirname,'..'),encoding:'utf8',env:{...process.env,DW_CONSOLE_HOSTED_FIXTURE:'1'},
});
const mutation=process.env.MUTATION||'';
if(mutation){
  const anchors={
    notice:["(n.quota_mode==='soft'?'【无硬磁盘配额】':'')","''"],
    consent:["if(softConsent&&softConsent.checked)body.allow_soft=true;","if(softConsent&&softConsent.checked)body.allow_soft=false;"],
    eligibility:["disabled:!n.online||n.quota_eligible===false","disabled:!n.online"],
    roster:["a.hosted.quota_mode==='soft'","false"],
  };
  assert(anchors[mutation],'unknown mutation');const [from,to]=anchors[mutation];assert.strictEqual(html.split(from).length-1,1,'mutation anchor');html=html.replace(from,to);
}
const options={
 nodes:[{id:'hard',name:'Hard',online:true,quota_mode:'hard',quota_eligible:true,free_mem_gb:16,free_vcpu:8,free_disk_gb:100},
 {id:'soft',name:'Soft',online:true,quota_mode:'soft',quota_eligible:true,free_mem_gb:16,free_vcpu:8,free_disk_gb:100},
 {id:'unknown',name:'Unready',online:true,quota_mode:'soft',quota_eligible:false,free_mem_gb:16,free_vcpu:8,free_disk_gb:100}],
 combos:[{cli:'codex',llm:'openai',cred_kind:'api_key',verified:true}],accounts:[{id:'fixture',name:'Fixture',llm:'openai',kind:'api_key',last4:'0000'}],
 plans:[{id:'1c1.5g40',name:'1C/1.5G/40G',vcpu:1,mem_gb:1.5,fits:true},{id:'2c4g',name:'2C/4G',fits:true},{id:'4c8g',name:'4C/8G',fits:false}],models:[],quota:{remaining:{agents:4,vcpu:8,mem_gb:16,disk_gb:100}},
};
const json=(res,status,body)=>{res.writeHead(status,{'content-type':'application/json'});res.end(JSON.stringify(body));};
const server=http.createServer(async(req,res)=>{
 const p=req.url.split('?')[0];
 if(p==='/console'){res.writeHead(200,{'content-type':'text/html'});return res.end(html);}
 if(p==='/api/console/platform')return json(res,200,{role:'owner',actions:[],items:[{id:'agent-groups',kind:'agent-groups',title:'Agent 编组',href:'#agent-groups',status:'ready'}]});
 if(p==='/api/console/agents')return json(res,200,{agents:[{agent_id:'ag_fixture',name:'FixtureSeat',full_name:'FixtureSeat.zzc',origin:'hosted',online:false,state:'active',hosted:{state:'abnormal',quota_mode:'soft',quota_hold:true}},{agent_id:'ag_unknown',name:'UnknownSeat',full_name:'UnknownSeat.zzc',origin:'hosted',online:true,state:'active',hosted:{state:'online',quota_mode:'soft',quota_warning:true,quota_usage_unknown:true}}]});
 if(p==='/api/console/agent-groups')return json(res,200,{principal:'owner',owned_agents:[],groups:[]});
 if(p==='/api/console/hosted/options')return json(res,200,options);
 if(p==='/api/console/hosted/agents'&&req.method==='POST')return json(res,409,{error:'node_no_capacity',message:'UNTRUSTED_DETAIL'});
 if(p==='/api/console/enroll/applications')return json(res,200,{applications:[]});
 if(p==='/api/console/llm-accounts')return json(res,200,[]);
 res.writeHead(404);res.end();
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 const context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(5000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.goto(`http://127.0.0.1:${server.address().port}/console`);
  await page.locator('#platform-items a[data-item-id="agent-groups"]').click();await page.locator('#hostedForm').waitFor({state:'visible'});
  assert.match(await page.locator('#hostedNode option[value=soft]').textContent(),/无硬磁盘配额/);
  assert.doesNotMatch(await page.locator('#hostedNode option[value=hard]').textContent(),/无硬磁盘配额/);
  assert.strictEqual(await page.locator('#hostedNode option[value=unknown]').evaluate(el=>el.disabled),true);
  assert.strictEqual(await page.locator('#hostedAllowSoft').isChecked(),false);
  await page.locator('#hostedCli').selectOption('codex');await page.locator('#hostedLlm').selectOption('openai');await page.locator('#hostedAccount').selectOption('fixture');await page.locator('#hostedPlan').selectOption('2c4g');
  assert.strictEqual(await page.locator('#hostedPlan option[value="1c1.5g40"]').textContent(),'1C/1.5G/40G');
  assert.strictEqual(await page.locator('#hostedPlan option[value="1c1.5g40"]').evaluate(el=>el.disabled),false);
  await page.locator('#hostedPlan').selectOption('1c1.5g40');
  for(const consent of [false,true]){
   await page.locator('#hostedName').fill(consent?'WithConsent':'NoConsent');await page.locator('#hostedNode').selectOption('soft');await page.locator('#hostedAllowSoft').setChecked(consent);
   const response=page.waitForResponse(r=>r.url().endsWith('/api/console/hosted/agents')&&r.request().method()==='POST');await page.locator('#hostedCreateBtn').click();const r=await response;
   if(consent)assert.strictEqual(r.request().postDataJSON().allow_soft,true);else assert(!('allow_soft' in r.request().postDataJSON()),'legacy default body shape');assert.strictEqual(r.request().postDataJSON().node_id,'soft');
   assert.strictEqual(r.request().postDataJSON().plan_id,'1c1.5g40');
  }
  assert.match(await page.locator('#rosterItems').textContent(),/无硬磁盘配额（磁盘保护，数据保留）/);
  assert.match(await page.locator('#rosterItems').textContent(),/磁盘测量暂不可用，暂停新建与启动/);
  assert.doesNotMatch(await page.locator('#rosterItems').textContent(),/接近限额/);
  assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(),/UNTRUSTED_DETAIL/);
  assert.deepStrictEqual(errors,[]);console.log('SOFT_QUOTA_BROWSER_OK assertions=16');
 }finally{await context.close();await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
