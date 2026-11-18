#!/usr/bin/env python3
"""S-R61-N2 端到端工装：按 Hub 逐事件序号语义发 SSE，并供历史分页，验证回合外输出折叠条。

与 renderer-replay-seq-fixture.py 同语义（id「<纪元>-<seq>」、after、ring 回放），另加 /history 分页。
回合外事件形状＝Claude 快照：kind=assistant_text、turn_id 与 delivery_id 皆空、data={}。

MODE：
  w2          正常回合（user_input→回合输出→turn_completed）之后 N 条回合外输出，保持连接
  w1          先 N 条回合外输出，再 user_input 与回合输出（首段投递前窗口），保持连接
  boundary    4 条回合外 → BOUNDARY 指定边界事件 → 4 条回合外；boundary_delay 则不发边界事件、延迟 1.2 s 发后半段
  gatea       N 条无 turn/delivery 但 data 带 Codex 标记（item_id/thread_id）的 assistant_text
  gateb       6 条回合外 → 1 条带 Codex 标记的回合内输出 → 6 条回合外
  reconnect   带 id；首连发完即断；重连忽略 after 整份重放（同 id 去重），保持连接
  legacy      不带 id（旧 Hub）；首连发完即断；重连整份重放；LEGACY_RING=new 时重放换成单条新事件
  split       带 id；首连跳过 seq5（回合内卡），实时跳号触发补缺；补缺连接补上 seq5，保持连接
  history     实时流 seq30..39 回合外，0.4 s 后再发 40..41（落入 bootstrap 缓冲）；
              /history 首页延迟 1 s 返回 seq20..29 回合外（has_older），翻页返回 seq10..19（15 为回合内卡）
  evict       30 组「user_input 卡＋2 条回合外」共 60 节点，配合窄视口 20 上限触发逐出
"""
import json, os, sys, time, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

RENDERER = os.environ.get('RENDERER', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'renderer-canary-sse.html'))
MODE = os.environ.get('MODE', 'w2')
N = int(os.environ.get('N', '12'))
BOUNDARY = os.environ.get('BOUNDARY', 'turn')
LEGACY_RING = os.environ.get('LEGACY_RING', 'same')
READONLY = os.environ.get('READONLY', '1') == '1'
TS_BASE = 1763000000000

LOCK = threading.Lock()
STATE = {'epoch': 'a1b2c3d4', 'ring': [], 'connections': 0, 'afters': [], 'history_requests': []}


def ts(seq):
    return TS_BASE + seq * 1000


def offturn(seq):
    return {'kind': 'assistant_text', 'text': f'[{seq:03d}]', 'cursor': f'{seq:06d}',
            'turn_id': '', 'delivery_id': '', 'data': {}, 'ts': ts(seq)}


def turn_text(seq, turn='t-1', delivery='d-1'):
    return {'kind': 'assistant_text', 'text': f'<{seq:03d}>', 'cursor': f'{seq:06d}',
            'turn_id': turn, 'delivery_id': delivery, 'data': {}, 'ts': ts(seq)}


def user_input(seq, delivery='d-1', turn=''):
    return {'kind': 'user_input', 'text': f'问题{seq}', 'cursor': f'{seq:06d}',
            'delivery_id': delivery, 'turn_id': turn, 'stage': 'cli_injected', 'ts': ts(seq)}


def boundary_event(seq):
    if BOUNDARY == 'turn':
        return turn_text(seq, turn='t-b', delivery='d-b')
    if BOUNDARY == 'input':
        return user_input(seq, delivery='d-q')
    if BOUNDARY == 'mention':
        return {'kind': 'mention_reply', 'message_id': 'm-1', 'text': '来自别席的回复',
                'data': {'from_label': '别席'}, 'cursor': f'{seq:06d}', 'ts': ts(seq)}
    if BOUNDARY == 'error':
        return {'kind': 'error', 'text': '出错样例', 'data': {}, 'cursor': f'{seq:06d}', 'ts': ts(seq)}
    if BOUNDARY == 'hidden':
        return {'kind': 'tool_call', 'text': '工具调用', 'data': {}, 'cursor': f'{seq:06d}', 'ts': ts(seq)}
    if BOUNDARY == 'update':
        return {'kind': 'delivery_update', 'delivery_id': 'd-none', 'stage': 'turn_processed',
                'data': {'stage': 'turn_processed'}, 'cursor': f'{seq:06d}', 'ts': ts(seq)}
    raise ValueError(BOUNDARY)


