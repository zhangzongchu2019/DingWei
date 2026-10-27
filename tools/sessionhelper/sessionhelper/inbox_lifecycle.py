"""Per-delivery protocol provenance and durable driver lifecycle storage.

No driver request is made here. Historical rows retain unknown provenance and
never acquire model admission/terminal evidence during migration.
"""
from __future__ import annotations

import sqlite3
from .inbox_ack import DeliveryACKOutbox, initialize_ack_cursor


LIFECYCLE_COLUMNS = """
    ack_protocol INTEGER CHECK(ack_protocol IN (1,2)),
    processing_mode TEXT,
    ack_generation INTEGER CHECK(ack_generation > 0),
    dispatch_intent_at REAL,
    request_id TEXT,
    turn_id TEXT,
    admitted_at REAL,
    processed_at REAL,
    turn_outcome TEXT CHECK(turn_outcome IN ('completed','interrupted','failed'))
"""


def migrate_lifecycle(db: sqlite3.Connection) -> None:
    initialize_ack_cursor(db)
    columns = {row['name'] for row in db.execute('PRAGMA table_info(delivery_inbox)')}
    if 'ack_protocol' not in columns:
        db.execute('ALTER TABLE delivery_inbox RENAME TO delivery_inbox_pre_lifecycle')
        db.execute(f"""CREATE TABLE delivery_inbox (
            delivery_id TEXT NOT NULL, session_name TEXT NOT NULL,
            envelope_json TEXT NOT NULL,
            state TEXT NOT NULL CHECK(state IN
                ('received','processing','admitted','admission_unknown','processed','failed','cancelled')),
            attempts INTEGER NOT NULL DEFAULT 0, error TEXT,
            received_at REAL NOT NULL, updated_at REAL NOT NULL,
            {LIFECYCLE_COLUMNS},
            PRIMARY KEY(session_name,delivery_id))""")
        db.execute("""INSERT INTO delivery_inbox
            (delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at)
            SELECT delivery_id,session_name,envelope_json,
                CASE WHEN state='processing' THEN 'admission_unknown' ELSE state END,
                attempts,error,received_at,updated_at FROM delivery_inbox_pre_lifecycle""")
        db.execute('DROP TABLE delivery_inbox_pre_lifecycle')
    db.execute("""CREATE TABLE IF NOT EXISTS delivery_ack_outbox (
        session_name TEXT NOT NULL, delivery_id TEXT NOT NULL,
        phase TEXT NOT NULL CHECK(phase IN ('queued','injected','processed','admission_unknown','failed')),
        sequence INTEGER NOT NULL,
        payload_json TEXT NOT NULL,
        created_at REAL NOT NULL,
        PRIMARY KEY(session_name,delivery_id,phase),
        UNIQUE(session_name,delivery_id,sequence))""")


def delivery_protocol(env: dict) -> tuple[int, str, int]:
    meta = env.get('meta') or {}
    protocol = meta.get('delivery_ack_protocol', 1)
    mode = meta.get('processing_mode') or 'legacy'
    generation = meta.get('ack_generation', 1)
    if type(protocol) is not int or protocol not in (1, 2):
        raise ValueError('invalid delivery ACK protocol')
    if type(generation) is not int or generation < 1:
        raise ValueError('invalid delivery ACK generation')
    if mode not in ('driver', 'desktop', 'legacy') or (protocol == 2 and mode != 'driver'):
        raise ValueError('invalid delivery ACK processing mode')
    return protocol, mode, generation



