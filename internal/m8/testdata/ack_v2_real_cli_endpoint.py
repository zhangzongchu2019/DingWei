"""Loopback controlled model endpoint for actual Claude/Codex executables.

No fake CLI, admission, terminal event or receipt. Only model HTTP/SSE output is
controlled; the real CLI must produce its own native admission/terminal wire.
"""
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import re
import threading
import time


class ControlledModel:
    def __init__(self, root):
        self.root = root
        self.lock = threading.RLock()
        self.gates = {}
        path = root / 'model-observations.json'
        self.data = json.loads(path.read_text()) if path.exists() else {'requests': {}, 'admissions': {}, 'terminals': {}}
        model = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                self.wfile.write(b'{"object":"list","data":[]}')
            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers.get('Content-Length', 0))))
                if self.path.split('?')[0].endswith('/count_tokens'):
                    self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
                    self.wfile.write(b'{"input_tokens":10}'); return
                native = 'claude' if self.path.split('?')[0].endswith('/messages') else 'codex' if self.path.split('?')[0].endswith('/responses') else ''
                if not native:
                    self.send_response(404); self.end_headers(); return
                label = model.label(data)
                with model.lock:
                    rows = model.data['requests'].setdefault(label, [])
                    rows.append({'at': time.time(), 'native': native})
                    index = len(rows)
                    gate = model.gates.setdefault(label, threading.Event())
                    model.save()
                self.send_response(200); self.send_header('Content-Type','text/event-stream'); self.send_header('Connection','close'); self.end_headers()
                try:
                    if native == 'claude': self.claude(data, label, index, gate)
                    else: self.codex(data, label, index, gate)
                except (BrokenPipeError, ConnectionResetError):
                    pass  # An intentionally terminated isolated CLI owns this connection.
            def event(self, kind, data):
                self.wfile.write(('event: '+kind+'\ndata: '+json.dumps(dict(type=kind, **data))+'\n\n').encode()); self.wfile.flush()
            def claude(self, data, label, index, gate):
                self.event('message_start', {'message': {'id':'msg_'+label+str(index),'type':'message','role':'assistant','model':data.get('model'),'content':[],'stop_reason':None,'stop_sequence':None,'usage':{'input_tokens':10,'output_tokens':0}}})
                self.event('content_block_start', {'index':0,'content_block':{'type':'text','text':''}})
                if not gate.wait(150): return
                self.event('content_block_delta', {'index':0,'delta':{'type':'text_delta','text':'controlled model result'}})
                self.event('content_block_stop', {'index':0})
                self.event('message_delta', {'delta':{'stop_reason':'end_turn','stop_sequence':None},'usage':{'output_tokens':4}})
                self.event('message_stop', {})
            def codex(self, data, label, index, gate):
                response={'id':'resp_'+label+str(index),'object':'response','created_at':int(time.time()),'status':'in_progress','model':data.get('model'),'output':[]}
                item={'id':'msg_'+label+str(index),'type':'message','role':'assistant','status':'in_progress','content':[]}
                content={'type':'output_text','text':'','annotations':[]}
                self.event('response.created', {'sequence_number':0,'response':dict(response)})
                self.event('response.in_progress', {'sequence_number':1,'response':dict(response)})
                self.event('response.output_item.added', {'sequence_number':2,'output_index':0,'item':dict(item)})
                self.event('response.content_part.added', {'sequence_number':3,'item_id':item['id'],'output_index':0,'content_index':0,'part':dict(content)})
                if not gate.wait(150): return
                text='controlled model result'
                self.event('response.output_text.delta', {'sequence_number':4,'item_id':item['id'],'output_index':0,'content_index':0,'delta':text})
                self.event('response.output_text.done', {'sequence_number':5,'item_id':item['id'],'output_index':0,'content_index':0,'text':text})
                content['text']=text;item['status']='completed';item['content']=[content]
                response['status']='completed';response['output']=[item];response['usage']={'input_tokens':10,'output_tokens':4,'total_tokens':14}
                self.event('response.content_part.done', {'sequence_number':6,'item_id':item['id'],'output_index':0,'content_index':0,'part':content})
                self.event('response.output_item.done', {'sequence_number':7,'output_index':0,'item':item})
                self.event('response.completed', {'sequence_number':8,'response':response})
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.daemon_threads = True
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    @staticmethod
    def label(value):
        labels = re.findall(r'R40_ACK_[A-Z0-9_]+', json.dumps(value, ensure_ascii=False))
        return labels[-1] if labels else 'UNLABELLED'

    def save(self):
        with self.lock:
            temporary = self.root / 'model-observations.tmp'
            temporary.write_text(json.dumps(self.data))
            temporary.replace(self.root / 'model-observations.json')

    def observe_admission(self, delivery_id, turn_id):
        with self.lock:
            self.data['admissions'].setdefault(delivery_id, {'turn_id':turn_id,'at':time.time()})
            self.save()

    def observe_terminal(self, delivery_id, turn_id):
        with self.lock:
            self.data['terminals'].setdefault(delivery_id, {'turn_id':turn_id,'at':time.time()})
            self.save()

    def release(self, label):
        with self.lock:
            self.gates.setdefault(label, threading.Event()).set()

    def close(self):
        with self.lock:
            for gate in self.gates.values():gate.set()
        self.server.shutdown()
