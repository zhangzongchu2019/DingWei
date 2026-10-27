"""Observational delivery journal. Never schedules, acknowledges or replays input."""
import threading
import time
import uuid

from .sqlite_utils import connection


STAGES = ('disk_received', 'adapter_queued', 'dispatching', 'model_admitted', 'terminal')


class InboxProgress:
    def __init__(self, path, session, *, clock=time.time, monotonic=time.monotonic):
        self.path, self.session = path, session
        self.clock, self.monotonic = clock, monotonic
        self.instance, self.seq = uuid.uuid4().hex, 0
        self.lock = threading.RLock()
        self.arrivals = {}
        self.unavailable = False
        self.changed = threading.Event()
        with connection(path) as db:
            db.execute('''CREATE TABLE IF NOT EXISTS delivery_progress (
                session_name TEXT NOT NULL, delivery_id TEXT NOT NULL,
                stage TEXT NOT NULL, received_at REAL NOT NULL, updated_at REAL NOT NULL,
                unknown_instance TEXT,
                PRIMARY KEY(session_name,delivery_id))''')
            if 'unknown_instance' not in {row[1] for row in db.execute('PRAGMA table_info(delivery_progress)')}:
                db.execute('ALTER TABLE delivery_progress ADD COLUMN unknown_instance TEXT')
            db.execute('CREATE INDEX IF NOT EXISTS delivery_progress_updated ON delivery_progress(session_name,updated_at DESC,delivery_id)')
            db.execute('CREATE INDEX IF NOT EXISTS delivery_progress_stage ON delivery_progress(session_name,stage,received_at)')
            # An old processed ACK is not model admission. Lost in-memory queue
            # and dispatch state cannot be reconstructed as an actionable input.
            # Only uncertainty observed/recovered by this instance contributes
            # to its live counter. Prior uncertainty remains available per ID.
            db.execute("UPDATE delivery_progress SET stage='unknown',unknown_instance=? WHERE session_name=? AND stage IN ('adapter_queued','dispatching')", (self.instance, session))
            db.execute('''INSERT OR IGNORE INTO delivery_progress
                (session_name,delivery_id,stage,received_at,updated_at)
                SELECT session_name,delivery_id,'unknown',received_at,updated_at
                FROM delivery_inbox WHERE session_name=?''', (session,))
            # Existing unprocessed inbox work is a recovery observation now,
            # unlike historical processed/failed/cancelled uncertainty.
            db.execute('''UPDATE delivery_progress SET unknown_instance=?
                WHERE session_name=? AND stage='unknown' AND delivery_id IN (
                    SELECT delivery_id FROM delivery_inbox WHERE session_name=?
                    AND state IN ('received','processing','admission_unknown'))''', (self.instance, session, session))

    def accepted(self, db, delivery_id, received_at):
        # Caller owns the inbox transaction: disk receipt and progress are atomic.
        db.execute("INSERT OR IGNORE INTO delivery_progress (session_name,delivery_id,stage,received_at,updated_at) VALUES(?,?,'disk_received',?,?)",
                   (self.session, delivery_id, received_at, received_at))
        with self.lock:
            self.arrivals.setdefault(delivery_id, self.monotonic())
            self.changed.set()

    def mark(self, delivery_id, stage):
        if stage not in STAGES + ('unknown', 'failed', 'cancelled'):
            raise ValueError('invalid progress stage')
        with self.lock, connection(self.path) as db:
            row = db.execute('SELECT stage FROM delivery_progress WHERE session_name=? AND delivery_id=?',
                             (self.session, delivery_id)).fetchone()
            if row is None:
                return  # Non-durable/local human commands have no inbox receipt.
            old = row[0]
            if old in ('terminal', 'failed', 'cancelled'):
                return
            if old == 'model_admitted' and stage not in ('terminal', 'failed'):
                return
            if old in STAGES and stage in STAGES and STAGES.index(stage) < STAGES.index(old):
                return
            if old == 'unknown' and stage in STAGES[:3]:
                return
            db.execute('UPDATE delivery_progress SET stage=?,updated_at=?,unknown_instance=? WHERE session_name=? AND delivery_id=?',
                       (stage, self.clock(), self.instance if stage == 'unknown' else None, self.session, delivery_id))
            self.changed.set()
            if stage not in STAGES[:3]:
                self.arrivals.pop(delivery_id, None)

    def snapshot(self):
        with self.lock, connection(self.path) as db:
            pending = db.execute("SELECT delivery_id,received_at FROM delivery_progress WHERE session_name=? AND stage IN ('disk_received','adapter_queued','dispatching')", (self.session,)).fetchall()
            count = len(pending)
            unknown = db.execute("SELECT count(*) FROM delivery_progress WHERE session_name=? AND stage='unknown' AND unknown_instance=?", (self.session, self.instance)).fetchone()[0]
            rows = db.execute("SELECT delivery_id,stage FROM delivery_progress WHERE session_name=? ORDER BY CASE WHEN stage IN ('disk_received','adapter_queued','dispatching') THEN 0 ELSE 1 END,updated_at DESC,delivery_id LIMIT 256", (self.session,)).fetchall()
            uncertain = False
            age = None
            now, mono_now = self.clock(), self.monotonic()
            for delivery_id, received_at in pending:
                mono = self.arrivals.get(delivery_id)
                # Choose the greatest elapsed wait, not the smallest wall stamp:
                # a later arrival after a clock rollback must not replace oldest.
                elapsed = now - received_at if mono is None else mono_now - mono
                uncertain |= mono is None or elapsed < 0 or elapsed > 30 * 86400
                wait = int(min(30 * 86400, max(0, elapsed)) * 1000)
                age = wait if age is None else max(age, wait)
            self.seq += 1
            return {'available': not self.unavailable, 'schema': 'inbox-progress/v1', 'instance': self.instance, 'seq': self.seq,
                    'disk_not_admitted_count': count, 'dispatch_unknown_count': unknown,
                    'oldest_wait_ms': age, 'clock_uncertain': uncertain,
                    'deliveries': {row[0]: row[1] for row in rows}}


def observe(journal, delivery_id, stage):
    # Telemetry failure must not alter the established delivery/ACK contract.
    if journal is not None:
        try:
            journal.mark(delivery_id, stage)
        except Exception:
            journal.unavailable = True
            journal.changed.set()
            print('[inbox-progress] observation unavailable', flush=True)
