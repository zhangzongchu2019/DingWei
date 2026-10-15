#!/usr/bin/env python3
"""Isolated real-Docker guard matrix. Synthetic candidates; not Hub/helper E2E."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
import uuid

CORE = Path(__file__).resolve().parents[1]


class Blocked(RuntimeError):
    pass


def docker(*args, timeout=60):
    return subprocess.check_output(['docker', *args], stderr=subprocess.PIPE, timeout=timeout)


def wait_for(test, message, timeout=20):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = test()
        if value:
            return value
        time.sleep(.1)
    raise AssertionError(message)


def launch_root(root):
    (root / 'sessionhelper').mkdir(parents=True)
    (root / 'sessionhelper/__init__.py').write_text('')
    shutil.copyfile(CORE / 'update_launcher/launcher_main.py', root / 'sessionhelper/__main__.py')
    shutil.copyfile(CORE / 'update_launcher/guard.py', root / 'helper_update_guard.py')
    (root / 'factory/sessionhelper').mkdir(parents=True)
    (root / 'factory/sessionhelper/__init__.py').write_text('')
    (root / 'factory/sessionhelper/__main__.py').write_text(candidate('factory'))


def candidate(label, crash=False):
    # This is deliberately a deterministic test candidate, not a real Hub CLI.
    return ("import json,os,time\nfrom pathlib import Path\n"
            "p=Path(os.environ['SH_STATE_DIR'])\n"
            "(p/'observed.json').write_text(json.dumps({'label':" + repr(label) + ", 'pid':os.getpid()}))\n" +
            ("raise RuntimeError('synthetic candidate startup failure')\n" if crash else "while True: time.sleep(.05)\n"))


def matrix(image, evidence, keep=False):
    if not re.fullmatch(r'(?:sha256:|[^\s]+@sha256:)[0-9a-f]{64}', image):
        raise Blocked('image_digest_required')
    try:
        docker('info', '--format', '{{.OSType}}')
        details = json.loads(docker('image', 'inspect', image))[0]
    except (OSError, subprocess.SubprocessError, ValueError):
        raise Blocked('docker_or_pinned_image_unavailable') from None
    if details.get('Os') != 'linux':
        raise Blocked('linux_image_required')
    evidence = Path(evidence).resolve()
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    if any(evidence.iterdir()):
        raise Blocked('evidence_directory_must_be_empty')
    temp = Path(tempfile.mkdtemp(prefix='dw-guard-matrix-'))
    name = 'dw-guard-matrix-' + uuid.uuid4().hex[:12]
    mount, state = temp/'source', temp/'state'
    mount.mkdir();state.mkdir(mode=0o700)
    launch_root(mount)
    envfile=temp/'fixture.env';envfile.write_text('export SH_STATE_DIR=/state\nexport PYTHONPATH=/opt/dingwei/sessionhelper-src\n');envfile.chmod(0o600)
    checks=[]
    def record(label):
        checks.append(label);print('PASS '+label,flush=True)
    def execute(script, *args):
        return docker('exec', name, '/opt/dingwei/sessionhelper-venv/bin/python','-c',script,*args).decode().strip()
    def journal():
        path=state/'update/journal.json'
        return json.loads(path.read_text()) if path.exists() else {}
    def seen(label):
        try:return json.loads((state/'observed.json').read_text())['label']==label
        except (OSError,ValueError,KeyError):return False
    def arm(label, crash=False, expires=False):
        sha=hashlib.sha256(label.encode()).hexdigest()
        slot=state/'update/slots'/sha/'sessionhelper';slot.mkdir(parents=True)
        (slot/'__init__.py').write_text('');(slot/'__main__.py').write_text(candidate(label,crash))
        item=dict(job_id='synthetic-'+label,release_id='release-'+label,agent_id='synthetic-agent',owner_id='synthetic-owner',version='0.2.3.'+str(ord(label[0])),sha256=sha)
        execute("import sys,json;sys.path.insert(0,'/opt/dingwei/sessionhelper-src');from helper_update_guard import State;s=State(__import__('pathlib').Path('/state/update'),__import__('pathlib').Path('/opt/dingwei/sessionhelper-src/factory'));s.arm(json.loads(sys.argv[1]))",json.dumps(item))
        if expires:
            execute("import json,time;from pathlib import Path;p=Path('/state/update/journal.json');d=json.loads(p.read_text());d['pending']['deadline']=time.time()-1;p.write_text(json.dumps(d))")
        return item
    def restart():
        (state/'observed.json').unlink(missing_ok=True)
        docker('restart',name)
    def confirm(item):
        execute("import sys,json;sys.path.insert(0,'/opt/dingwei/sessionhelper-src');from helper_update_guard import State;from pathlib import Path;State(Path('/state/update'),Path('/opt/dingwei/sessionhelper-src/factory')).confirm(json.loads(sys.argv[1]))",json.dumps(item))
    try:
        docker('run','-d','--name',name,'--label','dw.test=helper-guard-matrix','--network','none','--restart','unless-stopped','--read-only','--tmpfs','/tmp',
               '--user',str(os.getuid())+':'+str(os.getgid()),
               '--mount','type=bind,source='+str(mount)+',target=/opt/dingwei/sessionhelper-src,readonly',
               '--mount','type=bind,source='+str(state)+',target=/state',
               '--mount','type=bind,source='+str(envfile)+',target=/run/secrets/sessionhelper_env,readonly',
               '--entrypoint','/usr/bin/tini',image,'--','bash','-c',
               'source /run/secrets/sessionhelper_env; exec /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper')
        wait_for(lambda:seen('factory'),'manual no-job factory failed')
        assert not journal().get('pending');record('manual no-job factory bootstrap')
        info=json.loads(docker('inspect',name))[0]
        assert info['HostConfig']['RestartPolicy']['Name']=='unless-stopped'
        assert next(m for m in info['Mounts'] if m['Destination']=='/opt/dingwei/sessionhelper-src')['RW'] is False
        execute("from pathlib import Path;p=Path('/opt/dingwei/sessionhelper-src/prohibited-write');\ntry:p.write_text('x')\nexcept OSError:print('readonly')\nelse:raise SystemExit(1)")
        record('actual readonly mount and unchanged tini/bash/env/python entry')
        marker=state/'persistent-marker';marker.write_text('synthetic')
        restart();wait_for(lambda:seen('factory'),'factory restart failed');assert marker.read_text()=='synthetic';record('state marker survives docker restart')
        a=arm('A');restart();wait_for(lambda:seen('A'),'A trial failed');confirm(a);assert journal()['current']['job_id']==a['job_id'];record('factory to A confirm')
        b=arm('B');restart();wait_for(lambda:seen('B'),'B trial blocked by A');assert journal()['pending']['starts']==1
        restart();wait_for(lambda:seen('B'),'B second trial failed');assert journal()['pending']['starts']==2;record('actual supervisor restart preserves unspent B trial')
        # Real runtime-side terminal rollback must stop/reap candidate and trigger Docker.
        execute("import sys,json;sys.path.insert(0,'/opt/dingwei/sessionhelper-src');from helper_update_guard import State;from pathlib import Path;State(Path('/state/update'),Path('/opt/dingwei/sessionhelper-src/factory')).fail(json.loads(sys.argv[1]),'synthetic_rejection')",json.dumps(b))
        wait_for(lambda:seen('A'),'B rollback did not restart A',60);assert journal()['terminal'][b['job_id']]['stage']=='rolled_back';record('B rejection automatically restarts confirmed A')
        c=arm('C');restart();wait_for(lambda:seen('C'),'new authorized C failed');confirm(c)
        assert journal()['current']['job_id']==c['job_id'] and journal()['previous']['job_id']==a['job_id'];record('fresh C succeeds after failed B')
        d=arm('D',crash=True);restart()
        wait_for(lambda:journal().get('terminal',{}).get(d['job_id'],{}).get('stage')=='rolled_back','startup crash not terminal',60)
        wait_for(lambda:seen('C'),'startup crash did not roll back C',60);record('unimportable/startup-crash slot rollback')
        e=arm('E');started=time.monotonic();restart();wait_for(lambda:seen('E'),'health trial did not start')
        wait_for(lambda:seen('C'),'120-second health expiry did not roll back',150)
        assert time.monotonic()-started>=110
        assert journal()['terminal'][e['job_id']]['stage']=='rolled_back';record('real 120-second unconfirmed health deadline rolls back C')
        # Separate consecutive sequence: retained A2 must not prevent B2 or C2.
        for label in ('A2','B2'):
            item=arm(label);restart();wait_for(lambda:seen(label),label+' trial failed')
            restart();wait_for(lambda:seen(label),label+' second trial failed')
            assert journal()['pending']['starts']==2
            confirm(item)
        c2=arm('C2');restart();wait_for(lambda:seen('C2'),'C2 blocked by confirmed B2')
        restart();wait_for(lambda:seen('C2'),'C2 second trial failed')
        restart();wait_for(lambda:seen('B2'),'exhausted C2 did not select B2')
        assert journal()['terminal'][c2['job_id']]['stage']=='rolled_back'
        assert journal()['current']['job_id']=='synthetic-B2'
        record('consecutive A2 to B2 to C2; actual restarts; C2 budget rollback to B2')
        # No terminal job may rearm even across actual container restart.
        restart();wait_for(lambda:seen('B2'),'terminal restart failed');assert journal()['pending'] is None;record('terminal anti-loop and confirmed restart')
        result=dict(rc=0,checks=checks,image_id=details['Id'],source_guard_sha256=hashlib.sha256((CORE/'update_launcher/guard.py').read_bytes()).hexdigest(),scope='real Docker guard with synthetic candidates; NOT authenticated Hub/helper E2E')
        (evidence/'RESULT.json').write_text(json.dumps(result,indent=2)+'\n')
        return 0
    finally:
        try:(evidence/'docker.log').write_bytes(subprocess.run(['docker','logs',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=60).stdout)
        except (OSError,subprocess.SubprocessError):pass
        if not (evidence/'RESULT.json').exists():
            (evidence/'RESULT.json').write_text(json.dumps({'rc':1,'checks_completed':checks,'scope':'real Docker guard fixture, incomplete'},indent=2)+'\n')
        if not keep:
            try:docker('rm','-f',name)
            finally:shutil.rmtree(temp)
        else:
            print('KEPT isolated fixture '+name+' at '+str(temp),file=sys.stderr)


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--image',required=True);p.add_argument('--evidence',required=True);p.add_argument('--keep',action='store_true')
    args=p.parse_args()
    try:return matrix(args.image,args.evidence,args.keep)
    except Blocked as exc:print('BLOCK '+str(exc),file=sys.stderr);return 77
    except (AssertionError,OSError,subprocess.SubprocessError,ValueError,KeyError) as exc:
        print('FAIL '+type(exc).__name__,file=sys.stderr);return 1

if __name__=='__main__':raise SystemExit(main())
