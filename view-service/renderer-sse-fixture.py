#!/usr/bin/env python3
"""renderer 端到端 SSE 工装:真 EventSource + 真节奏地回放一段长回复。

存在的理由(2026-08-29 教训):用 page.evaluate 直接调 handleIncomingEvent 的合成回归,
会把整批 chunk 塞进同一个 JS task,只触发一次节流 flush —— 那是最好情况,不是现网。
现网是一条 SSE 一个 task、会断线重连、hub 会重放 ring。长文本卡死的真根因
(重放事件无去重导致正文无界累加)只有在这套真流工装下才暴露得出来。

用法:
  RENDERER=<件> [CHUNK=160] [DELAY=0.04] [TOOLS=0] [REPEAT=1] [RECONNECT=0] \
    python3 renderer-sse-fixture.py <port>
  RECONNECT=1 时发完即断开连接,用于复现「重连 → hub 重放 → 是否二次累加」。
"""
import json, os, sys, time, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

RENDERER = os.environ.get('RENDERER', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'renderer-canary-sse.html'))
TEXT = os.environ.get('TEXT_FILE') and open(os.environ['TEXT_FILE'], encoding='utf-8').read() \
    or ('契约正文压测段落,含表格与列表。\n\n| 列A | 列B |\n|---|---|\n| 1 | 值 |\n\n- 要点一\n- 要点二\n\n' * 120)
CHUNK = int(os.environ.get('CHUNK', '160'))     # 段落级 chunk,贴近 sample-events
DELAY = float(os.environ.get('DELAY', '0.04'))  # 每 chunk 间隔秒
TOOLS = int(os.environ.get('TOOLS', '0'))       # 回合内穿插的 tool_call 数
REPEAT = int(os.environ.get('REPEAT', '1'))     # 文本重复倍数(放大正文)
RING_TURNS = int(os.environ.get('RING_TURNS', '0'))  # 连接时先补放的历史回合数(模拟 hub ring 重放)
# SHARED_CURSOR=1:同一 item 的全部 delta 共用一个 cursor —— Codex 流式的真实形态。
# 2026-08-29 P0 就出在这里:按 cursor 去重会把首片之后的全部 delta 当成重放丢掉。
SHARED_CURSOR = os.environ.get('SHARED_CURSOR') == '1'

def chunks(t, n):
    return [t[i:i+n] for i in range(0, len(t), n)]

class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *a): pass
    def _send(self, code, ctype, body):
        self.send_response(code); self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(body))); self.end_headers()
        self.wfile.write(body)
    def do_GET(self):
        p = self.path.split('?')[0]
        if p in ('/', '/renderer'):
            return self._send(200, 'text/html; charset=utf-8', open(RENDERER,'rb').read())
        if p.endswith('/status'):
            return self._send(200, 'application/json', json.dumps({
                'session':'smoke-2642','state':'running','readonly':True,'can_write':False,
                'auth_mode':'legacy','owner':'zzc','model':'m','cost_total':0,'cost_last':0,'turns':1}).encode())
        if p.endswith('/tasks'):
            return self._send(200, 'application/json', b'{}')
        if p.endswith('/events'):
            self.send_response(200)
            self.send_header('Content-Type','text/event-stream')
            self.send_header('Cache-Control','no-cache')
            self.send_header('Connection','keep-alive')
            self.end_headers()
            cur = 0
            def emit(ev):
                nonlocal cur
                cur += 1
                ev.setdefault('cursor', f'{cur:06d}')
                self.wfile.write(b'data: ' + json.dumps(ev, ensure_ascii=False).encode() + b'\n\n')
                self.wfile.flush()
            try:
                emit({'kind':'state_change','data':{'change':'session_init','session_id':'s1','model':'m'},
                      'delivery_id':'','turn_id':'','ts':int(time.time()*1000)})
                # 刷新/重连时 hub 会把 ring 里的历史整批重放,这是「刷新后较长才出」的真实入口
                for h in range(RING_TURNS):
                    emit({'kind':'user_input','text':f'历史问题 {h}','delivery_id':f'h-{h}',
                          'turn_id':f'ht-{h}','stage':'cli_injected','ts':int(time.time()*1000)})
                    for j in range(3):
                        emit({'kind':'assistant_text',
                              'text':f'历史回答 {h}-{j}。\n\n| 列A | 列B |\n|---|---|\n| {h} | 值{j} |\n\n- 要点一\n- 要点二\n',
                              'delivery_id':f'h-{h}','turn_id':f'ht-{h}','ts':int(time.time()*1000)})
                    emit({'kind':'state_change','data':{'change':'turn_completed'},
                          'delivery_id':f'h-{h}','turn_id':f'ht-{h}','ts':int(time.time()*1000)})
                emit({'kind':'user_input','text':'请输出契约全文','delivery_id':'d-1','turn_id':'t-1',
                      'stage':'cli_injected','ts':int(time.time()*1000)})
                for i in range(TOOLS):
                    emit({'kind':'tool_call','data':{'name':'Read','input':{
                        'file_path': f'/specs/sp{i}/spec.md', 'offset': i*40, 'limit': 400,
                        'note': 'x'*300}},'delivery_id':'d-1','turn_id':'t-1','ts':int(time.time()*1000)})
                    time.sleep(DELAY)
                for c in chunks(TEXT*REPEAT, CHUNK):
                    ev = {'kind':'assistant_text','text':c,'delivery_id':'d-1','turn_id':'t-1',
                          'ts':int(time.time()*1000)}
                    if SHARED_CURSOR:
                        ev['cursor'] = 'turn:t-1:item:0003'   # 全部 delta 共用,不参与 cur 自增
                    emit(ev)
                    time.sleep(DELAY)
                emit({'kind':'state_change','data':{'change':'turn_completed','turn_cost_usd':0.5},
                      'delivery_id':'d-1','turn_id':'t-1','ts':int(time.time()*1000)})
                emit({'kind':'state_change','data':{'change':'__done__'},'delivery_id':'','turn_id':''})
                # RECONNECT=1:发完即断开,模拟现网断线重连后 hub 重放 ring 里的同一批事件。
                # 必须显式关掉底层连接并置 close_connection —— 光 return 在 HTTP/1.1 keep-alive 下
                # 浏览器可能不认为连接已断,EventSource 就不会重连,该用例会静默空转。
                if os.environ.get('RECONNECT') == '1':
                    self.close_connection = True
                    try:
                        self.wfile.flush()
                        self.connection.shutdown(1)
                    except Exception:
                        pass
                    try:
                        self.connection.close()
                    except Exception:
                        pass
                    return
                time.sleep(30)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        self._send(404,'text/plain',b'nope')
    def do_POST(self):
        n=int(self.headers.get('Content-Length') or 0); self.rfile.read(n)
        self._send(200,'application/json',b'{"ok":true}')

if __name__ == '__main__':
    port=int(sys.argv[1]) if len(sys.argv)>1 else 19400
    print('chunks:', len(chunks(TEXT, CHUNK)), 'chunk size', CHUNK, 'delay', DELAY, flush=True)
    ThreadingHTTPServer(('127.0.0.1',port), H).serve_forever()
