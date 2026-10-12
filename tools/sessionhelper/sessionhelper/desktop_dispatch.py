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
            db.execute('''CREATE TABLE IF NOT EXISTS desktop_reply (
                message_id TEXT PRIMARY KEY, turn_id TEXT NOT NULL,
                target TEXT NOT NULL, body TEXT NOT NULL,
                state TEXT NOT NULL DEFAULT 'ready')''')

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

    def complete_with_reply(self, message_id, turn_id, *, thread_id, body):
        """Trusted provider submits the final output of this exact DW turn only.

        Native completed != Hub delivered. Persist output + terminal admission
        atomically. Incoming/model-supplied routing fields cannot pick a target.
        """
        if not isinstance(body, str) or not body.strip() or len(body.encode('utf-8')) > 65536:
            raise ValueError('bounded nonempty reply required')
        with self._lock, self.adapter._lock, self.adapter._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row = db.execute("""SELECT m.reply_to FROM desktop_admission a
                JOIN desktop_message m ON m.id=a.message_id
                WHERE a.message_id=? AND a.turn_id=? AND a.thread_id=?
                AND a.state='admitted'""", (message_id, turn_id, thread_id)).fetchone()
            if row is None:
                return False
            target = row['reply_to']
            if not target or len(target) > 256 or any(ch.isspace() for ch in target):
                raise ValueError('original message has no valid reply target')
            db.execute('INSERT INTO desktop_reply(message_id,turn_id,target,body) VALUES(?,?,?,?)',
                       (message_id, turn_id, target, body))
            db.execute("UPDATE desktop_admission SET state='completed' WHERE message_id=?", (message_id,))
            return True

    def flush_reply(self, sender):
        """One scoped send through a future helper-owned Hub sender.

        sender(payload) must return {'ok': True} only after the defined durable
        Hub ACK. No original native output is reported as sent before that ACK.
        Exceptions or invalid ACK remain uncertain and are not retried. This
        engine is not currently wired to a production sender.
        """
        with self._lock:
            with self.adapter._lock, self.adapter._connect() as db:
                db.execute('BEGIN IMMEDIATE')
                pause = db.execute("SELECT value FROM desktop_setting WHERE key='paused'").fetchone()
                if pause and pause[0] == '1':
                    return {'state': 'paused'}
                pending = db.execute("SELECT state FROM desktop_reply WHERE state IN ('sending','uncertain') LIMIT 1").fetchone()
                if pending:
                    return {'state': 'blocked', 'reply_state': pending[0]}
                row = db.execute("SELECT * FROM desktop_reply WHERE state='ready' ORDER BY rowid LIMIT 1").fetchone()
                if row is None:
                    return {'state': 'empty'}
                db.execute("UPDATE desktop_reply SET state='sending' WHERE message_id=?", (row['message_id'],))
            payload = {'to': row['target'], 'body': row['body'],
                       'meta': {'in_reply_to': row['message_id'], 'kind': 'reply'}}
            try:
                result = sender(payload)
                if not isinstance(result, dict) or result.get('ok') is not True:
                    raise ValueError('durable reply ACK missing')
            except BaseException:
                with self.adapter._lock, self.adapter._connect() as db:
                    db.execute("UPDATE desktop_reply SET state='uncertain' WHERE message_id=?", (row['message_id'],))
                raise
            with self.adapter._lock, self.adapter._connect() as db:
                db.execute("UPDATE desktop_reply SET state='sent' WHERE message_id=?", (row['message_id'],))
                db.execute("UPDATE desktop_message SET state='read' WHERE id=?", (row['message_id'],))
            return {'state': 'sent', 'message_id': row['message_id'], 'turn_id': row['turn_id']}
