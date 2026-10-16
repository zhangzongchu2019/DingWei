"""Run fixed/withdrawn controls in disposable copies; never edit the checkout."""
import datetime
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
OUTPUT = Path(sys.argv[1]).resolve()
OUTPUT.mkdir(parents=True, exist_ok=True)
PACKAGE = ROOT / 'tools/sessionhelper/sessionhelper'
TARGETS = {'sqlite': 'sqlite_utils.py', 'shim': 'mcp_shim.py'}
original = {name: hashlib.sha256((PACKAGE / path).read_bytes()).hexdigest()
            for name, path in TARGETS.items()}
results = []
for name, filename in TARGETS.items():
    with tempfile.TemporaryDirectory(prefix='dw-withdraw-') as tmp:
        helper = Path(tmp) / 'tools/sessionhelper'
        shutil.copytree(PACKAGE, helper / 'sessionhelper',
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        tests = helper / 'tests'
        tests.mkdir()
        test = 'test_sqlite_connection_lifetime.py' if name == 'sqlite' else 'test_mcp_shim_transport_errors.py'
        shutil.copyfile(ROOT / 'tools/sessionhelper/tests' / test, tests / test)
        for variant in ('fixed', 'withdrawn'):
            if variant == 'withdrawn':
                path = helper / 'sessionhelper' / filename
                source = path.read_text()
                if name == 'sqlite':
                    needle = '        db.close()\n'
                    assert source.count(needle) == 1
                    source = source.replace(needle, '        pass  # withdraw deterministic close\n')
                else:
                    for needle in (
                            '        except _IPCFailure as exc:\n            return _tool_failure(req.id, exc.category)\n',
                            '        except Exception:\n            return _tool_failure(req.id, "tool_failed")\n'):
                        assert source.count(needle) == 1
                        source = source.replace(needle, '')
                path.write_text(source)
            process = subprocess.run([sys.executable, '-B', '-m', 'unittest', 'discover',
                                      '-s', 'tests', '-p', test, '-v'], cwd=helper,
                                     capture_output=True, text=True, timeout=240)
            content = process.stdout + process.stderr
            log = OUTPUT / (name + '-' + variant + '.log')
            log.write_text(content)
            expected = 0 if variant == 'fixed' else 1
            assert process.returncode == expected, (name, variant, process.returncode, str(log))
            if variant == 'withdrawn':
                assert 'FAILED (' in content and 'AssertionError' in content
                assert 'ModuleNotFoundError' not in content and 'ImportError' not in content
            results.append({'repair': name, 'variant': variant, 'exit': process.returncode,
                            'tests': re.findall(r'Ran (\d+) tests?', content),
                            'log': log.name, 'sha256': hashlib.sha256(log.read_bytes()).hexdigest()})
assert original == {name: hashlib.sha256((PACKAGE / path).read_bytes()).hexdigest()
                    for name, path in TARGETS.items()}
summary = {'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
           'fixed_source_sha256': original, 'checkout_unchanged': True, 'results': results}
(OUTPUT / 'withdrawal.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary, indent=2))
