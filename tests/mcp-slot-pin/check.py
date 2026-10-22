"""Run exact assertion-red withdrawals in a detached, private worktree.

Use an existing locked Python venv; no installs or network. Output must be new.
Keeps every original stdout/stderr/rc and restores bytes even after a failure.
"""
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
PRODUCT = 'tools/sessionhelper/legacy_mcp_entry.py'
SUITE = 'tools.sessionhelper.tests.test_legacy_mcp_entry.LegacyEntryTests'
MUTATIONS = [
    ('pending-not-current', [("data.get('current')", "data.get('pending')")],
     'test_confirmed_only_and_readonly_without_guard_or_locks', 'CURRENT_NOT_SELECTED'),
    ('previous-not-current', [("data.get('current')", "data.get('previous')")],
     'test_confirmed_only_and_readonly_without_guard_or_locks', 'CURRENT_NOT_SELECTED'),
    ('journal-format', [("type(data.get('format')) is not int or data['format'] != 1", "False")],
     'test_invalid_journal_shapes_fall_back', 'INVALID_JOURNAL_ACCEPTED'),
    ('sha-shape', [("not re.fullmatch(r'[0-9a-f]{64}', sha)", 'False')],
     'test_invalid_journal_shapes_fall_back', 'INVALID_SHA_ACCEPTED'),
    ('journal-nofollow', [(' | os.O_NOFOLLOW', '')],
     'test_path_and_permission_guards', 'JOURNAL_SYMLINK_ACCEPTED'),
    ('journal-nonblock', [(' | os.O_NONBLOCK', '')],
     'test_special_journals_nonblocking', 'JOURNAL_NONBLOCK_TIMEOUT'),
    ('journal-size', [('info.st_size > MAX_JOURNAL_BYTES', 'False'),
                      ('len(raw) > MAX_JOURNAL_BYTES', 'False'),
                      ('stream.read(MAX_JOURNAL_BYTES + 1)', 'stream.read()')],
     'test_invalid_journal_shapes_fall_back', 'INVALID_JOURNAL_ACCEPTED'),
    ('private-mode', [('0o077 if private else 0o022', '0o022')],
     'test_path_and_permission_guards', 'UNSAFE_MODE_ACCEPTED'),
    ('directory-owner', [('info.st_uid in owners', 'True')],
     'test_path_and_permission_guards', 'FOREIGN_DIRECTORY_OWNER_ACCEPTED'),
    ('canonical-parent', [('path.resolve() == path', 'True')],
     'test_path_and_permission_guards', 'PARENT_ALIAS_ACCEPTED'),
    ('payload-regular', [('not stat.S_ISREG(info.st_mode)\n                or', 'False\n                or')],
     'test_slot_symlinks_incomplete_and_special_files_fall_back', 'PAYLOAD_SYMLINK_ACCEPTED'),
    ('complete-package', [("REQUIRED_FILES = ('__init__.py', '__main__.py', 'mcp_shim.py')", "REQUIRED_FILES = ('__init__.py', '__main__.py')")],
     'test_no_valid_factory_visible_failure', 'INVALID_FACTORY_EXECUTED'),
    ('factory-validation', [('if valid_package(factory, factory=True):', 'if True:')],
     'test_no_valid_factory_visible_failure', 'INVALID_FACTORY_EXECUTED'),
    ('import-precedence', [("import runpy,sys;sys.path.insert(0,sys.argv.pop(1));", "import runpy,sys;sys.argv.pop(1);")],
     'test_real_stdio_current_factory_versions_and_ipc_failure', 'CWD_OR_PYTHONPATH_SHADOWED_SLOT'),
    ('ipc-environment', [('dict(os.environ, PYTHONPATH=str(selected), PYTHONDONTWRITEBYTECODE=\'1\')',
                           "dict(PYTHONPATH=str(selected), PYTHONDONTWRITEBYTECODE='1')")],
     'test_actual_shim_uses_only_supplied_fake_ipc', 'FAKE_IPC_FAILED'),
    ('exec-error-status', [("sys.stderr.write('mcp_slot_pin: exec_failed\\n')\n        return 78", "sys.stderr.write('mcp_slot_pin: exec_failed\\n')\n        return 0")],
     'test_exec_failure_is_visible_without_retries', 'EXEC_FAILURE_HIDDEN'),
]


def main(output):
    out = Path(output).resolve(); out.mkdir(mode=0o700, parents=True, exist_ok=False)
    source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    wt = out / 'worktree'
    subprocess.run(['git', 'worktree', 'add', '--detach', str(wt), source], cwd=ROOT,
                   check=True, capture_output=True)
    home = out / 'home'; home.mkdir(mode=0o700)
    # Keep AF_UNIX fixture paths below the platform socket path limit.
    import tempfile
    with tempfile.TemporaryDirectory(prefix='mcp-mut-') as tmp:
        env = {'HOME': str(home), 'PATH': str(Path(sys.executable).parent) + ':/usr/bin:/bin',
               'TMPDIR': tmp, 'PYTHONDONTWRITEBYTECODE': '1'}
        rows = []
        def run(name, test='', marker=''):
            cmd = [sys.executable, '-B', '-m', 'unittest', '-v', SUITE + ('.' + test if test else '')]
            start = datetime.datetime.now(datetime.timezone.utc).isoformat()
            proc = subprocess.run(cmd, cwd=wt, env=env, capture_output=True, text=True, timeout=45)
            for suffix, data in [('stdout', proc.stdout), ('stderr', proc.stderr), ('rc', str(proc.returncode) + '\n')]:
                (out / (name + '.' + suffix)).write_text(data)
            red = (proc.returncode == 1 and marker in proc.stderr
                   and 'AssertionError' in proc.stderr and 'FAIL:' in proc.stderr
                   and not re.search(r'^ERROR:', proc.stderr, re.M)) if marker else False
            row = dict(name=name, command=cmd, start=start,
                       end=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                       rc=proc.returncode, marker=marker, assertion_red=red)
            rows.append(row)
            (out / 'CHECKS.json').write_text(json.dumps(dict(source=source, rows=rows), indent=2) + '\n')
            assert red if marker else proc.returncode == 0, row
        target = wt / PRODUCT; original = target.read_text()
        run('baseline')
        for name, replacements, test, marker in MUTATIONS:
            modified = original
            for before, after in replacements:
                assert modified.count(before) == 1, (name, before)
                modified = modified.replace(before, after, 1)
            try:
                target.write_text(modified)
                run(name + '-red', test, marker)
            finally:
                target.write_text(original)
            run(name + '-restored', test)
        run('final-restored')
        assert target.read_text() == original
        (out / 'RESULT.json').write_text(json.dumps(dict(source=source, mutations=len(MUTATIONS),
                                      commands=len(rows), result='LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE'), indent=2) + '\n')
        print('LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE mutations=' + str(len(MUTATIONS)) + ' commands=' + str(len(rows)))


if __name__ == '__main__':
    main(sys.argv[1])
