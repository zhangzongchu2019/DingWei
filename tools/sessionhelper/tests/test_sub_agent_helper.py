"""M2 criterion tests: local synthetic native transport only, never model inference."""
import asyncio
from concurrent.futures import ThreadPoolExecutor
import json
import os
from pathlib import Path
import queue
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import types
import unittest
from unittest.mock import patch
import uuid

from sessionhelper.app_server_broker import AppServerBroker, LocalDispatchRejected, SeatReadBudget
from sessionhelper.codexDriver import CodexProtocolDriver, StdioJsonRpc, _DeliveryStore
from sessionhelper.config import Config, load_config
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.inbox import DeliveryInbox
from sessionhelper.sub_agent_connection import SubAgentConnection
from sessionhelper.sub_agent_runtime import SubAgentRuntime
from sessionhelper.sub_agent_store import SubAgentStore, DEFAULT_POLICY, default_name, resolve_policy


def eventually(predicate, timeout=3):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if predicate(): return
        time.sleep(.005)
    raise AssertionError('expected condition did not become true')


def delivery(node, did='same', body='original business bytes'):
    return dict(id='original-envelope', to='root.owner#key', body=body, **{'from': 'sender.owner#key'},
        meta=dict(delivery_id=did, delivery_ack_protocol=2, processing_mode='driver', ack_generation=1,
                  target_node_id=node['node_id'], lane_generation=node['generation']))


class Native:
    """Native response waits are separate from writes; each write has a unique ID."""
    def __init__(self, cwd):
        self.cwd = cwd
        self.notifications = queue.Queue()
        self.calls, self.responses = [], []
        self.live = True
        self._next_id = 1
        self._write_started = None
        self.lock = threading.Lock()
        self.holds = {}
    def start(self): self.live = True
    def healthy(self): return self.live
    def stop(self): self.live = False
    def request(self, method, params, timeout=30):
        return self.wait_request(self.submit_request(method, params), method, timeout=timeout)
    def notify(self, method): self.calls.append((threading.current_thread().name, 0, method, {}))
    def submit_request(self, method, params, on_write=None):
        with self.lock:
            request_id = self._next_id; self._next_id += 1
            if method == 'turn/start' and on_write: on_write(params)
            self.calls.append((threading.current_thread().name, request_id, method, dict(params)))
            return request_id, method, dict(params)
    def wait_request(self, token, method, timeout=30):
        request_id, _, params = token
        hold = self.holds.get(params.get('threadId'))
        if hold is not None and method == 'turn/start':
            if not hold.wait(timeout): raise TimeoutError('synthetic native response withheld')
        if method == 'initialize': return {'protocol': 'synthetic'}
        if method == 'thread/start': return {'thread': {'id': str(uuid.uuid4()), 'cwd': self.cwd, 'status': {'type': 'idle'}}}
        if method == 'thread/read': return {'thread': {'id': params['threadId'], 'cwd': self.cwd, 'status': {'type': 'idle'}, 'canAcceptDirectInput': True, 'turns': []}}
        if method == 'thread/turns/list': return {'data': [], 'nextCursor': None}
        if method == 'turn/start': return {'turn': {'id': 'turn-' + str(request_id), 'status': 'inProgress'}}
        return {}
    def respond(self, request_id, result=None, error=None):
        self.responses.append((threading.current_thread().name, request_id, result, error))


class StoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.now = [1000000000.0]
        self.store = SubAgentStore(str(Path(self.tmp.name)/'inbox.db'), 'root', clock=lambda: self.now[0])
    def node(self, name='request', policy=None, aliases=(), native=None):
        node, _ = self.store.allocate(name, policy, aliases)
        self.store.mark_creation_submitted(node['node_id'])
        self.store.bind(node['node_id'], native or str(uuid.uuid4()))
        return self.store.node(node['node_id'])
    def acknowledge(self): self.store.acknowledge(self.store.snapshot(1, 0)['subtree_revision'])

    def test_aa006_random_tail_collision_alias_and_tombstone(self):
        self.assertEqual(default_name('019a6500-0000-7000-8000-5678abcdef12', {'abcdef12'}), '78abcdef12')
        a = self.node('a', native='019a6500-0000-7000-8000-1234abcdef12')
        b = self.node('b', native='019a6500-0000-7000-8000-5678abcdef12')
        self.assertEqual(a['canonical_name'], 'abcdef12')
        self.assertEqual(b['canonical_name'], '78abcdef12')
        self.assertNotEqual(a['canonical_name'], b['canonical_name'])
        self.store.seal(a['node_id'], 'completed'); self.store.end(a['node_id'])
        with self.assertRaisesRegex(ValueError, 'alias_conflict'): self.store.allocate('c', aliases=['abcdef12'])
        with self.assertRaisesRegex(ValueError, 'invalid creation'): self.store.bind(a['node_id'], str(uuid.uuid4()))
        self.assertEqual(default_name('019a6500-0000-7000-8000-123456789012', set()), '56789012')

    def test_create_idempotent_before_after_crash_and_conflicts(self):
        node, fresh = self.store.allocate('same-request', aliases=['alias'])
        self.assertTrue(fresh)
        self.assertTrue(self.store.mark_creation_submitted(node['node_id']))
        recovered = SubAgentStore(self.store.path, 'root')
        same, fresh = recovered.allocate('same-request', aliases=['alias'])
        self.assertFalse(fresh); self.assertEqual(same['creation_status'], 'submitted')
        self.assertFalse(recovered.mark_creation_submitted(node['node_id']))
        with self.assertRaisesRegex(ValueError, 'alias conflict'): recovered.allocate('same-request', aliases=['other'])
        with self.assertRaisesRegex(ValueError, 'policy conflict'): recovered.allocate('same-request', {'max_lifetime': 60}, ['alias'])

    def test_aa007_policy_defaults_and_negative_bounds(self):
        self.assertEqual(resolve_policy(), dict(exit_on_task_complete=True, idle_timeout=3600, max_lifetime=172800, platform_lifetime_cap=432000, exit_grace=600))
        self.assertEqual(resolve_policy({'idle_timeout': None, 'max_lifetime': None})['platform_lifetime_cap'], 432000)
        for value in ({'idle_timeout': 3601}, {'platform_lifetime_cap': 432001}, {'exit_grace': 601},
                      {'max_lifetime': False}, {'exit_on_task_complete': 1}, {'unknown': 10}, {'exit_grace': float('nan')}):
            with self.subTest(value=value), self.assertRaises(ValueError): resolve_policy(value)

    def test_tree_ack_current_connection_required_and_ended_not_revived(self):
        a = self.node(); inbox = self.store.inbox(a)
        with self.assertRaisesRegex(ValueError, 'registry_unavailable'): inbox.accept('same', delivery(a))
        self.acknowledge(); inbox.accept('same', delivery(a))
        self.store.seal(a['node_id'], 'completed'); self.store.end(a['node_id'])
        reopened = SubAgentStore(self.store.path, 'root')
        self.assertEqual(reopened.node(a['node_id'])['state'], 'ended')
        with self.assertRaisesRegex(ValueError, 'ended'): inbox.accept('late', delivery(a, 'late'))

    def test_aa007_atomic_seal_returns_only_definitely_unexecuted(self):
        a = self.node(); self.acknowledge(); inbox = self.store.inbox(a)
        for did in ['queued', 'local-dispatch', 'possibly-written']:
            inbox.accept(did, delivery(a, did))
        with inbox._connect() as db:
            for did in ('local-dispatch', 'possibly-written'):
                db.execute("UPDATE delivery_inbox SET state='processing' WHERE session_name=? AND delivery_id=?", (inbox.session_name, did))
        inbox.begin_dispatch('local-dispatch'); inbox.begin_dispatch('possibly-written')
        self.store.mark_native_write(a, 'possibly-written')
        self.store.seal(a['node_id'], 'completed')
        self.assertEqual({r['delivery_id'] for r in self.store.returns()}, {'queued', 'local-dispatch'})
        self.assertEqual(inbox.lifecycle('possibly-written')['state'], 'admission_unknown')
        self.assertFalse(any(r['payload']['state'] == 'processed' for r in inbox.delivery_ack_chain()))
        self.assertEqual(inbox.lifecycle('local-dispatch')['state'], 'failed')
        with self.assertRaisesRegex(ValueError, 'ended'): self.store.mark_native_write(a, 'local-dispatch')
        with self.assertRaisesRegex(ValueError, 'ended'): inbox.accept('late', delivery(a, 'late'))
        self.assertFalse(self.store.seal(a['node_id'], 'operator_closed'))
        pending = self.store.returns()[0]
        self.store.return_result(pending['delivery_id'], {}, 'uncertain', node_id=pending['node_id'], generation=pending['generation'])
        self.assertNotIn(pending['delivery_id'], [r['delivery_id'] for r in self.store.returns()])
        self.store.return_result(pending['delivery_id'], {'state': 'queued'}, 'returned', node_id=pending['node_id'], generation=pending['generation'])
        with self.store.transaction() as db:
            self.assertEqual(db.execute('SELECT state FROM helper_sub_agent_return WHERE delivery_id=?', (pending['delivery_id'],)).fetchone()[0], 'returned')

    def test_aa009_inbox_ledger_query_callback_generation_scope(self):
        a, b = self.node('a'), self.node('b'); self.acknowledge()
        ia, ib = self.store.inbox(a), self.store.inbox(b)
        root = DeliveryInbox(self.store.path, session_name='root')
        for inbox, node in ((ia, a), (ib, b)): inbox.accept('same', delivery(node))
        self.assertIsNone(root.lifecycle('same'))
        self.assertEqual(ia.claim_next()[0], 'same'); self.assertEqual(ib.claim_next()[0], 'same')
        ia.begin_dispatch('same'); ib.begin_dispatch('same')
        ctx = {key: ia.lifecycle('same')[key] for key in ('ack_generation', 'dispatch_intent_at', 'request_id')}
        ctx.update(lane_id=b['node_id'], lane_generation=1)
        with self.assertRaises(ValueError): ia.record_admitted('same', 'turn-b', time.time(), expected_context=ctx)
        self.assertIsNone(ia.lifecycle('same')['admitted_at'])
        with self.assertRaisesRegex(ValueError, 'generation_mismatch'): ia.accept('wrong', delivery(dict(a, generation=2), 'wrong'))
        p = str(Path(self.tmp.name)/'driver.db')
        sa = _DeliveryStore(p, lane_id=a['node_id'], lane_generation=1); sb = _DeliveryStore(p, lane_id=b['node_id'], lane_generation=1)
        sa.upsert('thread-a', 'same', 'ta', 'body', 'processing'); sb.upsert('thread-b', 'same', 'tb', 'body', 'processing')
        self.assertEqual(sa.get_by_envelope('same').turn_id, 'ta')
        self.assertEqual(sb.get_by_envelope('same').turn_id, 'tb')
        self.assertIsNone(_DeliveryStore(p).get_by_envelope('same'))
        self.assertIsNone(_DeliveryStore(p, lane_id=a['node_id'], lane_generation=2).get_by_envelope('same'))
        sb.update_state('thread-a', 'same', 'completed')
        self.assertEqual(sa.get('thread-a', 'same').state, 'processing')
        self.assertIsNone(sb.get('thread-a', 'same'))
        self.assertEqual(ia.pending_delivery_acks()[0]['payload']['target_node_id'], a['node_id'])

    def test_additive_legacy_root_data_readable(self):
        root = self.store.root_inbox
        root.accept('old', {'id': 'old', 'body': 'legacy', 'meta': {}})
        before = root.lifecycle('old')
        self.node(); SubAgentStore(self.store.path, 'root')
        self.assertEqual(root.lifecycle('old')['envelope_json'], before['envelope_json'])
        p = str(Path(self.tmp.name)/'old-driver.db')
        db = sqlite3.connect(p)
        db.execute('CREATE TABLE deliveries(thread_id TEXT,envelope_id TEXT,turn_id TEXT,body_hash TEXT,state TEXT,detail TEXT,created_at REAL,updated_at REAL,PRIMARY KEY(thread_id,envelope_id))')
        db.execute("INSERT INTO deliveries VALUES('old','e','t','h','processing','',1,1)"); db.commit(); db.close()
        self.assertEqual(_DeliveryStore(p).get_by_envelope('e').turn_id, 't')
        self.assertIsNone(_DeliveryStore(p, lane_id='new', lane_generation=1).get_by_envelope('e'))


