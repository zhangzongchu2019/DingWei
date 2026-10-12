"""Isolated real HTTP E1 tests; signed subject verification is stubbed.
These tests do not claim full Hub/VS authentication coverage.
"""
import http.client, json, os, sqlite3, tempfile, threading, unittest
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from unittest.mock import patch
import view_service as vs

class Handler(vs._Handler):
    def _mux_subject(self): return {'agent_id':'stable','full_name':'a.owner'}
    def log_message(self,*args): pass

class FailureHTTPTest(unittest.TestCase):
    def start(self, handler):
        server=ThreadingHTTPServer(('127.0.0.1',0),handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        self.addCleanup(thread.join);self.addCleanup(server.server_close);self.addCleanup(server.shutdown)
        return server
    def request(self,server,path):
        conn=http.client.HTTPConnection(*server.server_address,timeout=2)
        conn.request('GET',path,headers={'X-DW-Session':'a.owner'});resp=conn.getresponse()
        result=resp.status,resp.read();conn.close();return result
    def test_status_storage_failure_is_503_not_zero_success(self):
        server=self.start(Handler)
        with patch.object(vs,'VIEW_UNLOCK_V2',True),patch.object(vs,'_cost_projection',side_effect=sqlite3.OperationalError('synthetic storage unavailable')):
            code,body=self.request(server,'/status?session=a.owner')
        self.assertEqual(code,503);self.assertNotIn(b'"cost_total": 0',body)
        self.assertNotIn(b'synthetic storage unavailable',body)
    def test_stream_failed_projection_drops_frame_then_replays(self):
        payload=json.dumps({'kind':'state_change','data':{'change':'turn_completed','cost_usd':3.5}}).encode()
        frame=b'id: deadbeef-7\ndata: '+payload+b'\n\n'
        class Upstream(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200);self.send_header('Content-Length',str(len(frame)));self.end_headers();self.wfile.write(frame)
            def log_message(self,*args): pass
        upstream=self.start(Upstream);server=self.start(Handler)
        with tempfile.TemporaryDirectory() as root:
            projection=vs.CostProjection(os.path.join(root,'cost.db'))
            with sqlite3.connect(projection.path) as db:
                db.execute("CREATE TRIGGER fault BEFORE INSERT ON mux_cost BEGIN SELECT RAISE(ABORT,'controlled failure'); END")
            with patch.object(vs,'HUB_INTERNAL','http://%s:%s'%upstream.server_address),patch.object(vs,'_ensure_ready'),patch.object(vs,'_cost_projection',return_value=projection):
                self.assertEqual(self.request(server,'/events?session=a.owner'),(200,b''))
                with sqlite3.connect(projection.path) as db:
                    self.assertEqual(db.execute('SELECT count(*) FROM mux_completed').fetchone()[0],0)
                    self.assertEqual(db.execute('SELECT count(*) FROM mux_cost').fetchone()[0],0)
                    db.execute('DROP TRIGGER fault')
                self.assertEqual(self.request(server,'/events?session=a.owner'),(200,frame))
                self.assertEqual(projection.status('stable'),dict(cost_total=3.5,cost_last=3.5,turns=1))
                self.assertFalse(projection.apply('stable','a.owner','deadbeef-7',payload))

    def test_aggregate_storage_failure_closes_before_forward(self):
        packet={'type':'event','agent_id':'stable','full_name':'a.owner','epoch':'deadbeef','seq':'7',
                'payload':{'kind':'state_change','data':{'change':'turn_completed','cost_usd':3.5}}}
        frame=b'data: '+json.dumps(packet).encode()+b'\n\n'
        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                self.rfile.read(int(self.headers.get('Content-Length','0')))
                self.send_response(200);self.send_header('Content-Length',str(len(frame)));self.end_headers();self.wfile.write(frame)
            def log_message(self,*args): pass
        upstream=self.start(Upstream);server=self.start(Handler)
        with tempfile.TemporaryDirectory() as root:
            key=os.path.join(root,'reverse-key')
            with open(key,'wb') as f:f.write(os.urandom(32).hex().encode())
            projection=vs.CostProjection(os.path.join(root,'cost.db'))
            with sqlite3.connect(projection.path) as db:
                db.execute("CREATE TRIGGER fault BEFORE INSERT ON mux_cost BEGIN SELECT RAISE(ABORT,'controlled failure'); END")
            body=json.dumps({'feed_id':'a'*32,'bridge_id':'b'*32,'target':'test-target'})
            with patch.object(vs,'HUB_INTERNAL','http://%s:%s'%upstream.server_address),patch.object(vs,'VIEW_TARGET_NAME','test-target'),patch.object(vs,'VS_TO_HUB_SECRET_FILE',key),patch.object(vs,'_get_page_store'),patch.object(vs,'_verify_control_request',return_value=(True,200)),patch.object(vs,'_cost_projection',return_value=projection):
                conn=http.client.HTTPConnection(*server.server_address,timeout=2)
                conn.request('POST','/internal/console-mux/events',body=body,headers={'Content-Type':'application/json'})
                response=conn.getresponse();self.assertEqual(response.status,200)
                self.assertEqual(response.read(),b'');conn.close()
                with sqlite3.connect(projection.path) as db:
                    self.assertEqual(db.execute('SELECT count(*) FROM mux_completed').fetchone()[0],0)
                    db.execute('DROP TRIGGER fault')
                conn=http.client.HTTPConnection(*server.server_address,timeout=2)
                conn.request('POST','/internal/console-mux/events',body=body,headers={'Content-Type':'application/json'})
                response=conn.getresponse();self.assertEqual(response.read(),frame);conn.close()
                self.assertEqual(projection.status('stable')['turns'],1)
