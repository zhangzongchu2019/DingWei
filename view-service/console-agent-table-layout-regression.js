// Real Console HTML and helper UI in Chromium, synthetic HTTP only. No live service.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('playwright');
const root=path.join(__dirname,'..'),go=fs.readFileSync(path.join(root,'internal/m8/console_auth.go'),'utf8');
const marker='const consolePageHTML = `',start=go.indexOf(marker),end=go.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents',start);
assert(start>=0&&end>start);
let html=go.slice(start+marker.length,end).replace('{{FEATURE_HOSTED}}','true'),helper=fs.readFileSync(path.join(root,'internal/m8/console_helper_updates.js'),'utf8');
const mutation=process.env.MUTATION||'',out=process.env.LAYOUT_EVIDENCE_DIR||'/tmp/console-agent-table-layout';
const mutations={
 'fixed-columns':['.group-table{width:auto;table-layout:auto;border-collapse:collapse}', '.group-table{width:100%;table-layout:fixed;border-collapse:collapse}.group-table th,.group-table td{width:20%}'],
 'actions-wrap':['.group-table td:last-child{white-space:nowrap;min-width:7em}', '.group-table td:last-child{white-space:normal;min-width:0}'],
 'buttons-wrap':['.group-table button{white-space:nowrap}', '.group-table button{white-space:normal}'],
 'no-scroll':['.console-table-scroll{max-width:100%;overflow-x:auto}', '.console-table-scroll{max-width:100%;overflow-x:visible}'],
 'no-ellipsis':['.group-table .table-name{max-width:clamp(12rem,25vw,24rem);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}', '.group-table .table-name{white-space:normal}'],
 'helper-below':['toolbar.append(open);','section.append(open);'],
 'wide-select':['.group-workspace select{max-width:100%}','.group-workspace select{max-width:none}'],
 'wide-checkbox':['.group-workspace input[type=checkbox]{min-width:0}','.group-workspace input[type=checkbox]{min-width:320px}'],
 'status-overflow':['.group-workspace [role=status]{overflow-wrap:anywhere}','.group-workspace [role=status]{overflow-wrap:normal}'],
};
if(mutation){assert(mutations[mutation],'UNKNOWN_MUTATION');const [from,to]=mutations[mutation];let source=mutation==='helper-below'?helper:html;assert.equal(source.split(from).length,2,'MUTATION_ANCHOR_STALE');source=source.replace(from,to);if(mutation==='helper-below')helper=source;else html=source;}
const long='Agent名称很长需要保留完整提示_'+('long_agent_name_'.repeat(12));
const agents=[{agent_id:'self',id:'self',key_id:'fixture',name:long,full_name:long+'.owner',session_name:long+'.owner',online:true,origin:'selfhost',state:'active',host:'VM-4-12'},
 {agent_id:'hosted',id:'hosted',name:'HostedAgent',full_name:'HostedAgent.owner',session_name:'HostedAgent.owner',online:true,origin:'hosted',state:'active',host:'host43',hosted:{state:'running',node_name:'host43',upgradable:true,model:'fixture-model',quota_mode:'soft',quota_warning:true}}];