class DeliveryLifecycle(DeliveryACKOutbox):
    """Lifecycle transitions share the inbox lock/transaction with cancellation."""

    def lifecycle(self, delivery_id):
        with self._lock, self._connect() as db:
            row = db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                             (self.session_name, delivery_id)).fetchone()
            return dict(row) if row else None

    def recoverable_lifecycles(self):
        with self._lock, self._connect() as db:
            return [dict(row) for row in db.execute("""SELECT * FROM delivery_inbox
                WHERE session_name=? AND state IN ('admitted','admission_unknown')
                AND dispatch_intent_at IS NOT NULL ORDER BY received_at,delivery_id""", (self.session_name,))]

    def begin_dispatch(self, delivery_id):
        import time
        with self._lock, self._connect() as db:
            changed = db.execute("""UPDATE delivery_inbox SET dispatch_intent_at=?,request_id=delivery_id,updated_at=?
                WHERE session_name=? AND delivery_id=? AND state='processing' AND dispatch_intent_at IS NULL""",
                (time.time(), time.time(), self.session_name, delivery_id)).rowcount
            return changed == 1

    def record_admitted_before_deadline(self, delivery_id, turn_id, timeout_seconds, occurred_at=None):
        """Accepted-only and expiry have one durable, monotonic winner."""
        import time
        if not turn_id:raise ValueError('admission requires a turn ID')
        with self._lock, self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row=db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                           (self.session_name,delivery_id)).fetchone()
            if row is None or row['dispatch_intent_at'] is None:raise ValueError('admission lacks dispatch intent')
            if row['admitted_at'] is not None:
                if row['turn_id'] != turn_id:raise ValueError('admission cannot replace original turn')
                return True
            if row['state'] != 'processing':return False
            now=time.time()
            if now >= row['dispatch_intent_at'] + timeout_seconds:
                db.execute("UPDATE delivery_inbox SET state='admission_unknown',error='model_admission_unknown',updated_at=? WHERE session_name=? AND delivery_id=?",(now,self.session_name,delivery_id))
                self._lifecycle_ack(db,delivery_id,'admission_unknown')
                return False
            original=now if occurred_at is None else occurred_at
            if original > now:raise ValueError('native admission is in the future')
            db.execute("UPDATE delivery_inbox SET state='admitted',turn_id=?,admitted_at=?,error=NULL,updated_at=? WHERE session_name=? AND delivery_id=?",(turn_id,original,now,self.session_name,delivery_id))
            self._lifecycle_ack(db,delivery_id,'injected')
            return True

    def expire_admission(self, delivery_id, timeout_seconds):
        import time
        with self._lock, self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row=db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                           (self.session_name,delivery_id)).fetchone()
            if row is None or row['admitted_at'] is not None:return False
            if row['state']=='admission_unknown':return True
            now=time.time()
            if row['state']!='processing' or row['dispatch_intent_at'] is None or now < row['dispatch_intent_at']+timeout_seconds:return False
            db.execute("UPDATE delivery_inbox SET state='admission_unknown',error='model_admission_unknown',updated_at=? WHERE session_name=? AND delivery_id=?",(now,self.session_name,delivery_id))
            self._lifecycle_ack(db,delivery_id,'admission_unknown')
            return True

    def record_native_terminal(self, delivery_id, turn_id, admitted_at, processed_at, outcome):
        """Commit complete native evidence and its two ACK phases together."""
        if not turn_id or admitted_at is None or processed_at is None or processed_at < admitted_at or outcome not in ('completed','interrupted','failed'):
            raise ValueError('native terminal requires complete original evidence')
        with self._lock,self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row=db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',(self.session_name,delivery_id)).fetchone()
            if row is None or row['dispatch_intent_at'] is None or row['state'] in ('cancelled','failed'):raise ValueError('native terminal lacks dispatch intent')
            if row['admitted_at'] is not None:
                if row['turn_id']!=turn_id:raise ValueError('native terminal cannot replace original turn')
                admitted_at=row['admitted_at']
            if row['state']=='processed':
                if row['turn_outcome']!=outcome:raise ValueError('native terminal cannot replace original outcome')
                return
            if processed_at < admitted_at:raise ValueError('native terminal precedes admission')
            db.execute("UPDATE delivery_inbox SET state='admitted',turn_id=?,admitted_at=?,error=NULL,updated_at=? WHERE session_name=? AND delivery_id=?",(turn_id,admitted_at,processed_at,self.session_name,delivery_id))
            self._lifecycle_ack(db,delivery_id,'injected')
            db.execute("UPDATE delivery_inbox SET state='processed',processed_at=?,turn_outcome=?,error=NULL,updated_at=? WHERE session_name=? AND delivery_id=?",(processed_at,outcome,processed_at,self.session_name,delivery_id))
            self._lifecycle_ack(db,delivery_id,'processed')

    def record_admitted(self, delivery_id, turn_id, occurred_at=None):
        import time
        if not turn_id:
            raise ValueError('admission requires a turn ID')
        now = time.time() if occurred_at is None else occurred_at
        with self._lock, self._connect() as db:
            row = db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                             (self.session_name, delivery_id)).fetchone()
            if row is None or row['dispatch_intent_at'] is None or row['state'] in ('cancelled','failed'):
                raise ValueError('admission lacks dispatch intent')
            if row['admitted_at'] is not None:
                if row['turn_id'] != turn_id:
                    raise ValueError('admission cannot replace the original turn')
                return
            if row['state'] not in ('processing','admission_unknown'):
                raise ValueError('invalid admission predecessor')
            db.execute("""UPDATE delivery_inbox SET state='admitted',turn_id=?,admitted_at=?,error=NULL,updated_at=?
                WHERE session_name=? AND delivery_id=?""", (turn_id, now, now, self.session_name, delivery_id))
            self._lifecycle_ack(db, delivery_id, 'injected')

    def record_completed(self, delivery_id, turn_id, outcome, occurred_at=None):
        import time
        if outcome not in ('completed','interrupted','failed'):
            raise ValueError('invalid turn outcome')
        now = time.time() if occurred_at is None else occurred_at
        with self._lock, self._connect() as db:
            row = db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                             (self.session_name, delivery_id)).fetchone()
            if row is None or row['admitted_at'] is None or row['turn_id'] != turn_id or now < row['admitted_at']:
                raise ValueError('terminal requires the original admitted turn')
            if row['state'] == 'processed':
                if row['turn_outcome'] != outcome:
                    raise ValueError('terminal cannot replace original outcome')
                return
            if row['state'] != 'admitted':
                raise ValueError('invalid terminal predecessor')
            db.execute("""UPDATE delivery_inbox SET state='processed',processed_at=?,turn_outcome=?,error=NULL,updated_at=?
                WHERE session_name=? AND delivery_id=?""", (now, outcome, now, self.session_name, delivery_id))
            self._lifecycle_ack(db, delivery_id, 'processed')

    def record_admission_unknown(self, delivery_id, reason='model_admission_unknown'):
        import time
        with self._lock, self._connect() as db:
            changed = db.execute("""UPDATE delivery_inbox SET state='admission_unknown',error=?,updated_at=?
                WHERE session_name=? AND delivery_id=? AND state='processing' AND dispatch_intent_at IS NOT NULL
                AND admitted_at IS NULL""", (reason, time.time(), self.session_name, delivery_id)).rowcount
            if changed:
                self._lifecycle_ack(db, delivery_id, 'admission_unknown')

    def record_transport_unknown(self, delivery_id):
        """Release transport work without inventing or clearing model evidence."""
        import time
        with self._lock, self._connect() as db:
            changed = db.execute("""UPDATE delivery_inbox SET
                state=CASE WHEN admitted_at IS NULL THEN 'admission_unknown' ELSE state END,
                error=CASE WHEN admitted_at IS NULL THEN 'model_admission_unknown' ELSE 'turn_terminal_unknown' END,
                updated_at=? WHERE session_name=? AND delivery_id=?
                AND state IN ('processing','admitted') AND dispatch_intent_at IS NOT NULL""",
                (time.time(), self.session_name, delivery_id)).rowcount
            if changed:
                self._lifecycle_ack(db, delivery_id, 'admission_unknown')

    def record_rejected(self, delivery_id):
        """Only a driver proof of nonadmission may use this path."""
        import time
        with self._lock, self._connect() as db:
            changed = db.execute("""UPDATE delivery_inbox SET state='failed',error='session_unavailable',updated_at=?
                WHERE session_name=? AND delivery_id=? AND state IN ('processing','admission_unknown')
                AND admitted_at IS NULL AND dispatch_intent_at IS NOT NULL""", (time.time(), self.session_name, delivery_id)).rowcount
            if changed:
                self._lifecycle_ack(db, delivery_id, 'failed')


    def reject_unstarted(self, delivery_id):
        """A control/notification is known not to have made a driver request."""
        import time
        with self._lock, self._connect() as db:
            changed = db.execute("""UPDATE delivery_inbox SET state='failed',error='session_unavailable',updated_at=?
                WHERE session_name=? AND delivery_id=? AND state IN ('received','processing')
                AND dispatch_intent_at IS NULL AND admitted_at IS NULL""", (time.time(), self.session_name, delivery_id)).rowcount
            if changed:
                self._lifecycle_ack(db, delivery_id, 'failed')
