// Only new severity negatives; actual source functions in isolated VM, no browser/network.
const fs=require('fs'),vm=require('vm'),assert=require('assert/strict'),path=require('path');
const source=process.argv[2]||path.join(__dirname,'../../../view-service/renderer-canary-sse.html');
const html=fs.readFileSync(source,'utf8');
const start=html.indexOf('    const STREAM_ALERT_TEXT ='),end=html.indexOf('    const managedParams =',start);assert(start>0&&end>start);
function make(){const alert={hidden:true,setAttribute(k,v){this[k]=v;}},text={textContent:'connection'},btn={};
 const box={streamAlert:alert,streamAlertText:text,streamRetryBtn:btn,Math,Set,Map,lastContiguousSeq:3,lastEventEpoch:'epoch',seenAboveSeq:new Set(),c1MergeBuf:new Map(),pendingRenders:new Set(),clearTimeout(){},document:{hidden:false},managedMux:false};
 vm.createContext(box);vm.runInContext(html.slice(start,end),box);return {box,alert,text,btn,run:s=>vm.runInContext(s,box)};}
let count=0;
for(const next of ["noteIntegrityIssue('recoverable')","noteIntegrityIssue('tracking','纪元改变')","markIntegrityApplyFailed()","noteIntegrityIssue('managed','父链已停止')","showStreamAlert('conn')","hideStreamAlert();maybeClearRecoveredGap()"]){
 const h=make();h.run("noteIntegrityIssue('history','服务端overflow')");h.run(next);
 assert.equal(h.run('integrityIssue.kind'),'history');assert.equal(h.alert.hidden,false);assert.match(h.text.textContent,/刷新不会找回已淘汰历史/);assert.equal(h.alert.role,'alert');console.log('PASS history retained: '+next);count++;
}
{const h=make();h.run("noteIntegrityIssue('recoverable');noteIntegrityIssue('history','history_unavailable');maybeClearRecoveredGap()");assert.equal(h.run('integrityIssue.kind'),'history');assert.equal(h.alert.role,'alert');console.log('PASS history escalates temporary gap, never success');count++;}
console.log('TOTAL '+count+' new severity observations');
