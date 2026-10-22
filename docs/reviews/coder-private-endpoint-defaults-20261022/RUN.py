"""Test-only follow-up: isolated real assertions and three designated mutations."""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,sys,tempfile
from datetime import datetime,timezone
BASE=Path(__file__).resolve().parent;ROOT=BASE.parents[2]
source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
rows=[]
with tempfile.TemporaryDirectory(prefix='pce-def-') as td:
 tmp=Path(td);package=tmp/'helper'
 shutil.copytree(ROOT/'tools/sessionhelper',package,ignore=shutil.ignore_patterns('__pycache__','*.pyc','.venv','venv'))
 home=tmp/'h';home.mkdir(mode=0o700)
 env={'HOME':str(home),'TMPDIR':str(tmp),'PATH':str(Path(sys.executable).parent)+':/usr/bin:/bin','PYTHONPATH':str(package)+os.pathsep+str(package/'tests'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8'}
 def run(label,case):
  cmd=[sys.executable,'-B','-m','unittest','-v',case]
  start=datetime.now(timezone.utc).isoformat();p=subprocess.run(cmd,env=env,cwd=tmp,capture_output=True,timeout=180)
  (BASE/(label+'.stdout')).write_bytes(p.stdout);(BASE/(label+'.stderr')).write_bytes(p.stderr);(BASE/(label+'.rc')).write_text(str(p.returncode)+'\n')
  rows.append({'label':label,'command':cmd,'start':start,'end':datetime.now(timezone.utc).isoformat(),'rc':p.returncode})
  return p
 assert run('module','test_private_endpoint').returncode==0
 mutations=[
 ('config-dataclass','config.py','codex_endpoint_mode: str = "legacy"','codex_endpoint_mode: str = "private"','test_unset_endpoint_mode_load_config_remains_legacy','CONFIG_DATACLASS_DEFAULT_PRIVATE'),
 ('config-loader','config.py','env.get("SH_CODEX_ENDPOINT_MODE", "legacy")','env.get("SH_CODEX_ENDPOINT_MODE", "private")','test_unset_endpoint_mode_load_config_remains_legacy','CONFIG_UNSET_ENDPOINT_DEFAULT_PRIVATE'),
 ('driver-default','codexDriver.py',"env_get('SH_CODEX_ENDPOINT_MODE', 'legacy')","env_get('SH_CODEX_ENDPOINT_MODE', 'private')",'test_unset_endpoint_mode_driver_remains_legacy','DRIVER_UNSET_ENDPOINT_DEFAULT_PRIVATE')]
 for label,file,old,new,case,marker in mutations:
  path=package/'sessionhelper'/file;original=path.read_text();assert original.count(old)==1
  try:
   path.write_text(original.replace(old,new));p=run(label+'-withdrawn','test_private_endpoint.DriverWiring.'+case)
   assert p.returncode!=0 and marker.encode() in p.stderr
   rows[-1].update(designated_red=True,marker=marker)
  finally:path.write_text(original)
  assert run(label+'-restored','test_private_endpoint.DriverWiring.'+case).returncode==0
 test=ROOT/'tools/sessionhelper/tests/test_private_endpoint.py'
 (BASE/'RESULT.json').write_text(json.dumps({'source':source,'passed':True,'module_tests':44,'mutations':3,'rows':rows,'env_keys':sorted(env),'python':sys.executable,'test_sha256':hashlib.sha256(test.read_bytes()).hexdigest(),'scope':'isolated HOME, copied helper only; no native daemon or online seat'},indent=2)+'\n')
 print('DEFAULTS_TEST_ONLY_PASS module44 mutations3 restored3')
