#!/usr/bin/env python3
"""serverInfo omission/drift mutations checked through inert Go source overlays."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'internal/mcpgw/gateway.go'
LIST_META = ', "_meta": serverInfoMeta()'
DRIFTED_META = ', "_meta": map[string]any{metaPrefix + "serverInfo": map[string]string{"name": "mutant-name", "version": "mutant-version"}}'
CASES = [
    ('omit_tools_list_serverinfo', LIST_META, '', 'MCP_SERVERINFO_TOOLS_LIST'),
    ('different_tools_list_literals', LIST_META, DRIFTED_META, 'MCP_SERVERINFO_SHARED_VALUES'),
]

def main():
    source = SOURCE.read_text()
    env = dict(os.environ)
    env.pop('GOFLAGS', None)
    env['HUB_TEST_PG_EMBEDDED'] = '1'
    env['HUB_TEST_PG_STRICT'] = '1'
    env['HUB_TEST_PG_SKIP_OK'] = '0'
    ok = True
    for name, old, new, marker in CASES:
        if source.count(old) != 2:
            print(f'FAIL {name}: shared helper anchor count={source.count(old)}, want 2 route occurrences')
            ok = False
            continue
        # Mutate only the tools/list occurrence; keep discover and tools/call intact.
        before, tail = source.split('case "tools/list":', 1)
        if tail.count(old) < 1:
            print(f'FAIL {name}: tools/list anchor missing')
            ok = False
            continue
        mutated_source = before + 'case "tools/list":' + tail.replace(old, new, 1)
        with tempfile.TemporaryDirectory(prefix='mcp-serverinfo-mutation-') as td:
            td = Path(td)
            mutated = td / 'gateway.go'
            mutated.write_text(mutated_source)
            overlay = td / 'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(SOURCE): str(mutated)}}))
            cmd = ['go', 'test', '-overlay', str(overlay), './internal/mcpgw', '-run', '^TestServerInfoPresentInEvery2026Result$', '-count=1', '-timeout=3m']
            run = subprocess.run(cmd, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=210)
            passed = run.returncode == 1 and marker in run.stdout and '--- FAIL:' in run.stdout and '[build failed]' not in run.stdout and 'setup failed' not in run.stdout
            print(('PASS ' if passed else 'FAIL ') + f'{name} rc={run.returncode} witness={marker}')
            if not passed:
                print(run.stdout[-4000:])
                ok = False
    raise SystemExit(0 if ok else 1)

if __name__ == '__main__': main()
