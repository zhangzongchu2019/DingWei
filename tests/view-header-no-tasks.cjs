// Full renderer in real Chromium, all HTTP intercepted. No real Hub or browser credentials.
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const {chromium}=require('playwright');
const file=path.resolve(process.argv[2]||'view-service/renderer-canary-sse.html');
const html=fs.readFileSync(file,'utf8');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PW_CHANNEL||'chrome'});
 try{
  const context=await browser.newContext(),page=await context.newPage();
  await page.route('**/*',route=>{
   const u=new URL(route.request().url());
   if(u.origin!=='https://header.invalid')return route.abort();
   if(u.pathname==='/view/test')return route.fulfill({contentType:'text/html',body:html});
   if(u.pathname.endsWith('/status'))return route.fulfill({contentType:'application/json',body:JSON.stringify({session:'test',state:'idle',readonly:false})});
   return route.fulfill({status:404,body:''});
  });
  await page.goto('https://header.invalid/view/test');
  assert.equal(await page.locator('header.topbar').count(),1);
  assert.equal(await page.locator('header.topbar').isVisible(),true);
  assert.equal(await page.locator('header.topbar .brand #headerSessionName').count(),1);
  const links=await page.locator('header.topbar a[href]').evaluateAll(nodes=>nodes.filter(n=>new URL(n.href).pathname==='/console/tasks').map(n=>n.getAttribute('href')));
  assert.deepEqual(links,[],'View topbar must not contain a /console/tasks link');
  console.log('PASS actual View header retains brand and has no duplicate task link');
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.locator('header.topbar').isVisible(),true);
  assert.equal(await page.locator('header.topbar .brand').isVisible(),true);
  assert.equal(await page.locator('header.topbar a[href="/console/tasks"]').count(),0);
  console.log('PASS narrow View header still retains brand without task link');
  console.log('TOTAL 2 Chromium header checks PASS; intercepted HTTP only');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
