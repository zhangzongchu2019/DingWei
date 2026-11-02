#!/usr/bin/env python3
"""S007-full：真实混合负载另加两个不退出请求，只用隔离测试装配 overlay。"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--repeat',type=int,default=1)
    parser.add_argument('--race',action='store_true')
    parser.add_argument('--mutant-timeout-code',action='store_true')
    args=parser.parse_args()
    if args.repeat<1:
        parser.error('--repeat must be positive')
    root=Path(__file__).resolve().parents[2]
    out=args.output.resolve();out.mkdir(parents=True,exist_ok=True)
    main_source=root/'cmd/workpulse/main.go'
    original=main_source.read_text()
    anchor='\tinternalAddr := strings.TrimSpace(os.Getenv("WP_INTERNAL_ADDR"))'
    assert original.count(anchor)==1
    injected=original.replace(anchor,'''
    if os.Getenv("HLC_LOAD_CHILD") == "1" {
        hlcReceiverFactory = feishu.HLCFixtureReceiver
        feishuReceiver = hlcFullFaultSetup(prefixHub, st, mux, internalMux)
    }
'''+anchor)
    sdk_source=root/'internal/feishu/lifecycle.go'
    sdk=sdk_source.read_text().replace('"sync/atomic"','"sync/atomic"\n "time"\n "github.com/zhangzongchu2019/dingwei/internal/model"')
    sdk+=(root/'tests/hlc/sdk_fixture.go.txt').read_text()
    with tempfile.TemporaryDirectory(prefix='h7-',dir='/tmp') as directory:
        temp=Path(directory);(temp/'home').mkdir();(temp/'tmp').mkdir()
        (temp/'main.go').write_text(injected);(temp/'sdk.go').write_text(sdk)
        replacements={str(main_source):str(temp/'main.go'),str(sdk_source):str(temp/'sdk.go')}
        if args.mutant_timeout_code:
            source=root/'cmd/workpulse/shutdown.go'
            text=source.read_text();anchor='report.ExitCode = 2'
            assert text.count(anchor)==1
            (temp/'shutdown.go').write_text(text.replace(anchor,'report.ExitCode = 1 // withdrawn: timeout exit classification'))
            replacements[str(source)]=str(temp/'shutdown.go')
        overlay=temp/'overlay.json';overlay.write_text(json.dumps({'Replace':replacements}))
        env=dict(HOME=str(temp/'home'),TMPDIR=str(temp/'tmp'),
                 PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
                 GOCACHE='/home/dev/.cache/go-build',GOMODCACHE='/home/dev/go/pkg/mod',GOPATH='/home/dev/go',
                 LANG='C.UTF-8',TZ='UTC',HUB_TEST_PG_STRICT='1',
                 HLC_S007_FULL_ENABLED='1',HLC_CASE='S007-full',HLC_MATRIX_EVIDENCE=str(out),
                 HLC_REPO=str(root),HLC_REPEAT=str(args.repeat))
        command=['go','test']+(['-race'] if args.race else [])+[
            '-overlay',str(overlay),'./cmd/workpulse','-run','^TestHLCS007Full$',
            '-count='+str(args.repeat),'-shuffle=on','-timeout=10m','-v']
        sources=[main_source,sdk_source,root/'cmd/workpulse/shutdown.go',
                 root/'cmd/workpulse/shutdown_load_test.go',
                 root/'cmd/workpulse/shutdown_mixed_fault_process_test.go',
                 root/'tests/hlc/sdk_fixture.go.txt',Path(__file__)]
        hashes={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sources}
        (out/'RESOURCES.json').write_text(json.dumps(dict(home=env['HOME'],tmp=env['TMPDIR'],
            ipc='Unique NOTIFY_SOCKET under each Go test temp root; no sessionhelper',
            ports='Kernel-selected loopback ports; recorded before each Hub/VS launch',
            pg='Owned pgtest instance; no inherited external DSN',source_sha256=hashes),indent=2)+'\n')
        started=datetime.datetime.now(datetime.timezone.utc).isoformat()
        log=out/'S007-full.log'
        with log.open('w') as f:
            result=subprocess.run(command,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
        receipt=dict(command=command,started_utc=started,ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                     rc=result.returncode,race=args.race,repeat=args.repeat,mutant=args.mutant_timeout_code,
                     log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),sources=hashes,
                     overlay='Test routes + SDK transport; production shutdown, budgets, lifecycle unchanged except explicitly withdrawn timeout code mutant')
        if args.mutant_timeout_code:
            reports=[json.loads(p.read_text()) for p in out.rglob('S007-full-shutdown.json')]
            receipt['target_red']=result.returncode==1 and 'S007_FULL_EXIT_CODE' in log.read_text() and len(reports)==args.repeat and all(
                r['process_exit_code']==1 and r['report']['exit_code']==1 and r['report']['stages'][1]['timeout']
                for r in reports)
        (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        print(json.dumps(receipt),flush=True)
        if args.mutant_timeout_code:
            raise SystemExit(0 if receipt['target_red'] else 1)
        raise SystemExit(result.returncode)

if __name__=='__main__':
    main()
