#!/usr/bin/env python3
"""S-R62-CLI-VIEW-B 设计期版式校准：本地 mock OpenAI 兼容 provider（无真凭据）。
只回固定短答「收到。」的 chat completions 流；不触在线、不用真密钥。"""
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 0

class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass
    def _send(self, code, body, ctype="application/json"):
        b = body.encode() if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)
    def do_GET(self):
        if self.path.endswith("/models"):
            self._send(200, json.dumps({"object": "list", "data": [{"id": "mock-model", "object": "model", "owned_by": "mock"}]}))
        else:
            self._send(200, "{}")
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        req = json.loads(self.rfile.read(n) or b"{}")
        if self.path.endswith("/responses"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.end_headers()
            def ev(obj):
                self.wfile.write(("data: " + json.dumps(obj, ensure_ascii=False) + "\n\n").encode())
                self.wfile.flush()
            msg = {"id": "msg_mock", "type": "message", "role": "assistant", "status": "in_progress", "content": []}
            ev({"type": "response.created", "response": {"id": "resp_mock", "object": "response", "status": "in_progress", "model": "mock-model", "output": []}})
            ev({"type": "response.output_item.added", "output_index": 0, "item": msg})
            ev({"type": "response.content_part.added", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "", "annotations": []}})
            time.sleep(0.2)
            ev({"type": "response.output_text.delta", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "delta": "收到"})
            time.sleep(0.2)
            ev({"type": "response.output_text.delta", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "delta": "。"})
            time.sleep(0.2)
            ev({"type": "response.output_text.done", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "text": "收到。"})
            ev({"type": "response.content_part.done", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "收到。", "annotations": []}})
            done_msg = {"id": "msg_mock", "type": "message", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "收到。", "annotations": []}]}
            ev({"type": "response.output_item.done", "output_index": 0, "item": done_msg})
            ev({"type": "response.completed", "response": {"id": "resp_mock", "object": "response", "status": "completed", "model": "mock-model", "output": [done_msg], "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})
            return
        if not self.path.endswith("/chat/completions"):
            self._send(200, "{}")
            return
        if not req.get("stream"):
            self._send(200, json.dumps({"id": "chatcmpl-mock", "object": "chat.completion", "created": int(time.time()),
                "model": "mock-model", "choices": [{"index": 0, "message": {"role": "assistant", "content": "收到。"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}))
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        def chunk(delta, finish=None, usage=None):
            c = {"index": 0, "delta": delta, "finish_reason": finish}
            obj = {"id": "chatcmpl-mock", "object": "chat.completion.chunk", "created": int(time.time()), "model": "mock-model", "choices": [c]}
            if usage:
                obj["usage"] = usage
            self.wfile.write(("data: " + json.dumps(obj, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
        chunk({"role": "assistant", "content": ""})
        time.sleep(0.2)
        chunk({"content": "收到"})
        time.sleep(0.2)
        chunk({"content": "。"})
        time.sleep(0.2)
        chunk({}, finish="stop", usage={"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15})
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
