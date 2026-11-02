#!/usr/bin/env python3
"""只在独立 overlay 撤 WS already_closed 分类，要求真实子进程退1。"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
root = Path(__file__).resolve().parents[2]
out = a.output.resolve()
out.mkdir(parents=True, exist_ok=True)
source = root / 'internal/m8/lifecycle.go'
text = source.read_text()
anchor = 'if websocketAlreadyClosed(err) {'
assert text.count(anchor) == 1
with tempfile.TemporaryDirectory(prefix='hlc-ws-mutation-') as directory:
    temp = Path(directory)
    (temp / 'home').mkdir()
    (temp / 'tmp').mkdir()
    replacement = temp / 'lifecycle.go'
    replacement.write_text(text.replace(anchor, 'if false { // withdrawn: already_closed classification'))
    overlay = temp / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(source): str(replacement)}}))
    env = dict(HOME=str(temp / 'home'), TMPDIR=str(temp / 'tmp'),
               PATH='/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin',
               GOCACHE='/home/dev/.cache/go-build', GOMODCACHE='/home/dev/go/pkg/mod', GOPATH='/home/dev/go',
               TZ='UTC', LANG='C.UTF-8', HLC_WS_EVIDENCE=str(out))
    cmd = ['go', 'test', '-race', '-overlay', str(overlay), './internal/m8',
           '-run', '^TestHubWSCloseProcesses$/^peer-first$', '-count=3', '-v', '-timeout=2m']
    start = datetime.datetime.now(datetime.timezone.utc).isoformat()
    result = subprocess.run(cmd, cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    log = result.stdout.decode(errors='replace')
    (out / 'withdrawn.log').write_text(log)
    valid = result.returncode != 0 and log.count('S014_EXIT: got 1 want 0') == 3
    receipt = dict(started_utc=start, ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                   command=cmd, rc=result.returncode, target_red=valid,
                   expected_exit1_occurrences=log.count('S014_EXIT: got 1 want 0'),
                   source_sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                   raw_log_sha256=hashlib.sha256(result.stdout).hexdigest(),
                   resources=dict(home=str(temp / 'home'), tmp=str(temp / 'tmp'),
                                  database='none; m8 socket lifecycle only', ports='loopback kernel-assigned; raw log'))
    (out / 'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
    print(json.dumps(receipt))
    raise SystemExit(0 if valid else 1)
