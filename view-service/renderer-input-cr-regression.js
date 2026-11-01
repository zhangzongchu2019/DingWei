// Actual renderer, loopback HTTP capture; no live Agent messages.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require('playwright');
let html=fs.readFileSync(path.join(__dirname,'renderer-canary-sse.html'),'utf8');
if(process.argv.includes('--mutate')){
 const line="const text = humanInput.value.replace(/\\r\\n?/g, '\\n').replace(/^\\n+/u, '').replace(/\\s+$/u, '');";
 assert.ok(html.includes(line),'normalization mutation anchor');html=html.replace(line,'const text = humanInput.value;');
}
const inputs=[],streams=new Set();
const server=http.createServer((req,res)=>{
 const u=req.url.split('?')[0];const json=v=>{res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(v));};
 if(u==='/view/dev.zzc'){res.writeHead(200,{'Content-Type':'text/html; charset=utf-8'});res.end(html);return;}
 if(u.endsWith('/status'))return json({session:'dev.zzc',state:'idle',readonly:false,can_write:true,auth_mode:'legacy',owner:'zzc',model:'fixture',cost_total:0,cost_last:0,turns:0});
 if(u.endsWith('/events')){streams.add(res);res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(': ready\n\n');req.on('close',()=>streams.delete(res));return;}
 if(u.endsWith('/input')&&req.method==='POST'){let b='';req.on('data',x=>b+=x);req.on('end',()=>{inputs.push(JSON.parse(b));json({ok:true});});return;}
 if(u.endsWith('/mention-candidates'))return json({candidates:[]});
 json({});
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true,executablePath:'/usr/bin/google-chrome',args:['--no-sandbox','--disable-dev-shm-usage']});
 try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(String(e)));
 await page.goto('http://127.0.0.1:'+server.address().port+'/view/dev.zzc');await page.waitForFunction(()=>!humanInput.disabled);
 for(const [raw,want] of [['hello\r','hello'],['\r\nfirst\r\n\r\nsecond\r\n','first\n\nsecond'],['  indented\n  next \n','  indented\n  next']]){
  const before=inputs.length;await page.locator('#humanInput').fill(raw);await page.locator('#sendBtn').click();await page.waitForFunction(()=>humanInput.value==='');
  assert.equal(inputs.length,before+1);assert.equal(inputs.at(-1).text,want);console.log('PASS '+JSON.stringify({raw,want}));
 }
 const before=inputs.length;await page.locator('#humanInput').fill('  \t \r\n');await page.locator('#sendBtn').click();await page.locator('#humanInput').press('Enter');await page.waitForTimeout(200);assert.equal(inputs.length,before,'blank must not send');assert.deepEqual(errors,[]);console.log('PASS whitespace click/Enter sends nothing; middle newlines/indent retained');
 }finally{await browser.close();for(const res of streams)res.destroy();server.closeAllConnections();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1;});
