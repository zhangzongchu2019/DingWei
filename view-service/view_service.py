"""View Service —— 独立进程,对接 hub 内网口(C2/C3),服务新事件流页。

与 sidecar 的区别:sidecar 寄生在 helper 进程里、共享 driver;
View Service 是独立进程,只跟 **hub** 打交道(不认识 driver):
  GET  /                浏览器页面(renderer-canary-sse.html)
  GET  /events          SSE:转发 hub /internal/view/{session}/events(C2),顺带记成本
  POST /input {text}    → hub /internal/view/{session}/input(C3),必要时先解锁
  GET  /status          状态栏数据(会话/成本/内存/磁盘/时间)

会话来源:hub 入口路由器(C4)反代时带的 X-DW-Session 头;缺省用 VS_SESSION。
鉴权:向 hub /internal/view/authorize 注册虚拟 viewer，页面出码后由飞书 #unlock 授权。
"""
from __future__ import annotations

import http.client
import hashlib
import hmac
import json
import os
import re
import secrets
import time
import tempfile
import threading
import urllib.request
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, quote, urlparse

import base64
import math
import sqlite3
import socket
import select
import stat
import sys
from contextlib import contextmanager

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
RENDERER_PATH = os.path.join(BASE_DIR, "renderer-canary-sse.html")
HUB_INTERNAL = os.environ.get("VS_HUB_INTERNAL", "http://127.0.0.1:8792").rstrip("/")
DEFAULT_SESSION = os.environ.get("VS_SESSION", "")
STATE_FILE = os.environ.get("VS_STATE_FILE", os.path.join(BASE_DIR, "view-state.json"))
VIEW_UNLOCK_V2 = os.environ.get("VS_VIEW_UNLOCK_V2", "0") == "1"
VIEW_TARGET_NAME = os.environ.get("VS_TARGET_NAME", "").strip()
HUB_TO_VS_SECRET_FILE = os.environ.get("VS_HUB_TO_VS_SECRET_FILE", "").strip()
PAGE_DB_FILE = os.environ.get("VS_PAGE_DB", os.path.join(BASE_DIR, "view-pages.db"))
VS_TO_HUB_SECRET_FILE = os.environ.get("VS_TO_HUB_SECRET_FILE", "").strip()
PAGE_SWEEP_INTERVAL = max(5.0, float(os.environ.get("VS_PAGE_SWEEP_INTERVAL", "30")))
# 全局任务看板投影(C3):非 session 子路径,渲染器轮询 GET /tasks 取 VS_TASKS_FILE 快照。
# 空/缺失/坏 JSON 一律 fail-closed 返 200 {tasks:[]}——绝不 404/5xx,免背景轮询噪声污染 UI。
VS_TASKS_FILE = os.environ.get("VS_TASKS_FILE", "").strip()
CONTROL_CLOCK_SKEW = 60
HUB_TO_VS_DIRECTION = "hub-to-view-service/v1"
CONTROL_UNLOCK_PATH = "/internal/control/view-page/unlock"

# Process-static capability, never inferred from a failed projection or request.
# No resume/backfill API: returning to active after a paused deployment requires
# separately approved E2 coverage and explicit exclusion of paused completions.
COST_ACCOUNTING_MODE = os.environ.get("VS_COST_ACCOUNTING_MODE", "active")


class CostAccountingPaused(RuntimeError):
    pass


def _require_cost_active():
    if COST_ACCOUNTING_MODE != "active":
        raise CostAccountingPaused("cost accounting unavailable")


def _validate_cost_config():
    if COST_ACCOUNTING_MODE not in ("active", "paused"):
        raise ValueError("invalid cost accounting mode")
    if COST_ACCOUNTING_MODE == "active":
        return
    if not VIEW_UNLOCK_V2:
        raise ValueError("paused cost accounting requires view page v2")
    cost_dir = os.path.dirname(os.path.realpath(STATE_FILE))
    def in_cost_dir(path):
        return os.path.commonpath((cost_dir, os.path.realpath(path))) == cost_dir
    if not sys.dont_write_bytecode and (in_cost_dir(BASE_DIR)
            or (sys.pycache_prefix and in_cost_dir(sys.pycache_prefix))):
        raise ValueError("Python bytecode must not write inside the cost directory")
    if in_cost_dir(PAGE_DB_FILE):
        raise ValueError("page database must be outside the cost directory")
    db_path = STATE_FILE + ".mux-cost.db"
    cost_files = (STATE_FILE, db_path, db_path + "-wal", db_path + "-shm", db_path + "-journal")
    for cost in cost_files:
        if os.path.exists(cost) and os.path.exists(PAGE_DB_FILE) and os.path.samefile(cost, PAGE_DB_FILE):
            raise ValueError("page database must not alias a cost file")
    # The service logs to stdout/stderr, not a service-owned file. Validate
    # regular-file redirection when Linux exposes it; pipes need no cost file.
    for fd in (1, 2):
        if stat.S_ISREG(os.fstat(fd).st_mode):
            target = f"/proc/self/fd/{fd}"
            if (not os.path.exists(target) or in_cost_dir(target)
                    or any(os.path.exists(cost) and os.path.samefile(target, cost) for cost in cost_files)):
                raise ValueError("service log must be outside the cost directory")


_validate_cost_config()
# Import only after paused layout validation, before any local cache can write.
from view_page_store import ViewPageStore


def _validate_public_view_base(value: str) -> str:
    value = value.strip()
    parsed = urlparse(value)
    if (not value.startswith("/") or value == "/" or value.endswith("/")
            or parsed.scheme or parsed.netloc or parsed.query or parsed.fragment
            or parsed.path != value or "//" in value):
        raise ValueError("VS_PUBLIC_VIEW_BASE must be a canonical absolute path without a trailing slash")
    segments = value[1:].split("/")
    if any(not segment or segment in (".", "..") for segment in segments):
        raise ValueError("VS_PUBLIC_VIEW_BASE must not contain empty or dot path segments")
    return value


PUBLIC_VIEW_BASE = _validate_public_view_base(os.environ.get("VS_PUBLIC_VIEW_BASE", "/view"))

_state_lock = threading.Lock()
_sessions: dict[str, dict] = {}   # session -> {viewer_id, token, cost_total, cost_last, turns}
_ready_locks_lock = threading.Lock()
_ready_locks: dict[str, threading.Lock] = {}
_page_store_lock = threading.Lock()
_page_store: ViewPageStore | None = None


def _get_page_store() -> ViewPageStore:
    _validate_cost_config()
    global _page_store
    with _page_store_lock:
        if _page_store is None:
            import time
            _page_store = ViewPageStore(PAGE_DB_FILE, now=time.time)
        return _page_store


def _run_page_sweeper(stop_event: threading.Event, interval: float,
                      store: ViewPageStore | None = None) -> None:
    store = store or _get_page_store()
    while not stop_event.wait(interval):
        try:
            store.sweep()
        except Exception:
            # A transient SQLite error must not kill the only lifecycle worker.
            continue


