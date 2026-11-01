"""Run all four cells against current product and immutable r46 source."""
import argparse,json,os,pathlib,subprocess,tempfile,sys
ROOT=pathlib.Path(__file__).resolve().parents[2]
BASE='2af469dd6919d8eb6064aa7da0e646f0851becf8'
def main():
 p=argparse.ArgumentParser();p.add_argument('--legacy-tree',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);p.add_argument('--helper-tree',type=pathlib.Path);p.add_argument('--python',default='/data/ai-dev-data/helper-deps-lock-venv/bin/python');a=p.parse_args();old=a.legacy_tree.resolve();a.output.mkdir(parents=True,exist_ok=True)
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=old,text=True).strip()==BASE,'r46 source must be fixed'
 assert (old/'VERSION').read_text().strip()=='0.2.3.21'
 bridge=old/'cmd/workpulse/r6_compat_local_fixture_test.go';assert not bridge.exists(),'refuse to overwrite existing test bridge'
 with tempfile.TemporaryDirectory(prefix='r6-compat-env-') as home:
  env={'HOME':home,'PATH':str(pathlib.Path(a.python).parent)+':/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','LANG':'C.UTF-8','TZ':'UTC','HUB_TEST_PG_STRICT':'1','DW_R6_COMPAT_PYTHON':a.python,'DW_R6_COMPAT_LEGACY_TREE':str(old),'DW_R6_COMPAT_NEW_TREE':str(ROOT)}
  if a.helper_tree:env['DW_R6_COMPAT_HELPER_TREE']=str(a.helper_tree.resolve())
  bridge.write_text((ROOT/'tests/r6-compat/legacy_hub_fixture.gotxt').read_text())
  try:
   for name,cwd,test,pkg in [('old-hub',old,'TestR6LegacyHubCompatibilityFixture','./cmd/workpulse'),('new-hub',ROOT,'TestR6HelperHubCompatibility','./internal/m8')]:
    with (a.output/(name+'.log')).open('w') as log:r=subprocess.run(['go','test','-tags=r6_compat','-v',pkg,'-run','^'+test+'$','-count=1','-timeout=120s'],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
    print(json.dumps({'fixture':name,'rc':r.returncode}),flush=True)
    if r.returncode:return r.returncode
  finally:bridge.unlink()
 records=[]
 for file in ('old-hub.log','new-hub.log'):
  for line in (a.output/file).read_text().splitlines():
   pos=line.find('{"result": "R6_COMPAT_PASS"')
   if pos>=0:records.append(json.loads(line[pos:]))
 assert len(records)==4,'all four cells required'
 (a.output/'results.json').write_text(json.dumps(records,ensure_ascii=False,indent=2)+'\n');print('R6_COMPAT_GRID_OK cells=4',flush=True);return 0
if __name__=='__main__':sys.exit(main())
