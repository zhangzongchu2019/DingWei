"""Synthetic Docker responses only; never contacts a host or live Agent."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / 'helper-bootstrap-inventory.py'
spec = importlib.util.spec_from_file_location('inventory', SCRIPT)
inventory = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inventory)


class InventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.mount = self.root / 'source'
        (self.mount / 'sessionhelper').mkdir(parents=True)
        (self.mount / 'sessionhelper/app.py').write_text('raise RuntimeError("MUST NEVER IMPORT")')
        (self.mount / 'requirements-runtime.lock').write_text('SYNTHETIC_NOT_READ')
        self.calls = []

    def record(self, number=1):
        return {'id': f'{number:064x}', 'name': '/agent-' + str(number), 'running': True,
                'mounts': [{'Source': str(self.mount), 'Destination': inventory.MOUNT, 'RW': False, 'Type': 'bind'}],
                'entrypoint': ['/usr/bin/tini', '--'], 'cmd': ['bash', '-c',
                    'set -a; source /run/secrets/sessionhelper_env; exec ' + inventory.PYTHON + ' -m sessionhelper'],
                'restart_policy': 'unless-stopped', 'pythonpath_entries': ['', 'PYTHONPATH=' + inventory.MOUNT]}

    def runner(self, records, python_version=(3, 12, 3)):
        def fake(argv):
            self.calls.append(argv)
            if argv == ['docker', 'ps', '-a', '-q']:
                return '\n'.join(x['id'] for x in records), ''
            if argv[:2] == ['docker', 'inspect']:
                self.assertEqual(argv[2:4], ['--format', inventory.INSPECT_FORMAT])
                return json.dumps(next(x for x in records if x['id'] == argv[-1])), ''
            if argv[:2] == ['docker', 'exec']:
                self.assertEqual(argv[3:], [inventory.PYTHON, '-I', '-S', '-B', '-c', inventory.VERSION_PROBE])
                return json.dumps(python_version), ''
            if argv[:2] == ['docker', 'logs']:
                self.assertEqual(argv[2:5], ['--timestamps', '--tail', '500'])
                return ('2026-10-16T02:00:00.000000000Z [sessionHelper] connected self=test.owner version=0.2.2.2\n',
                        '2026-10-16T01:00:00.000000000Z [sessionHelper] connected self=test.owner version=0.2.2.1\n')
            self.fail('Unexpected command: ' + repr(argv))
        return fake

    def test_twelve_container_inventory_is_readonly_and_passive(self):
        before = {str(x): x.stat().st_mtime_ns for x in self.mount.rglob('*')}
        result = inventory.inventory(self.runner([self.record(n) for n in range(1, 13)]))
        self.assertEqual(len(result['containers']), 12)
        for item in result['containers']:
            self.assertEqual(item['stop_reasons'], [])
            self.assertEqual(item['passive_connection_version'], '0.2.2.2')
            self.assertEqual(item['python_version'], '3.12.3')
            self.assertEqual(item['source_scan']['non_python_files'], [
                {'path': 'requirements-runtime.lock', 'blocks_install': False}])
        self.assertEqual(before, {str(x): x.stat().st_mtime_ns for x in self.mount.rglob('*')})
        self.assertFalse(list(self.mount.rglob('__pycache__')))
        self.assertEqual({x[1] for x in self.calls}, {'ps', 'inspect', 'exec', 'logs'})

    def test_unrecognized_entry_and_pythonpath_never_echo_private_values(self):
        record = self.record()
        record['cmd'][2] = 'export SECRET=SYNTHETIC_PRIVATE_SENTINEL; exec sessionhelper'
        record['pythonpath_entries'] = ['PYTHONPATH=SYNTHETIC_PRIVATE_SENTINEL']
        result = inventory.inventory(self.runner([record]))
        serialized = json.dumps(result)
        self.assertNotIn('SYNTHETIC_PRIVATE_SENTINEL', serialized)
        self.assertIn('entry_unsupported', result['containers'][0]['stop_reasons'])
        self.assertNotIn('exec', {x[1] for x in self.calls})

    def test_non_python_files_and_symlinks_list_names_without_following(self):
        (self.mount / 'sessionhelper/app.py.bak').write_text('SYNTHETIC_PRIVATE_SENTINEL')
        outside = self.root / 'outside'
        outside.mkdir()
        (outside / 'must-not-list.txt').write_text('SYNTHETIC_PRIVATE_SENTINEL')
        (self.mount / 'sessionhelper/link').symlink_to(outside)
        result = inventory.inventory(self.runner([self.record()]))
        item = result['containers'][0]
        self.assertIn({'path': 'sessionhelper/app.py.bak', 'blocks_install': True},
                      item['source_scan']['non_python_files'])
        self.assertEqual(item['source_scan']['symlinks'], ['sessionhelper/link'])
        self.assertNotIn('SYNTHETIC_PRIVATE_SENTINEL', json.dumps(result))
        self.assertNotIn('must-not-list', json.dumps(result))

    def test_wrong_python_writable_mount_restart_policy_and_stopped_fail_closed(self):
        record = self.record()
        record['mounts'][0]['RW'] = True
        record['restart_policy'] = 'always'
        item = inventory.inventory(self.runner([record], (3, 11, 9)))['containers'][0]
        self.assertTrue({'readonly_bind_required', 'restart_policy_unsupported', 'python_3_12_required'} <= set(item['stop_reasons']))
        record['running'] = False
        item = inventory.inventory(self.runner([record]))['containers'][0]
        self.assertIn('container_not_running', item['stop_reasons'])
        self.assertIsNone(item['passive_connection_version'])

    def test_unknown_logs_and_missing_selected_container_are_not_pass(self):
        runner = self.runner([self.record()])
        def no_connection(argv):
            return ('SYNTHETIC_PRIVATE_SENTINEL', '') if argv[1] == 'logs' else runner(argv)
        item = inventory.inventory(no_connection)['containers'][0]
        self.assertIn('passive_connection_version_unknown', item['stop_reasons'])
        self.assertNotIn('SYNTHETIC_PRIVATE_SENTINEL', json.dumps(item))
        result = inventory.inventory(runner, containers=['nonexistent'])
        self.assertEqual(result['missing_requested_containers'], ['nonexistent'])

    def test_main_outputs_exactly_one_json_line_and_propagates_stops(self):
        for root, payload, expected in ((False, None, 1), (True, {'containers': [], 'missing_requested_containers': []}, 1),
                                       (True, {'containers': [{'stop_reasons': []}], 'missing_requested_containers': []}, 0)):
            out = io.StringIO()
            with patch('sys.argv', [str(SCRIPT)]), patch.object(os, 'geteuid', return_value=0 if root else 1001), \
                 patch.object(inventory, 'inventory', return_value=payload), contextlib.redirect_stdout(out):
                self.assertEqual(inventory.main(), expected)
            self.assertEqual(len(out.getvalue().splitlines()), 1)
            json.loads(out.getvalue())

    def test_runtime_errors_never_emit_raw_docker_output(self):
        with patch.object(inventory.subprocess, 'run', side_effect=OSError('SYNTHETIC_PRIVATE_SENTINEL')):
            with self.assertRaisesRegex(inventory.InventoryError, '^docker_unavailable_or_timeout$'):
                inventory.run(['docker', 'ps', '-a', '-q'])


if __name__ == '__main__':
    unittest.main()
