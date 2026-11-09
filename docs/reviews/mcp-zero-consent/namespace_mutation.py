"""撤掉state域的写入和读取前缀，两格GET负例必须各自目标红。"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[3]
parser = argparse.ArgumentParser()
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=False)
source = ROOT / 'internal/mcpgw/oauth.go'
text = source.read_text()
for old in ('hash("state:"+location.Query().Get("state"))', 'key = "state:" + req.State'):
    assert text.count(old) == 1
mutant = text.replace('hash("state:"+location.Query().Get("state"))', 'hash(location.Query().Get("state"))').replace('key = "state:" + req.State', 'key = req.State')
with tempfile.TemporaryDirectory(prefix='zc-namespace-') as directory:
    temporary = Path(directory)
    (temporary / 'oauth.go').write_text(mutant)
    overlay = temporary / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(source): str(temporary / 'oauth.go')}}))
    env = {'PATH':'/usr/bin:/bin', 'HOME':directory, 'TMPDIR':directory, 'GOCACHE':'/home/dev/.cache/go-build', 'GOMODCACHE':'/home/dev/go/pkg/mod', 'GOPROXY':'off'}
    records = []
    for name, target in [('started-as-state','ZC_STARTED_AS_STATE_RESTART'), ('selection-as-state','ZC_SELECTION_AS_STATE_CONSUMED')]:
        base = ['go','test','-count=1','-v','./internal/mcpgw','-run','^TestConsentLoginNamespaceIsolation$/^'+name+'$']
        for mode in ('fixed','mutant'):
            command = base if mode == 'fixed' else base[:2] + ['-vet=off','-overlay='+str(overlay)] + base[2:]
            result = subprocess.run(command, cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            (args.output / (name+'-'+mode+'.stdout')).write_bytes(result.stdout)
            txt = result.stdout.decode()
            valid = result.returncode == 0 if mode == 'fixed' else result.returncode != 0 and target in txt and '[build failed]' not in txt
            record = {'case': name, 'mode': mode, 'command': command, 'rc': result.returncode, 'target': target, 'expected_result':valid}
            records.append(record)
            print(json.dumps(record),flush=True)
    (args.output / 'RESULT.json').write_text(json.dumps(records,indent=2)+'\n')
    assert all(r['expected_result'] for r in records)
