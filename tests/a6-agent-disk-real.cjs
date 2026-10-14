const assert=require('assert'),fs=require('fs'),vm=require('vm');
const html=fs.readFileSync(process.argv[2]||'view-service/renderer-canary-sse.html','utf8');
const begin=html.indexOf('    // A6 authenticated Agent disk display gate BEGIN'),end=html.indexOf('    // A6 authenticated Agent disk display gate END',begin);assert(begin>0&&end>begin);
const c={};vm.createContext(c);vm.runInContext(html.slice(begin,end),c);const AgentDiskState=vm.runInContext('agentDiskState.constructor',c);
const valid=()=>({agent_id:'stable-agent-a',connection_generation:'conn-a',status:'ok',source:'helper_self_report',received_at_ms:1000000,age_ms:0,expires_in_ms:360000,sample:{schema:'agent-disk/v1',status:'ok',producer:'helper',sample_instance:'a'.repeat(32),filesystem_instance:'b'.repeat(32),sample_seq:'1',sampled_at_ms:1000000,path_basis:'cli_workdir_filesystem',used_bytes:0,total_bytes:6500000000,available_bytes:6500000000}});
let count=0;const test=(name,f)=>{f();console.log('PASS '+name);count++;};
test('missing means unknown not zero',()=>{const s=new AgentDiskState();assert.equal(JSON.stringify(s.display(0)),JSON.stringify({text:'未知',numeric:false}));});
test('valid authenticated-context zero is zero',()=>{const s=new AgentDiskState();assert(s.apply(valid(),'stable-agent-a',0,100));assert.equal(s.display(100).text,'0 / 6.5 GB');});
test('nonzero small used is not rounded into literal zero',()=>{const s=new AgentDiskState(),v=valid();v.sample.used_bytes=100000;assert(s.apply(v,'stable-agent-a',0,100));assert.equal(s.display(100).text,'<0.1 / 6.5 GB');});
for(const [name,mutate]of Object.entries({
 'wrong Agent':v=>v.agent_id='other',
 'VS source':v=>v.source='view-service',
 'fake source':v=>v.source='agent',
 'no generation':v=>delete v.connection_generation,
 'no schema':v=>delete v.sample.schema,
 'bad basis':v=>v.sample.path_basis='view_service_home',
 'bool zero':v=>v.sample.used_bytes=false,
 'unsafe total':v=>v.sample.total_bytes=2**53,
 'used exceeds total':v=>v.sample.used_bytes=6500000001,
 'future renewal budget':v=>v.expires_in_ms=360001,
 'age plus ttl exceeds bound':v=>v.age_ms=1,
 'missing age':v=>delete v.age_ms,
 'missing timestamp':v=>delete v.sample.sampled_at_ms,
 'unavailable cannot keep zero':v=>v.status='unavailable',
})){test(name,()=>{const s=new AgentDiskState(),v=valid();mutate(v);assert.equal(s.apply(v,'stable-agent-a',0,1),false);assert.equal(s.display(1).numeric,false);});}
test('request duration is charged and expiry updates locally',()=>{const s=new AgentDiskState(),v=valid();v.expires_in_ms=1000;v.age_ms=359000;assert(s.apply(v,'stable-agent-a',10,999));assert.equal(s.display(1009).numeric,true);assert.equal(s.display(1010).text,'已过期');});
test('slow arrival never gains a new lease',()=>{const s=new AgentDiskState(),v=valid();v.expires_in_ms=1000;assert.equal(s.apply(v,'stable-agent-a',0,1000),false);assert.equal(s.display(1000).text,'已过期');});
test('current unavailable clears old valid value',()=>{const s=new AgentDiskState();s.apply(valid(),'stable-agent-a',0,1);s.apply({agent_id:'stable-agent-a',connection_generation:'conn-a',status:'unavailable',source:'helper_self_report'},'stable-agent-a',0,2);assert.equal(s.display(2).text,'未知');});
test('returned snapshot cannot mutate stored numeric',()=>{const s=new AgentDiskState(),v=valid();s.apply(v,'stable-agent-a',0,1);v.sample.used_bytes=999;assert.equal(s.display(2).text,'0 / 6.5 GB');});
test('zero sequence rejected',()=>{const s=new AgentDiskState(),v=valid();v.sample.sample_seq='0';assert.equal(s.apply(v,'stable-agent-a',0,1),false);});
test('oversized uint64 sequence rejected',()=>{const s=new AgentDiskState(),v=valid();v.sample.sample_seq='18446744073709551616';assert.equal(s.apply(v,'stable-agent-a',0,1),false);});
test('same sample cannot renew client expiry on response replay',()=>{const s=new AgentDiskState(),v=valid();v.expires_in_ms=1000;v.age_ms=359000;s.apply(v,'stable-agent-a',0,1);s.apply(v,'stable-agent-a',900,901);assert.equal(s.display(1000).text,'已过期');});
test('JSON arrays cannot masquerade as sequence or instance strings',()=>{for(const key of ['sample_seq','sample_instance','filesystem_instance']){const s=new AgentDiskState(),v=valid();v.sample[key]=[v.sample[key]];assert.equal(s.apply(v,'stable-agent-a',0,1),false);}});
test('same sample cannot change filesystem or available bytes',()=>{for(const key of ['filesystem_instance','available_bytes']){const s=new AgentDiskState(),v=valid();s.apply(v,'stable-agent-a',0,1);v.sample[key]=key==='available_bytes'?1:'c'.repeat(32);assert.equal(s.apply(v,'stable-agent-a',2,3),false);}});
test('normal maximum sample age 330 seconds remains numeric under 360 second TTL',()=>{
 const s=new AgentDiskState(),v=valid();v.age_ms=30000;v.expires_in_ms=330000;
 assert(s.apply(v,'stable-agent-a',0,0)); // up to 30s old at a resource response
 assert.equal(v.age_ms+300000,330000); // next page poll after 300s
 assert.equal(s.display(300000).numeric,true);
 assert.equal(s.display(330000).text,'已过期'); // no TTL extension
});
console.log('OBSERVATIONS '+count);
