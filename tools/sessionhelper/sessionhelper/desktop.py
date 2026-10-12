"""Desktop inbox adapter. It does not launch a CLI or claim to wake a native UI.

MCP reads are explicit and bounded; receiving a message does not mark it as
injected or completed by an AI. Connection-level deliveries remain deduplicated
by the regular helper inbox, and this second inbox survives helper reconnects.
"""
from __future__ import annotations

import json
import os
import queue
import sqlite3
import stat
import threading
import time


class DesktopAdapter:
    name = "desktop"

    def __init__(self, cfg):
        self.path = cfg.inbox_db + ".desktop.sqlite"
        self._lock = threading.Lock()
        self._events = queue.Queue(maxsize=1000)
        self.paused = False
        parent = os.path.dirname(self.path) or "."
        os.makedirs(parent, mode=0o700, exist_ok=True)
        # Create privately from the start, including when this file doesn't yet contain messages.
        if os.path.islink(self.path):
            raise ValueError("desktop inbox must not be a symlink")
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0), 0o600)
        metadata = os.fstat(fd)
        os.close(fd)
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_mode & 0o077:
            raise ValueError("desktop inbox must be private (0600)")
        with self._connect() as db:
            db.execute("""CREATE TABLE IF NOT EXISTS desktop_message (
                id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL,
                body TEXT NOT NULL, reply_to TEXT NOT NULL,
                received_at REAL NOT NULL, state TEXT NOT NULL DEFAULT 'unread')""")
            db.execute("CREATE TABLE IF NOT EXISTS desktop_setting(key TEXT PRIMARY KEY, value TEXT NOT NULL)")
            row = db.execute("SELECT value FROM desktop_setting WHERE key='paused'").fetchone()
            self.paused = bool(row and row[0] == "1")

    def _connect(self):
        db = sqlite3.connect(self.path, timeout=5)
        db.row_factory = sqlite3.Row
        return db

    def handle(self, env):
        return self.handle_delivery(env, on_injected=None)

    def handle_delivery(self, env, on_injected=None, on_admitted=None, on_admission_failed=None):
        meta = env.get("meta") or {}
        message_id = str(env.get("id") or env.get("message_id") or meta.get("message_id") or "")
        delivery_id = str(meta.get("delivery_id") or message_id)
        if not message_id or len(message_id) > 256:
            raise ValueError("desktop message lacks a bounded ID")
        body = str(env.get("body") or "")
        if len(body.encode("utf-8")) > 65536:
            raise ValueError("desktop message exceeds 64 KiB")
        reply_to = str(meta.get("reply_to") or env.get("from") or "")
        with self._lock, self._connect() as db:
            if not db.execute("SELECT 1 FROM desktop_message WHERE id=?", (message_id,)).fetchone() and db.execute("SELECT count(*) FROM desktop_message WHERE state='unread'").fetchone()[0] >= 1000:
                raise OverflowError("desktop inbox full")
            db.execute("INSERT OR IGNORE INTO desktop_message(id,delivery_id,body,reply_to,received_at) VALUES(?,?,?,?,?)",
                       (message_id, delivery_id, body, reply_to, time.time()))
        if on_admission_failed is not None:
            on_admission_failed()  # release staged guide claim until native model admission is confirmed
        # Do not call on_injected: a durable unread item has not entered a model turn.
        return ""

    def request(self, args):
        op = args.get("operation")
        if op == "status":
            with self._lock, self._connect() as db:
                count = db.execute("SELECT count(*) FROM desktop_message WHERE state='unread'").fetchone()[0]
            return {"ok": True, "unread": count, "paused": self.paused,
                    "native_auto_notify": "NOT_VERIFIED", "native_auto_notify_implemented": False, "mode": "desktop"}
        if op in ("pause", "resume"):
            with self._lock, self._connect() as db:
                db.execute("INSERT OR REPLACE INTO desktop_setting(key,value) VALUES('paused',?)", ("1" if op == "pause" else "0",))
                self.paused = op == "pause"
            return {"ok": True, "paused": self.paused}
        if op == "list":
            limit = args.get("limit", 20)
            if type(limit) is not int or not 1 <= limit <= 100:
                raise ValueError("limit must be 1..100")
            with self._lock, self._connect() as db:
                rows = db.execute("SELECT id,received_at,state FROM desktop_message WHERE state='unread' ORDER BY received_at,id LIMIT ?", (limit,)).fetchall()
            return {"ok": True, "messages": [dict(r) for r in rows], "paused": self.paused}
        message_id = args.get("message_id")
        if not isinstance(message_id, str) or not 1 <= len(message_id) <= 256:
            raise ValueError("message_id is required")
        with self._lock, self._connect() as db:
            row = db.execute("SELECT * FROM desktop_message WHERE id=?", (message_id,)).fetchone()
            if row is None:
                return {"ok": False, "error": "not_found"}
            if op == "read":
                return {"ok": True, "message": dict(row), "paused": self.paused}
            if op == "ack":
                db.execute("UPDATE desktop_message SET state='read' WHERE id=? AND state='unread'", (message_id,))
                return {"ok": True, "state": "read"}
        raise ValueError("unsupported desktop inbox operation")

    def next_session_event(self, timeout=0.5):
        try:
            return self._events.get(timeout=timeout)
        except queue.Empty:
            return None
