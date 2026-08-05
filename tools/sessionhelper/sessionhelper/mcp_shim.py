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
import time
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


# ── Online roster helpers (send_smart) ─────────────────────────────────

import re as _re

_SESSION_NAME_RE = _re.compile(r"^([a-z0-9]+)-([a-z0-9]+)-([0-9a-f]{4})$")
_ROSTER_LINE_RE = _re.compile(
    r"^\d+\.\s+#(?P<short>[^\s]+)\s+.*?全名:(?P<full>[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4})"
)
_ADDR_RE = _re.compile(r"^@([a-z0-9]+)#([a-z0-9]+)$")


def _roster_path() -> str:
    """Compute the online roster file path from session name env vars."""
    session = os.environ.get("SH_SESSION_NAME", "").strip()
    # normalize: if full name like zzc-shencedeepseek-1111, extract short name
    parsed = _SESSION_NAME_RE.match(session)
    short = parsed.group(2) if parsed else session
    base = os.path.expanduser(os.path.join("~", ".dingwei"))
    return os.path.join(base, f"{short}.DingWeiOnlineSessions.list")


def _read_roster() -> list[dict[str, str]]:
    """Parse the online roster file. Returns list of {short, full, addr}."""
    path = _roster_path()
    if not os.path.exists(path):
        return []
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except OSError:
        return []
    sessions: list[dict[str, str]] = []
    for line in text.splitlines():
        m = _ROSTER_LINE_RE.search(line)
        if m:
            full = m.group("full")
            short = m.group("short")
            # Canonical address: full session name (hub resolves #key internally)
            sessions.append({"short": short, "full": full, "addr": full})
    return sessions


def _resolve_recipient(to: str) -> str:
    """Resolve a recipient from short name, full name, or full address.

    Returns the resolved address (usable as the `to` field in _ipc_send),
    or raises ValueError with disambiguation guidance.
    """
    to = to.strip()
    # Already a full address: contains '#'
    if "#" in to:
        return to

    sessions = _read_roster()
    if not sessions:
        raise ValueError(
            "在线清单为空或不可读。请确认 sessionHelper 已连接且清单文件已刷新。"
        )

    # Try exact short name match
    short_matches = [s for s in sessions if s["short"] == to]
    if len(short_matches) == 1:
        return short_matches[0]["addr"]

    # Try exact full name match
    full_matches = [s for s in sessions if s["full"] == to]
    if len(full_matches) == 1:
        return full_matches[0]["addr"]

    # Try substring match (short name contains or is contained)
    fuzzy_short = [s for s in sessions if to in s["short"] or s["short"] in to]
    fuzzy_full = [s for s in sessions if to in s["full"] or s["full"] in to]
    fuzzy = list({s["full"]: s for s in fuzzy_short + fuzzy_full}.values())

    if len(short_matches) > 1 or len(full_matches) > 1:
        names = ", ".join(s["full"] for s in (short_matches or full_matches))
        raise ValueError(f"多个会话匹配 '{to}'，请指定完整全名: {names}")
    if len(fuzzy) == 1:
        return fuzzy[0]["addr"]
    if len(fuzzy) > 1:
        names = ", ".join(s["full"] for s in fuzzy)
        raise ValueError(f"多个会话匹配 '{to}'，请指定完整全名: {names}")

    # No match at all — list available sessions
    available = ", ".join(s["full"] for s in sessions)
    raise ValueError(
        f"未找到匹配 '{to}' 的会话。当前在线({len(sessions)}个): {available}"
    )


def _roster_age_seconds() -> float | None:
    """Return the age of the roster file in seconds, or None if unreadable."""
    path = _roster_path()
    try:
        mtime = os.path.getmtime(path)
        return time.time() - mtime
    except OSError:
        return None


# ── MCP tool ④: send_smart ──────────────────────────────────────────

