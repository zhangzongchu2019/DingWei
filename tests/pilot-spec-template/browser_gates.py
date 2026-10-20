#!/usr/bin/env python3
"""Exact-tree, loopback browser regressions with an isolated HOME and offline Go."""
import argparse,datetime,io,json,subprocess,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SUITES=['pilot-spec-template-browser-regression.js','pilot-feedback-browser-regression.js','pilot-blocker-wake-browser-regression.js','r32-spec-review-browser-regression.js','r32-task-flow-browser-regression.js','r31-hosted-nodes-browser-regression.js','console-browser-regression.js','console-agent-mgmt-enroll-regression.js','console-agent-groups-vega-regression.js','console-task-board-regression.js']
def run(args):
 sha=subprocess.check_output(['git','rev-parse',args.commit+'^{commit}'],cwd=ROOT,text=True).strip();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
 with tempfile.TemporaryDirectory(prefix='pilot-template-browser-') as temp:
  root=Path(temp)/'source';root.mkdir();home=Path(temp)/'home';home.mkdir(mode=0o700);(home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
  archive=subprocess.check_output(['git','archive',sha],cwd=ROOT)
  with tarfile.open(fileobj=io.BytesIO(archive))as tar:tar.extractall(root,filter='data')
  env={'HOME':str(home),'PATH':'/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','NODE_PATH':'/tmp/coder-a3-playwright/node_modules','PW_CHANNEL':'chrome'}
  for suite in SUITES:
   argv=['node','view-service/'+suite];start=datetime.datetime.now(datetime.timezone.utc).isoformat();p=subprocess.run(argv,cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=240);(out/(suite+'.log')).write_text(p.stdout)
   (out/(suite+'.json')).write_text(json.dumps(dict(commit=sha,argv=argv,rc=p.returncode,start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat(),isolated_home=True,git_export=True,live_access=False),indent=2)+'\n');print(suite,p.returncode,flush=True)
   if p.returncode:raise AssertionError('browser failed '+suite)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);run(p.parse_args())
