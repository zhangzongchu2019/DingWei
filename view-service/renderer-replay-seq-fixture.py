#!/usr/bin/env python3
"""R-V2-0105 件一 端到端工装：按 Hub 的逐事件序号语义发 SSE（id「<纪元>-<seq>」、after、replay_gap）。

与 internal/m8/terminal.go selectSSEReplay 同语义：不带 after ⇒ 整份；同纪元且 after 在 ring 覆盖内 ⇒ 只补其后；
纪元不同 ⇒ 先发 replay_gap(epoch_changed) 再整份；after 早于 ring 最旧 ⇒ replay_gap(overflow)＋整份。

MODE：
  reconnect   首连发完即断；之后每次连接先静默 FIRST_DELAY 秒再按 after 回放并断开，共 RECONNECTS 次，之后保持
  no_after    同 reconnect，但忽略 after（模拟旧 view-service 丢弃 after：整份重放）
  epoch       首连发完即断；第二次起换纪元、ring 只剩一条新纪元事件（模拟 Hub 重启／会话终端重建）
  overflow    首连发 1..N 即断；第二次起 ring 被截为 N+3..N+5（N+1、N+2 丢失）
  live_gap    首连发 1..N 后在实时流里跳过 N+1 直接发 N+2 并保持；带 after 的补缺连接补上 N+1、N+2
  gap_storm   首连发 1..N 后每 0.3 s 发一条跳号的实时事件（缺口永远补不上）并保持；记录连接次数
  ticket      SSE 同默认；可写会话（READONLY=0）。第一次申请上传凭证按 TICKET 出毛病，之后正常发票、/upload 收字节：
              hang 不回响应头 · body_hang 回了头、正文只写一半不结束 · http503 立即 503
"""
import json, os, sys, time, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

RENDERER = os.environ.get('RENDERER', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'renderer-canary-sse.html'))
MODE = os.environ.get('MODE', 'reconnect')
N = int(os.environ.get('N', '30'))
FIRST_DELAY = float(os.environ.get('FIRST_DELAY', '1.5'))
RECONNECTS = int(os.environ.get('RECONNECTS', '10'))
READONLY = os.environ.get('READONLY', '1') == '1'
TICKET = os.environ.get('TICKET', '')

LOCK = threading.Lock()
STATE = {'epoch': 'a1b2c3d4', 'ring': [], 'connections': 0, 'afters': [], 'tickets': 0, 'uploads': 0}


def text_event(seq, turn='t-1', delivery='d-1'):
    # 每片 cursor 不同（claude／opencode 形态）；文本带序号便于逐字核对
    return {'kind': 'assistant_text', 'text': f'[{seq:03d}]', 'cursor': f'{seq:06d}',
            'delivery_id': delivery, 'turn_id': turn, 'ts': 0}


def initial_ring():
    ring = [(1, {'kind': 'user_input', 'text': '问题', 'cursor': '000000', 'delivery_id': 'd-1', 'turn_id': 't-1',
                 'stage': 'cli_injected', 'ts': 0})]
    for s in range(2, N + 1):
        ring.append((s, text_event(s)))
    return ring


STATE['ring'] = initial_ring()


def expected_text(seqs):
    return ''.join(f'[{s:03d}]' for s in seqs)


def select(epoch, ring, after):
    if not after:
        return ring, ''
    try:
        ae, aseq = after.split('-')
        aseq = int(aseq)
    except ValueError:
        return ring, ''
    if ae != epoch:
        return ring, 'epoch_changed'
    oldest = ring[0][0] if ring else 0
    if oldest and aseq + 1 < oldest:
        return ring, 'overflow'
    return [(s, e) for s, e in ring if s > aseq], ''


