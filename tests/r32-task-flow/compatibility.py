#!/usr/bin/env python3
"""Compile the same reader against exact old/new libraries; start old Hub locally."""
import argparse
import json
import os
from pathlib import Path
import socket
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
