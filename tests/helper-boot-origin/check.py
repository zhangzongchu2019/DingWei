"""Isolated boot-origin withdrawal checks, not production acceptance."""
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[2]
MODULE='sessionhelper/boot_origin.py'
CASE='test_boot_origin.BootOriginTests.'
MUTATIONS=[
 ('pid',MODULE,'"pid": pid,','"pid": pid + 1,','test_actual_pid_starttime_and_resolved_package_origin','BOOT_PID_MISMATCH'),
 ('starttime',MODULE,'"starttime": _starttime(pid),','"starttime": 1,','test_actual_pid_starttime_and_resolved_package_origin','BOOT_STARTTIME_MISMATCH'),
 ('source',MODULE,'str(Path(sessionhelper.__file__).resolve(strict=True))','str(sessionhelper.__file__)','test_source_symlink_resolves','BOOT_SOURCE_NOT_RESOLVED'),
 ('isolated',MODULE,'int(sys.flags.isolated)','0','test_isolation_flag_matches_real_interpreters','BOOT_ISOLATION_MISMATCH'),
 ('marker',MODULE,'"boot_marker": secrets.token_hex(16)','"boot_marker": ""','test_actual_pid_starttime_and_resolved_package_origin','BOOT_MARKER_MISSING'),
 ('stderr',MODULE,'    _stderr(line)\n','    pass\n','test_actual_pid_starttime_and_resolved_package_origin','BOOT_STDERR_MISMATCH'),
 ('once',MODULE,'if _ATTEMPTED_PID == pid:','if False:','test_once_per_process_not_per_call','BOOT_REPEATED'),
 ('write-warning',MODULE,'_stderr("HELPER_BOOT_ORIGIN_WARNING record_write_failed")','pass','test_failure_warns_sanitized_and_remains_nonblocking','BOOT_FAILURE_SILENT'),
 ('atomic',MODULE,'os.replace(temporary, RECORD_NAME, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)','os.unlink(RECORD_NAME, dir_fd=directory_fd)\n        os.replace(temporary, RECORD_NAME, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)','test_replace_failure_preserves_old_and_cleans_temporary','BOOT_NOT_ATOMIC'),
 ('directory-mode',MODULE,'info.st_uid != os.geteuid() or info.st_mode & 0o022','info.st_uid != os.geteuid()','test_unprotected_or_symlink_state_directory_refused','BOOT_UNSAFE_DIRECTORY_WRITTEN'),
 ('directory-symlink',MODULE,'os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW','os.O_RDONLY | os.O_DIRECTORY','test_unprotected_or_symlink_state_directory_refused','BOOT_UNSAFE_DIRECTORY_WRITTEN'),
 ('app-wiring','sessionhelper/app.py','    record_boot_origin(cfg.state_dir)\n','','test_application_wiring_and_write_failure_do_not_block_run','BOOT_APP_UNWIRED'),
]



def main(output):
    out=Path(output).resolve();out.mkdir(parents=True,exist_ok=False)
    rows=[]
    with tempfile.TemporaryDirectory(prefix='boot-origin-withdraw-') as tmp:
        sandbox=Path(tmp);package=sandbox/'helper'
        shutil.copytree(ROOT/'tools/sessionhelper',package,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
        home=sandbox/'home';home.mkdir(mode=0o700)
        env={'HOME':str(home),'PATH':'/usr/bin:/bin','LANG':'C.UTF-8','PYTHONDONTWRITEBYTECODE':'1',
             'PYTHONPATH':str(package)+os.pathsep+str(package/'tests')}
        def run(label,case=None):
            command=[sys.executable,'-B','-m','unittest','-v',CASE+case if case else 'test_boot_origin']
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
    print('BOOT_ORIGIN_WITHDRAWALS_PASS mutations='+str(len(MUTATIONS)))


if __name__=='__main__':main(sys.argv[1])
