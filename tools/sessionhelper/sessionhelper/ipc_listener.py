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

import asyncio
import json
import os
from typing import Any, Callable, Awaitable


# ── message type ──────────────────────────────────────────────────────

IPC_SEND = "mcp_send"           # request: {type, to, body? , file? , request_id,
                                #           reply_to?, in_reply_to?, reply_policy?, kind?}
IPC_VSAPI = "vsapi_v1"         # request: {type, method, path, body?, request_id}
IPC_FILE = "file_access_v1"    # request: {type, file_ref, method=HEAD|GET, range?}

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


async def _handle_connection(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
    file_handler: _FileHandler | None = None,
    *,
    allowed_socket_dir: str | None = None,
) -> None:
    """Serve a single Unix-socket connection: read one request, dispatch by type, ack."""
    try:
        msg = await _read_one(reader)
    except _IPCError as exc:
        await _write_one(writer, {"ok": False, "error": str(exc)})
        return

    msg_type = msg.get("type", "")

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


def _single_range(value: str) -> bool:
    if not value.startswith("bytes=") or "," in value:
        return False
    bounds = value[6:].split("-")
    if len(bounds) != 2 or not any(bounds):
        return False
    return all(not part or (part.isascii() and part.isdigit()) for part in bounds)


async def serve_ipc(
    socket_path: str,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
    file_handler: _FileHandler | None = None,
    *,
    allowed_socket_dir: str | None = None,
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
    """
    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = await asyncio.start_unix_server(
        lambda r, w: _handle_connection(r, w, send_handler, vsapi_handler, file_handler,
                                        allowed_socket_dir=allowed_socket_dir),
        path=socket_path,
    )
    addr = server.sockets[0].getsockname() if server.sockets else socket_path
    print(f"[ipc_listener] listening on {addr}", flush=True)

    async with server:
        await server.serve_forever()
