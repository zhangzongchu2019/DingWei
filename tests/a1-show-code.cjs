// Full Console source in Chromium; all HTTP intercepted, no real membership writes.
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const {chromium}=require('playwright');
const source=fs.readFileSync(process.argv[2]||path.join(__dirname,'../internal/m8/console_auth.go'),'utf8');
const start=source.indexOf('const consolePageHTML = `'),end=source.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents',start);
assert(start>=0&&end>start);const html=source.slice(start+'const consolePageHTML = `'.length,end);
const code='AbCdEf0123456789_-AbCdEf01234567';assert.equal(code.length,32);
let mode='manage',writes=0;const errors=[];
const payload=()=>({groups:mode==='empty'?[]:[{code,short_name:'Share',can_manage:mode==='manage',leader_key_id:'key',leader_session_name:'lead',members:[]}],owned_agents:[{id:'own',session_name:'mine',online:true}]});
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 try{
 const context=await browser.newContext();await context.grantPermissions(['clipboard-read','clipboard-write'],{origin:'https://console.invalid'});
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',async r=>{
  const req=r.request(),u=new URL(req.url());if(u.origin!=='https://console.invalid')return r.abort();
  if(req.method()!=='GET'){writes++;return r.fulfill({status:403,body:'no live writes'});}
  if(u.pathname==='/console')return r.fulfill({contentType:'text/html',body:html});
  const body=u.pathname==='/api/console/platform'?{role:'owner',actions:[],items:[]}:u.pathname==='/api/console/agent-groups'?payload():{agents:[],applications:[],requests:[],items:[]};
  return r.fulfill({contentType:'application/json',body:JSON.stringify(body)});
 });
 await page.goto('https://console.invalid/console');await page.evaluate(()=>openGroupWorkspace());
 await page.locator('.group-row').first().click();
 assert.equal(await page.locator('#groupShareCode').isVisible(),true,'manager must see group code');
 assert.equal(await page.locator('#selectedGroupCode').textContent(),code);assert.match(code,/^[a-zA-Z0-9_-]{32}$/);
 await page.locator('#groupJoinCode').fill(code);assert.equal(await page.locator('#groupJoinCode').inputValue(),code);
 console.log('PASS manager sees exact case-sensitive 32-character join-compatible code');
 await page.locator('#copyGroupCode').click();await page.waitForFunction(()=>document.getElementById('groupCodeCopyFeedback').textContent==='已复制');
 assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),code);
 console.log('PASS explicit copy uses real Chromium clipboard and reports success');
 assert.match(await page.locator('#groupShareCode').textContent(),/组码（邀请码）/);
 assert.match(await page.locator('#groupShareCode').textContent(),/把组码发给其他业主/);
 assert.match(await page.locator('#groupShareCode').textContent(),/关闭添加/);
 assert.equal(await page.locator('#groupAdditionsToggle').isVisible(),true);
 mode='member';await page.evaluate(()=>refreshGroups('test'));assert.equal(await page.locator('#groupShareCode').isVisible(),true);
 assert.equal(await page.locator('#selectedGroupCode').textContent(),code);
 await page.evaluate(()=>navigator.clipboard.writeText('before-member-copy'));
 await page.locator('#copyGroupCode').click();await page.waitForFunction(()=>document.getElementById('groupCodeCopyFeedback').textContent==='已复制');
 assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),code);
 assert.equal(await page.locator('#groupAdditionsToggle').isVisible(),false);
 assert.equal(await page.locator('#groupMemberAdd').isVisible(),false);
 assert.equal(await page.locator('.group-delete').count(),0);
 console.log('PASS visible nonmanager member owner can see/copy invitation code without management controls');
 mode='manage';await page.evaluate(()=>refreshGroups('test'));
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async()=>{throw Error('denied');}}}));
 await page.locator('#copyGroupCode').click();await page.waitForFunction(()=>document.getElementById('groupCodeCopyFeedback').textContent.includes('复制失败'));
 assert.equal(await page.locator('#selectedGroupCode').textContent(),code);
 console.log('PASS clipboard failure gives truthful manual-copy feedback');
 mode='empty';await page.evaluate(()=>refreshGroups('test'));assert.equal(await page.locator('#selectedGroupCode').textContent(),'');assert.equal(await page.locator('#groupShareCode').isVisible(),false);
 assert.equal(await page.locator('#copyGroupCode').isDisabled(),true);await page.evaluate(()=>document.getElementById('copyGroupCode').click());assert.equal(await page.locator('#groupCodeCopyFeedback').textContent(),'');
 assert.equal(writes,0);assert.deepEqual(errors,[]);
 console.log('PASS no groups clears code; no writes or page errors');
 console.log('TOTAL 5 PASS (full Console, intercepted HTTP, real local clipboard)');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
