"""Run the supplemental consumer contract in an independent detached source tree."""
import argparse
import os
from pathlib import Path
import subprocess

p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);args=p.parse_args()
root=args.root.resolve();target=root/'internal/tempgroup/sendwait_production_probe_test.go'
if subprocess.check_output(['git','-C',str(root),'status','--porcelain']).strip():
    raise SystemExit('probe requires a clean detached source tree')
if target.exists():
    raise SystemExit('probe target already exists')
source=Path(__file__).with_name('production_groups_probe.go.fixture')
original_env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_','HUB_TEST_PG_DSN'))}
original_env['HUB_TEST_PG_STRICT']='1'
try:
    target.write_bytes(source.read_bytes())
    code=subprocess.call(['go','test','-v','-count=1','./internal/tempgroup','-run','TestSendwaitProductionGroupsReadOnlyProbe'],cwd=root,env=original_env)
finally:
    target.unlink()
raise SystemExit(code)
