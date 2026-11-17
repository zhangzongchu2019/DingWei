import pathlib, subprocess, os, json, hashlib, datetime, re, time
root=pathlib.Path('/data/ai-dev-data/r60-ingress-fix-mutations'); out=pathlib.Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-ingress-fix/final-candidate/new-mutations');out.mkdir(exist_ok=True)
env={'HOME':'/tmp/rif-home','TMPDIR':'/tmp','PATH':'/usr/local/bin:/usr/bin:/bin','GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod','GOCACHE':'/home/dev/.cache/go-build','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':'/data/ai-dev-data/helper-deps-lock-venv/bin/python3'}
mutations=[]
def add(name,path,before,after,run): mutations.append((name,path,before,after,run))
i='internal/m8/session_ingress.go';d='internal/m8/session_diag.go';w='internal/m8/session_write_policy.go';v='internal/m8/viewer_output_queue.go'
add('window_64',i,'sessionIngressItems       = 2048','sessionIngressItems       = 64','TestLegacyIngressBurstKeepsFIFOAndCommittedACK')
add('physical_two_seconds',w,'sessionDataWriteLimit    = 20 * time.Second','sessionDataWriteLimit    = 2 * time.Second','TestLegacyIngressReadPauseResumesWithoutDisconnect')
add('fifo_reordered',i,'case f := <-q.frames:\n\t\tif ctx.Err()', 'case f := <-q.frames:\n\t\tselect { case next := <-q.frames: q.frames <- f; f = next; default: }\n\t\tif ctx.Err()', 'TestLegacyIngressBurstKeepsFIFOAndCommittedACK')
add('ack_before_commit','internal/m8/delivery_ack_v2.go','\tresult, _, err := h.Repo.ApplySessionDeliveryACK', '\t_ = c.write(ctx, model.Envelope{ID: randomHex(16), Meta: map[string]any{"type":"delivery_ack_confirm", "delivery_id":d.ID, "ack_protocol":2, "processing_mode":mode, "ack_generation":d.AckGeneration, "state":phase}})\n\tresult, _, err := h.Repo.ApplySessionDeliveryACK','TestLegacyIngressBurstKeepsFIFOAndCommittedACK')
add('no_fragment',w,'if len(payload) <= sessionControlMaxBytes {','if true {','TestLegacyIngressLimitedWirePongCompetition')
add('connection_items',i,'case q.items >= sessionIngressItems:','case false:','TestSessionIngressGlobalDescriptorBudgetAndRelease')
add('global_items',i,'case q.h.ingressBudgetItems >= sessionIngressGlobalItems:','case false:','TestSessionIngressGlobalDescriptorBudgetAndRelease')
add('reserve_connection_bytes',i,'case q.bytes+n > sessionIngressBytes:','case false:','TestSessionIngressQueueBudgetAndRelease')
add('reserve_global_bytes',i,'case q.h.ingressBudgetBytes+n > sessionIngressGlobalBytes:','case false:','TestSessionIngressQueueBudgetAndRelease')
add('read_connection_bytes',i,'if q.bytes+n > sessionIngressBytes {','if false {','TestSessionIngressReadByteBoundsAndCancelledDrain')
add('read_global_bytes',i,'if q.h.ingressBudgetBytes+n > sessionIngressGlobalBytes {','if false {','TestSessionIngressReadByteBoundsAndCancelledDrain')
add('release_global_item',i,'q.h.ingressBudgetItems--','// removed global item release','TestSessionIngressGlobalDescriptorBudgetAndRelease')
add('release_bytes',i,'q.h.ingressBudgetBytes -= n','// removed global byte release','TestSessionIngressReadByteBoundsAndCancelledDrain')
add('cancel_release_active',i,'\tif q.hasActive {\n\t\tq.release(q.active)\n\t\tq.active = 0\n\t\tq.hasActive = false\n\t}\n\tq.mu.Lock()', '\tif q.hasActive { q.active = 0; q.hasActive = false }\n\tq.mu.Lock()', 'TestSessionIngressReadByteBoundsAndCancelledDrain')
add('cancel_release_pending',i,'case f := <-q.frames:\n\t\t\tq.release(len(f.data))','case <-q.frames:\n\t\t\t// pending release removed','TestSessionIngressReadByteBoundsAndCancelledDrain')
add('end_reason',d,'"reason": reason, "phase":','"omitted_reason": reason, "phase":','TestSessionDiagnosticsRealPeerEndAndPrivacy')
add('end_event',d,'d.endOnce.Do(func() { d.emit(d.snapshot("session_end")) })','d.endOnce.Do(func() {})','TestSessionDiagnosticsOverflowAndPhysicalWriteCauses')
add('overflow_reason',d,'d.reason("business_queue_overflow")','d.reason("reader_error")','TestSessionDiagnosticsOverflowAndPhysicalWriteCauses')
add('overflow_limit',d,'data["limit"] = q.limitReason','data["limit"] = "none"','TestSessionDiagnosticsOverflowAndPhysicalWriteCauses')
add('physical_reason',d,'data["reason"] = reason','data["reason"] = "physical_write_error"','TestSessionDiagnosticsOverflowAndPhysicalWriteCauses')
add('physical_error_class',d,'data["error_class"] = class','data["omitted_error_class"] = class','TestSessionDiagnosticsOverflowAndPhysicalWriteCauses')
add('diag_rate',d,'if h.sessionJournalCount >= sessionJournalPerSecond {','if false {','TestSessionDiagnosticsRateAndLineBounds')
add('diag_line_bound',d,'if len(raw) > 4096 || !write(raw) {','if !write(raw) {','TestSessionDiagnosticsRateAndLineBounds')
add('offline_sealed_gap',v,'// Shutdown already sealed admission: never start unowned cleanup.\n\t\t\tv.outputGap.Store(true)','// Shutdown sealed; gap guard removed.','TestViewerOfflineSealedExistingConnectionMarksGapAndCloses')
add('offline_sealed_close',v,'\t\t\t_ = conn.CloseNow()\n\t\t}\n\t}\n}\nfunc (q *viewerOutputQueue) enqueue','\t\t\t// sealed close removed\n\t\t}\n\t}\n}\nfunc (q *viewerOutputQueue) enqueue','TestViewerOfflineSealedExistingConnectionMarksGapAndCloses')
add('offline_pending_gap',v,'if q.bytes != 0 {\n\t\t\t\tv.outputGap.Store(true)\n\t\t\t}','if q.bytes != 0 { /* removed gap */ }','TestViewerOfflinePendingQueueRejectsNewDisplayAndMarksGap')
add('offline_stopped',v,'\t\t\tq.stopped = true\n\t\t\tif q.bytes != 0','\t\t\t// removed stopped\n\t\t\tif q.bytes != 0','TestViewerOfflinePendingQueueRejectsNewDisplayAndMarksGap')
add('ping_cancel','internal/m8/hub.go','\t\t\tif ctx.Err() != nil {\n\t\t\t\treturn\n\t\t\t}\n\t\t\tif err != nil {\n\t\t\t\th.recordKeepaliveFailure','\t\t\tif err != nil {\n\t\t\t\th.recordKeepaliveFailure','TestSessionKeepaliveCancellationDuringActualPing')
results=[]
def run(args,log,cwd=root):
 with open(out/log,'wb') as f:
  p=subprocess.run(args,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=150)
 return p.returncode
