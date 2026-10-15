import os,pathlib,shutil,subprocess,tempfile,json
repo=pathlib.Path.cwd(); core=repo/'tools/sessionhelper';py='/data/ai-dev-data/helper-deps-lock-venv/bin/python'
cases=[('R1','update_launcher/guard.py','test_external_rollback_must_stop_failed_candidate'),('R2','update_launcher/guard.py','test_supervisor_restart_should_preserve_unspent_trial'),('R3','package_release.py','test_ignored_forbidden_file_must_reject'),('R4','bootstrap.py','test_plain_preflight_must_not_create_state'),('R5','bootstrap.py','test_wrong_container_entry_must_be_rejected'),('R5-shared','bootstrap.py','test_shared_source_mount_is_rejected_before_any_probe')]
results=[]
for label,relative,test in cases:
 with tempfile.TemporaryDirectory(prefix='core-withdraw-',dir='/tmp') as td:
  root=pathlib.Path(td); copied=root/'tools/sessionhelper'
  shutil.copytree(core,copied,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
  original=subprocess.check_output(['git','show','cfb2bdf8:tools/sessionhelper/'+relative],cwd=repo)
  (copied/relative).write_bytes(original)
  env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_'))}
  env['PYTHONPATH']=str(copied)+':'+str(copied/'tests')
  path='/tmp/coder-repair-withdraw-'+label+'.log'
  with open(path,'w') as out:r=subprocess.run([py,'-B','-m','unittest','-v','test_update_core_review_regressions.ReviewRegressionTests.'+test],cwd=root,env=env,stdout=out,stderr=subprocess.STDOUT)
  results.append(dict(case=label,rc=r.returncode,log=path));print(label,r.returncode,flush=True)
pathlib.Path('/tmp/coder-repair-withdraw.json').write_text(json.dumps(results,indent=2)+'\n')
assert all(x['rc']==1 for x in results)
