"""View Service —— 独立进程,对接 hub 内网口(C2/C3),服务新事件流页。

与 sidecar 的区别:sidecar 寄生在 helper 进程里、共享 driver;
View Service 是独立进程,只跟 **hub** 打交道(不认识 driver):
  GET  /                浏览器页面(renderer.html)
  GET  /events          SSE:转发 hub /internal/view/{session}/events(C2),顺带记成本
  POST /input {text}    → hub /internal/view/{session}/input(C3),必要时先解锁
  GET  /status          状态栏数据(会话/成本/内存/磁盘/时间)

会话来源:hub 入口路由器(C4)反代时带的 X-DW-Session 头;缺省用 VS_SESSION。
鉴权:向 hub /internal/view/authorize 注册虚拟 viewer;演示用 mock-unlock(code 0000)
自动解锁——真上线换成"页面出码→飞书 #unlock→轮询 authorize"。
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
from urllib.parse import urlparse, parse_qs

from view_page_store import ViewPageStore

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
RENDERER = os.environ.get("VS_RENDERER", os.path.join(BASE_DIR, "renderer.html"))
CANARY_SESSIONS = frozenset(
    session.strip()
    for session in os.environ.get("VS_CANARY_SESSIONS", "").split(",")
    if session.strip()
)
CANARY_RENDERER = os.environ.get("VS_CANARY_RENDERER", "").strip()
HUB_INTERNAL = os.environ.get("VS_HUB_INTERNAL", "http://127.0.0.1:8792").rstrip("/")
HUB_PUBLIC = os.environ.get("VS_HUB_PUBLIC", "http://127.0.0.1:8791").rstrip("/")
DEFAULT_SESSION = os.environ.get("VS_SESSION", "")
AUTO_UNLOCK = os.environ.get("VS_AUTO_UNLOCK", "1") == "1"   # 演示便利:mock-unlock 自动解锁
STATE_FILE = os.environ.get("VS_STATE_FILE", os.path.join(BASE_DIR, "view-state.json"))
VIEW_UNLOCK_V2 = os.environ.get("VS_VIEW_UNLOCK_V2", "0") == "1"
VIEW_TARGET_NAME = os.environ.get("VS_TARGET_NAME", "").strip()
HUB_TO_VS_SECRET_FILE = os.environ.get("VS_HUB_TO_VS_SECRET_FILE", "").strip()
PAGE_DB_FILE = os.environ.get("VS_PAGE_DB", os.path.join(BASE_DIR, "view-pages.db"))
VS_TO_HUB_SECRET_FILE = os.environ.get("VS_TO_HUB_SECRET_FILE", "").strip()
PAGE_SWEEP_INTERVAL = max(5.0, float(os.environ.get("VS_PAGE_SWEEP_INTERVAL", "30")))
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


def _hub_v2_input(session: str, page_id: str, request_id: str, text: str) -> tuple[int, dict]:
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
    body = json.dumps({"session": session, "page_id": page_id, "request_id": request_id, "text": text},
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


def _renderer_path_for_session(session: str) -> str:
    if CANARY_RENDERER and session in CANARY_SESSIONS:
        return CANARY_RENDERER
    return RENDERER


def _read_renderer(session: str) -> bytes:
    # Per-request read keeps renderer hot updates working for both tracks.
    with open(_renderer_path_for_session(session), "rb") as fh:
        return fh.read()


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
        if AUTO_UNLOCK and st["code"]:  # 演示模式(mock-unlock 开时)才自动解锁
            _hub_post(HUB_PUBLIC, "/api/terminal/mock-unlock", {"code": "0000", "session_name": session})
    return st


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
        q = parse_qs(urlparse(self.path).query)
        if q.get("session"):
            return q["session"][0]
        return DEFAULT_SESSION

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/index.html"):
            return self._serve_file()
        if path == "/events":
            return self._serve_events()
        if path == "/status":
            return self._serve_status()
        self.send_error(404)

    def _serve_file(self):
        try:
            body = _read_renderer(self._session())
        except Exception as exc:
            return self.send_error(500, str(exc))
        # The trusted router supplies the session. This process-level base is a deployment fact,
        # so the renderer has one path rewriter and request headers cannot choose its public prefix.
        hdr_sess = self.headers.get("X-DW-Session")
        if hdr_sess:
            pre = PUBLIC_VIEW_BASE + "/" + hdr_sess.strip()
            text = body.decode("utf-8", "replace")
            for ep in ("/events", "/input", "/interrupt", "/status", "/page"):
                text = text.replace("'" + ep + "'", "'" + pre + ep + "'")
            body = text.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _serve_events(self):
        session = self._session()
        _ensure_ready(session)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
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
        if path == "/page":
            if not VIEW_UNLOCK_V2:
                return self.send_error(404)
            page = _get_page_store().create_page(self._session())
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
            page = _get_page_store().resume_page(page_id, page_token, self._session())
            return self._json_response(200, page) if page else self._json_response(403, {"ok": False})
        if path != "/input":
            return self.send_error(404)
        session = self._session()
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
                if page_id and page_token and request_id and store.authorize_page(page_id, page_token, session):
                    code, _ = _hub_v2_input(session, page_id, request_id, text)
                    ok = code == 200
                    if ok:
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


def main():
    port = int(os.environ.get("VS_PORT", "19320"))
    sweeper = _start_page_sweeper()
    srv = ThreadingHTTPServer(("127.0.0.1", port), _Handler)
    print(f"[view-service] :{port} hub_internal={HUB_INTERNAL} hub_public={HUB_PUBLIC} "
          f"session={DEFAULT_SESSION or '(from X-DW-Session)'} renderer={RENDERER} "
          f"public_view_base={PUBLIC_VIEW_BASE}", flush=True)
    try:
        srv.serve_forever()
    finally:
        if sweeper:
            sweeper[0].set()
            sweeper[1].join(timeout=PAGE_SWEEP_INTERVAL + 1)


if __name__ == "__main__":
    main()
