from pathlib import Path
import subprocess,json,hashlib,datetime
root=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-r1r2-withdraw');out=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-r1r2-evidence');app=root/'tools/sessionhelper/sessionhelper/app.py';hub=root/'internal/m8/agent_ws.go';original={p:p.read_text() for p in (app,hub)}
env={'HOME':str(out/'withdraw-home'),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0','HUB_TEST_PG_EMBEDDED':'1','DW_DESKTOP_INTEGRATION_PYTHON':'/tmp/coder-0232-locked-venv/bin/python3','LANG':'C.UTF-8','TZ':'UTC'}
Path(env['HOME']).mkdir(exist_ok=True);records=[]
def run(name,pattern):
 cmd=['/usr/bin/go','test','-p','1','-count=1','./internal/m8','-run',pattern,'-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.stdout')).open('wb') as so,(out/(name+'.stderr')).open('wb') as se:r=subprocess.run(cmd,cwd=root,env=env,stdout=so,stderr=se,timeout=120)
 d={'name':name,'command':cmd,'rc':r.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'app_sha256':hashlib.sha256(app.read_bytes()).hexdigest(),'hub_sha256':hashlib.sha256(hub.read_bytes()).hexdigest(),'source_commit':'dda2540227f18864debeb1a98bb834205d24ad78'};records.append(d);(out/(name+'.json')).write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(d),flush=True);return r.returncode
try:
 cases=[('r1-ack-reason',app,'self.delivery_ack(delivery_id,"processed",reason=reason)','self.delivery_ack(delivery_id,"processed")','^TestBlockerAcrossRealHTTPHelperIPCMCP$/missing_identity_ack_returns_to_hub'),('r2-preauth-header',hub,'func (h *Hub) HandleAgentWS(w http.ResponseWriter, r *http.Request) {','func (h *Hub) HandleAgentWS(w http.ResponseWriter, r *http.Request) {\n w.Header().Set("X-DW-Agent-ID", "synthetic-leak")','^TestAgentWSRejectedHandshakeDoesNotExposeAgentIdentity$')]
 for name,p,old,new,pattern in cases:
  assert run(name+'-before',pattern)==0
  assert original[p].count(old)==1
  p.write_text(original[p].replace(old,new));assert run(name+'-withdrawn',pattern)!=0
  p.write_text(original[p]);assert run(name+'-restored',pattern)==0
finally:
 for p,s in original.items():p.write_text(s)
 (out/'withdraw-summary.json').write_text(json.dumps({'records':records,'restored':all(p.read_text()==s for p,s in original.items())},indent=2)+'\n')
