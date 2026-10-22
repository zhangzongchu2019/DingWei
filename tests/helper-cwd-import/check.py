"""Isolated executable-start withdrawal checks, not production acceptance."""
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[2]
HOOK='update_launcher/cwd_import.py'
CASE='test_cwd_import.CwdImportTests.'
MUTATIONS=[
 ('hook-admission',HOOK,'module = args[1]','return\n            module = args[1]',
  'test_cwd_fake_package_cannot_execute','CWD_PACKAGE_EXECUTED'),
 ('hook-isolation-json',HOOK,"[INTERPRETER, '-I', '-B', *options, '-c'","[INTERPRETER, '-B', *options, '-c'",
  'test_cwd_fake_json_cannot_execute','CWD_JSON_EXECUTED'),
 ('hook-isolation-sitecustomize',HOOK,"[INTERPRETER, '-I', '-B', *options, '-c'","[INTERPRETER, '-B', *options, '-c'",
  'test_cwd_fake_sitecustomize_cannot_execute','CWD_SITECUSTOMIZE_EXECUTED'),
 ('guard-child-isolation','update_launcher/guard.py',"[sys.executable, '-I', '-B', '-c'","[sys.executable, '-B', '-c'",
  'test_guard_child_excludes_cwd_standard_library_shadow','GUARD_CHILD_CWD_JSON_EXECUTED'),
 ('mcp-child-isolation','update_launcher/mcp_forwarder.py',"[sys.executable, '-I', '-B', '-c'","[sys.executable, '-B', '-c'",
  'test_mcp_forwarder_child_excludes_cwd_standard_library_shadow','MCP_CHILD_CWD_JSON_EXECUTED'),
 ('legacy-mcp-child-isolation','legacy_mcp_entry.py',"[sys.executable, '-I', '-B', '-c'","[sys.executable, '-B', '-c'",
  'test_legacy_mcp_entry_child_excludes_cwd_standard_library_shadow','LEGACY_MCP_CHILD_CWD_JSON_EXECUTED'),
 ('fail-closed',HOOK,'os._exit(78)','return',
  'test_exec_error_exits_instead_of_site_swallowing_exception','EXEC_FAILURE_FELL_THROUGH'),
 ('visible-failure',HOOK,"os.write(2, b'HELPER_CWD_IMPORT_ERROR startup_refused\\\\n')",'pass',
  'test_bad_root_missing_package_symlink_and_mode_fail_closed_visible','BOOTSTRAP_FAILURE_NOT_VISIBLE'),
 ('nonhelper-scope',HOOK,"if module != 'sessionhelper' and not module.startswith('sessionhelper.'):", 'if False:',
  'test_same_venv_nonhelper_script_c_and_module_unaffected','NONHELPER_BEHAVIOR_CHANGED'),
 ('isolated-idempotence',HOOK,"sys.flags.isolated or getattr(sys, '_dw_cwd_import_reexecuted', False)","getattr(sys, '_dw_cwd_import_reexecuted', False)",
  'test_already_isolated_and_internal_marker_are_idempotent','ISOLATED_HOOK_REEXECUTED'),
 ('root-mode',HOOK,'or (info.st_mode & 0o022 and not sticky)','or False',
  'test_bad_root_missing_package_symlink_and_mode_fail_closed_visible','BAD_BOOTSTRAP_FELL_THROUGH_mode'),
]


def main(output):
    out=Path(output).resolve();out.mkdir(parents=True,exist_ok=False)
    rows=[]
    with tempfile.TemporaryDirectory(prefix='cwd-withdraw-') as tmp:
        sandbox=Path(tmp);package=sandbox/'helper'
        shutil.copytree(ROOT/'tools/sessionhelper',package,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
        home=sandbox/'home';home.mkdir(mode=0o700)
        env={'HOME':str(home),'PATH':'/usr/bin:/bin','LANG':'C.UTF-8','PYTHONDONTWRITEBYTECODE':'1',
             'PYTHONPATH':str(package)+os.pathsep+str(package/'tests')}
        def run(label,case=None):
            command=[sys.executable,'-B','-m','unittest','-v',CASE+case if case else 'test_cwd_import']
            with (out/(label+'.stdout')).open('wb') as stdout,(out/(label+'.stderr')).open('wb') as stderr:
                rc=subprocess.run(command,cwd=sandbox,env=env,stdout=stdout,stderr=stderr,timeout=60).returncode
            text=(out/(label+'.stderr')).read_text()
            row=dict(label=label,rc=rc,command=command,utc=datetime.now(timezone.utc).isoformat())
            (out/(label+'.rc')).write_text(str(rc)+'\n');rows.append(row)
            return row,text
        row,_=run('baseline');assert row['rc']==0,'BASELINE_RED'
        for name,file,old,new,case,marker in MUTATIONS:
            path=package/file;original=path.read_text();assert original.count(old)==1,'TARGET_DRIFT_'+name
            try:
                path.write_text(original.replace(old,new,1));row,text=run(name+'-withdrawn',case)
                row['marker']=marker
                row['designated_red']=row['rc']!=0 and marker in text and 'AssertionError' in text and 'FAIL:' in text and 'ERROR:' not in text
                assert row['designated_red'],'WITHDRAWAL_NOT_DESIGNATED_RED_'+name
            finally:path.write_text(original)
            row,_=run(name+'-restored',case);assert row['rc']==0,'RESTORE_RED_'+name
        row,_=run('final');assert row['rc']==0,'FINAL_RED'
    (out/'RESULT.json').write_text(json.dumps(dict(scope='LOCAL_ISOLATED_NOT_ONLINE',mutations=len(MUTATIONS),rows=rows),indent=2)+'\n')
    print('CWD_IMPORT_WITHDRAWALS_PASS mutations='+str(len(MUTATIONS)))


if __name__=='__main__':main(sys.argv[1])
