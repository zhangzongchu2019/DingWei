#!/usr/bin/env python3
"""在指定只读Git锚的独立worktree复现原起步段，只观测SIGTERM前409。"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--base',required=True)
    p.add_argument('--rounds',type=int,default=100)
    p.add_argument('--output',type=Path,required=True)
    args=p.parse_args()
    root=Path(__file__).resolve().parents[2]
    out=args.output.resolve();out.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='hlc-startup-control-') as temp:
        tmp=Path(temp);tree=tmp/'worktree'
        subprocess.run(['git','worktree','add','--detach',str(tree),args.base],cwd=root,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        try:
            fixture=root/'tests/hlc/startup_readiness_test.go.txt'
            (tree/'cmd/workpulse/hlc_readiness_control_test.go').write_bytes(fixture.read_bytes())
            (tmp/'home').mkdir()
            env=dict(HOME=str(tmp/'home'),PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
                     GOCACHE='/home/dev/.cache/go-build',GOMODCACHE='/home/dev/go/pkg/mod',GOPATH='/home/dev/go',LANG='C.UTF-8',TZ='UTC',HUB_TEST_PG_STRICT='1',
                     HLC_READINESS_ENABLED='1',HLC_READINESS_OUTPUT=str(out),HLC_READINESS_BASE=args.base,HLC_READINESS_ROUNDS=str(args.rounds))
            cmd=['go','test','-race','./cmd/workpulse','-run','^TestHLCStartupReadinessControl$','-count=1','-timeout=10m','-v']
            started=datetime.datetime.now(datetime.timezone.utc).isoformat();log=out/'test.log'
            with log.open('w') as f:result=subprocess.run(cmd,cwd=tree,env=env,stdout=f,stderr=subprocess.STDOUT)
            receipt=dict(base=args.base,command=cmd,rc=result.returncode,started_utc=started,ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),fixture_sha256=hashlib.sha256(fixture.read_bytes()).hexdigest(),source_changes='one untracked test fixture only; product tree unchanged')
            (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt))
        finally:
            subprocess.run(['git','worktree','remove','--force',str(tree)],cwd=root,check=True)
        raise SystemExit(result.returncode)


if __name__=='__main__':main()
