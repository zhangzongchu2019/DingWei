import pathlib,subprocess,os,json,hashlib,datetime
root=pathlib.Path('/data/ai-dev-data/wt-r61-n3-legacy-cuts');out=pathlib.Path(__file__).parent/'mutations';out.mkdir(exist_ok=True)
store='internal/store/session_history_provenance.go';history='internal/store/session_history.go';ddl='internal/store/history_migrations/0078_history_provenance.sql'
S=['go','test','./internal/store','-run','^TestHistoryProvenance','-count=1','-v']
M=['go','test','./internal/m8','-run','^TestHistoryProvenance','-count=1','-v']
B=['node','view-service/history-provenance-browser-regression.js']
env=json.loads((pathlib.Path(__file__).parent/'env.json').read_text())
pathlib.Path(env['HOME']).mkdir(exist_ok=True)
# Sections constrain replacements to the Append consumer rather than producer.
cases=[]
def cut(name,file,before,after,command=S,section=None):cases.append(dict(name=name,file=file,before=before,after=after,command=command,section=section))
for predicate in ['owner_key=?','agent_key_id=?','session_name=?','generation=?','delivery_id=?']:
 cut('association_'+predicate.split('=')[0],store,predicate,'? IS NOT NULL',section='func enrichHistoryInput')
cut('admitted_only',store,'data.Lifecycle != "admitted"','false',section='func enrichHistoryInput')
cut('user_input_only',store,'if kind != "user_input" {','if false {',section='func enrichHistoryInput')
cut('same_delivery_origin',store,'delivery != origin','false',section='func enrichHistoryInput')
cut('nonempty_turn',store,'turn == ""','false',section='func enrichHistoryInput')
cut('stored_trust',store,'err == nil && p.valid()','err == nil',section='func enrichHistoryInput')
cut('conflict',store,'AND conflicted=0','AND 1=1',section='func enrichHistoryInput')
cut('expiry_match',store,'AND expires_at_ms>?','AND ?>0',section='func enrichHistoryInput')
cut('strip_client_claim',store,'delete(event, "provenance")','// cut: retain client claim',section='func enrichHistoryInput')
cut('consume',store,'DELETE FROM session_history_provenance WHERE owner_key=?','DELETE FROM session_history_provenance WHERE 0 AND owner_key=?',section='func enrichHistoryInput')
cut('oversize_rollback',store,'if n > SessionHistoryEventMaxBytes {','if false {',section='func enrichHistoryInput')
cut('agent_quota',store,'own+n > SessionHistoryAgentMaxBytes','false')
cut('global_quota',store,'total+n > SessionHistoryGlobalMaxBytes','false')
cut('ttl_cleanup',history,'"expires_at_ms<=?", now.UnixMilli()','"0 AND expires_at_ms<=?", now.UnixMilli()')
cut('identity_cleanup',history,'"owner_key=? AND agent_key_id=?", owner, agent); e != nil {','"0 AND owner_key=? AND agent_key_id=?", owner, agent); e != nil {',section='func (h *SessionHistory) CleanupBatch')
cut('disable_cleanup',history,'`DELETE FROM session_history_provenance`','`DELETE FROM session_history_provenance WHERE 0`')
cut('queued_eviction',history,"delivery_id=(SELECT json_extract", "0 AND delivery_id=(SELECT json_extract",section='func deleteHistoryRows')
cut('sidecar_eviction',store,'out = append(out, r)','if r.delivery=="" {out = append(out,r)}',section='func historyEvictionRows')
cut('generation_cleanup',ddl,'WHEN OLD.generation<>NEW.generation','WHEN 0')
cut('global_insert_accounting',ddl,'retained_bytes=retained_bytes+NEW.record_bytes WHERE singleton','retained_bytes=retained_bytes+0*NEW.record_bytes WHERE singleton')
cut('agent_insert_accounting',ddl,'retained_bytes=retained_bytes+NEW.record_bytes WHERE owner_key','retained_bytes=retained_bytes+0*NEW.record_bytes WHERE owner_key')
cut('global_consume_accounting',ddl,'retained_bytes=retained_bytes-OLD.record_bytes WHERE singleton','retained_bytes=retained_bytes-0*OLD.record_bytes WHERE singleton')
cut('agent_consume_accounting',ddl,'retained_bytes=retained_bytes-OLD.record_bytes WHERE owner_key','retained_bytes=retained_bytes-0*OLD.record_bytes WHERE owner_key')
cut('primary_only','internal/m8/terminal.go','if role == "primary" && h.SessionHistoryEnabled','if role != "" && h.SessionHistoryEnabled',M)
cut('hub_persist_dom','internal/m8/terminal.go','if role == "primary" && h.SessionHistoryEnabled','if false && role == "primary" && h.SessionHistoryEnabled',B)
cut('append_field_dom',store,'event["provenance"], err = json.Marshal','event["removed_provenance"], err = json.Marshal',B,section='func enrichHistoryInput')
cut('renderer_attestation','view-service/renderer-canary-sse.html',"prov.trust_decision === 'trusted' && attested","prov.trust_decision === 'trusted'",B)
cut('oversize_gap_kind',history,'if errors.Is(oneErr, ErrSessionHistoryOversize) {','if false {')
cut('sidecar_no_gap',history,'if r.delivery != "" {\n\t\t\tcontinue\n\t\t}\n\t\tk := historyKey','if false {continue}\n\t\tk := historyKey',section='// A deletion batch')
cut('sidecar_4KiB_boundary',store,'if n > 4096 {','if false {')

