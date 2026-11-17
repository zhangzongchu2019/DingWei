exec(open('/tmp/r60-formal-run.py').read().split("if __name__")[0])
w=Path('/data/ai-dev-data/r60-integration-mutation');go=['go','test','./internal/m8','-count=1','-v','-timeout','2m'];q='internal/m8/viewer_output_queue.go';hp='tools/sessionhelper/sessionhelper/app.py'
cuts=[
('sealed_gap',q,'\t\t\t// Shutdown already sealed admission: never start unowned cleanup.\n\t\t\tv.outputGap.Store(true)','\t\t\t// removed sealed gap','TestViewerOfflineSealedExistingConnectionMarksGapAndCloses'),
('sealed_close',q,'\t\t\t_ = conn.CloseNow()','\t\t\t// removed sealed close','TestViewerOfflineSealedExistingConnectionMarksGapAndCloses'),
('pending_gap',q,'\t\t\tif q.bytes != 0 {\n\t\t\t\tv.outputGap.Store(true)\n\t\t\t}','\t\t\t// removed pending gap','TestViewerOfflinePendingQueueRejectsNewDisplayAndMarksGap'),
('offline_stop',q,'\t\t\tq.stopped = true','\t\t\t// removed offline stop','TestViewerOfflinePendingQueueRejectsNewDisplayAndMarksGap'),
('heartbeat_current',hp,'heartbeat_handler=current(self._make_heartbeat_handler)','heartbeat_handler=self._make_heartbeat_handler(ws)',None),
('cli_original_receipt','tools/sessionhelper/sessionhelper/outbound_queue.py','return self.outcome(old)',"return self.outcome(dict(old, source_id=uuid.uuid4().hex))",'TestHostedBodySendSameRequestIDAcrossCLIProcesses'),
('joint_write_deadline','internal/m8/hub.go','ctx, cancel := context.WithTimeout(ctx, sessionWriteLimit)','ctx, cancel := context.WithCancel(ctx)','TestR60OutboundControlBackpressureRecoversWithSendStatus'),
('joint_stable_retry','tools/sessionhelper/sessionhelper/outbound_recovery.py',"source_id=row['source_id'],recovery=row,timeout=10)","source_id=(row['source_id'] if row['attempts']==1 else __import__('uuid').uuid4().hex),recovery=row,timeout=10)",'TestR60OutboundControlBackpressureRecoversWithSendStatus')]
base=run('mutations-baseline',w,go+['-run','TestViewerOfflineSealedExisting|TestViewerOfflinePendingQueue|TestHostedBodySendSameRequest|TestR60OutboundControlBackpressure']);assert base['rc']==0
out=[]
for name,file,before,after,test in cuts:
 p=w/file;orig=p.read_bytes();s=orig.decode();assert s.count(before)==1,(name,s.count(before))
 try:
  p.write_text(s.replace(before,after));python=file.endswith('.py');compilecmd=['python3','-m','py_compile',file] if python else ['go','test','./internal/m8','-run','^$','-timeout','2m']
  c=run('cut-'+name+'-compile',w,compilecmd,mutation=True)
  cmd=go+['-run','^'+test+'$'] if test else ['python3','-m','unittest','test_session_heartbeat.PersistentHeartbeatReconnectTest']
  v=run('cut-'+name,w,cmd,extra={'PYTHONPATH':str(w/'tools/sessionhelper')+':'+str(w/'tools/sessionhelper/tests')},mutation=True)
  raw=(R/('cut-'+name+'.log')).read_text();assert c['rc']==0 and v['rc']!=0 and ('--- FAIL:' in raw or 'FAIL:' in raw) and 'panic:' not in raw and '[build failed]' not in raw,(name,'not business red')
  out.append(dict(name=name,file=file,before=before,after=after,compile=c,result=v,failed_assertions=[x for x in raw.splitlines() if 'FAIL:' in x or 'test.go:' in x or 'AssertionError' in x]));(R/'mutations.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
 finally:p.write_bytes(orig);assert p.read_bytes()==orig
assert not git(w,'status','--porcelain')
assert run('mutations-restored',w,go+['-run','TestViewerOfflineSealedExisting|TestViewerOfflinePendingQueue|TestHostedBodySendSameRequest|TestR60OutboundControlBackpressure'])['rc']==0
assert run('heartbeat-restored',w,['python3','-m','unittest','test_session_heartbeat.PersistentHeartbeatReconnectTest'],extra={'PYTHONPATH':str(w/'tools/sessionhelper')+':'+str(w/'tools/sessionhelper/tests')})['rc']==0
print('8 CUTS BUSINESS RED; RESTORED BASELINES GREEN',flush=True)
