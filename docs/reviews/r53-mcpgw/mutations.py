#!/usr/bin/env python3
"""在 Go overlay 中逐刀破坏保护；原源码不变，必须由指定语义断言杀死。"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--out', default='/tmp/r53-mcpgw-mutations')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    cases = [
        ('pkce', 'oauth.go',
         'subtle.ConstantTimeCompare([]byte(a.Challenge), []byte(challenge)) != 1',
         'subtle.ConstantTimeCompare([]byte(a.Challenge), []byte(challenge)) < 0',
         '^TestOAuthPKCEAndBindings/pkce$', 'code_verifier not bound'),
        ('code-replay', 'oauth.go',
         'g.cfg.Store.Take(r.Context(), "code", hash(f.Get("code")), g.cfg.Now())',
         'g.cfg.Store.Get(r.Context(), "code", hash(f.Get("code")), g.cfg.Now())',
         '^TestOAuthCodeReplay$', 'authorization code reusable'),
        ('rest-extra', 'gateway.go',
         'name := strings.TrimPrefix(r.URL.Path, "/api/external/")',
         'name := strings.TrimPrefix(r.URL.Path, "/api/external/")\n if name == "execute" { writeJSON(w,200,map[string]any{"ok":true});return }',
         '^TestDiscoveryWithoutInitializeAndNoExtraCapabilities$', 'extra capability execute allowed'),
        ('origin', 'gateway.go',
         'if len(origins) > 1 || (len(origins) == 1 && (origins[0] == "" || !g.trusted(r.Context(), origins[0]))) {',
         'if false && (len(origins) > 1 || (len(origins) == 1 && (origins[0] == "" || !g.trusted(r.Context(), origins[0])))) {',
         '^TestOAuthMetadataAndOrigin$', 'Origin guard absent'),
        ('headers', 'gateway.go',
         'if version == "" || singleHeader(r, "MCP-Protocol-Version") != version || singleHeader(r, "Mcp-Method") != q.Method || (q.Method == "tools/call" && (q.Params.Name == "" || nameHeader(singleHeader(r, "Mcp-Name")) != q.Params.Name)) {',
         'if false && (version == "" || singleHeader(r, "MCP-Protocol-Version") != version || singleHeader(r, "Mcp-Method") != q.Method || (q.Method == "tools/call" && (q.Params.Name == "" || nameHeader(singleHeader(r, "Mcp-Name")) != q.Params.Name))) {',
         '^TestRequestMetadataAndOriginGuards/method_mismatch$', 'guard missing'),
        ('version', 'gateway.go',
         'if version != ProtocolVersion {', 'if false && version != ProtocolVersion {',
         '^TestRequestMetadataAndOriginGuards$', 'bad metadata response'),
        ('wait-max', 'tools.go',
         '*a.Seconds > extshared.WaitMax.Seconds()', '*a.Seconds < -1',
         '^TestArgumentsAndCrossAG$', '200'),
        ('authentication', 'gateway.go',
         'return g.cfg.Seats.Authenticate(r.Context(), p[1])', 'return extseat.Seat{}, nil',
         '^TestToolBoundaryMatrix/list_online/mcp/revoked$', 'status 200'),
        ('application-code', 'gateway.go',
         '{extshared.ErrQueueFull, 429, 1029, "queue_full"}',
         '{extshared.ErrQueueFull, 429, -32029, "queue_full"}',
         '^TestApplicationErrorsAvoidMCPReservedRange$', 'application error uses reserved MCP code'),
        ('code-expiry', 'store.go',
         'if now.UnixNano() >= expiry || len(sealed) < s.aead.NonceSize() {',
         'if false && now.UnixNano() >= expiry || len(sealed) < s.aead.NonceSize() {',
         '^TestOAuthExpiryAndRevocationBeforeExchange/code_expired$', 'expired/revoked grant accepted'),
    ]
    env = {k: v for k, v in os.environ.items() if not k.startswith(('SH_', 'DINGWEI_'))}
    env.update(TMPDIR='/tmp', TZ='UTC')
    results = []
    for name, filename, old, new, test, marker in cases:
        source = root / 'internal/mcpgw' / filename
        raw = source.read_text()
        if raw.count(old) != 1:
            raise SystemExit(f'{name}: mutation target count is {raw.count(old)}')
        mutated = out / f'{name}.go'
        mutated.write_text(raw.replace(old, new, 1))
        overlay = out / f'{name}.overlay.json'
        overlay.write_text(json.dumps({'Replace': {str(source): str(mutated)}}))
        cmd = ['go', 'test', '-json', '-count=1', '-overlay', str(overlay), './internal/mcpgw', '-run', test]
        start = datetime.datetime.now(datetime.timezone.utc).isoformat()
        process = subprocess.run(cmd, cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = out / f'{name}.log'
        log.write_text(process.stdout)
        killed = process.returncode != 0 and '"Action":"fail"' in process.stdout and marker in process.stdout and '[build failed]' not in process.stdout
        result = {'name': name, 'command': cmd, 'rc': process.returncode, 'semantic_kill': killed,
                  'started_utc': start, 'ended_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  'source_sha256': hashlib.sha256(raw.encode()).hexdigest(), 'log': str(log),
                  'log_sha256': hashlib.sha256(process.stdout.encode()).hexdigest(), 'expected_marker': marker}
        results.append(result)
        print(json.dumps(result), flush=True)
    (out / 'RESULTS.json').write_text(json.dumps(results, indent=2) + '\n')
    return 0 if all(r['semantic_kill'] for r in results) else 1


if __name__ == '__main__':
    raise SystemExit(main())