def _start_page_sweeper() -> tuple[threading.Event, threading.Thread] | None:
    if not VIEW_UNLOCK_V2:
        return None
    stop_event = threading.Event()
    thread = threading.Thread(
        target=_run_page_sweeper,
        args=(stop_event, PAGE_SWEEP_INTERVAL),
        name="view-page-sweeper",
        daemon=True,
    )
    thread.start()
    return stop_event, thread


def _control_signature(secret: bytes, method: str, path: str, timestamp: str,
                       nonce: str, body: bytes, direction=HUB_TO_VS_DIRECTION) -> str:
    direction_key = hmac.new(secret, direction.encode(), hashlib.sha256).digest()
    body_hash = hashlib.sha256(body).hexdigest()
    canonical = "\n".join((method, path, timestamp, nonce, body_hash)).encode()
    return hmac.new(direction_key, canonical, hashlib.sha256).hexdigest()


def _verify_control_request(headers, method: str, path: str, body: bytes,
                            store: ViewPageStore, *, now: float) -> tuple[bool, int]:
    if not VIEW_UNLOCK_V2 or not VIEW_TARGET_NAME or not HUB_TO_VS_SECRET_FILE:
        return False, 404
    target = headers.get("X-DW-VS-Target", "")
    timestamp = headers.get("X-DW-Timestamp", "")
    nonce = headers.get("X-DW-Nonce", "")
    signature = headers.get("X-DW-Signature", "")
    if target != VIEW_TARGET_NAME or not re.fullmatch(r"[0-9a-f]{32}", nonce):
        return False, 401
    try:
        request_time = int(timestamp)
    except (TypeError, ValueError):
        return False, 401
    if abs(now - request_time) > CONTROL_CLOCK_SKEW:
        return False, 401
    try:
        with open(HUB_TO_VS_SECRET_FILE, "rb") as fh:
            secret = fh.read().strip()
    except OSError:
        return False, 503
    if len(secret) < 32:
        return False, 503
    expected = _control_signature(secret, method, path, timestamp, nonce, body)
    if not hmac.compare_digest(signature, expected):
        return False, 401
    if not store.claim_nonce(target, nonce):
        return False, 409
    return True, 200


# R-V2-0102-VIEW-MENTION-ROUTE：页面候选浮层选中的目标 full_name，原样透传给 Hub（不解析正文、不判成员关系——
# 同组同 owner 校验在 Hub）。只收字符串数组，最多 16 项、单项 ≤128 字节；其余丢弃。每条最多投 5 个由 Hub 判并回执。
VIEW_MENTIONS_MAX = 16
VIEW_MENTION_MAX_BYTES = 128


def _sanitize_mentions(raw) -> list[str]:
    if not isinstance(raw, list):
        return []
    out = []
    for item in raw[:VIEW_MENTIONS_MAX]:
        if isinstance(item, str) and item and len(item.encode("utf-8")) <= VIEW_MENTION_MAX_BYTES:
            out.append(item)
    return out


def _hub_v2_input(session: str, page_id: str, request_id: str, text: str,
                  principal_id: str = "", mentions: list[str] | None = None) -> tuple[int, dict]:
    if not VIEW_TARGET_NAME or not VS_TO_HUB_SECRET_FILE:
        return 0, {"error": "v2 input auth unavailable"}
    try:
        with open(VS_TO_HUB_SECRET_FILE, "rb") as fh:
            secret = fh.read().strip()
    except OSError as exc:
        return 0, {"error": str(exc)}
    if len(secret) < 32:
        return 0, {"error": "v2 input secret too short"}
    path = f"/internal/view-v2/{session}/input"
    payload = {"session": session, "page_id": page_id, "request_id": request_id, "text": text,
               "principal_id": principal_id}
    if mentions:
        payload["mentions"] = mentions   # 无提及时请求体与之前逐字节相同
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    timestamp = str(int(time.time()))
    nonce = secrets.token_hex(16)
    signature = _control_signature(secret, "POST", path, timestamp, nonce, body,
                                   direction="view-service-to-hub/v1")
    req = urllib.request.Request(
        HUB_INTERNAL + path, data=body, method="POST",
        headers={"Content-Type": "application/json", "X-DW-VS-Target": VIEW_TARGET_NAME,
                 "X-DW-Timestamp": timestamp, "X-DW-Nonce": nonce,
                 "X-DW-Request-ID": request_id, "X-DW-Signature": signature},
    )
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            raw = response.read().decode("utf-8", "replace")
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        return exc.code, {"error": exc.read().decode("utf-8", "replace")}
    except Exception as exc:
        return 0, {"error": str(exc)}


def _hub_v2_cancel(session: str, page_id: str, request_id: str,
                   message_id: str, delivery_id: str) -> tuple[int, dict]:
    """V2 真删除链:VS 侧对 hub V2 签名 cancel 端点(dev55)的调用,镜像 _hub_v2_input。
    body 双键 G-ADDR(message_id + delivery_id,注入前无 turn_id);request_id 幂等键
    (每个取消意图一个、重试复用,hub 同 id+同 body 返 duplicate:true 不再进 core)。
    hub 只验 VS 签名(VS 侧已 authorize_page),keyID 由 hub 从 delivery 记录解析。
    返回 (hub_status, hub_body)。"""
    if not VIEW_TARGET_NAME or not VS_TO_HUB_SECRET_FILE:
        return 0, {"error": "v2 cancel auth unavailable"}
    try:
        with open(VS_TO_HUB_SECRET_FILE, "rb") as fh:
            secret = fh.read().strip()
    except OSError as exc:
        return 0, {"error": str(exc)}
    if len(secret) < 32:
        return 0, {"error": "v2 cancel secret too short"}
    path = f"/internal/view-v2/{session}/cancel"
    body = json.dumps({"session": session, "page_id": page_id, "request_id": request_id,
                       "message_id": message_id, "delivery_id": delivery_id},
                      ensure_ascii=False, separators=(",", ":")).encode()
    timestamp = str(int(time.time()))
    nonce = secrets.token_hex(16)
    signature = _control_signature(secret, "POST", path, timestamp, nonce, body,
                                   direction="view-service-to-hub/v1")
    req = urllib.request.Request(
        HUB_INTERNAL + path, data=body, method="POST",
        headers={"Content-Type": "application/json", "X-DW-VS-Target": VIEW_TARGET_NAME,
                 "X-DW-Timestamp": timestamp, "X-DW-Nonce": nonce,
                 "X-DW-Request-ID": request_id, "X-DW-Signature": signature},
    )
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            raw = response.read().decode("utf-8", "replace")
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        return exc.code, {"error": exc.read().decode("utf-8", "replace")}
    except Exception as exc:
        return 0, {"error": str(exc)}


