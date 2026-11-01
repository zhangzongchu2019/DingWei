// Actual Console HTML + mux broker in Chrome; only HTTP dependencies are fixtures.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const {chromium} = require('playwright');
const root = path.resolve(__dirname, '..');
const mode = process.argv[2] || '';
const readSource = file => process.env.CONSOLE_SOURCE_REF ? require('node:child_process').execFileSync('git',['show',process.env.CONSOLE_SOURCE_REF+':'+file],{cwd:root,encoding:'utf8'}) : fs.readFileSync(path.join(root,file),'utf8');
const source = readSource('internal/m8/console_auth.go');
const start = 'const consolePageHTML = `';
let html = source.slice(source.indexOf(start) + start.length, source.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents'));
let broker = readSource('internal/m8/console_mux_broker.js');
if(mode==='--mutate=close-guard'){
 const guard="function cleanup(label,fn){try{fn();}catch(err){console.warn('Console tab cleanup failed: '+label,err);}}";
 assert.ok(html.includes(guard),'close guard mutation anchor');
 html=html.replace(guard,'function cleanup(label,fn){fn();}');
}
if(mode==='--mutate=release-fallback'){
 const call='window.DWConsoleMux.release(session);';assert.ok(html.includes(call),'release mutation anchor');
 html=html.replace(call,'void session;');
}

// Test-only readout: actual production maps, queue credits and transport reference.
broker = broker.replace('})();', 'window.__muxSnapshot=()=>({seats:Array.from(seats.keys()),ready:Array.from(seats.values()).filter(s=>s.ready&&s.generation).map(s=>s.name),count:totalCount,bytes:totalBytes,source:!!source,transportReady:!!source&&source.readyState===1,reconnect:!!reconnect});})();');
const streams = new Set(), children = new Set(); let subscriptions = [], revision = 0;
function packet(res, p) { res.write('data: '+JSON.stringify(p)+'\n\n'); }
const server = http.createServer(async(req,res)=>{
 const u = new URL(req.url, 'http://localhost');
 function json(v) { res.writeHead(200, {'Content-Type':'application/json'});res.end(JSON.stringify(v)); }
 if(u.pathname==='/console') { const mux=u.searchParams.get('mux')!=='0';res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});res.end(html.replace('const CONSOLE_MUX_ENABLED = false;', 'const CONSOLE_MUX_ENABLED = '+mux+';')+(mux?'<script>'+broker+'</script>':''));return; }
 if(u.pathname==='/api/console/agents') return json({agents:[]});
 if(u.pathname==='/api/console/platform') return json({items:[],actions:[],role:'owner'});
 if(u.pathname==='/api/console/event-context') return json({auth_context:'local-context'});
 if(u.pathname==='/api/console/event-feeds' || u.pathname==='/api/console/event-feeds/feed') {
  let body='';for await(const chunk of req)body+=chunk;
  const v=body?JSON.parse(body):{}; if(!v.ack_only)subscriptions=v.subscriptions||[];
  return json({feed_id:'feed',revision:++revision,subscriptions:subscriptions.map(s=>({...s,agent_id:s.full_name,generation:'g',replay_round:'r'}))});
 }
 if(u.pathname.endsWith('/events') || u.pathname==='/child-events') {
  const set=u.pathname==='/child-events'?children:streams;set.add(res);
  res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});res.write(': connected\n\n');
  req.on('close',()=>set.delete(res));return;
 }
 if(u.pathname.startsWith('/view/')) {
  res.writeHead(200,{'Content-Type':'text/html'});
  res.end('<textarea>retained draft</textarea><script>const p=new URLSearchParams(location.search);if(p.get("console_mux")){parent.postMessage({protocol:"dw-console-mux/v1",type:"ready",iframe_instance:p.get("iframe_instance")},location.origin);}else{window.stream=new EventSource("/child-events");}</script>');return;
 }
 json({});
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',args:['--no-sandbox','--disable-dev-shm-usage']});
 const results=[];
 try {
 for(const scenario of ['disconnected','remove-throws','reset-throws','iframe-null','iframe-throws','standalone']) {
  console.log('SCENARIO '+scenario);subscriptions=[]; const page=await browser.newPage(); const errors=[];page.on('pageerror',e=>errors.push(String(e)));
  await page.goto('http://127.0.0.1:'+server.address().port+'/console?mux='+(scenario==='standalone'?'0':'1'));
  await page.evaluate(()=>{openTab('alpha.owner');openTab('beta.owner');});
  if(scenario==='standalone') { await page.waitForFunction(()=>Array.from(document.querySelectorAll('#panes iframe')).every(f=>f.contentWindow.stream?.readyState===1)); }
  else {
   await page.waitForFunction(()=>window.__muxSnapshot().transportReady&&window.__muxSnapshot().ready.length===2);
   // Queue one unacknowledged real event, so closing must also release credits/timer.
   const scope=subscriptions.find(s=>s.full_name==='alpha.owner');assert.ok(scope);
   const common={full_name:scope.full_name,iframe_instance:scope.iframe_instance,agent_id:scope.full_name,generation:'g',replay_round:'r',epoch:'epoch'};
   for(const res of streams) {packet(res,{...common,type:'replay_begin',floor:'1',H:'1'});packet(res,{...common,type:'event',seq:'1',payload:{kind:'assistant_text',text:'queued'}});}
   await page.waitForFunction(()=>window.__muxSnapshot().count===1);
  }
  if(scenario==='disconnected') for(const res of streams)res.destroy();
  await page.evaluate(s=>{
   const pane=tabs.get('alpha.owner').pane;
   if(s==='remove-throws')window.DWConsoleMux.remove=()=>{throw Error('synthetic disconnected remove failure');};
   if(s==='reset-throws'){const original=crypto.getRandomValues.bind(crypto);let once=true;crypto.getRandomValues=a=>{if(once){once=false;throw Error('synthetic reset cleanup failure');}return original(a);};}
   if(s==='iframe-null')Object.defineProperty(pane,'contentWindow',{get:()=>null});
   if(s==='iframe-throws')Object.defineProperty(pane,'contentWindow',{get:()=>{throw Error('synthetic inaccessible frame');}});
  },scenario);
  await page.locator('#tabs .tab[title="alpha.owner"] .x').click();
  const closed=await page.evaluate(()=>!tabs.has('alpha.owner')&&!document.querySelector('#tabs .tab[title="alpha.owner"]')&&!document.querySelector('#panes iframe[title="Agent view: alpha.owner"]'));
  if(mode==='--baseline'&&!closed){results.push({scenario,baseline_reproduced:true,errors});await page.close();continue;}
  assert.ok(closed,scenario+': clicked close must remove tab, pane and map entry');
  assert.equal(await page.evaluate(()=>tabs.get('beta.owner').tab.classList.contains('on')&&tabs.get('beta.owner').pane.style.display==='block'),true,scenario+': remaining tab activates');
  assert.equal(await page.evaluate(()=>tabs.get('beta.owner').pane.contentDocument.querySelector('textarea').value),'retained draft',scenario+': remaining iframe and draft survive');
  assert.equal(await page.locator('.agent-offline-reason').count(),1,scenario+': closed offline overlay removed');
  if(scenario!=='standalone'){
   await page.waitForFunction(()=>window.__muxSnapshot().seats.length===1&&window.__muxSnapshot().count===0&&window.__muxSnapshot().bytes===0);
   // Actual control contract must remove the closed server subscription as well.
   for(let i=0;i<100&&subscriptions.some(s=>s.full_name==='alpha.owner');i++)await new Promise(r=>setTimeout(r,20));
   assert.deepEqual(subscriptions.map(s=>s.full_name),['beta.owner']);
  }
  await page.locator('#tabs .tab[title="beta.owner"] .x').click();
  await page.waitForFunction(()=>tabs.size===0&&document.querySelectorAll('#panes iframe').length===0);
  await new Promise(r=>setTimeout(r,2300));
  if(mode==='--baseline'){results.push({scenario,closed:true,afterLastClose:scenario==='standalone'?{children:children.size}:await page.evaluate(()=>window.__muxSnapshot()),errors});await page.close();continue;}
  if(scenario!=='standalone'){
   assert.deepEqual(await page.evaluate(()=>window.__muxSnapshot().seats),[]);
   assert.equal(await page.evaluate(()=>window.__muxSnapshot().source),false);
   assert.equal(await page.evaluate(()=>window.__muxSnapshot().reconnect),false);
   assert.equal(streams.size,0,'last-tab close leaves no SSE/reconnect');
   assert.deepEqual(subscriptions,[],'server subscriptions released');
  }else assert.equal(children.size,0,'standalone iframe SSE released');
  assert.deepEqual(errors,[],scenario+': no uncaught errors');
  results.push({scenario,closed:true,remaining_active:true,resource_cleanup:true});await page.close();
 }
 console.log(JSON.stringify({mode:mode||'fixed',results},null,2));
 } finally {await browser.close();for(const r of [...streams,...children])r.destroy();server.closeAllConnections();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1;});
