#!/usr/bin/env python3
"""R8 additive migration rollback and exact 0.2.3.5 helper compatibility.
Run the existing R9 compatibility producer first; it supplies old/new probes and
an actual r31.1 Hub process reading a library upgraded by this source (0056 too).
Never upload the local binaries or databases as evidence.
"""
import argparse,json,os,sqlite3,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OLD='9c79f269b2ff8f4c3fef1e38bc2c59b14b07f04b'
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--base-output',required=True);parser.add_argument('--output',required=True);args=parser.parse_args()
    base=Path(args.base_output).resolve();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
    def run(cmd,cwd=ROOT,env=None):
        r=subprocess.run(cmd,cwd=cwd,env=env,capture_output=True,text=True)
        if r.returncode:raise RuntimeError('compatibility producer failed: '+r.stderr[-2000:])
        return r.stdout
    db=out/'rollback-0056.sqlite';run([str(base/'old-probe'),'seed',str(db)])
    with sqlite3.connect(db) as c:c.execute("CREATE TRIGGER reject_r8_ledger BEFORE INSERT ON schema_migrations WHEN NEW.version='0056_console_spec_agent_review_r32.sql' BEGIN SELECT RAISE(ABORT,'fixture r8 ledger unavailable'); END")
    failed=subprocess.run([str(base/'new-probe'),'inspect',str(db)],capture_output=True,text=True)
    (out/'rollback-0056.stdout').write_text(failed.stdout);(out/'rollback-0056.stderr').write_text(failed.stderr);(out/'rollback-0056.rc').write_text(str(failed.returncode)+'\n')
    assert failed.returncode!=0,'R8_MIGRATION_LEDGER_FAILURE_ACCEPTED'
    with sqlite3.connect(db) as c:
        assert c.execute("SELECT count(*) FROM schema_migrations WHERE version='0056_console_spec_agent_review_r32.sql'").fetchone()[0]==0
        assert c.execute("SELECT count(*) FROM sqlite_master WHERE name LIKE 'console_spec_agent_%'").fetchone()[0]==0,'R8_MIGRATION_PARTIAL_OBJECTS'
        assert c.execute("SELECT count(*) FROM console_spec_task WHERE id='compat-task'").fetchone()[0]==1
        c.execute('DROP TRIGGER reject_r8_ledger')
    upgraded=json.loads(run([str(base/'new-probe'),'inspect',str(db)]));old_read=json.loads(run([str(base/'old-probe'),'inspect',str(db)]))
    with sqlite3.connect(db) as c:
        assert c.execute("SELECT count(*) FROM schema_migrations WHERE version='0056_console_spec_agent_review_r32.sql'").fetchone()[0]==1
        assert c.execute("SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'console_spec_agent_%'").fetchone()[0]==4
    legacy=Path(tempfile.mkdtemp(prefix='r8-helper-0235-'));run(['git','worktree','add','--detach',str(legacy),OLD])
    script="""import json
from types import SimpleNamespace
from sessionhelper.app import SessionHelper
from sessionhelper import __version__,mcp_shim
h=object.__new__(SessionHelper)
h.cfg=SimpleNamespace(mode='driver',device_id_v1=False,device_id='')
h.book=SimpleNamespace(key_id='fixture',self_addr='fixture#fixture')
h.upload_configured=lambda:False
h.enroll_credential='fixture-only'
meta=h.session_capabilities_envelope()['meta']
print(json.dumps({'version':__version__,'delivery_ack_v1':meta['delivery_ack_v1'],'spec_review_v1':meta.get('spec_review_v1',False),'new_tools':sorted(k for k in mcp_shim._tools if k.startswith('dw_spec_review'))}))
"""
    python='/tmp/coder-0232-locked-venv/bin/python'
    def helper(path):return json.loads(run([python,'-c',script],cwd=path,env=dict(os.environ,PYTHONPATH=str(path/'tools/sessionhelper'),SH_UPDATE_LAUNCHER='')))
    old=helper(legacy);new=helper(ROOT)
    assert old=={'version':'0.2.3.5','delivery_ack_v1':True,'spec_review_v1':False,'new_tools':[]},'R8_OLD_HELPER_CAPABILITY_CHANGED'
    assert new['version']=='0.2.3.6' and new['spec_review_v1'] is True and new['new_tools']==['dw_spec_review_reply','dw_spec_review_requests']
    result={'ledger_0056_failure_rc':failed.returncode,'rollback_no_partial_objects':True,'old_task_preserved':True,'new_read':upgraded,'r31_1_old_read_after_0056':old_read,'old_helper_anchor':OLD,'old_helper':old,'new_helper':new,'scope':'local isolated old/new libraries, actual r31.1 Hub read in base producer, no live helper upgrade or deployment'}
    (out/'COMPATIBILITY.json').write_text(json.dumps(result,indent=2)+'\n');print('R8_COMPATIBILITY_OK')
if __name__=='__main__':main()