def _hub_v2_interrupt(session: str, page_id: str, request_id: str,
                      delivery_id: str, turn_id: str) -> tuple[int, dict]:
    """V2 中断链:VS 侧对 hub V2 签名 interrupt 端点(devo48)的调用,镜像 _hub_v2_cancel。
    body 双键 delivery_id + turn_id(注入后打运行中 turn,故带 turn_id、无 message_id);
    request_id 幂等键(每个中断意图一个、重试复用)。hub 只验 VS 签名(VS 侧已 authorize_page),
    按 session 找在线 client 后写 terminalInterruptType 帧(无 Path A/B、无 CAS)。
    返回 (hub_status, hub_body)。"""
    if not VIEW_TARGET_NAME or not VS_TO_HUB_SECRET_FILE:
        return 0, {"error": "v2 interrupt auth unavailable"}
    try:
        with open(VS_TO_HUB_SECRET_FILE, "rb") as fh:
            secret = fh.read().strip()
    except OSError as exc:
        return 0, {"error": str(exc)}
    if len(secret) < 32:
        return 0, {"error": "v2 interrupt secret too short"}
    path = f"/internal/view-v2/{session}/interrupt"
    body = json.dumps({"session": session, "page_id": page_id, "request_id": request_id,
                       "delivery_id": delivery_id, "turn_id": turn_id},
                      ensure_ascii=False, separators=(",", ":")).encode()
    timestamp = str(int(time.time()))
    nonce = secrets.token_hex(16)
    signature = _control_signature(secret, "POST", path, timestamp, nonce, body,
                                   direction="view-service-to-hub/v1")
    req = urllib.request.Request(
        HUB_INTERNAL + path, data=body, method="POST",
        headers={"Content-Type": "application/json", "X-DW-VS-Target": VIEW_TARGET_NAME,
                 "X-DW-Timestamp": timestamp, "X-DW-Nonce": nonce,
                 "X-DW-Request-ID": request_id, "X-DW-Signature": signature},
    )
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            raw = response.read().decode("utf-8", "replace")
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        return exc.code, {"error": exc.read().decode("utf-8", "replace")}
    except Exception as exc:
        return 0, {"error": str(exc)}


def _read_renderer() -> bytes:
    # Per-request read keeps the single production renderer hot-updatable.
    with open(RENDERER_PATH, "rb") as fh:
        return fh.read()


# display-provenance v1 灰度 flag。渲染器把来源头像(飞书/Agent/人工/系统 四个 SVG)
# 整体挂在 window.PROVENANCE_ICONS_V1 之后;而在此之前 **VS 从未注入过该变量**
# ⇒ provFlagOn() 恒假 ⇒ 四个来源图标在生产里是死代码,页面只剩按事件类型给的
# 表情兜底(user_input→👤、assistant_text→🤖),于是「飞书头像不见了」、
# 「Agent 投递的消息显示成 👤 而不是 Agent 头像」。
# Hub 侧数据早已齐备(provenanceForEnvelope 盖 attested_by=hub + trust_decision=trusted,
# 字段名与渲染器读取逐字一致),缺的只有这一行注入。
# 仍留环境变量,便于无需改代码即可回退:VS_PROVENANCE_ICONS=0 关闭。
_PROVENANCE_ICONS_ON = os.environ.get("VS_PROVENANCE_ICONS", "1").strip().lower() not in ("0", "false", "off", "no")
_PROV_FLAG_SCRIPT = "<script>window.PROVENANCE_ICONS_V1=true;</script>"
_PROV_FLAG_ANCHOR = "</head>"


def _inject_provenance_flag(text: str) -> str:
    """在 </head> 前注入灰度 flag。找不到锚点则原样返回(fail-open:宁可没图标,不可白页)。"""
    if not _PROVENANCE_ICONS_ON or _PROV_FLAG_SCRIPT in text:
        return text
    idx = text.find(_PROV_FLAG_ANCHOR)
    if idx < 0:
        return text
    return text[:idx] + _PROV_FLAG_SCRIPT + text[idx:]


def _read_tasks_snapshot() -> dict:
    """GET /tasks 快照读取。fail-closed:无 VS_TASKS_FILE / 读失败 / 坏 JSON / 形状不符
    一律返 {"project":"","tasks":[]}(渲染器要 tasks 是 list),绝不抛——保证 handler 恒 200。"""
    if VS_TASKS_FILE:
        try:
            with open(VS_TASKS_FILE, encoding="utf-8") as fh:
                data = json.load(fh)
            if isinstance(data, dict) and isinstance(data.get("tasks"), list):
                return data
        except Exception:
            pass
    return {"project": "", "tasks": []}


_legacy_state_unreadable = False


def _load_state() -> None:
    global _legacy_state_unreadable
    _legacy_state_unreadable = False
    try:
        with open(STATE_FILE, encoding="utf-8") as fh:
            saved = json.load(fh)
        if isinstance(saved, dict) and all(isinstance(k,str) and isinstance(v,dict) for k,v in saved.items()):
            _sessions.update(saved)
        else:
            _legacy_state_unreadable = True
    except FileNotFoundError:
        pass
    except (json.JSONDecodeError, OSError):
        _legacy_state_unreadable = True


def _save_state_locked() -> None:
    _require_cost_active()
    directory = os.path.dirname(STATE_FILE) or "."
    os.makedirs(directory, mode=0o700, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".view-state-", dir=directory, text=True)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            json.dump(_sessions, fh, ensure_ascii=False)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp, STATE_FILE)
    except Exception:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


if COST_ACCOUNTING_MODE == "active":
    _load_state()


def _sess_state(session: str) -> dict:
    with _state_lock:
        st = _sessions.get(session)
        if st is None:
            st = {"viewer_id": "", "token": "", "code": "", "can_write": False,
                  "cost_total": 0.0, "cost_last": 0.0, "turns": 0}
            _sessions[session] = st
        return st


def _ready_lock_for(session: str) -> threading.Lock:
    with _ready_locks_lock:
        lock = _ready_locks.get(session)
        if lock is None:
            lock = threading.Lock()
            _ready_locks[session] = lock
        return lock


def _hub_post(base: str, path: str, obj: dict) -> tuple[int, dict]:
    data = json.dumps(obj).encode()
    req = urllib.request.Request(base + path, data=data,
                                 headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=8) as r:
            body = r.read().decode("utf-8", "replace")
            try:
                return r.status, json.loads(body)
            except Exception:
                return r.status, {"raw": body}
    except urllib.error.HTTPError as e:
        return e.code, {"error": e.read().decode("utf-8", "replace")}
    except Exception as e:
        return 0, {"error": str(e)}


def _ensure_ready(session: str) -> dict:
    """注册虚拟 viewer 拿真码;每次刷新写权(用户真 #unlock 后 can_write 会翻转)。
    返回 {viewer_id, code, can_write, token}。"""
    # 页面加载会并发请求 /events 与 /status。必须按 session 串行化首次注册，
    # 否则两条请求会各拿一个 viewer/code，后写入缓存者令先展示的解锁码失效。
    _require_cost_active()
    with _ready_lock_for(session):
        return _ensure_ready_locked(session)


