// Actual Specs page in Chromium; intercepted HTTP, no production or real deletion.
const assert=require('node:assert/strict'),fs=require('fs'),http=require('http'),path=require('path');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'..');
const source=fs.readFileSync(process.argv[2]||path.join(root,'internal/m8/console_specs_page.go'),'utf8');
const start='const consoleSpecsPageHTML = `',end='`\n\nfunc (h *Hub) HandleConsoleSpecsPage';
assert(source.includes(start)&&source.includes(end));
let html=source.slice(source.indexOf(start)+start.length,source.indexOf(end,source.indexOf(start)));
html=html.replace('<script id=dw-md-core></script>',()=>'<script>'+fs.readFileSync(path.join(__dirname,'md-render-core.js'),'utf8')+'</script>');
const specs=[{id:'spec-a',title:'Agent A spec',state:'editing',current_revision:1,current_editor_short_name:'Owner',can_edit:true,can_delete:true,archived:false},{id:'spec-b',title:'Agent B spec',state:'editing',current_revision:1,current_editor_short_name:'Owner',can_edit:false,can_delete:true,archived:false},{id:'spec-other',title:'Other visible spec',state:'editing',current_revision:1,can_edit:false,can_delete:false,archived:false}];
const calls=[];let batchFails=false;
function json(res,value,status=200){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));}
const server=http.createServer(async(req,res)=>{
    // R8 lookup added to the spec page; this legacy fixture has no active requests.
    if(req.method==='GET' && /^\/api\/console\/specs\/[^/?]+\/agent-review(?:\?|$)/.test(req.url)){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({requests:[],agents:[]}));return;}

 const url=new URL(req.url,'http://local');let body='';for await(const chunk of req)body+=chunk;
 calls.push({path:url.pathname,method:req.method,query:url.search,body});
 if(url.pathname==='/console/specs'){res.writeHead(200,{'Content-Type':'text/html; charset=utf-8'});return res.end(html);}
 if(url.pathname.startsWith('/console/spec-assets/')){const name=path.basename(url.pathname);if(!['easymde.min.js','easymde.min.css'].includes(name)){res.writeHead(404);return res.end();}return res.end(fs.readFileSync(path.join(root,'internal/m8/static/easymde',name)));}
 if(url.pathname==='/api/console/specs'&&req.method==='GET')return json(res,{role:'owner',specs:specs.filter(x=>!x.archived||url.searchParams.get('include_archived')==='1'),page:1,total_pages:1,agents:[]});
 if(url.pathname==='/api/console/specs/archive'&&req.method==='POST'){
  const ids=JSON.parse(body).spec_ids;if(batchFails)return json(res,{error:'synthetic batch failure'},409);
  ids.forEach(id=>{specs.find(s=>s.id===id).archived=true;});res.writeHead(204);return res.end();
 }
 const match=url.pathname.match(/^\/api\/console\/specs\/([^/]+)(\/unarchive)?$/);
 if(match){const spec=specs.find(s=>s.id===match[1]);if(!spec){res.writeHead(404);return res.end();}
  if(req.method==='DELETE'){spec.archived=true;res.writeHead(204);return res.end();}
  if(match[2]&&req.method==='POST'){spec.archived=false;res.writeHead(204);return res.end();}
  return json(res,{spec,revision:{content_markdown:'# actual content',acceptance_json:'[]',input_anchors_json:'[]'},attribution:{creator:'Agent A',created_at:'2026-10-15T01:02:03Z',last_editor:'Agent A',last_edited_at:'2026-10-15T02:03:04Z'},tasks:[],dependency_candidates:[]});
 }
 res.writeHead(404);res.end();
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({channel:process.env.PW_CHANNEL||'chrome',headless:true});let checks=0;
 try{
  const page=await browser.newPage(),errors=[];page.setDefaultTimeout(4000);page.on('pageerror',e=>errors.push(e.message));
  await page.goto('http://127.0.0.1:'+server.address().port+'/console/specs');await page.locator('#spec-list tr').last().waitFor();
  const row=id=>page.locator('#spec-list tr').filter({has:page.locator('.spec-title',{hasText:id})});
  assert.equal(await page.locator('.delete-spec').count(),2,'owner rows must offer deletion');assert.equal(await row('Other visible').locator('.delete-spec,.select-spec').count(),0);checks++;
  page.once('dialog',d=>d.dismiss());await row('Agent A').locator('.delete-spec').click();assert.equal(calls.filter(c=>c.method==='DELETE').length,0);checks++;
  page.once('dialog',d=>d.accept());await row('Agent A').locator('.delete-spec').click();await page.waitForFunction(()=>document.querySelectorAll('#spec-list tr').length===2);assert(specs[0].archived);checks++;
  await page.locator('#show-archived').check();await page.waitForFunction(()=>document.querySelectorAll('#spec-list tr').length===3);assert(calls.some(c=>c.query.includes('include_archived=1')));assert.equal(await row('Agent A').locator('.restore-spec').count(),1);checks++;
  page.once('dialog',d=>d.accept());await row('Agent A').locator('.restore-spec').click();await page.waitForFunction(()=>!document.querySelector('.restore-spec'));assert.equal(specs[0].archived,false);checks++;
  await row('Agent A').locator('.select-spec').check();await row('Agent B').locator('.select-spec').check();batchFails=true;
  page.once('dialog',d=>d.accept());await page.locator('#delete-selected-specs').click();await page.waitForFunction(()=>document.querySelector('#spec-delete-status').textContent.includes('synthetic batch failure'));assert(specs.every(s=>!s.archived));checks++;
  batchFails=false;page.once('dialog',d=>d.accept());await page.locator('#delete-selected-specs').click();await page.waitForFunction(()=>document.querySelectorAll('.restore-spec').length===2);assert.deepEqual(JSON.parse(calls.filter(c=>c.path.endsWith('/archive')).at(-1).body).spec_ids.sort(),['spec-a','spec-b']);checks++;
  await row('Agent A').locator('.spec-title').click();await page.locator('#detail-screen').waitFor({state:'visible'});assert.equal(await page.locator('#spec-creator').textContent(),'Agent A');assert.match(await page.locator('#spec-created-at').textContent(),/2026-10-15/);assert.equal(await page.locator('#spec-last-editor').textContent(),'Agent A');checks++;
  assert.deepEqual(errors,[]);console.log('A3 direct Specs browser '+checks+' checks PASS (HTTP intercepted)');
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
