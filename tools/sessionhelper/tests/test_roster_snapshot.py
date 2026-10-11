import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock
from sessionhelper.app import SessionHelper


class RosterSnapshotTests(unittest.TestCase):
    def test_full_replacement_empty_and_heartbeat(self):
        # No product constructor or CLI: exercise only the real roster file writer.
        helper = object.__new__(SessionHelper)
        helper.last_online_list = ''
        helper._online_list_lock = threading.Lock()
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'roster.list'
            helper.online_list_path = lambda: str(path)
            helper.write_online_list('leader\nremoved-peer')
            helper.write_online_list('leader')
            self.assertNotIn('removed-peer', path.read_text())
            helper.write_online_list(None)
            self.assertIn('leader', path.read_text())
            helper.write_online_list('')
            self.assertEqual(helper.last_online_list, '')
            self.assertNotIn('leader', path.read_text())
            self.assertFalse(Path(str(path) + '.tmp').exists())