def _ensure_ready_locked(session: str) -> dict:
    _require_cost_active()
    st = _sess_state(session)
    # 1) 已有 viewer:查写权。hub 返回 404 = viewer 已失效(pilot 断连重连清了 hub 侧 viewer)
    #    → 清缓存,走下面重注册(自愈,避免 pilot 重启后旧码 unlock 不到/订不到事件)。
    if st["viewer_id"]:
        c2, ws = _hub_post(HUB_INTERNAL, "/internal/view/authorize",
                           {"session": session, "viewer_id": st["viewer_id"]})
        if c2 == 200:
            with _state_lock:
                st["can_write"] = bool(ws.get("can_write"))
                if ws.get("writer_token"):
                    st["token"] = ws.get("writer_token", "")
                _save_state_locked()
            return st
        # 只有 Hub 明确确认 viewer 不存在时才能换码。网络超时、5xx 等瞬时
        # 故障保留原 viewer/token，避免一次状态探测把稳定解锁状态冲掉。
        if c2 != 404:
            return st
        with _state_lock:  # 失效(404 等):清缓存
            st["viewer_id"] = ""
            st["code"] = ""
            st["token"] = ""
            st["can_write"] = False
            _save_state_locked()
    # 2) 注册新虚拟 viewer,拿 hub 分配的真实页面码(新码,需重新 #unlock)
    code, reg = _hub_post(HUB_INTERNAL, "/internal/view/authorize", {"session": session})
    if code == 200 and reg.get("viewer_id"):
        with _state_lock:
            st["viewer_id"] = reg["viewer_id"]
            st["code"] = reg.get("code", "")
            _save_state_locked()
    return st


# 走甲最小 control:仅支持"删待提交区未注入条目"(pre-injection cancel)。
# §5 provenance / durable / 全套 control robustness 后补(张宗楚 2026-07-21 定 A)。
CONTROL_KINDS = frozenset({"cancel"})


def _forward_control(session: str, kind: str, message_id: str, delivery_id: str) -> tuple[bool, int]:
    """把 renderer 的 control(cancel)转发到 hub 内网口(dev55 做 hub 侧真删)。

    - 过 page 写权(viewer_id + writer token = G-AUTH,别裸):无写权不转发。
      写权与 /input 同源(_ensure_ready → hub /internal/view/authorize 的 writer_token,
      hub 侧再核 token)。
    - G-ADDR:message_id + delivery_id 定位待提交区条目;注入前无 turn_id,故不带。
    - 走甲最小 scope:仅 kind=cancel;其余 kind 拒(400)。
    返回 (ok, hub_status)。"""
    if kind not in CONTROL_KINDS:
        return False, 400
    if not (message_id and delivery_id):
        return False, 400
    st = _ensure_ready(session)
    if not (st.get("viewer_id") and st.get("token")):
        return False, 403  # 无 page 写权 → 不转发(别裸)
    # VS→hub 线格式(与 dev55 锁定 2026-07-21):POST /internal/view/{session}/cancel。
    # path 即表 cancel(不带 kind);鉴权沿用 /input 的 viewer_id+writer token(hub 再核)。
    # hub 仅对"注入前 pending"原子转 cancelled;终态/已注入 → 409(VS 视为 ok=False);
    # renderer 更新由 hub SSE delivery_update(cancelled)驱动(单一权威源),VS 不本地改。
    code, _ = _hub_post(HUB_INTERNAL, f"/internal/view/{session}/cancel", {
        "viewer_id": st["viewer_id"],
        "token": st["token"],
        "message_id": message_id,
        "delivery_id": delivery_id,
    })
    return (code == 200), code


def _forward_control_v2(session: str, page_id: str, page_token: str, request_id: str,
                        message_id: str, delivery_id: str, *, console_verified: bool = False) -> tuple[int, bool]:
    """V2 真删除链:人工页锁或Hub已验的Console短票，加签转发Hub V2 cancel。
    返回 (http_status, ok) 供 _handle_control 直接回 renderer。四态返回码(07-19 教训:不压 403):
      400=请求错(缺 page 凭证) / 403=真·未鉴权(authorize_page 失败,与 404/5xx 严格区分)
      200{ok:bool}=已转发(hub 200:成功/幂等 duplicate 均 ok:true;hub 409 已注入终态→ok:false 未生效)
      502=hub 5xx/网络/签名配置错(未发出,非未鉴权)。
    边界:本轮只"接通被 nginx/V2 断掉的通路",不修 cancel 抢在 accept 前的静默丢竞态
          (那是 hub durable cancel_requested 状态机、GA 前补)。"""
    if not (page_id and page_token and request_id):
        return 400, False
    # Only the handler supplies this attestation from the trusted Hub proxy;
    # never accept a payload/query flag or persist a manual page unlock.
    store = None if console_verified else _get_page_store()
    if not console_verified and not store.authorize_page(page_id, page_token, session):
        return 403, False  # 真·未鉴权
    code, payload = _hub_v2_cancel(session, page_id, request_id, message_id, delivery_id)
    if code == 200:
        # 成功 / 幂等 duplicate:true 均 ok:true;条目消失只由 hub SSE delivery_update(cancelled) 驱动。
        ok = bool(payload.get("ok"))
        if ok and not console_verified:
            store.record_successful_write(page_id, page_token)
        return 200, ok
    if code == 409:
        return 200, False  # 注入前条目已终态/已注入 → 取消未生效(非未发出)
    return 502, False  # hub 5xx/网络/签名错 → 未发出,独立码,绝不压成 403


def _forward_interrupt(session: str, delivery_id: str, turn_id: str) -> tuple[bool, int]:
    """legacy(VS_VIEW_UNLOCK_V2=0):把 renderer 的 interrupt 转发到 hub legacy 内网口。
    鉴权沿用 /input 的 viewer_id+writer token(hub 侧 HandleInternalViewInterrupt 再核 writerPage/token)。
    G-ADDR:delivery_id + turn_id(注入后打运行中 turn)。返回 (ok, hub_status)。"""
    if not (delivery_id and turn_id):
        return False, 400
    st = _ensure_ready(session)
    if not (st.get("viewer_id") and st.get("token")):
        return False, 403  # 无 page 写权 → 不转发(别裸)
    code, _ = _hub_post(HUB_INTERNAL, f"/internal/view/{session}/interrupt", {
        "viewer_id": st["viewer_id"],
        "token": st["token"],
        "delivery_id": delivery_id,
        "turn_id": turn_id,
    })
    return (code == 200), code


