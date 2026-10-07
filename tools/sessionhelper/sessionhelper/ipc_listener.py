"""Local IPC server that the MCP shim calls when an Agent invokes send_message.

Architecture:
  MCP shim --[stdio JSON-RPC]--> mcp_shim.py
  mcp_shim  --[Unix socket]---> ipc_listener.py  (this file)
  ipc_listener --[existing WSS]--> hub

The listener is a thin dispatch loop: receive, validate, hand to the
existing WSS send path, return outcome.  It does NOT open its own network
connections and does NOT manage authentication — the owning sessionHelper
is already authenticated.
"""

from __future__ import annotations

import array
import asyncio
import json
import os
import socket
from typing import Any, Callable, Awaitable

from .file_upload import UploadRejected


# ── message type ──────────────────────────────────────────────────────

IPC_SEND = "mcp_send"           # request: {type, to, body? , file? , request_id,
                                #           reply_to?, in_reply_to?, reply_policy?, kind?}
IPC_VSAPI = "vsapi_v1"         # request: {type, method, path, body?, request_id}
IPC_FILE = "file_access_v1"    # request: {type, file_ref, method=HEAD|GET, range?}
IPC_UPLOAD = "file_upload_v1"  # request: {type, request_id, name?, media_type?} + exactly one fd (SCM_RIGHTS)
                               # R-V2-AGENT-FILE-UPLOAD：helper 不接受、不解析路径；文件由 Agent 进程自己打开后交 fd。
MAX_PASSED_FDS = 4

# reply-origin 契约(shence 08-03 定):shim IPC payload 顶层四个 optional key,
# 与 to/body 并列、无嵌套;存在才取,原样塞进出站 envelope.meta 同名 key。
REPLY_ORIGIN_KEYS = ("reply_to", "in_reply_to", "reply_policy", "kind")
SEND_META_KEYS = REPLY_ORIGIN_KEYS + ("ipc_body_amr_v1",)


def extract_reply_origin_meta(msg: dict[str, Any]) -> dict[str, Any]:
    """Pick the four optional top-level reply-origin keys out of an IPC
    request.  Present-and-non-null values only; nothing else is copied."""
    return {k: msg[k] for k in SEND_META_KEYS if msg.get(k) is not None}


# ── helpers ───────────────────────────────────────────────────────────

class _IPCError(Exception):
    """Non-fatal IPC protocol error — the connection is dropped but the
    server continues accepting new connections."""


async def _read_one(reader: asyncio.StreamReader, timeout: float = 5.0) -> dict[str, Any]:
    """Read one newline-delimited JSON object."""
    try:
        line = await asyncio.wait_for(reader.readline(), timeout)
    except asyncio.TimeoutError:
        raise _IPCError("read timeout") from None
    if not line:
        raise _IPCError("client disconnected")
    try:
        return json.loads(line.decode("utf-8"))
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise _IPCError(f"bad json: {exc}") from exc


async def _write_one(writer: asyncio.StreamWriter, payload: dict[str, Any]) -> None:
    """Write one newline-delimited JSON response."""
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n"
    writer.write(data)
    await writer.drain()


# ── IPC server ─────────────────────────────────────────────────────────

_SendHandler = Callable[[str, str, dict[str, Any]], Awaitable[dict[str, Any]]]
_VsapiHandler = Callable[[str, str, dict[str, Any] | None], Awaitable[dict[str, Any]]]
_FileHandler = Callable[[str, str, str], Awaitable[dict[str, Any]]]
_UploadHandler = Callable[[int, str, str], Awaitable[dict[str, Any]]]


