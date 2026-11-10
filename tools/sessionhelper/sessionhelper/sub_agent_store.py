"""Persistent helper tree, policies, scoped inboxes and atomic per-delivery returns."""
from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
import math
import re
import sqlite3
import threading
import time
import uuid

from .inbox import DeliveryInbox
from .inbox_ack import enqueue_lifecycle_ack
from .sqlite_utils import connection


DEFAULT_POLICY = dict(exit_on_task_complete=True, idle_timeout=3600, max_lifetime=172800,
                      platform_lifetime_cap=432000, exit_grace=600)
NAME = re.compile(r'^[a-z0-9]{2,32}$')


def resolve_policy(policy=None):
    if policy is not None and not isinstance(policy, dict): raise ValueError('invalid lifecycle policy')
    if set(policy or {}) - set(DEFAULT_POLICY): raise ValueError('unknown lifecycle policy')
    result = {**DEFAULT_POLICY, **(policy or {})}
    if type(result['exit_on_task_complete']) is not bool: raise ValueError('invalid completion policy')
    for key in ('idle_timeout', 'max_lifetime', 'platform_lifetime_cap', 'exit_grace'):
        value = result[key]
        if value is None and key in ('idle_timeout', 'max_lifetime'): continue
        if type(value) not in (int, float) or not math.isfinite(value) or value <= 0: raise ValueError('invalid lifecycle duration')
    if result['platform_lifetime_cap'] > 432000 or result['exit_grace'] > 600: raise ValueError('platform lifetime bound')
    if result['idle_timeout'] is not None and result['idle_timeout'] > 3600: raise ValueError('idle lifetime bound')
    return result


def lane_session(root, lane_id, generation):
    if not lane_id: return root
    # A storage namespace, never an Agent identity or environment address.
    return root + '\x1flane:' + hashlib.sha256(lane_id.encode()).hexdigest() + ':' + str(generation)


def default_name(thread_id, occupied):
    tail = uuid.UUID(thread_id).hex
    for width in range(8, 33, 2):
        candidate = tail[-width:]
        if candidate not in occupied: return candidate
    raise ValueError('sub_agent_alias_conflict')


class ScopedDeliveryInbox(DeliveryInbox):
    def __init__(self, path, root, lane_id, generation, *, capacity=100, guard=None):
        self.lane_id, self.lane_generation = lane_id, generation
        self.root_session_name = root
        self.admission_guard = guard
        super().__init__(path, capacity, lane_session(root, lane_id, generation))


