#!/usr/bin/env python3
"""隔离实进程矩阵；overlay 只加负载制造钩子，不替换停机实现。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import datetime


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--case', choices=['S006', 'S011'], required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--repeat', type=int, default=1)
    p.add_argument('--race', action='store_true')
    p.add_argument('--mutant', choices=['no-sse-cancel', 'no-ws-close', 'no-callback-seal'])
    args = p.parse_args()
    root = Path(__file__).resolve().parents[2]
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    main_source = root / 'cmd/workpulse/main.go'
    sdk_source = root / 'internal/feishu/lifecycle.go'
    original = main_source.read_text()
    anchor = '\tinternalAddr := strings.TrimSpace(os.Getenv("WP_INTERNAL_ADDR"))'
    assert original.count(anchor) == 1
    injected = original.replace(anchor, '''
	if os.Getenv("HLC_LOAD_CHILD") == "1" {
		hlcReceiverFactory = feishu.HLCFixtureReceiver
		feishuReceiver = hlcLoadSetup(prefixHub, st, mux, internalMux)
	}
''' + anchor)
    sdk = sdk_source.read_text()
    sdk = sdk.replace('"sync/atomic"', '"sync/atomic"\n "time"\n "github.com/zhangzongchu2019/dingwei/internal/model"')
    sdk += (root / 'tests/hlc/sdk_fixture.go.txt').read_text()
    with tempfile.TemporaryDirectory(prefix='hlc-matrix-') as tmp:
        temp = Path(tmp)
        (temp / 'home').mkdir()
        (temp / 'main.go').write_text(injected)
        (temp / 'sdk.go').write_text(sdk)
        replacements = {
            str(main_source): str(temp / 'main.go'),
            str(sdk_source): str(temp / 'sdk.go'),
        }
        if args.mutant == 'no-sse-cancel':
            source = root / 'internal/m8/terminal.go'
            body = source.read_text()
            a = body.index('func (h *Hub) HandleInternalViewEvents(')
            b = body.index('// ---- C4:', a)
            method = body[a:b]
            cancel = 'case <-r.Context().Done():\n\t\t\treturn\n'
            closed = 'if !ok {\n\t\t\t\treturn\n\t\t\t}'
            assert method.count(cancel) == 1 and method.count(closed) == 1
            method = method.replace(cancel, 'case <-r.Context().Done():\n if h.lifetime().ctx.Err()==nil{return};r=r.WithContext(context.WithoutCancel(r.Context()))\n').replace(closed, 'if !ok {ch=nil;continue}')
            (temp / 'terminal.go').write_text(body[:a] + method + body[b:])
            replacements[str(source)] = str(temp / 'terminal.go')
        if args.mutant == 'no-ws-close':
            source = root / 'internal/m8/lifecycle.go'
            body = source.read_text()
            close = 'err := c.Close(websocket.StatusGoingAway, "hub shutdown")'
            assert body.count(close) == 1
            (temp / 'lifecycle.go').write_text(body.replace(close, '_ = c; err := error(nil)'))
            replacements[str(source)] = str(temp / 'lifecycle.go')
        if args.mutant == 'no-callback-seal':
            source = root / 'internal/feishu/feishu.go'
            body = source.read_text()
            guard = 'if !ok {\n\t\tg.receiver.rejected.Add(1)\n\t\treturn ErrReceiverDraining\n\t}'
            assert body.count(guard) == 1
            (temp / 'feishu.go').write_text(body.replace(guard, 'if !ok {done=func(){}}'))
            replacements[str(source)] = str(temp / 'feishu.go')
        overlay = temp / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': replacements}))
        env = dict(HOME=str(temp / 'home'), PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
                   GOCACHE='/home/dev/.cache/go-build', GOMODCACHE='/home/dev/go/pkg/mod', GOPATH='/home/dev/go',
                   LANG='C.UTF-8', TZ='UTC', HUB_TEST_PG_STRICT='1', HLC_LOAD_ENABLED='1',
                   HLC_CASE=args.case, HLC_MATRIX_EVIDENCE=str(out), HLC_REPO=str(root), HLC_REPEAT=str(args.repeat))
        command = ['go', 'test'] + (['-race'] if args.race else []) + ['-overlay', str(overlay), './cmd/workpulse', '-run', '^TestHLCLoadMatrix$', '-count='+str(args.repeat), '-shuffle=on', '-timeout=20m', '-v']
        source_hashes = {str(x.relative_to(root)): hashlib.sha256(x.read_bytes()).hexdigest() for x in
                         [main_source, sdk_source, root / 'cmd/workpulse/shutdown_load_test.go',
                          root / 'tests/hlc/sdk_fixture.go.txt', Path(__file__)]}
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        log = out / (args.case + '-first.log')
        with log.open('w') as f:
            result = subprocess.run(command, cwd=root, env=env, stdout=f, stderr=subprocess.STDOUT)
        receipt = dict(case=args.case, command=command, started_utc=started,
                       ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), rc=result.returncode,
                       log=log.name, log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),
                       overlays='Only fixture setup injection + test SDK transport; shutdown coordinator/handlers/queues unchanged',
                       race=args.race, repetitions=args.repeat, sources=source_hashes)
        if args.mutant:
            receipt['mutation'] = args.mutant
            raw = out / (args.case + '-shutdown.json')
            observed = json.loads(raw.read_text()) if raw.exists() else {}
            report = observed.get('report', {})
            stages = report.get('stages', [])
            timeout = report.get('exit_code') == 2 and len(stages) == 5 and stages[1]['timeout']
            if args.mutant == 'no-callback-seal':
                receipt['target_red'] = result.returncode == 1 and 'S006_POST_SEAL_DB_ZERO' in log.read_text()
            else:
                initial = json.loads((out / (args.case + '-load.json')).read_text()) if (out / (args.case + '-load.json')).exists() else {}
                receipt['target_red'] = result.returncode == 1 and timeout and initial.get('hub', {}).get('session_ws') == 20 and 'EXPECT_EXIT0' in log.read_text()
                receipt['target_assertion'] = 'S009_LONG_STREAM_DRAIN' if args.mutant == 'no-sse-cancel' else 'S005_WS_DRAIN'
        (out / (args.case + '-receipt.json')).write_text(json.dumps(receipt, indent=2) + '\n')
        print(json.dumps(receipt))
        raise SystemExit(0 if receipt.get('target_red') else result.returncode)


if __name__ == '__main__':
    main()
