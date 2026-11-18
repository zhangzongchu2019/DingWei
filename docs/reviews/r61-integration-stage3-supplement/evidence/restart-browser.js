const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const ROOT='/data/ai-dev-data/r61-stage3-probe';
const {startRendererServer,launchChromium,finishRegression}=require(path.join(ROOT,'view-service/renderer-regression-selfhost'));
(async()=>{
 const input=process.env.STAGE3_RESTART_RESULT;assert(input,'explicit private result path required');
 const r=JSON.parse(fs.readFileSync(input,'utf8'));assert(r.different_os_processes&&r.hub_process_before!==r.hub_process_after);assert(r.full_event_equality&&r.trusted_agent_and_timer_labels);
 const fixture=await startRendererServer();let browser;const readings=[];
 try{
  browser=await launchChromium();
  for(const stage of ['before','after']){
   const page=await browser.newPage();const history=r[stage];
   await page.route('**/status',q=>q.fulfill({status:200,contentType:'application/json',body:JSON.stringify({session:'Kimi.zzc',state:'idle',readonly:true,can_write:false,owner:'fixture',model:'fixture'})}));
   await page.route('**/events',q=>q.fulfill({status:200,contentType:'text/event-stream',body:''}));
   await page.route('**/history',q=>q.fulfill({status:200,contentType:'application/json',body:JSON.stringify(history)}));
   await page.addInitScript(()=>{window.PROVENANCE_ICONS_V1=true;});
   await page.goto(fixture.url+'?mock=1&unlocked=1',{waitUntil:'domcontentloaded'});await page.waitForTimeout(2200);
   assert.equal(await page.locator('.prov-ic').count(),2,stage+': agent and timer trusted icons required from history only');
   for(const name of ['agent','timer'])assert.equal(await page.locator('.turn-group[data-turn-key="restart-turn-'+name+'"]').count(),1,stage+': one persisted turn per source');
   const titles=(await page.locator('.prov-ic').evaluateAll(xs=>xs.map(x=>x.title))).sort();assert(titles.some(x=>/opus50ts/i.test(x)),stage+': canonical Agent display missing');assert(titles.some(x=>/timer/i.test(x)),stage+': canonical timer display missing');
   readings.push({stage,icons:2,turns:2,titles,sse_replay:false,fresh_browser_context:true});await page.close();
  }
  assert.deepEqual(readings[0].titles,readings[1].titles,'source icon titles changed after cold Hub restart');
  const out={source:r.source,rc:0,different_hub_os_processes:true,full_event_and_provenance_equality:true,readings,method:'actual Hub authenticated HTTP snapshots rendered by production renderer in separate fresh pages; live SSE blank; synthetic protocol events no CLI'};
  fs.writeFileSync(process.env.STAGE3_BROWSER_OUTPUT,JSON.stringify(out,null,2)+'\n');console.log('HISTORY_RESTART_BROWSER_OK before_icons=2 after_icons=2 provenance_fields_and_titles_equal=true');
 }finally{if(browser)await browser.close();await fixture.close();}
})().catch(finishRegression);
