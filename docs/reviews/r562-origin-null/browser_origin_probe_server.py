import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def do_GET(self):
        pol=self.path.split('?pol=')[-1] if '?pol=' in self.path else 'no-referrer'
        self.send_response(200); self.send_header('Content-Type','text/html'); self.send_header('Referrer-Policy',pol); self.end_headers()
        self.wfile.write(b'<form method="post" action="/submit"><button id="b">go</button></form>')
    def do_POST(self):
        print('POST Origin=%r Referer=%r' % (self.headers.get('Origin'), self.headers.get('Referer')), flush=True)
        self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
http.server.HTTPServer(('127.0.0.1',int(sys.argv[1])),H).serve_forever()
