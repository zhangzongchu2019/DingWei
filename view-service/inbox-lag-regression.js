// Isolated real renderer, mocked HTTP/SSE only; no real Hub or Agent traffic.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const {chromium} = require('playwright');
const html = fs.readFileSync(path.join(__dirname,'renderer-canary-sse.html'),'utf8');
(async()=>{
 const browser=await chromium.launch({channel:process.env.PW_CHANNEL||'chrome',headless:true,args:['--no-sandbox']});
 let checks=0;
 try {
  for(const width of [1280,390]) {
   const page=await browser.newPage({viewport:{width,height:900}});await page.clock.install();let lagPolls=0;const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.addInitScript(()=>{window.EventSource=class{static CONNECTING=0;static OPEN=1;static CLOSED=2;constructor(){this.readyState=1;}addEventListener(){}close(){this.readyState=2;}};});
   let sample={status:'ok',source:'helper_self_report',agent_id:'fixture',connection_seq:73,connection_generation:'c'.repeat(32),sample_instance:'a'.repeat(32),sample_seq:1,disk_not_admitted_count:2,dispatch_unknown_count:1,oldest_wait_ms:2400000,remaining_fresh_ms:10000,clock_uncertain:false};
   await page.route('https://fixture.invalid/**',async route=>{
    const url=new URL(route.request().url());
    if(url.pathname==='/view/seat/')return route.fulfill({contentType:'text/html',body:html});
    if(url.pathname.endsWith('/status')&&url.searchParams.get('inbox_lag_only')==='1'){lagPolls++;return route.fulfill({contentType:'application/json',body:JSON.stringify({agent_id:'fixture',inbox_lag:sample})});}
    if(url.pathname.endsWith('/status'))return route.fulfill({contentType:'application/json',body:JSON.stringify({session:'seat',session_id:'fixture',session_name:'seat',agent_id:'fixture',readonly:true,auth_mode:'legacy',agent_disk:{agent_id:'fixture',source:'helper_self_report',status:'unavailable',inbox_lag:sample}})});
    return route.fulfill({status:404,body:''});
   });
   await page.goto('https://fixture.invalid/view/seat/');
   const strip=page.locator('#inboxLagStat');
   await strip.waitFor({state:'visible'});
   await page.waitForFunction(()=>document.getElementById('inboxLagStat').textContent==='已到盘未注入 2 条，最老等待 40 分钟；本次启动另有 1 条注入状态未知');checks++;
   assert.deepEqual(errors,[]);checks++;
   // Run the actual scheduled five-second poll, then let duplicate samples
   // expire via the scheduled local clock callback without changing deadlines.
   sample={...sample,sample_seq:2,disk_not_admitted_count:3,oldest_wait_ms:720000};
   await page.clock.runFor(5000);await page.evaluate(()=>inboxLagPending);
   assert(lagPolls>=1);assert.equal(await strip.textContent(),'已到盘未注入 3 条，最老等待 12 分钟；本次启动另有 1 条注入状态未知');checks++;
   await page.clock.runFor(10000);await page.evaluate(()=>inboxLagPending);
   assert.equal(await strip.textContent(),'排队采样已过期');checks++;
   sample={status:'unknown'};await page.evaluate(()=>refreshStatus());
   assert.equal(await strip.textContent(),'注入状态未知');checks++;
   sample={status:'ok',source:'helper_self_report',agent_id:'fixture',connection_seq:74,connection_generation:'d'.repeat(32),sample_instance:'b'.repeat(32),sample_seq:1,disk_not_admitted_count:0,dispatch_unknown_count:0,oldest_wait_ms:null,remaining_fresh_ms:10000};
   await page.evaluate(()=>refreshInboxLag());assert.equal(await strip.textContent(),'已到盘未注入 0 条');checks++;
   sample={...sample,agent_id:'other',sample_seq:2,disk_not_admitted_count:99,oldest_wait_ms:0};
   await page.evaluate(()=>refreshInboxLag());assert.equal(await strip.textContent(),'注入状态未知');checks++;
   assert.equal(await page.locator('#inboxLagStat script').count(),0);checks++;
   const before=lagPolls;await page.evaluate(()=>Object.defineProperty(document,'hidden',{configurable:true,get:()=>true}));
   await page.clock.runFor(5000);assert.equal(lagPolls,before);checks++;
   await page.evaluate(()=>closeViewRefresh());await page.clock.runFor(5000);assert.equal(lagPolls,before);checks++;
   await page.close();
  }
  console.log(JSON.stringify({checks,viewports:[1280,390],result:'PASS',actual_renderer:true,isolated_transport:true}));
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