async def _handle_connection(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
    file_handler: _FileHandler | None = None,
    *,
    allowed_socket_dir: str | None = None,
    upload_handler: _UploadHandler | None = None,
    passed_fds: list[int] | None = None,
    upload_only: bool = False,
) -> None:
    """Serve a single Unix-socket connection: read one request, dispatch by type, ack.

    *passed_fds* are descriptors received with the request (SCM_RIGHTS); the caller owns and closes them.
    """
    try:
        msg = await _read_one(reader)
    except _IPCError as exc:
        await _write_one(writer, {"ok": False, "error": str(exc)})
        return

    msg_type = msg.get("type", "")

    if msg_type == IPC_UPLOAD:
        return await _handle_upload(writer, msg, upload_handler, passed_fds or [])
    if upload_only:
        # 托管席（组长 R-V2-IMAGE-R5-UPLOAD-ONLY）：本 socket 只为上传而开；发信／vsapi／读文件不经此提供。
        await _write_one(writer, {"ok": False, "error": "unavailable: this socket accepts file_upload_v1 only"})
        return
    if passed_fds:
        await _write_one(writer, {"ok": False, "error": "file descriptors are only accepted for file_upload_v1"})
        return
    if msg_type == IPC_VSAPI:
        return await _handle_vsapi(writer, msg, vsapi_handler)
    if msg_type == IPC_FILE:
        return await _handle_file(writer, msg, file_handler)
    if msg_type != IPC_SEND:
        await _write_one(writer, {"ok": False, "error": f"unknown type {msg_type!r}"})
        return

    to = (msg.get("to") or "").strip()
    body = (msg.get("body") or "").strip()
    file_path = (msg.get("file") or "").strip()
    request_id = msg.get("request_id", "")
    meta = extract_reply_origin_meta(msg)

    if not to:
        await _write_one(writer, {"ok": False, "error": "to is required"})
        return
    if not body and not file_path:
        await _write_one(writer, {"ok": False, "error": "body or file is required"})
        return
    if body and file_path:
        await _write_one(writer, {"ok": False, "error": "exactly one of body or file"})
        return

    if file_path:
        resolved = os.path.abspath(file_path)
        if allowed_socket_dir and not resolved.startswith(allowed_socket_dir.rstrip("/") + "/"):
            await _write_one(writer, {"ok": False, "error": "file path not in allowed directory"})
            return
        try:
            with open(resolved, encoding="utf-8") as fh:
                body = fh.read()
        except OSError as exc:
            await _write_one(writer, {"ok": False, "error": f"cannot read file: {exc}"})
            return
        if len(body) > 64 * 1024:
            await _write_one(writer, {"ok": False, "error": "file exceeds 64 KiB"})
            return

    try:
        outcome = await send_handler(to, body, meta)
    except Exception as exc:
        await _write_one(writer, {"ok": False, "error": f"send failed: {exc}"})
        return

    await _write_one(writer, {"ok": True, "outcome": outcome, "request_id": request_id})


async def _handle_vsapi(
    writer: asyncio.StreamWriter,
    msg: dict[str, Any],
    vsapi_handler: _VsapiHandler,
) -> None:
    """Dispatch a vsapi_v1 request to datad via the helper pipe."""
    method = (msg.get("method") or "GET").strip().upper()
    path = (msg.get("path") or "").strip()
    body = msg.get("body")
    request_id = msg.get("request_id", "")
    if method not in ("GET", "POST", "PUT", "PATCH", "DELETE"):
        await _write_one(writer, {"ok": False, "error": f"unsupported method {method}"})
        return
    if not path.startswith("/"):
        await _write_one(writer, {"ok": False, "error": "path must start with /"})
        return
    try:
        result = await vsapi_handler(method, path, body)
    except Exception as exc:
        await _write_one(writer, {"ok": False, "error": f"vsapi call failed: {exc}"})
        return
    await _write_one(writer, {"ok": True, "result": result, "request_id": request_id})


_FILE_RESULT_KEYS = {
    "HEAD": {"file_ref", "size", "sha256", "media_type", "expires_at", "range"},
    "GET": {"file_ref", "content_base64", "content_range", "sha256", "total_size"},
}
_FILE_RESULT_REQUIRED = {
    "HEAD": {"file_ref", "size", "sha256"},
    "GET": {"file_ref", "content_base64", "content_range", "sha256", "total_size"},
}


