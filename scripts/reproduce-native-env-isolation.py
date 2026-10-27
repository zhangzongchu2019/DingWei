#!/usr/bin/env python3
"""Exercise configuration I/O with an external sentinel and a native-prefix negative cut."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
NATIVE_PREFIXES = ('CODEX_', 'CLAUDE_', 'ANTHROPIC_', 'OPENAI_', 'XDG_')
NATIVE_PATHS = ('CODEX_HOME', 'CLAUDE_CONFIG_DIR', 'XDG_CONFIG_HOME',
                'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_RUNTIME_DIR')


def snapshot(root):
    return {str(p.relative_to(root)): (stat.S_IFMT(p.lstat().st_mode), p.lstat().st_mtime_ns)
            for p in [root, *sorted(root.rglob('*'))]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--driver', choices=('claude', 'codex'), default='codex')
    parser.add_argument('--reverse', action='store_true')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='native-sentinel-') as tmp:
        sentinel = Path(tmp) / 'protected'
        sentinel.mkdir()
        (sentinel / 'settings.json').write_text('{"sentinel":"must remain untouched"}')
        before = snapshot(sentinel)
        # Never put actual provider credentials in a negative-cut environment.
        env = {k: v for k, v in os.environ.items() if not k.startswith(NATIVE_PREFIXES)}
        env.update({k: str(sentinel) for k in NATIVE_PATHS})
        env.update(CLAUDE_FOO='sentinel-claude', CODEX_BAR='sentinel-codex',
                   ANTHROPIC_SYNTHETIC='sentinel-provider', OPENAI_SYNTHETIC='sentinel-provider',
                   XDG_SYNTHETIC='sentinel-xdg', TZ='UTC')
        arguments = [str(ROOT / 'scripts/test-helper-suite.py'), '--driver', args.driver,
                     '--pattern', 'test_suite_isolation.py']
        command = [sys.executable, *arguments]
        if args.reverse:
            source = (ROOT / 'tools/sessionhelper/tests/_suite_isolation.py').read_text()
            for prefix in NATIVE_PREFIXES:
                replacement = source.replace(", '" + prefix + "'", '')
                assert replacement != source
                source = replacement
            (Path(tmp) / '_suite_isolation.py').write_text(source)
            # Preload the isolated negative module, then invoke the real runner.
            # HOME_KEYS rebasing remains; only arbitrary prefix stripping is cut.
            wrapper = ("import sys,runpy; sys.path.insert(0," + repr(tmp) + ");"
                       "import _suite_isolation; sys.argv=" + repr(arguments) + ";"
                       "runpy.run_path(" + repr(arguments[0]) + ",run_name='__main__')")
            command = [sys.executable, '-c', wrapper]
        result = subprocess.run(command, cwd=ROOT, env=env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        after = snapshot(sentinel)
        print(result.stdout, end='', flush=True)
        unchanged = before == after
        criterion = ('inherited native variable: CLAUDE_FOO' in result.stdout)
        valid = unchanged and (result.returncode == 1 and criterion if args.reverse else result.returncode == 0)
        print(json.dumps({'driver': args.driver, 'reverse': args.reverse, 'child_rc': result.returncode,
                          'sentinel_unchanged': unchanged, 'sentinel_before': before,
                          'sentinel_after': after, 'sentinel_metadata_sha256': hashlib.sha256(
                              json.dumps(before, sort_keys=True).encode()).hexdigest(),
                          'negative_assertion_seen': criterion, 'expected_result': valid}), flush=True)
        return (1 if args.reverse else 0) if valid else 2


if __name__ == '__main__':
    raise SystemExit(main())
