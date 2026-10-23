import contextlib
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import sessionhelper
from sessionhelper import boot_origin as origin


class BootOriginTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.stderr = io.StringIO()
        self.addCleanup(patch.stopall)
        patch.object(origin, '_ATTEMPTED_PID', None).start()
        patch('sys.stderr', self.stderr).start()

    def record(self):
        origin.record_boot_origin(str(self.root))
        return json.loads((self.root / origin.RECORD_NAME).read_text())

    def test_actual_pid_starttime_and_resolved_package_origin(self):
        data = self.record()
        raw = Path('/proc/self/stat').read_text()
        self.assertEqual(data['pid'], os.getpid(), 'BOOT_PID_MISMATCH')
        self.assertEqual(data['starttime'], int(raw.rpartition(')')[2].split()[19]), 'BOOT_STARTTIME_MISMATCH')
        self.assertEqual(data['sessionhelper_file'], str(Path(sessionhelper.__file__).resolve()), 'BOOT_SOURCE_MISMATCH')
        self.assertEqual(set(data), {'type', 'pid', 'starttime', 'sessionhelper_file', 'isolated', 'boot_marker'})
        self.assertEqual(data['type'], 'helper_boot_origin')
        self.assertRegex(data['boot_marker'], r'^[0-9a-f]{32}$', 'BOOT_MARKER_MISSING')
        self.assertEqual(self.stderr.getvalue(), (self.root / origin.RECORD_NAME).read_text(), 'BOOT_STDERR_MISMATCH')
        self.assertEqual(stat.S_IMODE((self.root / origin.RECORD_NAME).stat().st_mode), 0o600, 'BOOT_MODE_NOT_PRIVATE')

    def test_isolation_flag_matches_real_interpreters(self):
        results = []
        for isolated in (False, True):
            state = self.root / str(isolated)
            script = ('import sys; sys.path.insert(0, ' + repr(str(Path(sessionhelper.__file__).resolve().parents[1])) + '); '
                      'from sessionhelper.boot_origin import record_boot_origin; import os; '
                      'record_boot_origin(' + repr(str(state)) + '); print(os.getpid())')
            process = subprocess.run([sys.executable, *(['-I'] if isolated else []), '-B', '-c', script],
                                     capture_output=True, text=True, timeout=15, check=True)
            data = json.loads((state / origin.RECORD_NAME).read_text())
            self.assertEqual(data['pid'], int(process.stdout), 'BOOT_SUBPROCESS_PID_MISMATCH')
            self.assertEqual(data['isolated'], int(isolated), 'BOOT_ISOLATION_MISMATCH')
            self.assertEqual(json.loads(process.stderr), data)
            results.append(data['boot_marker'])
        self.assertNotEqual(*results, 'BOOT_MARKER_REUSED')

    def test_source_symlink_resolves(self):
        linked = self.root / 'package-link.py'
        linked.symlink_to(Path(sessionhelper.__file__).resolve())
        with patch.object(sessionhelper, '__file__', str(linked)):
            self.assertEqual(self.record()['sessionhelper_file'], str(linked.resolve()), 'BOOT_SOURCE_NOT_RESOLVED')

    def test_once_per_process_not_per_call(self):
        data = self.record()
        with patch.object(origin, '_replace', side_effect=AssertionError('second write')):
            origin.record_boot_origin(str(self.root))
        self.assertEqual(len(self.stderr.getvalue().splitlines()), 1, 'BOOT_REPEATED')
        self.assertEqual(json.loads((self.root / origin.RECORD_NAME).read_text()), data)

    def test_failure_warns_sanitized_and_remains_nonblocking(self):
        with patch.object(origin, '_replace', side_effect=OSError('SECRET_FIXTURE /private/secret')):
            origin.record_boot_origin(str(self.root))
        text = self.stderr.getvalue()
        self.assertIn('HELPER_BOOT_ORIGIN_WARNING record_write_failed', text, 'BOOT_FAILURE_SILENT')
        self.assertNotIn('SECRET_FIXTURE', text, 'BOOT_FAILURE_LEAKED')
        self.assertNotIn('/private/secret', text)
        self.assertEqual(json.loads(text.splitlines()[-1])['pid'], os.getpid())

    def test_collection_failure_is_nonblocking_sanitized(self):
        with patch.object(origin, '_record', side_effect=ValueError('SECRET_FIXTURE')):
            origin.record_boot_origin(str(self.root))
        self.assertEqual(self.stderr.getvalue(), 'HELPER_BOOT_ORIGIN_WARNING origin_unavailable\n')
        self.assertFalse((self.root / origin.RECORD_NAME).exists())

    def test_broken_stderr_does_not_block(self):
        broken = unittest.mock.Mock()
        broken.write.side_effect = OSError('broken stderr')
        with patch('sys.stderr', broken):
            self.record()

    def test_replace_failure_preserves_old_and_cleans_temporary(self):
        target = self.root / origin.RECORD_NAME
        target.write_text('previous')
        with patch.object(origin.os, 'replace', side_effect=OSError('failure')):
            origin.record_boot_origin(str(self.root))
        self.assertTrue(target.exists(), 'BOOT_NOT_ATOMIC')
        self.assertEqual(target.read_text(), 'previous', 'BOOT_NOT_ATOMIC')
        self.assertEqual(sorted(p.name for p in self.root.iterdir()), [origin.RECORD_NAME], 'BOOT_TEMP_LEAK')
        self.assertIn('record_write_failed', self.stderr.getvalue())

    def test_target_symlink_does_not_modify_referent(self):
        outside = self.root / 'outside'
        outside.write_text('untouched')
        target = self.root / origin.RECORD_NAME
        target.symlink_to(outside)
        self.record()
        self.assertEqual(outside.read_text(), 'untouched', 'BOOT_SYMLINK_FOLLOWED')
        self.assertFalse(target.is_symlink())

    def test_unprotected_or_symlink_state_directory_refused(self):
        for kind in ('writable', 'symlink'):
            with self.subTest(kind=kind):
                origin._ATTEMPTED_PID = None
                directory = self.root / kind
                directory.mkdir(mode=0o700)
                if kind == 'writable':
                    directory.chmod(0o777)
                    state = directory
                else:
                    state = self.root / 'alias'
                    state.symlink_to(directory, target_is_directory=True)
                origin.record_boot_origin(str(state))
                self.assertFalse((directory / origin.RECORD_NAME).exists(), 'BOOT_UNSAFE_DIRECTORY_WRITTEN')
        self.assertEqual(self.stderr.getvalue().count('record_write_failed'), 2)

    def test_umask_cannot_make_record_public(self):
        previous = os.umask(0)
        try:
            self.record()
        finally:
            os.umask(previous)
        self.assertEqual(stat.S_IMODE((self.root / origin.RECORD_NAME).stat().st_mode), 0o600, 'BOOT_MODE_NOT_PRIVATE')

    def test_self_stat_invalid_never_fabricates_starttime(self):
        for text in ('1 (x) broken', str(os.getpid()+1) + ' (x) ' + ' '.join(['1']*30)):
            with patch.object(origin.Path, 'read_text', return_value=text), self.assertRaises((ValueError, IndexError)):
                origin._starttime(os.getpid())

    def test_application_wiring_and_write_failure_do_not_block_run(self):
        from sessionhelper import app
        events = []
        cfg = SimpleNamespace(state_dir=str(self.root))
        class FakeHelper:
            def __init__(self, config):
                events.append('construct')
            async def run_forever(self):
                events.append('run')
        with patch.object(app.enroll, 'enabled_for_env', return_value=False), \
             patch.object(app, 'load_config', return_value=cfg), patch.object(app, 'SessionHelper', FakeHelper), \
             patch.object(origin, '_replace', side_effect=OSError('SECRET_FIXTURE')):
            app.main()
        self.assertEqual(events, ['construct', 'run'], 'BOOT_WRITE_BLOCKED_STARTUP')
        self.assertIn('record_write_failed', self.stderr.getvalue(), 'BOOT_APP_UNWIRED')
        self.assertIn('helper_boot_origin', self.stderr.getvalue(), 'BOOT_APP_UNWIRED')

    def test_application_records_before_constructor(self):
        from sessionhelper import app
        cfg = SimpleNamespace(state_dir=str(self.root))
        events = []
        root = self.root
        case = self
        class FakeHelper:
            def __init__(self, config):
                case.assertTrue((root / origin.RECORD_NAME).exists(), 'BOOT_APP_ORDER_WRONG')
                case.assertEqual(json.loads((root / origin.RECORD_NAME).read_text())['pid'], os.getpid())
                events.append('construct')
            async def run_forever(self):
                events.append('run')
        with patch.object(app.enroll, 'enabled_for_env', return_value=False), \
             patch.object(app, 'load_config', return_value=cfg), patch.object(app, 'SessionHelper', FakeHelper):
            app.main()
        self.assertEqual(events, ['construct', 'run'])

    def test_version_probe_does_not_write(self):
        from sessionhelper import launch
        with patch.object(sys, 'argv', ['sessionhelper', '--version']), contextlib.redirect_stdout(io.StringIO()), \
             patch.object(origin, 'record_boot_origin', side_effect=AssertionError('not a runtime')):
            launch.main()
        self.assertEqual(self.stderr.getvalue(), '')
        self.assertEqual(list(self.root.iterdir()), [])


if __name__ == '__main__':
    unittest.main()
