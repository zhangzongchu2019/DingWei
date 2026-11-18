// N-3: real authenticated Agent/timer ingress -> history SQLite -> HTTP Page ->
// renderer refresh. Browser receives only the admitted history row, never a
// canonical input_posted from live memory. Synthetic test identities only.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

(async () => {
 const root=path.resolve(__dirname,'..');
 const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'n3-browser-'));
 const output=path.join(tmp,'hub-history.json');
 const env={HOME:tmp,TMPDIR:tmp,PATH:process.env.PATH,LANG:'C.UTF-8',GOPROXY:'off',
   GOPATH:process.env.GOPATH||path.join(os.homedir(),'go'),
   GOMODCACHE:process.env.GOMODCACHE||path.join(os.homedir(),'go/pkg/mod'),
   GOCACHE:process.env.GOCACHE||path.join(os.homedir(),'.cache/go-build'),
   HUB_TEST_PG_STRICT:'1',N3_HISTORY_FIXTURE_OUTPUT:output};
 const run=spawnSync('go',['test','./internal/m8','-run','^TestHistoryProvenanceRealHubAgentTimerReplay$','-count=1','-v'],{cwd:root,env,encoding:'utf8',timeout:180000});
 assert.equal(run.status,0,`real Hub history export failed:\n${run.stdout}\n${run.stderr}`);
 const fixtures=JSON.parse(fs.readFileSync(output,'utf8'));
 const fixture=await startRendererServer();let browser;
 try {
  browser=await launchChromium();const checks=regressionCollector();
  for(const name of ['agent','timer']){
   const record=fixtures[name];assert(record.sse.includes('id: '));assert(record.sse.includes(record.message_id));
   const page=await browser.newPage();let history=record.history;
   await page.route('**/status',r=>r.fulfill({status:200,contentType:'application/json',body:JSON.stringify({session:'Kimi.zzc',state:'idle',readonly:true,can_write:false,owner:'fixture',model:'fixture'})}));
   await page.route('**/events',r=>r.fulfill({status:200,contentType:'text/event-stream',body:''}));
   await page.route('**/history',r=>r.fulfill({status:200,contentType:'application/json',body:JSON.stringify(history)}));
   await page.addInitScript(()=>{window.PROVENANCE_ICONS_V1=true;});
   const refresh=async()=>{await page.goto(`${fixture.url}?mock=1&unlocked=1`,{waitUntil:'domcontentloaded'});await page.waitForTimeout(2200);};
   await refresh();
   await checks.check(`${name}: DB/HTTP history only, refresh icon_count==1`,async()=>{
    assert.equal(await page.locator('.prov-ic').count(),1,`${name}: no canonical live memory; persisted provenance must supply exactly one icon`);
    assert.equal(await page.locator(`.turn-group[data-turn-key="${record.turn_id}"]`).count(),1);
    assert.equal(await page.locator('.prov-ic[title*="'+(name==='timer'?'timer':'opus50ts')+'" i]').count(),1);
   });
   for(const negative of ['unattested','untrusted','absent']){
    history=JSON.parse(JSON.stringify(record.history));
    const event=history.events[0].event;
    if(negative==='absent')delete event.provenance;
    else if(negative==='unattested')event.provenance.attested_by='client';
    else event.provenance.trust_decision='unknown';
    // Decoy source never substitutes for canonical Hub evidence.
    event.data.source='agent';event.data.display_name='timer';
    await refresh();
    await checks.check(`${name}/${negative}: history icon_count==0`,async()=>{assert.equal(await page.locator('.prov-ic').count(),0,'unproven history must not render a trusted icon');});
   }
   console.log(JSON.stringify({case:name,message_id:record.message_id,delivery_id:record.delivery_id,origin_delivery_id:record.origin_delivery_id,turn_id:record.turn_id,history_event_id:record.history.events[0].event_id,sse_ids:record.sse.split('\n').filter(x=>x.startsWith('id: '))}));
   await page.close();
  }
  checks.finish();console.log('history-provenance-browser-regression: 8/8 PASS');
 }finally{if(browser)await browser.close();await fixture.close();fs.rmSync(tmp,{recursive:true,force:true});}
})().catch(finishRegression);
