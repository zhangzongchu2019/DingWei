// Exact renderer functions and CSS: retention is a notice, real failures still warn.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert/strict');
const {createRequire}=require('module');
const renderer=path.resolve(process.argv[2]||path.join(__dirname,'../view-service/renderer-canary-sse.html'));
const html=fs.readFileSync(renderer,'utf8');
const base=path.resolve(__dirname,'../internal/m8/testdata/renderer_integrity_recovery.cjs');
const original=fs.readFileSync(base,'utf8'),end=original.indexOf('let checks=0;');assert.ok(end>0);
const ctx={URLSearchParams,require:createRequire(base),__dirname:path.dirname(base),console,process:{argv:['node',base,renderer]}};
vm.createContext(ctx);vm.runInContext(original.slice(0,end),ctx);const fixture=vm.runInContext('fixture',ctx);
const message='更早的历史已超出服务端保留范围，无法加载；当前内容完整保留。';
let n=0;
function check(name,fn){fn();console.log('PASS '+name);n++;}
check('overflow is a status notice with the exact single-line text and no warning icon',()=>{
 const h=fixture();h.event(1);h.run('handleReplayGap({data:JSON.stringify({reason:"overflow"})})');
 assert.equal(h.alert['data-tone'],'notice');assert.equal(h.alert.role,'status');assert.equal(h.text.textContent,message);assert.equal(h.alert.hidden,false);assert.equal(h.button.textContent,'查看范围');
 h.run('requestStreamRecovery()');assert.equal(h.text.textContent,message);assert.equal(h.counts().resets,0);h.event(2);assert.equal(h.counts().rendered,2);assert.equal(h.counts().resets,0);assert.equal(h.text.textContent,message);
});
for(const kind of ['recoverable','tracking','apply','managed'])check(kind+' retains its warning presentation',()=>{
 const h=fixture();h.run(`noteIntegrityIssue('${kind}')`);assert.equal(h.alert['data-tone'],'warning');assert.equal(h.alert.role,'alert');assert.ok(h.text.textContent.startsWith('⚠️ '));
});
for(const kind of ['recoverable','apply','managed'])check('history priority cannot hide concurrent '+kind+' warning',()=>{
 const h=fixture();h.run(`noteIntegrityIssue('history');noteIntegrityIssue('${kind}')`);assert.equal(h.state().issue.kind,'history');assert.equal(h.alert.role,'alert');assert.equal(h.alert['data-tone'],'warning');assert.ok(h.text.textContent.startsWith('⚠️ '));assert.equal(h.counts().resets,0);
});
(async()=>{
 const {chromium}=require('playwright');const browser=await chromium.launch({headless:true,...(process.env.PW_CHANNEL?{channel:process.env.PW_CHANNEL}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1000,height:500}});
  const style=html.match(/<style>([\s\S]*?)<\/style>/)[1];const section=html.match(/<section class="stream-alert"[\s\S]*?<\/section>/)[0];
  await page.setContent('<style>'+style+'</style>'+section);
  const a=html.indexOf('    const STREAM_ALERT_TEXT ='),b=html.indexOf('    const managedParams =',a);assert.ok(a>0&&b>a);
  await page.evaluate(code=>{
   window.lastEventEpoch='aa';window.lastContiguousSeq=1;window.seenAboveSeq=new Set();window.c1MergeBuf=new Map();window.pendingRenders=new Set();
   const pre='const streamAlert=document.getElementById("streamAlert"),streamAlertText=document.getElementById("streamAlertText"),streamRetryBtn=document.getElementById("streamRetryBtn");';
   (0,eval)(pre+code+';window.paintTest=noteIntegrityIssue;');
   window.paintTest('history');
  },html.slice(a,b));
  for(const theme of ['light','dark']){
   await page.evaluate(t=>document.body.dataset.theme=t,theme);
   const got=await page.locator('#streamAlert').evaluate(el=>{const s=getComputedStyle(el),t=getComputedStyle(document.getElementById('streamAlertText')),root=getComputedStyle(el);return {color:s.color,muted:root.getPropertyValue('--muted').trim(),role:el.getAttribute('role'),whiteSpace:t.whiteSpace,text:el.textContent,border:s.borderTopColor};});
   const normalized=await page.evaluate(color=>{const e=document.createElement('span');e.style.color=color;document.body.append(e);const c=getComputedStyle(e).color;e.remove();return c;},got.muted);
   assert.equal(got.color,normalized);assert.equal(got.role,'status');assert.equal(got.whiteSpace,'nowrap');assert.ok(!got.text.includes('⚠️'));console.log('PASS actual CSS '+theme+' uses neutral muted color and single-line notice');n++;
  }
  await page.evaluate(()=>window.paintTest('apply'));
  assert.equal(await page.locator('#streamAlert').getAttribute('data-tone'),'warning');assert.equal(await page.locator('#streamAlert').getAttribute('role'),'alert');
  const danger=await page.locator('#streamAlert').evaluate(el=>{const e=document.createElement('span');e.style.color='var(--danger)';document.body.append(e);const expected=getComputedStyle(e).color;e.remove();return {actual:getComputedStyle(el).color,expected};});assert.equal(danger.actual,danger.expected);console.log('PASS actual CSS restores danger color for a real apply failure');n++;
  console.log('TOTAL '+n+' retention notice and warning checks');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
