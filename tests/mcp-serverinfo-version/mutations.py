#!/usr/bin/env python3
"""Reject version drift in 2026 and 2025 server identity paths using Go overlays."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    ('legacy_initialize_literal', 'internal/mcpgw/codec2025.go', '"version": serverInfoVersion', '"version": "r53"', '', './internal/mcpgw', 'TestCompat2025LifecycleAndFourTools', 'MCP_2025_INITIALIZE_SERVERINFO_VERSION'),
    ('2026_identity_literal', 'internal/mcpgw/gateway.go', 'const serverInfoVersion = dwversion.Version', 'const serverInfoVersion = "r53"', '\tdwversion \"github.com/zhangzongchu2019/dingwei/internal/version\"\n', './internal/mcpgw', 'TestServerInfoPresentInEvery2026Result', 'MCP_SERVERINFO_VERSION'),
]

def main():
    env = dict(os.environ)
    env.pop('GOFLAGS', None)
    env['HUB_TEST_PG_EMBEDDED'] = '1'
    env['HUB_TEST_PG_STRICT'] = '1'
    env['HUB_TEST_PG_SKIP_OK'] = '0'
    ok = True
    for name, relative, old, new, import_line, package, test, marker in CASES:
        source_path = ROOT / relative
        source = source_path.read_text()
        if source.count(old) != 1:
            print(f'FAIL {name}: anchor count={source.count(old)}')
            ok = False
            continue
        with tempfile.TemporaryDirectory(prefix='mcp-sinfo-version-mut-') as td:
            td = Path(td)
            replacement = td / source_path.name
            mutated_source = source.replace(old, new, 1)
            if import_line:
                if mutated_source.count(import_line) != 1:
                    print(f'FAIL {name}: import cleanup anchor count={mutated_source.count(import_line)}')
                    ok = False
                    continue
                mutated_source = mutated_source.replace(import_line, '', 1)
            replacement.write_text(mutated_source)
            overlay = td / 'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(source_path): str(replacement)}}))
            cmd = ['go', 'test', '-overlay', str(overlay), package, '-run', '^'+test+'$', '-count=1', '-timeout=3m']
            try:
                run = subprocess.run(cmd, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=210)
            except subprocess.TimeoutExpired as exc:
                print(f'FAIL {name}: timeout')
                print((exc.stdout or '')[-4000:])
                ok = False
                continue
            passed = run.returncode == 1 and marker in run.stdout and '--- FAIL:' in run.stdout and '[build failed]' not in run.stdout and 'setup failed' not in run.stdout
            print(('PASS ' if passed else 'FAIL ') + f'{name} rc={run.returncode} witness={marker}')
            if not passed:
                print(run.stdout[-4000:])
                ok = False
    raise SystemExit(0 if ok else 1)

if __name__ == '__main__': main()
