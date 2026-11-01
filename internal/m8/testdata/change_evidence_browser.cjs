const assert=require('node:assert/strict');
const {chromium}=require(process.env.CHANGEFLOW_PLAYWRIGHT);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:'/usr/bin/google-chrome',args:['--no-sandbox','--disable-dev-shm-usage']});
 try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(String(e)));await page.goto(process.env.CHANGEFLOW_BASE+'/console/change-evidence');
 const coordinate={provider:'github',git_instance:'github.com',repository:'owner/repo',change_dir:'openspec/changes/demo',change_id:'42',commit_sha:'a'.repeat(40)};
 for(const [name,v] of Object.entries(coordinate)){const el=page.locator('[name="'+name+'"]');if(name==='provider')await el.selectOption(v);else await el.fill(v);}
 await page.locator('#coordinates button').click();await page.locator('#actions').waitFor({state:'visible'});
 await page.locator('#detect').click();await page.waitForFunction(()=>document.querySelectorAll('#content article').length===1);
 await page.getByRole('button',{name:'已知悉该修订'}).click();await page.waitForFunction(()=>!Array.from(document.querySelectorAll('#content button')).some(b=>b.textContent.includes('已知悉')));
 await page.evaluate(()=>fetch('/test/labels',{method:'POST'}));await page.locator('#detect').click();await page.waitForFunction(()=>document.querySelectorAll('#content article').length===2);assert.equal(await page.getByRole('button',{name:'已知悉该修订'}).count(),1,'changed set requires new acknowledgement');
 assert.ok((await page.locator('#content article').first().textContent()).includes('manager'),'old knowledge retained');
 for(const phase of ['SPEC_REVIEW','SPEC_APPROVED','IMPLEMENTING','ACCEPTANCE']){await page.locator('#next').selectOption(phase);await page.locator('#advance').click();await page.waitForFunction(p=>document.getElementById('content').textContent.includes('阶段：'+p),phase);}
 for(const [id,status] of [['S01','PASS'],['S02','NOT_RUN']]){await page.locator('#scenario').fill(id);await page.locator('#result-status').selectOption(status);await page.locator('#tested').fill('a'.repeat(40));await page.locator('#evidence').fill('report:case/'+id);await page.locator('#reason').fill(status==='NOT_RUN'?'deferred with report':'');await page.locator('#result').click();await page.waitForFunction(s=>document.getElementById('content').textContent.includes('"Scenario": "'+s+'"'),id);}
 await page.locator('#next').selectOption('ARCHIVED');await page.locator('#advance').click();await page.waitForFunction(()=>document.getElementById('content').textContent.includes('阶段：ARCHIVED'));assert.ok((await page.locator('#content').textContent()).includes('全部场景已执行通过：否'),'unexecuted not washed green');assert.ok((await page.locator('#content').textContent()).includes('deferred with report'));
 await page.locator('#next').selectOption('RELEASED');await page.locator('#environment').fill('staging');await page.locator('#advance').click();await page.waitForFunction(()=>document.getElementById('content').textContent.includes('阶段：RELEASED'));assert.ok((await page.locator('#content').textContent()).includes('staging'));
 await page.evaluate(()=>fetch('/test/revoke',{method:'POST'}));await page.locator('#detect').click();await page.waitForFunction(()=>document.getElementById('content').childElementCount===0);assert.equal(await page.locator('#actions').isHidden(),true,'revoked data/actions cleared');assert.deepEqual(errors,[]);
 console.log('PASS actual Console + service + SQLite: detect, acknowledgement/reset/history, ordinary without R7, four-state evidence, archive/release environment, revoked cache');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
