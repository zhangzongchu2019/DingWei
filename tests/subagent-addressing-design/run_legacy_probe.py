"""Go overlay 与Python旧入口隔离复现；不写产品文件、不运行在线helper。"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace

ROOT=Path(__file__).resolve().parents[2]


def main():
 p=argparse.ArgumentParser();p.add_argument('--output',required=True);p.add_argument('--future',action='store_true');args=p.parse_args()
 output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=False)
 with tempfile.TemporaryDirectory(prefix='subagent-legacy-',dir='/tmp') as tmp:
  clean={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_')) and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR')}
  clean.update(HOME=tmp,CODEX_HOME=tmp+'/codex',SUBAGENT_EXPECT_PHASE1='1' if args.future else '0')
  original_home=os.environ.get('HOME','')
  clean.setdefault('GOCACHE',original_home+'/.cache/go-build')
  clean.setdefault('GOMODCACHE',original_home+'/go/pkg/mod')
  overlay=Path(tmp)/'overlay.json'
  overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/m8/subagent_design_probe_test.go'):str(Path(__file__).with_name('legacy_probe_test.go.txt')),str(ROOT/'internal/mcs/hubdriver/subagent_design_probe_test.go'):str(Path(__file__).with_name('legacy_online_probe_test.go.txt'))}}))
  run=subprocess.run(['go','test','-vet=off','-overlay='+str(overlay),'./internal/m8','./internal/mcs/hubdriver','-run','^TestSubagentDesign','-count=1','-v'],cwd=ROOT,env=clean,text=True,capture_output=True,timeout=180)
  (output/'go.log').write_text(run.stdout+run.stderr)
  sys.path[:0]=[str(ROOT/'tools/sessionhelper'),str(ROOT/'view-service')]
  from sessionhelper import send_dingwei
  from sessionhelper.routing import agent_route_envelope
  from sessionhelper.protocol import AddressBook
  import view_service
  # 只调纯函数；不调用ipc_send，不读取席位secret或在线目录。
  with __import__('unittest.mock',fromlist=['patch']).patch.dict(os.environ,{'SH_KEY_ID':'u_synthetic'}):
   completed=send_dingwei.complete_addr('r2.kimi.zzc')
  handler=SimpleNamespace(headers={'X-DW-Session':'r2.kimi.zzc'})
  visible=view_service._Handler._session(handler)
  envelope=agent_route_envelope('#r2.kimi.zzc synthetic',AddressBook('root','u_synthetic'))
  facts={'send_py':{'result':completed,'validates_depth':False},'view_service_header':{'result':visible,'validates_identity':False},'helper_legacy_route':{'to':envelope['to'],'validates_sub_path':False},'future_assertions':args.future,'go_rc':run.returncode}
  (output/'RESULT.json').write_text(json.dumps(facts,ensure_ascii=False,indent=2)+'\n')
  print(json.dumps(facts,ensure_ascii=False));print(run.stdout[-5000:]);print(run.stderr[-1000:])
  return run.returncode


if __name__=='__main__':raise SystemExit(main())
