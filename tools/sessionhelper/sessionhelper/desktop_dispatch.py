"""Offline-tested admission engine; no native GUI transport is enabled here.

A future native provider must verify the owner's selected existing GUI backend
and thread, serialize idle admission with that backend, and retain its normal
approval policy. MCP/inbox content cannot supply or change this binding.
"""
from __future__ import annotations

import threading


class DesktopDispatcher:
    def __init__(self, adapter, provider, thread_id):
        if not isinstance(thread_id, str) or not 1 <= len(thread_id) <= 256:
            raise ValueError("explicit thread ID required")
        self.adapter = adapter
        self.provider = provider
        self.thread_id = thread_id
        self._lock = threading.Lock()
        with adapter._lock, adapter._connect() as db:
            db.execute('''CREATE TABLE IF NOT EXISTS desktop_admission (
                message_id TEXT PRIMARY KEY, thread_id TEXT NOT NULL,
                state TEXT NOT NULL, turn_id TEXT)''')

    def step(self):
        """Admit at most one item. Persist intent before any native side effect.

        A start with an unknown outcome remains uncertain across restarts. It
        blocks subsequent admissions and is never automatically retried.
        A confirmed start is admission only, not execution/completion or reply.
        """
        with self._lock:
            # Provider metadata must never be derived from an incoming envelope.
            if not self.provider.bound_to_original_desktop(self.thread_id):
                return {"state": "unbound"}
            with self.adapter._lock, self.adapter._connect() as db:
                db.execute("BEGIN IMMEDIATE")
                pause = db.execute("SELECT value FROM desktop_setting WHERE key='paused'").fetchone()
                if pause and pause[0] == '1':
                    return {"state": "paused"}
                pending = db.execute("SELECT state FROM desktop_admission WHERE state IN ('starting','uncertain','admitted') LIMIT 1").fetchone()
                if pending:
                    return {"state": "blocked", "admission_state": pending[0]}
                row = db.execute('''SELECT m.id,m.body FROM desktop_message m
                    LEFT JOIN desktop_admission a ON a.message_id=m.id
                    WHERE m.state='unread' AND a.message_id IS NULL
                    ORDER BY m.received_at,m.id LIMIT 1''').fetchone()
                if row is None:
                    return {"state": "empty"}
                # Provider must atomically refuse busy admission, not steer or
                # interrupt an existing user turn. No RPC status/read history.
                if not self.provider.loaded_and_idle(self.thread_id):
                    return {"state": "busy"}
                db.execute("INSERT INTO desktop_admission VALUES(?,?,'starting',NULL)", (row['id'], self.thread_id))
            try:
                turn_id = self.provider.start_if_idle(
                    self.thread_id, row['id'], row['body'])
                if not isinstance(turn_id, str) or not 1 <= len(turn_id) <= 256:
                    raise ValueError("missing native admission ID")
            except BaseException:
                # Includes cancellation; never leak native exception text or
                # assume an exception proves the request didn't reach backend.
                with self.adapter._lock, self.adapter._connect() as db:
                    db.execute("UPDATE desktop_admission SET state='uncertain' WHERE message_id=?", (row['id'],))
                raise
            with self.adapter._lock, self.adapter._connect() as db:
                db.execute("UPDATE desktop_admission SET state='admitted',turn_id=? WHERE message_id=?", (turn_id, row['id']))
            return {"state": "admitted", "message_id": row['id'], "turn_id": turn_id}

    def complete(self, message_id, turn_id, *, thread_id):
        """Only a correlated terminal backend event releases the next message.

        This does not ACK the inbox or send a reply; those remain explicit. No
        unrelated desktop turn or chat body is consumed by this engine.
        """
        with self._lock, self.adapter._lock, self.adapter._connect() as db:
            result = db.execute('''UPDATE desktop_admission SET state='completed'
                WHERE message_id=? AND turn_id=? AND thread_id=? AND state='admitted' ''',
                (message_id, turn_id, thread_id))
            return result.rowcount == 1
