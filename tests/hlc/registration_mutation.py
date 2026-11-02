#!/usr/bin/env python3
"""撤掉Hub注册封口，真实20WS注册竞争格须在晚到准入断言红。"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output',type=Path,required=True)
    args=p.parse_args();root=Path(__file__).resolve().parents[2];out=args.output.resolve();out.mkdir(parents=True,exist_ok=True)
    source=root/'internal/m8/lifecycle.go';body=source.read_text();anchor='func (h *Hub) SealRegistrations() { h.lifetime().registrations.Seal() }'
    assert body.count(anchor)==1
    mutated=body.replace(anchor,'func (h *Hub) SealRegistrations() {}')
    with tempfile.TemporaryDirectory(prefix='hlc-registration-mutation-') as temp:
        tmp=Path(temp);(tmp/'home').mkdir();(tmp/'lifecycle.go').write_text(mutated);overlay=tmp/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(source):str(tmp/'lifecycle.go')}}))
        env=dict(HOME=str(tmp/'home'),PATH='/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',GOCACHE='/home/dev/.cache/go-build',GOMODCACHE='/home/dev/go/pkg/mod',GOPATH='/home/dev/go',LANG='C.UTF-8',TZ='UTC')
        command=['go','test','-race','-overlay',str(overlay),'./internal/m8','-run','^TestHLCRegistrationBarrierTwentyWebSockets$','-count=1','-timeout=40s','-v']
        started=datetime.datetime.now(datetime.timezone.utc).isoformat();log=out/'no-registration-seal.log'
        with log.open('w') as f:r=subprocess.run(command,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
        raw=log.read_text();receipt=dict(command=command,started_utc=started,ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),rc=r.returncode,
          source_sha256=hashlib.sha256(body.encode()).hexdigest(),mutant_sha256=hashlib.sha256(mutated.encode()).hexdigest(),log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),
          target_assertion='S005_REGISTRATION_AFTER_SEAL',target_red=r.returncode==1 and 'S005_REGISTRATION_AFTER_SEAL: late upgrade admitted' in raw and 'test timed out' not in raw)
        (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt))
        if not receipt['target_red']:raise SystemExit(1)


if __name__=='__main__':main()
