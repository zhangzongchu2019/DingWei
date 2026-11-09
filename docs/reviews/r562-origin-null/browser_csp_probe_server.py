import http.server, sys, threading
P1,P2=18941,18942
class A(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def do_GET(self):
        fa = "'self'" if 'add' not in self.path else "'self' http://127.0.0.1:%d"%P2
        self.send_response(200); self.send_header('Content-Type','text/html'); self.send_header('Referrer-Policy','same-origin')
        self.send_header('Content-Security-Policy',"default-src 'none'; form-action %s"%fa); self.end_headers()
        self.wfile.write(b'<form method="post" action="/submit"><button id="b">go</button></form>')
    def do_POST(self):
        self.send_response(303); self.send_header('Location','http://127.0.0.1:%d/landed'%P2); self.end_headers()
class B(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def do_GET(self):
        print('LANDED on second origin', self.path, flush=True); self.send_response(200); self.end_headers(); self.wfile.write(b'landed')
for port,h in ((P1,A),(P2,B)):
    s=http.server.HTTPServer(('127.0.0.1',port),h); threading.Thread(target=s.serve_forever,daemon=True).start()
import time; time.sleep(40)
