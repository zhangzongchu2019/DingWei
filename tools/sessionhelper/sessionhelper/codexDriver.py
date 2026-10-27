"""CodexProtocolDriver -- Codex app-server JSON-RPC driver.

P0-A implementation for contract.py. The default transport uses the approved
managed daemon over its Unix-socket WebSocket endpoint. `direct` mode remains
available as a helper-owned stdio fallback.
"""
from __future__ import annotations

import hashlib
import json
import logging
import os
import queue
import base64
import shutil
import sqlite3
from contextlib import AbstractContextManager
import socket
import struct
import subprocess
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Optional

from .sqlite_utils import connection as sqlite_connection
from .config import env_get
from .daemon_identity import DaemonIdentityError, OwnedDaemonIdentity, verify_peer
from .private_endpoint import Client as PrivateEndpointClient, Spec as PrivateEndpointSpec, PrivateEndpointError
from .contract import (
    Capabilities,
    DeliveryReceipt,
    Driver,
    EventKind,
    ReceiptState,
    SessionEvent,
)

log = logging.getLogger(__name__)

# codex 二进制:SH_CODEX_BIN env → PATH 解析,别写死开发机路径(否则每台都挂)
_CODEX_BIN = env_get("SH_CODEX_BIN") or shutil.which("codex") or "codex"
DEFAULT_WORKDIR = os.path.expanduser("~")
DEFAULT_STORE = os.path.join(os.path.expanduser("~"), ".codex-driver-delivery.sqlite3")
JSONRPC_TIMEOUT = 30.0
# T-094: 一级按可归属增量事件后的空闲时长判卡死，二级限制回合总时长。
ACCEPTED_STALL_TIMEOUT = 90.0
ABSOLUTE_TURN_TIMEOUT = 900.0
EVENT_DRAIN_TIMEOUT = 5.0
FANOUT_QUEUE_MAX = 1000


class JsonRpcError(RuntimeError):
    """Keep the protocol error separate from transport failure, without logging payloads."""

    def __init__(self, method: str, error: dict):
        self.method = method
        self.code = error.get("code") if type(error.get("code")) is int else None
        self.message = error.get("message")
        super().__init__(f"{method} failed: error_class={self.classify()} rpc_code={self.code}")

    def classify(self, thread_id: str = "") -> str:
        # These exact method/code/message combinations were measured on 0.159.2.
        if type(self.code) is int and self.code == -32600 and thread_id:
            if self.method == "thread/read" and self.message == f"thread not loaded: {thread_id}":
                return "thread_not_loaded"
            if ((self.method == "thread/resume" and self.message == f"no rollout found for thread id {thread_id}")
                    or (self.method == "turn/start" and self.message == f"thread not found: {thread_id}")):
                return "missing_thread"
        if self.code in (401, 403) or (isinstance(self.message, str)
                and self.message.startswith(("401 Unauthorized", "403 Forbidden"))):
            return "authentication"
        return "rpc_error"


class ThreadUnavailable(RuntimeError):
    pass


def _error_class(exc: Exception, thread_id: str = "") -> str:
    if isinstance(exc, DaemonIdentityError):
        return "daemon_identity_" + exc.reason
    if isinstance(exc, JsonRpcError):
        return exc.classify(thread_id)
    if isinstance(exc, TimeoutError):
        return "timeout"
    if isinstance(exc, (ConnectionError, OSError)):
        return "disconnected"
    if isinstance(exc, ThreadUnavailable):
        return "invalid_thread_binding"
    return "driver_error"


def _failure_detail(prefix: str, exc: Exception, thread_id: str = "") -> str:
    # No raw server text, prompts, environment values or error.data in events/logs.
    category = _error_class(exc, thread_id)
    method = exc.method if isinstance(exc, JsonRpcError) else "local"
    code = exc.code if isinstance(exc, JsonRpcError) and type(exc.code) is int else "none"
    return f"{prefix}: error_class={category}; method={method}; rpc_code={code}"


@dataclass
class _DeliveryRecord:
    envelope_id: str
    thread_id: str
    turn_id: str
    body_hash: str
    state: ReceiptState
    detail: str
    created_at: float
    updated_at: float


class _DeliveryStore:
    """Small persistent idempotency ledger.

    Codex accepts a `clientUserMessageId`, but the server does not document
    exactly-once semantics for repeated `turn/start` calls. The driver therefore
    keeps a durable ledger keyed by `(thread_id, envelope_id)` so a helper restart
    cannot create a second user message for an already accepted envelope.
    """

    def __init__(self, path: str):
        self._path = path
        self._lock = threading.Lock()
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        with self._connect() as db:
            db.execute(
                """
                CREATE TABLE IF NOT EXISTS deliveries (
                    thread_id TEXT NOT NULL,
                    envelope_id TEXT NOT NULL,
                    turn_id TEXT NOT NULL,
                    body_hash TEXT NOT NULL,
                    state TEXT NOT NULL,
                    detail TEXT NOT NULL DEFAULT '',
                    created_at REAL NOT NULL,
                    updated_at REAL NOT NULL,
                    PRIMARY KEY (thread_id, envelope_id)
                )
                """
            )

    def _connect(self) -> AbstractContextManager[sqlite3.Connection]:
        return sqlite_connection(self._path, timeout=10)

    def get(self, thread_id: str, envelope_id: str) -> Optional[_DeliveryRecord]:
        with self._lock, self._connect() as db:
            row = db.execute(
                """
                SELECT envelope_id, thread_id, turn_id, body_hash, state, detail, created_at, updated_at
                FROM deliveries
                WHERE thread_id = ? AND envelope_id = ?
                """,
                (thread_id, envelope_id),
            ).fetchone()
        if row is None:
            return None
        return _DeliveryRecord(*row)

    def get_by_envelope(self, envelope_id: str) -> Optional[_DeliveryRecord]:
        """Recovery lookup independent of a freshly initialized thread alias."""
        with self._lock, self._connect() as db:
            row = db.execute(
                """SELECT envelope_id, thread_id, turn_id, body_hash, state, detail, created_at, updated_at
                   FROM deliveries WHERE envelope_id = ? ORDER BY updated_at DESC LIMIT 1""",
                (envelope_id,),
            ).fetchone()
        return _DeliveryRecord(*row) if row is not None else None

    def upsert(
        self,
        thread_id: str,
        envelope_id: str,
        turn_id: str,
        body_hash: str,
        state: ReceiptState,
        detail: str = "",
    ) -> None:
        now = time.time()
        with self._lock, self._connect() as db:
            db.execute(
                """
                INSERT INTO deliveries
                    (thread_id, envelope_id, turn_id, body_hash, state, detail, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(thread_id, envelope_id) DO UPDATE SET
                    turn_id = excluded.turn_id,
                    body_hash = excluded.body_hash,
                    state = excluded.state,
                    detail = excluded.detail,
                    updated_at = excluded.updated_at
                """,
                (thread_id, envelope_id, turn_id, body_hash, state, detail, now, now),
            )

    def update_state(
        self,
        thread_id: str,
        envelope_id: str,
        state: ReceiptState,
        detail: str = "",
    ) -> None:
        with self._lock, self._connect() as db:
            db.execute(
                """
                UPDATE deliveries
                SET state = ?, detail = ?, updated_at = ?
                WHERE thread_id = ? AND envelope_id = ?
                """,
                (state, detail, time.time(), thread_id, envelope_id),
            )


