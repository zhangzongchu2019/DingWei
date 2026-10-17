"""Local synthetic withdrawal controls; no helper/host/PG deployment."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
mutants = [
    ('listener-withdrawal', 'ipc_listener.py', '    if send_body_only:\n        return await _handle_body_send(writer, msg, send_handler, passed_fds or [])\n', ''),
    ('shim-file-withdrawal', 'mcp_shim.py', '"file" in args or any(', 'False or any('),
    ('wiring-withdrawal', 'app.py', 'send_body_only=getattr(self.cfg, "ipc_send_body_only", False)', 'send_body_only=False'),
]
results = []
for name, filename, before, after in mutants:
    with tempfile.TemporaryDirectory(prefix='coder-h1-withdraw-') as scratch:
        scratch = Path(scratch)
        shutil.copytree(ROOT/'tools/sessionhelper/sessionhelper', scratch/'sessionhelper', ignore=shutil.ignore_patterns('__pycache__'))
        (scratch/'tests').mkdir(); (scratch/'home').mkdir()
        for test in ('test_hosted_mcp_send.py', 'test_file_upload.py', '_home_isolation.py'):
            shutil.copyfile(ROOT/'tools/sessionhelper/tests'/test, scratch/'tests'/test)
        source = scratch/'sessionhelper'/filename
        data = source.read_text(); assert data.count(before) == 1
        baseline = hashlib.sha256(source.read_bytes()).hexdigest()
        source.write_text(data.replace(before, after))
        env = dict(os.environ, HOME=str(scratch/'home'), PYTHONPATH=str(scratch)+':'+str(scratch/'tests'), PYTHONDONTWRITEBYTECODE='1')
        with (OUT/'raw'/(name+'.log')).open('w') as log:
            rc = subprocess.run([sys.executable, '-m', 'unittest', '-q', 'test_hosted_mcp_send'], cwd=scratch, env=env, stdout=log, stderr=subprocess.STDOUT).returncode
        results.append(dict(name=name, file=filename, rc=rc, baseline_sha256=baseline, mutant_sha256=hashlib.sha256(source.read_bytes()).hexdigest(), before=before, after=after))
(OUT/'WITHDRAWAL.json').write_text(json.dumps(results, indent=2)+'\n')
assert all(item['rc'] == 1 for item in results), results
print('three source withdrawals rc1; temporary copies removed')
