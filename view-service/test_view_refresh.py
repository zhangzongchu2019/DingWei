"""Recorder-only handlers, real temporary page/cost SQLite; no HTTP/listeners."""
import base64
import hashlib
import io
import json
from pathlib import Path
import secrets
import sqlite3
import tempfile
import time
import unittest
from unittest.mock import patch

import view_service as vs
from view_page_store import ViewPageStore


class Recorder(vs._Handler):
    def __init__(self, path, headers=None, body=None):
        self.path = path
        self.headers = {"X-DW-Session": "agent.owner", **(headers or {})}
        raw = json.dumps(body or {}).encode()
        self.headers["Content-Length"] = str(len(raw))
        self.rfile, self.wfile = io.BytesIO(raw), io.BytesIO()
        self.response_headers, self.code = {}, None

    def send_response(self, code):
        self.code = code

    def send_header(self, key, value):
        self.response_headers[key] = value

    def end_headers(self):
        pass

    def send_error(self, code, *args):
        self.code = code

    def result(self):
        return json.loads(self.wfile.getvalue())


def snapshot(root):
    return [(str(p.relative_to(root)), p.stat().st_ino, p.stat().st_size,
             p.stat().st_mtime_ns, p.stat().st_ctime_ns,
             hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None)
            for p in [root] + sorted(root.rglob('*'))]


class ViewRefreshRecorderTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.cost = root / 'cost'
        self.cost.mkdir()
        (self.cost / 'old.json').write_text('{"retained_cost":17}')
        self.db = sqlite3.connect(self.cost / 'old.db')
        self.db.execute('PRAGMA journal_mode=WAL')
        self.db.execute('PRAGMA wal_autocheckpoint=0')
        self.db.execute('CREATE TABLE old(cost INTEGER)')
        self.db.execute('INSERT INTO old VALUES(17)')
        self.db.commit()
        self.assertTrue((self.cost / 'old.db-wal').exists())
        self.page = ViewPageStore(str(root / 'page.db'), now=time.time)
        self.key = b'synthetic-signing-key-longer-than-32-bytes'
        keypath = root / 'forward.key'
        keypath.write_bytes(self.key)
        self.context = patch.multiple(vs, VIEW_UNLOCK_V2=True,
            COST_ACCOUNTING_MODE='paused', STATE_FILE=str(self.cost / 'old.json'),
            PAGE_DB_FILE=str(root / 'page.db'), _page_store=self.page,
            VIEW_TARGET_NAME='fixture', HUB_TO_VS_SECRET_FILE=str(keypath), VS_TO_HUB_SECRET_FILE=str(keypath))
        self.context.start()
        self.no_network = patch.object(vs.urllib.request, 'urlopen', side_effect=AssertionError('network prohibited'))
        self.no_network.start()
        self.before = snapshot(self.cost)

    def tearDown(self):
        try:
            self.assertEqual(snapshot(self.cost), self.before)
        finally:
            self.no_network.stop()
            self.context.stop()
            self.page._db.close()
            self.db.close()
            self.tmp.cleanup()

    def signed(self, bad=False):
        raw = json.dumps({'agent_id': 'stable', 'full_name': 'agent.owner'}).encode()
        stamp, nonce = str(int(time.time())), secrets.token_hex(16)
        return {'X-DW-Mux-Subject': base64.urlsafe_b64encode(raw).decode().rstrip('='),
                'X-DW-VS-Target': 'fixture', 'X-DW-Timestamp': stamp, 'X-DW-Nonce': nonce,
                'X-DW-Signature': '0' * 64 if bad else vs._control_signature(self.key, 'GET', '/status', stamp, nonce, raw)}

    def test_resource_only_modes_subject_and_no_cost(self):
        for mode in ['paused', 'active']:
            with self.subTest(mode=mode), patch.object(vs, 'COST_ACCOUNTING_MODE', mode), \
                 patch.object(vs, '_cost_projection', side_effect=AssertionError('projection touched')), \
                 patch.object(vs, '_ensure_ready', side_effect=AssertionError('collector touched')):
                begin = int(time.time() * 1000)
                h = Recorder('/status?resources_only=1', self.signed())
                h._serve_status()
                self.assertEqual(h.code, 200)
                data = h.result()
                self.assertEqual(set(data), {'mem', 'disk', 'datetime_epoch_ms', 'resource_scope'})
                self.assertLessEqual(begin, data['datetime_epoch_ms'])
                self.assertLessEqual(data['datetime_epoch_ms'], int(time.time() * 1000))
                self.assertEqual(data['resource_scope'], 'view-service')
                self.assertEqual(h.response_headers['Cache-Control'], 'no-store')
                bad = Recorder('/status?resources_only=1', self.signed(True))
                bad._serve_status()
                self.assertEqual(bad.code, 401)
                unsigned = Recorder('/status?resources_only=1'); unsigned._serve_status()
                self.assertEqual(unsigned.code, 401)

    def test_full_paused_compatible_active_fail_closed(self):
        with patch.object(vs, '_cost_projection', side_effect=sqlite3.OperationalError('synthetic')) as factory:
            h = Recorder('/status', self.signed()); h._serve_status()
            self.assertEqual(h.code, 200)
            self.assertEqual(h.result()['auth_mode'], 'view_page_v2')
            self.assertIn('datetime', h.result())
            self.assertIn('datetime_epoch_ms', h.result())
            self.assertNotIn('cost_total', h.result())
            factory.assert_not_called()
            with patch.object(vs, 'COST_ACCOUNTING_MODE', 'active'):
                h = Recorder('/status', self.signed()); h._serve_status()
                self.assertEqual(h.code, 503)

    def test_legacy_auth_heartbeat_excludes_resources(self):
        with patch.object(vs, 'VIEW_UNLOCK_V2', False), \
             patch.object(vs, '_ensure_ready', return_value={'can_write': True}) as ready, \
             patch.object(vs, '_read_mem', side_effect=AssertionError('resource touched')), \
             patch.object(vs, '_read_disk', side_effect=AssertionError('resource touched')):
            h = Recorder('/status?auth_only=1'); h._serve_status()
            self.assertEqual(h.code, 200)
            self.assertEqual(h.result(), {'readonly': False})
            ready.assert_called_once_with('agent.owner')

    def test_console_controls_and_manual_lock_preserved(self):
        identity = self.page.create_page('agent.owner')
        base = {**identity, 'request_id': 'request', 'message_id': 'message',
                'delivery_id': 'delivery', 'turn_id': 'turn', 'kind': 'cancel'}
        for endpoint, function in [('/control', '_hub_v2_cancel'), ('/interrupt', '_hub_v2_interrupt')]:
            with self.subTest(endpoint=endpoint), patch.object(vs, function, return_value=(200, {'ok': True})) as hub:
                manual = Recorder(endpoint, body=base)
                getattr(manual, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                self.assertEqual(manual.code, 403)
                hub.assert_not_called()
                # Attestation consumed here is supplied only by the Hub's unchanged
                # cookie+owner+Origin+page/Agent/session/ticket verifier (Go recorder test).
                trusted = {'X-DW-Console-Capability-Verified': '1',
                           'X-DW-Console-Page': identity['page_id'], 'X-DW-Console-Principal': 'owner'}
                lock_before = tuple(self.page._db.execute('SELECT state,unlock_expires_at,last_write_at FROM view_pages WHERE page_id=?', (identity['page_id'],)).fetchone())
                console = Recorder(endpoint, trusted, base)
                getattr(console, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                self.assertEqual(console.code, 200)
                self.assertTrue(console.result()['ok'])
                hub.assert_called_once()
                self.assertFalse(self.page.authorize_page(identity['page_id'], identity['page_token'], 'agent.owner'))
                lock_after = tuple(self.page._db.execute('SELECT state,unlock_expires_at,last_write_at FROM view_pages WHERE page_id=?', (identity['page_id'],)).fetchone())
                self.assertEqual(lock_before, lock_after)
                for bad_headers in [
                    {**trusted, 'X-DW-Console-Page': 'other-page'},
                    {**trusted, 'X-DW-Console-Principal': ''},
                    {**trusted, 'X-DW-Console-Capability-Verified': 'true'},
                    {},
                ]:
                    bad = Recorder(endpoint, bad_headers, {**base, 'console_verified': True})
                    getattr(bad, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                    self.assertEqual(bad.code, 403)
                for field in ['request_id', 'page_id', 'page_token', 'delivery_id', 'message_id' if endpoint == '/control' else 'turn_id']:
                    bad = Recorder(endpoint, trusted, {**base, field: ''})
                    getattr(bad, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                    self.assertEqual(bad.code, 400)
                invalid = Recorder(endpoint, body={**base, 'page_token': ''})
                getattr(invalid, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                self.assertEqual(invalid.code, 400)
                spoof = Recorder(endpoint + '?console_mux=1', body={**base, 'console_verified': True})
                getattr(spoof, '_handle_control' if endpoint == '/control' else '_handle_interrupt')('agent.owner')
                self.assertEqual(spoof.code, 403)
        self.assertEqual(self.page.unlock({'command_id': 'manual-cmd', 'request_id': 'manual-unlock',
            'session': 'agent.owner', 'code': identity['code'], 'owner_key': 'owner'}), 200)
        with patch.object(vs, '_hub_v2_cancel', return_value=(200, {'ok': True})) as hub:
            manual = Recorder('/control', body=base); manual._handle_control('agent.owner')
            self.assertEqual(manual.code, 200); hub.assert_called_once()


    def test_real_signer_preserves_signed_address_contract(self):
        for function, address in [(vs._hub_v2_cancel, ('message', 'delivery')),
                                  (vs._hub_v2_interrupt, ('delivery', 'turn'))]:
            with self.subTest(function=function.__name__), patch.object(vs.urllib.request, 'urlopen') as client:
                response = client.return_value.__enter__.return_value
                response.status, response.read.return_value = 200, b'{"ok":true}'
                self.assertEqual(function('agent.owner', 'page', 'request', *address), (200, {'ok': True}))
                req = client.call_args.args[0]
                headers = {k.lower(): v for k, v in req.header_items()}
                self.assertEqual(headers['x-dw-signature'], vs._control_signature(
                    self.key, 'POST', req.selector, headers['x-dw-timestamp'], headers['x-dw-nonce'], req.data,
                    direction='view-service-to-hub/v1'))
                payload = json.loads(req.data)
                self.assertEqual(payload['page_id'], 'page'); self.assertEqual(payload['request_id'], 'request')
                self.assertIn('delivery_id', payload)


if __name__ == '__main__':
    unittest.main()
