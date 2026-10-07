"""MCP stdio shim skeleton (B1) — JSON‑RPC 2.0 over stdin/stdout.

Architecture (07‑31 张宗楚定):
  CLI --[stdio JSON‑RPC]--> mcp_shim --[local IPC]--> sessionHelper --[WSS]--> hub

This file provides the MCP-facing half:
  • stdio read/write loop
  • JSON‑RPC 2.0 message dispatch
  • tool list / tool call handlers
  • an IPC stub that forwards sends to the real sessionHelper (B2 flesh-out).

The shim does NOT:
  • open its own network connections
  • manage authentication (identity is the already-authenticated sessionHelper)
  • replace passive receive (pushing messages into a sleeping CLI stays with helper)
"""

from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from typing import Any, Callable


# ── JSON‑RPC 2.0 types ────────────────────────────────────────────────

@dataclass
class Request:
    jsonrpc: str
    method: str
    params: dict[str, Any] | None
    id: int | str | None


@dataclass
class Response:
    id: int | str | None
    result: Any | None = None
    error: dict[str, Any] | None = None


# ── tool registry ─────────────────────────────────────────────────────

@dataclass
class ToolDef:
    name: str
    description: str
    input_schema: dict[str, Any]


_tools: dict[str, ToolDef] = {}
_handlers: dict[str, Callable[[dict[str, Any]], Any]] = {}


def register(name: str, description: str, input_schema: dict[str, Any]):
    def decorator(fn: Callable[[dict[str, Any]], Any]):
        _tools[name] = ToolDef(name, description, input_schema)
        _handlers[name] = fn
        return fn
    return decorator


# ── IPC client (B2: real Unix-socket call to sessionHelper) ──────────

import socket as _socket
import uuid as _uuid

_IPC_SOCK = os.environ.get("DINGWEI_MCP_IPC_SOCK", "")
_IPC_TIMEOUT = 5.0


def _ipc_send(payload: dict[str, Any]) -> dict[str, Any]:
    """Forward a validated send request to the live sessionHelper via a
    local Unix-domain socket, then return the helperʼs outcome."""
    payload["request_id"] = _uuid.uuid4().hex[:12]
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n"

    sock = _socket.socket(_socket.AF_UNIX, _socket.SOCK_STREAM)
    sock.settimeout(_IPC_TIMEOUT)
    try:
        sock.connect(_IPC_SOCK)
        sock.sendall(body)
        chunks: list[bytes] = []
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            chunks.append(chunk)
            if b"\n" in chunk:
                break
        response = json.loads(b"".join(chunks).decode("utf-8"))
    finally:
        sock.close()

    if not response.get("ok"):
        raise RuntimeError(response.get("error", "IPC send failed"))
    return response.get("outcome", response)


# ── IPC vsapi_v1 (T18: generic datad pipe) ────────────────────────────

