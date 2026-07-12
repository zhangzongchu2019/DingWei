"""Durable, idempotent inbox for reliable Hub deliveries."""

from __future__ import annotations

import json
import os
import sqlite3
import threading
import time


class DeliveryInbox:
    def __init__(self, path: str, capacity: int = 100):
        self.path = os.path.expanduser(path)
        self.capacity = max(1, capacity)
        self._lock = threading.Lock()
        os.makedirs(os.path.dirname(self.path) or ".", exist_ok=True)
        self._initialize()

    def _connect(self) -> sqlite3.Connection:
        db = sqlite3.connect(self.path, timeout=5)
        db.row_factory = sqlite3.Row
        return db

    def _initialize(self) -> None:
        with self._lock, self._connect() as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute(
                """CREATE TABLE IF NOT EXISTS delivery_inbox (
                    delivery_id TEXT PRIMARY KEY,
                    envelope_json TEXT NOT NULL,
                    state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                    attempts INTEGER NOT NULL DEFAULT 0,
                    error TEXT,
                    received_at REAL NOT NULL,
                    updated_at REAL NOT NULL
                )"""
            )
            db.execute("CREATE INDEX IF NOT EXISTS idx_delivery_inbox_fifo ON delivery_inbox(state,received_at)")
            db.execute("UPDATE delivery_inbox SET state='received', updated_at=? WHERE state='processing'", (time.time(),))

    def accept(self, delivery_id: str, env: dict, retry_failed: bool = False) -> tuple[str, int, int]:
        """Persist a delivery and return (ack_state, queue_position, queue_depth)."""
        now = time.time()
        payload = json.dumps(env, ensure_ascii=False, separators=(",", ":"))
        with self._lock, self._connect() as db:
            row = db.execute(
                "SELECT state,received_at FROM delivery_inbox WHERE delivery_id=?", (delivery_id,)
            ).fetchone()
            if row is None:
                depth = db.execute(
                    "SELECT COUNT(*) FROM delivery_inbox WHERE state IN ('received','processing')"
                ).fetchone()[0]
                if depth >= self.capacity:
                    raise OverflowError("queue_full")
                db.execute(
                    "INSERT INTO delivery_inbox(delivery_id,envelope_json,state,received_at,updated_at) VALUES(?,?,'received',?,?)",
                    (delivery_id, payload, now, now),
                )
                received_at = now
                state = "received"
            else:
                state, received_at = row["state"], row["received_at"]
                if state == "failed" and retry_failed:
                    db.execute(
                        """UPDATE delivery_inbox
                           SET envelope_json=?,state='received',error=NULL,updated_at=?
                           WHERE delivery_id=? AND state='failed'""",
                        (payload, now, delivery_id),
                    )
                    state = "received"
            position = db.execute(
                """SELECT COUNT(*) FROM delivery_inbox
                   WHERE state IN ('received','processing') AND (received_at<? OR (received_at=? AND delivery_id<?))""",
                (received_at, received_at, delivery_id),
            ).fetchone()[0]
            depth = db.execute(
                "SELECT COUNT(*) FROM delivery_inbox WHERE state IN ('received','processing')"
            ).fetchone()[0]
        # processing is an internal state; the wire protocol maps it to delivered.
        ack_state = "processed" if state == "processed" else "failed" if state == "failed" else "delivered"
        return ack_state, int(position), int(depth)

    def claim_next(self) -> tuple[str, dict] | None:
        now = time.time()
        with self._lock, self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT delivery_id,envelope_json FROM delivery_inbox WHERE state='received' ORDER BY received_at,delivery_id LIMIT 1"
            ).fetchone()
            if row is None:
                db.commit()
                return None
            changed = db.execute(
                "UPDATE delivery_inbox SET state='processing',attempts=attempts+1,updated_at=? WHERE delivery_id=? AND state='received'",
                (now, row["delivery_id"]),
            ).rowcount
            db.commit()
            if changed != 1:
                return None
            return str(row["delivery_id"]), json.loads(row["envelope_json"])

    def finish(self, delivery_id: str, error: str = "") -> None:
        state = "failed" if error else "processed"
        with self._lock, self._connect() as db:
            db.execute(
                "UPDATE delivery_inbox SET state=?,error=?,updated_at=? WHERE delivery_id=?",
                (state, error or None, time.time(), delivery_id),
            )

    def release(self, delivery_id: str) -> None:
        with self._lock, self._connect() as db:
            db.execute(
                "UPDATE delivery_inbox SET state='received',updated_at=? WHERE delivery_id=? AND state='processing'",
                (time.time(), delivery_id),
            )
