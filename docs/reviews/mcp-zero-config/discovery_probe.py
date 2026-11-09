"""Measure pinned native OAuth discovery; no credentials, inference or live servers."""
import argparse, hashlib, http.server, json, os, re, subprocess, tempfile, threading, time, urllib.parse
from pathlib import Path

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--binary', default='/home/dev/.local/bin/codex')
    ap.add_argument('--output', type=Path, required=True)
    a = ap.parse_args()
    results = []
    for registration in (False, True):
        with tempfile.TemporaryDirectory(prefix='zc-discovery-') as temp:
            root = Path(temp)
            events = []
            class Handler(http.server.BaseHTTPRequestHandler):
                def log_message(self, *args): pass
                def respond(self, status, body, headers=None):
                    self.send_response(status)
                    for k,v in (headers or {}).items(): self.send_header(k,v)
                    self.send_header('Content-Type','application/json')
                    self.end_headers()
                    self.wfile.write(json.dumps(body).encode())
                def do_GET(self):
                    events.append({'method':'GET','path':self.path})
                    if 'oauth-protected-resource' in self.path:
                        self.respond(200, {'resource':origin+'/mcp','authorization_servers':[origin]})
                    elif 'oauth-authorization-server' in self.path or 'openid-configuration' in self.path:
                        metadata = {'issuer':origin,'authorization_endpoint':origin+'/oauth/authorize','token_endpoint':origin+'/oauth/token','response_types_supported':['code'],'grant_types_supported':['authorization_code'],'code_challenge_methods_supported':['S256'],'token_endpoint_auth_methods_supported':['none'],'client_id_metadata_document_supported':True,'authorization_response_iss_parameter_supported':True}
                        if registration: metadata['registration_endpoint'] = origin+'/register'
                        self.respond(200, metadata)
                    else:
                        self.respond(401, {}, {'WWW-Authenticate':'Bearer resource_metadata="'+origin+'/.well-known/oauth-protected-resource"'})
                def do_POST(self):
                    body=self.rfile.read(int(self.headers.get('Content-Length','0'))).decode()
                    events.append({'method':'POST','path':self.path,'body':body})
                    request=json.loads(body)
                    self.respond(201, {**request,'client_id':'dynamic-measured-client','token_endpoint_auth_method':'none'})
            server = http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
            origin = 'http://127.0.0.1:'+str(server.server_port)
            threading.Thread(target=server.serve_forever,daemon=True).start()
            for name in ('home','codex','tmp','xdg'): (root/name).mkdir()
            (root/'codex/config.toml').write_text('mcp_oauth_credentials_store = "file"\n')
            env={'PATH':os.environ['PATH'],'HOME':str(root/'home'),'CODEX_HOME':str(root/'codex'),'TMPDIR':str(root/'tmp'),'XDG_CONFIG_HOME':str(root/'xdg'),'BROWSER':'true'}
            version=subprocess.check_output([a.binary,'--version'],env=env,text=True).strip()
            assert version == 'codex-cli 0.167.0', version
            command=[a.binary,'mcp','add','dingwei','--url',origin+'/mcp']
            output=root/'stdout'
            with output.open('wb') as log:
                proc=subprocess.Popen(command,env=env,cwd=root,stdout=log,stderr=subprocess.STDOUT)
                try:
                    deadline=time.monotonic()+30
                    while time.monotonic()<deadline:
                        match=re.search(r'http://[^\s]+/oauth/authorize\?[^\s]+',output.read_text())
                        if match or proc.poll() is not None: break
                        time.sleep(.05)
                    assert match, output.read_text()
                    params=urllib.parse.parse_qs(urllib.parse.urlsplit(match[0]).query)
                    assert params['client_id']==['https://chatgpt.com/oauth/codex/client.json'], params
                    assert params['code_challenge_method']==['S256'], params
                    assert not any(e['path']=='/register' for e in events), events
                    results.append({'registration_endpoint_advertised':registration,'version':version,'command':command,'authorization_params':params,'http_events':events,'stdout':output.read_text(),'dynamic_registration_attempted':False})
                finally:
                    if proc.poll() is None: proc.terminate()
                    proc.wait(timeout=5)
                    server.shutdown()
    result={'measured_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'binary_sha256':hashlib.sha256(Path(a.binary).resolve().read_bytes()).hexdigest(),'cases':results,'desktop':{'status':'未测','reason':'Linux workspace has no Codex desktop application installed; CLI results do not establish desktop behavior.'},'real_credentials_used':False,'inference_started':False}
    a.output.write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result,indent=2))

if __name__=='__main__': main()
