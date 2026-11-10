"""Actual 0.167.0 add+login+catalog+list_online with synthetic local Feishu."""
import argparse,atexit,http.cookiejar,html,json,os,queue,re,ssl,subprocess,time,urllib.error,urllib.parse,urllib.request
from pathlib import Path
RUN=Path(os.environ.get('INTEROP_RUN_DIR','/tmp/coder-r56-compat-native'))
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args):return None

def cleanup(proc):
 if proc.poll() is None:proc.terminate();proc.wait(timeout=5)

def main():
 ap=argparse.ArgumentParser();ap.add_argument('--binary',default='/home/dev/.local/bin/codex');ap.add_argument('--label',default='native');ap.add_argument('--foreign',action='store_true');ap.add_argument('--client-mode',choices=['default','explicit'],default='default');a=ap.parse_args()
 ready=json.loads((RUN/'ready.json').read_text());origin=ready['origin'];p=RUN/a.label;p.mkdir(exist_ok=False)
 for n in ['home','codex','tmp','xdg']:(p/n).mkdir()
 (p/'codex/config.toml').write_text('mcp_oauth_credentials_store = "file"\n')
 env={'PATH':os.environ['PATH'],'HOME':str(p/'home'),'CODEX_HOME':str(p/'codex'),'TMPDIR':str(p/'tmp'),'XDG_CONFIG_HOME':str(p/'xdg'),'XDG_CACHE_HOME':str(p/'xdg'),'XDG_STATE_HOME':str(p/'xdg'),'BROWSER':'true','SSL_CERT_FILE':ready['ca_file'],'CODEX_CA_CERTIFICATE':ready['ca_file']}
 version=subprocess.check_output([a.binary,'--version'],env=env,text=True).strip();assert version=='codex-cli 0.167.0',version
 context=ssl.create_default_context(cafile=ready['ca_file']);jar=http.cookiejar.CookieJar();browser=urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),urllib.request.HTTPCookieProcessor(jar),NoRedirect())
 def req(url,data=None):
  request=urllib.request.Request(url,data=urllib.parse.urlencode(data).encode() if data is not None else None)
  if data is not None:request.add_header('Origin',origin)
  try:r=browser.open(request,timeout=10)
  except urllib.error.HTTPError as e:r=e
  return r.status,dict(r.headers),r.read().decode()
 status,headers,body=req(origin+'/mcp/clients/codex.json');assert status==200
 document=json.loads(body);assert document['redirect_uris']==['http://127.0.0.1/callback','http://[::1]/callback']
 expected_id=document['client_id'] if a.client_mode=='explicit' else 'https://chatgpt.com/oauth/codex/client.json'
 record={'version':version,'origin':origin,'legacy_explicit_client_document':document,'issuer_metadata_removed_for_cli_probe':ready.get('issuer_metadata_removed_for_cli_probe',False),'client_mode':a.client_mode,'foreign_tenant':a.foreign,'commands':[],'real_credentials_used':False}
 try:
  for name,args in [('add',['mcp','add','dingwei','--url',ready['resource']]+(['--oauth-client-id',expected_id] if a.client_mode=='explicit' else [])),('login',['mcp','login','dingwei'])]:
   path=p/(name+'.stdout');log=path.open('wb');cmd=[a.binary,*args];proc=subprocess.Popen(cmd,env=env,cwd=p,stdout=log,stderr=subprocess.STDOUT);atexit.register(cleanup,proc)
   item={'command':cmd,'stdout':path.name};record['commands'].append(item);deadline=time.monotonic()+25;url=None
   while time.monotonic()<deadline:
    match=re.search(r'https?://[^\s]+/oauth/authorize\?[^\s]+',path.read_text(errors='replace'))
    if match:url=match.group(0);break
    if proc.poll() is not None:break
    time.sleep(.05)
   assert url,'NATIVE_AUTHORIZATION_URL_MISSING'
   q=urllib.parse.parse_qs(urllib.parse.urlparse(url).query);item['authorization_params']=q
   assert len(q['resource'])==1 and q['client_id']==[expected_id]
   status,headers,body=req(url);item['authorize_status']=status
   assert status==302,'NATIVE_OAUTH_AUTHORIZE_MUST_SUCCEED: '+str((status,body))
   connect=urllib.parse.urljoin(origin,headers['Location']);status,headers,body=req(connect)
   m=re.search(r'name="csrf_token" value="([^"]+)"',body);assert status==200 and m
   assert 'name="short_name"' not in body and 'name="target_agent"' not in body and 'type="text"' not in body,'ZC_NATIVE_INPUT_PRESENT'
   assert '同意并进入飞书' in body,'ZC_NATIVE_CONSENT_MISSING'
   item['consent_page_no_editable_inputs']=True
   attempt=urllib.parse.parse_qs(urllib.parse.urlparse(connect).query)['attempt_id'][0]
   status,headers,body=req(origin+'/feishu/connect',{'attempt_id':attempt,'csrf_token':html.unescape(m[1])});assert status==303,(status,body)
   state=urllib.parse.parse_qs(urllib.parse.urlparse(headers['Location']).query)['state'][0]
   status,headers,body=req(origin+'/mcp/oauth/feishu/callback?'+urllib.parse.urlencode({'state':state,'code':'synthetic-feishu-code'}));item['feishu_callback_status']=status
   if a.foreign:
    assert status==400 and 'access_denied' in body,'FOREIGN_TENANT_MUST_BE_REJECTED'
    cleanup(proc);item['rc']=proc.returncode;record['foreign_rejected']=True;break
   assert status==302,(status,body)
   status,headers,body=req(headers['Location']);item['native_callback_status']=status;assert status==200
   proc.wait(timeout=15);item['rc']=proc.returncode;assert proc.returncode==0,'NATIVE_LOGIN_MUST_SUCCEED'
  if not a.foreign:
   configuration=(p/'codex/config.toml').read_text()
   assert 'oauth_resource' not in configuration,'ZERO_CONFIG_CONTAINS_EXTRA_PARAMETERS'
   if a.client_mode=='default':assert 'oauth_client_id' not in configuration,'ZERO_CONFIG_CONTAINS_CLIENT_ID'
   record['url_only_configuration' if a.client_mode=='default' else 'explicit_configuration']=configuration
   command=[os.sys.executable,str(Path(__file__).with_name('rpc_probe.py')),'--binary',a.binary,'--label',a.label]
   r=subprocess.run(command,env={**env,'INTEROP_RUN_DIR':str(RUN)},stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=60)
   (p/'rpc.stdout').write_bytes(r.stdout);assert r.returncode==0,'NATIVE_RPC_TRANSPORT_FAILED'
   rpc=json.loads((p/'rpc.json').read_text());responses={x['receive']['id']:x['receive'] for x in rpc['transcript'] if 'receive' in x and 'id' in x['receive']}
   status=responses[3]['result']['data'][0];assert set(status['tools'])=={'list_online','send_message','wait_for_messages','ack'},'CODEX_2025_INITIALIZE: native handshake must populate tool catalog'
   call=responses[4];assert 'result' in call,'NATIVE_LIST_ONLINE_FAILED: '+str(call)
   record['tool_names']=list(status['tools']);record['read_only_call']=call['result'];record['native_e2e_passed']=True
 finally:
  (p/'RESULT.json').write_text(json.dumps(record,indent=2)+'\n')
 print(json.dumps(record,indent=2))
if __name__=='__main__':main()
