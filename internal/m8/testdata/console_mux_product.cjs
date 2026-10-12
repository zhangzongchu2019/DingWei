const assert=require('node:assert/strict');
const puppeteer=require(process.env.MUX_PUPPETEER);
function chromeRSS(root){const fs=require('node:fs'),rows=new Map();for(const pid of fs.readdirSync('/proc').filter(v=>/^\d+$/.test(v))){try{const s=fs.readFileSync('/proc/'+pid+'/status','utf8');rows.set(Number(pid),{parent:Number(s.match(/PPid:\s+(\d+)/)[1]),rss:Number(s.match(/VmRSS:\s+(\d+) kB/)?.[1]||0)*1024});}catch{}}let rss=0,count=0;const todo=[root];while(todo.length){const p=todo.pop(),v=rows.get(p);if(!v)continue;rss+=v.rss;count++;for(const [id,r] of rows)if(r.parent===p)todo.push(id);}return {rss_sum_bytes:rss,processes:count,scope:'sum per-process RSS, shared pages counted repeatedly; includes browser/renderers/utility processes; snapshot'};}

(async()=>{
 const browser=await puppeteer.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 try{
 const page=await browser.newPage(),base=process.env.MUX_BASE;
 const active=new Set();let peakSSE=0;page.on("requestfinished",r=>active.delete(r));page.on("requestfailed",r=>active.delete(r));const errors=[],long=[],child=[],applied=[],appliedRecords=[];const resetRequests=[];const diagnostics=[];page.on('response',r=>{if(r.url().includes('/event-'))diagnostics.push({path:new URL(r.url()).pathname,status:r.status()});});page.on('console',m=>{if(m.type()==='error')diagnostics.push({console:m.text().slice(0,160)});});page.on('pageerror',e=>errors.push(String(e)));page.on('request',r=>{if(r.method()==='PUT'&&r.url().includes('/event-feeds/')){try{const b=JSON.parse(r.postData());if(b.reset_request_id)resetRequests.push(b);for(const a of b.applied||[]){applied.push(a.after);appliedRecords.push({name:a.full_name,after:a.after});}}catch{}}if(r.url().includes('/event-feeds/')&&r.url().endsWith('/events')){long.push(r.url());active.add(r);peakSSE=Math.max(peakSSE,active.size);}if(/\/view\/[^/]+\/events/.test(r.url()))child.push(r.url());});
 await page.setCookie({name:'dw_np_session',value:process.env.MUX_TEST_SESSION,url:base});
 await page.goto(base+'/console',{waitUntil:'domcontentloaded'});
 await page.evaluate(()=>{for(let i=0;i<6;i++)openTab('agent'+String(i).padStart(2,'0')+'.owner');});
 await page.waitForFunction(()=>document.querySelectorAll('#panes iframe').length===6);
 // Native production renderer receives each own replay exactly through the broker.
 for(let i=0;i<6;i++){const name='agent'+String(i).padStart(2,'0');const f=page.frames().find(f=>f.url().includes('/view/'+name+'.owner'));try{await f.waitForFunction(s=>document.body.innerText.includes(s),{timeout:12000},'product-initial-'+name);}catch(e){console.log(JSON.stringify({diagnostics:diagnostics.filter(v=>v.path),errors,status:await page.evaluate(()=>document.getElementById('consoleMuxStatus')?.textContent),body:await f.evaluate(()=>document.body.innerText.slice(-1200))}));throw e;}}
 assert.equal(long.length,1,'one public HTTP1 SSE');assert.equal(child.length,0,'managed children must not open SSE');
 await page.evaluate(()=>{openTab('agent06.owner');openTab('agent07.owner');});for(let i=6;i<8;i++){const name='agent'+String(i).padStart(2,'0');await page.waitForFunction(n=>Array.from(document.querySelectorAll('#panes iframe')).some(f=>f.src.includes(n)),{},name);let f;for(let k=0;k<200;k++){f=page.frames().find(f=>f.url().includes('/view/'+name+'.owner'));if(f)break;await new Promise(r=>setTimeout(r,10));}assert.ok(f,'new product frame loaded');await f.waitForFunction(s=>document.body.textContent.includes(s),{timeout:15000},'product-initial-'+name);}
 assert.equal(long.length,1,'dynamic add keeps same public SSE');
 await page.evaluate(()=>{const iframe=document.querySelector('#panes iframe');iframe.contentDocument.querySelector('textarea').value='preserved-product-draft';});
 const short=await page.evaluate(async()=>{const start=performance.now();const list=await fetch('/api/console/agents');const ticket=await fetch('/api/console/agents/agent00.owner/file-upload-ticket',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({max_bytes:1024})});const t=await ticket.json();let uploaded=0;if(ticket.ok){const body=new TextEncoder().encode('product-upload-verification');const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',body)),v=>v.toString(16).padStart(2,'0')).join('');uploaded=(await fetch(t.upload_url,{method:'POST',headers:{Authorization:'Bearer '+t.ticket,'X-DingWei-Session':t.session,'X-File-Name':'product-test.txt','X-Content-SHA256':digest,'Content-Type':'text/plain'},body})).status;}const input=await fetch('/view/agent00.owner/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:'must remain page-authorized'})});return {list:list.status,ticket:ticket.status,upload:uploaded,input:input.status,ms:performance.now()-start};});
 assert.equal(short.list,200);assert.equal(short.ticket,200);assert.equal(short.upload,201);assert.ok(short.ms<1500,'list/input/ticket short requests do not wait behind SSE');
 for(let i=0;i<8;i++){const name='agent'+String(i).padStart(2,'0');await page.evaluate(async n=>{await fetch('/test-publish/'+n,{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'product-live-'+n,data:{}})});},name);}
 for(let i=0;i<8;i++){const name='agent'+String(i).padStart(2,'0');const f=page.frames().find(f=>f.url().includes('/view/'+name+'.owner'));await f.waitForFunction(s=>document.body.innerText.includes(s),{timeout:15000},'product-live-'+name);}
 await page.evaluate(()=>activate('agent00.owner'));
 await page.evaluate(async()=>fetch('/test-publish/agent00',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:Array.from({length:180},(_,i)=>'product-scroll-line-'+i+' '+('long text '.repeat(8))).join('\n\n'),turn_id:'scroll-turn',data:{turn_id:'scroll-turn'}})}));
 const scrollFrame=page.frames().find(f=>f.url().includes('/view/agent00.owner'));await scrollFrame.waitForFunction(()=>document.getElementById('timeline').scrollHeight>document.getElementById('timeline').clientHeight+100,{timeout:10000});
 await new Promise(r=>setTimeout(r,300));const scrollBefore=await scrollFrame.evaluate(()=>{const el=document.getElementById('timeline');el.style.scrollBehavior='auto';document.dispatchEvent(new WheelEvent('wheel',{deltaY:-500}));el.scrollTop=60;el.dispatchEvent(new Event('scroll'));return el.scrollTop;});assert.ok(scrollBefore>0);
 await page.evaluate(()=>{activate('agent07.owner');activate('agent00.owner');});
 const scrollAfter=await scrollFrame.evaluate(()=>document.getElementById('timeline').scrollTop);assert.equal(scrollAfter,scrollBefore,'scroll anchor retained when hiding pane');
 const draft=await page.evaluate(()=>document.querySelector('#panes iframe').contentDocument.querySelector('textarea').value);assert.equal(draft,'preserved-product-draft');
 const ownerFrame=page.frames().find(f=>f.url().includes('/view/agent00.owner'));await ownerFrame.waitForFunction(()=>!document.getElementById('humanInput').disabled,{timeout:10000});const sent=page.waitForResponse(r=>r.url().endsWith('/view/agent00.owner/input')&&r.request().method()==='POST');await ownerFrame.evaluate(()=>{document.getElementById('humanInput').value='product-owner-send';document.getElementById('sendBtn').click();});const sendResponse=await sent;assert.equal(sendResponse.status(),200,'native owner capability send accepted');assert.equal((await sendResponse.json()).ok,true);

 await page.waitForFunction(()=>true);await new Promise(r=>setTimeout(r,200));assert.ok(applied.some(a=>BigInt(a.split('-').pop())>9007199254740992n),'uint64 above 2^53 preserved end to end');
 const resourcesBefore=await page.evaluate(async()=> (await fetch('/test-resources')).json());
 const maximumFrames=[];
 for(let i=0;i<8;i++){
  const name='agent'+String(i).padStart(2,'0');
  const max=await page.evaluate(async n=>{const r=await fetch('/test-maximum/'+n,{method:'POST'});if(!r.ok)throw new Error('maximum frame construction '+r.status);return r.json();},name);
  assert.equal(max.wire_bytes,1048576);
  const f=page.frames().find(f=>f.url().includes('/view/'+name+'.owner'));
  await f.waitForFunction(m=>document.body.textContent.includes(m),{timeout:15000},max.marker);
  for(let k=0;k<400&&!appliedRecords.some(a=>a.name===name+'.owner'&&a.after===max.after);k++)await new Promise(r=>setTimeout(r,10));
  assert.ok(appliedRecords.some(a=>a.name===name+'.owner'&&a.after===max.after),'max F must get production applied ACK');maximumFrames.push({name,wire_bytes:max.wire_bytes,raw_payload_bytes:max.raw_payload_bytes,applied:true});
 }
 if(process.env.MUX_SATURATION==='normal'){
 let credit;for(let k=0;k<400;k++){credit=await page.evaluate(async()=> (await fetch('/test-resources')).json());if(credit.hub_queue_events===0)break;await new Promise(r=>setTimeout(r,10));}assert.equal(credit.hub_queue_events,0,'pre-burst server actually applied credit barrier');assert.equal(credit.active_mux_transports,1);assert.equal(credit.active_subscription_count,8);
 const burst=await page.evaluate(async()=>{const frames=[];for(let round=0;round<2;round++)frames.push(...await Promise.all(Array.from({length:8},async(_,i)=>{const r=await fetch('/test-maximum/agent'+String(i).padStart(2,'0'),{method:'POST'});if(!r.ok)throw new Error('normal burst '+r.status);return r.json();})));return frames;});
 for(let i=0;i<8;i++){const max=burst[i+8];for(let k=0;k<1000&&!appliedRecords.some(a=>a.name==='agent'+String(i).padStart(2,'0')+'.owner'&&a.after===max.after);k++)await new Promise(r=>setTimeout(r,10));assert.ok(appliedRecords.some(a=>a.name==='agent'+String(i).padStart(2,'0')+'.owner'&&a.after===max.after),'normal ACK final burst cursor '+i+'; status='+await page.evaluate(()=>document.getElementById('consoleMuxStatus')?.textContent));}
 console.log(JSON.stringify({pressure_case:'normal_ack',legal_frames:16,all_last_cursors_actually_applied:true,ack_seconds:2,total_deliveries_cap:2}));
 }
 if(process.env.MUX_SATURATION==='late'){
 const lateFrame=page.frames().find(f=>f.url().includes('/view/agent06.owner'));
 await lateFrame.evaluate(()=>{document.getElementById('humanInput').value='explicit-user-recovery-draft';});
 const appliedBefore=appliedRecords.filter(a=>a.name==='agent06.owner').map(a=>a.after).at(-1);
 await page.evaluate(()=>{window.lateOriginal=window.postMessage;window.lateAckCount=0;window.postMessage=function(d,...args){if(d?.protocol==='dw-console-mux/v1'&&d.type==='applied'&&d.agent_id==='agent06'){window.lateAckCount++;return;}return window.lateOriginal.call(window,d,...args);};const target=tabs.get('agent06.owner').pane.contentWindow;window.lateDeliveryCount=0;window.lateDeliverOriginal=target.postMessage.bind(target);target.postMessage=(d,...a)=>{if(d?.protocol==='dw-console-mux/v1'&&d.type==='event')window.lateDeliveryCount++;return window.lateDeliverOriginal(d,...a);};});
 await page.evaluate(async()=>fetch('/test-publish/agent06',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'late-ack-real-DOM-event',data:{}})}));
 await lateFrame.waitForFunction(()=>document.body.textContent.includes('late-ack-real-DOM-event'),{timeout:10000});
 await page.waitForFunction(()=>tabs.get('agent06.owner').pane.dataset.streamError?.includes('ACK 超时'),{timeout:8000});
 assert.equal(await page.evaluate(()=>window.lateDeliveryCount),2);assert.ok(await page.evaluate(()=>window.lateAckCount)>=2);
 assert.equal(appliedRecords.filter(a=>a.name==='agent06.owner').map(a=>a.after).at(-1),appliedBefore,'late ACK cannot advance confirmed cursor');
 assert.equal(await lateFrame.evaluate(()=>document.getElementById('humanInput').value),'explicit-user-recovery-draft');
 await page.evaluate(()=>{window.postMessage=window.lateOriginal;});
 await page.evaluate(async()=>fetch('/test-publish/agent04',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'other-seat-during-late-stop',data:{}})}));await page.frames().find(f=>f.url().includes('/view/agent04.owner')).waitForFunction(()=>document.body.textContent.includes('other-seat-during-late-stop'),{timeout:10000});
 // Explicit existing product action: user closes the stopped tab and opens it again.
 // Fresh DOM starts at the retained suffix floor; this is not automatic resume.
 await page.evaluate(()=>{closeTab('agent06.owner');openTab('agent06.owner');});
 let recovered;for(let i=0;i<300;i++){recovered=page.frames().find(f=>f.url().includes('/view/agent06.owner')&&f!==lateFrame);if(recovered)break;await new Promise(r=>setTimeout(r,10));}assert.ok(recovered);
 await recovered.waitForFunction(()=>document.body.textContent.includes('late-ack-real-DOM-event'),{timeout:15000});
 await page.evaluate(async()=>fetch('/test-publish/agent06',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'after-explicit-user-reopen',data:{}})}));await recovered.waitForFunction(()=>document.body.textContent.includes('after-explicit-user-reopen'),{timeout:10000});
 console.log(JSON.stringify({pressure_case:'ack_later_than_2s',expected_stop:true,total_deliveries:2,no_confirmed_cursor_jump:true,draft_preserved_until_explicit_close:true,other_seat_continues:true,user_action:'existing closeTab then openTab',replay_baseline:'fresh DOM retained suffix floor-1, actual DOM before applied; cost projection unchanged',user_recovery_live:true}));
 }
 if(process.env.MUX_SATURATION==='1'){
 // Hold applied messages at the test observer, filling real pending+sent credits.
 await page.evaluate(()=>{window.testOriginalPostMessage=window.postMessage;window.testHeld=0;window.testHeldMessages=[];window.postMessage=function(d,...args){if(d?.protocol==='dw-console-mux/v1'&&d.type==='applied'){window.testHeld++;window.testHeldMessages.push(d);return;}return window.testOriginalPostMessage.call(window,d,...args);};});
 const burst=await page.evaluate(async()=>{const frames=[];for(let round=0;round<2;round++)frames.push(...await Promise.all(Array.from({length:8},async(_,i)=>{const r=await fetch('/test-maximum/agent'+String(i).padStart(2,'0'),{method:'POST'});if(!r.ok)throw new Error('burst '+r.status);return r.json();})));return frames;});
 let saturation;for(let k=0;k<100;k++){saturation=await page.evaluate(async()=> (await fetch('/test-resources')).json());if(saturation.hub_queue_events===16)break;await new Promise(r=>setTimeout(r,10));}
 console.log(JSON.stringify({saturation_observed:saturation,console_status:await page.evaluate(()=>document.getElementById('consoleMuxStatus')?.textContent),held_applied:await page.evaluate(()=>window.testHeld),transport_requests:long.length,network_errors:diagnostics.filter(d=>d.console)}));
 assert.equal(saturation.hub_queue_events,16,'real queue retains pending+sent before applied');assert.equal(saturation.hub_queue_wire_bytes,16*1048576,'full 16MiB wire budget');
 const delayed=await page.evaluate(()=>{window.postMessage=window.testOriginalPostMessage;return window.testHeldMessages;});
 // Re-deliver the renderer's own actual ACK from its trusted iframe Window.
 // No forged applied success: these exact objects were emitted after real DOM apply.
 for(const d of delayed){const f=page.frames().find(f=>f.url().includes('/view/'+d.agent_id+'.owner'));await f.evaluate(d=>parent.postMessage(d,location.origin),d);}
 for(let k=0;k<700;k++){const q=await page.evaluate(async()=> (await fetch('/test-resources')).json());if(q.hub_queue_events===0)break;if(k===699)throw new Error('held queue did not release after bounded duplicate ACK retry');await new Promise(r=>setTimeout(r,10));}
 console.log(JSON.stringify({real_wire_queue_saturation:saturation,burst_legal_frames:burst.length,burst_complete_wire_bytes:burst.reduce((n,v)=>n+v.wire_bytes,0)}));
 }
 await page.evaluate(()=>activate('agent00.owner'));
 const resourcesAfter=await page.evaluate(async()=> (await fetch('/test-resources')).json());
 console.log(JSON.stringify({maximum_legal_frames:maximumFrames,resources_before:resourcesBefore,resources_after:resourcesAfter,chrome_process_tree:chromeRSS(browser.process().pid)}));
 // Real server transport cancellation: registry remains metadata-only until a fresh slot/gate is reacquired.
 const transportBeforeDisconnect=long.length,disconnectFeed=new URL(long.at(-1)).pathname.split('/')[4];
 const disconnectStatus=await page.evaluate(async id=>(await fetch('/test-disconnect?feed_id='+id,{method:'POST'})).status,disconnectFeed);assert.equal(disconnectStatus,204,'must cancel this exact active transport');
 for(let i=0;i<300&&long.length<=transportBeforeDisconnect;i++)await new Promise(r=>setTimeout(r,10));if(long.length<=transportBeforeDisconnect)console.log(JSON.stringify({disconnect_status:disconnectStatus,pre_connections:transportBeforeDisconnect,post_connections:long.length,status:await page.evaluate(()=>document.getElementById('consoleMuxStatus')?.textContent)}));assert.ok(long.length>transportBeforeDisconnect,'actual EventSource reconnect attempted after this cancellation');
 await page.evaluate(async()=>fetch('/test-publish/agent04',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'after-real-feed-reconnect',data:{}})}));
 await page.frames().find(f=>f.url().includes('/view/agent04.owner')).waitForFunction(()=>document.body.textContent.includes('after-real-feed-reconnect'),{timeout:10000});
 assert.equal(await scrollFrame.evaluate(()=>document.getElementById('timeline').scrollTop),scrollBefore,'reconnect retains reading position');
 if(process.env.MUX_R4==='1'){
  const oldFrame=page.frames().find(f=>f.url().includes('/view/agent05.owner'));
  await oldFrame.evaluate(()=>{document.getElementById('humanInput').value='history-reset-draft';});
  const before=resetRequests.length;
  const activeID=new URL(long.at(-1)).pathname.split('/')[4];
  const result=await page.evaluate(async fid=>(await fetch('/test-reset-history?feed_id='+fid,{method:'POST'})).status,activeID);
  assert.equal(result,204);
  await page.waitForFunction(()=>tabs.get('agent05.owner').pane.dataset.resyncRequired==='true',{timeout:12000});
  await new Promise(r=>setTimeout(r,300));assert.equal(resetRequests.length,before,'no automatic reset intent');
  assert.equal(await oldFrame.evaluate(()=>document.getElementById('humanInput').value),'history-reset-draft');
  await page.evaluate(()=>{closeTab('agent05.owner');openTab('agent05.owner');});
  let rebuilt;for(let i=0;i<300;i++){rebuilt=page.frames().find(f=>f.url().includes('/view/agent05.owner')&&f!==oldFrame);if(rebuilt)break;await new Promise(r=>setTimeout(r,10));}
  assert.ok(rebuilt);await rebuilt.waitForFunction(()=>document.body.textContent.includes('history-cut-1001'),{timeout:20000});
  assert.ok(resetRequests.length>before,'history rebuild has explicit reset_request_id');
  assert.ok(await page.evaluate(()=>tabs.get('agent05.owner').pane.dataset.historyWarning?.includes('明确重建')));
  console.log(JSON.stringify({r4_history_gap:true,no_auto_reset:true,draft_retained_until_close:true,explicit_bound_reset:true,retained_suffix_rebuilt:true}));
 }
 // A simultaneous legacy independent view and aggregate bridge share the actual VS projection transaction.
 const independent=await browser.newPage();await independent.goto(base+'/view/agent04.owner',{waitUntil:'domcontentloaded'});
 const seeded=false;await page.evaluate(v=>{window.__costSeeded=v;},seeded);await page.evaluate(async cost=>fetch('/test-publish/agent04',{method:'POST',body:JSON.stringify({kind:'state_change',data:{change:'turn_completed',cost_usd:cost}})}),seeded?15:1.25);
 await page.waitForFunction(async()=>{const s=await (await fetch('/view/agent04.owner/status')).json();return s.cost_total===(window.__costSeeded?15:1.25)&&s.cost_last===(window.__costSeeded?2.5:1.25)&&s.turns===(window.__costSeeded?8:1);},{timeout:10000});
 await new Promise(r=>setTimeout(r,200));const costs=await page.evaluate(async()=> (await fetch('/view/agent04.owner/status')).json());const independentCosts=await independent.evaluate(async()=> (await fetch('/view/agent04.owner/status')).json());for(const key of ['cost_total','cost_last','turns'])assert.equal(independentCosts[key],costs[key],'independent/managed status shares seeded authority');assert.equal(costs.turns,seeded?8:1,'legacy + aggregate completion counted once');assert.equal(costs.cost_last,seeded?2.5:1.25,'duplicate completion cannot clear last cost');await independent.close();
 // A production renderer DOM failure must not acknowledge or advance that seat.
 const faultFrame=page.frames().find(f=>f.url().includes('/view/agent02.owner'));
 const beforeFault=appliedRecords.filter(a=>a.name==='agent02.owner').map(a=>BigInt(a.after.split('-').pop())).reduce((a,b)=>a>b?a:b,0n);
 await faultFrame.evaluate(()=>{document.getElementById('humanInput').value='fault-draft-preserved';document.getElementById('timeline').appendChild=()=>{throw Error('injected DOM failure');};});
 await page.evaluate(async()=>fetch('/test-publish/agent02',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'must-not-ack-fault',turn_id:'fault-turn',data:{turn_id:'fault-turn'}})}));
 await page.waitForFunction(()=>document.getElementById('consoleMuxStatus')?.textContent.includes('未完整应用'),{timeout:10000});
 const afterFault=appliedRecords.filter(a=>a.name==='agent02.owner').map(a=>BigInt(a.after.split('-').pop())).reduce((a,b)=>a>b?a:b,0n);assert.equal(afterFault,beforeFault,'negative applied never advances cursor');
 assert.equal(await faultFrame.evaluate(()=>document.getElementById('humanInput').value),'fault-draft-preserved');
 await page.evaluate(async()=>fetch('/test-publish/agent04',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'other-seat-after-s11',data:{}})}));
 await page.frames().find(f=>f.url().includes('/view/agent04.owner')).waitForFunction(()=>document.body.textContent.includes('other-seat-after-s11'),{timeout:10000});
 // ACK timeout delivers exactly twice, retains the cursor and isolates the stalled seat.
 const timeoutFrame=page.frames().find(f=>f.url().includes('/view/agent03.owner'));
 await page.evaluate(()=>{const target=tabs.get('agent03.owner').pane.contentWindow;window.__muxIgnored=0;const original=target.postMessage.bind(target);target.postMessage=(d,...args)=>{if(d?.protocol==='dw-console-mux/v1'&&d.type==='event'){window.__muxIgnored++;return;}return original(d,...args);};});
 await page.evaluate(async()=>fetch('/test-publish/agent03',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'timeout-event-must-not-apply',data:{}})}));
 await page.waitForFunction(()=>document.getElementById('consoleMuxStatus')?.textContent.includes('agent03.owner')&&document.getElementById('consoleMuxStatus')?.textContent.includes('ACK 超时'),{timeout:8000});assert.equal(await page.evaluate(()=>window.__muxIgnored),2,'initial + one retry only');
 // A second top-level Console gets a visible shared PG occupancy refusal; no session logout.
 const second=await browser.newPage();await second.goto(base+'/console',{waitUntil:'domcontentloaded'});await second.evaluate(()=>openTab('agent00.owner'));await second.waitForFunction(()=>document.getElementById('consoleMuxStatus')?.textContent.includes('另一个 Console'),{timeout:10000});assert.equal(await second.evaluate(async()=> (await fetch('/api/console/agents')).status),200);await second.close();
 const over=await page.evaluate(async()=> (await fetch('/test-maximum/agent06?over=1',{method:'POST'})).json());assert.equal(over.wire_bytes,1048577);
 const oversizedFrame=page.frames().find(f=>f.url().includes('/view/agent06.owner'));await page.waitForFunction(()=>tabs.get('agent06.owner').pane.dataset.streamError?.includes('event_too_large'),{timeout:10000});await oversizedFrame.waitForFunction(()=>document.getElementById('streamAlertText')?.textContent.includes('event_too_large'),{timeout:10000});
 await page.evaluate(async()=>fetch('/test-publish/agent04',{method:'POST',body:JSON.stringify({kind:'assistant_text',text:'after-oversize-other-seat-live',data:{}})}));await page.frames().find(f=>f.url().includes('/view/agent04.owner')).waitForFunction(()=>document.body.textContent.includes('after-oversize-other-seat-live'),{timeout:10000});
 console.log(JSON.stringify({illegal_complete_frame_bytes:1048577,oversized_seat_stopped:true,other_seat_after_oversize_continues:true}));
 if(process.env.MUX_RESOURCE_RSS==='1'){const ringLoad=await page.evaluate(async()=> (await fetch('/test-ring-resource-load',{method:'POST'})).json());assert.ok(ringLoad.retained_global_bytes<=128*1048576);assert.ok(ringLoad.retained_after_test_terminal_delete<=128*1048576);const retainedCost=await page.evaluate(async()=> (await fetch('/view/agent04.owner/status')).json());assert.equal(retainedCost.turns,1);assert.equal(retainedCost.cost_last,1.25);assert.equal(retainedCost.cost_total,1.25);console.log(JSON.stringify({resource_ring_load:ringLoad,chrome_process_tree_after_load:chromeRSS(browser.process().pid)}));}
 if(process.env.MUX_SATURATION==='late')assert.ok(resetRequests.length>=1,'explicit user recovery sends bound reset_request_id');
 const start=Date.now();await page.evaluate(async()=>fetch('/test-db-revoke',{method:'POST'}));await new Promise(r=>setTimeout(r,2100));
 assert.equal(peakSSE,1,'maximum simultaneous public SSE, including reconnect');const denial=await page.evaluate(async()=> (await fetch('/api/console/event-context')).status);assert.equal(denial,401);
 assert.equal(errors.length,0,errors.join('\n'));
 console.log(JSON.stringify({scenario:'actual Go+PG+Python VS+native renderer+Chrome HTTP1',seats:[6,8],trusted_vs_targets:2,uint64_above_2p53:true,max_concurrent_public_sse:peakSSE,child_sse:0,short_requests:short,short_request_scope:'actual list+upload pipeline; arbitrary send without page capability remains rejected; helper reader is a local test peer',draft_preserved:true,scroll_preserved:true,ack_total_deliveries:2,real_feed_reconnect:true,owner_send_200:true,legacy_aggregate_cost_once:true,s11_negative_no_advance:true,other_seat_continues:true,second_console_409_visible:true,db_revoke_denied:true,revocation_sample_ms:Date.now()-start,page_errors:errors,reset_requests:resetRequests.length,seeded_history:seeded}));
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exit(1);});