_SEND_SMART_SCHEMA = {
    "type": "object",
    "properties": {
        "to": {
            "type": "string",
            "description": (
                "接收方: 短名(如 manager)、全名(如 zzc-manager-2642) 或完整地址(含#key)。"
                " 短名/全名自动从在线清单匹配并补全 #key。"
            ),
        },
        "body": {
            "type": "string",
            "description": "消息正文(Markdown), 与 file 二选一",
        },
        "file": {
            "type": "string",
            "description": "可选: 文本文件绝对路径, 与 body 二选一",
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
    "required": ["to"],
}


@register(
    "send_smart",
    "智能发信: 短名/全名从在线清单自动匹配后发送, 支持正文或文件",
    _SEND_SMART_SCHEMA,
)
def _send_smart(args: dict[str, Any]) -> dict[str, Any]:
    """Resolve recipient from online roster, then forward via _ipc_send."""
    to = (args.get("to") or "").strip()
    body = (args.get("body") or "").strip()
    file_path = (args.get("file") or "").strip()

    if not to:
        raise ValueError("to is required")

    provided = sum(1 for v in (body, file_path) if v)
    if provided == 0:
        raise ValueError("body or file is required")
    if provided > 1:
        raise ValueError("exactly one of body or file is required")

    # Resolve via roster (after body/file check — fail fast on missing content)
    resolved = _resolve_recipient(to)

    # optional reply-origin envelope fields (same as send_message)
    reply_to = (args.get("reply_to") or "").strip()
    in_reply_to = (args.get("in_reply_to") or "").strip()
    reply_policy = (args.get("reply_policy") or "").strip()
    kind = (args.get("kind") or "").strip()

    for name, val in [("reply_policy", reply_policy), ("kind", kind)]:
        if val and val not in _SEND_SMART_SCHEMA["properties"][name].get("enum", []):
            raise ValueError(
                f"{name} must be one of {_SEND_SMART_SCHEMA['properties'][name]['enum']}"
            )

    payload: dict[str, Any] = {"to": resolved, "body": body}
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

    result = _ipc_send(payload)
    # Attach resolution metadata so caller can verify
    result["resolved_to"] = resolved
    age = _roster_age_seconds()
    if age is not None:
        result["roster_age_seconds"] = round(age, 1)
    return result


# ── MCP tool ⑤: task_rw ─────────────────────────────────────────────

_TASK_RW_SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["read", "write"],
            "description": "read=查询自身任务; write=写入拆解子任务",
        },
        "task_id": {
            "type": "string",
            "description": "[read] 可选: 指定任务ID查单条, 不填则查全部自身任务",
        },
        "filter": {
            "type": "string",
            "enum": ["all", "active", "done", "pending", "blocked"],
            "description": "[read] 可选: 状态过滤, 默认 all",
        },
        "parent_id": {
            "type": "string",
            "description": "[write] 父任务ID(必填, 用于拆解关联)",
        },
        "title": {
            "type": "string",
            "description": "[write] 子任务标题(必填)",
        },
        "description": {
            "type": "string",
            "description": "[write] 可选: 子任务详细描述",
        },
        "assignee": {
            "type": "string",
            "description": "[write] 可选: 指派给谁(会话全名), 默认=自身",
        },
        "priority": {
            "type": "string",
            "enum": ["low", "medium", "high", "critical"],
            "description": "[write] 可选: 优先级, 默认 medium",
        },
    },
    "required": ["action"],
}


@register(
    "task_rw",
    "任务读写: 查询自身任务列表/详情, 或写入拆解子任务",
    _TASK_RW_SCHEMA,
)
def _task_rw(args: dict[str, Any]) -> dict[str, Any]:
    action = (args.get("action") or "").strip()
    if action not in ("read", "write"):
        raise ValueError("action must be read or write")

    if action == "read":
        task_id = (args.get("task_id") or "").strip()
        filter_status = (args.get("filter") or "all").strip()
        if filter_status not in ("all", "active", "done", "pending", "blocked"):
            raise ValueError("filter must be all/active/done/pending/blocked")

        if task_id:
            return _ipc_vsapi("GET", f"/v1/tasks/{task_id}")
        path = "/v1/tasks?owner=me"
        if filter_status != "all":
            path += f"&status={filter_status}"
        return _ipc_vsapi("GET", path)

    # action == "write"
    parent_id = (args.get("parent_id") or "").strip()
    title = (args.get("title") or "").strip()
    if not parent_id:
        raise ValueError("parent_id is required for write action")
    if not title:
        raise ValueError("title is required for write action")
    if len(title) > 500:
        raise ValueError("title exceeds 500 chars")

    body: dict[str, Any] = {
        "parent_id": parent_id,
        "title": title,
    }
    for field in ("description", "assignee", "priority"):
        val = (args.get(field) or "").strip()
        if val:
            body[field] = val
    if "priority" not in body:
        body["priority"] = "medium"
    return _ipc_vsapi("POST", "/v1/tasks", body)