def _ipc_vsapi(method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "type": "vsapi_v1",
        "request_id": _uuid.uuid4().hex[:12],
        "method": method,
        "path": path,
    }
    if body is not None:
        payload["body"] = body
    sock = _socket.socket(_socket.AF_UNIX, _socket.SOCK_STREAM)
    sock.settimeout(_IPC_TIMEOUT)
    try:
        sock.connect(_IPC_SOCK)
        sock.sendall(json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n")
        chunks: list[bytes] = []
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            chunks.append(chunk)
            if b"\n" in chunk:
                break
        response = json.loads(b"".join(chunks).decode("utf-8"))
    finally:
        sock.close()
    if not response.get("ok"):
        raise RuntimeError(response.get("error", "vsapi call failed"))
    return response.get("result", response)


# ── MCP tools: send_message + T18 ─────────────────────────────────────

_SEND_TOOL_SCHEMA = {
    "type": "object",
    "properties": {
        "to": {
            "type": "string",
            "description": "接收方全名(如 zzc-manager-2642), 缺 #key 由 helper 补全",
        },
        "body": {
            "type": "string",
            "description": "消息正文(Markdown), 首行冠字头如 [发件全名→收件全名]",
        },
        "file": {
            "type": "string",
            "description": "可选: 文本文件绝对路径, --file 仅二选一(body/file)",
        },
        "reply_to": {
            "type": "string",
            "description": "可选: 回复目标地址(默认=origin), 填入 envelope meta.reply_to",
        },
        "in_reply_to": {
            "type": "string",
            "description": "可选: 本消息回复的 message_id(业务关联, 非 ACK), 填入 meta.in_reply_to",
        },
        "reply_policy": {
            "type": "string",
            "enum": ["expect_reply", "no_reply", "ack_only"],
            "description": "可选: 收方处理后是否产生 content-reply, 缺省不产 auto-reply(保守)",
        },
        "kind": {
            "type": "string",
            "enum": ["content", "reply", "ack", "mirror"],
            "description": "可选: 消息类型, 默认 content",
        },
    },
    "required": [],
}


@register("send_message", "经 DingWei hub 向队友发送消息", _SEND_TOOL_SCHEMA)
def _send_message(args: dict[str, Any]) -> dict[str, Any]:
    """Validate and forward."""
    to = (args.get("to") or "").strip()
    body = (args.get("body") or "").strip()
    file_path = (args.get("file") or "").strip()

    if not to:
        raise ValueError("to is required")
    if len(to) > 512:
        raise ValueError("to exceeds 512 chars")

    provided = sum(1 for v in (body, file_path) if v)
    if provided != 1:
        raise ValueError("exactly one of body or file is required")

    # optional reply-origin envelope fields (design v2.5 §9)
    reply_to = (args.get("reply_to") or "").strip()
    in_reply_to = (args.get("in_reply_to") or "").strip()
    reply_policy = (args.get("reply_policy") or "").strip()
    kind = (args.get("kind") or "").strip()

    for name, val in [("reply_policy", reply_policy), ("kind", kind)]:
        if val and val not in _SEND_TOOL_SCHEMA["properties"][name].get("enum", []):
            raise ValueError(f"{name} must be one of {_SEND_TOOL_SCHEMA['properties'][name]['enum']}")

    payload: dict[str, Any] = {"to": to, "body": body}
    if reply_to:
        payload["reply_to"] = reply_to
    if in_reply_to:
        payload["in_reply_to"] = in_reply_to
    if reply_policy:
        payload["reply_policy"] = reply_policy
    if kind:
        payload["kind"] = kind
    if file_path:
        try:
            with open(file_path, encoding="utf-8") as fh:
                payload["body"] = fh.read()
            if len(payload["body"]) > 64 * 1024:
                raise ValueError("file exceeds 64 KiB")
        except OSError as exc:
            raise ValueError(f"cannot read file: {exc}") from exc

    return _ipc_send(payload)


# ── T18 MCP tool: heartbeat_report ────────────────────────────────────

@register("heartbeat_report", "向 datad 自报心跳(状态+任务+卡点)", {
    "type": "object",
    "properties": {
        "status": {"type": "string", "enum": ["active", "busy", "blocked", "idle"]},
        "task": {"type": "string", "description": "当前任务简述"},
        "eta": {"type": "string", "description": "预计完成时间(可选)"},
        "blocker": {"type": "string", "description": "卡点说明(可选)"},
    },
    "required": ["status"],
})
def _heartbeat_report(args: dict[str, Any]) -> dict[str, Any]:
    status = (args.get("status") or "").strip()
    if status not in ("active", "busy", "blocked", "idle"):
        raise ValueError("status must be active/busy/blocked/idle")
    body: dict[str, Any] = {"status": status}
    for field in ("task", "eta", "blocker"):
        val = (args.get(field) or "").strip()
        if val:
            body[field] = val
    return _ipc_vsapi("POST", "/v1/sessions/me/heartbeat", body)


# ── T18 MCP tool: project_context ─────────────────────────────────────

@register("project_context", "拉取项目上下文(任务/名册/配置)", {
    "type": "object",
    "properties": {
        "scope": {"type": "string", "enum": ["tasks", "sessions", "config", "all"]},
    },
    "required": ["scope"],
})
def _project_context(args: dict[str, Any]) -> dict[str, Any]:
    scope = (args.get("scope") or "").strip()
    if scope not in ("tasks", "sessions", "config", "all"):
        raise ValueError("scope must be tasks/sessions/config/all")
    paths: list[str] = []
    if scope in ("tasks", "all"):
        paths.append("/v1/tasks?owner=me")
    if scope in ("sessions", "all"):
        paths.append("/v1/sessions")
    if scope in ("config", "all"):
        paths.append("/v1/config")
    results: dict[str, Any] = {}
    for path in paths:
        key = path.split("?")[0].split("/")[-1]
        results[key] = _ipc_vsapi("GET", path)
    return results


# ── MCP tool: upload_file (R-V2-AGENT-FILE-UPLOAD) ────────────────────
# Same wire contract as dingwei_file.upload_file; duplicated because this file is a
# self-contained entry point (no package imports). The shim is a child of the CLI, so the
# file is opened with the Agent's own permissions and only the descriptor crosses IPC.

def _ipc_upload(path: str, timeout: float = 300.0) -> dict[str, Any]:
    import array as _array

    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0) | getattr(os, "O_CLOEXEC", 0)
    fd = os.open(path, flags)
    try:
        payload = {"type": "file_upload_v1", "request_id": _uuid.uuid4().hex[:12], "name": os.path.basename(path)}
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n"
        sock = _socket.socket(_socket.AF_UNIX, _socket.SOCK_STREAM)
        sock.settimeout(timeout)
        try:
            sock.connect(_IPC_SOCK)
            sent = sock.sendmsg([data], [(_socket.SOL_SOCKET, _socket.SCM_RIGHTS, _array.array("i", [fd]))])
            if sent < len(data):
                sock.sendall(data[sent:])
            reply = bytearray()
            while b"\n" not in reply:
                chunk = sock.recv(4096)
                if not chunk:
                    break
                reply.extend(chunk)
        finally:
            sock.close()
    finally:
        os.close(fd)
    response = json.loads(bytes(reply).decode("utf-8"))
    if not response.get("ok"):
        reason = response.get("reason")
        raise RuntimeError(response.get("error", "file upload failed") + (f" ({reason})" if reason else ""))
    return response["result"]


