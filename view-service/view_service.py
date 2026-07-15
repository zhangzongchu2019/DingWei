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
import json
import os
import tempfile
import threading
import urllib.request
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
RENDERER = os.environ.get("VS_RENDERER", os.path.join(BASE_DIR, "renderer.html"))
HUB_INTERNAL = os.environ.get("VS_HUB_INTERNAL", "http://127.0.0.1:8792").rstrip("/")
HUB_PUBLIC = os.environ.get("VS_HUB_PUBLIC", "http://127.0.0.1:8791").rstrip("/")
DEFAULT_SESSION = os.environ.get("VS_SESSION", "")
AUTO_UNLOCK = os.environ.get("VS_AUTO_UNLOCK", "1") == "1"   # 演示便利:mock-unlock 自动解锁
STATE_FILE = os.environ.get("VS_STATE_FILE", os.path.join(BASE_DIR, "view-state.json"))

_state_lock = threading.Lock()
_sessions: dict[str, dict] = {}   # session -> {viewer_id, token, cost_total, cost_last, turns}


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
            with open(RENDERER, "rb") as fh:
                body = fh.read()
        except Exception as exc:
            return self.send_error(500, str(exc))
        # 经 hub /view/<名> 路由来的(带 X-DW-Session 头):页面里的绝对 API 端点要挂到 /view/<会话>/ 下,
        # 否则浏览器请求 /events 会打到 hub 根路径(404)。这 4 个带引号字面量只在 renderer 的 API 对象出现。
        # 走 nginx 专属路径的(无此头,用 DEFAULT_SESSION)不改,靠 nginx sub_filter 改写。
        hdr_sess = self.headers.get("X-DW-Session")
        if hdr_sess:
            pre = "/view/" + hdr_sess.strip()
            text = body.decode("utf-8", "replace")
            for ep in ("/events", "/input", "/interrupt", "/status"):
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
        if path != "/input":
            return self.send_error(404)
        session = self._session()
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            text = json.loads(raw).get("text", "").strip()
        except Exception:
            text = ""
        ok = False
        if text:
            st = _ensure_ready(session)
            if st.get("viewer_id") and st.get("token"):
                # pilot 的 write_terminal_input 是按键式:遇回车才提交 → 整条消息末尾补 \r 触发投递。
                # 内嵌换行(Ctrl+Enter)先归一为空格,避免被拆成多条(多行完整投递属后续优化)。
                payload_text = text.replace("\r\n", "\n").replace("\n", " ") + "\r"
                code, _ = _hub_post(HUB_INTERNAL, f"/internal/view/{session}/input",
                                    {"viewer_id": st["viewer_id"], "token": st["token"], "text": payload_text})
                ok = (code == 200)
        body = json.dumps({"ok": ok}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    port = int(os.environ.get("VS_PORT", "19320"))
    srv = ThreadingHTTPServer(("127.0.0.1", port), _Handler)
    print(f"[view-service] :{port} hub_internal={HUB_INTERNAL} hub_public={HUB_PUBLIC} "
          f"session={DEFAULT_SESSION or '(from X-DW-Session)'} renderer={RENDERER}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
