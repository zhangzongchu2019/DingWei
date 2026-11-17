import pathlib, json, subprocess, datetime, hashlib,re
root=pathlib.Path('/data/ai-dev-data/r60-ingress-fix-mutations');out=pathlib.Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-ingress-fix/final-candidate/retained-mutations');out.mkdir(exist_ok=True)
s=(root/'docs/reviews/r60-hub-keepalive/evidence-q6/mutate-final.py').read_text();ns={};exec(compile(s[:s.index('records=[]')],'<trusted cut definitions>','exec'),ns);cuts=ns['cuts']
env={'HOME':'/tmp/rif-home','TMPDIR':'/tmp','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod','GOCACHE':'/home/dev/.cache/go-build','GOPROXY':'off','GOSUMDB':'off','PYTHONPATH':'tools/sessionhelper:tools/sessionhelper/tests','PYTHONDONTWRITEBYTECODE':'1','DW_DESKTOP_INTEGRATION_PYTHON':'/data/ai-dev-data/helper-deps-lock-venv/bin/python3'}
results=[]
for name,path,before,after,cmd in cuts:
 if name=='reader_independence':before='h.startSessionIngress(ingressCtx, stopIngress, c)';after='c.ingress = nil'
 if name=='default_session_write_budget':before='waitCtx, waitCancel := context.WithTimeout(ctx, sessionWriteLimit)';after='waitCtx, waitCancel := context.WithCancel(ctx)'
 if name=='failed_ack_not_success':
  before='result, _, err := h.Repo.ApplySessionDeliveryACK(ctx, ack, h.now().UTC())\n\tif err != nil {\n\t\treturn true\n\t}'
  after='result, _, err := h.Repo.ApplySessionDeliveryACK(ctx, ack, h.now().UTC())\n\tif err != nil {\n\t\t_ = c.write(ctx, model.Envelope{ID: randomHex(16), Meta:map[string]any{"type":"delivery_ack_confirm", "delivery_id":d.ID, "ack_protocol":2,"processing_mode":mode,"ack_generation":d.AckGeneration,"state":phase}})\n\t\treturn true\n\t}'
 p=root/path;orig=p.read_bytes();text=orig.decode();matches=text.count(before)
 if not matches:raise RuntimeError((name,'anchor absent'))
 def run(args,kind):
  log=out/(name+kind+'.log')
  with log.open('wb') as f:r=subprocess.run(args,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=150)
  return r.returncode,log.read_bytes()
 try:
  p.write_text(text.replace(before,after))
  compilecmd=['python3','-m','py_compile',path] if path.endswith('.py') else ['go','test',cmd[2],'-run','^$','-count=1','-timeout','60s']
  build,_=run(compilecmd,'-build');rc,raw=run(cmd,'') if build==0 else (None,b'');data=raw.decode(errors='replace')
  assertion=[l for l in data.splitlines() if re.search(r'(AssertionError:|_test.go:\d+:)',l)]
  v={'name':name,'source_commit':'a24bb16335040ae06486f1c76be69ee542958214','path':path,'before':before,'after':after,'replacement_count':matches,'build_rc':build,'rc':rc,'failed_assertions':assertion,'raw_sha256':hashlib.sha256(raw).hexdigest(),'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};v['killed']=build==0 and rc not in (None,0) and bool(assertion) and 'panic:' not in data and 'panic serving' not in data
  results.append(v);(out/'summary.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n');print(name,'KILLED' if v['killed'] else 'REVIEW',flush=True)
 finally:p.write_bytes(orig)
print('restored',subprocess.run(['git','status','--porcelain'],cwd=root,stdout=subprocess.PIPE,text=True).stdout,flush=True)
