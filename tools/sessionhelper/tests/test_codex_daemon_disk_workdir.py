"""Local managed-daemon cwd binding; never starts a real Codex daemon/model."""
import json
import os
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

from sessionhelper.agent_disk_metrics import BoundedDiskPublisher, DiskSampler
from sessionhelper.codexDriver import CodexProtocolDriver
from sessionhelper.driver_adapter import DriverAdapter


class ManagedDaemonDisk(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.workdir = os.path.join(self.temp.name, 'agent-work')
        os.mkdir(self.workdir)
        self.driver = CodexProtocolDriver(
            workdir=self.workdir, store_path=os.path.join(self.temp.name, 'ledger.db'))
        self.adapter = object.__new__(DriverAdapter)
        self.adapter._started = True
        self.adapter._which = 'codex'
        self.adapter.driver = self.driver
        # daemon_ws's real transport has no owned _proc, unlike stdio.
        self.driver._rpc = types.SimpleNamespace(
            request=Mock(return_value={'thread': {'id': 'synthetic-thread'}}))

    def collect(self):
        sampler = DiskSampler(instance='a' * 32)
        return json.loads(sampler.wire(sampler.collect(self.adapter.agent_metrics_workdir())))

    def test_thread_and_daemon_start_use_same_configured_directory(self):
        self.assertEqual(self.driver._transport_mode, 'daemon_ws')
        self.assertIs(self.driver._manage_daemon, True)
        self.driver._ensure_thread()
        args = self.driver._rpc.request.call_args.args
        self.assertEqual(args[0], 'thread/start')
        self.assertEqual(args[1]['cwd'], self.workdir)
        with patch('sessionhelper.codexDriver.subprocess.run', return_value=types.SimpleNamespace(returncode=0)) as run:
            self.driver._start_daemon()
        self.assertEqual(run.call_args.kwargs['cwd'], self.workdir)
        self.assertEqual(run.call_args.args[0][1:], ['app-server', 'daemon', 'start'])
        self.assertEqual(self.adapter.agent_metrics_workdir(), self.workdir)

    def test_managed_directory_uses_real_open_fd_filesystem_capacity(self):
        observations = []
        class Syscalls:
            def __getattr__(self, name):
                return getattr(os, name)
            def fstatvfs(self, fd):
                result = os.fstatvfs(fd)
                observations.append((os.fstat(fd), result))
                return result
        sampler = DiskSampler(instance='a' * 32, syscalls=Syscalls())
        record = json.loads(sampler.wire(sampler.collect(self.adapter.agent_metrics_workdir())))
        self.assertEqual(record['status'], 'ok')
        self.assertEqual(record['path_basis'], 'cli_workdir_filesystem')
        self.assertEqual(len(observations), 1)
        inode, fs = observations[0]
        expected = os.stat(self.workdir)
        self.assertEqual((inode.st_dev, inode.st_ino), (expected.st_dev, expected.st_ino))
        self.assertEqual(record['total_bytes'], fs.f_blocks * fs.f_frsize)
        self.assertEqual(record['used_bytes'], (fs.f_blocks - fs.f_bfree) * fs.f_frsize)
        self.assertEqual(record['available_bytes'], fs.f_bavail * fs.f_frsize)
        self.assertNotIn(self.workdir, json.dumps(record))

    def test_missing_relative_symlink_and_regular_file_are_unavailable(self):
        link = os.path.join(self.temp.name, 'symlink')
        os.symlink(self.workdir, link)
        plain = os.path.join(self.temp.name, 'file')
        with open(plain, 'w') as f:
            f.write('not a directory')
        for path, reason in [(os.path.join(self.temp.name, 'missing'), 'sample_failed'),
                             ('relative', 'workdir_unbound'),
                             (link, 'sample_failed'), (plain, 'sample_failed')]:
            with self.subTest(path_kind=reason):
                self.driver._workdir = path
                record = self.collect()
                self.assertEqual(record['status'], 'unavailable')
                self.assertEqual(record['reason'], reason)
                self.assertNotIn('total_bytes', record)

    def test_external_daemon_and_not_started_remain_unbound(self):
        self.driver._manage_daemon = False
        self.assertIsNone(self.adapter.agent_metrics_workdir())
        self.assertEqual(self.collect()['reason'], 'workdir_unbound')
        self.driver._manage_daemon = True
        self.adapter._started = False
        self.assertIsNone(self.adapter.agent_metrics_workdir())

    def test_other_drivers_and_direct_codex_keep_owned_process_binding(self):
        proc = types.SimpleNamespace(pid=123, poll=lambda: None)
        with patch('sessionhelper.agent_disk_metrics.process_workdir', return_value='/owned-process') as get:
            for which in ['codex', 'claude', 'opencode']:
                with self.subTest(which=which):
                    self.adapter._which = which
                    self.adapter.driver = types.SimpleNamespace(
                        _transport_mode='direct', _rpc=types.SimpleNamespace(_proc=proc), _proc=proc)
                    self.assertEqual(self.adapter.agent_metrics_workdir(), '/owned-process')
                    self.assertEqual(get.call_args.args[0], 123)


class ManagedPublisher(unittest.IsolatedAsyncioTestCase):
    async def test_actual_publisher_accepts_managed_daemon_candidate(self):
        with tempfile.TemporaryDirectory() as workdir:
            adapter = object.__new__(DriverAdapter)
            adapter._started, adapter._which = True, 'codex'
            adapter.driver = types.SimpleNamespace(
                _transport_mode='daemon_ws', _manage_daemon=True, _workdir=workdir,
                _rpc=types.SimpleNamespace())
            record = json.loads(await BoundedDiskPublisher().sample(adapter))
            self.assertEqual(record['status'], 'ok')
            self.assertEqual(record['path_basis'], 'cli_workdir_filesystem')


if __name__ == '__main__':
    unittest.main()
