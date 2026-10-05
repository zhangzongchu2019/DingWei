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
from urllib.parse import urlparse

from view_page_store import ViewPageStore

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


def _hub_v2_input(session: str, page_id: str, request_id: str, text: str,
                  principal_id: str = "") -> tuple[int, dict]:
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
    body = json.dumps({"session": session, "page_id": page_id, "request_id": request_id, "text": text,
                       "principal_id": principal_id},
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


def _load_state() -> None:
    try:
        with open(STATE_FILE, encoding="utf-8") as fh:
            saved = json.load(fh)
        if isinstance(saved, dict):
            _sessions.update({k: v for k, v in saved.items() if isinstance(k, str) and isinstance(v, dict)})
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        pass


def _save_state_locked() -> None:
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
    with _ready_lock_for(session):
        return _ensure_ready_locked(session)


def _ensure_ready_locked(session: str) -> dict:
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
                        message_id: str, delivery_id: str) -> tuple[int, bool]:
    """V2 真删除链 VS 侧:page 授权(authorize_page,V2 唯一写权门)+ 签名转发 hub V2 cancel。
    返回 (http_status, ok) 供 _handle_control 直接回 renderer。四态返回码(07-19 教训:不压 403):
      400=请求错(缺 page 凭证) / 403=真·未鉴权(authorize_page 失败,与 404/5xx 严格区分)
      200{ok:bool}=已转发(hub 200:成功/幂等 duplicate 均 ok:true;hub 409 已注入终态→ok:false 未生效)
      502=hub 5xx/网络/签名配置错(未发出,非未鉴权)。
    边界:本轮只"接通被 nginx/V2 断掉的通路",不修 cancel 抢在 accept 前的静默丢竞态
          (那是 hub durable cancel_requested 状态机、GA 前补)。"""
    if not (page_id and page_token and request_id):
        return 400, False
    store = _get_page_store()
    if not store.authorize_page(page_id, page_token, session):
        return 403, False  # 真·未鉴权
    code, payload = _hub_v2_cancel(session, page_id, request_id, message_id, delivery_id)
    if code == 200:
        # 成功 / 幂等 duplicate:true 均 ok:true;条目消失只由 hub SSE delivery_update(cancelled) 驱动。
        ok = bool(payload.get("ok"))
        if ok:
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
                          delivery_id: str, turn_id: str) -> tuple[int, bool]:
    """V2 中断链 VS 侧:page 授权(authorize_page,V2 唯一写权门)+ 签名转发 hub V2 interrupt。
    返回 (http_status, ok) 供 _handle_interrupt 直接回 renderer。四态返回码(07-19 教训:不压 403):
      400=请求错(缺 page 凭证) / 403=真·未鉴权(authorize_page 失败,与 409/5xx 严格区分)
      200{ok:bool}=已转发(hub 200:成功/幂等 duplicate 均 ok:true;hub 409 会话离线不可中断→ok:false)
      502=hub 5xx/网络/签名配置错/401 验签失败(未发出,非未鉴权)。
    与 _forward_control_v2 逐字节同构,仅换端点与地址键(delivery_id+turn_id,无 message_id)。"""
    if not (page_id and page_token and request_id):
        return 400, False
    store = _get_page_store()
    if not store.authorize_page(page_id, page_token, session):
        return 403, False  # 真·未鉴权
    code, payload = _hub_v2_interrupt(session, page_id, request_id, delivery_id, turn_id)
    if code == 200:
        # 成功 / 幂等 duplicate:true 均 ok:true;turn 停止由既有 turn/流事件收尾(非特殊 SSE)。
        ok = bool(payload.get("ok"))
        if ok:
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
        body = text.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _serve_events(self):
        session = self._session()
        if not session:
            return self.send_error(400, "session required")
        _ensure_ready(session)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Accel-Buffering", "no")
        self.end_headers()
        # 转发 hub 的内网 SSE(C2),逐行透传,并记成本
        u = urlparse(HUB_INTERNAL)
        conn = http.client.HTTPConnection(u.hostname, u.port or 80, timeout=3600)
        try:
            conn.request("GET", f"/internal/view/{session}/events")
            resp = conn.getresponse()
            while True:
                line = resp.readline()
                if not line:
                    break
                if line.startswith(b"data: "):
                    self._track_cost(session, line[6:].strip())
                self.wfile.write(line)
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return
        except Exception:
            return
        finally:
            conn.close()

    def _track_cost(self, session: str, payload: bytes):
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

    def _serve_status(self):
        session = self._session()
        if not session:
            return self.send_error(400, "session required")
        if VIEW_UNLOCK_V2:
            body = json.dumps({
                "session": session, "session_id": session, "state": "idle", "driver": "via-hub",
                "auth_mode": "view_page_v2", "can_write": False, "readonly": True,
                "unlock_code": "", "cost_total": 0, "cost_last": 0, "turns": 0,
                "mem": _read_mem(), "disk": _read_disk(),
                "datetime": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
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
        }, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        path = urlparse(self.path).path
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
        if isinstance(text, str) and text:
            if VIEW_UNLOCK_V2:
                page_id = str(incoming.get("page_id", ""))
                page_token = str(incoming.get("page_token", ""))
                request_id = str(incoming.get("request_id", ""))
                store = _get_page_store()
                console_verified = self.headers.get("X-DW-Console-Capability-Verified") == "1"
                if page_id and page_token and request_id and (console_verified or store.authorize_page(page_id, page_token, session)):
                    principal_id = self.headers.get("X-DW-Console-Principal", "") if console_verified else ""
                    code, _ = _hub_v2_input(session, page_id, request_id, text, principal_id)
                    ok = code == 200
                    if ok and not console_verified:
                        store.record_successful_write(page_id, page_token)
            else:
                st = _ensure_ready(session)
                if st.get("viewer_id") and st.get("token"):
                    # pilot 按键式输入以末尾 CR 提交；内嵌 LF 由 adapter 保留为消息换行。
                    payload_text = text.replace("\r\n", "\n").replace("\r", "\n") + "\r"
                    code, _ = _hub_post(HUB_INTERNAL, f"/internal/view/{session}/input",
                                        {"viewer_id": st["viewer_id"], "token": st["token"], "text": payload_text})
                    ok = (code == 200)
        return self._json_response(200 if ok else 403, {"ok": ok})

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
                message_id, delivery_id)
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
                delivery_id, turn_id)
            return self._json_response(status, {"ok": ok})
        # legacy:直通 _forward_interrupt(viewer_id+writer token,hub legacy interrupt 端点)。
        ok, _ = _forward_interrupt(session, delivery_id, turn_id)
        self._json_response(200, {"ok": ok})


def main():
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
