// Local fixture with the actual rendered Console page, including hosted-off mode.
const assert=require('assert');
const {execFileSync}=require('child_process');
const path=require('path'),http=require('http');
const {chromium}=require('playwright');
let html=execFileSync('go',['run','./view-service/console-hosted-page-fixture'],{cwd:path.join(__dirname,'..'),encoding:'utf8',env:{...process.env,DW_CONSOLE_HOSTED_FIXTURE:'0'}});
const mutation=process.env.MUTATION||'';
function mutate(from,to){assert.strictEqual(html.split(from).length-1,1,'MUTATION_ANCHOR_STALE');html=html.replace(from,to);}
if(mutation==='admin-menu-for-manager')mutate('agentListAdmin=platform.agent_list_admin===true;','agentListAdmin=true;');
else if(mutation==='drop-ip-render')mutate("a.host||'未知'","'未知'");
else if(mutation==='drop-page-cursor')mutate("if(after)query.set('after',after);",'');

else if(mutation)throw new Error('MUTATION_UNKNOWN');
let admin=true,deny=false,pass=0,requests=[],errors=[];
const first=[{agent_id:'ag-a',owner:{user_id:'alice',short_name:'al'},full_name:'Alpha.al',agent_type:'selfhost',host:'203.0.113.44',online:true,state:'active'},{agent_id:'ag-b',owner:{user_id:'bob',short_name:'bo'},full_name:'Beta.bo',agent_type:'hosted',host:'node-private',online:false,state:'suspended'}];
const json=(res,status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://fixture');
 if(url.pathname==='/console'){res.writeHead(200,{'Content-Type':'text/html'});res.end(html);return;}
 if(url.pathname==='/api/console/platform'){json(res,200,{role:'management',agent_list_admin:admin,feishu_bound:true,items:[{id:'platform-admin',title:'平台管理',href:'#platform-admin'}]});return;}
 if(url.pathname==='/api/console/platform/agents'){
  requests.push({method:req.method,limit:url.searchParams.get('limit'),after:url.searchParams.get('after')||''});
  if(!admin||deny){json(res,403,{error:'forbidden',detail:'PRIVATE-SERVER-DETAIL-MUST-NOT-RENDER'});return;}
  const after=url.searchParams.get('after');json(res,200,after?{agents:[{...first[0],agent_id:'ag-c',full_name:'Gamma.al',host:'2001:db8::9'}],next_after:''}:{agents:first,next_after:'ag-b'});return;
 }
 if(url.pathname==='/api/console/admin/users'){json(res,200,{users:[]});return;}
 json(res,200,{agents:[],groups:[],owned_agents:[]});
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch(process.env.PW_CHANNEL?{headless:true,channel:process.env.PW_CHANNEL}:{headless:true});
 const context=await browser.newContext(),page=await context.newPage();page.setDefaultTimeout(2500);page.on('pageerror',e=>errors.push(e.message));
 const check=async(name,fn)=>{try{await fn();pass++;console.log('PASS '+name);}catch(e){console.error('FAIL '+name+': '+e.message);throw e;}};
 try{
  await page.goto('http://127.0.0.1:'+server.address().port+'/console');await page.locator('[data-item-id="platform-admin"]').click();
  await page.locator('#adminMenu button[data-capability="all-agents"]').click();
  await check('active administrator sees a read-only inventory even without hosted enabled',async()=>{
   await page.locator('#allAgentItems tr').first().waitFor();
   const rows=await page.locator('#allAgentItems tr').evaluateAll(rows=>rows.map(r=>[...r.cells].map(c=>c.textContent)));
   assert.deepStrictEqual(rows,[['alice（al）','Alpha.al','自有','203.0.113.44','在线','有效'],['bob（bo）','Beta.bo','托管','node-private','离线','已停用']],'ADMIN_LIST_IP_OR_HOST_RENDER_MISSING');
   assert.strictEqual(await page.locator('#adminAllAgents input,#allAgentItems button,#allAgentItems a').count(),0);
   assert.deepStrictEqual(requests,[{method:'GET',limit:'100',after:''}]);
  });
  await page.locator('#allAgentNext').click();
  await check('pagination uses opaque cursor and replaces previous rows',async()=>{await page.waitForFunction(()=>document.getElementById('allAgentStatus').textContent!=='加载中…');assert.strictEqual(requests.at(-1).after,'ag-b','ADMIN_LIST_CURSOR_MISSING');await page.waitForFunction(()=>document.getElementById('allAgentItems').textContent.includes('Gamma.al'));assert.strictEqual(await page.locator('#allAgentItems tr').count(),1);assert.strictEqual(await page.locator('#allAgentNext').isDisabled(),true);assert.match(await page.locator('#allAgentItems').textContent(),/2001:db8::9/);});
  await page.locator('#allAgentRefresh').click();
  await check('explicit refresh resets cursor with one read',async()=>{await page.waitForFunction(()=>document.querySelectorAll('#allAgentItems tr').length===2);assert.strictEqual(requests.at(-1).after,'');assert.strictEqual(requests.length,3);assert(requests.every(r=>r.method==='GET'));});
  deny=true;await page.locator('#allAgentRefresh').click();
  await check('role refusal leaves no stale inventory or upstream private details',async()=>{await page.locator('#allAgentStatus').filter({hasText:'仅平台管理员'}).waitFor();assert.strictEqual(await page.locator('#allAgentItems tr').count(),0,'ADMIN_LIST_CLEAR_ON_DENIAL_MISSING');assert.strictEqual(await page.locator('#allAgentNext').isDisabled(),true);assert(!await page.locator('#adminAllAgents').textContent().then(t=>t.includes('203.0.113.44')||t.includes('PRIVATE-SERVER-DETAIL')));});
  admin=false;await page.reload();await page.locator('[data-item-id="platform-admin"]').click();
  await check('manager has no global-agent capability or table',async()=>{assert.strictEqual(await page.locator('#adminMenu button[data-capability="all-agents"]').count(),0,'ADMIN_LIST_MENU_ROLE_GATE_MISSING');assert.strictEqual(await page.locator('#adminAllAgents').count(),0);});
  await check('no page errors',async()=>assert.deepStrictEqual(errors,[]));
 }finally{await context.close();await browser.close();await new Promise(r=>server.close(r));}
 console.log('R31 admin-agent browser: passed='+pass+' failed=0 mutation='+(mutation||'none'));
})().catch(e=>{console.error(e.message);server.close();process.exitCode=1;});