cut('capture_target_snapshot','internal/m8/history_provenance.go','if len(target) > 0 {','if false {',M)
cut('capture_key_scope','internal/m8/history_provenance.go','c.keyID != key','false',M)
cut('capture_session_scope','internal/m8/history_provenance.go','c.sessionName != session','false',M)
cut('route_passes_captured_target','internal/m8/hub.go','env, deliveryID, "hub_received", "primary", true, c)','env, deliveryID, "hub_received", "primary", true)',M)

cut('cleanup_quantum',store,' LIMIT 500`',' LIMIT 501`',section='func deleteProvenanceBatch')
cut('eviction_quantum',store,'args = append(args, SessionHistoryBatch)','args = append(args, SessionHistoryBatch+1)',section='func historyEvictionRows')
original={c['file']:(root/c['file']).read_bytes() for c in cases};results=[]
def run(name,cmd):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 p=subprocess.run(cmd,cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=240)
 (out/(name+'.log')).write_bytes(p.stdout)
 return dict(name=name,rc=p.returncode,command=cmd,started_utc=start,ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),raw_sha256=hashlib.sha256(p.stdout).hexdigest()),p.stdout
for name,cmd in [('baseline_store',S),('baseline_m8',M),('baseline_browser',B)]:
 r,data=run(name,cmd);results.append(r);assert r['rc']==0,(name,data[-2000:])
try:
 for c in cases:
  p=root/c['file'];s=original[c['file']].decode();start=s.index(c['section']) if c['section'] else 0
  before=s[:start];section=s[start:];assert c['before'] in section,c['name']
  p.write_text(before+section.replace(c['before'],c['after'],1))
  if c['name']=='sidecar_4KiB_boundary':
   # Both redundant implementations of the single public size boundary must
   # be removed; deleting only one intentionally leaves the other enforcing it.
   (root/ddl).write_text(original[ddl].decode().replace(' AND record_bytes<=4096','',1))
  r,data=run(c['name'],c['command'])
  r.update(file=c['file'],before=c['before'],after=c['after'],compile_error=b'build failed' in data or b'[setup failed]' in data,panic=b'panic:' in data,business_failure=(b'--- FAIL:' in data or b'  FAIL ' in data))
  r['killed']=r['rc']!=0 and r['business_failure'] and not r['compile_error'] and not r['panic']
  if c['name']=='sidecar_4KiB_boundary':r['additional_edit']=dict(file=ddl,before=' AND record_bytes<=4096',after='')
  results.append(r)
  for file,raw in original.items():(root/file).write_bytes(raw)
  assert p.read_bytes()==original[c['file']]
  (out/'summary.json').write_text(json.dumps(dict(source='9313197f8ac3e2a585cd399935d5b313084b3b1e',results=results),indent=2)+'\n')
  print(c['name'],r['rc'],'KILLED' if r['killed'] else 'REVIEW',flush=True)
finally:
 for file,raw in original.items():(root/file).write_bytes(raw)
r,data=run('final_baseline_store',S);results.append(r);assert r['rc']==0
r,data=run('final_baseline_browser',B);results.append(r);assert r['rc']==0
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
(out/'summary.json').write_text(json.dumps(dict(source='9313197f8ac3e2a585cd399935d5b313084b3b1e',results=results,clean_after=True,valid=len(cases),killed=sum(r.get('killed',False) for r in results)),indent=2)+'\n')