async def _handle_file(
    writer: asyncio.StreamWriter,
    msg: dict[str, Any],
    file_handler: _FileHandler | None,
) -> None:
    """Run file access inside helper; raw bearer values never cross IPC."""
    file_ref = str(msg.get("file_ref") or "").strip()
    method = str(msg.get("method") or "").strip().upper()
    byte_range = str(msg.get("range") or "").strip()
    request_id = str(msg.get("request_id") or "")
    if file_handler is None:
        await _write_one(writer, {"ok": False, "error": "file access unavailable"})
        return
    if not file_ref.startswith("dwfile:") or len(file_ref) > 256 or method not in _FILE_RESULT_KEYS:
        await _write_one(writer, {"ok": False, "error": "invalid file request"})
        return
    if (method == "HEAD" and byte_range) or (method == "GET" and not _single_range(byte_range)):
        await _write_one(writer, {"ok": False, "error": "invalid file request"})
        return
    try:
        result = await file_handler(file_ref, method, byte_range)
    except Exception:
        # Handler errors may contain Authorization headers or backend URLs.
        await _write_one(writer, {"ok": False, "error": "file access failed"})
        return
    if (not isinstance(result, dict)
            or not _FILE_RESULT_REQUIRED[method].issubset(result)
            or not set(result).issubset(_FILE_RESULT_KEYS[method])
            or result.get("file_ref") != file_ref):
        await _write_one(writer, {"ok": False, "error": "unsafe file response"})
        return
    await _write_one(writer, {"ok": True, "result": result, "request_id": request_id})


_UPLOAD_RESULT_KEYS = {"url", "name", "size", "sha256", "expires_at"}


async def _handle_upload(
    writer: asyncio.StreamWriter,
    msg: dict[str, Any],
    upload_handler: _UploadHandler | None,
    passed_fds: list[int],
) -> None:
    """Upload the file behind the passed descriptor; tickets and backend details never cross IPC."""
    request_id = str(msg.get("request_id") or "")
    if upload_handler is None:
        await _write_one(writer, {"ok": False, "error": "file upload unavailable"})
        return
    if any(key in msg for key in ("path", "file", "file_path")):
        await _write_one(writer, {"ok": False, "error": "paths are not accepted; pass an open file descriptor"})
        return
    if len(passed_fds) != 1:
        await _write_one(writer, {"ok": False, "error": "exactly one file descriptor is required"})
        return
    name = str(msg.get("name") or "")
    media_type = str(msg.get("media_type") or "")
    if len(name) > 255 or "/" in name or "\0" in name or len(media_type) > 255:
        await _write_one(writer, {"ok": False, "error": "invalid file upload request"})
        return
    try:
        result = await upload_handler(passed_fds[0], name, media_type)
    except UploadRejected as exc:
        await _write_one(writer, {"ok": False, "error": "file upload rejected", "reason": exc.code})
        return
    except Exception:
        # Handler errors may contain the ticket, Authorization header or backend URL.
        await _write_one(writer, {"ok": False, "error": "file upload failed"})
        return
    if not isinstance(result, dict) or set(result) != _UPLOAD_RESULT_KEYS:
        await _write_one(writer, {"ok": False, "error": "unsafe file response"})
        return
    await _write_one(writer, {"ok": True, "result": result, "request_id": request_id})


def _single_range(value: str) -> bool:
    if not value.startswith("bytes=") or "," in value:
        return False
    bounds = value[6:].split("-")
    if len(bounds) != 2 or not any(bounds):
        return False
    return all(not part or (part.isascii() and part.isdigit()) for part in bounds)


class _SocketWriter:
    """Minimal StreamWriter stand-in over a raw non-blocking socket (write + drain only)."""

    def __init__(self, loop: asyncio.AbstractEventLoop, conn: socket.socket):
        self._loop, self._conn, self._buf = loop, conn, bytearray()

    def write(self, data: bytes) -> None:
        self._buf.extend(data)

    async def drain(self) -> None:
        if self._buf:
            data, self._buf = bytes(self._buf), bytearray()
            await self._loop.sock_sendall(self._conn, data)


def _fds_from_ancillary(ancdata) -> list[int]:
    fds: list[int] = []
    for level, kind, data in ancdata:
        if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
            arr = array.array("i")
            arr.frombytes(data[: len(data) - (len(data) % arr.itemsize)])
            fds.extend(arr)
    return fds


async def _recvmsg(loop: asyncio.AbstractEventLoop, conn: socket.socket):
    flags = getattr(socket, "MSG_CMSG_CLOEXEC", 0)
    while True:
        try:
            return conn.recvmsg(65536, socket.CMSG_SPACE(MAX_PASSED_FDS * array.array("i").itemsize), flags)
        except (BlockingIOError, InterruptedError):
            ready = loop.create_future()
            loop.add_reader(conn.fileno(), lambda: ready.done() or ready.set_result(None))
            try:
                await ready
            finally:
                loop.remove_reader(conn.fileno())


