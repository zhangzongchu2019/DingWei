#!/usr/bin/env python3
"""Compile the same reader against exact old/new libraries; start old Hub locally."""
import argparse
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.request

ROOT=Path(__file__).resolve().parents[2]
BASE='ef439b82b1550e9ca5c179edf61be78666c5fff0'
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--output',required=True);args=parser.parse_args()
    output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=True)
    old=Path(tempfile.mkdtemp(prefix='r32-old-'))
    def run(argv,cwd=ROOT):
        proc=subprocess.run(argv,cwd=cwd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        if proc.returncode:raise RuntimeError('compat command failed: '+argv[0]+'\n'+proc.stderr[-3000:])
        return proc.stdout
    run(['git','worktree','add','--detach',str(old),BASE])
    fixture=Path('tests/r32-task-flow/compat-probe/main.go');(old/fixture).parent.mkdir(parents=True,exist_ok=True);(old/fixture).write_bytes((ROOT/fixture).read_bytes())
    new_probe=output/'new-probe';old_probe=output/'old-probe';hub=output/'old-hub'
    run(['go','build','-o',str(new_probe),'./tests/r32-task-flow/compat-probe'])
    run(['go','build','-o',str(old_probe),'./tests/r32-task-flow/compat-probe'],cwd=old)
    run(['go','build','-o',str(hub),'./cmd/workpulse'],cwd=old)
    readings=[]
    def probe(binary,mode,db):
        value=json.loads(run([str(binary),mode,str(db)]));readings.append({'binary':binary.name,'operation':mode,'db':db.name,'result':value});return value
    upgraded=output/'from-r31.sqlite';probe(old_probe,'seed',upgraded)
    for _ in range(2):assert probe(new_probe,'inspect',upgraded)['r32_ledger_rows']==1
    created=output/'from-r32.sqlite';assert probe(new_probe,'seed',created)['r32_ledger_rows']==1
    for _ in range(2):assert probe(old_probe,'inspect',created)['r32_ledger_rows']==1
    rollback=output/'rollback.sqlite';probe(old_probe,'seed',rollback)
    with sqlite3.connect(rollback) as connection:
        connection.execute("CREATE TRIGGER reject_r32_ledger BEFORE INSERT ON schema_migrations WHEN NEW.version='0055_console_task_flow_r32.sql' BEGIN SELECT RAISE(ABORT,'fixture ledger unavailable'); END")
    rejected=subprocess.run([str(new_probe),'inspect',str(rollback)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    assert rejected.returncode!=0,'MIGRATION_LEDGER_FAILURE_ACCEPTED'
    (output/'rollback.stderr').write_text(rejected.stderr)
    with sqlite3.connect(rollback) as connection:
        columns={row[1] for row in connection.execute('PRAGMA table_info(console_spec_task)')}
        assert 'group_code' not in columns,'MIGRATION_LEFT_PARTIAL_COLUMNS'
        assert not connection.execute("SELECT 1 FROM sqlite_master WHERE name='console_task_candidate'").fetchone(),'MIGRATION_LEFT_PARTIAL_TABLES'
        assert connection.execute("SELECT count(*) FROM console_spec_task WHERE id='compat-task'").fetchone()[0]==1
        connection.execute('DROP TRIGGER reject_r32_ledger')
    readings.append({'ledger_failure_rc':rejected.returncode,'rollback_columns_and_tables_absent':True,'old_task_preserved':True})
    assert probe(new_probe,'inspect',rollback)['r32_ledger_rows']==1
    helper_script="""import json
from types import SimpleNamespace
from sessionhelper.app import SessionHelper
helper=object.__new__(SessionHelper)
helper.cfg=SimpleNamespace(mode='driver',device_id_v1=False,device_id='')
helper.book=SimpleNamespace(key_id='fixture',self_addr='fixture#fixture')
helper.upload_configured=lambda:False
meta=helper.session_capabilities_envelope()['meta']
assert meta['helper_version']=='0.2.3.4'
assert meta['delivery_ack_v1'] is True
assert 'task_flow_v1' not in meta
print(json.dumps({'old_helper_version':meta['helper_version'],'delivery_ack_v1':True,'task_flow_v1':False}))
"""
    helper_env=dict(os.environ,PYTHONPATH=str(old/'tools/sessionhelper'),SH_UPDATE_LAUNCHER='')
    helper=subprocess.run(['/tmp/coder-0232-locked-venv/bin/python3','-c',helper_script],cwd=old,env=helper_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    assert helper.returncode==0,helper.stderr
    readings.append(json.loads(helper.stdout))
    with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    private=output/'hub-state';private.mkdir(mode=0o700,exist_ok=True)
    # Start with a clean environment, private cwd, fake transport and loopback
    # ports. Inherit no account state, PG DSN, external URLs or credentials.
    env={'PATH':os.environ['PATH'],'HOME':str(private),'WP_DB_PATH':str(created),'WP_DATA_DIR':str(private),'WP_ADDR':'127.0.0.1:'+str(port),'WP_INTERNAL_ADDR':'127.0.0.1:0','WP_CONSOLE_AUTH_ENABLED':'false','WP_HUB_ID':'r32-compat-local','FEISHU_TRANSPORT':'fake','WP_SECRET_KEY':'synthetic-local-test-only','WP_ADMIN_INIT_PASSWORD':'synthetic-local-test-only'}
    with (output/'old-hub.log').open('w') as log:
        proc=subprocess.Popen([str(hub)],cwd=private,env=env,stdout=log,stderr=log)
        try:
            deadline=time.monotonic()+20;body=''
            while time.monotonic()<deadline:
                if proc.poll() is not None:raise RuntimeError('old Hub exited during isolated startup')
                try:
                    with urllib.request.urlopen('http://127.0.0.1:'+str(port)+'/api/specs/shared/compat-spec',timeout=1) as response:body=response.read().decode()
                    if 'preserved-content' in body and 'preserved-dod' in body:break
                except OSError:pass
                time.sleep(.05)
            else:raise RuntimeError('old Hub did not expose preserved task/spec data')
            (output/'old-hub-read.json').write_text(body)
            readings.append({'old_hub_startup':True,'old_hub_http_read':True,'loopback_only':True,'transport':'fake'})
        finally:
            proc.terminate()
            try:proc.wait(timeout=10)
            except subprocess.TimeoutExpired:proc.kill();proc.wait()
    (output/'COMPATIBILITY.json').write_text(json.dumps({'baseline':BASE,'probe_fixture_shared':True,'old_worktree':str(old),'readings':readings},indent=2)+'\n')
    print('R32_COMPATIBILITY_OK scenarios='+str(len(readings)))
if __name__=='__main__':main()
