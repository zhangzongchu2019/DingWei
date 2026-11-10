#!/usr/bin/env python3
"""三项 tools/list CacheableResult 语义撤回；只通过 Go overlay 改临时源码。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'internal/mcpgw/gateway.go'
CASES = [
    ('omit_ttl_ms', '"ttlMs": toolsListTTLMS, ', '', 'MCP_LIST_TTL_MS'),
    ('string_ttl_ms', 'const toolsListTTLMS = 300000', 'const toolsListTTLMS = "300000"', 'MCP_LIST_TTL_MS'),
    ('invalid_cache_scope', 'const toolsListCacheScope = "private"', 'const toolsListCacheScope = "bogus"', 'MCP_LIST_CACHE_SCOPE'),
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
        if source.count(old) != 1:
            print(f'FAIL {name}: mutation anchor count={source.count(old)}')
            ok = False
            continue
        with tempfile.TemporaryDirectory(prefix='mcp-cacheable-mutation-') as td:
            td = Path(td)
            mutated = td / 'gateway.go'
            mutated.write_text(source.replace(old, new, 1))
            overlay = td / 'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(SOURCE): str(mutated)}}))
            cmd = ['go', 'test', '-overlay', str(overlay), './internal/mcpgw', '-run', '^TestDiscoveryWithoutInitializeAndNoExtraCapabilities$', '-count=1', '-timeout=2m']
            run = subprocess.run(cmd, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=150)
            passed = run.returncode == 1 and marker in run.stdout and '--- FAIL:' in run.stdout and '[build failed]' not in run.stdout and 'setup failed' not in run.stdout
            print(('PASS ' if passed else 'FAIL ') + f'{name} rc={run.returncode} witness={marker}')
            if not passed:
                print(run.stdout[-5000:])
                ok = False
    raise SystemExit(0 if ok else 1)

if __name__ == '__main__': main()