def initial_ring():
    if MODE == 'w2':
        ring = [(1, user_input(1)), (2, turn_text(2)), (3, turn_text(3)),
                (4, {'kind': 'state_change', 'turn_id': 't-1', 'data': {'change': 'turn_completed'},
                     'cursor': '000004', 'ts': ts(4)})]
        ring += [(s, offturn(s)) for s in range(5, 5 + N)]
        return ring
    if MODE == 'w1':
        ring = [(s, offturn(s)) for s in range(1, N + 1)]
        ring += [(N + 1, user_input(N + 1)), (N + 2, turn_text(N + 2)), (N + 3, turn_text(N + 3))]
        return ring
    if MODE in ('boundary', 'boundary_delay'):
        ring = [(s, offturn(s)) for s in range(1, 5)]
        if MODE == 'boundary':
            ring.append((5, boundary_event(5)))
        ring += [(s, offturn(s)) for s in range(6, 10)]
        return ring
    if MODE == 'gatea':
        return [(s, {'kind': 'assistant_text', 'text': f'[{s:03d}]', 'cursor': f'{s:06d}',
                     'turn_id': '', 'delivery_id': '', 'data': {'item_id': 'i1', 'thread_id': 'th1'},
                     'ts': ts(s)}) for s in range(1, 7)]
    if MODE == 'gateb':
        ring = [(s, offturn(s)) for s in range(1, 7)]
        ring.append((7, {'kind': 'assistant_text', 'text': '<codex>', 'cursor': '000007',
                         'turn_id': 't-c', 'delivery_id': 'd-c',
                         'data': {'item_id': 'ic', 'thread_id': 'thc'}, 'ts': ts(7)}))
        ring += [(s, offturn(s)) for s in range(8, 14)]
        return ring
    if MODE in ('reconnect', 'legacy'):
        return [(s, offturn(s)) for s in range(1, N + 1)]
    if MODE == 'split':
        ring = [(1, user_input(1)),
                (2, {'kind': 'state_change', 'turn_id': 't-1', 'data': {'change': 'turn_completed'},
                     'cursor': '000002', 'ts': ts(2)}),
                (3, offturn(3)), (4, offturn(4)),
                (5, turn_text(5, turn='t-x', delivery='d-x'))]
        ring += [(s, offturn(s)) for s in range(6, 13)]
        return ring
    if MODE == 'history':
        return [(s, offturn(s)) for s in range(30, 40)]
    if MODE == 'evict':
        ring = []
        seq = 0
        for i in range(1, 31):
            seq += 1
            ring.append((seq, user_input(seq, delivery=f'd-{i}')))
            for _ in range(2):
                seq += 1
                ring.append((seq, offturn(seq)))
        return ring
    raise ValueError(MODE)


STATE['ring'] = initial_ring()


