import datetime,hashlib,json,os,re,subprocess
from pathlib import Path
root=Path('/data/ai-dev-data/r60-send-lost-mutations')
out=Path('/tmp/r60-send-lost-20261116/mutations-approved');out.mkdir(exist_ok=True)
env={'HOME':str(out/'home'),'TMPDIR':str(out/'tmp'),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1','HUB_TEST_PG_STRICT':'1','GOTOOLCHAIN':'local','GOCACHE':'/data/ai-dev-data/local-run-artifacts/coder56solzzc/go-cache','GOMODCACHE':'/tmp/busy-cf05/h-normal/go/pkg/mod','GOFLAGS':'-p=1','GOPROXY':'off','GOSUMDB':'off','PYTHONPATH':'tools/sessionhelper:tools/sessionhelper/tests'}
Path(env['HOME']).mkdir(exist_ok=True);Path(env['TMPDIR']).mkdir(exist_ok=True)
py=lambda target:['python3','-m','unittest',target,'-v']
go=lambda pkg,target:['go','test',pkg,'-run','^'+target+'$','-count=1','-v','-timeout','45s']
q='tools/sessionhelper/sessionhelper/outbound_queue.py';w='tools/sessionhelper/sessionhelper/outbound_recovery.py';b='tools/sessionhelper/sessionhelper/outbound_receipt.py';cli='tools/sessionhelper/sessionhelper/send_dingwei.py';gr='internal/m8/outbound_recovery.go';st='internal/store/outbound_intent.go'
cuts=[
('source_id_stable',w,"source_id=row['source_id']","source_id=uuid.uuid4().hex",py('test_outbound_queue.RecoveryWorkerTest.test_disconnect_then_same_id_and_lost_receipt_recovery')),
('unknown_body_retained',q,"IN ('hub_durable_queued','rejected')","IN ('hub_durable_queued','rejected','unknown')",py('test_outbound_queue.DurableQueueTest.test_offline_admission_restart_and_conflict')),
('local_commit_before_acceptance',q,'try:db.commit()','try:db.rollback()',py('test_outbound_queue.DurableQueueTest.test_offline_admission_restart_and_conflict')),
('one_writer',w,'try: fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)','try: pass',py('test_outbound_queue.RecoveryWorkerTest.test_only_one_process_writer_can_hold_outbox')),
('receipt_responsibility_schema',cli,"if valid:return out","if isinstance(out,dict):return out",py('test_outbound_queue.ReceiptSchemaTest.test_accepted_requires_correlated_responsibility_identifiers')),
('physical_identity_fence',gr,'func (h *Hub) checkOutboundDelivery(ctx context.Context, c *sessionClient, d *model.SessionDelivery) error {','func (h *Hub) checkOutboundDelivery(ctx context.Context, c *sessionClient, d *model.SessionDelivery) error {\nreturn nil',go('./internal/m8','TestOutboundRecoveryStableTargetAndRevocationCheckedAtPhysicalWrite')),
('business_digest',gr,'[]any{env.To, env.Body, meta}','[]any{env.To, meta}',go('./internal/m8','TestOutboundRecoveryReplaysOriginalAcceptanceAndRejectsConflict')),
('atomic_acceptance_queue',st,'if _, err = tx.ExecContext(ctx, `INSERT INTO session_delivery','if err=tx.Commit();err!=nil{return nil,false,err}\nif _, err = s.db.ExecContext(ctx, `INSERT INTO session_delivery',go('./internal/store','TestOutboundIntentRecipientFailureRollsBackAcceptance')),
('durable_receipt_replay',st,'if old != nil {','if false && old != nil {',go('./internal/store','TestOutboundIntentAtomicConcurrentRestartAndRetention')),
('old_hub_no_fallback',w,"if not cap:\n            self._unsupported_ws=ws\n            await asyncio.to_thread(self.queue.unsupported_peer)\n            return  # Old Hub: no body write and an explicit local result.","if not cap:\n            cap={'sender_id':self.queue.sender(),'targets':[]}",py('test_outbound_queue.RecoveryWorkerTest.test_old_or_missing_capability_never_writes')),
('nonce_generation',b,"meta.get('nonce') != nonce","False",py('test_outbound_receipt.ReceiptTest.test_wrong_nonce_or_other_connection_not_accepted')),
('temporary_not_revocation','internal/m8/sub_agent.go','if errors.Is(err, errOutboundDenied) {\n\t\treturn true','if err != nil {\n\t\treturn true',go('./internal/m8','TestOutboundRecoveryPhysicalGuardPreservesTemporaryError')),
('status_current_actor',q,"expected=actor_id if actor_id is not None else (sender[0] if sender else '')","expected=r['sender_id']",py('test_outbound_queue.DurableQueueTest.test_replacement_sender_cannot_recover_prior_receipt_before_catalog_tick')),
('notice_original_sender',gr,'h.Identity == nil || c.metricsAgentID != intent.SenderID','h.Identity == nil',go('./internal/m8','TestOutboundRecoveryFailureNoticeIsVisibleAndBoundToOriginalSender')),

('verified_owner_alias',q,"alias = target.lower()","alias = target.split('#',1)[0].lower()",py('test_outbound_queue.DurableQueueTest.test_explicit_owner_key_is_not_silently_discarded')),
('legacy_schema_preflight','internal/store/sqlite.go',' || name == "0077_outbound_intent.sql"','',go('./internal/store','TestMigrationLedger_LegacyAckBackfillsWithoutReplay')),
('unsupported_recipient_is_final',gr,'if errors.Is(e, errOutboundTargetUnsupported) {','if false && errors.Is(e, errOutboundTargetUnsupported) {',go('./internal/m8','TestOutboundRecoveryReplaysOriginalAcceptanceAndRejectsConflict')),

('atomic_capacity_fence',st,'if own >= 4096 || total >= 65536 {','if false {',go('./internal/store','TestOutboundIntentCapacityPreservesOldReceiptAndRejectsNewAtomically')),
('known_refusal_not_unknown',gr,'if errors.Is(e, store.ErrOutboundCapacity) {','if false && errors.Is(e, store.ErrOutboundCapacity) {',go('./internal/m8','TestOutboundRecoveryKnownCapacityFailureIsNotAmbiguousAcceptance')),

('prior_acceptance_uncertainty',w,"if row['attempts'] > 1:","if False:",py('test_outbound_queue.RecoveryWorkerTest.test_current_refusal_after_lost_receipt_preserves_original_unknown')),


('offline_expiry',w,'await asyncio.to_thread(self.queue.expire)','pass',py('test_outbound_queue.RecoveryWorkerTest.test_offline_unattempted_request_expires')),
('capability_deadline',w,'if time.monotonic()-self._cap_wait_started >= self.CAPABILITY_WAIT:','if False:',py('test_outbound_queue.RecoveryWorkerTest.test_missing_capability_deadline_is_explicit')),
('unsupported_unknown_hold',q,"AND reason NOT IN ('outbound_recovery_unsupported','sender_identity_changed')",'',py('test_outbound_queue.RecoveryWorkerTest.test_unsupported_unknown_is_not_automatically_replayed_on_upgrade')),
('recipient_not_sender_key',cli,'return to.strip()',"return to.strip()+'#'+os.environ.get('SH_KEY_ID','')",py('test_ipc_send_addr.DestinationIdentityTest.test_inherited_sender_key_never_rewrites_recipient')),
('canonical_receipt_scope',b,'name.casefold()', 'name',py('test_outbound_receipt.ReceiptTest.test_canonical_casefolded_session_receipt')),

]
cuts[-1]=(*cuts[-1][:4],['python3','-c',"from sessionhelper.outbound_receipt import OutboundReceiptBroker; b=OutboundReceiptBroker(['Desktop.ZZC']); ws=object(); c=b.prepare(ws); b.receive(ws,{'from':'workpulse#u_fixture','to':'desktop.zzc#u_fixture','meta':{'type':'outbound_capabilities','version':1,'nonce':c['outbound_receipt_cap_nonce'],'durable_agent_queue':True}}); assert b.negotiated, 'canonical authenticated session name lost durable capability'"])
records=[]
def run(name,cmd):
 p=out/(name+'.log')
 with p.open('w') as f:r=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=150)
 data=p.read_text();return r.returncode,data,hashlib.sha256(p.read_bytes()).hexdigest()
