"""Run both helper discovery forms in separate outer synthetic homes."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
OUTPUT = Path(sys.argv[1]).resolve()
OUTPUT.mkdir(parents=True, exist_ok=True)
# Reuse only build caches; never inherit the running seat's helper configuration.
cache = json.loads(subprocess.check_output(['go', 'env', '-json', 'GOCACHE', 'GOMODCACHE']))
results = []
for name, cwd, discovery in (
        ('helper-root', ROOT, 'tools/sessionhelper/tests'),
        ('helper-cwd', ROOT / 'tools/sessionhelper', 'tests')):
    with tempfile.TemporaryDirectory(prefix='dw-helper-gate-') as tmp:
        child_env = {key: value for key, value in os.environ.items()
                     if not key.startswith(('SH_', 'DINGWEI_', 'DATAD_'))}
        child_env.update(HOME=tmp, SH_STATE_DIR=str(Path(tmp) / 'state'),
                         SH_PROVISION_NO_EXIT='1', PYTHONPATH=str(ROOT / 'tools/sessionhelper'),
                         PYTHONDONTWRITEBYTECODE='1', **cache)
        command = [sys.executable, '-B', '-m', 'unittest', 'discover', '-s', discovery, '-v']
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        with (OUTPUT / (name + '.log')).open('w') as log:
            result = subprocess.run(command, cwd=cwd, env=child_env,
                                    stdout=log, stderr=subprocess.STDOUT, timeout=1500)
        results.append({'name': name, 'cwd': str(cwd), 'command': command,
                        'started_utc': started,
                        'finished_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                        'exit': result.returncode, 'outer_home': 'disposable synthetic directory',
                        'helper_configuration': 'removed inherited SH_, DINGWEI_, DATAD_ variables'})
        (OUTPUT / 'helper-gates.json').write_text(json.dumps(results, indent=2) + '\n')
        print(json.dumps(results[-1]), flush=True)
        if result.returncode:
            raise SystemExit(result.returncode)
