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

IPC_SEND = "mcp_send"           # request: {type, to, body?, file?, request_id}
IPC_VSAPI = "vsapi_v1"         # request: {type, method, path, body?, request_id}


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

_SendHandler = Callable[[str, str], Awaitable[dict[str, Any]]]
_VsapiHandler = Callable[[str, str, dict[str, Any] | None], Awaitable[dict[str, Any]]]


async def _handle_connection(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
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
    if msg_type != IPC_SEND:
        await _write_one(writer, {"ok": False, "error": f"unknown type {msg_type!r}"})
        return

    to = (msg.get("to") or "").strip()
    body = (msg.get("body") or "").strip()
    file_path = (msg.get("file") or "").strip()
    request_id = msg.get("request_id", "")

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
        outcome = await send_handler(to, body)
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


async def serve_ipc(
    socket_path: str,
    send_handler: _SendHandler,
    vsapi_handler: _VsapiHandler | None = None,
    *,
    allowed_socket_dir: str | None = None,
) -> None:
    """Start a Unix-domain socket server.

    *socket_path*    – filesystem path for the listening socket.
    *send_handler*   – async callable ``(to: str, body: str) -> dict`` that
                       actually sends the message via the authenticated WSS.
    *vsapi_handler*  – async callable ``(method, path, body?) -> dict`` that
                       forwards a vsapi_v1 request to datad with auth token.
    """
    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = await asyncio.start_unix_server(
        lambda r, w: _handle_connection(r, w, send_handler, vsapi_handler,
                                        allowed_socket_dir=allowed_socket_dir),
        path=socket_path,
    )
    addr = server.sockets[0].getsockname() if server.sockets else socket_path
    print(f"[ipc_listener] listening on {addr}", flush=True)

    async with server:
        await server.serve_forever()
