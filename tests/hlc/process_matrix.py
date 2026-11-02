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
    p.add_argument('--case', choices=['S006', 'S011', 'S014', 'S014-hub-first', 'S014-live-failure'], required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--repeat', type=int, default=1)
    p.add_argument('--race', action='store_true')
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
    if args.case == 'S014-live-failure':
        anchor = '\tpublicShutdownListener := &shutdownListener{Listener: publicListener}'
        assert injected.count(anchor) == 1
        injected = injected.replace(anchor, '\tpublicListener = hlcCloseWriteFailListener{publicListener}\n' + anchor)
    sdk = sdk_source.read_text()
    sdk = sdk.replace('"sync/atomic"', '"sync/atomic"\n "time"\n "github.com/zhangzongchu2019/dingwei/internal/model"')
    sdk += (root / 'tests/hlc/sdk_fixture.go.txt').read_text()
    with tempfile.TemporaryDirectory(prefix='hlc-matrix-') as tmp:
        temp = Path(tmp)
        (temp / 'home').mkdir()
        (temp / 'tmp').mkdir()
        (temp / 'main.go').write_text(injected)
        (temp / 'sdk.go').write_text(sdk)
        overlay = temp / 'overlay.json'
        lifecycle_source = root / 'internal/m8/lifecycle.go'
        lifecycle = lifecycle_source.read_text()
        if args.case == 'S014':
            # Pin the tracked-set side of the real unregister/SIGTERM race.
            # CloseContext, WS Close and error classification remain real.
            assert lifecycle.count('delete(l.sockets, conn)') == 1
            lifecycle = lifecycle.replace('delete(l.sockets, conn)', '{ /* S014: retain tracked snapshot until SIGTERM */ }')
        (temp / 'lifecycle.go').write_text(lifecycle)
        overlay.write_text(json.dumps({'Replace': {
            str(main_source): str(temp / 'main.go'),
            str(sdk_source): str(temp / 'sdk.go'),
            str(lifecycle_source): str(temp / 'lifecycle.go'),
        }}))
        env = dict(TMPDIR=str(temp / 'tmp'), HOME=str(temp / 'home'), PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
                   GOCACHE='/home/dev/.cache/go-build', GOMODCACHE='/home/dev/go/pkg/mod', GOPATH='/home/dev/go',
                   LANG='C.UTF-8', TZ='UTC', HUB_TEST_PG_STRICT='1', HLC_LOAD_ENABLED='1',
                   HLC_CASE=args.case, HLC_MATRIX_EVIDENCE=str(out), HLC_REPO=str(root), HLC_REPEAT=str(args.repeat))
        command = ['go', 'test'] + (['-race'] if args.race else []) + ['-overlay', str(overlay), './cmd/workpulse', '-run', '^TestHLCLoadMatrix$', '-count='+str(args.repeat), '-shuffle=on', '-timeout=20m', '-v']
        source_hashes = {str(x.relative_to(root)): hashlib.sha256(x.read_bytes()).hexdigest() for x in
                         [main_source, sdk_source, root / 'cmd/workpulse/shutdown_load_test.go',
                          root / 'tests/hlc/sdk_fixture.go.txt', lifecycle_source, Path(__file__)]}
        (out / 'RESOURCES.json').write_text(json.dumps(dict(
            temporary_root=str(temp), home=str(temp / 'home'), tmp=str(temp / 'tmp'),
            ipc='No sessionhelper; isolated NOTIFY_SOCKET beneath Go test temp root',
            ports='loopback ephemeral ports selected by kernel; actual addresses recorded before Hub starts in RESOURCE lines',
            sqlite='unique hub.db beneath Go test temp root', pg='pgtest owned embedded instance, no external DSN inherited'), indent=2)+'\n')
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        log = out / (args.case + '-first.log')
        with log.open('w') as f:
            result = subprocess.run(command, cwd=root, env=env, stdout=f, stderr=subprocess.STDOUT)
        receipt = dict(case=args.case, command=command, started_utc=started,
                       ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), rc=result.returncode,
                       log=log.name, log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),
                       overlays=('Fixture setup + SDK transport; S014 additionally pins tracked socket snapshot; S014-live-failure injects real listener close-frame write timeout; classification/coordinator unchanged'),
                       race=args.race, repetitions=args.repeat, sources=source_hashes)
        (out / (args.case + '-receipt.json')).write_text(json.dumps(receipt, indent=2) + '\n')
        print(json.dumps(receipt))
        raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
