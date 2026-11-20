"""Antigravity ACP stdio driver for sessionHelper turn events."""
from __future__ import annotations

import hashlib
import json
import os
import queue
import shutil
import sqlite3
import subprocess
import threading
import time
import uuid
from pathlib import Path

from .contract import Capabilities, DeliveryReceipt, SessionEvent


class AntigravityProtocolDriver:
    caps = Capabilities(protocol_ack=True, replayable_events=False, human_attach=False,
                        interrupt=True, resumable=False, unattended=True,
                        session_survives_helper_restart=False, platforms=frozenset({"linux", "darwin"}))

    def __init__(self, store_path: str, cwd: str, cmd=None):
        self.store_path = store_path
        self.cwd = cwd
        self.cmd = list(cmd) if cmd else [shutil.which("agy") or "agy"]
        if self.cmd[-1] != "acp":
            self.cmd.append("acp")
        self._lock = threading.RLock()
        self._write_lock = threading.Lock()
        self._condition = threading.Condition(self._lock)
        self._subscribers = []
        self._history = []
        self._cursor = 0
        self._next_rpc = 0
        self._pending = {}
        self._process = None
        self._session_id = ""
        self._active = None
        self.on_activity = None
        Path(store_path).parent.mkdir(parents=True, exist_ok=True)
        with self._db() as db:
            db.execute("CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, hash TEXT NOT NULL, turn TEXT NOT NULL, state TEXT NOT NULL, detail TEXT NOT NULL)")

    def _db(self):
        db = sqlite3.connect(self.store_path, timeout=10)
        db.execute("PRAGMA busy_timeout=10000")
        return db

    def _row(self, delivery_id):
        with self._db() as db:
            return db.execute("SELECT hash,turn,state,detail FROM deliveries WHERE id=?", (delivery_id,)).fetchone()

    def _save(self, delivery_id, digest, turn, state, detail):
        with self._db() as db:
            db.execute("INSERT INTO deliveries VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,detail=excluded.detail", (delivery_id, digest, turn, state, detail))

    def _send(self, message):
        with self._write_lock:
            if not self.health():
                raise RuntimeError("Antigravity ACP process is unavailable")
            self._process.stdin.write(json.dumps(message, ensure_ascii=False) + "\n")
            self._process.stdin.flush()

    def _request(self, method, params, timeout=30):
        with self._lock:
            self._next_rpc += 1
            rpc_id = self._next_rpc
            response = queue.Queue(maxsize=1)
            self._pending[rpc_id] = response
        try:
            self._send({"jsonrpc": "2.0", "id": rpc_id, "method": method, "params": params})
            result = response.get(timeout=timeout)
            if "error" in result:
                raise RuntimeError(str(result["error"]))
            return result.get("result", {})
        finally:
            with self._lock:
                self._pending.pop(rpc_id, None)

    def start(self):
        with self._lock:
            if self.health():
                return
            # A dead pipe has an unknowable active outcome. Never replay its
            # message; allow independent later deliveries on a new session.
            self._active = None
            self._process = subprocess.Popen(self.cmd, cwd=self.cwd, stdin=subprocess.PIPE,
                                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                             text=True, bufsize=1)
            threading.Thread(target=self._read, name="antigravity-acp-reader", daemon=True).start()
        try:
            self._request("initialize", {"protocolVersion": 1, "clientCapabilities": {},
                                          "clientInfo": {"name": "dingwei-sessionhelper", "version": "1"}})
            result = self._request("session/new", {"cwd": self.cwd, "mcpServers": []})
            self._session_id = result["sessionId"]
        except Exception:
            self.stop()
            raise

    def stop(self):
        with self._lock:
            process = self._process
            self._process = None
            self._condition.notify_all()
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=3)
            for pipe in (process.stdin, process.stdout, process.stderr):
                if pipe is not None:
                    pipe.close()

    def health(self):
        return self._process is not None and self._process.poll() is None

    def _emit(self, event):
        with self._lock:
            self._cursor += 1
            event.cursor = str(self._cursor)
            self._history.append(event)
            self._history = self._history[-10000:]
            for subscriber in list(self._subscribers):
                subscriber.put_nowait(event)
        if callable(self.on_activity):
            self.on_activity()

    def _read(self):
        process = self._process
        try:
            for line in process.stdout:
                try:
                    message = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if "id" in message and ("result" in message or "error" in message):
                    with self._lock:
                        pending = self._pending.get(message["id"])
                    if pending:
                        pending.put(message)
                    continue
                if message.get("method") == "session/update":
                    self._update(message.get("params") or {})
                elif "id" in message and message.get("method") == "session/request_permission":
                    # Unattended sessions fail closed on interactive permission prompts.
                    self._send({"jsonrpc": "2.0", "id": message["id"],
                                "result": {"outcome": {"outcome": "cancelled"}}})
        finally:
            with self._lock:
                if self._active:
                    self._active = None
                self._condition.notify_all()

    def _update(self, params):
        with self._lock:
            active = self._active
            if not active or params.get("sessionId") != self._session_id:
                return
            delivery_id, digest, turn, admitted_at = active
            update = params.get("update") or {}
            kind = update.get("sessionUpdate")
            if kind in ("agent_message_chunk", "tool_call", "tool_call_update") and not admitted_at:
                admitted_at = time.time()
                self._active = (delivery_id, digest, turn, admitted_at)
                self._emit(SessionEvent(kind="state_change", data={"change": "turn_started"},
                                        session_id=self._session_id, delivery_id=delivery_id, turn_id=turn))
                self._save(delivery_id, digest, turn, "accepted", f"native ACP update; turn={turn}")
                self._condition.notify_all()
            if kind == "agent_message_chunk":
                content = update.get("content") or {}
                if content.get("type") == "text" and content.get("text"):
                    self._emit(SessionEvent(kind="assistant_text", text=content["text"],
                                            session_id=self._session_id, delivery_id=delivery_id, turn_id=turn))
            elif kind in ("tool_call", "tool_call_update"):
                tool = {"name": update.get("title") or update.get("kind") or "tool",
                        "input": update.get("rawInput") or {}, "status": update.get("status")}
                self._emit(SessionEvent(kind="tool_call", data=tool,
                                        session_id=self._session_id, delivery_id=delivery_id, turn_id=turn))

    def deliver(self, envelope_id, body):
        digest = hashlib.sha256(body.encode()).hexdigest()
        with self._condition:
            row = self._row(envelope_id)
            if row:
                if row[0] != digest:
                    return DeliveryReceipt(envelope_id, "failed", "protocol", "idempotency conflict")
                return DeliveryReceipt(envelope_id, row[2], "protocol", row[3])
        if not self.health():
            self.start()
        with self._condition:
            row = self._row(envelope_id)
            if row:
                if row[0] != digest:
                    return DeliveryReceipt(envelope_id, "failed", "protocol", "idempotency conflict")
                return DeliveryReceipt(envelope_id, row[2], "protocol", row[3])
            if self._active:
                return DeliveryReceipt(envelope_id, "queued", "protocol", "another turn is active")
            turn = str(uuid.uuid4())
            self._save(envelope_id, digest, turn, "unknown", f"dispatch intent; turn={turn}")
            self._active = (envelope_id, digest, turn, None)
            threading.Thread(target=self._run_prompt, args=(envelope_id, digest, turn, body), daemon=True).start()
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                row = self._row(envelope_id)
                if row[2] != "unknown":
                    return DeliveryReceipt(envelope_id, row[2], "protocol", row[3])
                self._condition.wait(min(0.1, max(0, deadline - time.monotonic())))
            return DeliveryReceipt(envelope_id, "unknown", "protocol", f"native admission unconfirmed; turn={turn}")

    def _run_prompt(self, delivery_id, digest, turn, body):
        try:
            result = self._request("session/prompt", {"sessionId": self._session_id,
                            "prompt": [{"type": "text", "text": body}]}, timeout=3600)
            reason = result.get("stopReason")
            state = "done" if reason == "end_turn" else "failed"
            status = "completed" if state == "done" else "interrupted" if reason == "cancelled" else "failed"
            detail = f"turn interrupted; turn={turn}" if reason == "cancelled" else f"ACP stopReason={reason}; turn={turn}"
        except Exception as exc:
            state, status, detail = "unknown", "failed", f"ACP prompt outcome unknown: {exc}; turn={turn}"
        with self._condition:
            active = self._active
            if not active or active[0] != delivery_id or active[2] != turn:
                return
            if state != "unknown":
                if active[3] is None:
                    self._emit(SessionEvent(kind="state_change", data={"change": "turn_started"},
                                            session_id=self._session_id, delivery_id=delivery_id, turn_id=turn))
                self._emit(SessionEvent(kind="state_change", data={"change": "turn_completed", "status": status,
                                        "raw_method": "session/prompt"},
                                        session_id=self._session_id, delivery_id=delivery_id, turn_id=turn))
            self._save(delivery_id, digest, turn, state, detail)
            self._active = None
            self._condition.notify_all()

    def poll_receipt(self, envelope_id):
        row = self._row(envelope_id)
        if not row:
            return DeliveryReceipt(envelope_id, "unknown", "protocol", "no delivery")
        return DeliveryReceipt(envelope_id, row[2], "protocol", row[3])

    def events(self, since=None):
        subscriber = queue.SimpleQueue()
        with self._lock:
            for event in self._history:
                if since is not None and int(event.cursor) > int(since):
                    subscriber.put_nowait(event)
            self._subscribers.append(subscriber)
        try:
            while self.health() or not subscriber.empty():
                try:
                    yield subscriber.get(timeout=0.5)
                except queue.Empty:
                    continue
        finally:
            with self._lock:
                self._subscribers.remove(subscriber)

    def interrupt(self):
        with self._lock:
            active = self._active
        return bool(active and self.interrupt_delivery(active[0], active[2]))

    def interrupt_delivery(self, envelope_id, turn_id):
        with self._condition:
            active = self._active
            if not active or (active[0], active[2]) != (envelope_id, turn_id):
                return False
            self._send({"jsonrpc": "2.0", "method": "session/cancel", "params": {"sessionId": self._session_id}})
            deadline = time.monotonic() + 10
            while self._active is not None and time.monotonic() < deadline:
                self._condition.wait(min(.1, max(0, deadline - time.monotonic())))
            row = self._row(envelope_id)
            return bool(row and row[2] == "failed" and row[3].startswith("turn interrupted"))

    def human_input(self, data):
        raise NotImplementedError("ACP pipe has no human attach channel")

    def resize(self, rows, cols):
        pass