for name,path,before,after,test in mutations:
 p=root/path; original=p.read_bytes(); s=original.decode()
 if s.count(before)!=1: raise RuntimeError((name,'anchor matches',s.count(before)))
 started=datetime.datetime.now(datetime.timezone.utc).isoformat()
 try:
  p.write_text(s.replace(before,after)); build=run(['/usr/bin/go','test','-c','-o',str(out/'mutant.test'),'./internal/m8'],name+'-build.log')
  rc=run([str(out/'mutant.test'),'-test.run=^'+test+'$','-test.v','-test.count=1','-test.timeout=90s'],name+'.log',root/'internal/m8') if build==0 else None
  raw=(out/(name+'.log')).read_text(errors='replace') if rc is not None else ''
  record={'name':name,'source_commit':'a24bb16335040ae06486f1c76be69ee542958214','path':path,'before':before,'after':after,'run':test,'utc_start':started,'build_rc':build,'rc':rc,'failed_assertions':[line for line in raw.splitlines() if re.search(r'\.go:\d+:',line)],'panic':('panic:' in raw),'raw_sha256':hashlib.sha256(raw.encode()).hexdigest()}
  record['killed']=build==0 and rc!=0 and '--- FAIL:' in raw and bool(record['failed_assertions']) and not record['panic']
  results.append(record);(out/'summary.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n');print(name,'KILLED' if record['killed'] else 'REVIEW',flush=True)
 finally:
  p.write_bytes(original)
  if p.read_bytes()!=original:raise RuntimeError('restore failed')
# Baseline after every source has been restored. All new compatibility + all retained guards.
rc=run(['/usr/bin/go','test','./internal/m8','-run','LegacyIngress|SessionDiagnostics|SessionIngress|SessionWrite|SessionKeepalive|ViewerOutput|ViewerOffline','-count=1','-v','-timeout=2m'],'restored-baseline.log')
print('restored baseline',rc,flush=True)
(out/'baseline.json').write_text(json.dumps({'rc':rc,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_commit':'a24bb16335040ae06486f1c76be69ee542958214'})+'\n')
(out/'mutant.test').unlink(missing_ok=True)
