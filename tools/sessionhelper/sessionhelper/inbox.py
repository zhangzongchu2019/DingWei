"""Durable, idempotent inbox for reliable Hub deliveries."""

from __future__ import annotations

import json
import os
import sqlite3
import sys
import threading
import time


class DeliveryInbox:
    def __init__(
        self,
        path: str,
        capacity: int = 100,
        session_name: str = "default",
        session_full_name: str = "",
        registered_session_name: str = "",
        key_id: str = "",
    ):
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

    def _connect(self) -> sqlite3.Connection:
        db = sqlite3.connect(self.path, timeout=5)
        db.row_factory = sqlite3.Row
        return db

    def _initialize(self) -> None:
        with self._lock, self._connect() as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute(
                """CREATE TABLE IF NOT EXISTS delivery_inbox (
                    delivery_id TEXT NOT NULL,
                    session_name TEXT NOT NULL,
                    envelope_json TEXT NOT NULL,
                    state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                    attempts INTEGER NOT NULL DEFAULT 0,
                    error TEXT,
                    received_at REAL NOT NULL,
                    updated_at REAL NOT NULL,
                    PRIMARY KEY(session_name, delivery_id)
                )"""
            )
            self._migrate_schema(db)
            db.execute(
                "CREATE INDEX IF NOT EXISTS idx_delivery_inbox_fifo ON delivery_inbox(session_name,state,received_at)"
            )
            db.execute(
                "UPDATE delivery_inbox SET state='received', updated_at=? WHERE session_name=? AND state='processing'",
                (time.time(), self.session_name),
            )

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
                state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
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

    def _legacy_shared_path(self) -> str:
        return os.path.join(os.path.dirname(self.path) or ".", "sessionhelper_inbox.db")

    def _import_legacy_shared_inbox(self) -> None:
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
                            "received",
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
        with sqlite3.connect(f"file:{legacy_path}?mode=ro", uri=True, timeout=5) as db:
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
        with self._lock, self._connect() as db:
            row = db.execute(
                "SELECT state,received_at FROM delivery_inbox WHERE delivery_id=? AND session_name=?",
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
                    "INSERT INTO delivery_inbox(delivery_id,session_name,envelope_json,state,received_at,updated_at) VALUES(?,?,?,'received',?,?)",
                    (delivery_id, self.session_name, payload, now, now),
                )
                received_at = now
                state = "received"
            else:
                state, received_at = row["state"], row["received_at"]
                if state == "failed" and retry_failed:
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

    def release(self, delivery_id: str) -> None:
        with self._lock, self._connect() as db:
            db.execute(
                """UPDATE delivery_inbox
                   SET state='received',updated_at=?
                   WHERE delivery_id=? AND session_name=? AND state='processing'""",
                (time.time(), delivery_id, self.session_name),
            )
