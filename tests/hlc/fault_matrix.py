#!/usr/bin/env python3
"""真实Hub故障/监督器格；不改变生产预算，仅在overlay注入受控故障和耗时。"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--case', choices=['S002', 'S003', 'S007', 'S012', 'S013', 'S010', 'S010-bind'], required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--withhold-watchdog', action='store_true')
    p.add_argument('--omit-internal-result', action='store_true')
    args = p.parse_args()
    root = Path(__file__).resolve().parents[2]
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    main_path = root / 'cmd/workpulse/main.go'
    source = main_path.read_text()
    anchor = '\tinternalAddr := strings.TrimSpace(os.Getenv("WP_INTERNAL_ADDR"))'
    assert source.count(anchor) == 1
    source = source.replace(anchor, '\tif os.Getenv("HLC_LOAD_CHILD")=="1" {feishuReceiver=hlcFaultSetup(prefixHub,st,mux,internalMux,"' + args.case + '")}\n' + anchor)
    if args.case == 'S012':
        anchor = 'err := internalSrv.Shutdown(drainCtx)'
        assert source.count(anchor) == 1
        source = source.replace(anchor, anchor+'\n err=errors.New("hlc injected internal close failure")')
        if args.omit_internal_result:
            anchor = 'results.record("internal", internalShutdownListener, err)'
            assert source.count(anchor) == 1
            source = source.replace(anchor, '// mutant: omit internal result')
    if args.case == 'S013':
        delays = {'Stop: func(stopCtx context.Context) error {': 800,
                  'Flush: func(queueStage context.Context) error {': 2700,
                  'Storage: func(context.Context) error {': 800,
                  'Finalize: func(finalCtx context.Context) error {': 1300}
        for anchor, delay in delays.items():
            assert source.count(anchor) == 1, anchor
            source = source.replace(anchor, anchor+'\n time.Sleep('+str(delay)+'*time.Millisecond)')
    with tempfile.TemporaryDirectory(prefix='hlc-fault-') as temp:
        tmp = Path(temp)
        (tmp / 'home').mkdir()
        (tmp / 'main.go').write_text(source)
        replacements = {str(main_path): str(tmp / 'main.go')}
        if args.withhold_watchdog:
            path = root / 'cmd/workpulse/hardening.go'
            body = path.read_text()
            anchor = 'func (w *systemdWatchdog) notify() {'
            assert body.count(anchor) == 1
            body = body.replace(anchor, 'var hlcFirstShutdownWatch atomic.Bool\n'+anchor+'\n if w.draining.Load() && !hlcFirstShutdownWatch.CompareAndSwap(false,true) {return}')
            (tmp / 'hardening.go').write_text(body)
            replacements[str(path)] = str(tmp / 'hardening.go')
        overlay = tmp / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': replacements}))
        env = dict(HOME=str(tmp / 'home'), PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
                   GOCACHE='/home/dev/.cache/go-build', GOMODCACHE='/home/dev/go/pkg/mod', GOPATH='/home/dev/go',
                   LANG='C.UTF-8', TZ='UTC', HLC_FAULT_ENABLED='1', HLC_FAULT_CASE=args.case,
                   HLC_MATRIX_EVIDENCE=str(out), HLC_WATCHDOG_MUTANT='1' if args.withhold_watchdog else '0')
        test_name = '^TestHLCBindFailure$' if args.case == 'S010-bind' else '^TestHLCFaultProcessMatrix$'
        command = ['go','test','-overlay',str(overlay),'./cmd/workpulse','-run',test_name,'-count=1','-timeout=60s','-v']
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        log = out / (args.case+'-test.log')
        with log.open('w') as f:
            result = subprocess.run(command,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
        receipt = dict(case=args.case,command=command,started_utc=started,ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                       rc=result.returncode,withhold_watchdog=args.withhold_watchdog,log=log.name,sha256=hashlib.sha256(log.read_bytes()).hexdigest())
        if args.withhold_watchdog:
            receipt['target_red'] = result.returncode==1 and 'S013_CONTINUOUS_WATCHDOG: mutant interrupted' in log.read_text()
        if args.omit_internal_result:
            receipt['target_red'] = result.returncode==1 and 'S012_BOTH_SERVER_RESULTS' in log.read_text()
        (out / 'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        print(json.dumps(receipt))
        raise SystemExit(0 if receipt.get('target_red') else result.returncode)


if __name__ == '__main__':
    main()
