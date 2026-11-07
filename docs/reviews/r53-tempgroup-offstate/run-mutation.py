import pathlib,subprocess,os,datetime,json,hashlib
root=pathlib.Path('/tmp/r53-offstate-mutant');out=pathlib.Path('/tmp/r53-offstate-gates');p=root/'internal/m8/console_auth.go';before=p.read_bytes();old="function tempGroupsEnabled(){return document.getElementById('tempGroups').dataset.enabled==='true';}";new='function tempGroupsEnabled(){return true;}'
s=before.decode();assert s.count(old)==1;p.write_text(s.replace(old,new,1))
(out/'mutation.diff').write_bytes(subprocess.check_output(['git','diff','--','internal/m8/console_auth.go'],cwd=root))
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_'))};env.update(NODE_PATH='/tmp/coder-a3-playwright/node_modules',TMPDIR='/tmp',TZ='UTC')
cmd=['node','view-service/console-temp-groups-offstate-regression.js'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
with (out/'mutation.log').open('w') as f:r=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=120)
raw=(out/'mutation.log').read_text();result={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'command':cmd,'mutation':'Only replace shared capability predicate with return true; no test or fixture changes','started_utc':start,'ended_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':r.returncode,'expected_assertion':'关态必须零 /api/console/temp-groups 请求','killed_at_expected_assertion':r.returncode!=0 and '关态必须零 /api/console/temp-groups 请求' in raw,'before_sha256':hashlib.sha256(before).hexdigest(),'mutated_sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
p.write_bytes(before);result['restored_sha256']=hashlib.sha256(p.read_bytes()).hexdigest();(out/'mutation.json').write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n');print(json.dumps(result,ensure_ascii=False))
if not result['killed_at_expected_assertion']:raise SystemExit(1)
