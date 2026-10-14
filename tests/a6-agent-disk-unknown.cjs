const fs=require('fs'),vm=require('vm'),assert=require('assert');
const s=fs.readFileSync('view-service/renderer-canary-sse.html','utf8');
const cut=(a,b)=>s.slice(s.indexOf(a),s.indexOf(b,s.indexOf(a)));
function fixture(){const c={stats:{disk:null},updateMetrics(){},updateLocalTime(){},performance:{now:()=>10},Date};vm.createContext(c);vm.runInContext(cut('    // A6 authenticated Agent disk display gate BEGIN','    // A6 authenticated Agent disk display gate END')+cut('    function formatDisk(', '    function updateHeaderLock(')+cut('    function applyResourceData(', '    async function refreshStatus('),c);return c;}
let count=0;function test(name,fn){fn();console.log('PASS '+name);count++;}
test('VS zero/6.5 cannot masquerade as Agent disk',()=>{const c=fixture();c.applyResourceData({disk:{used_gb:0,total_gb:6.5},resource_scope:'view-service'});assert.equal(c.formatDisk(c.stats.disk),'未知');});
test('initial absent sample is unknown, not zero',()=>{const c=fixture();assert.equal(c.formatDisk(c.stats.disk),'未知');assert(s.includes('disk: null,'));});
test('VS positive value also rejected',()=>{const c=fixture();c.applyResourceData({disk:{used_gb:45,total_gb:200}});assert.equal(c.formatDisk(c.stats.disk),'未知');});
test('raw agent scope is not identity attestation',()=>{const c=fixture();c.applyResourceData({resource_scope:'agent',agent_id:'spoof',disk:{used_gb:0,total_gb:200,sampled_at_ms:Date.now()}});assert.equal(c.formatDisk(c.stats.disk),'未知');});
test('unsupported stale legacy value is discarded',()=>{const c=fixture();c.stats.disk={used_gb:1,total_gb:2};c.applyResourceData({});assert.equal(c.formatDisk(c.stats.disk),'未知');});
test('missing/error disk cannot become a numeric zero',()=>{for(const disk of [undefined,null,{},0,'0']){const c=fixture();c.applyResourceData({disk});assert.equal(c.formatDisk(c.stats.disk),'未知');}});
test('memory and time still use existing resource snapshot',()=>{const c=fixture();c.applyResourceData({mem:{used_mb:20,total_mb:100},datetime_epoch_ms:12345,disk:{used_gb:1,total_gb:2}});assert.equal(c.stats.mem.used_mb,20);assert.equal(c.stats.statusDatetimeMs,12345);assert.equal(c.stats.statusMonotonicAt,10);});
test('Agent label and missing binding explanation present',()=>{assert(s.includes('Agent 磁盘 <strong id="diskStat">未知</strong>'));assert(s.includes('Agent 执行环境磁盘'));});
for(const m of s.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g))new vm.Script(m[1]);console.log('OBSERVATIONS '+count+' INLINE_SYNTAX rc0');
