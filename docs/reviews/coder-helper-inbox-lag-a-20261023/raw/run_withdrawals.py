from pathlib import Path
import datetime, hashlib, json, subprocess, time
root=Path('/data/ai-dev-data/helper-inbox-lag-a');out=Path('/data/ai-dev-data/helper-inbox-lag-a-final-evidence/withdrawals');out.mkdir(exist_ok=True)
py='/tmp/coder-0232-locked-venv/bin/python3'
home=out/'home';home.mkdir(exist_ok=True)
env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','PYTHONPATH':str(root/'tools/sessionhelper')+':'+str(root/'view-service'),'NODE_PATH':'/tmp/coder-a3-playwright/node_modules','LANG':'C.UTF-8','TZ':'UTC'}
def unittest(name):return [py,'-B','-m','unittest','-v',name]
def gotest(name):return ['go','test','./internal/m8','-run','^'+name+'$','-count=1']
claude='tools.sessionhelper.tests.test_inbox_progress.InboxProgressTest.test_real_claude_submitted_receipt_is_not_admitted_before_replay_ack'
source='tools/sessionhelper/sessionhelper/'
cases=[
 ('stage-admission-write',source+'driver_adapter.py','        observe(getattr(self, "delivery_progress", None), command.delivery_id, "model_admitted")','        pass # withdrawn admission observation',unittest(claude)),
 ('claude-weak-processing',source+'driver_adapter.py','if self._which == "claude" and receipt.state == "processing" and not str(receipt.detail).startswith("accepted,"):','if False:',unittest(claude)),
 ('restart-unknown',source+'inbox_progress.py','db.execute("UPDATE delivery_progress SET stage=\'unknown\' WHERE session_name=? AND stage IN (\'adapter_queued\',\'dispatching\')", (session,))','pass # withdrawn restart uncertainty',unittest('tools.sessionhelper.tests.test_inbox_progress.InboxProgressTest.test_restart_and_historical_processed_are_unknown_without_replay')),
 ('hub-sender-ownership','internal/m8/inbox_lag.go','json.Unmarshal([]byte(delivery.Envelope), &original) != nil || original.From != sessionAddress(c.sessionName, c.keyID)','json.Unmarshal([]byte(delivery.Envelope), &original) != nil',gotest('TestInboxLagSenderOwnDeliveryAndCurrentACL')),
 ('hub-current-group-acl','internal/m8/inbox_lag.go','if err != nil || !allowed {','if err != nil || (!allowed && false) {',gotest('TestInboxLagSenderOwnDeliveryAndCurrentACL')),
 ('hub-freshness','internal/m8/inbox_lag.go','if age < 0 || age >= 10*time.Second {','if age < 0 {',gotest('TestInboxLagFreshnessAndEpoch')),
 ('view-local-expiry','view-service/renderer-canary-sse.html',"if(this.state==='fresh'&&(!Number.isFinite(now)||now>=this.record.deadline))", "if(false&&this.state==='fresh'&&(!Number.isFinite(now)||now>=this.record.deadline))",unittest('view-service/test_inbox_lag_resource.py')),
 ('query-connection-binding',source+'outbound_receipt.py','if (connection is ws and self.valid_hub_address(env) and not future.done()','if (self.valid_hub_address(env) and not future.done()',unittest('tools.sessionhelper.tests.test_delivery_status.DeliveryStatusTest.test_wrong_nonce_connection_sender_or_delivery_cannot_resolve')),
 ('ipc-restricted-mode',source+'ipc_listener.py','    if send_body_only:\n','    if send_body_only and msg_type != \'delivery_status_v1\':\n',unittest('tools.sessionhelper.tests.test_delivery_status.DeliveryStatusTest.test_real_ipc_shim_roundtrip_and_restricted_modes')),
 ('legacy-direct-default','internal/m8/hub.go','if outboundReceiptRequested(env) && !(reliable && (deliveryACK || capabilityPending) && h.Repo != nil) {','if wantReceipt && !(reliable && (deliveryACK || capabilityPending) && h.Repo != nil) {',gotest('TestInboxLagObservationalReceiptPreservesLegacyDirectDelivery')),
 ('view-scheduled-lag-poll','view-service/renderer-canary-sse.html','inboxLagTimer = setInterval(refreshInboxLag, INBOX_LAG_REFRESH_MS);','inboxLagTimer = setInterval(() => {}, INBOX_LAG_REFRESH_MS);',['node','view-service/inbox-lag-regression.js']),
]
records=[]
for name,file,old,new,cmd in cases:
 p=root/file;original=p.read_bytes();s=original.decode();assert s.count(old)==1,(name,s.count(old))
 sha=hashlib.sha256(original).hexdigest();mutant=s.replace(old,new,1).encode();record={'name':name,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'file':file,'original_sha256':sha,'mutant_sha256':hashlib.sha256(mutant).hexdigest(),'command':cmd,'environment':env,'runs':[]}
 try:
  for phase in ['green-before','withdrawn-red','restored-green']:
   p.write_bytes(mutant if phase=='withdrawn-red' else original)
   start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
   r=subprocess.run(cmd,cwd=root,env=env,capture_output=True,timeout=120)
   (out/(name+'-'+phase+'.stdout')).write_bytes(r.stdout);(out/(name+'-'+phase+'.stderr')).write_bytes(r.stderr)
   text=(r.stdout+r.stderr).decode(errors='replace');semantic=('AssertionError' in text or '--- FAIL:' in text)
   record['runs'].append({'phase':phase,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':round(time.monotonic()-t,3),'rc':r.returncode,'semantic_assertion_failure':semantic})
   if phase=='withdrawn-red':assert r.returncode!=0 and semantic,(name,phase,text[-2500:])
   else:assert r.returncode==0,(name,phase,text[-2500:])
 finally:p.write_bytes(original)
 assert hashlib.sha256(p.read_bytes()).hexdigest()==sha
 record['restored_sha256']=sha;record['result']='GREEN_RED_GREEN';records.append(record);(out/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n');print(name,'GREEN_RED_GREEN',flush=True)
(out/'SUMMARY.json').write_text(json.dumps(records,indent=2)+'\n')
