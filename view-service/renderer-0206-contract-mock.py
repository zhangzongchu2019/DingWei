#!/usr/bin/env python3
"""0.2.0.6 A 线功能级验收用的**契约测试替身**（不是产品代码，不上生产）。

它只实现契约里 A 侧需要对接的那部分，用来在 B 线到位之前跑通「真的传一个 pdf、
一个视频」这类功能级验收：

  契约 tasks/v0.1-B 锚 d26bfd7002f70b7a9c9b5dcec6a1682ccbef974f
  runbooks/SPEC-0.2.0.6-file-direct-link.md §2 · 附录 A1 · A4 · A5

  · POST /api/console/agents/{session}/file-upload-ticket → {upload_url,ticket,session}
  · POST /files/upload                                     → §2 四字段 {url,name,size,media_type}
  · GET|HEAD /f/{owner}/{session}/YY/MMDD/{fileid}.{ext}   → 按 A5 出 Content-Type / Disposition

🔴 它**不代表** B 线的真实实现；B 线交付后必须用真后端重跑同一套验收。
"""
import http.server
import json
import os
import re
import secrets
import urllib.parse
import ssl
import sys
import threading
import time

ROOT = os.path.dirname(os.path.abspath(__file__))
STORE = {}   # url path -> (bytes, content_type, disposition)
SESSION = os.environ.get("MOCK_SESSION", "fixture-a-2642")
OWNER = "fixtureowner"

# 附录 A5：白名单内按 media_type 映射扩展名；白名单外一律 .bin ＋ attachment。
EXT_WHITELIST = {
    "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
    "video/mp4": ".mp4", "video/webm": ".webm",
    # B-11：音频。B 侧本批已落地（internal/fileservice/manager.go 的 filenameAudioTypes
    # ＋ nginx inline 块七行），替身不同步就会出现「测的是替身、不是契约」——
    # 那正是评审里立过的那条：素材/环境落在被测维度的同一侧，断言就无从分辨。
    "audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/flac": ".flac", "audio/aac": ".aac",
    "audio/opus": ".opus", "audio/ogg": ".oga",
    "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/vnd.wave": ".wav",
    "application/pdf": ".pdf",
    "text/plain": ".txt",
}


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code, body=b"", headers=None, head_only=False):
        self.send_response(code)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if not head_only and body:
            self.wfile.write(body)

    # ── 上传端 ────────────────────────────────────────────────
    def do_POST(self):
        if re.fullmatch(r"/api/console/agents/[^/]+/file-upload-ticket", self.path):
            length = int(self.headers.get("Content-Length") or 0)
            self.rfile.read(length)
            body = json.dumps({
                "upload_url": f"https://127.0.0.1:{self.server.server_port}/files/upload",
                "ticket": "mock-ticket", "session": SESSION,
            }).encode()
            return self._send(200, body, {"Content-Type": "application/json"})

        if self.path == "/files/upload":
            if self.headers.get("Authorization") != "Bearer mock-ticket":
                return self._send(401, json.dumps({"error": "票据无效"}).encode(),
                                  {"Content-Type": "application/json"})
            length = int(self.headers.get("Content-Length") or 0)
            data = self.rfile.read(length)
            media_type = (self.headers.get("Content-Type") or "application/octet-stream").split(";")[0].strip().lower()
            # 🔴 P0 协议（组长 2026-09-18 定死）：X-File-Name 为 `UTF-8''<percent-encoded>`，
            #    无该前缀则按裸值（旧客户端，向后兼容）。
            #    替身必须实现契约的这一半，否则 A 侧会对着编码后的名字写断言。
            #    ⚠️ 本替身只做解码；真正的 safeName 归 B 线，且必须**先解码再 safeName**。
            raw_name = self.headers.get("X-File-Name") or "upload.bin"
            if raw_name.startswith("UTF-8''"):
                name = urllib.parse.unquote(raw_name[len("UTF-8''"):], encoding="utf-8", errors="replace")
            else:
                name = raw_name
            # A4：50 MiB = 52428800
            if len(data) > 52428800:
                return self._send(413, json.dumps({"error": "超过 50 MiB 上限"}).encode(),
                                  {"Content-Type": "application/json"})
            ext = EXT_WHITELIST.get(media_type)
            if ext is None and media_type.startswith(("image/", "video/")):
                ext = ".png" if media_type.startswith("image/") else ".mp4"
            whitelisted = ext is not None
            if not whitelisted:
                ext = ".bin"
            fileid = secrets.token_hex(16)
            day = time.strftime("%y/%m%d")
            path = f"/f/{OWNER}/{SESSION}/{day}/{fileid}{ext}"
            STORE[path] = (
                data,
                media_type if whitelisted else "application/octet-stream",
                None if whitelisted else "attachment",
            )
            body = json.dumps({
                "url": f"https://127.0.0.1:{self.server.server_port}{path}",
                "name": name, "size": len(data), "media_type": media_type,
            }).encode()
            return self._send(201, body, {"Content-Type": "application/json"})

        return self._send(404, b'{"error":"not found"}', {"Content-Type": "application/json"})

    # ── 下载端（A5 的响应头口径）────────────────────────────────
    def _serve_file(self, head_only):
        entry = STORE.get(self.path.split("?")[0])
        if not entry:
            return self._send(404, b"not found", {"Content-Type": "text/plain"}, head_only)
        data, ctype, disp = entry
        headers = {
            "Content-Type": ctype,
            "X-Content-Type-Options": "nosniff",
            "Referrer-Policy": "no-referrer",
            "Accept-Ranges": "bytes",
        }
        if disp:
            headers["Content-Disposition"] = disp
        rng = self.headers.get("Range")
        m = re.fullmatch(r"bytes=(\d+)-(\d*)", rng or "")
        if m and not head_only:
            start = int(m.group(1))
            end = int(m.group(2)) if m.group(2) else len(data) - 1
            end = min(end, len(data) - 1)
            if start > end:
                return self._send(416, b"", headers)
            chunk = data[start:end + 1]
            headers["Content-Range"] = f"bytes {start}-{end}/{len(data)}"
            return self._send(206, chunk, headers)
        return self._send(200, data, headers, head_only)

    def do_GET(self):
        if self.path.startswith("/f/"):
            return self._serve_file(False)
        rel = self.path.split("?")[0].lstrip("/") or "renderer-canary-sse.html"
        target = os.path.join(ROOT, rel)
        if os.path.isfile(target) and os.path.commonpath([ROOT, os.path.abspath(target)]) == ROOT:
            with open(target, "rb") as fh:
                body = fh.read()
            ctype = "text/html; charset=utf-8" if rel.endswith(".html") else "application/octet-stream"
            return self._send(200, body, {"Content-Type": ctype})
        return self._send(404, b"not found", {"Content-Type": "text/plain"})

    def do_HEAD(self):
        if self.path.startswith("/f/"):
            return self._serve_file(True)
        return self._send(404, b"", {"Content-Type": "text/plain"}, True)


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 19443
    cert = os.environ["MOCK_CERT"]
    key = os.environ["MOCK_KEY"]
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert, key)
    httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
    print(f"mock listening https://127.0.0.1:{port}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