def _forward_interrupt_v2(session: str, page_id: str, page_token: str, request_id: str,
                          delivery_id: str, turn_id: str, *, console_verified: bool = False) -> tuple[int, bool]:
    """V2 中断链:人工页锁或Hub已验的Console短票，加签转发Hub V2 interrupt。
    返回 (http_status, ok) 供 _handle_interrupt 直接回 renderer。四态返回码(07-19 教训:不压 403):
      400=请求错(缺 page 凭证) / 403=真·未鉴权(authorize_page 失败,与 409/5xx 严格区分)
      200{ok:bool}=已转发(hub 200:成功/幂等 duplicate 均 ok:true;hub 409 会话离线不可中断→ok:false)
      502=hub 5xx/网络/签名配置错/401 验签失败(未发出,非未鉴权)。
    与 _forward_control_v2 逐字节同构,仅换端点与地址键(delivery_id+turn_id,无 message_id)。"""
    if not (page_id and page_token and request_id):
        return 400, False
    # Only the handler supplies this attestation from the trusted Hub proxy;
    # never accept a payload/query flag or persist a manual page unlock.
    store = None if console_verified else _get_page_store()
    if not console_verified and not store.authorize_page(page_id, page_token, session):
        return 403, False  # 真·未鉴权
    code, payload = _hub_v2_interrupt(session, page_id, request_id, delivery_id, turn_id)
    if code == 200:
        # 成功 / 幂等 duplicate:true 均 ok:true;turn 停止由既有 turn/流事件收尾(非特殊 SSE)。
        ok = bool(payload.get("ok"))
        if ok and not console_verified:
            store.record_successful_write(page_id, page_token)
        return 200, ok
    if code == 409:
        return 200, False  # 会话离线/无活连接 → 中断未生效(非未发出)
    return 502, False  # hub 5xx/网络/签名错/401 → 未发出,独立码,绝不压成 403


def _read_mem() -> dict:
    try:
        info = {}
        with open("/proc/meminfo") as fh:
            for line in fh:
                k, _, v = line.partition(":")
                info[k.strip()] = int(v.strip().split()[0])
        total, avail = info.get("MemTotal", 0), info.get("MemAvailable", 0)
        used = total - avail
        return {"used_mb": used // 1024, "total_mb": total // 1024,
                "pct": round(used * 100 / total, 1) if total else 0}
    except Exception:
        return {}


def _read_disk(path: str = "/home") -> dict:
    try:
        s = os.statvfs(path)
        total = s.f_blocks * s.f_frsize
        free = s.f_bavail * s.f_frsize
        used = total - free
        return {"used_gb": round(used / 1e9, 1), "total_gb": round(total / 1e9, 1),
                "pct": round(used * 100 / total, 1) if total else 0}
    except Exception:
        return {}



_SSE_EVENT_ID_RE = re.compile(r"[0-9a-f]{1,32}-[0-9]{1,20}")


def _sse_event_id(raw: str) -> str:
    """R-V2-0105 件一：只放行 <纪元>-<seq> 形状的 SSE 事件 id，其余（含注入换行）一律丢弃。"""
    raw = (raw or "").strip()
    return raw if _SSE_EVENT_ID_RE.fullmatch(raw) else ""

class CostProjection:
    def __init__(self, path):
        _require_cost_active()
        self.path = path
        self.lock = threading.Lock()
        with self._connect() as db:
            db.executescript("""
            CREATE TABLE IF NOT EXISTS mux_cost(agent_id TEXT PRIMARY KEY,
              full_name TEXT NOT NULL,cost_total REAL NOT NULL,cost_last REAL NOT NULL,turns INTEGER NOT NULL);
            CREATE TABLE IF NOT EXISTS mux_seed_floor(agent_id TEXT NOT NULL,epoch TEXT NOT NULL,H TEXT NOT NULL,PRIMARY KEY(agent_id,epoch));
            CREATE TABLE IF NOT EXISTS mux_migration(version TEXT PRIMARY KEY,fingerprint TEXT NOT NULL);
            CREATE TABLE IF NOT EXISTS mux_completed(agent_id TEXT NOT NULL,epoch TEXT NOT NULL,
              seq TEXT NOT NULL,PRIMARY KEY(agent_id,epoch,seq));
            """)

    @contextmanager
    def _connect(self):
        _require_cost_active()
        db = sqlite3.connect(self.path, timeout=0.2)
        try:
            os.chmod(self.path, 0o600)
            db.execute("PRAGMA synchronous=FULL")
            with db:
                yield db
        finally:
            db.close()

    def apply(self, agent_id, full_name, event_id, payload):
        _require_cost_active()
        match = re.fullmatch(r"([0-9a-f]{1,32})-(0|[1-9][0-9]{0,19})", event_id or "")
        if not agent_id or not full_name or not match or int(match[2]) > 2**64-1:
            raise ValueError("unsequenced projection")
        ev = json.loads(payload) if isinstance(payload, (bytes, str)) else payload
        if not isinstance(ev, dict):
            raise ValueError("invalid projection event")
        data = ev.get("data") or {}
        if ev.get("kind") != "state_change" or not isinstance(data, dict) or data.get("change") != "turn_completed":
            return False
        with self.lock, self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            floor = db.execute("SELECT H FROM mux_seed_floor WHERE agent_id=? AND epoch=?",(agent_id,match[1])).fetchone()
            if floor and int(match[2]) <= int(floor[0]):
                return False
            inserted = db.execute("INSERT OR IGNORE INTO mux_completed VALUES(?,?,?)",
                                  (agent_id, match[1], match[2])).rowcount
            if not inserted:
                return False
            row = db.execute("SELECT cost_total,cost_last,turns FROM mux_cost WHERE agent_id=?",
                             (agent_id,)).fetchone() or (0.0, 0.0, 0)
            total, last, turns = row
            cost = ev["data"].get("cost_usd")
            if isinstance(cost, (int, float)) and not isinstance(cost, bool) and math.isfinite(cost) and cost >= total:
                last, total = cost - total, cost
            n = ev["data"].get("num_turns")
            turns = max(turns, n) if isinstance(n, int) and not isinstance(n, bool) and n >= 0 else turns + 1
            db.execute("INSERT INTO mux_cost VALUES(?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET "
                       "full_name=excluded.full_name,cost_total=excluded.cost_total,"
                       "cost_last=excluded.cost_last,turns=excluded.turns", (agent_id, full_name, total, last, turns))
            # Context manager commits both the unique key and all side effects, or rolls both back.
            return True

    def has_seed(self, fingerprint):
        with self.lock, self._connect() as db:
            prior=db.execute("SELECT fingerprint FROM mux_migration WHERE version='cost-seed-v1'").fetchone()
            if prior and prior[0]!=fingerprint:
                raise ValueError("migration fingerprint conflict")
            return bool(prior)

    def seed(self, rows, fingerprint):
        # Offline only: caller has verified approved alias mapping and fresh
        # signed target authority. One transaction includes costs, replay cuts
        # and migration marker. Existing seen entries are never removed.
        with self.lock, self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            prior=db.execute("SELECT fingerprint FROM mux_migration WHERE version='cost-seed-v1'").fetchone()
            if prior:
                if prior[0] != fingerprint:
                    raise ValueError("migration fingerprint conflict")
                return False
            for row in rows:
                agent, name = row['agent_id'], row['full_name']
                values = (row['cost_total'],row['cost_last'],row['turns'])
                existing=db.execute("SELECT cost_total,cost_last,turns FROM mux_cost WHERE agent_id=?",(agent,)).fetchone()
                if existing and tuple(existing)!=values:
                    raise ValueError("existing cost conflict")
                if not existing:
                    db.execute("INSERT INTO mux_cost VALUES(?,?,?,?,?)",(agent,name,*values))
                db.execute("INSERT INTO mux_seed_floor VALUES(?,?,?)",(agent,row['epoch'],row['H']))
            db.execute("INSERT INTO mux_migration VALUES('cost-seed-v1',?)",(fingerprint,))
            return True

    def require_legacy_seed(self, sessions, unreadable=False):
        historical=any(any(v.get(k,0) != 0 for k in ('cost_total','cost_last','turns'))
                       for v in sessions.values() if isinstance(v,dict))
        if historical or unreadable:
            with self.lock, self._connect() as db:
                if not db.execute("SELECT 1 FROM mux_migration WHERE version='cost-seed-v1'").fetchone():
                    raise sqlite3.OperationalError("historical cost migration required")

    def status(self, agent_id):
        with self.lock, self._connect() as db:
            row = db.execute("SELECT cost_total,cost_last,turns FROM mux_cost WHERE agent_id=?",
                             (agent_id,)).fetchone() or (0.0, 0.0, 0)
            return dict(zip(("cost_total", "cost_last", "turns"), row))


