import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest

from sessionhelper.desktop import DesktopAdapter
from sessionhelper.desktop_stop_hook import continuation


class StopHookTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.path.realpath('/tmp'))
        self.root = Path(self.tmp.name)
        self.adapter = DesktopAdapter(SimpleNamespace(inbox_db=str(self.root / 'inbox')))
        self.binding = self.root / 'hook.json'
        self.config = dict(enabled=True, session_id='owner-thread', inbox_path=self.adapter.path)
        self.save()
        self.event = dict(hook_event_name='Stop', session_id='owner-thread',
                          turn_id='turn-1', stop_hook_active=False)

    def tearDown(self):
        self.tmp.cleanup()

    def save(self):
        self.binding.write_text(json.dumps(self.config))
        self.binding.chmod(0o600)

    def post(self):
        self.adapter.handle(dict(id='fixture', body='PRIVATE-BODY', **{'from': 'fixture-sender'}))

    def test_exact_bound_stop_one_shot_survives_reopen_unread(self):
        self.post()
        result = continuation(self.binding, self.event)
        self.assertEqual(result['decision'], 'block')
        self.assertNotIn('PRIVATE-BODY', json.dumps(result))
        self.assertEqual(self.adapter.request(dict(operation='status'))['unread'], 1)
        with sqlite3.connect(self.adapter.path) as db:
            self.assertEqual(db.execute('SELECT stage FROM desktop_stop_intent').fetchone()[0],
                             'continuation_requested')
        self.assertEqual(continuation(self.binding, dict(self.event, turn_id='turn-2')), {})

    def test_main_stop_only_and_no_continuation_loop(self):
        self.post()
        variants = [dict(hook_event_name='SubagentStop'), dict(hook_event_name='Interrupt'),
                    dict(stop_hook_active=True), dict(stop_hook_active=None),
                    dict(session_id='other'), dict(turn_id=''), dict(agent_id='subagent'),
                    dict(parent_agent_id='parent')]
        for delta in variants:
            self.assertEqual(continuation(self.binding, dict(self.event, **delta)), {})
        event = dict(self.event)
        del event['stop_hook_active']
        self.assertEqual(continuation(self.binding, event), {})
        self.assertEqual(continuation(self.binding, self.event)['decision'], 'block')

    def test_paused_empty_disabled_or_reply_uncertain_do_not_request(self):
        self.assertEqual(continuation(self.binding, self.event), {})
        self.post()
        self.adapter.request(dict(operation='pause'))
        self.assertEqual(continuation(self.binding, self.event), {})
        self.adapter.request(dict(operation='resume'))
        self.config['enabled'] = False
        self.save()
        self.assertEqual(continuation(self.binding, self.event), {})
        self.config['enabled'] = True
        self.save()
        self.adapter.reserve_mcp_reply('fixture', 'reply')
        self.assertEqual(continuation(self.binding, self.event), {})

    def test_cli_ignores_transcript_and_last_message(self):
        self.post()
        payload = dict(self.event, transcript_path='/does-not-exist/PRIVATE',
                       last_assistant_message='PRIVATE-ASSISTANT')
        proc = self.cli(json.dumps(payload))
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(json.loads(proc.stdout)['decision'], 'block')
        self.assertNotIn('PRIVATE', proc.stdout + proc.stderr)

    def cli(self, payload):
        env = dict(os.environ, PYTHONPATH=str(Path(__file__).resolve().parents[1]))
        return subprocess.run([sys.executable, '-B', '-m', 'sessionhelper.desktop_stop_hook',
                               '--binding', str(self.binding)], input=payload, text=True,
                              capture_output=True, env=env, timeout=3)

    def test_malformed_private_permissions_link_missing_db_no_creation(self):
        self.post()
        self.binding.chmod(0o644)
        self.assertEqual(json.loads(self.cli(json.dumps(self.event)).stdout), {})
        self.binding.chmod(0o600)
        self.assertEqual(json.loads(self.cli('{"session_id":"a","session_id":"b"}').stdout), {})
        self.assertEqual(json.loads(self.cli('X' * 65537).stdout), {})
        self.config['inbox_path'] = str(self.root / 'absent.sqlite')
        self.save()
        self.assertEqual(json.loads(self.cli(json.dumps(self.event)).stdout), {})
        self.assertFalse((self.root / 'absent.sqlite').exists())
        self.config['inbox_path'] = str(self.root / 'linked.sqlite')
        (self.root / 'linked.sqlite').symlink_to(self.adapter.path)
        self.save()
        self.assertEqual(json.loads(self.cli(json.dumps(self.event)).stdout), {})

    def test_locked_database_returns_without_continuing(self):
        self.post()
        with sqlite3.connect(self.adapter.path) as db:
            db.execute('BEGIN IMMEDIATE')
            self.assertEqual(json.loads(self.cli(json.dumps(self.event)).stdout), {})
            db.rollback()
        self.assertEqual(continuation(self.binding, self.event)['decision'], 'block')

    def test_two_hook_processes_cannot_repeat_one_intent(self):
        self.post()
        env = dict(os.environ, PYTHONPATH=str(Path(__file__).resolve().parents[1]))
        commands = [subprocess.Popen([sys.executable, '-B', '-m', 'sessionhelper.desktop_stop_hook',
                                     '--binding', str(self.binding)], stdin=subprocess.PIPE,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
                    for _ in range(2)]
        for process in commands:
            process.stdin.write(json.dumps(self.event))
            process.stdin.close()
            process.stdin = None
        results = [json.loads(process.communicate(timeout=3)[0]) for process in commands]
        self.assertEqual(sum(result.get('decision') == 'block' for result in results), 1)
        self.assertEqual(self.adapter.request(dict(operation='status'))['unread'], 1)


if __name__ == '__main__':
    unittest.main()
