// R31 administrator hosted-node UI and private Agent host/type rendering.
// Local HTTP fixture only. Authorization/storage are covered by real-PG Go tests.
const assert = require('assert');
const {execFileSync} = require('child_process');
const http = require('http');
const path = require('path');
const {chromium} = require('playwright');
const html = execFileSync('go',['run','./view-service/console-hosted-page-fixture'],{cwd:path.join(__dirname,'..'),encoding:'utf8',env:{...process.env,DW_CONSOLE_HOSTED_FIXTURE:'1'}});
let admin=true, nodes=[], writes=[], errors=[], pass=0;
const credential='R31-LOCAL-NODE-CREDENTIAL';
const json=(res,status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
const server=http.createServer(async(req,res)=>{
 const p=req.url.split('?')[0];
 if(p==='/console'){res.writeHead(200,{'Content-Type':'text/html'});res.end(html);return;}
 if(p==='/api/console/platform'){json(res,200,{role:'management',hosted_node_admin:admin,feishu_bound:true,actions:['manage_users'],items:[{id:'platform-admin',title:'平台管理',href:'#platform-admin',active:true}]});return;}
 if(p==='/api/console/agents'){json(res,200,{agents:[]});return;}
 if(p==='/api/console/admin/users'){json(res,200,{users:[]});return;}
 if(p==='/api/console/admin/hosted/nodes'&&req.method==='GET'){json(res,admin?200:403,admin?{nodes:nodes.map(n=>({node:n,online:true}))}:{error:'forbidden'});return;}
 if(p.startsWith('/api/console/admin/hosted/nodes')&&req.method!=='GET'){
  if(!admin){json(res,403,{error:'forbidden'});return;}
  let text='';for await(const chunk of req)text+=chunk;const body=JSON.parse(text);writes.push({method:req.method,body});
  if(req.method==='POST'){nodes.push({...body,scheduling_state:'enabled'});json(res,201,{node_id:body.id,credential});return;}
  Object.assign(nodes.find(n=>n.id===decodeURIComponent(p.split('/').at(-1))),body);json(res,200,{node_id:nodes[0].id});return;
 }
 json(res,200,{groups:[],owned_agents:[],agents:[]});
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch(process.env.PW_CHANNEL?{headless:true,channel:process.env.PW_CHANNEL}:{headless:true});
 const context=await browser.newContext();const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 const check=(name,fn)=>Promise.resolve().then(fn).then(()=>{pass++;console.log('PASS '+name);});
 try{
  await page.goto('http://127.0.0.1:'+server.address().port+'/console');
  await page.locator('[data-item-id="platform-admin"]').click();await page.locator('#adminMenu button[data-capability="nodes"]').click();
  await check('administrator sees hosted-node page and safe admin_only default',async()=>{await page.locator('#adminNodes').waitFor({state:'visible'});assert.strictEqual(await page.locator('#nodeScope').inputValue(),'admin_only');assert.strictEqual(await page.locator('#nodeCredentialBox').isHidden(),true);});
  await page.locator('#nodeId').fill('nd_ui');await page.locator('#nodeName').fill('UI主机');await page.locator('#nodePool').fill('shared');await page.locator('#nodeRevision').fill('a'.repeat(64));await page.locator('#nodeAddress').fill('203.0.113.20');await page.locator('#nodeSave').click();
  await check('registration displays credential once with intended request fields',async()=>{await page.locator('#nodeCredentialBox').waitFor({state:'visible'});assert.strictEqual(await page.locator('#nodeCredential').textContent(),credential);assert.strictEqual(writes[0].method,'POST');assert.deepStrictEqual(writes[0].body.access,{scope:'admin_only',users:[],groups:[]});assert(!Object.keys(writes[0].body).includes('credential'));});
  await page.locator('#nodeCredentialDone').click();
  await check('done clears credential from DOM and browser storage',async()=>{assert.strictEqual(await page.locator('#nodeCredential').textContent(),'');assert(!await page.evaluate(c=>JSON.stringify(localStorage).includes(c)||JSON.stringify(sessionStorage).includes(c),credential));});
  const row=page.locator('#nodeItems tr').filter({hasText:'UI主机'});await row.waitFor();await row.getByRole('button',{name:'范围 / 地址',exact:true}).click();
  await page.locator('#nodeScope').selectOption('restricted');await page.locator('#nodeUsers').fill('owner, member');await page.locator('#nodeGroups').fill('gggggggggggggggggggggggggggggggg');await page.locator('#nodeSave').click();
  await check('editing changes access/address without reissuing credentials',async()=>{await page.waitForFunction(()=>document.getElementById('nodeStatus').textContent==='已保存');assert.deepStrictEqual(writes.at(-1).body.access,{scope:'restricted',users:['owner','member'],groups:['gggggggggggggggggggggggggggggggg']});assert.strictEqual(writes.at(-1).method,'PATCH');assert.strictEqual(await page.locator('#nodeCredentialBox').isHidden(),true);});
  await row.getByRole('button',{name:'停用',exact:true}).click();
  await check('disable sends only scheduling state; access stays unchanged',async()=>{await page.waitForFunction(()=>document.querySelector('#nodeItems tr td:nth-child(4)').textContent.includes('已停用'));assert.deepStrictEqual(writes.at(-1).body,{scheduling_state:'disabled'});assert.strictEqual(nodes[0].access.scope,'restricted');});
  await row.getByRole('button',{name:'排空',exact:true}).click();
  await check('drain is explicit scheduling action',async()=>{await page.waitForFunction(()=>document.querySelector('#nodeItems tr td:nth-child(4)').textContent.includes('排空中'));assert.deepStrictEqual(writes.at(-1).body,{scheduling_state:'draining'});});
  admin=false;await page.reload();await page.locator('[data-item-id="platform-admin"]').click();
  await check('manager without platform-admin capability has no node page',async()=>{assert.strictEqual(await page.locator('#adminMenu button[data-capability="nodes"]').count(),0);assert.strictEqual(await page.locator('#adminNodes').count(),0);});
  await check('no browser errors',async()=>assert.deepStrictEqual(errors,[]));
 }finally{await context.close();await browser.close();await new Promise(r=>server.close(r));}
 console.log('R31 hosted-node browser: passed='+pass+' failed=0');
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
