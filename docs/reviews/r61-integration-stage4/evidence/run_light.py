from pathlib import Path
import subprocess,json,datetime,hashlib,os
A=Path(__file__).parent;W=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text());P=E['DW_DESKTOP_INTEGRATION_PYTHON'];old=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r61-n1c-review-20261118/r5-recheck');rows=[]
steps=[('go-build',['go','build','./...'],{}),('fifo-timeout',[P,'-B',str(old/'probe_fifo_interleaving.py')],{'R3_ERROR_KIND':'timeout'}),('fifo-rpc',[P,'-B',str(old/'probe_fifo_interleaving.py')],{'R3_ERROR_KIND':'rpc'}),('blocker-fairness',[P,'-B',str(old/'probe_blocker_head.py')],{}),('blocker-recovery-cross',[P,'-B',str(old/'probe_blocker_recovery_gate.py')],{}),('r6-and-AA010',[P,'-B',str(A/'run_isolated.py'),'test_driver_failure_faces.HubDeliveryPathTest.test_handle_delivery_initialize_auth_rejection_is_terminal','test_sub_agent_helper.StoreTests.test_aa010_feature_off_drains_without_native_or_capability'],{}),('helper-two-profiles',[P,'scripts/run-helper-build-tests.py'],{})]
(A/'runner.json').write_text(json.dumps({'pid':os.getpid(),'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}))
for name,cmd,extra in steps:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=W).strip();r={'name':name,'source':E['SOURCE_COMMIT'],'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with(A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=W,env=E|extra,stdout=f,stderr=subprocess.STDOUT).returncode
 r.update(ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),sha256=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest());rows.append(r);(A/'light-results.json').write_text(json.dumps(rows,indent=2));print('END',name,r['rc'],flush=True)
 if r['rc']:raise SystemExit(2)
