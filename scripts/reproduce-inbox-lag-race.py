#!/usr/bin/env python3
"""Repeat the legacy wire test, or restore its baseline body with a controlled publication gate."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BASE = '549f64e280f2c5496bffb506c709c1bf0a1c504d'
TEST = 'TestInboxLagObservationalReceiptPreservesLegacyDirectDelivery'
PATH = 'internal/m8/inbox_lag_test.go'
MARKER = 'func '+TEST+'(t *testing.T) {'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rounds', type=int, default=50)
    parser.add_argument('--reverse', action='store_true')
    args = parser.parse_args()
    command = ['go', 'test', '-json', '-count='+str(args.rounds), '-run=^'+TEST+'$', './internal/m8']
    env = {k: v for k, v in os.environ.items() if not k.startswith(('SH_', 'DW_', 'DINGWEI_', 'DATAD_'))}
    env['TZ'] = 'UTC'
    with tempfile.TemporaryDirectory(prefix='inbox-lag-repeat-') as tmp:
        if args.reverse:
            # Restore only the original test body. All product bytes remain current.
            baseline = subprocess.check_output(['git', 'show', BASE+':'+PATH], cwd=ROOT, text=True)
            original = MARKER+baseline.split(MARKER, 1)[1]
            original = original.replace('e := newAgentEnv(t)', '''e := newAgentEnv(t)
    blocked, release := make(chan struct{}), make(chan struct{})
    defer func() { select { case <-release: default: close(release) } }()
    e.hub.sessionBeforePublishHook = func(key, name string) {
        if name == "laglegacy" { close(blocked); <-release }
    }''', 1)
            original = original.replace('e.dial(e.cred, "")', 'e.dial(e.cred, "version=0.2.3.14")', 1)
            original = original.replace('defer source.CloseNow()', '''defer source.CloseNow()
    waitFor(t, 10*time.Second, "source published", func() bool {
        return e.hub.sessionOnline(AgentKeyID("zzc"), "opus50ts")
    })''', 1)
            original = original.replace('defer target.CloseNow()', '''defer target.CloseNow()
    select { case <-blocked: case <-ctx.Done(): t.Fatal("publication gate not reached") }''', 1)
            original = original.replace('got := readEnvelope(t, ctx, target)', '''// Observe actual rejection before releasing publication: no timing sleep.
    for {
        rejection := readEnvelope(t, ctx, source)
        if metaString(rejection.Meta, "type") == "delivery_failure" {
            if !strings.Contains(rejection.Body, "lag-legacy-observed") {
                t.Fatal("uncorrelated delivery failure", rejection)
            }
            t.Log("CONTROLLED_UNPUBLISHED_TARGET_REJECTED", rejection.Body)
            break
        }
    }
    close(release)
    got := readEnvelope(t, ctx, target)''', 1)
            source = (ROOT/PATH).read_text().split(MARKER, 1)[0]+original
            overlay_source = Path(tmp)/'inbox_lag_test.go'
            overlay_source.write_text(source)
            overlay = Path(tmp)/'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(ROOT/PATH): str(overlay_source)}}))
            command.insert(2, '-overlay='+str(overlay))
        result = subprocess.run(command, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        print(result.stdout, end='', flush=True)
        events = [json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
        passes = sum(e.get('Action') == 'pass' and e.get('Test') == TEST for e in events)
        failures = sum(e.get('Action') == 'fail' and e.get('Test') == TEST for e in events)
        output = ''.join(e.get('Output', '') for e in events)
        valid = (result.returncode == 1 and failures == args.rounds and passes == 0
                 and output.count('CONTROLLED_UNPUBLISHED_TARGET_REJECTED') == args.rounds
                 and output.count('read envelope: failed to get reader: context deadline exceeded') == args.rounds) if args.reverse else (result.returncode == 0 and passes == args.rounds and failures == 0)
        print(json.dumps({'rounds': args.rounds, 'reverse': args.reverse, 'go_rc': result.returncode, 'pass': passes, 'fail': failures, 'expected_result': valid}), flush=True)
        return (1 if args.reverse else 0) if valid else 2


if __name__ == '__main__':
    raise SystemExit(main())