class BrokerTests(unittest.TestCase):
    def setUp(self):
        self.native = Native('/synthetic')
        self.broker = AppServerBroker(self.native)
        self.addCleanup(self.broker.shutdown)
        self.broker.initialize({'capabilities': {'experimentalApi': True}})
        self.a = self.broker.facade('a', 1, 'thread-a')
        self.b = self.broker.facade('b', 1, 'thread-b')

    def test_aa008_outer_thread_only_and_bounded_quarantine(self):
        for n in range(150): self.native.notifications.put({'method': 'item/started', 'params': {'item': {'threadId': 'thread-a'}}})
        self.native.notifications.put({'method': 'turn/started', 'params': {'threadId': 'thread-b', 'turn': {'id': 'b'}}})
        eventually(lambda: not self.b.notifications.empty())
        self.assertTrue(self.a.notifications.empty()); self.assertEqual(len(self.broker.quarantine), 128)
        self.assertEqual(self.b.notifications.get()['params']['turn']['id'], 'b')
        with self.assertRaises(PermissionError): self.a.request('turn/start', {'threadId': 'thread-b'})
        with self.assertRaises(PermissionError): self.a.request('thread/start', {})

    def test_aa008_parallel_response_wait_and_single_physical_writer(self):
        gate = threading.Event(); self.native.holds['thread-a'] = gate
        with ThreadPoolExecutor(2) as executor:
            fa = executor.submit(self.a.request, 'turn/start', {'threadId': 'thread-a'})
            eventually(lambda: any(m == 'turn/start' for _, _, m, _ in self.native.calls))
            fb = executor.submit(self.b.request, 'turn/start', {'threadId': 'thread-b'})
            self.assertTrue(fb.result(timeout=2)['turn']['id'])
            self.assertFalse(fa.done()); gate.set(); self.assertTrue(fa.result(timeout=2))
        writes = [r for r in self.native.calls if r[2] == 'turn/start']
        self.assertEqual({r[0] for r in writes}, {'sub-agent-native-writer'})
        self.assertEqual(len({r[1] for r in writes}), 2)
        self.a.stop(); self.assertTrue(self.b.healthy()); self.assertTrue(self.native.live)
        self.assertTrue(self.b.request('thread/read', {'threadId': 'thread-b'}))

    def test_aa004_native_tool_source_and_stale_unknown_rejection(self):
        bound = []
        self.broker.tool_handler = lambda facade, params: bound.append(facade.identity.lane_id) or {'ok': True}
        call = dict(id=98, method='item/tool/call', params=dict(threadId='thread-b', turnId='turn-b', callId='c1', tool='send', arguments={'from_node_id': 'a'}))
        self.native.notifications.put(call)
        eventually(lambda: self.native.responses)
        self.assertEqual(bound, ['b']); self.assertEqual(self.native.responses[0][0], 'sub-agent-native-writer')
        self.native.notifications.put(call)
        self.native.notifications.put(dict(call, id=99, params=dict(call['params'], threadId='unknown', callId='c2')))
        eventually(lambda: len(self.native.responses) == 3)
        self.assertEqual(bound, ['b']); self.assertTrue(self.native.responses[-1][-1])

    def test_writer_rechecks_closed_node_and_starts_clock_at_write(self):
        called = []
        self.a.on_turn_start = lambda p: called.append('clock')
        self.a.write_guard = lambda p: (_ for _ in ()).throw(ValueError('ended'))
        with self.assertRaises(LocalDispatchRejected): self.a.request('turn/start', {'threadId': 'thread-a'})
        self.assertFalse(called)
        self.assertFalse(any(m == 'turn/start' for _, _, m, _ in self.native.calls))

    def test_recovery_does_not_kill_healthy_sibling(self):
        self.assertFalse(self.broker.recover()); self.assertTrue(self.b.healthy())
        self.native.live = False
        replacement = Native('/synthetic'); self.broker.transport_factory = lambda: replacement
        self.assertTrue(self.broker.recover()); self.assertEqual(self.broker.generation, 2)
        self.assertEqual(self.a.request('thread/read', {'threadId': 'thread-a'})['thread']['id'], 'thread-a')

    def test_aa009_seat_read_budget_shared_200_not_per_lane(self):
        self.assertIs(self.a.read_budget, self.b.read_budget)
        self.assertEqual(self.a.read_budget.available(), 200)
        self.a.read_budget.consume(199); self.assertEqual(self.b.read_budget.available(), 1)
        self.b.read_budget.consume(1); self.assertEqual(self.a.read_budget.available(), 0)

    def test_native_request_id_collision_with_client_pending(self):
        # Exercise the real stdio parser, not the Native response stub.
        code = '''import sys,json
for line in sys.stdin:
 m=json.loads(line)
 if m.get('method')=='probe':
  print(json.dumps({'id':m['id'],'method':'item/tool/call','params':{'threadId':'native-b'}}),flush=True)
  print(json.dumps({'id':m['id'],'result':{'actual_response':True}}),flush=True)
'''
        fixture = tempfile.TemporaryDirectory(); self.addCleanup(fixture.cleanup)
        # Stock failure-face fixtures can leave a patched constructor after a
        # module reload. A fresh Python process owns this transport experiment;
        # its PYTHONPATH points at the currently tested (also mutated) package.
        import sessionhelper.codexDriver as current_module
        module_root = str(Path(current_module.__file__).resolve().parent.parent)
        program = """import json,queue,sys
from sessionhelper.codexDriver import StdioJsonRpc
rpc=StdioJsonRpc([sys.executable,'-u','-c',SERVER],cwd='.',env={'PATH':'/usr/bin:/bin'})
rpc.start();process=rpc._proc
try:
 result=rpc.request('probe',{})
 try:notice=rpc.notifications.get(timeout=1)
 except queue.Empty:notice={}
 print(json.dumps({'response':result,'native_method':notice.get('method')}))
finally:
 rpc.stop()
 for stream in (process.stdin,process.stdout,process.stderr):stream.close()
""".replace('SERVER', repr(code))
        run = subprocess.run([sys.executable, '-B', '-c', program], cwd=fixture.name,
            env={'HOME': fixture.name, 'PATH': '/usr/bin:/bin', 'PYTHONPATH': module_root},
            text=True, capture_output=True, timeout=5)
        self.assertEqual(run.returncode, 0, run.stderr)
        actual = json.loads(run.stdout)
        self.assertEqual(actual['response'], {'actual_response': True}, 'BIDIRECTIONAL_ID_COLLISION_CONSUMED_NATIVE_REQUEST')
        self.assertEqual(actual['native_method'], 'item/tool/call')