MUX_FRAME_MAX = 1 << 20
_projection_lock = threading.Lock()
_projection = None
_projection_path = None


def _cost_projection():
    _require_cost_active()
    global _projection, _projection_path
    path = STATE_FILE + ".mux-cost.db"
    with _projection_lock:
        if _projection is None or _projection_path != path:
            os.makedirs(os.path.dirname(path) or ".", mode=0o700, exist_ok=True)
            candidate = CostProjection(path)
            with _state_lock:
                candidate.require_legacy_seed(_sessions, _legacy_state_unreadable)
            _projection = candidate
            _projection_path = path
        return _projection


def _bounded_sse_frames(response):
    frame = bytearray()
    while True:
        line = response.readline(MUX_FRAME_MAX + 1 - len(frame))
        if not line:
            if frame:
                raise ValueError("incomplete SSE frame")
            return
        frame.extend(line)
        if len(frame) > MUX_FRAME_MAX:
            raise ValueError("event_too_large")
        if line in (b"\n", b"\r\n"):
            yield bytes(frame)
            frame.clear()


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _session(self) -> str:
        s = self.headers.get("X-DW-Session")  # hub 路由器反代时带
        if s:
            return s.strip()
        return DEFAULT_SESSION.strip()

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/index.html"):
            return self._serve_file()
        if path == "/events":
            return self._serve_events()
        if path == "/status":
            return self._serve_status()
        if path == "/tasks":
            return self._serve_tasks()
        self.send_error(404)

    def _serve_tasks(self):
        """全局任务看板快照(GET /tasks,非 scoped)。fail-closed:无 VS_TASKS_FILE / 读失败 / 坏 JSON
        一律返 200 {tasks:[]},绝不 404/5xx——渲染器背景轮询不产噪声、不污染取消 UI。
        注意:该端点非 session 子路径,生产需 nginx 一并把裸 /tasks 路由到 VS 才生效。"""
        return self._json_response(200, _read_tasks_snapshot())

    def _serve_file(self):
        try:
            if not self._session():
                return self.send_error(400, "session required")
            body = _read_renderer()
        except Exception as exc:
            return self.send_error(500, str(exc))
        # The trusted router supplies the session. This process-level base is a deployment fact,
        # so the renderer has one path rewriter and request headers cannot choose its public prefix.
        hdr_sess = self.headers.get("X-DW-Session")
        text = body.decode("utf-8", "replace")
        if hdr_sess:
            pre = PUBLIC_VIEW_BASE + "/" + hdr_sess.strip()
            for ep in ("/events", "/input", "/interrupt", "/control", "/status", "/page"):
                text = text.replace("'" + ep + "'", "'" + pre + ep + "'")
        # 🔴 注入**无条件**:来源头像与是否带 X-DW-Session 无关,
        # 放进上面的 if 里会让直连 VS 的场景永远看不到图标。
        text = _inject_provenance_flag(text)
        mode_marker = "/* VS_COST_ACCOUNTING_MODE */ 'active'"
        if COST_ACCOUNTING_MODE == "paused" and text.count(mode_marker) != 1:
            return self.send_error(503, "cost mode template unavailable")
        text = text.replace(mode_marker, json.dumps(COST_ACCOUNTING_MODE))
        body = text.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _mux_subject(self):
        encoded = self.headers.get("X-DW-Mux-Subject", "")
        if not encoded:
            return None
        try:
            if len(encoded) > 2048:
                raise ValueError("subject size")
            raw = base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4))
            ok, _ = _verify_control_request(self.headers, "GET", urlparse(self.path).path,
                                            raw, _get_page_store(), now=time.time())
            subject = json.loads(raw)
            if not ok or subject.get("full_name") != self._session() or not subject.get("agent_id"):
                raise ValueError("subject mismatch")
            return subject
        except Exception:
            raise ValueError("invalid projection subject")

    def _serve_mux_bridge(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            return self.send_error(400)
        if not 0 < size <= 4096:
            return self.send_error(400)
        raw = self.rfile.read(size)
        ok, code = _verify_control_request(self.headers, "POST", "/internal/console-mux/events",
                                           raw, _get_page_store(), now=time.time())
        if not ok:
            return self.send_error(code)
        try:
            request = json.loads(raw)
            feed, bridge = request["feed_id"], request["bridge_id"]
            if not re.fullmatch(r"[0-9a-f]{32}", feed) or not re.fullmatch(r"[0-9a-f]{32}", bridge) or request["target"] != VIEW_TARGET_NAME:
                raise ValueError("bridge identity")
            with open(VS_TO_HUB_SECRET_FILE, "rb") as fh:
                secret = fh.read().strip()
            if len(secret) < 32:
                raise ValueError("service authentication unavailable")
            path = f"/internal/console/event-feeds/{feed}/bridges/{bridge}/events"
            body = json.dumps({"target": VIEW_TARGET_NAME}, separators=(",", ":")).encode()
            timestamp, nonce = str(int(time.time())), secrets.token_hex(16)
            signature = _control_signature(secret, "POST", path, timestamp, nonce, body,
                                           direction="view-service-to-hub/v1")
            u = urlparse(HUB_INTERNAL)
            conn = http.client.HTTPConnection(u.hostname, u.port or 80, timeout=1)
            conn.request("POST", path, body=body, headers={"Content-Type": "application/json",
                "X-DW-VS-Target": VIEW_TARGET_NAME, "X-DW-Timestamp": timestamp,
                "X-DW-Nonce": nonce, "X-DW-Signature": signature})
            response = conn.getresponse()
            if response.status != 200:
                conn.close()
                return self.send_error(502)
            # Socket write/flush is bounded; abandoned Hub readers cannot pin VS threads indefinitely.
            self.connection.settimeout(0.25)
            response.fp.raw._sock.settimeout(20)
            upstream_socket = response.fp.raw._sock
            peer_done = threading.Event()
            def stop_upstream_on_abandoned_peer():
                while not peer_done.wait(0.1):
                    try:
                        readable, _, _ = select.select([self.connection], [], [], 0)
                        if not readable or self.connection.recv(1, socket.MSG_PEEK):
                            continue
                    except OSError:
                        pass
                    try:
                        upstream_socket.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass
                    return
            peer_monitor = threading.Thread(target=stop_upstream_on_abandoned_peer, daemon=True)
            peer_monitor.start()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Accel-Buffering", "no")
            self.end_headers()
            try:
                for frame in _bounded_sse_frames(response):
                    for line in frame.splitlines():
                        if line.startswith(b"data: "):
                            event = json.loads(line[6:])
                            if event.get("type") == "event" and COST_ACCOUNTING_MODE == "active":
                                _cost_projection().apply(event["agent_id"], event["full_name"],
                                     event["epoch"] + "-" + event["seq"], event["payload"])
                    self.wfile.write(frame)
                    self.wfile.flush()
            finally:
                peer_done.set()
                conn.close()
                peer_monitor.join(timeout=0.5)
        except (ValueError, OSError, KeyError, json.JSONDecodeError, sqlite3.Error):
            self.close_connection = True
            return

    def _serve_events(self):
        session = self._session()
        if not session:
            return self.send_error(400, "session required")
        try:
            subject = self._mux_subject()
        except ValueError:
            return self.send_error(401)
        if COST_ACCOUNTING_MODE == "active":
            _ensure_ready(session)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Accel-Buffering", "no")
        self.end_headers()
        # 转发 hub 的内网 SSE(C2),逐行透传(含 id:／event: 行),并记成本
        u = urlparse(HUB_INTERNAL)
        conn = http.client.HTTPConnection(u.hostname, u.port or 80, timeout=3600)
        try:
            # R-V2-0105 件一：页面重连带 after=<纪元>-<seq>（或浏览器原生重连的 Last-Event-ID）⇒ 校验后转给 Hub，只补缺口。
            target = f"/internal/view/{session}/events"
            headers = {}
            after = _sse_event_id(parse_qs(urlparse(self.path).query).get("after", [""])[0])
            if after:
                target += "?after=" + quote(after, safe="")
            last_event_id = _sse_event_id(self.headers.get("Last-Event-ID", ""))
            if last_event_id:
                headers["Last-Event-ID"] = last_event_id
            conn.request("GET", target, headers=headers)
            resp = conn.getresponse()
            for frame in _bounded_sse_frames(resp):
                event_id = ""
                for line in frame.splitlines():
                    if line.startswith(b"id: "):
                        event_id = line[4:].decode("ascii")
                    elif line.startswith(b"data: "):
                        if COST_ACCOUNTING_MODE == "active":
                            if subject:
                                _cost_projection().apply(subject["agent_id"], session, event_id, line[6:])
                            else:
                                self._track_cost(session, line[6:].strip())
                self.wfile.write(frame)
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return
        except Exception:
            # Projection failure after SSE starts requires closing without forwarding.
            self.close_connection = True
            return
        finally:
            conn.close()

    def _track_cost(self, session: str, payload: bytes):
        _require_cost_active()
        try:
            ev = json.loads(payload)
        except Exception:
            return
        if ev.get("kind") == "state_change" and ev.get("data", {}).get("change") == "turn_completed":
            cost = ev["data"].get("cost_usd")
            st = _sess_state(session)
            if isinstance(cost, (int, float)):
                with _state_lock:
                    st["cost_last"] = max(0.0, cost - st["cost_total"])
                    st["cost_total"] = cost
                    _save_state_locked()
            n = ev["data"].get("num_turns")
            with _state_lock:
                st["turns"] = n if isinstance(n, int) else st["turns"] + 1
                _save_state_locked()

    def _console_page_attested(self, payload):
        # Only Hub-derived trust headers qualify; never consult payload flags or URL.
        page_id = payload.get("page_id")
        return (self.headers.get("X-DW-Console-Capability-Verified") == "1"
                and isinstance(page_id, str) and bool(page_id)
                and self.headers.get("X-DW-Console-Page", "") == page_id
                and bool(self.headers.get("X-DW-Console-Principal", "").strip()))

    def _serve_status(self):
        session = self._session()
        if not session:
            return self.send_error(400, "session required")
        query = parse_qs(urlparse(self.path).query)
        resources_only = query.get("resources_only") == ["1"]
        auth_only = query.get("auth_only") == ["1"]
        if VIEW_UNLOCK_V2:
            try:
                subject = self._mux_subject()
            except ValueError:
                return self.send_error(401)
        if resources_only:
            if VIEW_UNLOCK_V2 and subject is None:
                return self.send_error(401)
            # Read-only environment snapshot: no page identity/cost/collector creation.
            # Existing signed-subject nonce checks may use the separate page DB.
            return self._json_response(200, {"mem": _read_mem(), "disk": _read_disk(),
                "datetime_epoch_ms": int(time.time() * 1000), "resource_scope": "view-service"})
        if auth_only:
            if VIEW_UNLOCK_V2:
                return self._json_response(200, {"auth_mode": "view_page_v2", "readonly": True})
            st = _ensure_ready(session)
            return self._json_response(200, {"readonly": not st.get("can_write", False)})
        if VIEW_UNLOCK_V2:
            try:
                if COST_ACCOUNTING_MODE == "paused":
                    costs = {"cost_accounting": {"mode": "paused", "available": False,
                                                "history": "preserved_unmapped"}}
                else:
                    costs = {"cost_total": 0, "cost_last": 0, "turns": 0}
                    if subject:
                        costs.update(_cost_projection().status(subject["agent_id"]))
            except ValueError:
                return self.send_error(401)
            except (sqlite3.Error, OSError):
                return self.send_error(503, "cost projection unavailable")
            body = json.dumps({
                "session": session, "session_id": session, "state": "idle", "driver": "via-hub",
                "auth_mode": "view_page_v2", "can_write": False, "readonly": True,
                "unlock_code": "", **costs,
                "mem": _read_mem(), "disk": _read_disk(),
                "datetime": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
                "datetime_epoch_ms": int(time.time() * 1000), "resource_scope": "view-service",
            }, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)
            return
        st = _ensure_ready(session)  # 注册虚拟 viewer + 刷新写权(拿真码/can_write)
        body = json.dumps({
            "session": session,
            "state": "idle",
            "driver": "via-hub",
            "unlock_code": st.get("code", ""),      # 前端显示的真码(替代写死的假码)
            "can_write": st.get("can_write", False),
            "readonly": not st.get("can_write", False),
            "cost_total": round(st["cost_total"], 4),
            "cost_last": round(st["cost_last"], 4),
            "turns": st["turns"],
            "mem": _read_mem(),
            "disk": _read_disk(),
            "datetime": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
            "datetime_epoch_ms": int(time.time() * 1000), "resource_scope": "view-service",
        }, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        path = urlparse(self.path).path
        if path == "/internal/console-mux/events":
            return self._serve_mux_bridge()
        if path == CONTROL_UNLOCK_PATH:
            return self._control_unlock()
        session = self._session()
        if not session:
            return self.send_error(400, "session required")
        if path == "/control":
            return self._handle_control(session)
        if path == "/interrupt":
            return self._handle_interrupt(session)
        if path == "/page":
            if not VIEW_UNLOCK_V2:
                return self.send_error(404)
            page = _get_page_store().create_page(session)
            return self._json_response(201, page)
        if path in ("/page/resume", "/page/disconnect"):
            if not VIEW_UNLOCK_V2:
                return self.send_error(404)
            length = int(self.headers.get("Content-Length") or 0)
            try:
                payload = json.loads(self.rfile.read(length) if length else b"{}")
            except json.JSONDecodeError:
                return self._json_response(400, {"ok": False})
            page_id, page_token = str(payload.get("page_id", "")), str(payload.get("page_token", ""))
            if path == "/page/disconnect":
                ok = _get_page_store().mark_disconnected(page_id, page_token)
                return self._json_response(200 if ok else 403, {"ok": ok})
            page = _get_page_store().resume_page(page_id, page_token, session)
            return self._json_response(200, page) if page else self._json_response(403, {"ok": False})
        if path != "/input":
            return self.send_error(404)
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            incoming = json.loads(raw)
            text = incoming.get("text", "")
        except Exception:
            incoming = {}
            text = ""
        ok = False
        mentions = _sanitize_mentions(incoming.get("mentions"))
        receipts = None
        if isinstance(text, str) and text:
            if VIEW_UNLOCK_V2:
                page_id = str(incoming.get("page_id", ""))
                page_token = str(incoming.get("page_token", ""))
                request_id = str(incoming.get("request_id", ""))
                store = _get_page_store()
                console_verified = self.headers.get("X-DW-Console-Capability-Verified") == "1"
                if page_id and page_token and request_id and (console_verified or store.authorize_page(page_id, page_token, session)):
                    principal_id = self.headers.get("X-DW-Console-Principal", "") if console_verified else ""
                    extra = {"mentions": mentions} if mentions else {}   # 无提及时调用形状与之前相同
                    code, hub_body = _hub_v2_input(session, page_id, request_id, text, principal_id, **extra)
                    ok = code == 200
                    if ok and isinstance(hub_body, dict) and isinstance(hub_body.get("mentions"), list):
                        receipts = hub_body["mentions"]
                    if ok and not console_verified:
                        store.record_successful_write(page_id, page_token)
            else:
                st = _ensure_ready(session)
                if st.get("viewer_id") and st.get("token"):
                    # pilot 按键式输入以末尾 CR 提交；内嵌 LF 由 adapter 保留为消息换行。
                    payload_text = text.replace("\r\n", "\n").replace("\r", "\n") + "\r"
                    legacy = {"viewer_id": st["viewer_id"], "token": st["token"], "text": payload_text}
                    if mentions:
                        legacy["mentions"] = mentions
                    code, hub_body = _hub_post(HUB_INTERNAL, f"/internal/view/{session}/input", legacy)
                    ok = (code == 200)
                    if ok and isinstance(hub_body, dict) and isinstance(hub_body.get("mentions"), list):
                        receipts = hub_body["mentions"]
        result = {"ok": ok}
        if receipts is not None:
            result["mentions"] = receipts   # Hub 逐目标回执原样回页面
        return self._json_response(200 if ok else 403, result)

    def _control_unlock(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > 65536:
            return self._json_response(400, {"ok": False})
        raw = self.rfile.read(length)
        import time
        store = _get_page_store()
        verified, status = _verify_control_request(
            self.headers, "POST", CONTROL_UNLOCK_PATH, raw, store, now=time.time())
        if not verified:
            return self._json_response(status, {"ok": False})
        try:
            command = json.loads(raw)
        except (TypeError, json.JSONDecodeError):
            return self._json_response(400, {"ok": False})
        status = store.unlock(command)
        return self._json_response(status, {"ok": status == 200})

    def _json_response(self, status: int, obj: dict):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _handle_control(self, session: str):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            obj = json.loads(raw)
            if not isinstance(obj, dict):
                obj = {}
        except Exception:
            obj = {}
        kind = str(obj.get("kind", "")).strip()
        message_id = str(obj.get("message_id", "")).strip()
        delivery_id = str(obj.get("delivery_id", "")).strip()
        # 模式门互斥(镜像 /input):V2 下走 V2 分支、legacy 分支不可达 → 无未鉴权控制入口。
        if VIEW_UNLOCK_V2:
            # V2 走甲最小 scope:仅 kind=cancel + G-ADDR 双键;四态返回码(不压 403)见 _forward_control_v2。
            if kind not in CONTROL_KINDS or not (message_id and delivery_id):
                return self._json_response(400, {"ok": False})
            status, ok = _forward_control_v2(
                session,
                str(obj.get("page_id", "")).strip(),
                str(obj.get("page_token", "")).strip(),
                str(obj.get("request_id", "")).strip(),
                message_id, delivery_id,
                **({"console_verified": True} if self._console_page_attested(obj) else {}))
            return self._json_response(status, {"ok": ok})
        # legacy(VS_VIEW_UNLOCK_V2=0):直通 _forward_control(viewer_id+writer token),行为完全不变。
        ok, _ = _forward_control(session, kind, message_id, delivery_id)
        self._json_response(200, {"ok": ok})

    def _handle_interrupt(self, session: str):
        """中断运行中 turn。镜像 _handle_control 的模式门(V2/legacy 互斥)。
        body:{delivery_id,turn_id,page_id?,page_token?,request_id?}。
        interrupt 带 turn_id(注入后)、无 message_id/kind(与 cancel 相反)。"""
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            obj = json.loads(raw)
            if not isinstance(obj, dict):
                obj = {}
        except Exception:
            obj = {}
        delivery_id = str(obj.get("delivery_id", "")).strip()
        turn_id = str(obj.get("turn_id", "")).strip()
        # 模式门互斥(镜像 /input、/control):V2 下走 V2 分支、legacy 分支不可达。
        if VIEW_UNLOCK_V2:
            if not (delivery_id and turn_id):
                return self._json_response(400, {"ok": False})
            status, ok = _forward_interrupt_v2(
                session,
                str(obj.get("page_id", "")).strip(),
                str(obj.get("page_token", "")).strip(),
                str(obj.get("request_id", "")).strip(),
                delivery_id, turn_id,
                **({"console_verified": True} if self._console_page_attested(obj) else {}))
            return self._json_response(status, {"ok": ok})
        # legacy:直通 _forward_interrupt(viewer_id+writer token,hub legacy interrupt 端点)。
        ok, _ = _forward_interrupt(session, delivery_id, turn_id)
        self._json_response(200, {"ok": ok})


def main():
    _validate_cost_config()
    port = int(os.environ.get("VS_PORT", "19320"))
    sweeper = _start_page_sweeper()
    srv = ThreadingHTTPServer(("127.0.0.1", port), _Handler)
    print(f"[view-service] :{port} hub_internal={HUB_INTERNAL} "
          f"session={DEFAULT_SESSION or '(from X-DW-Session)'} renderer={RENDERER_PATH} "
          f"public_view_base={PUBLIC_VIEW_BASE}", flush=True)
    try:
        srv.serve_forever()
    finally:
        if sweeper:
            sweeper[0].set()
            sweeper[1].join(timeout=PAGE_SWEEP_INTERVAL + 1)


if __name__ == "__main__":
    main()