def history_page(before):
    def row(seq, ev):
        return {'cursor': {'generation': 'g1', 'seq': seq}, 'event_id': f'h-{seq}',
                'event': json.dumps(ev, ensure_ascii=False), 'received_at': ''}
    if before is None:
        events = [row(s, offturn(s)) for s in range(20, 30)]
        return {'generation': 'g1', 'high_water': 29, 'has_older': True, 'before_seq': 20,
                'gap_before': False, 'gaps': [], 'events': events}
    events = [row(s, offturn(s)) for s in range(10, 15)]
    events.append(row(15, turn_text(15, turn='t-9', delivery='d-9')))
    events += [row(s, offturn(s)) for s in range(16, 20)]
    return {'generation': 'g1', 'high_water': 29, 'has_older': False, 'before_seq': 10,
            'gap_before': False, 'gaps': [], 'events': events}


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

    def _frame(self, seq, ev, epoch, with_id=True):
        head = f'id: {epoch}-{seq}\n'.encode() if with_id else b''
        self.wfile.write(head + b'data: ' + json.dumps(ev, ensure_ascii=False).encode() + b'\n\n')
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
                'session': 'offturn-smoke', 'state': 'running', 'readonly': READONLY, 'can_write': not READONLY,
                'auth_mode': 'legacy', 'owner': 'zzc', 'model': 'm', 'cost_total': 0, 'cost_last': 0, 'turns': 1}).encode())
        if p.endswith('/tasks'):
            return self._send(200, 'application/json', b'{}')
        if p.endswith('/history'):
            if MODE != 'history':
                return self._send(404, 'text/plain', b'no history')
            before_raw = parse_qs(u.query).get('before', [''])[0]
            before = int(before_raw) if before_raw else None
            with LOCK:
                STATE['history_requests'].append(before_raw)
            if before is None:
                time.sleep(1.0)   # 让实时 seq40..41 落入 bootstrap 缓冲
            return self._send(200, 'application/json', json.dumps(history_page(before), ensure_ascii=False).encode())
        if p.endswith('/probe'):
            with LOCK:
                return self._send(200, 'application/json', json.dumps({
                    'event_connections': STATE['connections'], 'afters': STATE['afters'],
                    'epoch': STATE['epoch'], 'history_requests': STATE['history_requests']}).encode())
        if not p.endswith('/events'):
            return self._send(404, 'text/plain', b'nope')
        after = parse_qs(u.query).get('after', [''])[0]
        with LOCK:
            STATE['connections'] += 1
            n = STATE['connections']
            STATE['afters'].append(after)
            if n == 2 and MODE == 'legacy' and LEGACY_RING == 'new':
                STATE['ring'] = [(1, {'kind': 'assistant_text', 'text': '[NEW]', 'cursor': '999999',
                                      'turn_id': '', 'delivery_id': '', 'data': {}, 'ts': ts(999)})]
            epoch, ring = STATE['epoch'], list(STATE['ring'])
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Cache-Control', 'no-cache')
        self.send_header('Connection', 'keep-alive')
        self.end_headers()
        self.wfile.flush()
        try:
            with_id = MODE != 'legacy'
            if MODE == 'history' and n == 1:
                for s, e in ring:
                    self._frame(s, e, epoch)
                time.sleep(0.4)
                for s in (40, 41):
                    self._frame(s, offturn(s), epoch)
                time.sleep(30)
                return
            if MODE == 'boundary_delay' and n == 1:
                for s, e in ring[:4]:
                    self._frame(s, e, epoch)
                time.sleep(1.2)
                for s, e in ring[4:]:
                    self._frame(s, e, epoch)
                time.sleep(30)
                return
            if MODE == 'split' and n == 1:
                for s, e in ring:
                    if s == 5:
                        continue
                    self._frame(s, e, epoch)
                time.sleep(30)
                return
            replay, gap = select(epoch, ring, after)
            if gap:
                self.wfile.write(f'event: replay_gap\ndata: {{"reason":"{gap}","epoch":"{epoch}"}}\n\n'.encode())
                self.wfile.flush()
            for s, e in replay:
                self._frame(s, e, epoch, with_id)
            if MODE in ('reconnect', 'legacy') and n == 1:
                return self._close()
            time.sleep(30)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_POST(self):
        n = int(self.headers.get('Content-Length') or 0)
        self.rfile.read(n)
        self._send(200, 'application/json', b'{"ok":true}')


if __name__ == '__main__':
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 19402
    print('mode', MODE, 'n', N, flush=True)
    ThreadingHTTPServer(('127.0.0.1', port), H).serve_forever()
