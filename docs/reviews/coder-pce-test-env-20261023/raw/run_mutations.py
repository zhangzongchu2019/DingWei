from pathlib import Path
import datetime,hashlib,json,os,subprocess,time
root=Path('/data/ai-dev-data/pce-test-env-mutation')
out=Path('/data/ai-dev-data/pce-test-env-evidence/mutations');out.mkdir(exist_ok=True)
python='/tmp/coder-0232-locked-venv/bin/python3'
fixture='tools/sessionhelper/tests/pce_retention_crash_fixture.py'
guard='tools/sessionhelper/update_launcher/guard.py'
probe='/data/ai-dev-data/pce-test-env-evidence/missing_api_probe.py'
test='tools/sessionhelper/tests/test_pce_retention.py'
owned='/data/ai-dev-data/pce-test-env-evidence/owned_process_runner.py'
mutations=[
 ('M1-no-optional-api',fixture,"getattr(os,'pidfd_open',None)","os.pidfd_open",[python,probe,fixture]),
 ('M2-no-fallback-reap',fixture,"            elif child is not None:\n                reap_owned_child(child,child_starttime);child_reaped=True\n",'', [python,owned,python,'-B',test,'RetainTests.test_actual_owner_crash_without_python_pidfd_api']),
 ('M3-ignore-unowned',fixture,"    done,_=os.waitpid(pid,os.WNOHANG)\n    if done==pid:return\n", "    try:done,_=os.waitpid(pid,os.WNOHANG)\n    except ChildProcessError:return\n    if done==pid:return\n",[python,owned,python,'-B',test,'RetainTests.test_crash_fixture_refuses_to_signal_unowned_or_changed_pid']),
 ('M4-no-starttime',fixture,"    assert process_starttime(pid)==starttime,'OWNER_CRASH_CHILD_CHANGED'\n",'', [python,owned,python,'-B',test,'RetainTests.test_crash_fixture_refuses_to_signal_unowned_or_changed_pid']),
 ('M5-no-original-stop-proof',guard,"                if not pin or pin['state'] != 'stopped' or type(pin.get('stop')) is not dict:\n                    raise UpdateError('retain_stop')\n                if not self.valid_stop(pin):\n                    raise UpdateError('retain_stop')\n",'', [python,owned,python,'-B',test,'RetainTests.test_actual_owner_crash_does_not_reclaim_live_child','RetainTests.test_actual_owner_crash_without_python_pidfd_api'])
]
records=[]
for name,file,old,new,command in mutations:
    if (out/(name+'.json')).exists():
        record=json.loads((out/(name+'.json')).read_text());records.append(record);print(name,'recorded',flush=True);continue
    path=root/file;original=path.read_bytes();text=original.decode();assert text.count(old)==1,(name,text.count(old))
    record={'name':name,'file':file,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'original_sha256':hashlib.sha256(original).hexdigest(),'old':old,'new':new,'phases':[]}
    try:
        for phase in ('before','withdraw','restore'):
            path.write_bytes(text.replace(old,new).encode() if phase=='withdraw' else original)
            home=out/(name+'-'+phase+'-home');home.mkdir(exist_ok=True)
            env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','PYTHONPATH':str(root/'tools/sessionhelper'),'SH_UPDATE_LAUNCHER':str(root/'tools/sessionhelper/update_launcher/guard.py'),'LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1'}
            start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
            result=subprocess.run(command,cwd=root,env=env,capture_output=True,timeout=120)
            (out/(name+'-'+phase+'.stdout')).write_bytes(result.stdout);(out/(name+'-'+phase+'.stderr')).write_bytes(result.stderr)
            record['phases'].append({'phase':phase,'rc':result.returncode,'command':command,'environment':env,'utc':start,'elapsed_seconds':time.monotonic()-t,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()})
    finally:path.write_bytes(original)
    record['restored_sha256']=hashlib.sha256(path.read_bytes()).hexdigest()
    (out/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n');records.append(record)
    print(name,[p['rc'] for p in record['phases']],flush=True)
    assert [p['rc'] for p in record['phases']]==[0,1,0],name
(out/'SUMMARY.json').write_text(json.dumps(records,indent=2)+'\n')
