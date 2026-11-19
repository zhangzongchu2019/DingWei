// Production Console HTML with local API stubs; authorization is tested against real PG in Go.
const assert = require('assert');
const http = require('http');
const path = require('path');
const {execFileSync} = require('child_process');
const {chromium} = require('playwright');
const root=path.join(__dirname,'..');
const html=execFileSync('go',['run','./view-service/console-hosted-page-fixture'],{cwd:root,encoding:'utf8',env:{...process.env,DW_CONSOLE_HOSTED_FIXTURE:'0'}});
const code='abcdefghijklmnopqrstuv1234567890';
const unpaired={id:'unpaired',session_name:'bot_EX0123456789abcdef0123456789abcdef.owner',external:true,enrollment:'已验证／未编组',pairing:'未创建',target:'—',online:false};
const delegated={id:'delegated-external',session_name:'other_AG8.other',external:true,online:false};
const owned={id:'own-internal',session_name:'internal.owner',online:true};
const posts=[];
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://localhost');
 if(url.pathname==='/console'){res.writeHead(200,{'Content-Type':'text/html'});res.end(html);return;}
 if(req.method==='POST'&&url.pathname===`/api/console/agent-groups/${code}/members`){let body='';req.on('data',x=>body+=x);req.on('end',()=>{posts.push(JSON.parse(body));res.writeHead(204);res.end();});return;}
 let data={items:[]};
 if(url.pathname==='/api/console/agent-groups') data={principal:'owner',owned_agents:[owned,unpaired],external_candidates:[unpaired,delegated],groups:[{code,short_name:'ordinary',leader_agent_id:owned.id,leader_session_name:owned.session_name,can_manage:true,members:[],additions_closed:false,leader_only:false}]};
 else if(url.pathname==='/api/console/agents')data={agents:[]};
 else if(url.pathname==='/api/console/platform')data={items:[],role:'platform_admin'};
 res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(data));
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true,args:['--no-sandbox']});
 try{
  const page=await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}/console`,{waitUntil:'domcontentloaded'});
  await page.evaluate(()=>openGroupWorkspace());
  await page.waitForFunction(()=>document.querySelectorAll('#groupCodeJoinAgent option').length===3);
  assert.equal(await page.locator('#groupCodeJoinAgent option[value="unpaired"]').count(),1,'external candidate duplicated owned seat');
  assert((await page.locator('#groupCodeJoinAgent option[value="unpaired"]').textContent()).includes('已验证／未编组 · 配对未创建 · 目标—'),'unpaired state missing');
  assert.equal(await page.locator('#groupLeader option[value="delegated-external"]').count(),0,'admin join scope must not silently grant group-leader ownership');
  await page.locator('#groupJoinCode').fill(code);
  await page.locator('#groupCodeJoinAgent').selectOption(delegated.id);
  await page.locator('#joinByCode').click();
  await page.waitForFunction(()=>document.querySelector('#groupCodeJoinFeedback').textContent==='已加入编组');
  assert.deepEqual(posts,[{agent_id:delegated.id}],'admin-selected external seat was not submitted unchanged');
  console.log('EXTSEAT_GROUPS_BROWSER_OK cases=4');
 }finally{await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);server.close();process.exitCode=2;});