@register("upload_file", "上传工作区内的文件，返回公开 7 天的下载链接（不要上传凭据、令牌、密钥、登录态文件）", {
    "type": "object",
    "properties": {"path": {"type": "string", "description": "要上传的文件路径（工作区内、非点目录）"}},
    "required": ["path"],
})
def _upload_file(args: dict[str, Any]) -> dict[str, Any]:
    path = (args.get("path") or "").strip()
    if not path:
        raise ValueError("path is required")
    return _ipc_upload(path)


# ── JSON‑RPC message dispatch ────────────────────────────────────────

def _dispatch(req: Request) -> Response:
    """Route a single request to the correct handler."""

    # -- tools/list ----------------------------------------------------
    if req.method == "tools/list":
        tools = [
            {
                "name": t.name,
                "description": t.description,
                "inputSchema": t.input_schema,
            }
            for t in _tools.values()
        ]
        return Response(id=req.id, result={"tools": tools})

    # -- tools/call ----------------------------------------------------
    if req.method == "tools/call":
        name = (req.params or {}).get("name", "")
        arguments = (req.params or {}).get("arguments") or {}
        if not isinstance(name, str) or name not in _handlers:
            return Response(
                id=req.id,
                error={"code": -32601, "message": f"unknown tool: {name!r}"},
            )
        try:
            result = _handlers[name](dict(arguments))
            return Response(id=req.id, result={"content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]})
        except (TypeError, ValueError) as exc:
            return Response(
                id=req.id,
                error={"code": -32602, "message": str(exc)},
            )

    # -- initialize ----------------------------------------------------
    if req.method == "initialize":
        return Response(id=req.id, result={
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "dingwei-mcp-shim", "version": "0.1.0"},
        })

    # -- unknown method ------------------------------------------------
    return Response(id=req.id, error={"code": -32601, "message": f"unknown method: {req.method!r}"})


def _write(response: Response) -> None:
    """Write one JSON‑RPC response (with terminating newline) to stdout."""
    body: dict[str, Any] = {"jsonrpc": "2.0", "id": response.id}
    if response.error is not None:
        body["error"] = response.error
    else:
        body["result"] = response.result
    sys.stdout.write(json.dumps(body, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def serve() -> None:
    """Blocking stdio read loop — read one line, dispatch, write response."""
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            raw = json.loads(line)
        except json.JSONDecodeError:
            _write(Response(id=None, error={"code": -32700, "message": "parse error"}))
            continue

        req = Request(
            jsonrpc=raw.get("jsonrpc", ""),
            method=raw.get("method", ""),
            params=raw.get("params"),
            id=raw.get("id"),
        )
        if req.jsonrpc != "2.0":
            _write(Response(id=req.id, error={"code": -32600, "message": "invalid request"}))
            continue

        # notifications (no id) are silently dropped
        if req.id is None:
            continue

        _write(_dispatch(req))


if __name__ == "__main__":
    serve()