class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *a):
        pass

    def _send(self, code, ctype, body):
        self.send_response(code)
        self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _frame(self, seq, ev, epoch):
        self.wfile.write(f'id: {epoch}-{seq}\n'.encode() + b'data: ' + json.dumps(ev, ensure_ascii=False).encode() + b'\n\n')
        self.wfile.flush()

    def _close(self):
        self.close_connection = True
        try:
            self.wfile.flush()
            self.connection.shutdown(1)
        except Exception:
            pass

    def do_GET(self):
        u = urlparse(self.path)
        p = u.path
        if p in ('/', '/renderer'):
            return self._send(200, 'text/html; charset=utf-8', open(RENDERER, 'rb').read())
        if p.endswith('/status'):
            return self._send(200, 'application/json', json.dumps({
                'session': 'smoke-2642', 'state': 'running', 'readonly': READONLY, 'can_write': not READONLY,
                'auth_mode': 'legacy', 'owner': 'zzc', 'model': 'm', 'cost_total': 0, 'cost_last': 0, 'turns': 1}).encode())
        if p.endswith('/tasks'):
            return self._send(200, 'application/json', b'{}')
        if p.endswith('/probe'):
            with LOCK:
                return self._send(200, 'application/json', json.dumps({
                    'event_connections': STATE['connections'], 'afters': STATE['afters'], 'epoch': STATE['epoch'],
                    'tickets': STATE['tickets'], 'uploads': STATE['uploads']}).encode())
        if not p.endswith('/events'):
            return self._send(404, 'text/plain', b'nope')
        after = parse_qs(u.query).get('after', [''])[0]
        with LOCK:
            STATE['connections'] += 1
            n = STATE['connections']
            STATE['afters'].append(after)
            if n == 2 and MODE == 'epoch':
                STATE['epoch'] = 'e5f6a7b8'
                STATE['ring'] = [(1, {'kind': 'assistant_text', 'text': '[NEW]', 'cursor': '999999',
                                      'delivery_id': 'd-1', 'turn_id': 't-1', 'ts': 0})]
            if n == 2 and MODE == 'overflow':
                STATE['ring'] = [(s, text_event(s)) for s in range(N + 3, N + 6)]
            if MODE == 'live_gap' and n == 1:
                STATE['ring'] = initial_ring() + [(N + 1, text_event(N + 1)), (N + 2, text_event(N + 2))]
            epoch, ring = STATE['epoch'], list(STATE['ring'])
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Cache-Control', 'no-cache')
        self.send_header('Connection', 'keep-alive')
        self.end_headers()
        self.wfile.flush()
        try:
            if n > 1 and MODE in ('reconnect', 'no_after'):
                time.sleep(FIRST_DELAY)   # 首条重放慢于 1 s：旧页面靠的清空窗口此时已过期
            effective_after = '' if MODE == 'no_after' else after
            if MODE == 'live_gap' and n == 1:
                for s, e in ring[:-2]:
                    self._frame(s, e, epoch)
                time.sleep(0.3)
                self._frame(N + 2, ring[-1][1], epoch)   # 跳过 N+1
                time.sleep(30)
                return
            if MODE == 'gap_storm':
                replay, gap = select(epoch, ring, effective_after)
                for s, e in replay:
                    self._frame(s, e, epoch)
                seq = max([s for s, _ in ring] or [0])
                for _ in range(200):
                    time.sleep(0.3)
                    seq += 2   # 每次跳一号，缺口永远补不上
                    with LOCK:
                        STATE['ring'].append((seq, text_event(seq)))
                    self._frame(seq, text_event(seq), epoch)
                return
            replay, gap = select(epoch, ring, effective_after)
            if gap:
                self.wfile.write(f'event: replay_gap\ndata: {{"reason":"{gap}","epoch":"{epoch}"}}\n\n'.encode())
                self.wfile.flush()
            for s, e in replay:
                self._frame(s, e, epoch)
            if MODE in ('reconnect', 'no_after') and n <= RECONNECTS:
                return self._close()
            if MODE in ('epoch', 'overflow') and n == 1:
                return self._close()
            time.sleep(30)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_POST(self):
        n = int(self.headers.get('Content-Length') or 0)
        body = self.rfile.read(n)
        p = urlparse(self.path).path
        if p.endswith('/file-upload-ticket'):
            with LOCK:
                STATE['tickets'] += 1
                k = STATE['tickets']
            if k == 1 and TICKET == 'hang':
                time.sleep(120)
                return
            if k == 1 and TICKET == 'body_hang':
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', '200')
                self.end_headers()
                self.wfile.write(b'{"upload_url": "')
                self.wfile.flush()
                time.sleep(120)
                return
            if k == 1 and TICKET == 'http503':
                return self._send(503, 'text/plain', b'busy')
            host = self.headers.get('Host')
            return self._send(200, 'application/json', json.dumps({
                'upload_url': f'http://{host}/upload', 'ticket': f'tk-{k}', 'session': 'smoke-2642'}).encode())
        if p == '/upload':
            with LOCK:
                STATE['uploads'] += 1
            return self._send(200, 'application/json', json.dumps({
                'url': 'https://files.example.invalid/f/smoke/a.txt', 'name': 'a.txt', 'size': len(body),
                'media_type': 'text/plain'}).encode())
        self._send(200, 'application/json', b'{"ok":true}')


if __name__ == '__main__':
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 19401
    print('mode', MODE, 'n', N, flush=True)
    ThreadingHTTPServer(('127.0.0.1', port), H).serve_forever()
