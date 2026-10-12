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
        self._condition = threading.Condition(self._lock)
        self._wait_guard = threading.Lock()
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
            db.execute("""CREATE TABLE IF NOT EXISTS desktop_mcp_reply (
                message_id TEXT PRIMARY KEY, body TEXT NOT NULL, target TEXT NOT NULL,
                state TEXT NOT NULL, outcome TEXT)""")
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
            self._condition.notify_all()
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
                    "native_auto_notify": "NOT_VERIFIED", "native_auto_notify_implemented": False, "mcp_wait_supported": True, "mode": "desktop"}
        if op in ("pause", "resume"):
            with self._lock, self._connect() as db:
                db.execute("INSERT OR REPLACE INTO desktop_setting(key,value) VALUES('paused',?)", ("1" if op == "pause" else "0",))
                self.paused = op == "pause"
                self._condition.notify_all()
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

    def wait_message(self, args, *, cancel_event=None):
        """Bounded MCP tool wait. Returning data never ACKs/adopts/executes it."""
        seconds = args.get('wait_seconds', 45)
        if type(seconds) is not int or not 1 <= seconds <= 45:
            raise ValueError('wait_seconds must be 1..45')
        if not self._wait_guard.acquire(blocking=False):
            return {'ok': False, 'error': 'desktop_listener_busy'}
        deadline = time.monotonic() + seconds
        try:
            with self._condition:
                while True:
                    if cancel_event is not None and cancel_event.is_set():
                        return {'ok': True, 'state': 'cancelled'}
                    with self._connect() as db:
                        paused = db.execute("SELECT value FROM desktop_setting WHERE key='paused'").fetchone()
                        if paused and paused[0]=='1':
                            return {'ok': True, 'state': 'paused'}
                        row = db.execute("SELECT * FROM desktop_message WHERE state='unread' ORDER BY received_at,id LIMIT 1").fetchone()
                    if row:
                        with self._connect() as db:
                            reply = db.execute("SELECT state FROM desktop_mcp_reply WHERE message_id=?",(row['id'],)).fetchone()
                        if reply and reply[0] in ('sending','uncertain','rejected'):
                            return {'ok': True, 'state': 'reply_rejected' if reply[0]=='rejected' else 'reply_uncertain', 'message_id': row['id']}
                        return {'ok': True, 'state': 'message', 'message': dict(row),
                                'receipt_stage': 'returned_to_mcp', 'model_execution_verified': False}
                    remaining = deadline-time.monotonic()
                    if remaining <= 0:
                        return {'ok': True, 'state': 'timeout'}
                    self._condition.wait(min(remaining, .1))
        finally:
            self._wait_guard.release()

    def reserve_mcp_reply(self, message_id, body):
        if not isinstance(message_id,str) or not 1 <= len(message_id)<=256:
            raise ValueError('invalid message_id')
        if not isinstance(body,str) or not body.strip() or len(body.encode('utf-8'))>65536:
            raise ValueError('invalid reply body')
        with self._lock,self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            prior=db.execute('SELECT * FROM desktop_mcp_reply WHERE message_id=?',(message_id,)).fetchone()
            if prior:
                if prior['state']=='confirmed' and prior['body']==body:
                    return {'ok': True,'already_confirmed':True,'outcome':json.loads(prior['outcome'])}
                return {'ok': False,'error':'reply_already_attempted'}
            row=db.execute("SELECT reply_to FROM desktop_message WHERE id=? AND state='unread'",(message_id,)).fetchone()
            if row is None or not row[0] or len(row[0])>256 or any(c.isspace() for c in row[0]):
                return {'ok':False,'error':'no_unread_message_or_reply_target'}
            db.execute("INSERT INTO desktop_mcp_reply VALUES(?,?,?,'sending',NULL)",(message_id,body,row[0]))
            return {'ok':True,'target':row[0]}

    def finish_mcp_reply(self, message_id, outcome=None, *, rejected=False):
        confirmed=isinstance(outcome,dict) and outcome.get('ok') is True and outcome.get('stage')=='hub_durable_queued'
        with self._condition,self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            changed=db.execute("UPDATE desktop_mcp_reply SET state=?,outcome=? WHERE message_id=? AND state='sending'",
                ('confirmed' if confirmed else 'rejected' if rejected else 'uncertain',json.dumps(outcome) if confirmed else None,message_id)).rowcount
            if changed and confirmed:
                db.execute("UPDATE desktop_message SET state='read' WHERE id=?",(message_id,))
            self._condition.notify_all()
        return bool(changed and confirmed)

    def next_session_event(self, timeout=0.5):
        try:
            return self._events.get(timeout=timeout)
        except queue.Empty:
            return None
