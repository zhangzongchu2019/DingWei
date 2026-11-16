"""Private ordinary-send responsibility journal; no model execution or secrets.

Unconfirmed bodies aren't evicted. Small accepted/rejected fences are retained;
capacity rejects new admissions rather than silently reopening request IDs.
"""
from __future__ import annotations
import contextlib
import hashlib
import json
import os
import re
import sqlite3
import secrets
import time
import uuid
from . import __version__


class QueueRejected(Exception):
    def __init__(self, reason, retryable=False):
        self.reason, self.retryable = reason, retryable
        super().__init__(reason)


class QueueCommitUnknown(Exception):
    pass


class OutboundQueue:
    MAX_PENDING = 1024
    MAX_BYTES = 32 << 20
    MAX_RECORDS = 65536
    MAX_AUDIT = 8192
    MAX_ATTEMPTS = 64
    WINDOW = 24 * 3600

    def __init__(self, path, *, clock=time.time):
        self.path, self.clock = os.path.abspath(path), clock
        os.makedirs(os.path.dirname(self.path), mode=0o700, exist_ok=True)
        if os.path.lexists(self.path) and (os.path.islink(self.path) or not os.path.isfile(self.path)):
            raise ValueError('outbound queue requires a private regular file')
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        os.fchmod(fd, 0o600); os.close(fd)
        with self.db() as db:
            db.executescript('''
            PRAGMA journal_mode=WAL;
            CREATE TABLE IF NOT EXISTS outbound_request(
              request_id TEXT PRIMARY KEY, sender_id TEXT NOT NULL, source_id TEXT NOT NULL UNIQUE,
              target_id TEXT NOT NULL, target TEXT NOT NULL, digest TEXT NOT NULL,
              body TEXT NOT NULL, meta TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'local_queued',
              reason TEXT NOT NULL DEFAULT '', delivery_id TEXT NOT NULL DEFAULT '',
              created REAL NOT NULL, next_attempt REAL NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
              lease_owner TEXT NOT NULL DEFAULT '', lease_until REAL NOT NULL DEFAULT 0,
              ever_attempted INTEGER NOT NULL DEFAULT 0);
            CREATE INDEX IF NOT EXISTS outbound_pending ON outbound_request(state,sender_id,next_attempt,created);
            CREATE TABLE IF NOT EXISTS outbound_event(seq INTEGER PRIMARY KEY AUTOINCREMENT,source_id TEXT NOT NULL,ts REAL NOT NULL,data TEXT NOT NULL);
            CREATE INDEX IF NOT EXISTS outbound_event_source ON outbound_event(source_id,seq);
            CREATE TABLE IF NOT EXISTS outbound_target(alias TEXT PRIMARY KEY, agent_id TEXT NOT NULL, full_name TEXT NOT NULL);
            CREATE TABLE IF NOT EXISTS outbound_peer(key TEXT PRIMARY KEY,value TEXT NOT NULL);
            ''')

    @contextlib.contextmanager
    def db(self):
        db = sqlite3.connect(self.path, timeout=2, isolation_level=None)
        db.row_factory = sqlite3.Row
        db.execute('PRAGMA synchronous=FULL')
        try: yield db
        finally: db.close()

    def update_peer(self, sender_id, targets):
        if not isinstance(sender_id, str) or not sender_id or len(sender_id) > 256:
            raise ValueError('invalid authenticated sender')
        with self.db() as db:
            db.execute('BEGIN IMMEDIATE')
            old=db.execute("SELECT value FROM outbound_peer WHERE key='sender_id'").fetchone()
            db.execute("INSERT OR REPLACE INTO outbound_peer VALUES('sender_id',?)", (sender_id,))
            if old and old[0]!=sender_id:
                db.execute("UPDATE outbound_request SET state=CASE WHEN ever_attempted=1 THEN 'unknown' ELSE 'rejected' END,reason='sender_identity_changed',body=CASE WHEN ever_attempted=0 THEN '' ELSE body END,meta=CASE WHEN ever_attempted=0 THEN '{}' ELSE meta END WHERE sender_id!=? AND state IN ('local_queued','unknown')",(sender_id,))
            if targets is None and old and old[0]==sender_id:
                db.commit();return
            db.execute('DELETE FROM outbound_target')
            if targets is None:
                db.commit();return
            for target in targets[:1024]:
                if not isinstance(target, dict): continue
                aid, full = target.get('agent_id'), target.get('full_name')
                if not isinstance(aid, str) or not 0<len(aid)<=256 or not isinstance(full, str) or not 0<len(full)<=256: continue
                aliases = target.get('aliases', [])
                if not isinstance(aliases, list): continue
                for alias in [full, *aliases[:2]]:
                    if isinstance(alias, str) and 0 < len(alias) <= 256:
                        db.execute('INSERT OR REPLACE INTO outbound_target VALUES(?,?,?)', (alias.lower(), aid, full))
            db.commit()

    def sender(self):
        with self.db() as db:
            r = db.execute("SELECT value FROM outbound_peer WHERE key='sender_id'").fetchone()
            return r[0] if r else ''

    def submit(self, request_id, target, body, meta, *, actor_id=None):
        try:
            return self._submit(request_id,target,body,meta,actor_id=actor_id)
        except (sqlite3.Error,OSError):
            # _submit promotes a failed COMMIT to QueueCommitUnknown. Earlier
            # failures are rolled back by closing the private transaction.
            raise QueueRejected('local_queue_unavailable',True) from None

    def _submit(self, request_id, target, body, meta, *, actor_id=None):
        if not isinstance(request_id, str) or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}', request_id):
            raise QueueRejected('invalid_request_id')
        if not isinstance(body, str) or not body.strip() or len(body.encode()) > (1 << 20):
            raise QueueRejected('invalid_body')
        if not isinstance(target, str) or not target or len(target) > 512 or any(c.isspace() for c in target):
            raise QueueRejected('invalid_target')
        if target.startswith(('#','@')) or len(target.split('#',1)[0].split('.'))>2:
            raise QueueRejected('outbound_recovery_unsupported')
        if not isinstance(meta,dict) or set(meta)-{'reply_to','in_reply_to','reply_policy','kind','ipc_body_amr_v1'}:
            raise QueueRejected('invalid_metadata')
        metadata = json.dumps(meta or {}, sort_keys=True, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
        digest = hashlib.sha256(json.dumps([target, body, json.loads(metadata)], ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        now = self.clock()
        with self.db() as db:
            db.execute('BEGIN IMMEDIATE')
            sender = db.execute("SELECT value FROM outbound_peer WHERE key='sender_id'").fetchone()
            if actor_id is not None and (not sender or sender[0]!=actor_id):
                raise QueueRejected('sender_identity_changed',True)
            old = db.execute('SELECT * FROM outbound_request WHERE request_id=?', (request_id,)).fetchone()
            if old:
                if not sender or old['sender_id']!=sender[0]:raise QueueRejected('sender_identity_changed')
                if old['digest'] != digest: raise QueueRejected('idempotency_conflict')
                return self.outcome(old)
            # Names are only lookup inputs. The stable ID is pinned before any
            # body write, including an admission made while the WS is offline.
            alias = target.split('#', 1)[0].lower()
            binding = db.execute('SELECT * FROM outbound_target WHERE alias=?', (alias,)).fetchone()
            if not sender or not binding: raise QueueRejected('target_identity_unavailable', True)
            count, size = db.execute("SELECT count(*),COALESCE(sum(length(CAST(body AS BLOB))+length(CAST(meta AS BLOB))),0) FROM outbound_request WHERE state IN ('local_queued','unknown')").fetchone()
            total = db.execute('SELECT count(*) FROM outbound_request').fetchone()[0]
            if count >= self.MAX_PENDING or size + len(body.encode()) + len(metadata.encode()) > self.MAX_BYTES or total >= self.MAX_RECORDS:
                raise QueueRejected('local_queue_full', True)
            sid = uuid.uuid4().hex
            db.execute('INSERT INTO outbound_request(request_id,sender_id,source_id,target_id,target,digest,body,meta,created,next_attempt) VALUES(?,?,?,?,?,?,?,?,?,?)',
                       (request_id, sender[0], sid, binding['agent_id'], binding['full_name'], digest, body, metadata, now, now))
            row = db.execute('SELECT * FROM outbound_request WHERE request_id=?', (request_id,)).fetchone()
            try:db.commit()
            except sqlite3.Error:raise QueueCommitUnknown() from None
            return self.outcome(row)

    def status(self, request_id, *, actor_id=None):
        with self.db() as db:
            r=db.execute('SELECT * FROM outbound_request WHERE request_id=?',(request_id,)).fetchone()
            if r is None:
                # A concurrent IPC transaction may still be committing. Absence
                # here is not proof that the original request never took effect.
                return {'phase':'unknown','accepted_by':'unknown','outcome':'unknown','reason':'request_not_found','request_id':request_id,'status_ref':request_id,'retryable':False}
            sender=db.execute("SELECT value FROM outbound_peer WHERE key='sender_id'").fetchone()
            expected=actor_id if actor_id is not None else (sender[0] if sender else '')
            if r['sender_id']!=expected:
                return {'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':'sender_identity_changed','request_id':request_id,'retryable':False}
            result=self.outcome(r)
            result['recent_attempts']=[json.loads(e[0]) for e in db.execute('SELECT data FROM outbound_event WHERE source_id=? ORDER BY seq DESC LIMIT 4',(r['source_id'],))]
            oldest=db.execute('SELECT min(seq),min(ts) FROM outbound_event').fetchone()
            result['audit_retention']={'maximum_events':self.MAX_AUDIT,'oldest_sequence':oldest[0],'oldest_utc_epoch':oldest[1]}
            return result

    def record(self,db,row,phase,reason,delivery_id=''):
        event={'source_message_id':row['source_id'],'sender_agent_id':row['sender_id'],'target_agent_id':row['target_id'],
               'attempt':row['attempts'],'phase':phase,'reason':reason,'delivery_id':delivery_id,'helper_version':__version__,'utc_epoch':self.clock()}
        db.execute('INSERT INTO outbound_event(source_id,ts,data) VALUES(?,?,?)',(row['source_id'],self.clock(),json.dumps(event,sort_keys=True)))
        # Diagnostic retention is bounded; business bodies and idempotency
        # fences are separate and are never evicted with these events.
        db.execute('DELETE FROM outbound_event WHERE seq <= (SELECT COALESCE(max(seq),0)-? FROM outbound_event)',(self.MAX_AUDIT,))

    @staticmethod
    def outcome(row):
        phase = row['state']
        result = {'request_id': row['request_id'], 'source_message_id': row['source_id'], 'to': row['target'],
                  'phase': phase, 'accepted_by': 'hub' if phase == 'hub_durable_queued' else ('none' if phase == 'rejected' else 'local_helper'),
                  'outcome': 'accepted' if phase in ('local_queued', 'hub_durable_queued') else ('unknown' if phase == 'unknown' else 'definitive_failure'),
                  'reason': row['reason'], 'retryable': False, 'status_ref': row['request_id'], 'attempts':row['attempts']}
        if row['delivery_id']: result['delivery_id'] = row['delivery_id']
        if phase == 'unknown': result['accepted_by'] = 'unknown'
        return result

    def claim(self, owner, sender_id):
        now = self.clock()
        with self.db() as db:
            db.execute('BEGIN IMMEDIATE')
            # Never convert an attempted-but-unconfirmed intent into proof that
            # Hub didn't accept it. Keep its body and make the stop visible.
            db.execute("UPDATE outbound_request SET body=CASE WHEN ever_attempted=0 THEN '' ELSE body END,meta=CASE WHEN ever_attempted=0 THEN '{}' ELSE meta END,reason='outbound_recovery_expired',state=CASE WHEN ever_attempted=1 THEN 'unknown' ELSE 'rejected' END,lease_owner='',lease_until=0 WHERE state IN ('local_queued','unknown') AND created<=? AND reason!='outbound_recovery_expired' AND lease_until<=?", (now - self.WINDOW,now))
            row = db.execute("SELECT * FROM outbound_request WHERE sender_id=? AND state IN ('local_queued','unknown') AND created>? AND attempts<? AND next_attempt<=? AND lease_until<=? ORDER BY created,request_id LIMIT 1", (sender_id, now - self.WINDOW, self.MAX_ATTEMPTS, now, now)).fetchone()
            if not row: db.commit(); return None
            db.execute("UPDATE outbound_request SET state='unknown',ever_attempted=1,attempts=attempts+1,lease_owner=?,lease_until=? WHERE request_id=?", (owner, now + 30, row['request_id']))
            result = dict(db.execute('SELECT * FROM outbound_request WHERE request_id=?', (row['request_id'],)).fetchone())
            self.record(db,result,'unknown','outbound_confirmation_pending')
            db.commit();return result

    def finish(self, request_id, owner, *, phase, reason='', delivery_id=''):
        if phase not in ('hub_durable_queued', 'rejected', 'unknown', 'local_queued'): raise ValueError('invalid outbound phase')
        with self.db() as db:
            db.execute('BEGIN IMMEDIATE')
            row = db.execute('SELECT * FROM outbound_request WHERE request_id=? AND lease_owner=?', (request_id, owner)).fetchone()
            if not row: return False
            if phase in ('unknown', 'local_queued') and row['attempts'] >= self.MAX_ATTEMPTS:
                phase, reason = 'unknown', 'outbound_recovery_exhausted'
            delay = min(30, 2 ** min(5, row['attempts'] - 1) + secrets.randbelow(100)/100)
            db.execute("UPDATE outbound_request SET state=?,reason=?,delivery_id=?,lease_owner='',lease_until=0,next_attempt=?,body=CASE WHEN ? IN ('hub_durable_queued','rejected') THEN '' ELSE body END,meta=CASE WHEN ? IN ('hub_durable_queued','rejected') THEN '{}' ELSE meta END WHERE request_id=?", (phase, reason, delivery_id, self.clock() + delay, phase, phase, request_id))
            self.record(db,row,phase,reason,delivery_id)
            db.commit(); return True

    def unsupported_peer(self):
        with self.db() as db:
            db.execute("UPDATE outbound_request SET body=CASE WHEN ever_attempted=0 THEN '' ELSE body END,meta=CASE WHEN ever_attempted=0 THEN '{}' ELSE meta END,state=CASE WHEN ever_attempted=1 THEN 'unknown' ELSE 'rejected' END,reason='outbound_recovery_unsupported' WHERE state IN ('local_queued','unknown') AND reason!='outbound_recovery_unsupported' AND lease_until<=?",(self.clock(),))
