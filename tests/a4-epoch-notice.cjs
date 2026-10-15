// Execute actual renderer integrity/replay-gap functions; no copied behavior.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert/strict');
const {createRequire}=require('module');
const renderer=path.resolve(process.argv[2]||path.join(__dirname,'../view-service/renderer-canary-sse.html'));
const html=fs.readFileSync(renderer,'utf8');
const base=path.resolve(__dirname,'../internal/m8/testdata/renderer_integrity_recovery.cjs');
const original=fs.readFileSync(base,'utf8'),end=original.indexOf('let checks=0;');assert.ok(end>0);
const ctx={URLSearchParams,require:createRequire(base),__dirname:path.dirname(base),console,process:{argv:['node',base,renderer]}};
vm.createContext(ctx);vm.runInContext(original.slice(0,end),ctx);const fixture=vm.runInContext('fixture',ctx);
const message='服务端已重启，更早的事件无法再逐条核对；当前内容完整保留。';
const epoch=`handleReplayGap({data:JSON.stringify({reason:'epoch_changed'})})`;
let count=0;
function check(name,fn){fn();console.log('PASS '+name);count++;}
function notice(h){assert.equal(h.alert['data-tone'],'notice');assert.equal(h.alert.role,'status');assert.equal(h.text.textContent,message);assert.ok(!h.text.textContent.includes('⚠️'));assert.equal(h.alert.hidden,false);}
function warning(h){assert.equal(h.alert['data-tone'],'warning');assert.equal(h.alert.role,'alert');assert.ok(h.text.textContent.startsWith('⚠️'));assert.equal(h.alert.hidden,false);}
check('explicit epoch-only refusal is an exact neutral notice and preserves rendered content',()=>{
 const h=fixture();h.event(1);h.run(epoch);notice(h);assert.equal(h.button.textContent,'查看范围');
 h.run('requestStreamRecovery()');notice(h);assert.equal(h.counts().resets,0);
 h.event(2);notice(h);assert.equal(h.counts().rendered,2);assert.equal(h.counts().resets,0);
});
check('repeated epoch-only notices remain neutral without replacing the issue',()=>{
 const h=fixture();h.run(epoch);h.run("integrityIssue.outcome='SAVED';window.saved=integrityIssue");h.run(epoch);
 notice(h);assert.equal(h.run('window.saved===integrityIssue'),true);assert.equal(h.state().issue.outcome,'SAVED');
});
for(const kind of ['recoverable','apply','managed','tracking']){
 check('epoch then '+kind+' then repeated epoch keeps real warning',()=>{
  const h=fixture();h.event(1);h.run(epoch);h.run(`noteIntegrityIssue('${kind}','REAL_FAILURE')`);warning(h);
  assert.equal(h.state().issue.kind,kind);h.run("integrityIssue.outcome='SAVED';window.saved=integrityIssue");h.run(epoch);
  warning(h);assert.equal(h.run('window.saved===integrityIssue'),true);assert.equal(h.state().issue.outcome,'SAVED');assert.equal(h.counts().resets,0);
 });
 check(kind+' already active cannot be replaced by an epoch notice',()=>{
  const h=fixture();h.run(`noteIntegrityIssue('${kind}','REAL_FAILURE')`);h.run(epoch);warning(h);assert.equal(h.state().issue.kind,kind);
 });
}
check('tracking with epoch-like detail but no explicit epoch cause remains a warning',()=>{
 const h=fixture();h.run("noteIntegrityIssue('tracking','服务端事件纪元已改变，旧历史完整性无法确认。')");warning(h);
});
check('old unresolved gap crossing an actual event epoch remains a real tracking warning',()=>{
 const h=fixture();h.event(1);h.event(3);h.run('admitEventId("bb-1")');h.run(epoch);warning(h);assert.equal(h.counts().resets,0);
});
check('actual apply failure followed by epoch notice stays a warning',()=>{
 const h=fixture();h.run('markIntegrityApplyFailed()');h.run(epoch);warning(h);assert.equal(h.state().issue.kind,'apply');
});
check('history plus a real failure cannot be downgraded by epoch notification',()=>{
 const h=fixture();h.run("noteIntegrityIssue('history');noteIntegrityIssue('apply')");h.run(epoch);warning(h);assert.equal(h.state().issue.warningKind,'apply');
});
check('real gap refill exhaustion after epoch notice remains a warning',()=>{
 const h=fixture();h.event(1);h.run(epoch);h.event(3);
 h.run('gapRefillTimes=[Date.now(),Date.now(),Date.now()];requestStreamRecovery()');warning(h);
 assert.match(h.text.textContent,/次数已达上限/);assert.equal(h.counts().resets,0);
});
check('actual managed stop after epoch notice remains a warning',()=>{
 const h=fixture({managed:true});h.run(epoch);
 h.handlers.message({source:h.parent,origin:h.c.location.origin,data:{protocol:'dw-console-mux/v1',iframe_instance:'a'.repeat(32),type:'stopped',reason:'FIXED_STOP_REASON'}});
 warning(h);assert.equal(h.state().issue.kind,'managed');assert.match(h.text.textContent,/FIXED_STOP_REASON/);assert.equal(h.counts().resets,0);
});
(async()=>{
 const {chromium}=require('playwright');const browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 try{
  const page=await browser.newPage();const style=html.match(/<style>([\s\S]*?)<\/style>/)[1],section=html.match(/<section class="stream-alert"[\s\S]*?<\/section>/)[0];
  await page.setContent('<style>'+style+'</style>'+section);
  const a=html.indexOf('    const STREAM_ALERT_TEXT ='),b=html.indexOf('    const managedParams =',a);assert.ok(a>0&&b>a);
  await page.evaluate(code=>{
   Object.assign(window,{lastEventEpoch:'aa',lastContiguousSeq:1,seenAboveSeq:new Set(),c1MergeBuf:new Map(),pendingRenders:new Set()});
   const pre='const streamAlert=document.getElementById("streamAlert"),streamAlertText=document.getElementById("streamAlertText"),streamRetryBtn=document.getElementById("streamRetryBtn");';
   (0,eval)(pre+code+';window.showEpoch=()=>handleReplayGap({data:JSON.stringify({reason:"epoch_changed"})});window.realFailure=()=>markIntegrityApplyFailed();');window.showEpoch();
  },html.slice(a,b));
  for(const theme of ['light','dark']){
   await page.evaluate(t=>document.body.dataset.theme=t,theme);
   const got=await page.locator('#streamAlert').evaluate(el=>{const s=document.createElement('span');s.style.color='var(--muted)';document.body.append(s);const muted=getComputedStyle(s).color;s.remove();return {color:getComputedStyle(el).color,muted,role:el.getAttribute('role'),whiteSpace:getComputedStyle(document.getElementById('streamAlertText')).whiteSpace,text:document.getElementById('streamAlertText').textContent};});
   assert.equal(got.color,got.muted);assert.equal(got.role,'status');assert.equal(got.whiteSpace,'nowrap');assert.equal(got.text,message);console.log('PASS actual '+theme+' CSS uses neutral single-line epoch notice');count++;
  }
  await page.evaluate(()=>{window.realFailure();window.showEpoch();});
  assert.equal(await page.locator('#streamAlert').getAttribute('data-tone'),'warning');assert.equal(await page.locator('#streamAlert').getAttribute('role'),'alert');
  const got=await page.locator('#streamAlert').evaluate(el=>{const s=document.createElement('span');s.style.color='var(--danger)';document.body.append(s);const danger=getComputedStyle(s).color;s.remove();return {color:getComputedStyle(el).color,danger};});assert.equal(got.color,got.danger);console.log('PASS actual CSS real apply failure stays danger after repeated epoch notice');count++;
  console.log('TOTAL '+count+' epoch notice observations');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