class RuntimeTests(unittest.TestCase):
    def setUp(self):
        selected = patch.dict(os.environ, {'SH_DRIVER': 'codex'})
        selected.start(); self.addCleanup(selected.stop)
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.now = [1000000000.0]
        cfg = Config(session_name='root', key_id='key', secret='synthetic-only', session_full_name='root.owner', inbox_db=str(Path(self.tmp.name)/'inbox.db'))
        self.native = Native(self.tmp.name)
        self.broker = AppServerBroker(self.native); self.broker.initialize({})
        facade = self.broker.facade('', 1, 'root-thread', root=True)
        driver = CodexProtocolDriver(workdir=self.tmp.name, store_path=str(Path(self.tmp.name)/'driver.db'),
                                     shared_facade=facade, bound_thread_id='root-thread')
        driver._rpc = facade; driver._broker = self.broker; driver._initialized = True
        root = DriverAdapter(cfg, driver=driver); root._started = True
        self.runtime = SubAgentRuntime(cfg, root, 'root.owner', clock=lambda: self.now[0], watchdog=False)
        self.addCleanup(self.runtime.shutdown)
        self.runtime.on_capability(dict(sub_agents_v1=True, conn_seq=7, max_supported_depth=1, subtree_revision=0))
        self.acknowledge()
    def acknowledge(self):
        report = self.runtime.report()
        self.assertIsNotNone(report)
        self.assertTrue(self.runtime.on_ack(dict(ok=True, conn_seq=7, subtree_revision=report['subtree_revision'])))
    def child(self, name='req', policy=None):
        result = self.runtime.create(name, policy)
        return self.runtime.store.node(result['node_id'])

    def test_aa004_trusted_native_context_source_and_forgery(self):
        a, b = self.child('a'), self.child('b'); self.acknowledge()
        facade = self.runtime.adapters[a['node_id']].driver._rpc
        driver = self.runtime.adapters[a['node_id']].driver
        driver._active_turn_id = 'actual-turn'
        sources = []
        self.runtime.send_from_native = lambda source, args: sources.append(source) or {'state': 'queued'}
        params = dict(threadId=a['native_thread_id'], turnId='actual-turn', callId='valid-call', tool='dingwei_send_message', arguments={'to': 'target.owner', 'body': 'hello'})
        self.runtime.native_tool(facade, params)
        self.assertEqual(sources[0]['node_id'], a['node_id']); self.assertEqual(sources[0]['generation'], 1)
        self.assertEqual(sources[0]['full_name'], a['canonical_name']+'.root.owner')
        for forged in (dict(params, threadId=b['native_thread_id'], callId='forged-source'), dict(params, turnId='old', callId='forged-turn'),
                       dict(params, arguments=dict(params['arguments'], from_node_id=b['node_id']), callId='forged-args')):
            with self.subTest(forged=forged), self.assertRaises(PermissionError): self.runtime.native_tool(facade, forged)
        self.runtime.native_tool(facade, params)
        self.assertEqual(len(sources), 1)  # same native call is durably idempotent
        failed = dict(params, callId='uncertain-call')
        self.runtime.send_from_native = lambda source, args: (_ for _ in ()).throw(ConnectionError('synthetic lost receipt'))
        with self.assertRaises(ConnectionError): self.runtime.native_tool(facade, failed)
        self.runtime.send_from_native = lambda source, args: sources.append(source) or {'state': 'queued'}
        with self.assertRaisesRegex(PermissionError, 'unknown'): self.runtime.native_tool(facade, failed)
        self.runtime.close(a['node_id'], 'completed')
        with self.assertRaises(PermissionError): self.runtime.native_tool(facade, params)
        self.assertEqual(len(sources), 1)
        context = self.runtime.context(b['node_id'])
        self.assertEqual(context['parent'], 'root.owner')
        self.assertEqual(context['policy'], DEFAULT_POLICY)
        self.assertEqual(context['direct_children'], [])
        self.assertEqual({n['node_id'] for n in self.runtime.context('')['direct_children']}, {a['node_id'], b['node_id']})

    def test_create_repeated_unknown_native_response_never_second_thread(self):
        original = self.native.wait_request
        def uncertain(token, method, timeout=30):
            if method == 'thread/start': raise ConnectionError('lost after submission')
            return original(token, method, timeout=timeout)
        self.native.wait_request = uncertain
        with self.assertRaisesRegex(RuntimeError, 'creation_unknown'): self.runtime.create('lost')
        node = self.runtime.create('lost')
        self.assertEqual(node['creation_status'], 'submitted')
        self.assertEqual(len([m for _, _, m, _ in self.native.calls if m == 'thread/start']), 1)

    def test_aa008_ack_gate_reconnect_and_child_delivery_no_parent_fallback(self):
        a = self.child()
        with self.assertRaisesRegex(ValueError, 'registry_unavailable'): self.runtime.accept(delivery(a, 'dw-same'))
        self.acknowledge(); self.runtime.accept(delivery(a, 'dw-same'))
        self.runtime.worker_step()
        eventually(lambda: any(m == 'turn/start' for _, _, m, _ in self.native.calls))
        writes = [p for _, _, m, p in self.native.calls if m == 'turn/start']
        self.assertEqual(writes[0]['threadId'], a['native_thread_id'])
        self.assertEqual(writes[0]['input'][-1]['text'], 'original business bytes')
        self.assertIn('平台分道上下文', writes[0]['input'][0]['text'])
        self.runtime.disconnect()
        with self.assertRaisesRegex(ValueError, 'registry_unavailable'): self.runtime.accept(delivery(a, 'dw-late'))
        self.assertEqual(self.runtime.store.node(a['node_id'])['state'], 'active')
        self.assertFalse(self.runtime.on_ack(dict(ok=True, conn_seq=7, subtree_revision=2)))

    def test_aa007_completion_idle_cap_restart_and_parent_cascade(self):
        a = self.child('a', {'exit_grace': 1})
        b = self.child('b', {'exit_on_task_complete': False, 'idle_timeout': None, 'max_lifetime': None})
        self.acknowledge()
        with self.assertRaises(PermissionError): self.runtime.control(a['node_id'], 'close', target_node_id=b['node_id'])
        with self.assertRaises(PermissionError): self.runtime.control(a['node_id'], 'create', request_id='descendant')
        self.runtime.control(b['node_id'], 'task_completed')
        self.assertEqual(self.runtime.store.node(b['node_id'])['state'], 'active')
        self.runtime.control(a['node_id'], 'task_completed')
        self.assertEqual(self.runtime.store.node(a['node_id'])['state'], 'draining')
        self.now[0] += 2; self.runtime.tick()
        self.assertEqual(self.runtime.store.node(a['node_id'])['state'], 'ended')
        self.assertTrue(self.broker.healthy()); self.assertTrue(self.runtime.adapters[b['node_id']].driver.health())
        self.now[0] += 432000; self.runtime.tick()
        self.assertEqual(self.runtime.store.node(b['node_id'])['reason'], 'platform_cap')
        c = self.child('c'); self.runtime.close_parent()
        self.assertEqual(self.runtime.store.node(c['node_id'])['reason'], 'parent_closed')
        with self.assertRaisesRegex(ValueError, 'ended'): self.runtime.create('late')

    def test_aa007_idle_progress_and_pending_work_are_distinct(self):
        a = self.child('a'); self.acknowledge()
        self.runtime.accept(delivery(a, 'dw-same'))
        self.now[0] += 3601; self.runtime.tick()
        self.assertEqual(self.runtime.store.node(a['node_id'])['state'], 'active')
        b = self.child('b'); self.acknowledge()
        self.now[0] += 3500
        facade = self.runtime.adapters[b['node_id']].driver._rpc
        self.runtime.native_progress(facade, {'method': 'thread/read'})
        self.now[0] += 101; self.runtime.tick()
        self.assertEqual(self.runtime.store.node(b['node_id'])['reason'], 'idle_timeout')

    def test_aa007_restart_unknown_work_does_not_become_idle(self):
        node = self.child('restart'); self.acknowledge()
        inbox = self.runtime.inboxes[node['node_id']]
        inbox.accept('dw-restart', delivery(node, 'dw-restart'))
        inbox.claim_next(); inbox.begin_dispatch('dw-restart')
        self.runtime.store.mark_native_write(node, 'dw-restart')
        # Watchdog starts before native/adapter restoration after a crash.
        self.runtime.inboxes.clear()
        self.now[0] += 3601
        self.runtime.tick()
        self.assertEqual(self.runtime.store.node(node['node_id'])['state'], 'active')
        # Absolute lifetimes remain enforceable even with uncertain work.
        self.now[0] += 172800
        self.runtime.tick()
        self.assertEqual(self.runtime.store.node(node['node_id'])['reason'], 'max_lifetime')

    def test_aa009_return_outcomes_are_lane_and_generation_scoped(self):
        a, b = self.child('a'), self.child('b'); self.acknowledge()
        for node in (a, b):
            self.runtime.accept(delivery(node, 'dw-same'))
            self.runtime.close(node['node_id'], 'completed')
        pending = self.runtime.store.returns()
        self.assertEqual(len(pending), 2)
        self.runtime.store.return_result('dw-same', {'stage': 'hub_durable_queued', 'ok': True}, 'returned', node_id=a['node_id'], generation=1)
        self.assertEqual([r['node_id'] for r in self.runtime.store.returns()], [b['node_id']])


    def test_native_send_journal_survives_restart(self):
        binding = ('node', 1, 'native-thread', 'turn', 'call')
        self.assertIsNone(self.runtime.store.begin_native_send(*binding))
        restarted = SubAgentStore(self.runtime.store.path, 'root')
        with self.assertRaisesRegex(PermissionError, 'unknown'): restarted.begin_native_send(*binding)
        self.runtime.store.finish_native_send(*binding, {'ok': True, 'stage': 'hub_durable_queued'})
        self.assertTrue(restarted.begin_native_send(*binding)['ok'])
        self.assertIsNone(restarted.begin_native_send('node', 2, 'native-thread', 'turn', 'call'))

    def test_ended_lane_ack_restores_without_native_activation(self):
        node = self.child('ended', {'exit_grace': 1}); self.acknowledge()
        self.runtime.accept(delivery(node, 'dw-ended'))
        self.runtime.close(node['node_id'], 'completed')
        self.now[0] += 2; self.runtime.tick()
        self.assertEqual(self.runtime.store.node(node['node_id'])['state'], 'ended')
        self.runtime.adapters.pop(node['node_id']); self.runtime.inboxes.pop(node['node_id'])
        before = len(self.native.calls)
        self.runtime.worker_step()
        self.assertNotIn(node['node_id'], self.runtime.adapters)
        self.assertEqual(len(self.native.calls), before)
        phases = self.runtime.inboxes[node['node_id']].delivery_ack_chain()
        self.assertIn('failed', [r['phase'] for r in phases])
        self.assertNotIn('processed', [r['phase'] for r in phases])

    def test_broker_recovery_keeps_lane_notification_pump(self):
        node = self.child('recover'); self.acknowledge()
        driver = self.runtime.adapters[node['node_id']].driver
        self.native.live = False
        time.sleep(.6)  # exceed the pump's receive timeout during outage
        self.assertTrue(driver._event_thread.is_alive(), 'BROKER_OUTAGE_KILLED_LANE_PUMP')
        replacement = Native(self.tmp.name)
        self.broker.transport_factory = lambda: replacement
        self.broker.recover()
        replacement.notifications.put({'method': 'item/agentMessage/delta', 'params': {'threadId': node['native_thread_id'], 'turnId': 'synthetic-turn', 'itemId': 'item', 'delta': 'reconnected'}})
        eventually(lambda: any(ev.text == 'reconnected' for ev in driver._event_log))
        self.assertEqual(len([m for _, _, m, _ in replacement.calls if m == 'initialize']), 1)


    def test_slow_creation_does_not_block_parent_lifecycle_seal(self):
        entered, release = threading.Event(), threading.Event()
        original = self.native.wait_request
        outcomes = []
        def slow(token, method, timeout=30):
            if method == 'thread/start':
                entered.set(); release.wait(3)
            return original(token, method, timeout=timeout)
        self.native.wait_request = slow
        def create():
            try: outcomes.append(self.runtime.create('slow-create'))
            except Exception as exc: outcomes.append(type(exc).__name__)
        creator = threading.Thread(target=create)
        creator.start(); self.assertTrue(entered.wait(2))
        closer = threading.Thread(target=self.runtime.close_parent)
        closer.start()
        try:
            closer.join(.2)
            self.assertFalse(closer.is_alive(), 'NATIVE_CREATE_BLOCKED_LIFECYCLE_SEAL')
        finally:
            release.set(); creator.join(3); closer.join(3)
        node = self.runtime.store.nodes()[0]
        self.assertEqual(node['state'], 'draining')
        self.assertEqual(node['reason'], 'parent_closed')
        self.assertFalse(self.runtime.public_node(node)['registered'])
        self.assertEqual(len([m for _, _, m, _ in self.native.calls if m == 'thread/start']), 1)


    def test_m1_confirmation_without_lane_echo_uses_immutable_scope(self):
        a, b = self.child('a'), self.child('b'); self.acknowledge()
        helper = SubAgentConnection(); helper.sub_agents = self.runtime
        helper.cfg = self.runtime.cfg; helper.delivery_inbox = self.runtime.store.root_inbox
        self.runtime.accept(delivery(a, 'dw-ack-a'))
        meta = dict(type='delivery_ack_confirm', delivery_id='dw-ack-a', ack_protocol=2, processing_mode='driver', ack_generation=1, state='queued')
        inbox = helper.sub_agent_ack_inbox(meta)
        self.assertIs(inbox, self.runtime.inboxes[a['node_id']])
        self.assertTrue(inbox.confirm_delivery_ack(meta))
        self.assertEqual(inbox.pending_delivery_acks(), [])
        self.assertIsNone(helper.sub_agent_ack_inbox(dict(meta, ack_generation=2)))
        self.runtime.inboxes.pop(a['node_id'])
        restored = helper.sub_agent_ack_inbox(meta)
        self.assertEqual(restored.lane_id, a['node_id'])
        self.runtime.accept(delivery(b, 'dw-ack-a'))
        self.assertIsNone(helper.sub_agent_ack_inbox(meta), 'AMBIGUOUS_CONFIRMATION_SELECTED_SIBLING')
        self.assertIs(helper.sub_agent_ack_inbox(dict(meta, delivery_id='root-only')), helper.delivery_inbox)