# ── MCP tool ⑥: knowledge_search ─────────────────────────────────────

_KNOWLEDGE_SEARCH_SCHEMA = {
    "type": "object",
    "properties": {
        "query": {
            "type": "string",
            "description": "搜索关键词/正则, fsgrep 模式时支持基本正则",
        },
        "source": {
            "type": "string",
            "enum": ["local", "datad"],
            "description": "搜索后端: local=fsgrep(默认), datad=远程知识库",
        },
        "path": {
            "type": "string",
            "description": "[local] 搜索根目录, 默认当前工作目录",
        },
        "include": {
            "type": "string",
            "description": "[local] 文件匹配模式(glob), 如 '*.py,*.md', 默认 '*.*'",
        },
        "max_results": {
            "type": "integer",
            "description": "最大返回条数, 默认 40, 上限 200",
        },
        "context_lines": {
            "type": "integer",
            "description": "[local] 匹配行上下文行数, 默认 2",
        },
        "collection": {
            "type": "string",
            "description": "[datad] 知识库集合名, 默认 default",
        },
    },
    "required": ["query"],
}


def _fsgrep(
    query: str,
    root: str,
    include: str,
    max_results: int,
    context: int,
) -> dict[str, Any]:
    """Run a local filesystem grep. Pure Python fallback if grep unavailable."""
    import fnmatch as _fnmatch
    import subprocess as _sp

    root = os.path.abspath(os.path.expanduser(root))
    if not os.path.isdir(root):
        raise ValueError(f"directory not found: {root}")

    patterns = [p.strip() for p in include.split(",") if p.strip()] or ["*"]
    results: list[dict[str, Any]] = []
    matched_files = 0

    try:
        # Use grep -rn for speed when available
        cmd = [
            "grep", "-rn", "-I", "--include=" + patterns[0] if len(patterns) == 1 else "-I",
        ]
        # Build include args
        for pat in patterns:
            cmd.insert(2, f"--include={pat}")
        if context:
            cmd.insert(2, f"-C{context}")
        cmd.extend(["-m", str(max_results + 20), query, root])
        proc = _sp.run(
            cmd,
            capture_output=True, text=True, timeout=15,
        )
        for line in proc.stdout.splitlines()[:max_results]:
            parts = line.split(":", 2)
            if len(parts) >= 3:
                results.append({
                    "file": parts[0],
                    "line": int(parts[1]) if parts[1].isdigit() else 0,
                    "text": parts[2].strip(),
                })
        matched_files = len({r["file"] for r in results})
    except (FileNotFoundError, _sp.TimeoutExpired):
        # Pure Python fallback
        query_lower = query.lower()
        for dirpath, _dirnames, filenames in os.walk(root):
            for fn in filenames:
                if not any(_fnmatch.fnmatch(fn, p) for p in patterns):
                    continue
                fpath = os.path.join(dirpath, fn)
                try:
                    with open(fpath, encoding="utf-8", errors="replace") as f:
                        for lineno, line in enumerate(f, 1):
                            if query_lower in line.lower():
                                results.append({
                                    "file": fpath,
                                    "line": lineno,
                                    "text": line.strip(),
                                })
                                if len(results) >= max_results:
                                    raise StopIteration()
                except (OSError, UnicodeDecodeError):
                    continue
            if len(results) >= max_results:
                break
    except StopIteration:
        pass

    return {
        "query": query,
        "source": "local",
        "count": len(results),
        "matched_files": matched_files or len({r["file"] for r in results}),
        "results": results[:max_results],
        "truncated": len(results) > max_results,
    }


