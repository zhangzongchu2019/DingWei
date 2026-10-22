#!/usr/bin/env python3
"""Verify committed evidence and frozen Git inputs without executing product code."""
import gzip,hashlib,json,pathlib,subprocess,sys
ROOT='docs/reviews/coder-daemon-identity-20261022/'
ref=sys.argv[1] if len(sys.argv)>1 else 'HEAD'
def git(*args): return subprocess.check_output(['git',*args])
def read(path): return git('show',ref+':'+ROOT+path)
def obj(path): return json.loads(read(path))
def sha(b): return hashlib.sha256(b).hexdigest()
def files(anchor): return git('ls-tree','-r','-z','--name-only',anchor).decode().rstrip('\0').split('\0')
m=obj('SOURCE-MANIFEST.json'); source=m['source']; assert git('rev-parse',source+'^{tree}').decode().strip()==m['tree']
assert {x['path'] for x in m['files']}=={p for p in files(source) if not p.startswith('docs/')}
# One persistent cat-file process validates source bytes without 1882 subprocesses.
p=subprocess.Popen(['git','cat-file','--batch'],stdin=subprocess.PIPE,stdout=subprocess.PIPE)
for x in m['files']:
 p.stdin.write((source+':'+x['path']+'\n').encode()); p.stdin.flush()
 header=p.stdout.readline().decode().split(); assert header[0]==x['blob'] and header[1]=='blob'
 b=p.stdout.read(int(header[2])); assert p.stdout.read(1)==b'\n'
 assert len(b)==x['bytes'] and sha(b)==x['sha256'],x['path']
p.stdin.close(); assert p.wait()==0
assert not git('diff','--name-only',source,ref,'--',':(exclude)docs').strip(),'candidate changed frozen source'
s=obj('SCOPE.json'); assert s['source']==source and not s['deployment']
assert sorted(git('diff','--name-only',s['base'],source,'--',':(exclude)docs').decode().splitlines())==s['changed_non_docs']
assert not git('diff','--name-only',s['base'],source,'--',*s['unchanged_paths']).strip()
for x in obj('DOC-MANIFEST.json'):
 b=git('show',ref+':'+x['path']); assert len(b)==x['bytes'] and sha(b)==x['sha256']
e=obj('EVIDENCE-MANIFEST.json'); expected={ROOT+x['path'] for x in e['files']}|{ROOT+'EVIDENCE-MANIFEST.json'}
assert expected=={x for x in files(ref) if x.startswith(ROOT)}
for x in e['files']:
 b=read(x['path']); assert len(b)==x['bytes'] and sha(b)==x['sha256'],x['path']
for name in ('go-full','go-prewarm','vet','canonical','helper-final'):
 assert read('raw/gates/'+name+'.rc').strip()==b'0'
 meta=obj('raw/gates/'+name+'.json'); assert meta['source']==source and meta['rc']==0
assert b'BUILD_TESTS_OK python=58 shell=2' in read('raw/gates/canonical.stdout')
h=read('raw/gates/helper-final.stderr'); assert b'Ran 826 tests' in h and h.rstrip().endswith(b'OK')
assert b'FAILED (' not in h
c=obj('raw/mutations/CHECKS.json'); assert c['source']==source and len(c['rows'])==44
red=[r for r in c['rows'] if r['assertion_red']]; assert len(red)==21
for row in c['rows']:
 stderr=read('raw/mutations/'+row['name']+'.stderr')
 if row['assertion_red']:
  assert row['rc']==1 and row['marker'].encode() in stderr and b'AssertionError' in stderr and b'FAIL:' in stderr and b'ERROR:' not in stderr
 else: assert row['rc']==0 and stderr.rstrip().endswith(b'OK')
assert obj('raw/mutations/RESULT.json')==dict(source=source,mutations=21,commands=44,result='LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE')
for version in ('0.161.0','0.163.0'):
 pr=obj('raw/'+version+'/PROVENANCE.json'); result=obj('raw/'+version+'/RESULT.json')
 assert pr['source']==source and pr['not_from_PATH']
 old=json.loads(git('show',source+':'+pr['prior_provenance']))
 assert pr['binary_sha256']==old['binary_sha256'] and pr['binary_bytes']==old['binary_bytes']
 for path,hash_ in pr['inputs'].items(): assert sha(git('show',source+':'+path))==hash_
 assert len(result['rows'])==5 and all(x['result']=='PASS' and not x['new_thread'] for x in result['rows'])
 assert result['model_turns']==0 and not result['external_credentials_inherited']
 assert all(x['reaped'] for x in result['cleanup']) and len(result['cleanup'])==2
 assert result['rows'][0]['initialized_connections']==2 and result['rows'][1]['identity_scope']=='uid_exe_only'
 assert all(not x['rpc_methods'] for x in result['rows'][2:])
compression=obj('LOG-COMPRESSION.json'); assert len(compression)==1
x=compression[0]; raw=gzip.decompress(read(x['stored_path'])); assert len(raw)==x['raw_bytes'] and sha(raw)==x['raw_sha256']
events=[json.loads(line) for line in raw.splitlines() if line.startswith(b'{')]
assert not any(x.get('Action')=='fail' for x in events)
summary=obj('GO-SUMMARY.json'); assert (summary['packages'],summary['passed'],summary['no_tests'],summary['failed'])==(66,53,13,0)
assert summary['package_results']==[x for x in events if x.get('Action') in ('pass','skip','fail') and 'Test' not in x]
assert summary['test_skips']==[dict(package=x['Package'],test=x['Test']) for x in events if x.get('Action')=='skip' and 'Test' in x]
assert len(summary['test_skips'])==31
print('VERIFY_OK source=%d evidence=%d helper=826 canonical=58+2 mutations=21 native=2x5 Go=53/53 conditional_skips=31'%(len(m['files']),len(e['files'])))