class ConnectionTests(unittest.IsolatedAsyncioTestCase):
    async def test_native_send_full_route_transport_and_durable_receipt(self):
        from sessionhelper.outbound_receipt import OutboundReceiptBroker
        helper = SubAgentConnection()
        helper.book = types.SimpleNamespace(self_addr='root.owner#key', key_id='key')
        helper.outbound_receipts = OutboundReceiptBroker(('root.owner',))
        wires = []
        class WS:
            async def send(self, raw):
                env = json.loads(raw); wires.append(env)
                helper.outbound_receipts.receive(self, {'from': 'workpulse#key', 'to': 'root.owner#key', 'meta': {
                    'type': 'outbound_receipt', 'source_message_id': env['id'], 'nonce': env['meta']['outbound_receipt_nonce'],
                    'version': 1, 'state': 'queued', 'durable': True, 'delivery_id': 'dw-dlv_'+'1'*32}})
        ws = WS(); helper.outbound_receipts.prepare(ws); helper.outbound_receipts.negotiated = True
        source = {'node_id': 'native-bound-node', 'generation': 3, 'full_name': 'child.root.owner'}
        result = await helper.sub_agent_send_native(ws, source, {'to': 'sibling.root.owner', 'body': 'original'})
        self.assertEqual(result['stage'], 'hub_durable_queued')
        self.assertEqual(wires[0]['to'], 'sibling.root.owner#key', 'NATIVE_TARGET_TRANSPORT_KEY_MISSING')
        self.assertEqual(wires[0]['from'], 'root.owner#key')
        self.assertEqual(wires[0]['meta']['from_node_id'], 'native-bound-node')
        self.assertEqual(wires[0]['meta']['from_generation'], 3)
        self.assertEqual(wires[0]['body'], 'original')

    async def test_controls_only_trusted_connection_and_ack_scoped(self):
        helper = SubAgentConnection()
        helper.enroll_credential = object(); helper._connection_delivery_ack_source = 'workpulse#key'
        calls = []
        ws = object()
        helper.outbound_receipts = types.SimpleNamespace(connection=ws, valid_hub_address=lambda env: env.get('to') == 'root.owner#key')
        helper.sub_agents = types.SimpleNamespace(on_capability=lambda meta: calls.append(meta), on_ack=lambda meta: calls.append(meta), inboxes={})
        env = {'from': 'workpulse#key', 'to': 'root.owner#key', 'meta': {'type': 'sub_agent_capabilities'}}
        self.assertTrue(helper.sub_agent_control_frame(ws, env)); self.assertEqual(len(calls), 1)
        helper.sub_agent_control_frame(object(), env)
        helper.sub_agent_control_frame(ws, dict(env, **{'from': 'attacker#key'}))
        self.assertEqual(len(calls), 1)
        inbox = types.SimpleNamespace(lane_generation=2)
        helper.sub_agents.inboxes['a'] = inbox
        helper.delivery_inbox = object()
        self.assertIs(helper.sub_agent_ack_inbox({'target_node_id': 'a', 'lane_generation': 2}), inbox)
        self.assertIsNone(helper.sub_agent_ack_inbox({'target_node_id': 'a', 'lane_generation': 1}))

    async def test_unavailable_child_never_enters_parent_inbox(self):
        helper = SubAgentConnection(); helper.sub_agents = None
        helper.delivery_ack = lambda did, state, **extra: dict(id=did, state=state, **extra)
        sent = []
        async def send(value): sent.append(json.loads(value))
        self.assertTrue(await helper.accept_sub_agent_delivery(types.SimpleNamespace(send=send), delivery({'node_id': 'a', 'generation': 1})))
        self.assertEqual(sent[0]['reason'], 'sub_agent_unsupported')
        self.assertEqual(sent[0]['target_node_id'], 'a')


if __name__ == '__main__': unittest.main()
