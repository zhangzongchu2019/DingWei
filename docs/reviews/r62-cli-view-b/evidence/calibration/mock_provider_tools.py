#!/usr/bin/env python3
# S-R62-CLI-VIEW-B 原型期工具行版式校准：mock responses provider（无真凭据）。
# 第一轮回 function_call（shell 类工具，真名取自请求 tools），第二轮回短答。
# Codex 会在本地真实执行该 command（仅 printf，无副作用），从而产出真实 item/completed。
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 0
def pick_tool(req):
    for tool in req.get("tools") or []:
        fn = tool.get("function") or tool
        props = ((fn.get("parameters") or {}).get("properties") or {})
        if "command" in props:
            return fn.get("name") or tool.get("name"), json.dumps({"command": ["bash", "-lc", "printf 'ok\\tmain\\t1.2s\\n'"], "timeout_ms": 10000})
        if "cmd" in props:
            return fn.get("name") or tool.get("name"), json.dumps({"cmd": "printf 'ok\\tmain\\t1.2s\\n'", "yield_time_ms": 2000})
    return None, None

def has_tool_output(req):
    for item in req.get("input") or []:
        if isinstance(item, dict) and item.get("type") == "function_call_output":
            return True
    return False

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
        with open("/tmp/cli-view-b-evidence/provider-requests.jsonl", "a") as f:
            f.write(json.dumps({"path": self.path, "tools": [t.get("name") or (t.get("function") or {}).get("name") for t in req.get("tools") or []], "has_tool_output": has_tool_output(req)}, ensure_ascii=False) + "\n")
        with open("/tmp/cli-view-b-evidence/provider-full.jsonl", "a") as f:
            f.write(json.dumps({"path": self.path, "tools": req.get("tools"), "input_types": [i.get("type") for i in req.get("input") or [] if isinstance(i, dict)]}, ensure_ascii=False) + "\n")
        if not self.path.endswith("/responses"):
            self._send(200, "{}")
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        def ev(obj):
            self.wfile.write(("data: " + json.dumps(obj, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
        tool, args = pick_tool(req)
        if tool and not has_tool_output(req):
            fc = {"id": "fc_mock", "type": "function_call", "status": "in_progress", "name": tool, "arguments": "", "call_id": "call_mock_1"}
            ev({"type": "response.created", "response": {"id": "resp_mock", "object": "response", "status": "in_progress", "model": "mock-model", "output": []}})
            ev({"type": "response.output_item.added", "output_index": 0, "item": fc})
            ev({"type": "response.function_call_arguments.delta", "item_id": "fc_mock", "output_index": 0, "delta": args})
            ev({"type": "response.function_call_arguments.done", "item_id": "fc_mock", "output_index": 0, "arguments": args})
            done_fc = {**fc, "status": "completed", "arguments": args}
            ev({"type": "response.output_item.done", "output_index": 0, "item": done_fc})
            ev({"type": "response.completed", "response": {"id": "resp_mock", "object": "response", "status": "completed", "model": "mock-model", "output": [done_fc], "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})
            return
        msg = {"id": "msg_mock", "type": "message", "role": "assistant", "status": "in_progress", "content": []}
        ev({"type": "response.created", "response": {"id": "resp_mock2", "object": "response", "status": "in_progress", "model": "mock-model", "output": []}})
        ev({"type": "response.output_item.added", "output_index": 0, "item": msg})
        ev({"type": "response.content_part.added", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "", "annotations": []}})
        ev({"type": "response.output_text.delta", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "delta": "工具已跑完。"})
        ev({"type": "response.output_text.done", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "text": "工具已跑完。"})
        ev({"type": "response.content_part.done", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "工具已跑完。", "annotations": []}})
        done_msg = {"id": "msg_mock", "type": "message", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "工具已跑完。", "annotations": []}]}
        ev({"type": "response.output_item.done", "output_index": 0, "item": done_msg})
        ev({"type": "response.completed", "response": {"id": "resp_mock2", "object": "response", "status": "completed", "model": "mock-model", "output": [done_msg], "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})

if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
    print(srv.server_address[1], flush=True)
    srv.serve_forever()