class SubAgentStore:
    def __init__(self, path, root, *, clock=time.time):
        self.path, self.root, self.clock = path, root, clock
        self._lock = threading.RLock()
        # Establish the inbox schema before adding the shared registry/outbox.
        self.root_inbox = DeliveryInbox(path, session_name=root)
        with self.transaction() as db:
            db.executescript('''
            CREATE TABLE IF NOT EXISTS helper_sub_agent_delivery_scope (
                root TEXT NOT NULL, delivery_id TEXT NOT NULL, node_id TEXT NOT NULL,
                generation INTEGER NOT NULL, ack_generation INTEGER NOT NULL,
                PRIMARY KEY(root,delivery_id,node_id,generation));
            CREATE TABLE IF NOT EXISTS helper_sub_agent_tool_send (
                root TEXT NOT NULL, lane_id TEXT NOT NULL, generation INTEGER NOT NULL,
                thread_id TEXT NOT NULL, turn_id TEXT NOT NULL, call_id TEXT NOT NULL,
                outcome_json TEXT NOT NULL DEFAULT '',
                PRIMARY KEY(root,lane_id,generation,thread_id,turn_id,call_id));
            CREATE TABLE IF NOT EXISTS helper_sub_agent_native_tools (
                root TEXT NOT NULL, thread_id TEXT NOT NULL, PRIMARY KEY(root,thread_id));
            CREATE TABLE IF NOT EXISTS helper_sub_agent_tree (
                root TEXT PRIMARY KEY, revision INTEGER NOT NULL, ack_revision INTEGER NOT NULL DEFAULT 0);
            CREATE TABLE IF NOT EXISTS helper_sub_agent (
                root TEXT NOT NULL, node_id TEXT NOT NULL, generation INTEGER NOT NULL,
                request_id TEXT NOT NULL, parent_node_id TEXT NOT NULL DEFAULT '',
                native_thread_id TEXT DEFAULT NULL, canonical_name TEXT NOT NULL DEFAULT '',
                state TEXT NOT NULL, creation_status TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '',
                policy_json TEXT NOT NULL, created_at REAL NOT NULL, last_activity REAL NOT NULL,
                draining_at REAL, changed_revision INTEGER NOT NULL DEFAULT 0,
                PRIMARY KEY(root,node_id), UNIQUE(root,request_id), UNIQUE(root,native_thread_id));
            CREATE TABLE IF NOT EXISTS helper_sub_agent_alias (
                root TEXT NOT NULL, parent_node_id TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL,
                node_id TEXT NOT NULL, PRIMARY KEY(root,parent_node_id,alias));
            CREATE TABLE IF NOT EXISTS helper_sub_agent_return (
                root TEXT NOT NULL, delivery_id TEXT NOT NULL, node_id TEXT NOT NULL,
                generation INTEGER NOT NULL, source_json TEXT NOT NULL, reason TEXT NOT NULL,
                state TEXT NOT NULL DEFAULT 'pending', outcome_json TEXT NOT NULL DEFAULT '',
                PRIMARY KEY(root,node_id,generation,delivery_id));
            ''')
            db.execute('INSERT OR IGNORE INTO helper_sub_agent_tree(root,revision) VALUES(?,1)', (root,))

    @contextmanager
    def transaction(self):
        with self._lock, connection(self.path, timeout=10, row_factory=sqlite3.Row) as db:
            db.execute('BEGIN IMMEDIATE')
            yield db

    def _bump(self, db):
        db.execute('UPDATE helper_sub_agent_tree SET revision=revision+1 WHERE root=?', (self.root,))
        return db.execute('SELECT revision FROM helper_sub_agent_tree WHERE root=?', (self.root,)).fetchone()[0]

    def begin_native_send(self, lane_id, generation, thread_id, turn_id, call_id):
        with self.transaction() as db:
            key = (self.root, lane_id, generation, thread_id, turn_id, call_id)
            prior = db.execute('SELECT outcome_json FROM helper_sub_agent_tool_send WHERE root=? AND lane_id=? AND generation=? AND thread_id=? AND turn_id=? AND call_id=?', key).fetchone()
            if prior is not None:
                if prior[0]: return json.loads(prior[0])
                raise PermissionError('native send outcome unknown; no retry')
            db.execute('INSERT INTO helper_sub_agent_tool_send(root,lane_id,generation,thread_id,turn_id,call_id) VALUES(?,?,?,?,?,?)', key)
            return None

    def finish_native_send(self, lane_id, generation, thread_id, turn_id, call_id, result):
        with self.transaction() as db:
            db.execute('UPDATE helper_sub_agent_tool_send SET outcome_json=? WHERE root=? AND lane_id=? AND generation=? AND thread_id=? AND turn_id=? AND call_id=? AND outcome_json=\'\'',
                (json.dumps(result), self.root, lane_id, generation, thread_id, turn_id, call_id))

    def record_native_tools(self, thread_id):
        with self.transaction() as db:
            db.execute('INSERT OR IGNORE INTO helper_sub_agent_native_tools VALUES(?,?)', (self.root, thread_id))

    def has_native_tools(self, thread_id):
        with self.transaction() as db:
            return bool(db.execute('SELECT 1 FROM helper_sub_agent_native_tools WHERE root=? AND thread_id=?', (self.root, thread_id)).fetchone())

    def nodes(self):
        with self._lock, connection(self.path, timeout=10, row_factory=sqlite3.Row) as db:
            return [dict(r) for r in db.execute('SELECT * FROM helper_sub_agent WHERE root=? ORDER BY created_at,node_id', (self.root,))]

    def node(self, node_id):
        return next((r for r in self.nodes() if r['node_id'] == node_id), None)

    def allocate(self, request_id, policy=None, aliases=()):
        if not isinstance(request_id, str) or not 1 <= len(request_id) <= 128: raise ValueError('invalid create request id')
        policy = resolve_policy(policy)
        aliases = list(aliases)
        if len(aliases) > 16 or len(set(aliases)) != len(aliases) or any(not NAME.fullmatch(x) for x in aliases): raise ValueError('invalid aliases')
        with self.transaction() as db:
            prior = db.execute('SELECT * FROM helper_sub_agent WHERE root=? AND request_id=?', (self.root, request_id)).fetchone()
            if prior is not None:
                if json.loads(prior['policy_json']) != policy: raise ValueError('create request policy conflict')
                prior_aliases = {r[0] for r in db.execute('SELECT alias FROM helper_sub_agent_alias WHERE root=? AND node_id=? AND alias<>?', (self.root, prior['node_id'], prior['canonical_name']))}
                if prior_aliases != set(aliases): raise ValueError('create request alias conflict')
                return dict(prior), False
            # Empty thread identifiers are NULL so multiple durable intents do
            # not collide in the native binding UNIQUE constraint.
            node_id = uuid.uuid4().hex
            now = self.clock()
            db.execute('INSERT INTO helper_sub_agent(root,node_id,generation,request_id,native_thread_id,state,creation_status,policy_json,created_at,last_activity) VALUES(?,?,1,?,NULL,\'allocating\',\'intent\',?,?,?)',
                       (self.root, node_id, request_id, json.dumps(policy, sort_keys=True), now, now))
            for alias in aliases:
                if db.execute('SELECT 1 FROM helper_sub_agent_alias WHERE root=? AND alias=?', (self.root, alias)).fetchone(): raise ValueError('sub_agent_alias_conflict')
                db.execute('INSERT INTO helper_sub_agent_alias(root,alias,node_id) VALUES(?,?,?)', (self.root, alias, node_id))
            return dict(db.execute('SELECT * FROM helper_sub_agent WHERE root=? AND node_id=?', (self.root, node_id)).fetchone()), True

    def mark_creation_submitted(self, node_id):
        with self.transaction() as db:
            return db.execute("UPDATE helper_sub_agent SET creation_status='submitted' WHERE root=? AND node_id=? AND state='allocating' AND creation_status='intent'", (self.root, node_id)).rowcount == 1

    def bind(self, node_id, thread_id):
        with self.transaction() as db:
            row = db.execute('SELECT * FROM helper_sub_agent WHERE root=? AND node_id=?', (self.root, node_id)).fetchone()
            if row is None or row['state'] not in ('allocating', 'draining', 'ended') or row['creation_status'] != 'submitted': raise ValueError('invalid creation binding')
            occupied = {r[0] for r in db.execute('SELECT alias FROM helper_sub_agent_alias WHERE root=?', (self.root,))}
            name = default_name(thread_id, occupied)
            revision = self._bump(db)
            bound_state = 'active' if row['state'] == 'allocating' else row['state']
            status = 'bound' if bound_state == 'active' else 'bound_closed'
            db.execute("UPDATE helper_sub_agent SET native_thread_id=?,canonical_name=?,state=?,creation_status=?,changed_revision=? WHERE root=? AND node_id=?", (thread_id, name, bound_state, status, revision, self.root, node_id))
            db.execute('INSERT INTO helper_sub_agent_alias(root,alias,node_id) VALUES(?,?,?)', (self.root, name, node_id))
            return name

    def snapshot(self, conn_seq, hub_revision):
        from datetime import datetime, timezone
        with self._lock, connection(self.path, timeout=10, row_factory=sqlite3.Row) as db:
            revision = db.execute('SELECT revision FROM helper_sub_agent_tree WHERE root=?', (self.root,)).fetchone()[0]
            if hub_revision > revision: raise RuntimeError('sub_agent_registry_unavailable: local tree behind Hub')
            nodes = []
            for row in db.execute("SELECT * FROM helper_sub_agent WHERE root=? AND canonical_name<>''", (self.root,)):
                aliases = [r[0] for r in db.execute('SELECT alias FROM helper_sub_agent_alias WHERE root=? AND node_id=? AND alias<>? ORDER BY alias', (self.root, row['node_id'], row['canonical_name']))]
                nodes.append(dict(node_id=row['node_id'], parent_node_id=row['parent_node_id'], canonical_name=row['canonical_name'], aliases=aliases,
                    state=row['state'], generation=row['generation'], reason=row['reason'],
                    created_at=datetime.fromtimestamp(row['created_at'], timezone.utc).isoformat(), policy=json.loads(row['policy_json'])))
            return dict(type='sub_agent_tree_report', conn_seq=conn_seq, base_revision=hub_revision, subtree_revision=revision, nodes=nodes)

    def acknowledge(self, revision):
        with self.transaction() as db:
            db.execute('UPDATE helper_sub_agent_tree SET ack_revision=MAX(ack_revision,?) WHERE root=? AND revision>=?', (revision, self.root, revision))

    def guard(self, db, node_id, generation, env=None, *, require_ack=True):
        row = db.execute('SELECT * FROM helper_sub_agent WHERE root=? AND node_id=? AND generation=?', (self.root, node_id, generation)).fetchone()
        if row is None: raise ValueError('sub_agent_generation_mismatch')
        if row['state'] != 'active': raise ValueError('sub_agent_ended')
        ack = db.execute('SELECT ack_revision FROM helper_sub_agent_tree WHERE root=?', (self.root,)).fetchone()[0]
        if require_ack and row['changed_revision'] > ack: raise ValueError('sub_agent_registry_unavailable')
        if env is not None:
            meta = env.get('meta') or {}
            if meta.get('target_node_id') != node_id or type(meta.get('lane_generation')) is not int or meta['lane_generation'] != generation: raise ValueError('sub_agent_generation_mismatch')
        return row

    def inbox(self, node, *, ready=None, capacity=100):
        node_id, generation = node['node_id'], node['generation']
        def guard(db, env):
            if ready is not None and not ready(): raise ValueError('sub_agent_registry_unavailable')
            self.guard(db, node_id, generation, env)
            meta = env['meta']
            db.execute('INSERT OR IGNORE INTO helper_sub_agent_delivery_scope VALUES(?,?,?,?,?)',
                (self.root, meta['delivery_id'], node_id, generation, meta['ack_generation']))
            db.execute('UPDATE helper_sub_agent SET last_activity=? WHERE root=? AND node_id=?', (self.clock(), self.root, node_id))
        return ScopedDeliveryInbox(self.path, self.root, node_id, generation, capacity=capacity, guard=guard)

    def confirmation_scope(self, meta):
        # M1 confirms the globally unique Hub delivery_id without echoing lane
        # fields. Resolve only this immutable routing sidecar, never LIMIT 1 on
        # receipts/evidence. Duplicated identities are an explicit ambiguity.
        with self.transaction() as db:
            rows = db.execute('SELECT node_id,generation,ack_generation FROM helper_sub_agent_delivery_scope WHERE root=? AND delivery_id=?',
                (self.root, meta.get('delivery_id'))).fetchall()
            if not rows: return False, None
            if len(rows) != 1 or type(meta.get('ack_generation')) is not int or rows[0]['ack_generation'] != meta['ack_generation']:
                return True, None
            root_collision = db.execute('SELECT 1 FROM delivery_inbox WHERE session_name=? AND delivery_id=?', (self.root, meta['delivery_id'])).fetchone()
            if root_collision: return True, None
            return True, dict(rows[0])

    def mark_native_write(self, node, delivery_id):
        with self.transaction() as db:
            self.guard(db, node['node_id'], node['generation'])
            session = lane_session(self.root, node['node_id'], node['generation'])
            changed = db.execute('UPDATE delivery_inbox SET native_dispatch_at=? WHERE session_name=? AND delivery_id=? AND state IN (\'processing\',\'admission_unknown\') AND native_dispatch_at IS NULL', (self.clock(), session, delivery_id)).rowcount
            if not changed: raise ValueError('sub_agent_dispatch_not_owned')

    def touch(self, node_id):
        with self.transaction() as db:
            db.execute("UPDATE helper_sub_agent SET last_activity=? WHERE root=? AND node_id=? AND state='active'", (self.clock(), self.root, node_id))

    def seal(self, node_id, reason):
        with self.transaction() as db:
            row = db.execute('SELECT * FROM helper_sub_agent WHERE root=? AND node_id=?', (self.root, node_id)).fetchone()
            if row is None: raise ValueError('sub_agent_not_found')
            if row['state'] in ('draining', 'ended'): return False
            revision = self._bump(db)
            db.execute("UPDATE helper_sub_agent SET state='draining',reason=?,draining_at=?,changed_revision=? WHERE root=? AND node_id=?", (reason, self.clock(), revision, self.root, node_id))
            session = lane_session(self.root, node_id, row['generation'])
            pending = db.execute("SELECT * FROM delivery_inbox WHERE session_name=? AND state IN ('received','processing','admission_unknown') AND native_dispatch_at IS NULL AND admitted_at IS NULL", (session,)).fetchall()
            for item in pending:
                env = json.loads(item['envelope_json'])
                source = {k: (env.get('meta') or {}).get(k) for k in ('from_full_name', 'from_node_id', 'from_generation')}
                source['address'] = env.get('from', '')
                db.execute('INSERT OR IGNORE INTO helper_sub_agent_return(root,delivery_id,node_id,generation,source_json,reason) VALUES(?,?,?,?,?,?)', (self.root, item['delivery_id'], node_id, row['generation'], json.dumps(source), reason))
                db.execute("UPDATE delivery_inbox SET state='failed',error='sub_agent_ended',updated_at=? WHERE session_name=? AND delivery_id=?", (self.clock(), session, item['delivery_id']))
                enqueue_lifecycle_ack(db, session, item['delivery_id'], 'failed')
            db.execute("UPDATE delivery_inbox SET state='admission_unknown',error='sub_agent_exit_pending',updated_at=? WHERE session_name=? AND native_dispatch_at IS NOT NULL AND state IN ('processing','admitted')", (self.clock(), session))
            return True

    def end(self, node_id):
        with self.transaction() as db:
            if not db.execute("SELECT 1 FROM helper_sub_agent WHERE root=? AND node_id=? AND state='draining'", (self.root, node_id)).fetchone(): return
            revision = self._bump(db)
            db.execute("UPDATE helper_sub_agent SET state='ended',changed_revision=? WHERE root=? AND node_id=? AND state='draining'", (revision, self.root, node_id))

    def returns(self):
        with self._lock, connection(self.path, timeout=10, row_factory=sqlite3.Row) as db:
            return [dict(r) for r in db.execute("SELECT * FROM helper_sub_agent_return WHERE root=? AND state='pending' ORDER BY delivery_id", (self.root,))]

    def return_result(self, delivery_id, outcome, state, *, node_id, generation):
        # Uncertain outcomes are durable and never automatically retried.
        with self.transaction() as db:
            db.execute('UPDATE helper_sub_agent_return SET state=?,outcome_json=? WHERE root=? AND delivery_id=? AND node_id=? AND generation=? AND state IN (\'pending\',\'uncertain\')', (state, json.dumps(outcome), self.root, delivery_id, node_id, generation))


    def prepare_rollback(self):
        """Offline/feature-off: seal each lane using the normal atomic outbox path.

        Caller must stop dispatch writers first. Pending returns and unknown
        execution remain durable; downgrade is blocked until pending returns settle.
        """
        for node in self.nodes():
            self.seal(node['node_id'], 'operator_closed')
        with self._lock, connection(self.path, timeout=10, row_factory=sqlite3.Row) as db:
            pending=db.execute("SELECT count(*) FROM helper_sub_agent_return WHERE root=? AND state IN ('pending','uncertain')", (self.root,)).fetchone()[0]
            unknown=0
            for node in self.nodes():
                session=lane_session(self.root,node['node_id'],node['generation'])
                unknown += db.execute("SELECT count(*) FROM delivery_inbox WHERE session_name=? AND state IN ('processing','admitted','admission_unknown')", (session,)).fetchone()[0]
            return dict(returns_pending=pending, unknown_isolated=unknown, downgrade_ready=pending==0)


    @staticmethod
    def existing_tree(path, root):
        """Read-only detection before feature-off maintenance; no DB creation."""
        from pathlib import Path
        if not Path(path).is_file(): return None
        with connection(Path(path).resolve().as_uri()+'?mode=ro', uri=True) as db:
            if not db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='helper_sub_agent'").fetchone(): return None
            if not db.execute('SELECT 1 FROM helper_sub_agent WHERE root=? LIMIT 1',(root,)).fetchone(): return None
        store=SubAgentStore(path,root)
        store.prepare_rollback()
        return store
