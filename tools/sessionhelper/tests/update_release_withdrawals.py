"""Run two surgical core withdrawals in disposable copies, never alter checked-out source."""
from pathlib import Path
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-dir', required=True)
    args = parser.parse_args()
    evidence = Path(args.evidence_dir); evidence.mkdir(parents=True, exist_ok=True)
    source = Path(__file__).resolve().parents[1]
    variants = [
        ('withdraw-pending-priority', 'update_launcher/guard.py', 'if trial and pending:',
         "if trial and pending and not data.get('current'):",
         'test_update_release_core.GuardTests.test_consecutive_factory_a_b_c_and_restart_each_trial'),
        ('withdraw-secret-scan', 'package_release.py', 'if FORBIDDEN.search(name) or SECRET.search(content):',
         'if False:', 'test_update_release_core.PackageTests.test_literal_secret_content_rejected'),
    ]
    results = {}
    for label, file, before, after, test in variants:
        with tempfile.TemporaryDirectory(prefix='dw-update-withdraw-') as td:
            root = Path(td)
            for name in ('package_release.py', 'bootstrap.py', 'requirements-runtime.lock',
                         'update_launcher/guard.py', 'update_launcher/__init__.py',
                         'update_launcher/launcher_main.py', 'update_launcher/mcp_forwarder.py',
                         'tests/test_update_release_core.py'):
                dest = root / name; dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source / name, dest)
            target = root / file
            text = target.read_text()
            if text.count(before) != 1:
                raise SystemExit('withdrawal anchor changed')
            target.write_text(text.replace(before, after, 1))
            env = dict(os.environ, PYTHONPATH=str(root) + ':' + str(root / 'tests'), PYTHONDONTWRITEBYTECODE='1')
            with (evidence / (label + '.log')).open('w') as out:
                result = subprocess.run([sys.executable, '-B', '-m', 'unittest', '-v', test],
                                        cwd=root, env=env, stdout=out, stderr=subprocess.STDOUT)
            results[label] = result.returncode
            if result.returncode != 1:
                raise SystemExit('withdrawal did not fail as intended')
    (evidence / 'withdrawals.json').write_text(json.dumps(results, sort_keys=True, indent=2) + '\n')
    print(json.dumps(results, sort_keys=True))


if __name__ == '__main__':
    main()
