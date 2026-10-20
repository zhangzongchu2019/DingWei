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
  • start a new model turn in an idle desktop app (an explicit pending wait tool
    can return new messages to its requesting conversation)
"""

from __future__ import annotations

import json
import re
import os
import sys
from dataclasses import dataclass
from typing import Any, Callable

if __package__:
    from . import __version__
    from .ipc_permissions import send_body_only
else:
    # Preserve existing MCP configurations that execute this file directly.
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    from sessionhelper import __version__
    from sessionhelper.ipc_permissions import send_body_only


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

import threading
import time

_wait_context = threading.local()
_stdout_lock = threading.Lock()

import socket as _socket
import uuid as _uuid

_IPC_ENV = os.environ  # one stdlib-only environment entry point for this standalone shim
_SEND_BODY_ONLY = send_body_only(_IPC_ENV)
_IPC_SOCK = _IPC_ENV.get("DINGWEI_MCP_IPC_SOCK", "")
_IPC_TIMEOUT = 5.0
# Agent HTTP15 within helper aggregate30; shim35 receives explicit deadline result.
# Leave a further5 seconds for the helper's explicit result to reach the shim.
_SPEC_IPC_TIMEOUT = 35.0


class _IPCFailure(RuntimeError):
    """An IPC failure carrying only a fixed, caller-safe category."""

    def __init__(self, category: str):
        self.category = category
        super().__init__(category)


def _transport_failure(exc: OSError) -> _IPCFailure:
    return _IPCFailure("ipc_timeout" if isinstance(exc, TimeoutError) else "ipc_unavailable")


def _decode_ipc_response(data: bytes) -> dict[str, Any]:
    try:
        if b"\n" not in data:
            raise ValueError
        response = json.loads(data.split(b"\n", 1)[0].decode("utf-8"))
    except (ValueError, UnicodeError):
        raise _IPCFailure("ipc_invalid_response") from None
    if not isinstance(response, dict) or type(response.get("ok")) is not bool:
        raise _IPCFailure("ipc_invalid_response")
    if not response["ok"]:
        raise _IPCFailure("helper_rejected")
    return response


def _tool_failure(request_id, category: str) -> Response:
    # A missing response never proves that a side effect did not occur.
    # Do not echo exceptions, paths or helper error bodies, and never resend.
    return Response(id=request_id, result={
        "isError": True,
        "content": [{"type": "text", "text": json.dumps({
            "error": category, "outcome": "unknown", "retryable": False,
            "message": "Tool failed; do not automatically resend.",
        })}],
    })


def _ipc_send(payload: dict[str, Any]) -> dict[str, Any]:
    return _ipc_request({"type": "mcp_send", **payload})


def _ipc_request(payload: dict[str, Any], *, timeout=None) -> dict[str, Any]:
    """Forward a validated send request to the live sessionHelper via a
    local Unix-domain socket, then return the helperʼs outcome."""
    payload["request_id"] = _uuid.uuid4().hex[:12]
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n"

    sock = _socket.socket(_socket.AF_UNIX, _socket.SOCK_STREAM)
    sock.settimeout(_IPC_TIMEOUT if timeout is None else timeout)
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
        response = _decode_ipc_response(b"".join(chunks))
    except OSError as exc:
        raise _transport_failure(exc) from None
    finally:
        sock.close()

    outcome = response.get("outcome", response)
    if not isinstance(outcome, dict):
        raise _IPCFailure("ipc_invalid_response")
    return outcome


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
        response = _decode_ipc_response(b"".join(chunks))
    except OSError as exc:
        raise _transport_failure(exc) from None
    finally:
        sock.close()
    return response.get("result", response)


# ── MCP tools: send_message + T18 ─────────────────────────────────────

_SEND_TOOL_SCHEMA = {
    "type": "object",
    "properties": {
        "to": {
            "type": "string",
            "description": "对方 Agent 全名（名字.后缀，例如 oaagent.lbl）；跨用户发送须双方同在一个编组",
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
            "description": "可选: 要让收方回复回到发起 Agent，请明确设为 expect_reply；缺省不产 auto-reply，no_reply 不回传",
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
    if _SEND_BODY_ONLY and ("file" in args or any(
            not isinstance(args.get(k, ""), str) for k in ("to", "body", "reply_to", "in_reply_to", "reply_policy", "kind"))):
        raise _IPCFailure("operation_not_allowed")
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


@register("dw_inbox", "查询本席收件箱；driver只读与本地已读，desktop另支持暂停。接收不等于AI已执行。", {
    "type": "object",
    "properties": {
        "operation": {"type": "string", "enum": ["status", "list", "read", "ack", "pause", "resume"]},
        "message_id": {"type": "string"},
        "limit": {"type": "integer", "minimum": 1, "maximum": 100},
    },
    "required": ["operation"],
    "additionalProperties": False,
})
def _desktop_inbox(args):
    if not isinstance(args, dict) or set(args) - {"operation", "message_id", "limit"}:
        raise ValueError("unsupported inbox arguments")
    if args.get("operation") not in {"status", "list", "read", "ack", "pause", "resume"}:
        raise ValueError("unsupported inbox operation")
    if "limit" in args and (type(args["limit"]) is not int or not 1 <= args["limit"] <= 100):
        raise ValueError("limit must be 1..100")
    if "message_id" in args and (not isinstance(args["message_id"], str) or not 1 <= len(args["message_id"]) <= 256):
        raise ValueError("invalid message_id")
    return _ipc_request({**args, "type": "desktop_inbox_v1"})


@register("dw_wait_message", "在用户要求 DW 持续监听时调用：等待本机新消息交还当前 AI 会话。仅桌面模式；超时不是新消息，暂停/取消后停止等待。返回不代表 AI 已执行。", {
    "type": "object", "properties": {"wait_seconds": {"type": "integer", "minimum": 1, "maximum": 45}},
    "additionalProperties": False,
})
def _desktop_wait(args):
    if not isinstance(args, dict) or set(args)-{'wait_seconds'}:
        raise ValueError('invalid desktop wait arguments')
    seconds = args.get('wait_seconds',45)
    if type(seconds) is not int or not 1 <= seconds <= 45:
        raise ValueError('wait_seconds must be 1..45')
    cancelled = getattr(_wait_context, 'cancelled', threading.Event())
    sock = _socket.socket(_socket.AF_UNIX, _socket.SOCK_STREAM)
    deadline = time.monotonic()+seconds+2
    try:
        sock.settimeout(2)
        sock.connect(_IPC_SOCK)
        sock.sendall((json.dumps({'type':'desktop_inbox_v1','operation':'wait','wait_seconds':seconds,'request_id':_uuid.uuid4().hex})+'\n').encode())
        sock.settimeout(.1)
        data = bytearray()
        while b'\n' not in data:
            if cancelled.is_set(): raise ValueError('desktop wait cancelled')
            if time.monotonic() >= deadline: raise _IPCFailure('ipc_timeout')
            try: chunk = sock.recv(4096)
            except _socket.timeout: continue
            if not chunk: raise _IPCFailure('ipc_invalid_response')
            data.extend(chunk)
            if len(data)>512*1024: raise _IPCFailure('ipc_invalid_response')
        return _decode_ipc_response(bytes(data))
    except OSError as exc:
        raise _transport_failure(exc) from None
    finally:
        sock.close()


@register("dw_reply_message", "回复收到的 DW 消息：从原消息取目标，Hub 确认持久入队后才确认已处理。当前仅支持可靠 Agent 目标；结果不明时不要另发重试。", {
    "type":"object","properties":{"message_id":{"type":"string"},"body":{"type":"string"}},
    "required":["message_id","body"],"additionalProperties":False,
})
def _desktop_reply(args):
    if not isinstance(args,dict) or set(args)!={'message_id','body'}:
        raise ValueError('reply requires only message_id and body')
    message_id,body=args['message_id'],args['body']
    if not isinstance(message_id,str) or not 1<=len(message_id)<=256:
        raise ValueError('invalid message_id')
    if not isinstance(body,str) or not body.strip() or len(body.encode('utf-8'))>65536:
        raise ValueError('invalid reply body')
    return _ipc_request({'type':'desktop_inbox_v1','operation':'reply','message_id':message_id,'body':body},timeout=12)


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

@register("project_context", "读取当前 Hub 的本人任务、同组在线名册及受限 spec 摘要", {
    "type": "object", "additionalProperties": False,
    "properties": {
        "scope": {"type":"string","enum":["tasks","sessions","config","all"]},
        "limit": {"type":"integer","minimum":1,"maximum":100},
        "cursor": {"type":"string","maxLength":4096},
    }, "required":["scope"],
})
def _project_context(args: dict[str, Any]) -> dict[str, Any]:
    from sessionhelper.project_context import validate_args
    body=validate_args(args)
    try:
        result=_ipc_request({'type':'project_context_v1','body':body},timeout=20)
    except _IPCFailure as exc:
        if exc.category=='helper_rejected':
            raise _IPCFailure('project_context_unsupported') from None
        raise
    if result.get('error') in ('unsupported_type','unknown_type') or str(result.get('error','')).startswith('unknown type '):
        return {'status':'error','error':'project_context_unsupported','retryable':False}
    return result


# ── MCP tools: Console spec writing (A3) ─────────────────────────────
# Dedicated Agent identity stays in sessionHelper. Direct writes exchange
# only document data; no approval request, credential or capability argument.

_SPEC_BODY_PROPERTIES = {
    "title": {"type": "string"},
    "content_markdown": {"type": "string"},
    "acceptance": {"type": "array"},
    "input_anchors": {"type": "array"},
    "inferences": {"type": "array"},
    "expected_revision": {"type": "integer", "minimum": 1},
}


def _spec_body(args: dict[str, Any], *, update: bool) -> dict[str, Any]:
    allowed = set(_SPEC_BODY_PROPERTIES) | ({"spec_id"} if update else set())
    if not isinstance(args, dict) or set(args) - allowed:
        raise ValueError("unsupported spec arguments")
    body = {key: args[key] for key in _SPEC_BODY_PROPERTIES if key in args}
    required = {"title", "content_markdown", "acceptance", "input_anchors"}
    if update:
        required.add("expected_revision")
    if not required.issubset(body):
        raise ValueError("missing required spec fields")
    if not isinstance(body["title"], str) or not isinstance(body["content_markdown"], str):
        raise ValueError("invalid spec text")
    if not isinstance(body["acceptance"], list) or not isinstance(body["input_anchors"], list):
        raise ValueError("invalid spec arrays")
    if "inferences" in body and not isinstance(body["inferences"], list):
        raise ValueError("invalid spec inferences")
    if update and (type(body["expected_revision"]) is not int or body["expected_revision"] < 1):
        raise ValueError("invalid expected_revision")
    if len(json.dumps(body, ensure_ascii=False).encode("utf-8")) > 2 << 20:
        raise ValueError("spec body exceeds 2 MiB")
    return body


@register("dw_spec_create", "以当前 Agent 身份直接创建 spec；一次调用完成写入，不需要待批。", {
    "type": "object", "properties": {**_SPEC_BODY_PROPERTIES},
    "required": ["title", "content_markdown", "acceptance", "input_anchors"], "additionalProperties": False,
})
def _dw_spec_create(args: dict[str, Any]) -> dict[str, Any]:
    body = _spec_body(args, update=False)
    return _ipc_request({"type": "spec_write_v1", "action": "create", "protocol": "direct-v1", "body": body}, timeout=_SPEC_IPC_TIMEOUT)


@register("dw_spec_update", "直接更新当前 Agent 自建的 spec；必须提供最新 expected_revision。", {
    "type": "object", "properties": {**_SPEC_BODY_PROPERTIES, "spec_id": {"type": "string"}},
    "required": ["spec_id", "title", "content_markdown", "acceptance", "input_anchors", "expected_revision"], "additionalProperties": False,
})
def _dw_spec_update(args: dict[str, Any]) -> dict[str, Any]:
    spec_id = str(args.get("spec_id") or "").strip()
    if not spec_id or len(spec_id) > 256 or not re.fullmatch(r"[A-Za-z0-9_-]+", spec_id):
        raise ValueError("invalid spec_id")
    body = _spec_body(args, update=True)
    return _ipc_request({"type": "spec_write_v1", "action": "update", "spec_id": spec_id, "protocol": "direct-v1", "body": body}, timeout=_SPEC_IPC_TIMEOUT)


# R32: the shim transports task data only, never the Agent credential.
_TASK_UPDATE_PROPERTIES = {
    "task_id":{"type":"string"}, "request_id":{"type":"string"},
    "expected_version":{"type":"integer","minimum":1},
    "operation":{"type":"string","enum":["start","resume","block","complete","mark_case","confirm_case"]},
    "blocker":{"type":"string"}, "acceptance_id":{"type":"string"}, "case_name":{"type":"string"},
}
@register("dw_task_list", "列出当前 Agent 被指派的任务、依赖和已确认用例进度。", {
    "type":"object", "properties":{"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"},"spec_id":{"type":"string"},"status":{"type":"string"}},
    "additionalProperties":False,
})
def _dw_task_list(args):
    if not isinstance(args,dict) or set(args)-{'limit','cursor','spec_id','status'}:
        raise ValueError('invalid task list arguments')
    return _ipc_request({'type':'task_flow_v1','action':'list','body':args},timeout=_SPEC_IPC_TIMEOUT)

@register("dw_task_update", "更新自己任务；开发登记用例、审查确认。先读 version；结果不明先查询勿新ID重试。", {
    "type":"object","properties":_TASK_UPDATE_PROPERTIES,
    "required":["task_id","request_id","expected_version","operation"],"additionalProperties":False,
})
def _dw_task_update(args):
    if not isinstance(args,dict) or set(args)-set(_TASK_UPDATE_PROPERTIES) or not {'task_id','request_id','expected_version','operation'}.issubset(args):
        raise ValueError('invalid task update arguments')
    task_id=args['task_id']
    if not isinstance(task_id,str) or not 1 <= len(task_id) <= 256:
        raise ValueError('invalid task id')
    return _ipc_request({'type':'task_flow_v1','action':'update','task_id':task_id,'body':{k:v for k,v in args.items() if k!='task_id'}},timeout=_SPEC_IPC_TIMEOUT)

@register("dw_spec_review_requests", "读取本人当前有权处理的Console预审/补标准请求；取消或失权后不可读。", {
 "type":"object","properties":{"request_id":{"type":"string"},"cursor":{"type":"string"}},"additionalProperties":False,
})
def _dw_spec_review_requests(args):
    if not isinstance(args,dict) or set(args)-{'request_id','cursor'}:
        raise ValueError('invalid spec request list')
    return _ipc_request({'type':'spec_review_v1','action':'list','body':args},timeout=_SPEC_IPC_TIMEOUT)

_BLOCKER_IDS={"task_id":{"type":"string","minLength":1,"maxLength":256},"blocker_id":{"type":"string","minLength":1,"maxLength":256}}
_BLOCKER_REQUEST={"request_id":{"type":"string","minLength":1,"maxLength":128},"expected_version":{"type":"integer","minimum":1}}
_BLOCKER_CONDITION={"type":"object","properties":{"id":{"type":"string","enum":["materials","environment","authorization"]},"text":{"type":"string","minLength":1,"maxLength":1024}},"required":["id","text"],"additionalProperties":False}
_BLOCKER_CHECK={"type":"object","properties":{"id":{"type":"string","enum":["materials","environment","authorization"]},"result":{"type":"string","enum":["pass","fail","unknown"]},"evidence_ref":{"type":"string","minLength":1,"maxLength":2048}},"required":["id","result","evidence_ref"],"additionalProperties":False}
def _blocker_ipc(action,args,allowed,required):
    if not isinstance(args,dict) or set(args)-set(allowed) or not set(required).issubset(args):raise ValueError('invalid blocker tool arguments')
    request={'type':'blocker_v1','action':action,'task_id':args.get('task_id'),'body':{k:v for k,v in args.items() if k not in ('task_id','blocker_id')}}
    if 'blocker_id' in args:request['blocker_id']=args['blocker_id']
    if action!='list':request['body']['operation']=action
    from sessionhelper.task_blocker import validate_request
    validate_request(request)
    try:return _ipc_request(request,timeout=_SPEC_IPC_TIMEOUT)
    except _IPCFailure as exc:
        if exc.category=='helper_rejected':raise _IPCFailure('blocker_unsupported_upgrade_helper') from None
        raise

@register("dw_task_blocker_list","读取自己任务的阻塞卡、当前版本与答复；不取得恢复或业务执行授权。",{
 "type":"object","properties":{**_BLOCKER_IDS,"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"},"input_before":{"type":"integer","minimum":1}},"required":["task_id"],"additionalProperties":False})
def _dw_task_blocker_list(args):
    return _blocker_ipc('list',args,{'task_id','blocker_id','limit','cursor','input_before'},{'task_id'})

_BLOCKER_SET_PROPS={"task_id":_BLOCKER_IDS['task_id'],**_BLOCKER_REQUEST,"kind":{"type":"string","enum":["requirement_unclear","change","technical","resource"]},"where":{"type":"string","minLength":1,"maxLength":4096},"waiting_principal":{"type":"string","description":"稳定 user: 或 agent: 身份；未定为空，不能用昵称假造责任人"},"expected_input":{"type":"string","minLength":1,"maxLength":4096},"resume_recheck":{"type":"array","maxItems":3,"items":_BLOCKER_CONDITION},"production_or_security":{"type":"boolean"}}
@register("dw_task_blocker_set","当前开发 Agent 登记卡在哪、等谁、等什么；相同卡不重复通知。真实生产/安全选完整档。",{
 "type":"object","properties":_BLOCKER_SET_PROPS,"required":["task_id","request_id","expected_version","kind","where","expected_input"],"additionalProperties":False})
def _dw_task_blocker_set(args):
    return _blocker_ipc('create',args,_BLOCKER_SET_PROPS,{'task_id','request_id','expected_version','kind','where','expected_input'})

_BLOCKER_INPUT_PROPS={**_BLOCKER_IDS,**_BLOCKER_REQUEST,"event_id":{"type":"string","minLength":1,"maxLength":128},"body":{"type":"string","minLength":1,"maxLength":8192},"evidence_ref":{"type":"string","minLength":1,"maxLength":2048}}
@register("dw_task_blocker_input","所等 Agent 提交有身份的新答复；事件重发不重复唤醒，内容改变须新事件。",{
 "type":"object","properties":_BLOCKER_INPUT_PROPS,"required":["task_id","blocker_id","request_id","expected_version","event_id","body","evidence_ref"],"additionalProperties":False})
def _dw_task_blocker_input(args):
    return _blocker_ipc('input',args,_BLOCKER_INPUT_PROPS,{'task_id','blocker_id','request_id','expected_version','event_id','body','evidence_ref'})

_BLOCKER_RECHECK_PROPS={**_BLOCKER_IDS,**_BLOCKER_REQUEST,"operation":{"type":"string","enum":["begin_recheck","finish_recheck"]},"input_version":{"type":"integer","minimum":1},"lease_id":{"type":"string","minLength":1},"checks":{"type":"array","minItems":3,"maxItems":3,"items":_BLOCKER_CHECK}}
@register("dw_task_blocker_recheck","当前执行人认领一次重核或登记材料/环境/授权的真实核对结果；不自动resume/完成/签收。",{
 "type":"object","properties":_BLOCKER_RECHECK_PROPS,"required":["task_id","blocker_id","request_id","expected_version","operation","input_version"],"additionalProperties":False,
 "oneOf":[{"properties":{"operation":{"const":"begin_recheck"}},"not":{"anyOf":[{"required":["lease_id"]},{"required":["checks"]}]}},{"properties":{"operation":{"const":"finish_recheck"}},"required":["lease_id","checks"]}]})
def _dw_task_blocker_recheck(args):
    return _blocker_ipc(args.get('operation') if isinstance(args,dict) else None,args,_BLOCKER_RECHECK_PROPS,{'task_id','blocker_id','request_id','expected_version','operation','input_version'})

_SPEC_REVIEW_ISSUE_SCHEMA={"type":"object","properties":{"id":{"type":"string","minLength":1},"text":{"type":"string","minLength":1}},"required":["id","text"],"additionalProperties":False}
_SPEC_REVIEW_CRITERION_SCHEMA={"type":"object","properties":{"id":{"type":"string","minLength":1},"text":{"type":"string","minLength":1},"owner_ref":{"type":"string","minLength":1,"description":"必须逐字存在于当前spec正文的引用"},"cases":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"string","minLength":1}}},"required":["id","text","owner_ref","cases"],"additionalProperties":False}
@register("dw_spec_review_reply", "回Console预审issues或criteria/cases；只写pending/待人确认，不能定稿。结果未知请查询原请求。", {
 "type":"object","properties":{"request_id":{"type":"string"},"kind":{"type":"string","enum":["review","criteria"]},"revision":{"type":"integer","minimum":1},"content_sha256":{"type":"string"},"issues":{"type":"array","maxItems":100,"items":_SPEC_REVIEW_ISSUE_SCHEMA},"criteria":{"type":"array","minItems":1,"maxItems":100,"items":_SPEC_REVIEW_CRITERION_SCHEMA}},
 "required":["request_id","kind","revision","content_sha256"],"additionalProperties":False,
 "oneOf":[
     {"properties":{"kind":{"const":"review"}},"required":["issues"],"not":{"required":["criteria"]}},
     {"properties":{"kind":{"const":"criteria"}},"required":["criteria"],"not":{"required":["issues"]}},
 ],
})
def _dw_spec_review_reply(args):
    if not isinstance(args,dict) or set(args)-{'request_id','kind','revision','content_sha256','issues','criteria'}:
        raise ValueError('invalid spec reply arguments')
    if not {'request_id','kind','revision','content_sha256'}.issubset(args):
        raise ValueError('missing spec reply fields')
    return _ipc_request({'type':'spec_review_v1','action':'reply','body':args},timeout=_SPEC_IPC_TIMEOUT)

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
        except OSError as exc:
            raise _transport_failure(exc) from None
        finally:
            sock.close()
    finally:
        os.close(fd)
    response = _decode_ipc_response(bytes(reply))
    result = response.get("result")
    if not isinstance(result, dict):
        raise _IPCFailure("ipc_invalid_response")
    return result


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
        if _SEND_BODY_ONLY:
            tools = [t for t in tools if t["name"] in ("send_message", "upload_file")]
            for tool in tools:
                if tool["name"] == "send_message":
                    schema = dict(tool["inputSchema"])
                    schema["properties"] = {k: v for k, v in schema["properties"].items() if k != "file"}
                    schema["required"] = ["to", "body"]
                    tool["inputSchema"] = schema
        return Response(id=req.id, result={"tools": tools})

    # -- tools/call ----------------------------------------------------
    if req.method == "tools/call":
        name = (req.params or {}).get("name", "")
        arguments = (req.params or {}).get("arguments") or {}
        if _SEND_BODY_ONLY and (not isinstance(name, str) or name not in ("send_message", "upload_file")):
            return _tool_failure(req.id, "operation_not_allowed")
        if not isinstance(name, str) or name not in _handlers:
            return Response(
                id=req.id,
                error={"code": -32601, "message": f"unknown tool: {name!r}"},
            )
        try:
            result = _handlers[name](dict(arguments))
            return Response(id=req.id, result={"content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]})
        except _IPCFailure as exc:
            return _tool_failure(req.id, exc.category)
        except (TypeError, ValueError) as exc:
            return Response(
                id=req.id,
                error={"code": -32602, "message": "invalid tool arguments"},
            )
        except Exception:
            return _tool_failure(req.id, "tool_failed")

    # -- initialize ----------------------------------------------------
    if req.method == "initialize":
        return Response(id=req.id, result={
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "dingwei-mcp-shim", "version": __version__},
        })

    # -- unknown method ------------------------------------------------
    return Response(id=req.id, error={"code": -32601, "message": "unknown method" if _SEND_BODY_ONLY else f"unknown method: {req.method!r}"})


def _write(response: Response) -> None:
    """Write one JSON‑RPC response (with terminating newline) to stdout."""
    body: dict[str, Any] = {"jsonrpc": "2.0", "id": response.id}
    if response.error is not None:
        body["error"] = response.error
    else:
        body["result"] = response.result
    with _stdout_lock:
        sys.stdout.write(json.dumps(body, ensure_ascii=False) + "\n")
        sys.stdout.flush()


def serve() -> None:
    """Read requests while a single bounded desktop wait runs concurrently."""
    active_waits = {}
    def start_wait(req):
        cancelled = threading.Event()
        def worker():
            _wait_context.cancelled = cancelled
            response = _dispatch(req)
            if not cancelled.is_set(): _write(response)
        thread = threading.Thread(target=worker, daemon=True)
        active_waits[req.id] = (thread, cancelled)
        thread.start()
    try:
        _serve_requests(active_waits, start_wait)
    finally:
        for thread, cancelled in list(active_waits.values()): cancelled.set()
        for thread, _ in list(active_waits.values()): thread.join(timeout=1)


def _serve_requests(active_waits, start_wait):
    for line in sys.stdin:
        for key, (thread, _) in list(active_waits.items()):
            if not thread.is_alive(): active_waits.pop(key, None)
        line = line.strip()
        if not line:
            continue
        try:
            raw = json.loads(line)
        except json.JSONDecodeError:
            _write(Response(id=None, error={"code": -32700, "message": "parse error"}))
            continue

        # Request/cancellation IDs index the active wait. Never let malformed
        # input crash the reader and strand that pending tool call.
        if (not isinstance(raw, dict)
                or raw.get('id') is not None and type(raw['id']) not in (str, int)
                or raw.get('params') is not None and not isinstance(raw['params'], dict)):
            _write(Response(id=None, error={"code": -32600, "message": "invalid request"}))
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

        if req.id is None:
            if req.method == 'notifications/cancelled' and isinstance(req.params,dict):
                request_id = req.params.get('requestId')
                entry = active_waits.get(request_id) if type(request_id) in (str, int) else None
                if entry: entry[1].set()
            continue
        if req.method == 'tools/call' and (req.params or {}).get('name') == 'dw_wait_message':
            if active_waits:
                _write(Response(id=req.id,error={'code':-32000,'message':'desktop wait already active'}))
            else:
                start_wait(req)
            continue
        _write(_dispatch(req))





# Pilot questions never expose human PM business-answer, grant or signoff tools.
_Q_ID={"type":"string","minLength":1,"maxLength":256}
def _question_ipc(action,args,allowed,required):
    if not isinstance(args,dict) or set(args)-set(allowed) or not set(required)<=set(args):raise ValueError('invalid question arguments')
    from sessionhelper.pilot_question import validate_request
    request={'type':'pilot_question_v1','action':action,'body':{k:v for k,v in args.items() if k not in ('question_id',) and not (action=='create' and k=='task_id')}}
    if action=='create':request['task_id']=args['task_id']
    if 'question_id' in args:request['question_id']=args['question_id']
    validate_request(request)
    try:return _ipc_request(request)
    except _IPCFailure as exc:
        if exc.category=='helper_rejected':raise _IPCFailure('question_unsupported_upgrade_helper') from None
        raise

@register("dw_task_question_create","在自己当前任务挂研发问题；用所有者配置的产品联系人，不授予新执行或签收权限。",{
 "type":"object","properties":{"task_id":_Q_ID,"request_id":{"type":"string","minLength":1,"maxLength":128},"expected_version":{"type":"integer","minimum":1},"kind":{"enum":["requirement_unclear","change","technical","resource"]},"title":{"type":"string","minLength":1,"maxLength":512},"observed_fact":{"type":"string","minLength":1,"maxLength":4096},"question":{"type":"string","minLength":1,"maxLength":4096},"expected_answer":{"type":"string","minLength":1,"maxLength":4096},"input_anchors":{"type":"string","maxLength":4096},"production_or_security":{"type":"boolean"}},"required":["task_id","request_id","expected_version","kind","title","observed_fact","question","expected_answer"],"additionalProperties":False})
def _dw_task_question_create(args):
    return _question_ipc('create',args,{'task_id','request_id','expected_version','kind','title','observed_fact','question','expected_answer','input_anchors','production_or_security'},{'task_id','request_id','expected_version','kind','title','observed_fact','question','expected_answer'})

@register("dw_task_question_list","只读当前仍获准的问题摘要及答复；问题最小授权不扩大为整个spec/任务权限。",{
 "type":"object","properties":{"question_id":_Q_ID,"task_id":_Q_ID,"spec_id":_Q_ID,"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string","maxLength":4096},"before":{"type":"integer","minimum":1},"change_before":{"type":"integer","minimum":1}},"additionalProperties":False,
 "oneOf":[{"required":["question_id"],"not":{"anyOf":[{"required":[x]} for x in ('task_id','spec_id','limit','cursor')]}},{"not":{"anyOf":[{"required":[x]} for x in ('question_id','before','change_before')]}}]})
def _dw_task_question_list(args):
    return _question_ipc('list',args,{'question_id','task_id','spec_id','limit','cursor','before','change_before'},set())

@register("dw_task_question_input","向仍获准的问题补技术材料和证据；不冒充人类PM业务决定，不改spec或执行任务。",{
 "type":"object","properties":{"question_id":_Q_ID,"request_id":{"type":"string","minLength":1,"maxLength":128},"expected_version":{"type":"integer","minimum":1},"event_id":{"type":"string","minLength":1,"maxLength":128},"body":{"type":"string","minLength":1,"maxLength":8192},"evidence_ref":{"type":"string","minLength":1,"maxLength":2048}},"required":["question_id","request_id","expected_version","event_id","body","evidence_ref"],"additionalProperties":False})
def _dw_task_question_input(args):
    return _question_ipc('input',args,{'question_id','request_id','expected_version','event_id','body','evidence_ref'},{'question_id','request_id','expected_version','event_id','body','evidence_ref'})

if __name__ == "__main__":
    serve()