class StdioJsonRpc:
    """Newline-delimited JSON-RPC over a long-lived stdio process."""

    def __init__(self, cmd: list[str], *, cwd: str, env: dict[str, str] | None = None):
        self._cmd = cmd
        self._cwd = cwd
        self._env = env
        self._proc: subprocess.Popen[str] | None = None
        self._next_id = 1
        self._write_lock = threading.Lock()
        self._pending: dict[int, queue.Queue[dict]] = {}
        self.notifications: queue.Queue[dict] = queue.Queue()
        self._stderr: queue.Queue[str] = queue.Queue()
        self._stop = threading.Event()
        self._reader_dead = threading.Event()

    def start(self) -> None:
        if self._proc is not None and self._proc.poll() is None:
            return
        self._stop.clear()
        self._reader_dead.clear()
        self._proc = subprocess.Popen(
            self._cmd,
            cwd=self._cwd,
            env=self._env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        threading.Thread(target=self._read_stdout, daemon=True).start()
        threading.Thread(target=self._read_stderr, daemon=True).start()

    def stop(self) -> None:
        self._stop.set()
        proc = self._proc
        if proc is None:
            return
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
        self._proc = None

    def healthy(self) -> bool:
        return self._proc is not None and self._proc.poll() is None and not self._reader_dead.is_set()

    def request(self, method: str, params: dict | None = None, *, timeout: float = JSONRPC_TIMEOUT) -> dict:
        if self._proc is None or self._proc.stdin is None or self._proc.poll() is not None:
            raise ConnectionError("JSON-RPC transport is not running")

        with self._write_lock:
            request_id = self._next_id
            self._next_id += 1
            response_q: queue.Queue[dict] = queue.Queue(maxsize=1)
            self._pending[request_id] = response_q
            msg = {"id": request_id, "method": method}
            if params is not None:
                msg["params"] = params
            try:
                self._proc.stdin.write(json.dumps(msg, separators=(",", ":")) + "\n")
                self._proc.stdin.flush()
            except Exception:
                self._pending.pop(request_id, None)
                raise

        try:
            response = response_q.get(timeout=timeout)
        except queue.Empty as exc:
            raise TimeoutError(f"JSON-RPC request timed out: {method}") from exc
        finally:
            self._pending.pop(request_id, None)

        if response.get("_transport_closed") is True:
            raise ConnectionError(f"JSON-RPC connection closed: {method}")
        if "error" in response:
            raise JsonRpcError(method, response["error"])
        return response.get("result", {})

    def notify(self, method: str, params: dict | None = None) -> None:
        if self._proc is None or self._proc.stdin is None or self._proc.poll() is not None:
            raise ConnectionError("JSON-RPC transport is not running")
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        with self._write_lock:
            self._proc.stdin.write(json.dumps(msg, separators=(",", ":")) + "\n")
            self._proc.stdin.flush()

    def _read_stdout(self) -> None:
        assert self._proc is not None and self._proc.stdout is not None
        try:
            for line in self._proc.stdout:
                if self._stop.is_set():
                    break
                try:
                    msg = json.loads(line)
                except json.JSONDecodeError:
                    log.warning("codex app-server emitted non-JSON stdout: %r", line[:300])
                    continue
                request_id = msg.get("id")
                if request_id in self._pending:
                    self._pending[request_id].put(msg)
                else:
                    self.notifications.put(msg)
        finally:
            if not self._stop.is_set():
                self._reader_dead.set()
            for pending in list(self._pending.values()):
                try:
                    pending.put_nowait({"_transport_closed": True})
                except queue.Full:
                    pass

    def _read_stderr(self) -> None:
        assert self._proc is not None and self._proc.stderr is not None
        for line in self._proc.stderr:
            if self._stop.is_set():
                break
            self._stderr.put(line.rstrip("\n"))
            log.debug("codex app-server stderr: %s", line.rstrip("\n"))


class UnixWebSocketJsonRpc:
    """JSON-RPC over a WebSocket carried by the daemon's Unix control socket."""

    def __init__(self, socket_path: str, *, identity_check: str = "observe",
                 expected_exe: str | None = None,
                 owned_identity: OwnedDaemonIdentity | None = None, connector=None):
        self._socket_path = socket_path
        self._identity_check = identity_check
        self._expected_exe = expected_exe if expected_exe is not None else _CODEX_BIN
        self._owned_identity = owned_identity
        self._connector = connector
        self.peer_identity = None
        self.identity_warning = None
        self._sock: socket.socket | None = None
        self._next_id = 1
        self._write_lock = threading.Lock()
        self._pending: dict[int, queue.Queue[dict]] = {}
        self.notifications: queue.Queue[dict] = queue.Queue()
        self._stop = threading.Event()
        self._reader_dead = threading.Event()

    def start(self) -> None:
        self._stop.clear()
        self._reader_dead.clear()
        self._sock = None
        self.peer_identity = None
        self.identity_warning = None
        try:
            if self._connector is None:
                self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self._sock.connect(self._socket_path)
            else:
                self._sock = self._connector()
            if self._identity_check not in ('observe', 'enforce', 'off'):
                raise DaemonIdentityError('invalid_switch')
            if self._identity_check != 'off':
                try:
                    self.peer_identity = verify_peer(self._sock, self._expected_exe, self._owned_identity)
                except DaemonIdentityError as exc:
                    if self._identity_check == 'enforce':
                        raise
                    self.identity_warning = exc.reason
                    log.warning('codex_daemon_identity_observe warning=daemon_identity_%s identity_scope=unverified', exc.reason)
            else:
                log.warning('codex_daemon_identity_disabled identity_scope=unchecked')
            key = base64.b64encode(os.urandom(16)).decode("ascii")
            request = (
                "GET / HTTP/1.1\r\n"
                "Host: localhost\r\n"
                "Upgrade: websocket\r\n"
                "Connection: Upgrade\r\n"
                f"Sec-WebSocket-Key: {key}\r\n"
                "Sec-WebSocket-Version: 13\r\n"
                "\r\n"
            ).encode("ascii")
            self._sock.sendall(request)
            response = b""
            while b"\r\n\r\n" not in response:
                chunk = self._sock.recv(4096)
                if not chunk:
                    raise RuntimeError("websocket handshake closed before response")
                response += chunk
            if b" 101 " not in response.split(b"\r\n", 1)[0]:
                raise RuntimeError(f"websocket handshake failed: {response[:300]!r}")
            # A private connector bounds its verification/handshake. The live
            # notification stream must not expire merely because it is quiet.
            if self._connector is not None:
                self._sock.settimeout(None)
            threading.Thread(target=self._read_loop, daemon=True).start()

        except Exception:
            # No WS close message on a connection that failed admission.
            self._stop.set()
            self._reader_dead.set()
            if self._sock is not None:
                self._sock.close()
            self._sock = None
            raise

    def stop(self) -> None:
        self._stop.set()
        if self._sock is not None:
            try:
                self._send_close()
            except Exception:
                pass
            try:
                self._sock.close()
            except OSError:
                pass
            self._sock = None

    def healthy(self) -> bool:
        return self._sock is not None and not self._stop.is_set() and not self._reader_dead.is_set()

    def request(self, method: str, params: dict | None = None, *, timeout: float = JSONRPC_TIMEOUT) -> dict:
        with self._write_lock:
            request_id = self._next_id
            self._next_id += 1
            response_q: queue.Queue[dict] = queue.Queue(maxsize=1)
            self._pending[request_id] = response_q
            msg = {"id": request_id, "method": method}
            if params is not None:
                msg["params"] = params
            try:
                self._send_text(json.dumps(msg, separators=(",", ":")))
            except Exception:
                self._pending.pop(request_id, None)
                raise
        try:
            response = response_q.get(timeout=timeout)
        except queue.Empty as exc:
            raise TimeoutError(f"JSON-RPC request timed out: {method}") from exc
        finally:
            self._pending.pop(request_id, None)
        if response.get("_transport_closed") is True:
            raise ConnectionError(f"JSON-RPC connection closed: {method}")
        if "error" in response:
            raise JsonRpcError(method, response["error"])
        return response.get("result", {})

    def notify(self, method: str, params: dict | None = None) -> None:
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        with self._write_lock:
            self._send_text(json.dumps(msg, separators=(",", ":")))

    def _read_loop(self) -> None:
        try:
            while not self._stop.is_set():
                try:
                    text = self._recv_text()
                except OSError:
                    return
                if text is None:
                    return
                try:
                    msg = json.loads(text)
                except json.JSONDecodeError:
                    log.warning("codex websocket emitted non-JSON message: %r", text[:300])
                    continue
                request_id = msg.get("id")
                if request_id in self._pending:
                    self._pending[request_id].put(msg)
                else:
                    self.notifications.put(msg)
        finally:
            if not self._stop.is_set():
                self._reader_dead.set()
            for pending in list(self._pending.values()):
                try:
                    pending.put_nowait({"_transport_closed": True})
                except queue.Full:
                    pass

    def _send_text(self, text: str) -> None:
        if self._sock is None:
            raise ConnectionError("websocket transport is not running")
        payload = text.encode("utf-8")
        self._sock.sendall(self._encode_frame(0x1, payload))

    def _send_close(self) -> None:
        if self._sock is not None:
            self._sock.sendall(self._encode_frame(0x8, b""))

    @staticmethod
    def _encode_frame(opcode: int, payload: bytes) -> bytes:
        header = bytearray([0x80 | opcode])
        length = len(payload)
        if length < 126:
            header.append(0x80 | length)
        elif length < 65536:
            header.extend([0x80 | 126])
            header.extend(struct.pack("!H", length))
        else:
            header.extend([0x80 | 127])
            header.extend(struct.pack("!Q", length))
        mask = os.urandom(4)
        header.extend(mask)
        masked = bytes(byte ^ mask[i % 4] for i, byte in enumerate(payload))
        return bytes(header) + masked

    def _recv_text(self) -> str | None:
        sock = self._sock
        if sock is None:
            raise OSError('websocket closed')
        first = self._recv_exact(2)
        if not first:
            return None
        opcode = first[0] & 0x0F
        masked = bool(first[1] & 0x80)
        length = first[1] & 0x7F
        if length == 126:
            length = struct.unpack("!H", self._recv_exact(2))[0]
        elif length == 127:
            length = struct.unpack("!Q", self._recv_exact(8))[0]
        mask = self._recv_exact(4) if masked else b""
        payload = self._recv_exact(length)
        if masked:
            payload = bytes(byte ^ mask[i % 4] for i, byte in enumerate(payload))
        if opcode == 0x8:
            return None
        if opcode == 0x9:
            with self._write_lock:
                sock.sendall(self._encode_frame(0xA, payload))
            return self._recv_text()
        if opcode != 0x1:
            return self._recv_text()
        return payload.decode("utf-8")

    def _recv_exact(self, n: int) -> bytes:
        sock = self._sock
        if sock is None:
            raise OSError('websocket closed')
        chunks = bytearray()
        while len(chunks) < n:
            chunk = sock.recv(n - len(chunks))
            if not chunk:
                raise OSError("websocket closed")
            chunks.extend(chunk)
        return bytes(chunks)


class CodexProtocolDriver(Driver):
    """Driver for Codex app-server v2 JSON-RPC."""

    def __init__(
        self,
        *,
        workdir: str = DEFAULT_WORKDIR,
        transport_mode: str = "daemon_ws",
        manage_daemon: bool = True,
        daemon_verified: bool = False,
        replay_verified: bool = False,
        store_path: str = DEFAULT_STORE,
        model: str | None = None,
        model_provider: str | None = None,
        sandbox: str | None = "read-only",
        approval_policy: str | None = "never",
        ephemeral: bool = False,
        stall_timeout: float = ACCEPTED_STALL_TIMEOUT,
        view_item_backfill: bool = False,
        daemon_identity_check: str | None = None,
        daemon_expected_exe: str | None = None,
        owned_daemon_identity: OwnedDaemonIdentity | None = None,
        endpoint_mode: str | None = None,
        endpoint_state_dir: str | None = None,
        endpoint_seat: str | None = None,
        endpoint_codex_home: str | None = None,
    ):
        self._workdir = workdir
        self._transport_mode = transport_mode
        self._daemon_verified = daemon_verified
        self._replay_verified = replay_verified
        self.caps = self._caps_for_transport(
            transport_mode,
            daemon_verified=daemon_verified,
            replay_verified=replay_verified,
        )
        self._manage_daemon = manage_daemon
        switch = (env_get('SH_CODEX_DAEMON_IDENTITY_CHECK', 'observe')
                  if daemon_identity_check is None else daemon_identity_check)
        self._daemon_identity_check = switch if type(switch) is str else ''
        self._daemon_expected_exe = daemon_expected_exe if daemon_expected_exe is not None else (env_get('SH_CODEX_DAEMON_EXE') or _CODEX_BIN)
        # This is a trusted launcher API, never a PID guessed from process lists
        # or supplied by an unverified pidfile/environment variable.
        self._owned_daemon_identity = owned_daemon_identity
        self._endpoint_mode = env_get('SH_CODEX_ENDPOINT_MODE', 'legacy') if endpoint_mode is None else endpoint_mode
        self._private_endpoint = None
        self._endpoint_options = (endpoint_state_dir, endpoint_seat, endpoint_codex_home)
        self._model = model
        self._model_provider = model_provider
        self._sandbox = sandbox
        self._approval_policy = approval_policy
        self._ephemeral = ephemeral
        self._stall_timeout = stall_timeout
        # Backfill can only be enabled with a View renderer that understands
        # item-key replacement. Legacy Hub/View deployments stay unchanged.
        self._view_item_backfill = view_item_backfill is True
        self._backfill_pending = False
        self._backfill_lock = threading.Lock()
        self._absolute_timeout = ABSOLUTE_TURN_TIMEOUT
        self._store = _DeliveryStore(store_path)

        self._rpc: StdioJsonRpc | None = None
        self._initialized = False
        self._thread_id = ""
        self._session_id = ""  # compatibility with the conformance draft
        self._connection_generation = 0
        self._verified_binding = None
        self._active_turn_id = ""
        self._envelope_states: dict[str, ReceiptState] = {}
        self._envelope_to_turn: dict[str, str] = {}
        self._turn_to_envelope: dict[str, str] = {}
        self._failed_turns: dict[str, str] = {}  # turn/completed(failed) 通知先到时的暂存（竞态兜底）
        self._delivery_timestamps: dict[str, float] = {}
        self._delivery_last_event_at: dict[str, float] = {}
        self._delivery_pending_tools: dict[str, set[str]] = {}
        self._delivery_anonymous_tools: dict[str, list[str]] = {}
        self._anonymous_tool_seq = 0
        self._done_seen_at: dict[str, float] = {}
        self._events_drained: set[str] = set()
        self._delivery_lock = threading.Lock()
        self._interrupt_requested_turns: set[str] = set()
        self._turn_item_counts: dict[str, int] = {}
        self._item_cursor_by_id: dict[str, str] = {}
        self._consumers: list[queue.Queue[SessionEvent | None]] = []
        self._consumers_lock = threading.Lock()
        self._event_log: list[SessionEvent] = []  # in-memory buffer for replay (like claudeDriver._history)
        self._event_log_lock = threading.Lock()
        # R-V2-0105 件二（RECONNECT-MUSTFIX 必须 1）：start() 由桥线程与 deliver 线程并发进入，加重连锁。
        self._start_lock = threading.Lock()
        # 事件桥可挂一个无参回调：重连成功、新事件发出时唤醒它的可中断睡眠（RECONNECT-MUSTFIX 必须 2）。
        self.on_activity = None
        self._live_event_seq = 0
        self._stop_event = threading.Event()
        self._event_thread: threading.Thread | None = None
        self._last_interrupt_evidence = ""

    @staticmethod
    def _caps_for_transport(
        transport_mode: str,
        *,
        daemon_verified: bool = False,
        replay_verified: bool = False,
    ) -> Capabilities:
        if transport_mode == "daemon_ws":
            return Capabilities(
                protocol_ack=True,
                replayable_events=replay_verified,
                human_attach=daemon_verified,
                interrupt=True,
                resumable=True,
                unattended=True,
                session_survives_helper_restart=daemon_verified,
                platforms=frozenset({"linux", "darwin", "windows"}),
            )
        if transport_mode == "proxy":
            return Capabilities(
                protocol_ack=True,
                replayable_events=False,
                human_attach=False,
                interrupt=True,
                resumable=True,
                unattended=True,
                session_survives_helper_restart=False,
                platforms=frozenset({"linux", "darwin", "windows"}),
            )
        if transport_mode == "direct":
            return Capabilities(
                protocol_ack=True,
                replayable_events=replay_verified,
                human_attach=False,
                interrupt=True,
                resumable=True,
                unattended=True,
                session_survives_helper_restart=False,
                platforms=frozenset({"linux", "darwin", "windows"}),
            )
        raise ValueError(f"unsupported transport_mode={transport_mode!r}")

    def healthy(self) -> bool:
        """R-V2-0105 件二：供事件桥判断 app-server 连接是否已死（死了才重连）。"""
        rpc = self._rpc
        return rpc is not None and rpc.healthy()

    def _notify_activity(self) -> None:
        cb = getattr(self, "on_activity", None)
        if callable(cb):
            try:
                cb()
            except Exception:
                pass

    def start(self) -> None:
        # R-V2-0105 件二：重连锁内重判健康；停旧 rpc 与建新 rpc 都在锁内（两线程同时进入只建一个）。
        # Transport reconnection invalidates admission; the next new delivery
        # must validate/resume the exact bound thread under this same lock.
        lock = self.__dict__.get("_start_lock") or self.__dict__.setdefault("_start_lock", threading.Lock())
        with lock:
            if self._rpc is not None and self._rpc.healthy():
                return
            # A few lifecycle tests construct the driver with __new__ to
            # exercise start() without running __init__. Treat that as a
            # never-connected instance, matching the initialized value.
            had_connection = getattr(self, "_connection_generation", 0) > 0
            if self._rpc is not None:
                # 重连前先停掉旧的不健康连接（读线程死而进程仍在时不留孤儿进程）。
                try:
                    self._rpc.stop()
                except Exception:
                    pass
            self._start_locked()
            if had_connection and self._view_item_backfill and self._thread_id:
                self._backfill_pending = True
        self._notify_activity()

    def _start_locked(self) -> None:
        if self._transport_mode == "daemon_ws":
            self._initialized = False
            self._verified_binding = None
            self._rpc = None
            try:
                if self._endpoint_mode not in ('legacy', 'private'):
                    raise PrivateEndpointError('endpoint_config')
                if self._endpoint_mode == 'private':
                    if self._daemon_identity_check != 'enforce' or self._manage_daemon is not True:
                        raise PrivateEndpointError('endpoint_config')
                    if self._private_endpoint is None:
                        state, seat, codex_home = self._endpoint_options
                        spec = PrivateEndpointSpec(
                            state or env_get('SH_STATE_DIR') or os.path.expanduser('~/.dingwei'),
                            seat or env_get('SH_SESSION_NAME') or '',
                            codex_home or env_get('CODEX_HOME') or os.path.expanduser('~/.codex'),
                            self._workdir, _CODEX_BIN, self._daemon_expected_exe)
                        self._private_endpoint = PrivateEndpointClient(spec)
                    live = self._private_endpoint.ensure()
                    self._private_endpoint.take_writer()
                    child = live['child']
                    self._owned_daemon_identity = OwnedDaemonIdentity(child['pid'], child['starttime'])
                    socket_path = str(self._private_endpoint.spec.root / 'app.sock')
                else:
                    if self._manage_daemon:
                        # Current CLI returns no verifiable daemon PID contract.
                        # Do not confuse its short-lived start command with daemon.
                        self._start_daemon()
                    socket_path = self._daemon_socket_path()
                self._rpc = UnixWebSocketJsonRpc(socket_path,
                    identity_check=self._daemon_identity_check,
                    expected_exe=self._daemon_expected_exe,
                    owned_identity=self._owned_daemon_identity,
                    connector=(lambda: self._private_endpoint.open_app(live)) if self._private_endpoint is not None else None)
                self._rpc.start()
                self._initialize()
                if self._private_endpoint is not None and not self._thread_id:
                    # Persisted current binding is a hint, not admission.
                    # The next new delivery still read/resumes this exact ID.
                    self._thread_id = self._private_endpoint.bound_thread()
                    self._session_id = self._thread_id
                peer = self._rpc.peer_identity
                if self._rpc.identity_warning is not None:
                    self._emit_event(SessionEvent(kind='state_change',
                        text='daemon_identity_' + self._rpc.identity_warning,
                        data={'change': 'daemon_identity_observed',
                              'identity_mode': 'observe', 'identity_scope': 'unverified',
                              'error_class': 'daemon_identity_' + self._rpc.identity_warning}))
                if peer is not None:
                    self._emit_event(SessionEvent(kind='state_change',
                        data={'change': 'daemon_identity_verified',
                              'identity_mode': self._daemon_identity_check, **peer.metadata()}))
            except Exception as exc:
                if self._private_endpoint is not None:
                    self._private_endpoint.release_writer()
                if self._rpc is not None:
                    self._rpc.stop()
                self._rpc = None
                self._initialized = False
                self._verified_binding = None
                if isinstance(exc, DaemonIdentityError):
                    detail = _failure_detail('session_unavailable', exc)
                    self._emit_event(SessionEvent(kind='error', text=detail,
                        data={'reason': 'session_unavailable',
                              'change': 'daemon_identity_rejected',
                              'error_class': 'daemon_identity_' + exc.reason}))
                raise
            self._stop_event.clear()
            self._event_thread = threading.Thread(target=self._pump_notifications, daemon=True)
            self._event_thread.start()
            return
        if self._endpoint_mode != 'legacy':
            raise PrivateEndpointError('endpoint_config')
        if self._transport_mode == "proxy":
            if self._manage_daemon:
                self._start_daemon()
            cmd = [_CODEX_BIN, "app-server", "proxy"]
        elif self._transport_mode == "direct":
            cmd = [_CODEX_BIN, "app-server", "--stdio"]
        else:
            raise ValueError(f"unsupported transport_mode={self._transport_mode!r}")

        env = os.environ.copy()
        env.setdefault("PYTHONUTF8", "1")
        env.setdefault("PYTHONIOENCODING", "utf-8")
        env.setdefault("SH_SESSION_NAME", f"codex-driver-{os.getpid()}")

        self._rpc = StdioJsonRpc(cmd, cwd=self._workdir, env=env)
        self._rpc.start()
        self._initialize()
        self._stop_event.clear()
        self._event_thread = threading.Thread(target=self._pump_notifications, daemon=True)
        self._event_thread.start()

    def abort_transport(self) -> None:
        """Abort only this connection; never kill a shared daemon."""
        rpc = self._rpc
        if rpc is None:
            return
        proc = getattr(rpc, '_proc', None)
        if proc is not None:
            if proc.poll() is None:
                proc.kill()
            proc.wait(timeout=2)
        else:
            # Local websocket shutdown bypasses the potentially held write lock.
            sock = getattr(rpc, '_sock', None)
            if sock is not None:
                import socket
                sock.shutdown(socket.SHUT_RDWR)
                sock.close()

    def stop(self) -> None:
        self._stop_event.set()
        with self._consumers_lock:
            for q in self._consumers:
                try:
                    q.put_nowait(None)
                except queue.Full:
                    pass
        if self._rpc is not None:
            self._rpc.stop()
            self._rpc = None
        if self._private_endpoint is not None:
            self._private_endpoint.release_writer()
        self._initialized = False

    def health(self) -> bool:
        return self._rpc is not None and self._rpc.healthy() and self._initialized

    def deliver(self, envelope_id: str, body: str) -> DeliveryReceipt:
        with self._delivery_lock:
            self._sync_thread_alias()
            body_hash = hashlib.sha256(body.encode("utf-8")).hexdigest()
            existing = self._store.get(self._thread_id, envelope_id) or self._store.get_by_envelope(envelope_id)
            if existing is not None and (existing.state != "failed" or existing.turn_id
                    or existing.detail.startswith("session_unavailable:")):
                # Historical receipt lookup must not rebind the active session.
                self._remember_delivery(envelope_id, existing.turn_id, existing.state)
                return DeliveryReceipt(
                    id=envelope_id,
                    state="failed" if existing.state == "unknown" else existing.state,
                    source="protocol",
                    detail=existing.detail or f"persistent idempotency hit: turn={existing.turn_id}",
                )
            if not self.health():
                try:
                    self.start()
                except DaemonIdentityError as exc:
                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)
            # Do not replace the connection between validation and turn/start.
            with self._start_lock:
                if not self._thread_id:
                    try:
                        self._ensure_thread()
                    except PrivateEndpointError as exc:
                        return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)
                try:
                    self._validate_bound_thread()
                except Exception as exc:
                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)
                idle_error = self._wait_until_idle_for_delivery()
                if idle_error:
                    self._envelope_states[envelope_id] = "failed"
                    return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=idle_error)
                params = {
                    "threadId": self._thread_id,
                    "input": [{"type": "text", "text": body}],
                    "clientUserMessageId": envelope_id,
                }
                # Close the crash/EOF window before dispatch: uncertainty is
                # durable and cannot turn into a second submission after restart.
                self._store.upsert(self._thread_id, envelope_id, "", body_hash, "unknown", "model_admission_unknown: dispatch pending")
                try:
                    result = self._request("turn/start", params)
                except Exception as exc:
                    # An arbitrary/internal RPC error is not evidence that the
                    # server did nothing. Only explicit known rejections can be
                    # labelled not admitted; every other dispatch result stays
                    # durably uncertain, without automatic submission again.
                    rejected = (isinstance(exc, PrivateEndpointError)
                                or (isinstance(exc, JsonRpcError) and exc.classify(self._thread_id) in ("missing_thread", "authentication")))
                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=not rejected)
                turn = result.get("turn", {})
                turn_id = turn.get("id", "")
                if not isinstance(turn_id, str) or not turn_id:
                    return self._admission_failure(envelope_id, body_hash, ThreadUnavailable("missing turn id"), uncertain=True)
                state: ReceiptState = "processing" if turn.get("status") == "inProgress" else "accepted"
                detail = f"turn={turn_id} status={turn.get('status')}"
                self._store.upsert(self._thread_id, envelope_id, turn_id, body_hash, state, detail)
                self._remember_delivery(envelope_id, turn_id, state)
                self._delivery_timestamps[envelope_id] = time.monotonic()
                self._active_turn_id = turn_id
                return DeliveryReceipt(id=envelope_id, state=state, source="protocol", detail=detail)

    def _admission_failure(self, envelope_id: str, body_hash: str, exc: Exception, *, uncertain: bool) -> DeliveryReceipt:
        self._verified_binding = None
        prefix = "model_admission_unknown" if uncertain else "session_unavailable"
        detail = _failure_detail(prefix, exc, self._thread_id)
        self._store.upsert(self._thread_id, envelope_id, "", body_hash, "unknown" if uncertain else "failed", detail)
        self._envelope_states[envelope_id] = "failed"
        return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=detail)

    def poll_receipt(self, envelope_id: str) -> DeliveryReceipt:
        self._sync_thread_alias()
        current_state = self._envelope_states.get(envelope_id)
        record = self._store.get(self._thread_id, envelope_id) or self._store.get_by_envelope(envelope_id)
        if record is not None and record.state == "unknown" and not record.turn_id:
            return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=record.detail)
        if not self._thread_id and record is None:
            return DeliveryReceipt(id=envelope_id, state="unknown", source="protocol", detail="no thread")
        if record is not None and record.state == "done":
            self._remember_delivery(envelope_id, record.turn_id, record.state)
            return DeliveryReceipt(
                id=envelope_id,
                state=record.state,
                source="protocol",
                detail=record.detail or f"persistent state turn={record.turn_id}",
            )
        if record is not None and record.state == "failed" and "stall" not in record.detail.lower():
            self._remember_delivery(envelope_id, record.turn_id, record.state)
            return DeliveryReceipt(
                id=envelope_id,
                state=record.state,
                source="protocol",
                detail=record.detail or f"persistent state turn={record.turn_id}",
            )

        turn_id = self._envelope_to_turn.get(envelope_id) or (record.turn_id if record else "")
        if turn_id and turn_id in self._failed_turns:
            detail = self._failed_turns.pop(turn_id)  # 命中即清（复验提醒②：只写不删是无界增长）
            self._mark_delivery(envelope_id, "failed", detail)
            return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=detail)
        if turn_id:
            try:
                state, detail = self._read_record_turn_state(turn_id, record)
            except Exception as exc:
                state, detail = current_state or "unknown", f"turn poll failed: {exc}"
            if (
                state == "failed"
                and "interrupted" in detail.lower()
                and turn_id not in self._interrupt_requested_turns
            ):
                state, detail = self._confirm_unrequested_interrupt(turn_id, record)
            if state == "done":
                return self._done_receipt_after_event_drain(envelope_id, detail)
            if state == "failed":
                self._mark_delivery(envelope_id, state, detail)
                return DeliveryReceipt(id=envelope_id, state=state, source="protocol", detail=detail)
            if state in ("accepted", "queued", "processing"):
                self._mark_delivery(envelope_id, state, detail)
                current_state = state

        delivered_at = self._delivery_timestamps.get(envelope_id)
        if current_state in ("accepted", "queued", "processing") and delivered_at is not None:
            now = time.monotonic()
            total_elapsed = now - delivered_at
            idle_elapsed = now - self._delivery_last_event_at.get(envelope_id, delivered_at)
            if total_elapsed > self._absolute_timeout:
                detail = (
                    f"absolute turn timeout: total runtime {total_elapsed:.0f}s "
                    f"exceeded {self._absolute_timeout:.0f}s limit"
                )
                log.warning("[delivery][ABSOLUTE_TIMEOUT] envelope=%s %s", envelope_id, detail)
                self._mark_delivery(envelope_id, "failed", detail)
                return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=detail)
            if not self._delivery_pending_tools.get(envelope_id) and idle_elapsed > self._stall_timeout:
                detail = f"idle stalled: no progress event for {idle_elapsed:.0f}s"
                log.warning("[delivery][STALLED] envelope=%s %s", envelope_id, detail)
                self._mark_delivery(envelope_id, "failed", detail)
                return DeliveryReceipt(id=envelope_id, state="failed", source="protocol", detail=detail)

        return DeliveryReceipt(
            id=envelope_id,
            state=self._envelope_states.get(envelope_id, "unknown"),
            source="protocol",
            detail="no terminal state yet",
        )

    def events(self, since: str | None = None) -> Iterator[SessionEvent]:
        self._sync_thread_alias()
        if since is not None:
            # Replay from codex API (authoritative, handles gaps across restarts)
            yield from self._replay_events_since(since)
            # Also replay buffered events that may have arrived after the API snapshot
            with self._event_log_lock:
                for ev in self._event_log:
                    if self._cursor_after(ev.cursor, since):
                        yield ev
            return
        # No cursor: replay buffered events, then subscribe to live stream.
        # The in-memory buffer ensures events emitted before subscription
        # (e.g. during deliver→turn completion before events() was called)
        # are not lost (Bug1 fix).
        # R-V2-0105 REAL-EXIT-SAMECURSOR（组长 B，R-V2-0105-SAMECURSOR-IDENTITY-REVIEW-ADD）：
        # - 先登记订阅队列（只取消费者锁，放锁后）再取缓冲锁重放：登记之后发出的事件必进本队列；登记之前
        #   发出的都已在缓冲里——快照与登记之间不再有两边都不在的空窗。锁序：两把锁不嵌套，不会与
        #   _emit_event（先缓冲锁、放锁后再取消费者锁）反序。
        # - 重放仍在缓冲锁内 yield（既有行为，本轮不改）：重放期间发事件者在缓冲锁上等，不会往队列里灌。
        # - 去重按事件对象同一性，不按 cursor 或内容：Codex 的 cursor 是「每个 item 一个」（同一条消息各片
        #   共用），同 cursor、同文本的不同对象都是新片，须保留。重叠只可能是「登记后、快照前」发出的同一
        #   对象（既进缓冲又进队列）。replayed 以 id→对象持引用、取到再用 is 核：持引用使这些 id 在本次订阅
        #   期间不会被新对象复用（缓冲裁剪释放旧对象也不影响）。表只收快照内对象（≤ 缓冲上限），实时事件不入。
        consumer_q: queue.Queue[SessionEvent | None] = queue.Queue(maxsize=FANOUT_QUEUE_MAX)
        with self._consumers_lock:
            self._consumers.append(consumer_q)
        replayed: dict[int, SessionEvent] = {}
        try:
            # Subscribe first: notifications received while the bounded history
            # snapshot is read remain queued and are merged by stable item_id in
            # the View. The snapshot itself is read-only and never starts a turn.
            for ev in self._take_completed_item_backfill():
                self._emit_event(ev)
            with self._event_log_lock:
                for ev in self._event_log:
                    replayed[id(ev)] = ev
                    yield ev
            while not self._stop_event.is_set():
                try:
                    ev = consumer_q.get(timeout=0.5)
                    if ev is None:
                        return
                    if self._session_id and ev.session_id != self._session_id:
                        continue
                    # Dedup: skip the very objects already replayed from the buffer
                    if replayed.get(id(ev)) is ev:
                        continue
                    yield ev
                except queue.Empty:
                    if self._rpc is None or not self._rpc.healthy():
                        return
        finally:
            with self._consumers_lock:
                if consumer_q in self._consumers:
                    self._consumers.remove(consumer_q)

    def _take_completed_item_backfill(self) -> list[SessionEvent]:
        if not self._view_item_backfill or not self._thread_id:
            return []
        with self._backfill_lock:
            if not self._backfill_pending:
                return []
            # Consume this reconnect attempt once. Failure is observable and
            # intentionally not retried as a new model turn or delivery.
            self._backfill_pending = False
        try:
            self._validate_bound_thread()
            turns = self._request(
                "thread/turns/list",
                {"threadId": self._thread_id, "limit": 20, "itemsView": "full", "sortDirection": "desc"},
                timeout=20,
            ).get("data", [])
            if not isinstance(turns, list):
                raise ValueError("turn history is not a list")
        except Exception as exc:
            log.warning("[view-backfill] history unavailable thread=%s error=%s", self._thread_id, type(exc).__name__)
            return []

        events: list[SessionEvent] = []
        for turn in reversed(turns):
            if not isinstance(turn, dict):
                continue
            turn_id = turn.get("id")
            if not isinstance(turn_id, str) or not turn_id:
                continue
            # A completed turn guarantees its contained agentMessage items are
            # final when the protocol omits item.status. Explicit non-completed
            # item statuses are never backfilled.
            turn_status = turn.get("status")
            turn_completed = turn_status == "completed" or (isinstance(turn_status, dict) and turn_status.get("type") == "completed")
            delivery_id = self._turn_to_envelope.get(turn_id, "")
            items = turn.get("items", [])
            if not isinstance(items, list):
                continue
            if not delivery_id:
                delivery_id = next((item.get("clientId", "") for item in items
                                    if isinstance(item, dict) and item.get("type") == "userMessage"
                                    and isinstance(item.get("clientId"), str)), "")
            for item_index, item in enumerate(items, start=1):
                if not isinstance(item, dict) or item.get("type") != "agentMessage":
                    continue
                item_id, text = item.get("id"), item.get("text")
                if not isinstance(item_id, str) or not item_id or not isinstance(text, str) or not text:
                    continue
                item_status = item.get("status")
                if item_status not in (None, "completed") and not (
                    isinstance(item_status, dict) and item_status.get("type") == "completed"
                ):
                    continue
                if item_status is None and not turn_completed:
                    continue
                events.append(SessionEvent(
                    kind="assistant_text",
                    text=text,
                    data={"thread_id": self._thread_id, "item_id": item_id,
                          "backfill": True, "replay": True,
                          "raw_method": "history/completed_item"},
                    session_id=self._thread_id,
                    cursor=self._turn_item_cursor(turn_id, item_index),
                    delivery_id=delivery_id,
                    turn_id=turn_id,
                ))
        return events

    def interrupt(self) -> bool:
        self._sync_thread_alias()
        turn_id = self._active_turn_id
        envelope_id = self._turn_to_envelope.get(turn_id, "")
        if not turn_id or not envelope_id:
            self._last_interrupt_evidence = "no active turn"
            return False
        return self.interrupt_delivery(envelope_id, turn_id)

    def interrupt_delivery(self, envelope_id: str, turn_id: str) -> bool:
        """Interrupt the durable delivery mapping, never a mutable 'latest turn'."""
        self._sync_thread_alias()
        record = self._store.get(self._thread_id, envelope_id) or self._store.get_by_envelope(envelope_id)
        if not self._thread_id and record is None:
            return False
        mapped_turn = self._envelope_to_turn.get(envelope_id) or (record.turn_id if record else "")
        if not mapped_turn or mapped_turn != turn_id:
            self._last_interrupt_evidence = "stale or unverifiable delivery/turn mapping"
            return False
        thread_id = record.thread_id if record is not None else self._thread_id

        try:
            self._request("turn/interrupt", {"threadId": thread_id, "turnId": turn_id}, timeout=10)
        except Exception as exc:
            self._last_interrupt_evidence = f"interrupt request failed: {exc}"
            return False
        self._interrupt_requested_turns.add(turn_id)

        deadline = time.monotonic() + 8.0
        while time.monotonic() < deadline:
            state, detail = self._read_record_turn_state(turn_id, record)
            if state == "failed" and "interrupt" in detail.lower():
                self._last_interrupt_evidence = detail
                return True
            if "interrupted" in detail.lower():
                self._last_interrupt_evidence = detail
                return True
            if state == "done":
                self._last_interrupt_evidence = "turn completed naturally before interrupt evidence"
                return False
            time.sleep(0.5)
        self._last_interrupt_evidence = "timed out waiting for interrupted turn status"
        return False

    def human_input(self, data: bytes) -> None:
        if not data:
            return
        text = data.decode("utf-8", errors="replace")
        self.deliver(f"human-{int(time.time() * 1000)}", text)

    def resize(self, rows: int, cols: int) -> None:
        return None

    def compact(self) -> dict:
        if not self._thread_id:
            raise RuntimeError("no thread")
        return self._request("thread/compact/start", {"threadId": self._thread_id})

    def account_usage(self) -> dict:
        return self._request("account/usage/read", {})

    def _start_daemon(self) -> None:
        proc = subprocess.run(
            [_CODEX_BIN, "app-server", "daemon", "start"],
            cwd=self._workdir,
            text=True,
            capture_output=True,
            timeout=30,
        )
        if proc.returncode != 0:
            raise RuntimeError(
                "codex app-server daemon start failed; install standalone Codex or use "
                f"transport_mode='direct': {proc.stderr.strip() or proc.stdout.strip()}"
            )

    @staticmethod
    def _daemon_socket_path() -> str:
        return os.path.expanduser("~/.codex/app-server-control/app-server-control.sock")

    def _initialize(self) -> None:
        assert self._rpc is not None
        self._rpc.request(
            "initialize",
            {
                "clientInfo": {"name": "transport-v2-codex-driver", "version": "0"},
                "capabilities": {"experimentalApi": True},
            },
            timeout=15,
        )
        self._rpc.notify("initialized")
        self._initialized = True
        self._connection_generation += 1
        self._verified_binding = None

    def _ensure_thread(self) -> None:
        params: dict = {"cwd": self._workdir, "ephemeral": self._ephemeral}
        if self._approval_policy is not None:
            params["approvalPolicy"] = self._approval_policy
        if self._sandbox is not None:
            params["sandbox"] = self._sandbox
        if self._model is not None:
            params["model"] = self._model
        if self._model_provider is not None:
            params["modelProvider"] = self._model_provider
        result = self._request("thread/start", params)
        thread = result.get("thread", {})
        self._thread_id = thread["id"]
        self._session_id = self._thread_id
        if self._private_endpoint is not None:
            self._private_endpoint.save_thread(self._thread_id)
        self._verified_binding = self._binding_key()

    def _binding_key(self):
        return (self._rpc, self._connection_generation, self._thread_id)

    def _validate_bound_thread(self) -> None:
        if self._verified_binding == self._binding_key():
            return
        try:
            result = self._request("thread/read", {"threadId": self._thread_id, "includeTurns": False}, timeout=15)
        except JsonRpcError as exc:
            if exc.classify(self._thread_id) != "thread_not_loaded":
                raise
            result = {"thread": {"status": {"type": "notLoaded"}}}
        thread = result.get("thread", {})
        if thread.get("status", {}).get("type") == "notLoaded":
            result = self._request("thread/resume", {"threadId": self._thread_id, "excludeTurns": True}, timeout=15)
            thread = result.get("thread", {})
        if (thread.get("id") != self._thread_id
                or not isinstance(thread.get("cwd"), str)
                or os.path.realpath(thread["cwd"]) != os.path.realpath(self._workdir)
                or thread.get("status", {}).get("type") not in ("idle", "active")
                or thread.get("canAcceptDirectInput") is not True):
            raise ThreadUnavailable("thread identity, directory or admission status mismatch")
        self._verified_binding = self._binding_key()

    def _request(self, method: str, params: dict | None = None, *, timeout: float = JSONRPC_TIMEOUT) -> dict:
        if self._rpc is None:
            raise ConnectionError("driver not started")
        if self._private_endpoint is not None and method in ('thread/start', 'turn/start'):
            with self._private_endpoint.admission(self._rpc.peer_identity):
                return self._rpc.request(method, params or {}, timeout=timeout)
        return self._rpc.request(method, params or {}, timeout=timeout)

    def _pump_notifications(self) -> None:
        assert self._rpc is not None
        rpc = self._rpc
        while rpc is not None and rpc.healthy():
            try:
                msg = rpc.notifications.get(timeout=0.5)
            except queue.Empty:
                continue
            event = self._map_notification(msg)
            if event is not None:
                self._emit_event(event)

    def _emit_event(self, event: SessionEvent) -> None:
        # Store in in-memory buffer so events() can replay them even if it
        # subscribes after this event was emitted (Bug1 fix: events were lost
        # for late subscribers).
        with self._event_log_lock:
            self._event_log.append(event)
            if len(self._event_log) > 10000:
                self._event_log = self._event_log[-5000:]
        with self._consumers_lock:
            for q in self._consumers:
                try:
                    q.put_nowait(event)
                except queue.Full:
                    log.warning("[events][CONSUMER_SLOW] dropping event for slow consumer cursor=%s", event.cursor)
        self._record_published_event(event)
        self._notify_activity()  # R-V2-0105 件二：唤醒事件桥的可中断睡眠

    # codex 0.144.4 通知里只发管理信号、不包含用户可见内容的 method 集合。
    # 这些不映射为 SessionEvent，避免新 view 事件环被 state_change 刷屏。
    _ADMIN_METHODS: set[str] = {
        "remoteControl/status/changed",
        "mcpServer/startupStatus/updated",
        "account/rateLimits/updated",
        "thread/tokenUsage/updated",
    }

    def _map_notification(self, msg: dict) -> Optional[SessionEvent]:
        self._live_event_seq += 1
        method = msg.get("method", "")
        params = msg.get("params", {}) or {}
        thread_id = params.get("threadId") or self._thread_id
        cursor = f"live:{self._live_event_seq:020d}"
        data = {"raw_method": method, "params": params}

        # —— turn 生命周期（只记录内部状态，不暴露给 view） ——
        if method == "turn/started":
            turn = params.get("turn", {})
            self._active_turn_id = turn.get("id", self._active_turn_id)
            return None  # 纯生命周期通知，对用户无意义

        if method == "turn/completed":
            turn = params.get("turn", {})
            turn_id = turn.get("id", "")
            cursor = self._turn_event_cursor(turn_id, "9999")
            status = turn.get("status", "")
            envelope_id = self._turn_to_envelope.get(turn_id)
            if status == "failed":
                # turn 失败即该投递的终态（泊车枝复验第 1 件）：落账标 failed ⇒ poll 立即返回，
                # 不再拖到 terminal_timeout 补一条误导事件。
                err = turn.get("error") or {}
                detail = "turn failed: " + str(err.get("message") or err or "unknown")
                if envelope_id:
                    self._store.update_state(thread_id, envelope_id, "failed", detail)
                    self._envelope_states[envelope_id] = "failed"
                else:
                    # 通知可能先于 deliver 的映射注册到达（泵线程竞态）⇒ 按 turn 暂存，poll 兜底
                    self._failed_turns[turn_id] = detail
            if envelope_id:
                self._delivery_pending_tools.pop(envelope_id, None)
                self._delivery_anonymous_tools.pop(envelope_id, None)
            if turn_id == self._active_turn_id:
                self._active_turn_id = ""
            self._interrupt_requested_turns.discard(turn_id)
            # 把 turn 里的 usage/模型信息带到事件数据里,新 view 状态栏用
            extra = {"change": "turn_completed", "status": status}
            model_used = turn.get("model") or params.get("model", "")
            if model_used:
                extra["model"] = model_used
            usage = turn.get("usage") or params.get("usage") or {}
            if usage:
                extra["usage"] = usage
            return SessionEvent(
                kind="state_change",
                text="",
                data={**data, **extra},
                session_id=thread_id,
                cursor=cursor,
                delivery_id=envelope_id or "",
                turn_id=turn_id,
            )

        # —— item 生命周期（只记录内部状态，不暴露给 view） ——
        if method == "item/started":
            item = params.get("item", {})
            turn_id = params.get("turnId", "")
            item_id = item.get("id", "")
            if item_id and item_id not in self._item_cursor_by_id:
                self._turn_item_counts[turn_id] = self._turn_item_counts.get(turn_id, 0) + 1
                self._item_cursor_by_id[item_id] = self._turn_item_cursor(turn_id, self._turn_item_counts[turn_id])
            if item.get("type") == "userMessage":
                envelope_id = item.get("clientId")
                if envelope_id and turn_id:
                    self._remember_delivery(envelope_id, turn_id, "processing")
                    self._store.update_state(thread_id, envelope_id, "processing", f"user item admitted item={item.get('id')}")
            elif item.get("type") in ("commandExecution", "fileChange", "dynamicToolCall"):
                delivery_id = self._turn_to_envelope.get(turn_id, "")
                if delivery_id:
                    tool_id = item_id
                    if not tool_id:
                        self._anonymous_tool_seq += 1
                        tool_id = f"anonymous-tool-{self._anonymous_tool_seq}"
                        self._delivery_anonymous_tools.setdefault(delivery_id, []).append(tool_id)
                    self._delivery_pending_tools.setdefault(delivery_id, set()).add(tool_id)
                    self._delivery_last_event_at[delivery_id] = time.monotonic()
            return None  # 纯生命周期通知，对用户无意义

        if method == "item/completed":
            item = params.get("item", {})
            turn_id = params.get("turnId", "")
            delivery_id = self._turn_to_envelope.get(turn_id, "")
            item_id = item.get("id", "")
            cursor = self._item_cursor_by_id.get(item_id, cursor)
            item_type = item.get("type")
            if item_type == "agentMessage":
                # 流式文本已由 item/agentMessage/delta 提供;completed 不再重发全文,否则 view 累加翻倍
                return None
            if item_type in ("commandExecution", "fileChange", "dynamicToolCall"):
                if delivery_id:
                    pending = self._delivery_pending_tools.get(delivery_id)
                    if pending is not None:
                        if item_id:
                            pending.discard(item_id)
                        else:
                            anonymous = self._delivery_anonymous_tools.get(delivery_id)
                            if anonymous:
                                pending.discard(anonymous.pop(0))
                                if not anonymous:
                                    self._delivery_anonymous_tools.pop(delivery_id, None)
                        if not pending:
                            self._delivery_pending_tools.pop(delivery_id, None)
                    self._delivery_last_event_at[delivery_id] = time.monotonic()
                return SessionEvent(kind="tool_call", text=item.get("command", item_type), data=data, session_id=thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
            # userMessage / reasoning / 其他未知 item 类型都不作为独立事件暴露
            return None

        # —— 流式文本增量 ——
        if method == "item/agentMessage/delta":
            turn_id = params.get("turnId", "")
            delivery_id = self._turn_to_envelope.get(turn_id, "")
            item_id = params.get("itemId", "")
            cursor = self._item_cursor_by_id.get(item_id, cursor)
            if isinstance(item_id, str) and item_id:
                data["item_id"] = item_id
                data["thread_id"] = thread_id
            if delivery_id and params.get("delta", ""):
                self._delivery_last_event_at[delivery_id] = time.monotonic()
            return SessionEvent(kind="assistant_text", text=params.get("delta", ""), data=data, session_id=thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)

        # —— 错误 ——
        if method == "error":
            turn_id = params.get("turnId", "")
            envelope_id = self._turn_to_envelope.get(turn_id)
            detail = json.dumps(params.get("error", params), ensure_ascii=False)
            if envelope_id and not params.get("willRetry", False):
                self._mark_delivery(envelope_id, "failed", detail)
            return SessionEvent(kind="error", text=detail, data=data, session_id=thread_id, cursor=cursor, delivery_id=envelope_id or "", turn_id=turn_id)

        # —— thread 生命周期（只记录内部状态，不暴露给 view） ——
        if method == "thread/started":
            return None  # 纯生命周期通知，对用户无意义

        if method == "thread/status/changed":
            return None  # 纯生命周期通知，对用户无意义

        # —— 管理信号（静默过滤,不产生事件） ——
        if method in self._ADMIN_METHODS:
            return None

        # —— 未知 method（保留兜底,打日志便于发现新的通知类型） ——
        if method:
            log.info("[codexDriver] unmapped notification method=%s", method)
            return SessionEvent(kind="state_change", text="", data=data, session_id=thread_id, cursor=cursor)
        return None

    def _replay_events_since(self, since: str) -> Iterator[SessionEvent]:
        if not self._thread_id:
            return
        try:
            turns = self._request(
                "thread/turns/list",
                {
                    "threadId": self._thread_id,
                    "limit": 200,
                    "itemsView": "full",
                    "sortDirection": "asc",
                },
                timeout=20,
            ).get("data", [])
        except Exception as exc:
            yield SessionEvent(
                kind="error",
                text=f"history replay failed: {exc}",
                data={"replay_error": str(exc), "since": since},
                session_id=self._thread_id,
                cursor=since,
            )
            return

        for ev in self._events_from_turn_history(turns):
            if self._cursor_after(ev.cursor, since):
                yield ev

    def _events_from_turn_history(self, turns: list[dict]) -> Iterator[SessionEvent]:
        for turn_index, turn in enumerate(turns):
            turn_id = turn.get("id", f"turn-{turn_index}")
            delivery_id = self._turn_to_envelope.get(turn_id, "")
            if not delivery_id:
                for item in turn.get("items", []):
                    if item.get("type") == "userMessage" and item.get("clientId"):
                        delivery_id = item.get("clientId", "")
                        self._remember_delivery(delivery_id, turn_id, self._envelope_states.get(delivery_id, "done"))
                        break
            base = f"turn:{turn_id}"
            yield SessionEvent(
                kind="state_change",
                text="",
                data={"raw_method": "history/turn", "change": "turn_started", "turn": turn},
                session_id=self._thread_id,
                cursor=f"{base}:event:0000",
                delivery_id=delivery_id,
                turn_id=turn_id,
            )
            for item_index, item in enumerate(turn.get("items", []), start=1):
                cursor = f"{base}:item:{item_index:04d}"
                item_type = item.get("type", "")
                data = {"raw_method": "history/item", "item": item, "turnId": turn_id}
                if item_type == "userMessage":
                    text = self._text_from_user_content(item.get("content", []))
                    yield SessionEvent(kind="state_change", text=text, data={**data, "change": "user_message"}, session_id=self._thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
                elif item_type == "agentMessage":
                    yield SessionEvent(kind="assistant_text", text=item.get("text", ""), data=data, session_id=self._thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
                elif item_type in ("commandExecution", "fileChange", "dynamicToolCall"):
                    yield SessionEvent(kind="tool_call", text=item.get("command", item_type), data=data, session_id=self._thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
                elif "error" in item_type.lower() or item.get("status") == "failed":
                    yield SessionEvent(kind="error", text=json.dumps(item, ensure_ascii=False), data=data, session_id=self._thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
                else:
                    yield SessionEvent(kind="state_change", text="", data={**data, "change": item_type or "item"}, session_id=self._thread_id, cursor=cursor, delivery_id=delivery_id, turn_id=turn_id)
            yield SessionEvent(
                kind="state_change",
                text="",
                data={"raw_method": "history/turn", "change": "turn_completed", "turn": turn, "status": turn.get("status")},
                session_id=self._thread_id,
                cursor=f"{base}:event:9999",
                delivery_id=delivery_id,
                turn_id=turn_id,
            )

    @staticmethod
    def _turn_event_cursor(turn_id: str, event_id: str) -> str:
        return f"turn:{turn_id}:event:{event_id}" if turn_id else ""

    @staticmethod
    def _turn_item_cursor(turn_id: str, item_index: int) -> str:
        return f"turn:{turn_id}:item:{item_index:04d}" if turn_id else ""

    @staticmethod
    def _cursor_after(cursor: str, since: str) -> bool:
        if not since:
            return True
        cursor_key = CodexProtocolDriver._cursor_key(cursor)
        since_key = CodexProtocolDriver._cursor_key(since)
        if cursor_key is not None and since_key is not None:
            return cursor_key > since_key
        if cursor.startswith("live:") and since.startswith("live:"):
            return cursor > since
        if cursor.startswith("turn:") and since.startswith("live:"):
            return False
        if cursor.startswith("live:") and since.startswith("turn:"):
            return True
        return cursor > since

    @staticmethod
    def _cursor_key(cursor: str) -> tuple | None:
        parts = cursor.split(":")
        if len(parts) != 4 or parts[0] != "turn":
            return None
        _, turn_id, kind, value = parts
        if kind == "event":
            order = 0 if value == "0000" else 999999
        elif kind == "item":
            try:
                order = int(value)
            except ValueError:
                order = 1
        else:
            order = 1
        return (0, turn_id, order)

    @staticmethod
    def _text_from_user_content(content: list) -> str:
        parts: list[str] = []
        for item in content:
            if isinstance(item, dict) and "text" in item:
                parts.append(str(item.get("text", "")))
        return "".join(parts)

    def _read_turn_state(self, turn_id: str, *, thread_id: str | None = None) -> tuple[ReceiptState, str]:
        result = self._request(
            "thread/turns/list",
            {"threadId": self._thread_id if thread_id is None else thread_id, "limit": 20, "itemsView": "full", "sortDirection": "desc"},
            timeout=15,
        )
        for turn in result.get("data", []):
            if turn.get("id") != turn_id:
                continue
            status = turn.get("status", "")
            if status == "completed":
                return "done", "turn status=completed"
            if status == "interrupted":
                return "failed", "turn status=interrupted"
            if status == "failed":
                return "failed", f"turn failed: {turn.get('error')}"
            if status == "inProgress":
                return "processing", "turn status=inProgress"
            return "unknown", f"turn status={status}"
        return "unknown", f"turn {turn_id} not found"

    def _done_receipt_after_event_drain(self, envelope_id: str, detail: str) -> DeliveryReceipt:
        if envelope_id in self._events_drained:
            self._mark_delivery(envelope_id, "done", detail)
            self._done_seen_at.pop(envelope_id, None)
            return DeliveryReceipt(id=envelope_id, state="done", source="protocol", detail=detail)

        first_seen = self._done_seen_at.setdefault(envelope_id, time.monotonic())
        elapsed = time.monotonic() - first_seen
        if elapsed < EVENT_DRAIN_TIMEOUT:
            self._envelope_states[envelope_id] = "processing"
            return DeliveryReceipt(
                id=envelope_id,
                state="processing",
                source="protocol",
                detail="protocol done, draining events",
            )

        incomplete_detail = f"{detail}; events may be incomplete after {elapsed:.1f}s drain timeout"
        log.warning("[delivery][EVENTS_INCOMPLETE] envelope=%s %s", envelope_id, incomplete_detail)
        self._mark_delivery(envelope_id, "done", incomplete_detail)
        self._done_seen_at.pop(envelope_id, None)
        return DeliveryReceipt(id=envelope_id, state="done", source="protocol", detail=incomplete_detail)

    def _record_published_event(self, event: SessionEvent) -> None:
        if event.data.get("change") != "turn_completed" or not event.delivery_id:
            return
        self._events_drained.add(event.delivery_id)
        status = event.data.get("status", "")
        state: ReceiptState = "done" if status == "completed" else "failed"
        detail = "turn status=interrupted" if status == "interrupted" else f"turn status={status}"
        self._mark_delivery(event.delivery_id, state, detail)
        self._done_seen_at.pop(event.delivery_id, None)

    def _wait_until_idle_for_delivery(self) -> str:
        deadline = time.monotonic() + self._stall_timeout
        while True:
            turn_id = self._active_turn_id or self._latest_in_progress_turn_id()
            if not turn_id:
                return ""
            try:
                state, detail = self._read_turn_state(turn_id)
            except Exception as exc:
                return f"failed while waiting for active turn {turn_id}: {exc}"
            if state in ("done", "failed"):
                if turn_id == self._active_turn_id:
                    self._active_turn_id = ""
                return ""
            if state == "unknown":
                self._active_turn_id = ""
                return ""
            if time.monotonic() > deadline:
                return f"session busy: active turn {turn_id} still {detail} after {self._stall_timeout:.0f}s"
            time.sleep(0.5)

    def _read_record_turn_state(self, turn_id: str, record: _DeliveryRecord | None) -> tuple[ReceiptState, str]:
        if record is not None and record.thread_id != self._thread_id:
            return self._read_turn_state(turn_id, thread_id=record.thread_id)
        return self._read_turn_state(turn_id)

    def _confirm_unrequested_interrupt(self, turn_id: str, record: _DeliveryRecord | None = None) -> tuple[ReceiptState, str]:
        """Avoid treating a transient active-turn snapshot as a terminal interrupt.

        Interruptions are only accepted immediately if this driver sent
        `turn/interrupt`. Otherwise, require the same persisted status twice.
        """
        time.sleep(1.0)
        state, detail = self._read_record_turn_state(turn_id, record)
        if state == "failed" and "interrupted" in detail.lower():
            return state, detail
        return state, detail

    def _latest_in_progress_turn_id(self) -> str:
        try:
            result = self._request(
                "thread/turns/list",
                {"threadId": self._thread_id, "limit": 5, "itemsView": "notLoaded", "sortDirection": "desc"},
                timeout=10,
            )
        except Exception:
            return ""
        for turn in result.get("data", []):
            if turn.get("status") == "inProgress":
                return turn.get("id", "")
        return ""

    def _remember_delivery(self, envelope_id: str, turn_id: str, state: ReceiptState) -> None:
        self._envelope_states[envelope_id] = state
        if turn_id:
            self._envelope_to_turn[envelope_id] = turn_id
            self._turn_to_envelope[turn_id] = envelope_id

    def _mark_delivery(self, envelope_id: str, state: ReceiptState, detail: str) -> None:
        turn_id = self._envelope_to_turn.get(envelope_id, "")
        self._envelope_states[envelope_id] = state
        record = self._store.get_by_envelope(envelope_id)
        if turn_id or record is not None:
            thread_id = record.thread_id if record is not None else self._thread_id
            if thread_id:
                self._store.update_state(thread_id, envelope_id, state, detail)

    def _sync_thread_alias(self) -> None:
        if self._session_id and self._session_id != self._thread_id:
            self._thread_id = self._session_id
        elif self._thread_id and not self._session_id:
            self._session_id = self._thread_id


if __name__ == "__main__":
    import sys

    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
    mode = sys.argv[1] if len(sys.argv) > 1 else "direct"
    driver = CodexProtocolDriver(transport_mode=mode, workdir=DEFAULT_WORKDIR, stall_timeout=20)
    driver.start()
    eid = f"smoke-{int(time.time())}"
    print("deliver", driver.deliver(eid, "Reply exactly: CODEX_DRIVER_ACK"))
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        print("receipt", driver.poll_receipt(eid))
        if driver.poll_receipt(eid).state in ("done", "failed"):
            break
        time.sleep(2)
    start = time.monotonic()
    for ev in driver.events():
        print(ev)
        if time.monotonic() - start > 5:
            break
    driver.stop()
