const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('internal/m8/console_auth.go','utf8');
const join=source.slice(source.indexOf('var groupCodeJoinPending=false;'),source.indexOf('function groupShortName(s)'));
const render=source.slice(source.indexOf('function renderGroupShareCode(selected){'),source.indexOf('function openGroupWorkspace(){'));
let observations=0;
function fixture(){
 const nodes={};function node(){return {value:'',disabled:false,hidden:false,children:[],replaceChildren(){this.children=[];this.value='';},append(n){this.children.push(n);if(this.value==='')this.value=n.value||'';},get options(){return this.children;}};}
 const c={console,AbortController,groupFetchTimeoutMs:8000,groupSnapshot:{groups:[],owned_agents:[{id:'own',session_name:'u-Agent-x'}]},groupLeaderSelection:'',groupPanelMode:'',window:{},document:{getElementById(id){return nodes[id]||(nodes[id]=node());},createElement:node},groupShortName:s=>s,groupFeedback(id,text){c.document.getElementById(id).textContent=text;},setGroupPanelMode(){},setTimeout(fn){c.timeout=fn;return 1;},clearTimeout(){},async refreshGroups(){c.refreshes++;return c.groupSnapshot;},refreshes:0,calls:[],async fetch(url,opts){c.calls.push({url,opts});return {status:204,ok:true};}};
 vm.createContext(c);vm.runInContext(join+render,c);c.document.getElementById('groupJoinCode').value='AbCdEf0123456789_-AbCdEf01234567';c.document.getElementById('groupCodeJoinAgent').value='own';return c;
}
function check(name,fn){return Promise.resolve().then(fn).then(()=>{observations++;console.log('PASS '+name);});}
(async()=>{
 await check('valid case-sensitive code/body/cookies/explicit 204',async()=>{const c=fixture();await c.joinGroupByCode();assert.equal(c.calls.length,1);assert(c.calls[0].url.includes('AbCdEf0123456789_-AbCdEf01234567'));assert.equal(c.calls[0].opts.body,'{"agent_id":"own"}');assert.equal(c.calls[0].opts.credentials,'same-origin');assert.equal(c.refreshes,1);assert.equal(c.document.getElementById('groupCodeJoinFeedback').textContent,'已加入编组');});
 for(const code of ['x'.repeat(31),'x'.repeat(33),'!'.repeat(32),' '+ 'a'.repeat(31)])await check('invalid code '+JSON.stringify(code),async()=>{const c=fixture();c.document.getElementById('groupJoinCode').value=code;await c.joinGroupByCode();assert.equal(c.calls.length,0);});
 await check('foreign Agent tamper rejected',async()=>{const c=fixture();c.document.getElementById('groupCodeJoinAgent').value='foreign';await c.joinGroupByCode();assert.equal(c.calls.length,0);});
 await check('disabled stale selection rejected',async()=>{const c=fixture();c.document.getElementById('groupCodeJoinAgent').disabled=true;await c.joinGroupByCode();assert.equal(c.calls.length,0);});
 await check('no own Agent rejected',async()=>{const c=fixture();c.groupSnapshot.owned_agents=[];await c.joinGroupByCode();assert.equal(c.calls.length,0);});
 await check('picker populated with no visible groups',()=>{const c=fixture();c.renderGroups();assert.equal(c.document.getElementById('groupCodeJoinAgent').value,'own');assert.equal(c.document.getElementById('groupCodeJoinAgent').disabled,false);});
 for(const status of [401,403,404,409])await check('truthful HTTP '+status,async()=>{const c=fixture();c.fetch=async()=>({status,ok:false,text:async()=>JSON.stringify({message:'拒绝 '+status})});await c.joinGroupByCode();assert.equal(c.document.getElementById('groupCodeJoinFeedback').textContent,'拒绝 '+status);assert.equal(c.refreshes,0);});
 await check('unexpected 200 not treated as joined',async()=>{const c=fixture();c.fetch=async()=>({status:200,ok:true,text:async()=>''});await c.joinGroupByCode();assert(c.document.getElementById('groupCodeJoinFeedback').textContent.includes('未确认'));assert.equal(c.refreshes,0);});
 await check('plain error retained',async()=>{const c=fixture();c.fetch=async()=>({status:409,ok:false,text:async()=> '编组已关闭'});await c.joinGroupByCode();assert.equal(c.document.getElementById('groupCodeJoinFeedback').textContent,'编组已关闭');});
 await check('single flight and abort result unknown, no retry',async()=>{const c=fixture();c.fetch=(url,opts)=>{c.calls.push({url,opts});return new Promise((resolve,reject)=>opts.signal.addEventListener('abort',()=>reject(Object.assign(new Error(),{name:'AbortError'}))));};const p=c.joinGroupByCode();await c.joinGroupByCode();assert.equal(c.calls.length,1);c.timeout();await p;assert(c.document.getElementById('groupCodeJoinFeedback').textContent.includes('未确认'));assert.equal(c.groupCodeJoinPending,false);assert.equal(c.document.getElementById('joinByCode').disabled,false);});
 await check('204 remains success when listing refresh fails',async()=>{const c=fixture();c.refreshGroups=async()=>null;await c.joinGroupByCode();assert.equal(c.document.getElementById('groupCodeJoinFeedback').textContent,'已加入编组；列表刷新失败，请重试加载');});
 assert(source.includes("document.getElementById('joinByCode').onclick=joinGroupByCode"));assert(source.includes('id=groupCodeJoin'));console.log('OBSERVATIONS '+observations);
 // Compile actual full ConsolePage inline scripts without executing a browser.
 const page=source.slice(source.indexOf('const consolePageHTML'),source.indexOf('\nfunc (h *Hub) HandleConsoleAgents'));
 let scripts=0;for(const m of page.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)){new vm.Script(m[1]);scripts++;}assert(scripts>0);console.log('INLINE_SYNTAX '+scripts);
})().catch(e=>{console.error(e);process.exitCode=1;});
