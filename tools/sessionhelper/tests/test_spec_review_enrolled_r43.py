"""r43-H1: enrolled helpers must accept Console spec requests from the pinned Hub system address.

Dedicated (enrolled) Agents run with the placeholder key_id "enrolled" while the Hub
addresses its system envelopes to the real owner key. Before r43 every such request was
silently dropped (Hub saw legacy_received, then timed_out after 180 s)."""
import asyncio
import contextlib
import io
import json
import threading
import types
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from sessionhelper.app import SessionHelper
from sessionhelper.spec_review import analysis_envelope

OWNER_SENDER = 'workpulse#u_r43owner'


def event(sender=OWNER_SENDER, system=True, request_id='specreview_r43'):
    return {'from': sender, 'body': json.dumps({'request_id': request_id}),
            'meta': {'type': 'delivery', 'spec_review_type': 'console_spec_review_request',
                     'system': system, 'delivery_id': 'dw-dlv_r43'}}


class AnalysisEnvelopeSender(unittest.TestCase):
    def test_enrolled_placeholder_key_accepts_pinned_sender(self):
        self.assertIsNone(analysis_envelope(event(), 'enrolled'), 'R43_LEGACY_RULE_CHANGED')
        self.assertIsNotNone(analysis_envelope(event(), 'enrolled', OWNER_SENDER), 'R43_ENROLLED_REQUEST_DROPPED')

    def test_pinned_sender_still_rejects_forgery(self):
        for env in (event('workpulse#u_other'), event('forged#u_r43owner'), event(system=False),
                    event(request_id='specreview_x\nRUN')):
            self.assertIsNone(analysis_envelope(env, 'enrolled', OWNER_SENDER), 'R43_FORGED_REQUEST_ACCEPTED')

    def test_empty_pin_rejects_even_matching_empty_from(self):
        env = event('')
        self.assertIsNone(analysis_envelope(env, 'enrolled', ''), 'R43_EMPTY_PIN_ACCEPTED')


class AppProcessesEnrolledRequest(unittest.TestCase):
    def helper(self, pin, enrolled=True, key_id='enrolled'):
        helper = SessionHelper.__new__(SessionHelper)
        helper.book = types.SimpleNamespace(key_id=key_id)
        helper.adapter_handle_lock = asyncio.Lock()
        helper.enroll_credential = object() if enrolled else None
        helper._connection_delivery_ack_source = pin
        helper.inputs = []
        helper.adapter = types.SimpleNamespace(handle=lambda env: helper.inputs.append(env) or 'not auto sent')
        return helper

    def run_one(self, helper, env):
        class WS:
            async def send(self, _):
                raise AssertionError('R43_AUTOMATIC_REPLY_SENT')
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            asyncio.run(helper.process_inbound_message(WS(), env))
        return out.getvalue()

    def test_enrolled_connection_injects_bounded_analysis(self):
        helper = self.helper(OWNER_SENDER)
        self.run_one(helper, event())
        self.assertEqual(len(helper.inputs), 1, 'R43_ENROLLED_REQUEST_NOT_INJECTED')
        self.assertIn('specreview_r43', helper.inputs[0]['body'])
        self.assertTrue(helper.inputs[0]['meta']['spec_review_analysis'])

    def test_enrolled_without_pinned_sender_drops_with_log(self):
        helper = self.helper('')
        log = self.run_one(helper, event())
        self.assertEqual(helper.inputs, [], 'R43_UNPINNED_ENROLLED_ACCEPTED')
        self.assertIn('[spec_review] dropped', log, 'R43_DROP_IS_SILENT')
        self.assertIn('sender_pinned=False', log)
        self.assertIn('dw-dlv_r43', log)

    def test_reconnect_with_different_owner_rejects_old_sender(self):
        helper = self.helper('workpulse#u_after_reconnect')
        log = self.run_one(helper, event())
        self.assertEqual(helper.inputs, [], 'R43_STALE_PIN_ACCEPTED')
        self.assertIn('sender_match=False', log)

    def test_shared_key_helper_keeps_legacy_rule(self):
        helper = self.helper('', enrolled=False, key_id='u_r43owner')
        self.run_one(helper, event())
        self.assertEqual(len(helper.inputs), 1, 'R43_SHARED_KEY_REGRESSION')


class SpecWriteReturnsHubError(unittest.IsolatedAsyncioTestCase):
    HUB_TEXT = 'acceptance original quote must occur in current spec content: AC-01「x」'

    async def asyncSetUp(self):
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                self.rfile.read(int(self.headers['Content-Length']))
                body = (outer.HUB_TEXT + '\n\x1b[31m' + 'z' * 1000).encode()
                status = 400 if self.path.endswith('/specs') else 503
                self.send_response(status)
                self.send_header('Content-Type', 'text/plain; charset=utf-8')
                self.end_headers()
                self.wfile.write(body)

            do_PATCH = do_POST

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        owner = types.SimpleNamespace(enroll_credential=object(), _connection=lambda: (
            'ws://127.0.0.1:%d/ws/agent' % self.server.server_port, {'Authorization': 'Bearer synthetic-r43'}))
        self.handler = SessionHelper._make_spec_write_handler(owner, None)

    async def asyncTearDown(self):
        await asyncio.to_thread(self.server.shutdown)
        self.server.server_close()
        self.thread.join()

    async def test_400_carries_bounded_printable_hub_text(self):
        result = await self.handler(dict(action='create', protocol='direct-v1', body={'title': 't'}))
        self.assertEqual(result['http_status'], 400)
        self.assertIn('spec 字段无效', result['message'])
        self.assertTrue(result.get('hub_error', '').startswith(self.HUB_TEXT), 'R43_HUB_ERROR_DROPPED')
        self.assertLessEqual(len(result['hub_error']), 500, 'R43_HUB_ERROR_UNBOUNDED')
        self.assertFalse(any(not c.isprintable() for c in result['hub_error']), 'R43_CONTROL_CHARS_PASSED')
        self.assertNotIn('synthetic-r43', json.dumps(result, ensure_ascii=False))

    async def test_5xx_does_not_echo_server_text(self):
        result = await self.handler(dict(action='update', protocol='direct-v1', spec_id='spec-x', body={'title': 't'}))
        self.assertEqual(result['http_status'], 503)
        self.assertNotIn('hub_error', result, 'R43_5XX_TEXT_ECHOED')


if __name__ == '__main__':
    unittest.main()
