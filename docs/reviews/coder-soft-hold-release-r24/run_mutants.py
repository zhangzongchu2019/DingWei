from pathlib import Path
import subprocess,json,time,hashlib
lab=Path(__file__).parent
wt=lab/'mutant'
env=json.loads((lab/'env.json').read_text())
for src,dst in [('ops-binding_test.go.fixture','cmd/workpulse/zz_quota_probe_test.go'),('durable-queue_test.go.fixture','internal/m8/zz_quota_probe_test.go'),('enforcement-contention_test.go.fixture','internal/nodeagent/zz_quota_probe_test.go')]:
 (wt/dst).write_bytes((lab/'probes'/src).read_bytes())
CASES=[
 ('generation','internal/nodeagent/softquota_release.go','s.HoldID != generation || ','','./internal/nodeagent','TestSoftHoldReleaseGenerationPersistentStoppedOnly'),
 ('persist','internal/nodeagent/softquota_release.go','if err := q.persistLocked(); err != nil {','if err := error(nil); err != nil {','./internal/nodeagent','TestSoftHoldReleaseGenerationPersistentStoppedOnly'),
 ('owner','cmd/workpulse/hosted_quota_routes.go','actor != d.owner','actor == ""','./cmd/workpulse','TestHostedQuotaRealIdentityOwnerOnlyAndOpsReadOnly'),
 ('two-destinations','internal/hosted/node_quota.go','[]string{"owner", "ops"}','[]string{"owner"}','./internal/hosted','TestSoftQuotaMonitorProjectionAndDurableTwoRecipientNotices'),
 ('owner-proof','internal/hosted/node_quota.go',"AND EXISTS (SELECT 1 FROM g4_audit WHERE target=$2 AND action='node.soft_quota.release_requested' AND detail->>'seat_id'=$1 AND detail->>'hold_id'=$3)","AND $3<>''",'./internal/hosted','TestSoftHoldOwnerIntentAndNodeProofBeforeSeparateRestart'),
 ('projection','internal/hosted/softquota_monitor.go','type QuotaSeat struct {','type QuotaSeat struct {\nWriterRoot string `json:"writer_root"`','./internal/hosted','TestSoftQuotaMonitorProjectionAndDurableTwoRecipientNotices'),
 ('ops-binding','cmd/workpulse/hosted_quota_routes.go','full == d.ops','full != ""','./cmd/workpulse','TestQQQuotaExactOpsBinding'),
 ('durable-queue','internal/m8/hosted_quota_notice.go','return h.Repo.EnqueueMessage(ctx, model.Message{','return h.Outbound.Enqueue(ctx, model.Message{','./internal/m8','TestQQQuotaMustPersistBeforeNoticeReceipt'),
]
# Optional group argument lets non-PG controls run while earlier gates finish.
selected=CASES
if len(__import__('sys').argv)>1:
 names=set(__import__('sys').argv[1:]);selected=[x for x in CASES if x[0] in names]
results=[]
for name,file,old,new,pkg,test in selected:
 path=wt/file;original=path.read_text();assert original.count(old)==1,(name,original.count(old))
 result={'case':name,'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=wt,text=True).strip(),'file':file,'test':test,'original_sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
 try:
  for phase in ['baseline','withdrawal','restored']:
   path.write_text(original.replace(old,new) if phase=='withdrawal' else original)
   args=['go','test','-race','-count=1','-v','-run','^'+test+'$',pkg]
   log=lab/'raw'/('control-'+name+'-'+phase+'.log');t=time.monotonic()
   with log.open('w') as f:p=subprocess.run(args,cwd=wt,env=env,stdout=f,stderr=subprocess.STDOUT)
   out=log.read_text();semantic_red='--- FAIL:' in out and '[build failed]' not in out
   result[phase]={'rc':p.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'assertion_red':semantic_red,'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest(),'source_file_sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
   expected=1 if phase=='withdrawal' else 0
   assert p.returncode==expected and (phase!='withdrawal' or semantic_red),(name,phase,p.returncode,out[-2000:])
 finally:path.write_text(original)
 results.append(result)
 (lab/('controls-'+name+'.json')).write_text(json.dumps(result,indent=2)+'\n')
 print(name,'0/1/0',flush=True)
(lab/'controls-last.json').write_text(json.dumps(results,indent=2)+'\n')
