// Retention state still protects history; its presentation is now transient.
const fs=require('fs'),vm=require('vm'),assert=require('assert/strict'),path=require('path');
const {createRequire}=require('module');
const source=process.argv[2]||path.join(__dirname,'../../../view-service/renderer-canary-sse.html');
const base=path.join(__dirname,'renderer_integrity_recovery.cjs'),original=fs.readFileSync(base,'utf8');
const ctx={URLSearchParams,require:createRequire(base),__dirname,console,process:{argv:['node',base,source]}};
vm.createContext(ctx);vm.runInContext(original.slice(0,original.indexOf('let checks=0;')),ctx);
const make=vm.runInContext('fixture',ctx);
let count=0;
for(const next of ["noteIntegrityIssue('recoverable')","noteIntegrityIssue('tracking','纪元改变')","markIntegrityApplyFailed()","noteIntegrityIssue('managed','父链已停止')","showStreamAlert('conn')","hideStreamAlert();maybeClearRecoveredGap()"]){
 const h=make();h.run("noteIntegrityIssue('history','服务端overflow')");h.run(next);
 assert.equal(h.run('integrityIssue.kind'),'history');assert.match(h.notices[0].textContent,/更早的内容不在这里显示/);
 const fault=!next.startsWith('hideStreamAlert');assert.equal(h.alert.hidden,!fault);
 if(fault)assert.equal(h.alert.role,'alert');
 console.log('PASS history state retained without hiding real fault: '+next);count++;
}
{const h=make();h.run("noteIntegrityIssue('recoverable');noteIntegrityIssue('history','history_unavailable');maybeClearRecoveredGap()");assert.equal(h.run('integrityIssue.kind'),'history');assert.equal(h.run('integrityIssue.warningKind'),'recoverable');assert.equal(h.alert.hidden,false);assert.equal(h.alert.role,'alert');console.log('PASS history never declares an unresolved real gap healed');count++;}
console.log('TOTAL '+count+' history priority observations');