baseline=[]
for name,cmd in [('baseline-python',['python3','-m','unittest','test_outbound_queue','test_outbound_receipt','-v']),('baseline-go',['go','test','./internal/m8','./internal/store','-run','TestOutbound','-count=1','-v','-timeout','60s'])]:
 rc,data,sha=run(name,cmd)
 baseline.append({'name':name,'rc':rc,'raw_sha256':sha,'source_commit':'ba7acae7798eda8a6d17f3e03dfa07c8c65835f1'})
 if rc:raise SystemExit('baseline failed '+name)
(out/'baselines.json').write_text(json.dumps(baseline,indent=2)+'\n')
for name,path,before,after,cmd in cuts:
 p=root/path;original=p.read_text()
 if original.count(before)!=1 and name!='unknown_body_retained':raise RuntimeError('nonunique cut '+name+': '+str(original.count(before)))
 try:
  p.write_text(original.replace(before,after,1))
  compile_cmd=['python3','-m','py_compile',path] if path.endswith('.py') else ['go','test',cmd[2],'-run','^$','-count=1','-timeout','60s']
  build,_,_=run(name+'-compile',compile_cmd)
  rc,data,sha=run(name,cmd) if build==0 else (build,'','')
  assertions=[line for line in data.splitlines() if re.search(r'(AssertionError:|FAIL:|_test.go:\d+:)',line)]
  v={'name':name,'source_commit':'ba7acae7798eda8a6d17f3e03dfa07c8c65835f1','path':path,'before':before,'after':after,'build_rc':build,'rc':rc,'failed_assertions':assertions,'raw_sha256':sha,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
  records.append(v);(out/'summary.json').write_text(json.dumps(records,indent=2)+'\n');print(name,'build',build,'rc',rc,'assertions',len(assertions),flush=True)
 finally:p.write_text(original)
if any(v['build_rc'] or not v['rc'] or not v['failed_assertions'] for v in records):raise SystemExit(1)
