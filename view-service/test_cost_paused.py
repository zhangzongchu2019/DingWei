"""Paused capability: real local HTTP, auth/page SQLite, immutable cost artifacts.
Synthetic upstream stands in for Hub; this is not production or full Hub E2E.
"""
import base64
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path
import secrets
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import view_service as vs
import test_view_unlock_v2_integration as v2tests


def snapshot(root):
    result = {}
    for path in [root] + sorted(root.rglob('*')):
        st = path.lstat()
        result[str(path.relative_to(root))] = (
            st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_size,
            st.st_mtime_ns, st.st_ctime_ns,
            hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None)
    return result


class PausedHTTPTest(v2tests.ViewUnlockV2IntegrationTest):
    def setUp(self):
        super().setUp()
        self.cost_dir = Path(self.tmp.name) / 'cost'
        self.cost_dir.mkdir()
        self.state = self.cost_dir / 'old.json'
        self.state.write_text(json.dumps({'old-name': {'cost_total': 17, 'cost_last': 2,
                                                      'turns': 8, 'token': 'synthetic-private'}}))
        dbpath = str(self.state) + '.mux-cost.db'
        projection = vs.CostProjection(dbpath)
        projection.apply('old-agent', 'old-name', 'deadbeef-1',
                         {'kind': 'state_change', 'data': {'change': 'turn_completed', 'cost_usd': 17}})
        # A real, idle pre-existing WAL database keeps authentic sidecars alive
        # through the HTTP observation interval; VS must never open this DB.
        self.cost_db = sqlite3.connect(dbpath)
        self.cost_db.execute('PRAGMA journal_mode=WAL')
        self.cost_db.execute('PRAGMA wal_autocheckpoint=0')
        self.cost_db.execute('UPDATE mux_cost SET cost_total=cost_total')
        self.cost_db.commit()
        self.assertTrue(Path(dbpath + '-wal').is_file())
        self.assertTrue(Path(dbpath + '-shm').is_file())
        self.before = snapshot(self.cost_dir)
        self.mode = patch.multiple(vs, COST_ACCOUNTING_MODE='paused', STATE_FILE=str(self.state),
                                   PAGE_DB_FILE=str(Path(self.tmp.name) / 'pages.db'))
        self.mode.start()
        vs._validate_cost_config()

    def tearDown(self):
        try:
            self.assertEqual(snapshot(self.cost_dir), self.before)
        finally:
            # Checkpoint/close only after the zero-change observation has ended.
            self.cost_db.close()
            self.mode.stop()
            super().tearDown()

    def signed(self, method, path, raw):
        timestamp, nonce = str(int(time.time())), secrets.token_hex(16)
        return {'X-DW-VS-Target': 'canary', 'X-DW-Timestamp': timestamp,
                'X-DW-Nonce': nonce,
                'X-DW-Signature': vs._control_signature(self.forward, method, path, timestamp, nonce, raw)}

    def request(self, method, path, body=None, headers=None):
        conn = http.client.HTTPConnection('127.0.0.1', self.view.server_port, timeout=3)
        conn.request(method, path, body=body, headers=headers or {})
        response = conn.getresponse()
        result = response.status, dict(response.getheaders()), response.read()
        conn.close()
        return result

    def subject_headers(self, path):
        raw = json.dumps({'agent_id': 'stable', 'full_name': 'alice-dev55-2642'}).encode()
        return {**self.signed('GET', path, raw),
                'X-DW-Mux-Subject': base64.urlsafe_b64encode(raw).decode().rstrip('=')}

    def test_status_factory_unreachable_and_bad_subject_rejected(self):
        with patch.object(vs, '_cost_projection', side_effect=AssertionError('factory reached')) as factory:
            code, headers, raw = self.request('GET', '/status', headers=self.subject_headers('/status'))
            self.assertEqual(code, 200)
            body = json.loads(raw)
            self.assertEqual(body['cost_accounting']['mode'], 'paused')
            self.assertFalse(body['cost_accounting']['available'])
            self.assertTrue(body['readonly']); self.assertFalse(body['can_write'])
            for key in ('cost_total', 'cost_last', 'turns'):
                self.assertNotIn(key, body)
            self.assertNotIn(b'synthetic-private', raw)
            self.assertEqual(headers['Cache-Control'], 'no-store')
            self.assertIn('mem', body); self.assertIn('disk', body)
            bad = self.subject_headers('/status'); bad['X-DW-Signature'] = '0' * 64
            self.assertEqual(self.request('GET', '/status', headers=bad)[0], 401)
            factory.assert_not_called()

    def test_real_independent_and_aggregate_replay_leave_directory_unchanged(self):
        payload = {'kind': 'state_change', 'data': {'change': 'turn_completed', 'cost_usd': 99}}
        individual = b'id: deadbeef-9\ndata: ' + json.dumps(payload).encode() + b'\n\n'
        aggregate = b'data: ' + json.dumps({'type': 'event', 'agent_id': 'stable',
                   'full_name': 'alice-dev55-2642', 'epoch': 'deadbeef', 'seq': '9', 'payload': payload}).encode() + b'\n\n'
        reverse = self.reverse
        class Upstream(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Length', str(len(individual)))
                self.end_headers(); self.wfile.write(individual)
            def do_POST(self):
                raw = self.rfile.read(int(self.headers['Content-Length']))
                expected = vs._control_signature(reverse, 'POST', self.path,
                    self.headers['X-DW-Timestamp'], self.headers['X-DW-Nonce'], raw,
                    direction='view-service-to-hub/v1')
                if not hmac.compare_digest(expected, self.headers['X-DW-Signature']):
                    self.send_error(401); return
                self.send_response(200); self.send_header('Content-Length', str(len(aggregate)))
                self.end_headers(); self.wfile.write(aggregate)
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
        thread = threading.Thread(target=upstream.serve_forever, daemon=True); thread.start()
        try:
            with patch.object(vs, 'HUB_INTERNAL', f'http://127.0.0.1:{upstream.server_port}'), \
                 patch.object(vs, '_cost_projection', side_effect=AssertionError('factory reached')) as factory, \
                 patch.object(vs, '_ensure_ready', side_effect=AssertionError('legacy registration reached')) as legacy:
                for _ in range(2):
                    self.assertEqual(self.request('GET', '/events', headers=self.subject_headers('/events'))[2], individual)
                    # Unsigned direct V2 still cannot fall back to legacy cost writes.
                    self.assertEqual(self.request('GET', '/events')[2], individual)
                    raw = json.dumps({'feed_id': 'a'*32, 'bridge_id': 'b'*32, 'target': 'canary'}).encode()
                    signed = self.signed('POST', '/internal/console-mux/events', raw)
                    code, _, result = self.request('POST', '/internal/console-mux/events', raw, signed)
                    self.assertEqual((code, result), (200, aggregate))
                bad = self.signed('POST', '/internal/console-mux/events', raw)
                bad['X-DW-Signature'] = '0'*64
                self.assertEqual(self.request('POST', '/internal/console-mux/events', raw, bad)[0], 401)
                factory.assert_not_called(); legacy.assert_not_called()
        finally:
            upstream.shutdown(); upstream.server_close(); thread.join()

    def test_bad_legacy_json_and_unwritable_cost_dir_do_not_affect_status(self):
        self.state.write_bytes(b'not json')
        os.chmod(self.cost_dir, 0o500)
        self.before = snapshot(self.cost_dir)
        try:
            self.assertEqual(self.request('GET', '/status')[0], 200)
            with self.assertRaises(vs.CostAccountingPaused):
                vs.CostProjection(str(self.cost_dir / 'must-not-create.db'))
        finally:
            self.assertEqual(snapshot(self.cost_dir), self.before)
            os.chmod(self.cost_dir, 0o700)
            self.before = snapshot(self.cost_dir)

    def test_concentrated_guards_and_factory_existing_instance(self):
        handler = object.__new__(vs._Handler)
        for action in (vs._save_state_locked, vs._cost_projection,
                       lambda: vs._ensure_ready('a'), lambda: vs._ensure_ready_locked('a'),
                       lambda: handler._track_cost('a', b'{}'),
                       lambda: vs.CostProjection(str(self.cost_dir / 'new.db'))):
            with self.assertRaises(vs.CostAccountingPaused): action()
        # Existing objects cannot resume by bypassing the factory while paused.
        obj = object.__new__(vs.CostProjection)
        with self.assertRaises(vs.CostAccountingPaused): obj.apply('a', 'a', 'deadbeef-1', {})
        with self.assertRaises(vs.CostAccountingPaused):
            with obj._connect(): pass

    def test_both_template_modes_get_fixed_early_flag(self):
        for suffix in ('', '?console_mux=1&cost_mode=active'):
            code, _, raw = self.request('GET', '/' + suffix)
            self.assertEqual(code, 200)
            self.assertIn(b'const costAccountingMode = "paused";', raw)
            self.assertNotIn(b'VS_COST_ACCOUNTING_MODE */', raw)
            for ident in ('filePicker', 'shotBtn', 'humanInput', 'memStat', 'diskStat', 'localTime'):
                self.assertIn(('id="'+ident+'"').encode(), raw)
        stale = Path(self.tmp.name) / 'stale-renderer.html'
        stale.write_text('<html>old renderer without mode contract</html>')
        with patch.object(vs, 'RENDERER_PATH', str(stale)):
            self.assertEqual(self.request('GET', '/')[0], 503)


class ModeConfigTest(unittest.TestCase):
    def test_valid_paused_import_does_not_load_or_create_cost_artifacts(self):
        with tempfile.TemporaryDirectory() as root:
            cost = Path(root) / 'cost'; cost.mkdir()
            state = cost / 'old.json'; state.write_bytes(b'unreadable legacy json')
            before = snapshot(cost)
            env = dict(os.environ, VS_COST_ACCOUNTING_MODE='paused', VS_VIEW_UNLOCK_V2='1',
                       VS_STATE_FILE=str(state), VS_PAGE_DB=str(Path(root)/'pages.db'))
            result = subprocess.run([sys.executable, '-B', '-c',
                    "import view_service as v; assert v.COST_ACCOUNTING_MODE == 'paused'; "
                    "assert v._sessions == {}; assert v._projection is None"],
                    env=env, cwd=Path(vs.__file__).parent, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.assertEqual(result.returncode, 0)
            self.assertEqual(snapshot(cost), before)
            self.assertFalse((Path(root)/'pages.db').exists())
            # A co-located module cache is rejected unless bytecode is disabled.
            with patch.multiple(vs, COST_ACCOUNTING_MODE='paused', VIEW_UNLOCK_V2=True,
                                 STATE_FILE=str(state), PAGE_DB_FILE=str(Path(root)/'pages.db'),
                                 BASE_DIR=str(cost)), patch.object(sys, 'dont_write_bytecode', False):
                with self.assertRaises(ValueError): vs._validate_cost_config()

    def test_bad_enumeration_v1_and_path_alias_rejected_before_startup(self):
        with tempfile.TemporaryDirectory() as root:
            cost = Path(root) / 'cost'; cost.mkdir()
            state = cost / 'old.json'; state.write_text('{}')
            for mode, v2, pages in (('', '1', Path(root)/'page.db'),
                                    ('PAUSED', '1', Path(root)/'page.db'),
                                    ('paused', '0', Path(root)/'page.db'),
                                    ('paused', '1', cost/'page.db')):
                env = dict(os.environ, VS_COST_ACCOUNTING_MODE=mode, VS_VIEW_UNLOCK_V2=v2,
                           VS_STATE_FILE=str(state), VS_PAGE_DB=str(pages), PYTHONDONTWRITEBYTECODE='1')
                result = subprocess.run([sys.executable, '-B', '-c', 'import view_service'],
                        env=env, cwd=Path(vs.__file__).parent, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(pages.exists())
            # An existing hardlink outside the directory cannot alias a cost file.
            for original in (state, Path(str(state)+'.mux-cost.db-wal')):
                if not original.exists(): original.write_bytes(b'preserved sidecar')
                outside = Path(root)/'alias.db'; os.link(original, outside)
                try:
                    with patch.multiple(vs, COST_ACCOUNTING_MODE='paused', VIEW_UNLOCK_V2=True,
                                         STATE_FILE=str(state), PAGE_DB_FILE=str(outside)):
                        with self.assertRaises(ValueError): vs._validate_cost_config()
                finally:
                    outside.unlink()
            env = dict(os.environ, VS_COST_ACCOUNTING_MODE='paused', VS_VIEW_UNLOCK_V2='1',
                       VS_STATE_FILE=str(state), VS_PAGE_DB=str(Path(root)/'separate-pages.db'))
            # A redirected log inside costdir is rejected before page creation.
            with (cost/'service.log').open('wb') as log:
                result = subprocess.run([sys.executable, '-B', '-c', 'import view_service'],
                         env=env, cwd=Path(vs.__file__).parent, stdout=log, stderr=subprocess.PIPE)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((Path(root)/'separate-pages.db').exists())

    def test_interpreter_entrypoint_bytecode_boundary(self):
        # Python may cache the module being imported before its body validates
        # configuration. Rejection is not a promise of zero interpreter writes.
        for colocated, entry in ((True, 'unsafe'), (True, 'flag'),
                                 (True, 'environment'), (False, 'outside')):
            with self.subTest(entry=entry), tempfile.TemporaryDirectory() as root:
                root = Path(root)
                cost = root / 'cost'; cost.mkdir()
                state = cost / 'old.json'; state.write_bytes(b'preserved old artifact')
                code = cost if colocated else root / 'source'
                if not code.exists(): code.mkdir()
                for filename in ('view_service.py', 'view_page_store.py'):
                    shutil.copyfile(Path(vs.__file__).parent / filename, code / filename)
                before = snapshot(cost)
                env = dict(os.environ, VS_COST_ACCOUNTING_MODE='paused', VS_VIEW_UNLOCK_V2='1',
                           VS_STATE_FILE=str(state), VS_PAGE_DB=str(root / 'pages.db'),
                           PYTHONPATH=str(code))
                env.pop('PYTHONDONTWRITEBYTECODE', None)
                env.pop('PYTHONPYCACHEPREFIX', None)
                flags = ['-B'] if entry == 'flag' else []
                if entry == 'environment': env['PYTHONDONTWRITEBYTECODE'] = '1'
                result = subprocess.run([sys.executable, *flags, '-c', 'import view_service'],
                        env=env, cwd=code, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                if entry == 'unsafe':
                    self.assertEqual(result.returncode, 1)
                    self.assertIn(b'Python bytecode must not write inside the cost directory', result.stderr)
                    self.assertTrue(list((cost / '__pycache__').glob('view_service.*.pyc')))
                    self.assertNotEqual(snapshot(cost), before)
                    self.assertEqual(state.read_bytes(), b'preserved old artifact')
                else:
                    self.assertEqual(result.returncode, 0)
                    self.assertEqual(snapshot(cost), before)
                    if entry == 'outside':
                        self.assertTrue(list((code / '__pycache__').glob('view_service.*.pyc')))
                self.assertFalse((root / 'pages.db').exists())
                print('ENTRYPOINT_BOUNDARY ' + json.dumps({
                    'entry': entry, 'actual_rc': result.returncode,
                    'costdir_unchanged': snapshot(cost) == before,
                    'module_cache_in_costdir': bool(list((cost / '__pycache__').glob('view_service.*.pyc')))
                }), flush=True)