@register(
    "knowledge_search",
    "知识库查询: 本地 fsgrep 或 datad 远程搜索, 支持可插拔后端",
    _KNOWLEDGE_SEARCH_SCHEMA,
)
def _knowledge_search(args: dict[str, Any]) -> dict[str, Any]:
    query = (args.get("query") or "").strip()
    if not query:
        raise ValueError("query is required")

    source = (args.get("source") or "local").strip()
    if source not in ("local", "datad"):
        raise ValueError("source must be local or datad")

    max_results = int(args.get("max_results") or 40)
    if max_results < 1 or max_results > 200:
        raise ValueError("max_results must be 1-200")

    if source == "local":
        path = (args.get("path") or os.getcwd()).strip()
        include = (args.get("include") or "*").strip()
        context = int(args.get("context_lines") or 2)
        return _fsgrep(query, path, include, max_results, context)

    # source == "datad": pluggable remote backend
    collection = (args.get("collection") or "default").strip()
    body: dict[str, Any] = {
        "query": query,
        "max_results": max_results,
        "collection": collection,
    }
    return _ipc_vsapi("POST", "/v1/knowledge/search", body)


# ── MCP tool ⑦: skill_search ────────────────────────────────────────

_SKILL_SEARCH_SCHEMA = {
    "type": "object",
    "properties": {
        "query": {
            "type": "string",
            "description": "可选: 按名称/描述搜索 skill, 不填则列出全部",
        },
        "category": {
            "type": "string",
            "description": "可选: 按分类过滤 (如 tools, agents, workflows)",
        },
        "limit": {
            "type": "integer",
            "description": "最大返回数, 默认 50, 上限 200",
        },
    },
    "required": [],
}


@register(
    "skill_search",
    "Skill 库查询: 搜索/列出系统可用 skill, 支持按名称/分类过滤",
    _SKILL_SEARCH_SCHEMA,
)
def _skill_search(args: dict[str, Any]) -> dict[str, Any]:
    query = (args.get("query") or "").strip()
    category = (args.get("category") or "").strip()
    limit = int(args.get("limit") or 50)
    if limit < 1 or limit > 200:
        raise ValueError("limit must be 1-200")

    path = "/v1/skills"
    params: list[str] = []
    if query:
        params.append(f"q={query}")
    if category:
        params.append(f"category={category}")
    params.append(f"limit={limit}")
    if params:
        path += "?" + "&".join(params)
    return _ipc_vsapi("GET", path)


# ── MCP tool ⑧: team_rules ─────────────────────────────────────────

_TEAM_RULES_SCHEMA = {
    "type": "object",
    "properties": {
        "query": {
            "type": "string",
            "description": "可选: 按关键词搜索制度/规则, 不填则列出全部 rules.* 配置",
        },
        "include_kb": {
            "type": "boolean",
            "description": "可选: 是否同时搜索知识库长文档, 默认 true",
        },
        "limit": {
            "type": "integer",
            "description": "最大返回数, 默认 50, 上限 200",
        },
    },
    "required": [],
}


@register(
    "team_rules",
    "团队制度查询: 搜索项目规约/制度/规则(config rules.* + 知识库长文档)",
    _TEAM_RULES_SCHEMA,
)
def _team_rules(args: dict[str, Any]) -> dict[str, Any]:
    query = (args.get("query") or "").strip()
    include_kb = args.get("include_kb", True)
    limit = int(args.get("limit") or 50)
    if limit < 1 or limit > 200:
        raise ValueError("limit must be 1-200")

    result: dict[str, Any] = {"query": query, "rules": [], "kb_results": []}

    # 1) Fetch rules from config (datad GET /v1/config returns all, filter client-side)
    try:
        all_configs = _ipc_vsapi("GET", "/v1/config")
        if isinstance(all_configs, dict):
            for key, val in all_configs.items():
                if key.startswith("rules.") or key.startswith("rule."):
                    if not query or (query.lower() in key.lower() or
                                     query.lower() in str(val).lower()):
                        result["rules"].append({"key": key, "value": val})
    except RuntimeError:
        # datad unreachable — return empty rules (caller sees partial result)
        result["rules_error"] = "datad config endpoint unreachable"

    # 2) Optional: search knowledge base for long-form docs
    if include_kb:
        try:
            kb_body: dict[str, Any] = {
                "query": query or "团队制度 规则 规范",
                "max_results": min(limit, 200),
                "collection": "rules",
            }
            kb = _ipc_vsapi("POST", "/v1/knowledge/search", kb_body)
            if isinstance(kb, dict):
                result["kb_results"] = kb.get("results", kb.get("hits", kb))
        except RuntimeError:
            result["kb_error"] = "datad knowledge endpoint unreachable"

    # Trim to limit
    if len(result["rules"]) > limit:
        result["rules"] = result["rules"][:limit]
        result["rules_truncated"] = True
    return result


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
