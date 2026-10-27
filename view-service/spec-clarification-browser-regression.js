// Exact production HTML, loopback fixture only. No credentials or live traffic.
const assert=require('assert'),fs=require('fs'),http=require('http'),path=require('path'),{chromium}=require('playwright');
const source=fs.readFileSync(path.join(__dirname,'../internal/m8/spec_clarification_page.go'),'utf8');
const html=source.slice(source.indexOf('<!doctype html>'),source.lastIndexOf('`'));
let actor='user:pm',role='pm',enabled=true,deny=false,failAfterCommit=false,posts=[],requests=[];
const cache=new Map();let checks=0;
function json(res,status,data){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data))}
const server=http.createServer(async(req,res)=>{
 const u=new URL(req.url,'http://fixture');
 if(u.pathname==='/console/spec-clarifications'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(html)}
 if(deny)return json(res,403,{error:'forbidden'});
 if(u.pathname==='/api/console/specs/spec/clarification-policy')return json(res,role==='pm'?200:403,{policy:{spec_id:'spec',pm_user:'pm',deputy_user:'deputy',version:2},can_select_pm:false,can_select_deputy:true,enabled});
 const base='/api/console/questions/q/clarifications';
 if(u.pathname.startsWith(base+'/requests/')){const result=cache.get(decodeURIComponent(u.pathname.slice((base+'/requests/').length)));return result?json(res,200,result):json(res,404,{error:'request_not_recorded'})}
 if(u.pathname===base){
  if(req.method==='GET')return json(res,200,{context:{actor,spec_id:'spec',revision:3,content_sha256:'pinned-sha',question_id:'q',title:'<script>window.INJECTED=1</script>',question:'请解释条目 A1，保持原范围。',expected_answer:'可核对的结论',role,entry_refs:['A1','A1/case1'],archived:false,policy_version:2},posts:posts.slice().reverse(),next_before:0,enabled});
  let raw='';for await(const c of req)raw+=c;const b=JSON.parse(raw);requests.push(b);
  if(cache.has(b.request_id))return json(res,200,{...cache.get(b.request_id),replayed:true});
  const post={...b,floor:posts.length+1,role,actor,created_at:'2026-10-21T05:00:00Z',source:'console'};posts.push(post);const result={post,replayed:false,execution_authorized:false};cache.set(b.request_id,result);
  if(failAfterCommit){failAfterCommit=false;return json(res,503,{error:'response_lost_after_commit'})}return json(res,200,result);
 }
 return json(res,404,{error:'not_found'});
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch(process.env.PW_CHANNEL?{headless:true,channel:process.env.PW_CHANNEL}:{headless:true});const errors=[];
 try{
  for(const width of [390,820]){
   const page=await browser.newPage({viewport:{width,height:1000},hasTouch:true,isMobile:true});page.on('pageerror',e=>errors.push(e.message));
   await page.goto(origin+'/console/spec-clarifications?question_id=q');await page.waitForSelector('#compose:not([hidden])');
   assert.equal(await page.evaluate(()=>window.INJECTED),undefined);assert((await page.locator('#title').innerText()).includes('<script>'));checks++;
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   for(const id of ['submit','lookup','kind','refs','refresh']){const box=await page.locator('#'+id).boundingBox();assert(box&&box.height>=44,`${width} ${id} touch size`)}checks++;
   assert((await page.locator('#spec-link').getAttribute('href')).includes('/console/specs/spec?clarification_readonly=1'));assert((await page.locator('#change-link').getAttribute('href')).includes('predecessor_spec_id=spec'));checks++;
   await page.fill('#body','解释不改变范围');await page.click('#submit');await page.waitForFunction(()=>document.querySelector('#status').textContent.includes('已入第'));const first=requests.at(-1);checks++;
   const expectedCount=posts.length+1;await page.click('#submit');await page.waitForFunction(n=>document.querySelectorAll('#posts article').length===n,expectedCount);assert.notEqual(requests.at(-1).request_id,first.request_id);checks++;
   await page.selectOption('#kind','supplement');await page.fill('#body','显式补充原范围');await page.click('#submit');await page.waitForFunction(()=>Array.from(document.querySelectorAll('#posts article')).some(a=>a.querySelector('.authority').textContent.includes('补充')&&a.querySelector('.body').textContent==='显式补充原范围'));assert.equal(requests.at(-1).kind,'supplement');checks++;
   await page.selectOption('#kind','scope_change');assert((await page.locator('#scope-hint').innerText()).includes('不能用一句话批准'));checks++;
   await page.selectOption('#kind','correction');await page.fill('#target','1');await page.fill('#reason','更正边界表述');await page.fill('#body','仅追加更正');await page.click('#submit');await page.waitForFunction(()=>document.querySelector('#posts').textContent.includes('更正第 1 条'));assert.equal(requests.at(-1).corrects,1);assert.equal(posts[0].body,'解释不改变范围');checks++;
   if(process.env.SCREENSHOT_DIR){fs.mkdirSync(process.env.SCREENSHOT_DIR,{recursive:true});await page.screenshot({path:path.join(process.env.SCREENSHOT_DIR,`clarification-${width}.png`),fullPage:true})}
   await page.close();
  }
  const page=await browser.newPage({viewport:{width:390,height:1000}});page.on('pageerror',e=>errors.push(e.message));await page.goto(origin+'/console/spec-clarifications?question_id=q');await page.waitForSelector('#compose:not([hidden])');
  failAfterCommit=true;await page.fill('#body','弱网提交保留原标识');await page.click('#submit');await page.waitForFunction(()=>document.querySelector('#status').textContent.includes('待确认'));const uncertain=requests.at(-1),count=posts.length;checks++;
  await page.reload();await page.waitForFunction(()=>document.querySelector('#body').value==='弱网提交保留原标识');checks++;
  await page.fill('#body','不能在未知结果时另发一笔');await page.click('#submit');await page.waitForFunction(()=>!document.querySelector('#submit').disabled);assert.equal(posts.length,count,'MOBILE_UNKNOWN_REKEY_DUPLICATED');assert((await page.locator('#status').innerText()).includes('不能换内容'));checks++;
  await page.click('#lookup');await page.waitForFunction(()=>document.querySelector('#status').textContent.includes('没有重复新增'));assert.equal(posts.length,count);assert(cache.has(uncertain.request_id));checks++;
  actor='user:deputy';role='deputy';await page.reload();await page.waitForSelector('#compose:not([hidden])');await page.waitForTimeout(100);assert(!(await page.locator('#question').isHidden()));assert(await page.locator('#policy').isHidden());assert.equal(await page.locator('#body').inputValue(),'');assert.equal(await page.locator('#kind option[value="confirmation"]').count(),0);checks++;
  deny=true;await page.click('#refresh');await page.waitForFunction(()=>document.querySelector('#question').hidden);assert.equal(await page.locator('#excerpt').innerText(),'');assert.equal(await page.locator('#posts').innerText(),'');checks++;
  deny=false;enabled=false;await page.reload();await page.waitForSelector('#question:not([hidden])');assert(await page.locator('#compose').isHidden());assert(await page.locator('#posts article').count()>0);checks++;
  await page.close();assert.deepEqual(errors,[]);checks++;
  console.log(JSON.stringify({suite:'spec-clarification',passed:checks,failed:0,widths:[390,820],live_access:false}));
 }finally{await browser.close();await new Promise(r=>server.close(r))}
})().catch(e=>{console.error(e.stack);server.close();process.exitCode=1});