async def _recv_request(loop: asyncio.AbstractEventLoop, conn: socket.socket, fds: list[int]) -> bytes:
    """Read up to the first newline; every descriptor received on the way is appended to *fds*."""
    buf = bytearray()
    while b"\n" not in buf:
        data, ancdata, msg_flags, _ = await _recvmsg(loop, conn)
        fds.extend(_fds_from_ancillary(ancdata))
        if msg_flags & getattr(socket, "MSG_CTRUNC", 0):
            raise _IPCError("too many file descriptors")
        if not data:
            break
        buf.extend(data)
        if len(buf) > 1024 * 1024:
            raise _IPCError("request too large")
    return bytes(buf.split(b"\n", 1)[0] + b"\n") if b"\n" in buf else bytes(buf)


async def _serve_socket_connection(conn: socket.socket, dispatch) -> None:
    loop = asyncio.get_running_loop()
    fds: list[int] = []
    try:
        conn.setblocking(False)
        writer = _SocketWriter(loop, conn)
        try:
            line = await asyncio.wait_for(_recv_request(loop, conn, fds), 5.0)
        except asyncio.TimeoutError:
            await _write_one(writer, {"ok": False, "error": "read timeout"})
            return
        except _IPCError as exc:
            await _write_one(writer, {"ok": False, "error": str(exc)})
            return
        reader = asyncio.StreamReader()
        reader.feed_data(line)
        reader.feed_eof()
        await dispatch(reader, writer, fds)
    except (OSError, ConnectionError):
        pass
    finally:
        for fd in fds:
            try:
                os.close(fd)
            except OSError:
                pass
        conn.close()


async def serve_ipc(
    socket_path: str,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
    file_handler: _FileHandler | None = None,
    *,
    allowed_socket_dir: str | None = None,
    upload_handler: _UploadHandler | None = None,
    bound: asyncio.Event | None = None,
    upload_only: bool = False,
) -> None:
    """Start a Unix-domain socket server.

    *socket_path*    – filesystem path for the listening socket.
    *send_handler*   – async callable ``(to: str, body: str, meta: dict) -> dict``
                       that actually sends the message via the authenticated WSS;
                       *meta* carries the optional reply-origin fields
                       (reply_to / in_reply_to / reply_policy / kind).
    *vsapi_handler*  – async callable ``(method, path, body?) -> dict`` that
                       forwards a vsapi_v1 request to datad with auth token.
    *file_handler*   – async callable that obtains and consumes the short-lived
                       bearer inside helper, returning only bounded metadata/bytes.
    *upload_handler* – async callable ``(fd, name, media_type) -> dict`` for
                       file_upload_v1; *fd* arrives via SCM_RIGHTS.
    *upload_only*    – accept only file_upload_v1; every other type answers "unavailable"
                       (hosted seats, SH_IPC_UPLOAD_ONLY=1).
    *bound*          – set once the socket is listening, cleared when the server stops
                       (a leftover socket file is not proof that anyone is listening).

    Connections are served on a raw socket (not asyncio streams) so that
    descriptors passed with SCM_RIGHTS are received instead of silently dropped.
    """
    if os.path.exists(socket_path):
        os.unlink(socket_path)

    async def dispatch(reader, writer, fds):
        await _handle_connection(reader, writer, send_handler, vsapi_handler, file_handler,
                                 allowed_socket_dir=allowed_socket_dir,
                                 upload_handler=upload_handler, passed_fds=fds, upload_only=upload_only)

    loop = asyncio.get_running_loop()
    listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    tasks: set[asyncio.Task] = set()
    try:
        listener.bind(socket_path)
        listener.listen(64)
        listener.setblocking(False)
        print(f"[ipc_listener] listening on {socket_path}", flush=True)
        if bound is not None:
            bound.set()
        while True:
            conn, _ = await loop.sock_accept(listener)
            task = asyncio.create_task(_serve_socket_connection(conn, dispatch))
            tasks.add(task)
            task.add_done_callback(tasks.discard)
    finally:
        if bound is not None:
            bound.clear()
        listener.close()
        for task in list(tasks):
            task.cancel()
