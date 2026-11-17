import os,sys,json,sqlite3,subprocess,time,socket,signal,hashlib,datetime,urllib.request
from pathlib import Path
R=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-rollback-0077');D=R/'private';D.mkdir(exist_ok=True);DB=D/'hub.db';NEW=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-formal-integration-20261117/bin/workpulse');OLD=R/'old-workpulse'
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def startstop(name,binary):
 pub,internal=port(),port();env={'PATH':'/usr/local/bin:/usr/bin:/bin','HOME':str(D),'TZ':'UTC','WP_DATA_DIR':str(D),'WP_DB_PATH':str(DB),'WP_ADDR':f'127.0.0.1:{pub}','WP_INTERNAL_ADDR':f'127.0.0.1:{internal}','FEISHU_TRANSPORT':'fake','WP_CONSOLE_AUTH_ENABLED':'false','WP_SCHEDULE_TEAM_FILE':str(D/'missing.md'),'WP_SCHEDULE_PERSONAL_DIR':str(D/'none')};beg=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (R/(name+'.log')).open('w') as f:
  p=subprocess.Popen([str(binary)],cwd=D,env=env,stdout=f,stderr=subprocess.STDOUT)
  try:
   ready=False
   for i in range(200):
    if p.poll() is not None:break
    try:
     with urllib.request.urlopen(f'http://127.0.0.1:{pub}/healthz',timeout=.3) as res:
      if res.status==200:ready=True;break
    except Exception:pass
    time.sleep(.1)
   assert ready,(name,'startup failed')
   time.sleep(1)
  finally:
   if p.poll() is None:p.send_signal(signal.SIGTERM)
   try:rc=p.wait(timeout=20)
   except subprocess.TimeoutExpired:p.kill();p.wait();raise
 v=dict(name=name,binary=str(binary),binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),health_status=200 if ready else None,exit_code=rc,start_utc=beg,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),env=env,uid=os.getuid(),raw_sha256=hashlib.sha256((R/(name+'.log')).read_bytes()).hexdigest());(R/(name+'.json')).write_text(json.dumps(v,indent=2)+'\n');assert rc==0,(name,rc);return v
def snapshot():
 with sqlite3.connect(DB) as c:
  return {'ledger_0077':c.execute("SELECT version,digest,applied_at FROM schema_migrations WHERE version='0077_outbound_intent.sql'").fetchall(),'fences':c.execute('SELECT * FROM outbound_intent ORDER BY sender_id,source_message_id').fetchall(),'deliveries':c.execute("SELECT id,state FROM session_delivery ORDER BY id").fetchall(),'indices':c.execute("SELECT name,sql FROM sqlite_master WHERE type='index' AND tbl_name='outbound_intent' ORDER BY name").fetchall()}
results={'r60_initialize':startstop('r60-initialize',NEW)}
now=datetime.datetime.now(datetime.timezone.utc);old=now-datetime.timedelta(days=10)
fmt=lambda x:x.strftime('%Y-%m-%dT%H:%M:%S.%f')+'000Z'
with sqlite3.connect(DB) as c:
 for label,state,created in [('W','processed',now),('P','queued',old),('E','processed',old)]:
  src={'W':'a','P':'b','E':'c'}[label]*32;did='dw-dlv_'+src
  env=json.dumps(dict(id=src,from_='fixture#u_zzc',to='offline#u_zzc',body='ISOLATED_ROLLBACK_'+label,meta={'outbound_recovery_v1':True}))
  c.execute('INSERT INTO session_delivery(id,key_id,session_name,envelope_json,source_message_id,source_type,state,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)',(did,'u_zzc','offline',env,src,'agent_route',state,fmt(now),fmt(now),fmt(now)))
  c.execute('INSERT INTO outbound_intent VALUES(?,?,?,?,?,?)',('synthetic-sender',src,'synthetic-target','0'*64,did,fmt(created)))
 c.commit()
results['before_old']=snapshot();results['old_on_0077']=startstop('old-on-0077',OLD);results['after_old']=snapshot();assert results['before_old']==results['after_old'],'old altered0077 records'
results['r60_restart']=startstop('r60-restart',NEW);results['after_forward']=snapshot();assert results['after_old']==results['after_forward'],'forward startup changed fences'
(R/'binary-roundtrip.json').write_text(json.dumps(results,indent=2)+'\n');print('BINARY_ROUNDTRIP_OK old39health200 graceful0 retained0077/fences/indices;new40health200 graceful0 samefences',flush=True)
