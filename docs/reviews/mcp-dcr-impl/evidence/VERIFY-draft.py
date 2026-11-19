from pathlib import Path
import json,hashlib,gzip,subprocess,re
R=Path(__file__).resolve().parents[3]
D=Path(__file__).resolve().parent/'evidence'
SOURCE='5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4'
BASE='4a349ee59a1d8d7b8b9c562c462bded2f8b0f192'
def sha(b):return hashlib.sha256(b).hexdigest()
def git(*a):return subprocess.check_output(['git',*a],cwd=R)
def raw(name):
 e=records[name]
 if 'gzip_parts' in e:return b''.join(gzip.decompress((D/p['path']).read_bytes()) for p in e['gzip_parts'])
 return (D/name).read_bytes()
records={e['path']:e for e in json.loads((D/'RAW-MANIFEST.json').read_text())}
for name,e in records.items():
 for p in e.get('gzip_parts',[]):assert sha((D/p['path']).read_bytes())==p['sha256'],p
 b=raw(name);assert len(b)==e['bytes'] and sha(b)==e['sha256'],name
for line in (Path(__file__).resolve().parent/'SHA256SUMS').read_text().splitlines():
 h,p=line.split('  ',1);assert sha((R/p).read_bytes())==h,p
assert git('merge-base','--is-ancestor',BASE,SOURCE)==b''
changed=git('diff','--name-only',SOURCE,'HEAD').decode().splitlines()
assert all(x.startswith(('docs/','openspec/')) for x in changed),changed
structure=json.loads((D/'product-manifest.json').read_text())
for p,h in structure['product_sha256'].items():assert sha(git('show',SOURCE+':'+p))==h,p
assert structure['source']==SOURCE
end=json.loads(raw('migration-end-check.json'));assert end['rc']==0 and end['main_after_fetch']['object_read_rc']==0 and not end['main_after_fetch']['0115_paths']
assert all(c['object_read_rc']==0 and not c['0115_paths'] for c in end['checks'] if c['ref']!='refs/heads/main')
for protected in ('go.mod','go.sum','VERSION'):
 assert git('show',BASE+':'+protected)==git('show',SOURCE+':'+protected),protected
migration_changes=git('diff','--name-only',BASE,SOURCE,'--','internal/store/migrate/migrations').decode().splitlines()
assert migration_changes==['internal/store/migrate/migrations/0115_mcpgw_dcr.sql'],migration_changes
cuts=json.loads(raw('mutations/summary.json'));assert len(cuts)==41
for m in cuts:
 assert m['source']==SOURCE and m['accepted'] and m['killed'],m['id']
 original=git('show',SOURCE+':'+m['file']);assert sha(original)==m['original_sha256']==m['restored_sha256']
 text=original.decode();start=-1
 for i in range(m['occurrence']+1):start=text.index(m['old'],start+1)
 mutant=(text[:start]+m['new']+text[start+len(m['old']):]).encode()
 assert sha(mutant)==m['mutant_sha256']
 assert sha(raw('mutations/'+m['id']+'.patch'))==m['patch_sha256']
 for k,suffix in [('build','build'),('test','test'),('baseline','restored')]:
  assert sha(raw('mutations/'+m['id']+'-'+suffix+'.log'))==m[k]['log_sha256']
 assert m['build']['rc']==0 and m['test']['rc']!=0 and m['baseline']['rc']==0
 log=raw('mutations/'+m['id']+'-test.log').decode()
 assert m['assertions'] and all(a in log for a in m['assertions'])
 assert not any(x in log for x in ('panic:','[build failed]','SQLSTATE','test timed out','DATA RACE'))
gates=json.loads(raw('gate-summary.json'));assert len(gates)==6
for g in gates:
 assert g['source']==SOURCE and (g['rc']==0 or g['phase']=='go-packages-count3'),g
 assert sha(raw(g['phase']+'.log'))==g['log_sha256'],g['phase']
assert b'BUILD_TESTS_OK helper_profiles=2 python=27 shell=2' in raw('canonical-final.log')
assert b'DATA RACE' not in raw('focused-race.log')
for phase in ('migration-full','cmd-focused','focused-race'):
 s=json.loads(raw(phase+'.summary.json'))
 assert not s['non_json_lines'],phase
 assert all(p['package_action']=='pass' and not p['failures'] for p in s['packages'].values()),phase
z=json.loads(raw('z3-summary.json'));assert z['source']==SOURCE and z['complete'] and len(z['runs'])==5 and z['green']>=3
allowed={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
initial=json.loads(raw('go-packages-count3.summary.json'))
initial_fail=[f['test'] for p in initial['packages'].values() for f in p['failures']]
initial_leaf=[f for f in initial_fail if not any(x.startswith(f+'/') for x in initial_fail)]
assert initial_fail and all(x in allowed for x in initial_leaf) and initial['interrupted_lines']
assert all(p['package_action']=='pass' for name,p in initial['packages'].items() if not name.endswith('/m8'))
for run in z['runs']:
 assert sha(raw(run['phase']+'.log'))==run['log_sha256']
 s=json.loads(raw(run['phase']+'.summary.json'))
 fail=[f['test'] for p in s['packages'].values() for f in p['failures']]
 leaf=[f for f in fail if not any(x.startswith(f+'/') for x in fail)]
 assert all(x in allowed for x in leaf),leaf
 assert not fail or s['interrupted_lines']
 assert run['rc']==0 or fail
assert b'--- PASS: TestDCRAdditiveMigrationOldBinaryAndForward' in raw('cmd-focused.log')
print('VERIFY_OK',SOURCE,'products',len(structure['product_sha256']),'raw_files',len(records),'mutations',len(cuts),'gates',len(gates))
