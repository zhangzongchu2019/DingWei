"""Sidecar —— 给"新版事件流页"供数据的独立 HTTP 服务(不碰生产 hub)。

三个口(契约,developer 前端只认这三个):
  GET  /               新渲染器 HTML(每次读盘,developer 改了即时生效)
  GET  /events         SSE:逐条 SessionEvent(json),来一条推一条(driver.events() 扇出的独立消费者)
  POST /input {text}   人类输入 → 一条新投递(driver.deliver)
  POST /interrupt      打断当前轮(driver.interrupt)
  GET  /status         状态栏数据:{session, state, cost_total, cost_last, mem, disk, datetime}

设计:嵌在 pilot 进程内,和 DriverAdapter 共享同一个 driver 实例。
事件流用 driver.events() 的多消费者扇出——SSE 每个连接是一个独立消费者,
和喂老 view 的 _event_bridge 互不抢事件(扇出已实测)。
"""
from __future__ import annotations

import json
import os
import threading
import time
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

_RENDERER = os.environ.get(
    "SH_SIDECAR_RENDERER",
    str(Path(__file__).resolve().parents[3] / "view-service" / "renderer.html"),
)


def _read_mem() -> dict:
    try:
        info = {}
        with open("/proc/meminfo") as fh:
            for line in fh:
                k, _, v = line.partition(":")
                info[k.strip()] = int(v.strip().split()[0])  # kB
        total = info.get("MemTotal", 0)
        avail = info.get("MemAvailable", 0)
        used = total - avail
        return {"used_mb": used // 1024, "total_mb": total // 1024,
                "pct": round(used * 100 / total, 1) if total else 0}
    except Exception:
        return {}


def _read_disk(path: str | None = None) -> dict:
    try:
        path = path or str(Path.home())
        st = os.statvfs(path)
        total = st.f_blocks * st.f_frsize
        free = st.f_bavail * st.f_frsize
        used = total - free
        return {"used_gb": round(used / 1e9, 1), "total_gb": round(total / 1e9, 1),
                "pct": round(used * 100 / total, 1) if total else 0}
    except Exception:
        return {}


class _Handler(BaseHTTPRequestHandler):
    adapter = None  # 由 start_sidecar 注入

    def log_message(self, *a):  # 静音默认访问日志
        pass

    # ---------------- GET ----------------
    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path in ("/", "/index.html"):
            return self._serve_renderer()
        if path == "/events":
            return self._serve_events()
        if path == "/status":
            return self._serve_status()
        self.send_error(404)

    def _serve_renderer(self):
        try:
            with open(_RENDERER, "rb") as fh:
                body = fh.read()
        except Exception as exc:
            self.send_error(500, str(exc))
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _serve_events(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        drv = self.adapter.driver
        try:
            # 独立消费者:从当前位置起实时流(扇出,不抢 _event_bridge 的事件)
            for ev in drv.events():
                if ev is None:
                    continue
                payload = {
                    "kind": ev.kind, "text": ev.text, "data": ev.data,
                    "session_id": ev.session_id, "cursor": ev.cursor,
                    "delivery_id": ev.delivery_id, "turn_id": ev.turn_id,
                }
                chunk = f"data: {json.dumps(payload, ensure_ascii=False)}\n\n"
                self.wfile.write(chunk.encode("utf-8"))
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return
        except Exception:
            return

    def _serve_status(self):
        a = self.adapter
        body = json.dumps({
            "session": a.session_label(),
            "state": "busy" if a._active else "idle",
            "driver": a._which,
            "cost_total": round(a._cost_total, 4),
            "cost_last": round(a._cost_last_turn, 4),
            "turns": a._turns,
            "mem": _read_mem(),
            "disk": _read_disk(),
            "datetime": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
        }, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    # ---------------- POST ----------------
    def do_POST(self):
        path = self.path.split("?", 1)[0]
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        if path == "/input":
            try:
                text = json.loads(raw or b"{}").get("text", "").strip()
            except Exception:
                text = ""
            if text:
                self.adapter.deliver_human_text(text)
            return self._json({"ok": bool(text)})
        if path == "/interrupt":
            current = self.adapter._commands.current
            if current is None or not current.turn_id:
                return self._json({"ok": False, "state": "idle"})
            try:
                result = self.adapter.interrupt_current(current.delivery_id, current.turn_id)
            except Exception:
                result = {"ok": False, "state": "failed"}
            return self._json(result)
        self.send_error(404)

    def _json(self, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def start_sidecar(adapter, port: int) -> None:
    _Handler.adapter = adapter
    # Sidecar 只监听回环地址；对外暴露策略由运行环境决定。
    srv = ThreadingHTTPServer(("127.0.0.1", port), _Handler)
    t = threading.Thread(target=srv.serve_forever, daemon=True, name="sidecar")
    t.start()
    print(f"[sidecar] serving new event-stream page on :{port} "
          f"(renderer={_RENDERER})", flush=True)
