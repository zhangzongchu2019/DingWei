"""Durable, idempotent inbox for reliable Hub deliveries."""

from __future__ import annotations

import json
import os
import sqlite3
from contextlib import AbstractContextManager
import sys
import threading
import time

from .sqlite_utils import connection as sqlite_connection
from .inbox_lifecycle import migrate_lifecycle, delivery_protocol, DeliveryLifecycle


class DeliveryInbox(DeliveryLifecycle):
    def __init__(
        self,
        path: str,
        capacity: int = 100,
        session_name: str = "default",
        session_full_name: str = "",
        registered_session_name: str = "",
        key_id: str = "",
    ):
        self.lane_id=getattr(self,'lane_id','')
        self.lane_generation=getattr(self,'lane_generation',1)
        self.admission_guard=getattr(self,'admission_guard',None)
        self.path = os.path.expanduser(path)
        self.capacity = max(1, capacity)
        self.session_name = (session_name or "default").strip() or "default"
        self.session_full_name = (session_full_name or "").strip()
        self.registered_session_name = (registered_session_name or "").strip()
        self.key_tail = (key_id or "").strip()[-4:].lower()
        self._lock = threading.Lock()
        os.makedirs(os.path.dirname(self.path) or ".", exist_ok=True)
        self._initialize()
        self._import_legacy_shared_inbox()
        from .inbox_progress import InboxProgress
        self.progress = InboxProgress(self.path, self.session_name)

    def _connect(self) -> AbstractContextManager[sqlite3.Connection]:
        return sqlite_connection(self.path, timeout=5, row_factory=sqlite3.Row)

    def _initialize(self) -> None:
        with self._lock, self._connect() as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute("BEGIN IMMEDIATE")
            db.execute(
                """CREATE TABLE IF NOT EXISTS delivery_inbox (
                    delivery_id TEXT NOT NULL,
                    session_name TEXT NOT NULL,
                    envelope_json TEXT NOT NULL,
                    state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed','cancelled')),
                    attempts INTEGER NOT NULL DEFAULT 0,
                    error TEXT,
                    received_at REAL NOT NULL,
                    updated_at REAL NOT NULL,
                    PRIMARY KEY(session_name, delivery_id)
                )"""
            )
            self._migrate_schema(db)
            self._migrate_state_check(db)
            migrate_lifecycle(db)
            columns={r['name'] for r in db.execute('PRAGMA table_info(delivery_inbox)')}
            for name,definition in (('lane_id',"TEXT NOT NULL DEFAULT ''"),('lane_generation','INTEGER NOT NULL DEFAULT 1'),('native_dispatch_at','REAL')):
                if name not in columns:db.execute(f'ALTER TABLE delivery_inbox ADD COLUMN {name} {definition}')
            db.execute('CREATE INDEX IF NOT EXISTS idx_inbox_lane_generation ON delivery_inbox(lane_id,lane_generation,delivery_id)')
            db.execute("""CREATE TABLE IF NOT EXISTS delivery_inbox_read (
                session_name TEXT NOT NULL, delivery_id TEXT NOT NULL, read_at REAL NOT NULL,
                PRIMARY KEY(session_name,delivery_id))""")
            db.execute(
                "CREATE INDEX IF NOT EXISTS idx_delivery_inbox_fifo ON delivery_inbox(session_name,state,received_at)"
            )
            db.execute(
                """UPDATE delivery_inbox SET
                    state=CASE WHEN dispatch_intent_at IS NULL AND ack_protocol IS NOT NULL
                               THEN 'received' ELSE 'admission_unknown' END,
                    updated_at=? WHERE session_name=? AND state='processing'""",
                (time.time(), self.session_name),
            )
            rows = db.execute("SELECT * FROM delivery_inbox WHERE session_name=? AND ack_protocol IS NOT NULL AND state<>'cancelled'", (self.session_name,)).fetchall()
            for row in rows:
                self._lifecycle_ack(db, row['delivery_id'], 'queued')
                if row['admitted_at'] is not None:
                    self._lifecycle_ack(db, row['delivery_id'], 'injected')
                    if row['processed_at'] is not None and row['turn_outcome']:
                        self._lifecycle_ack(db, row['delivery_id'], 'processed')
                elif row['state'] == 'admission_unknown' and row['dispatch_intent_at'] is not None:
                    self._lifecycle_ack(db, row['delivery_id'], 'admission_unknown')
                elif row['state'] == 'failed' and row['error'] == 'session_unavailable':
                    self._lifecycle_ack(db, row['delivery_id'], 'failed')

    def _migrate_schema(self, db: sqlite3.Connection) -> None:
        columns = db.execute("PRAGMA table_info(delivery_inbox)").fetchall()
        column_names = {str(row["name"]) for row in columns}
        primary_key = [str(row["name"]) for row in sorted(columns, key=lambda row: int(row["pk"])) if int(row["pk"])]
        if "session_name" in column_names and primary_key == ["session_name", "delivery_id"]:
            db.execute(
                "UPDATE delivery_inbox SET session_name=? WHERE session_name IS NULL OR session_name=''",
                (self.session_name,),
            )
            return

        session_expr = "COALESCE(NULLIF(session_name,''), ?)" if "session_name" in column_names else "?"
        db.execute("ALTER TABLE delivery_inbox RENAME TO delivery_inbox_legacy")
        db.execute(
            """CREATE TABLE delivery_inbox (
                delivery_id TEXT NOT NULL,
                session_name TEXT NOT NULL,
                envelope_json TEXT NOT NULL,
                state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed','cancelled')),
                attempts INTEGER NOT NULL DEFAULT 0,
                error TEXT,
                received_at REAL NOT NULL,
                updated_at REAL NOT NULL,
                PRIMARY KEY(session_name, delivery_id)
            )"""
        )
        db.execute(
            f"""INSERT OR IGNORE INTO delivery_inbox(
                    delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at
                )
                SELECT delivery_id,{session_expr},envelope_json,state,attempts,error,received_at,updated_at
                FROM delivery_inbox_legacy""",
            (self.session_name,),
        )
        db.execute("DROP TABLE delivery_inbox_legacy")

    def _migrate_state_check(self, db: sqlite3.Connection) -> None:
        """Rebuild the table if its state CHECK predates the 'cancelled' state.

        SQLite CHECK constraints are immutable, so an existing DB created before the
        真删除 chain would REJECT an UPDATE to state='cancelled'. Fresh DBs already
        CREATE with 'cancelled' (this no-ops); only older DBs are rebuilt. Mirrors the
        rename→create→copy→drop shape of _migrate_schema. (N1 lesson: fresh deploy vs
        already-seeded DB — the fresh path can't be the only one that works.)"""
        row = db.execute(
            "SELECT sql FROM sqlite_master WHERE type='table' AND name='delivery_inbox'"
        ).fetchone()
        if row is None or "'cancelled'" in (row["sql"] or ""):
            return
        db.execute("ALTER TABLE delivery_inbox RENAME TO delivery_inbox_precancel")
        db.execute(
            """CREATE TABLE delivery_inbox (
                delivery_id TEXT NOT NULL,
                session_name TEXT NOT NULL,
                envelope_json TEXT NOT NULL,
                state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed','cancelled')),
                attempts INTEGER NOT NULL DEFAULT 0,
                error TEXT,
                received_at REAL NOT NULL,
                updated_at REAL NOT NULL,
                PRIMARY KEY(session_name, delivery_id)
            )"""
        )
        db.execute(
            """INSERT OR IGNORE INTO delivery_inbox(
                    delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at)
               SELECT delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at
               FROM delivery_inbox_precancel"""
        )
        db.execute("DROP TABLE delivery_inbox_precancel")

    def _legacy_shared_path(self) -> str:
        return os.path.join(os.path.dirname(self.path) or ".", "sessionhelper_inbox.db")

    def _import_legacy_shared_inbox(self) -> None:
        if self.lane_id:return
        legacy_path = self._legacy_shared_path()
        if os.path.abspath(legacy_path) == os.path.abspath(self.path) or not os.path.exists(legacy_path):
            return
        if os.path.getsize(legacy_path) == 0:
            return
        try:
            rows = self._pending_legacy_rows(legacy_path)
            if not rows:
                return
            imported = 0
            with self._lock, self._connect() as db:
                for row in rows:
                    cursor = db.execute(
                        """INSERT OR IGNORE INTO delivery_inbox(
                                delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at
                            ) VALUES(?,?,?,?,?,?,?,?)""",
                        (
                            row["delivery_id"],
                            self.session_name,
                            row["envelope_json"],
                            "admission_unknown" if row["state"] == "processing" else "received",
                            row["attempts"],
                            row["error"],
                            row["received_at"],
                            time.time(),
                        ),
                    )
                    imported += cursor.rowcount
            if imported:
                print(
                    f"[delivery][legacy_inbox] imported {imported}/{len(rows)} pending deliveries "
                    f"from {legacy_path} into {self.path} session={self.session_name}",
                    file=sys.stderr,
                    flush=True,
                )
        except Exception as exc:
            print(
                f"[delivery][legacy_inbox][WARN] failed to import pending deliveries "
                f"from {legacy_path} into {self.path} session={self.session_name}: {exc}",
                file=sys.stderr,
                flush=True,
            )

    def _pending_legacy_rows(self, legacy_path: str) -> list[sqlite3.Row]:
        with sqlite_connection(f"file:{legacy_path}?mode=ro", uri=True, timeout=5) as db:
            db.row_factory = sqlite3.Row
            table = db.execute(
                "SELECT name FROM sqlite_master WHERE type='table' AND name='delivery_inbox'"
            ).fetchone()
            if table is None:
                return []
            columns = db.execute("PRAGMA table_info(delivery_inbox)").fetchall()
            column_names = {str(row["name"]) for row in columns}
            required = {"delivery_id", "envelope_json", "state", "attempts", "error", "received_at"}
            missing = required - column_names
            if missing:
                raise RuntimeError(f"legacy delivery_inbox missing columns: {','.join(sorted(missing))}")
            if "session_name" in column_names:
                rows = list(
                    db.execute(
                        """SELECT delivery_id,session_name,envelope_json,state,attempts,error,received_at
                           FROM delivery_inbox
                           WHERE state IN ('received','processing')
                             AND (session_name=? OR session_name IS NULL OR session_name='')
                           ORDER BY received_at,delivery_id""",
                        (self.session_name,),
                    )
                )
                return self._filter_sessioned_legacy_rows(rows)
            rows = list(
                db.execute(
                    """SELECT delivery_id,envelope_json,state,attempts,error,received_at
                       FROM delivery_inbox
                       WHERE state IN ('received','processing')
                       ORDER BY received_at,delivery_id"""
                )
            )
            return self._filter_legacy_rows_for_session(rows)

    def _filter_legacy_rows_for_session(self, rows: list[sqlite3.Row]) -> list[sqlite3.Row]:
        out = []
        missing = 0
        mismatched = []
        for row in rows:
            try:
                env = json.loads(row["envelope_json"])
            except Exception:
                missing += 1
                continue
            to = str(env.get("to") or "").strip()
            if not to:
                missing += 1
                continue
            session = to.split("#", 1)[0]
            if self._target_matches_session(session):
                out.append(row)
            else:
                mismatched.append(to)
        if missing:
            print(
                f"[delivery][legacy_inbox][WARN] skipped {missing} pending deliveries without parseable target "
                f"session={self.session_name}",
                file=sys.stderr,
                flush=True,
            )
        if mismatched:
            print(
                f"[delivery][legacy_inbox][WARN] skipped {len(mismatched)} pending deliveries for other sessions "
                f"session={self.session_name} targets={sorted(self._target_sessions())} to={mismatched[:5]}",
                file=sys.stderr,
                flush=True,
            )
        return out

    def _target_sessions(self) -> set[str]:
        out = {self.session_name}
        if self.session_full_name:
            out.add(self.session_full_name)
        if self.registered_session_name:
            out.add(self.registered_session_name)
        return out

    def _target_matches_session(self, session: str) -> bool:
        session = (session or "").strip()
        if session in self._target_sessions():
            return True
        parts = session.split("-")
        return bool(self.key_tail) and len(parts) == 3 and parts[1] == self.session_name and parts[2].lower() == self.key_tail

    def _filter_sessioned_legacy_rows(self, rows: list[sqlite3.Row]) -> list[sqlite3.Row]:
        out = []
        empty_session_rows = []
        for row in rows:
            if str(row["session_name"] or "").strip() == self.session_name:
                out.append(row)
            else:
                empty_session_rows.append(row)
        out.extend(self._filter_legacy_rows_for_session(empty_session_rows))
        return out

    def accept(self, delivery_id: str, env: dict, retry_failed: bool = False) -> tuple[str, int, int]:
        """Persist a delivery and return (ack_state, queue_position, queue_depth)."""
        now = time.time()
        payload = json.dumps(env, ensure_ascii=False, separators=(",", ":"))
        protocol, mode, generation = delivery_protocol(env)
        with self._lock, self.progress.lock, self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            if self.admission_guard is not None:self.admission_guard(db,env)
            row = db.execute(
                "SELECT state,received_at,ack_protocol,processing_mode,ack_generation,dispatch_intent_at FROM delivery_inbox WHERE delivery_id=? AND session_name=?",
                (delivery_id, self.session_name),
            ).fetchone()
            if row is None:
                depth = db.execute(
                    "SELECT COUNT(*) FROM delivery_inbox WHERE session_name=? AND state IN ('received','processing')",
                    (self.session_name,),
                ).fetchone()[0]
                if depth >= self.capacity:
                    raise OverflowError("queue_full")
                db.execute(
                    """INSERT INTO delivery_inbox(delivery_id,session_name,envelope_json,state,received_at,updated_at,
                        ack_protocol,processing_mode,ack_generation,lane_id,lane_generation) VALUES(?,?,?,'received',?,?,?,?,?,?,?)""",
                    (delivery_id, self.session_name, payload, now, now, protocol, mode, generation,self.lane_id,self.lane_generation),
                )
                self.progress.accepted(db, delivery_id, now)
                self._lifecycle_ack(db, delivery_id, 'queued')
                received_at = now
                state = "received"
            else:
                state, received_at = row["state"], row["received_at"]
                # The first persisted protocol owns duplicates, including after
                # helper upgrades. Never relabel historical rows from new wire.
                if row['ack_protocol'] is not None and (row['ack_protocol'], row['processing_mode'], row['ack_generation']) != (protocol, mode, generation):
                    raise ValueError('delivery ACK provenance changed')
                if state == "failed" and retry_failed and row['dispatch_intent_at'] is None:
                    db.execute(
                        """UPDATE delivery_inbox
                           SET envelope_json=?,state='received',error=NULL,updated_at=?
                           WHERE delivery_id=? AND session_name=? AND state='failed'""",
                        (payload, now, delivery_id, self.session_name),
                    )
                    state = "received"
            position = db.execute(
                """SELECT COUNT(*) FROM delivery_inbox
                   WHERE session_name=? AND state IN ('received','processing') AND (received_at<? OR (received_at=? AND delivery_id<?))""",
                (self.session_name, received_at, received_at, delivery_id),
            ).fetchone()[0]
            depth = db.execute(
                "SELECT COUNT(*) FROM delivery_inbox WHERE session_name=? AND state IN ('received','processing')",
                (self.session_name,),
            ).fetchone()[0]
        # processing is an internal state; the wire protocol maps it to delivered.
        ack_state = "processed" if state == "processed" else "failed" if state == "failed" else "delivered"
        return ack_state, int(position), int(depth)

    def claim_next(self) -> tuple[str, dict] | None:
        now = time.time()
        with self._lock, self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                """SELECT delivery_id,envelope_json FROM delivery_inbox
                   WHERE session_name=? AND state='received'
                   ORDER BY received_at,delivery_id LIMIT 1""",
                (self.session_name,),
            ).fetchone()
            if row is None:
                db.commit()
                return None
            changed = db.execute(
                """UPDATE delivery_inbox
                   SET state='processing',attempts=attempts+1,updated_at=?
                   WHERE delivery_id=? AND session_name=? AND state='received'""",
                (now, row["delivery_id"], self.session_name),
            ).rowcount
            db.commit()
            if changed != 1:
                return None
            return str(row["delivery_id"]), json.loads(row["envelope_json"])

    def finish(self, delivery_id: str, error: str = "") -> None:
        state = "failed" if error else "processed"
        with self._lock, self._connect() as db:
            db.execute(
                "UPDATE delivery_inbox SET state=?,error=?,updated_at=? WHERE delivery_id=? AND session_name=?",
                (state, error or None, time.time(), delivery_id, self.session_name),
            )

        if error:
            from .inbox_progress import observe
            observe(self.progress, delivery_id, "unknown")

    def release(self, delivery_id: str) -> None:
        with self._lock, self._connect() as db:
            db.execute(
                """UPDATE delivery_inbox
                   SET state='received',updated_at=?
                   WHERE delivery_id=? AND session_name=? AND state='processing' AND dispatch_intent_at IS NULL""",
                (time.time(), delivery_id, self.session_name),
            )

    def cancel(self, message_id: str, delivery_id: str) -> dict:
        """走甲最小·真删除链末段 (dev55 spec 2026-07-21): pre-injection cancel.

        双键 G-ADDR (message_id + delivery_id;注入前无 turn_id)。仅 'received' 态可
        原子撤到 'cancelled';'processing'/'processed'/'failed'(已注入/已处理/终态)
        拒 not_cancelable(避免假删除——已注入的不能撤);未知/迟到/错地址不回退、
        不新建条目。返回 {result, state} 供上层组 cancelled ACK(带双键)。result:
          'cancelled'      —— was 'received' → cancelled(或幂等再撤 already cancelled)
          'not_cancelable' —— processing/processed/failed(已注入或终态,不可撤)
          'not_found'      —— 无此 (session, delivery_id)(迟到/错地址,不回退)

        以 delivery_id+session_name(PK)原子定位;message_id 随 ACK 回传供 hub CAS,
        helper 侧不据 message_id 二次判定(delivery_id 已唯一)。竞态:'received' 被
        claim_next 抢占后本次 UPDATE 影响 0 行 → 视为 not_cancelable(已进注入)。"""
        now = time.time()
        with self._lock, self.progress.lock, self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT state FROM delivery_inbox WHERE delivery_id=? AND session_name=?",
                (delivery_id, self.session_name),
            ).fetchone()
            if row is None:
                db.commit()
                return {"result": "not_found", "state": ""}
            state = row["state"]
            if state == "cancelled":
                db.commit()
                return {"result": "cancelled", "state": "cancelled"}  # 幂等
            if state != "received":
                db.commit()
                return {"result": "not_cancelable", "state": state}
            changed = db.execute(
                """UPDATE delivery_inbox SET state='cancelled',updated_at=?
                   WHERE delivery_id=? AND session_name=? AND state='received'""",
                (now, delivery_id, self.session_name),
            ).rowcount
            if changed == 1:
                db.execute("DELETE FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=?", (self.session_name, delivery_id))
                db.execute("UPDATE delivery_progress SET stage='cancelled',updated_at=? WHERE session_name=? AND delivery_id=?", (now, self.session_name, delivery_id))
                self.progress.arrivals.pop(delivery_id, None)
                self.progress.changed.set()
            db.commit()
            if changed != 1:  # 竞态:received 已被 claim_next 抢占 → 已进注入
                return {"result": "not_cancelable", "state": "processing"}
            return {"result": "cancelled", "state": "cancelled"}

    def accept_notification(self, delivery_id, env):
        """Persist a non-executable notification atomically; never worker-claim it."""
        if not delivery_id or (env.get('meta') or {}).get('kind') not in ('task_assigned','task_blocker_ready'):
            raise ValueError('invalid task notification')
        now = time.time()
        with self._lock, self._connect() as db:
            db.execute("""INSERT OR IGNORE INTO delivery_inbox(delivery_id,session_name,envelope_json,state,received_at,updated_at)
                VALUES(?,?,?,'processed',?,?)""", (delivery_id,self.session_name,json.dumps(env),now,now))

    def request(self, args):
        """Driver MCP inspection does not consume, pause or ACK Hub deliveries."""
        op = args.get('operation')
        if op not in ('status','list','read','ack'):
            return {'ok':False, 'error':'mode_operation_unsupported', 'mode':'driver'}
        with self._lock, self._connect() as db:
            base = "FROM delivery_inbox d LEFT JOIN delivery_inbox_read r ON r.session_name=d.session_name AND r.delivery_id=d.delivery_id WHERE d.session_name=?"
            if op == 'status':
                n = db.execute("SELECT count(*) " + base + " AND r.read_at IS NULL", (self.session_name,)).fetchone()[0]
                return {'ok':True,'mode':'driver','unread':n,'automatic_delivery_unchanged':True,'mcp_wait_supported':False}
            if op == 'list':
                limit = args.get('limit',20)
                if type(limit) is not int or not 1 <= limit <= 100:
                    raise ValueError('limit must be 1..100')
                rows = db.execute("SELECT d.delivery_id AS id,d.received_at,d.state,r.read_at " + base + " AND r.read_at IS NULL ORDER BY d.received_at,d.delivery_id LIMIT ?",(self.session_name,limit)).fetchall()
                return {'ok':True,'mode':'driver','messages':[dict(r) for r in rows]}
            key = args.get('message_id')
            if not isinstance(key,str) or not 1 <= len(key) <= 256:
                raise ValueError('invalid message id')
            row = db.execute("SELECT d.*,r.read_at " + base + " AND d.delivery_id=?",(self.session_name,key)).fetchone()
            if row is None:
                return {'ok':False,'error':'not_found'}
            if op == 'read':
                item=dict(row);env=json.loads(item.pop('envelope_json'));item['id']=item.pop('delivery_id');item['body']=str(env.get('body') or '')[:65536];item['body_truncated']=len(str(env.get('body') or ''))>65536
                return {'ok':True,'message':item,'mode':'driver'}
            db.execute("INSERT OR IGNORE INTO delivery_inbox_read(session_name,delivery_id,read_at) VALUES(?,?,?)",(self.session_name,key,time.time()))
            return {'ok':True,'state':'read','local_only':True,'mode':'driver'}