const group={code:'a'.repeat(32),short_name:'布局验证组',creator_principal:'owner',can_manage:true,owner_display_name:'业主',leader_key_id:'fixture',leader_session_name:'review.owner',members:[]};
const SHA='a'.repeat(64),results=[];
(async()=>{
 fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({channel:process.env.PW_CHANNEL||'chrome',headless:true,args:['--no-sandbox']});
 try{for(const width of [1440,1024,768]){
  const page=await browser.newPage({viewport:{width,height:1000}}),errors=[],writes=[];let catalogGets=0;
  page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.route('http://layout.test/**',async route=>{
   const req=route.request(),p=new URL(req.url()).pathname;
   if(p==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:html+'<script>'+helper+'</script>'});
   let data={};
   if(p==='/api/console/agents')data={agents};
   else if(p==='/api/console/agent-groups')data={principal:'owner',groups:[group],owned_agents:agents};
   else if(p==='/api/console/hosted/options')data={nodes:[],combos:[],plans:[],models:[],accounts:[],quota:{remaining:{}}};
   else if(p==='/api/console/platform')data={role:'owner',items:[],actions:[]};
   else if(p==='/api/console/enroll/applications')data={applications:[{application_id:'app1',source_ip:'10.0.0.8',device_fingerprint:'fixture-device-0123456789',client_version:'0.2.3.7',requested_at:'2026-10-20T23:00:00Z'}]};
   else if(p==='/api/console/helper-updates'){
    if(req.method()==='POST'){writes.push({path:p,body:req.postDataJSON()});data={results:[{agent_id:'self',ok:true,job:{stage:'offered',version:'0.2.3.7'}}]};}
    else{catalogGets++;data={releases:[{sha256:SHA,version:'0.2.3.7',platform:'linux'}],agents:[{agent_id:'self',name:long,version:'0.2.3.6',eligible:true},{agent_id:'old',name:'Legacy',version:'0.2.3.2',eligible:false,reason:'manual_required'}]};}
   }else if(req.method()!=='GET'){writes.push({path:p,body:req.postData()?req.postDataJSON():null});data={ok:true};}
   return route.fulfill({json:data});
  });
  await page.goto('http://layout.test/');await page.evaluate(()=>openGroupWorkspace());
  // Populate using the production renderers; keep full page chrome, actual controls and handlers.
  await page.evaluate(({agents,group})=>{rosterAgents=agents;renderRoster();enrollSnapshot=[{application_id:'app1',source_ip:'10.0.0.8',device_fingerprint:'fixture-0123456789',client_version:'0.2.3.7',requested_at:'2026-10-20T23:00:00Z'}];renderEnroll();groupSnapshot={principal:'owner',owned_agents:agents,groups:[group]};renderGroups();}, {agents,group});
  await page.locator('#helperUpdateOpen').waitFor();
  const hostedTitle=await page.locator('#rosterItems [data-agent-id=hosted] td').first().getAttribute('title');assert(hostedTitle.includes('fixture-model')&&hostedTitle.includes('磁盘用量接近限额'),'LAYOUT_METADATA_TOOLTIP');
  assert.equal(catalogGets,0,'LAYOUT_LAZY_CATALOG');
  const toolbar=await page.locator('#helperUpdateOpen').evaluate(e=>e.closest('#rosterToolbar')!==null);
  assert(toolbar,'LAYOUT_HELPER_TOOLBAR');
  // Await initial asynchronous list reads before replacing fixture rows deterministically.
  await page.waitForFunction(()=>!document.getElementById('rosterStatus').textContent.includes('加载中'));
  await page.evaluate(({agents})=>{rosterAgents=agents;renderRoster();enrollSnapshot=[{application_id:'app1',source_ip:'10.0.0.8',device_fingerprint:'fixture-0123456789',client_version:'0.2.3.7',requested_at:'2026-10-20T23:00:00Z'}];renderEnroll();}, {agents});
  const metrics=await page.evaluate(()=>{
   const tables=['rosterTable','enrollTable','groupListTable'].map(id=>{
    const t=document.getElementById(id),wrap=t.parentElement,cs=getComputedStyle(t),last=t.querySelector('tbody tr td:last-child');
    const headers=[...t.querySelectorAll('th')].map(e=>({text:e.textContent,whiteSpace:getComputedStyle(e).whiteSpace,height:e.getBoundingClientRect().height}));
    const buttons=[...t.querySelectorAll('tbody button:not([hidden])')].map(b=>{
     const r=b.getBoundingClientRect(),range=document.createRange();range.selectNodeContents(b);return {text:b.textContent,y:r.y,height:r.height,textRects:[...range.getClientRects()].map(x=>({x:x.x,y:x.y,width:x.width,height:x.height})),whiteSpace:getComputedStyle(b).whiteSpace};
    });
    return {id,layout:cs.tableLayout,width:t.getBoundingClientRect().width,wrapWidth:wrap.clientWidth,scrollWidth:wrap.scrollWidth,overflow:getComputedStyle(wrap).overflowX,actionWhiteSpace:last&&getComputedStyle(last).whiteSpace,actionMin:last&&parseFloat(getComputedStyle(last).minWidth),headers,buttons};
   });
   const name=document.querySelector('#rosterItems tr td'),n=getComputedStyle(name);
   const toolbar=document.getElementById('rosterToolbar').getBoundingClientRect(),open=document.getElementById('helperUpdateOpen').getBoundingClientRect(),table=document.getElementById('rosterTable').getBoundingClientRect();
   return {tables,name:{title:name.title,ellipsis:n.textOverflow,whiteSpace:n.whiteSpace,clipped:name.scrollWidth>name.clientWidth},toolbar:{top:toolbar.top,bottom:toolbar.bottom,right:toolbar.right},open:{top:open.top,bottom:open.bottom,right:open.right},tableTop:table.top,bodyOverflow:document.documentElement.scrollWidth>innerWidth};
  });
  for(const t of metrics.tables){
   assert.equal(t.layout,'auto','LAYOUT_CONTENT_COLUMNS');assert.equal(t.overflow,'auto','LAYOUT_SCROLL_CONTAINER');
   assert.equal(t.actionWhiteSpace,'nowrap','LAYOUT_ACTIONS_NOWRAP');assert(t.actionMin>=98,'LAYOUT_ACTION_MINIMUM');
   for(const h of t.headers)assert.equal(h.whiteSpace,'nowrap','LAYOUT_HEADER_SINGLE_LINE');
   for(const b of t.buttons){assert.equal(b.whiteSpace,'nowrap','LAYOUT_BUTTON_SINGLE_LINE');assert.equal(new Set(b.textRects.map(r=>Math.round(r.y))).size,1,'LAYOUT_BUTTON_TEXT_GEOMETRY');}
   if(t.id==='rosterTable'){assert.equal(t.buttons.filter(b=>b.text!=='吊销').length,4,'LAYOUT_HOSTED_ACTION_COVERAGE');const ys=t.buttons.filter(b=>b.text!=='吊销').map(b=>Math.round(b.y));assert(new Set(ys).size<=1,'LAYOUT_HOSTED_ACTIONS_SAME_ROW');}
  }
  assert.equal(metrics.name.ellipsis,'ellipsis','LAYOUT_NAME_ELLIPSIS');assert(metrics.name.clipped,'LAYOUT_LONG_NAME_CLIPPED');assert(metrics.name.title.includes(long),'LAYOUT_FULL_NAME_TOOLTIP');
  assert.equal(await page.locator('#rosterItems [data-agent-id=self] td').nth(2).getAttribute('title'),'VM-4-12','LAYOUT_FULL_HOST_TOOLTIP');
  assert(metrics.open.bottom<=metrics.tableTop&&metrics.open.right>=metrics.toolbar.right-12,'LAYOUT_TOOLBAR_POSITION');assert(!metrics.bodyOverflow,'LAYOUT_NO_PAGE_OVERFLOW');
  if(width===768){assert(metrics.tables[0].scrollWidth>metrics.tables[0].wrapWidth,'LAYOUT_NARROW_SCROLL_REQUIRED');const distance=await page.locator('#rosterTable').evaluate(t=>{const w=t.parentElement;w.scrollLeft=w.scrollWidth;return w.scrollLeft;});assert(distance>0,'LAYOUT_NARROW_SCROLL_WORKS');}
  if(width===1440)assert(metrics.tables[1].width<1000,'LAYOUT_WIDE_SHORT_COLUMNS_COMPACT');
  await page.locator('#enrollTable').evaluate(t=>t.parentElement.scrollLeft=t.parentElement.scrollWidth);
  await page.locator('.enroll-open').click();await page.locator('#enrollEditor').waitFor({state:'visible'});await page.click('#enrollCancel');
  await page.locator('#rosterTable').evaluate(t=>t.parentElement.scrollLeft=t.parentElement.scrollWidth);
  await page.locator('.hosted-op[data-op=restart]').click();
  await page.waitForFunction(()=>!document.querySelector('.hosted-op[data-op=restart]').disabled);
  assert(writes.some(w=>w.path==='/api/console/hosted/agents/hosted/restart'),'LAYOUT_HOSTED_RESTART_CLICK');
  await page.locator('.roster-revoke:visible').click();await page.waitForFunction(()=>document.getElementById('rosterStatus').textContent==='');
  assert(writes.some(w=>w.path==='/api/console/agents/self/revoke'),'LAYOUT_REVOKE_CLICK');
  await page.locator('#helperUpdateOpen').click();await page.locator('#helperUpdateRows tr').first().waitFor();
  assert.equal(await page.locator('#helperUpdateRows [data-agent-id=old] button').isDisabled(),true,'LAYOUT_DISABLED_PERMISSION_UNCHANGED');
  await page.locator('#helperUpdateRows [data-agent-id=self] button').click();await page.waitForFunction(()=>document.getElementById('helperUpdateStatus').textContent.includes('等待升级确认'));
  const checkWidth=await page.locator('#helperUpdateRows input').first().evaluate(e=>e.getBoundingClientRect().width);assert(checkWidth<32,'LAYOUT_COMPACT_CHECKBOX');
  const panelOverflow=await page.evaluate(()=>{const status=document.getElementById('helperUpdateStatus'),workspace=status.closest('.group-workspace');return {root:document.documentElement.scrollWidth>innerWidth,workspace:workspace.scrollWidth>workspace.clientWidth,status:status.scrollWidth>status.clientWidth,wrap:getComputedStyle(status).overflowWrap};});assert.equal(panelOverflow.wrap,'anywhere','LAYOUT_STATUS_TEXT_WRAP');assert(!panelOverflow.root&&!panelOverflow.workspace&&!panelOverflow.status,'LAYOUT_EXPANDED_PANEL_NO_PAGE_OVERFLOW');
  assert.deepEqual(writes.find(w=>w.path==='/api/console/helper-updates').body,{release_sha256:SHA,agent_ids:['self']},'LAYOUT_HELPER_CLICK');
  await page.locator('#enrollReview').screenshot({path:path.join(out,`enroll-${width}.png`)});
  await page.locator('#rosterTable').evaluate(t=>t.parentElement.scrollLeft=0);
  await page.locator('#agentRoster').screenshot({path:path.join(out,`roster-${width}.png`)});
  await page.locator('#rosterTable').evaluate(t=>t.parentElement.scrollLeft=t.parentElement.scrollWidth);
  await page.locator('#agentRoster').screenshot({path:path.join(out,`roster-actions-${width}.png`)});
  await page.locator('#helperUpdatePanel').screenshot({path:path.join(out,`helper-${width}.png`)});
  assert.deepEqual(errors,[],'LAYOUT_NO_PAGE_ERRORS');results.push({width,metrics,writes});await page.close();console.log('PASS layout '+width);
 }
 fs.writeFileSync(path.join(out,'RESULTS.json'),JSON.stringify({mutation,widths:results},null,2)+'\n');console.log('LAYOUT_PASS 3 widths; screenshots; live handlers with synthetic HTTP');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
