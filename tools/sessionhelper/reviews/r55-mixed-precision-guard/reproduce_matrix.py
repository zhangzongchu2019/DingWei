"""r54 Git源码与当前候选checkout，临时库/Go overlay；不启动真实服务。"""
import argparse
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
import hashlib
import sqlite3
import sys
import tarfile
import tempfile
from unittest.mock import patch
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[4]
BASE = '2026-11-08T16:00:00.750000Z'
T = datetime.fromisoformat(BASE.replace('Z', '+00:00')).timestamp()


def stamp(value):
    return datetime.fromtimestamp(value, timezone.utc).isoformat(timespec='microseconds').replace('+00:00', 'Z')


def worker(source, output, version):
    sys.path.insert(0, str(Path(source) / 'tools/sessionhelper'))
    from sessionhelper.inbox import DeliveryInbox
    from sessionhelper.inbox_ack import utc_stamp
    if version=='r55' and os.environ.get('R55_REMOVE_GUARD')=='1':
        from sessionhelper.inbox_lifecycle import DeliveryLifecycle
        DeliveryLifecycle._terminal_precedes_injected_ack=lambda *args:False
    cases = []
    names = ['same_clock', 'helper_minus_2s', 'wall_clock_step_back', 'stale_timestamp_argument'] if version == 'r54' else ['native_seconds_floor', 'native_too_old', 'native_milliseconds_rejected', 'live_stale_timestamp_argument', 'mixed_live_precise_native_floor']
    for n, name in enumerate(names):
        with tempfile.TemporaryDirectory(prefix='ack-clock-case-') as tmp:
            inbox = DeliveryInbox(str(Path(tmp)/'inbox.sqlite'), session_name='seat')
            did = 'dw-dlv_' + str(n).zfill(32)
            env = {'id':'synthetic-source', 'body':'SYNTHETIC_ONLY', 'meta':{'delivery_id':did,'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}}
            offset = -2 if name == 'helper_minus_2s' else 0
            with patch('time.time', return_value=T+.15+offset):
                inbox.accept(did, env); inbox.claim_next()
            with patch('time.time', return_value=T+.18+offset):
                assert inbox.begin_dispatch(did)
            rejected = False
            admission = T+.20+offset
            if name == 'wall_clock_step_back': admission = T-1.8
            if name in ('stale_timestamp_argument','live_stale_timestamp_argument'): admission = T-90
            if version == 'r54' or name == 'live_stale_timestamp_argument':
                now = admission if name == 'wall_clock_step_back' else T+.20+offset
                with patch('time.time', return_value=now):
                    assert inbox.record_admitted_before_deadline(did,'synthetic-turn',60,admission)
                with patch('time.time', return_value=T+4):
                    inbox.record_completed(did,'synthetic-turn','completed',T+4)
            else:
                if name == 'mixed_live_precise_native_floor':
                    with patch('time.time',return_value=T+.20):
                        assert inbox.record_admitted_before_deadline(did,'synthetic-turn',60,T+.20)
                admission = int(T+.20)
                completed = int(T+4)
                if name == 'mixed_live_precise_native_floor': completed=admission
                if name == 'native_too_old': admission -= 2
                if name == 'native_milliseconds_rejected':
                    admission *= 1000; completed *= 1000
                row = inbox.lifecycle(did)
                expected = {key:row[key] for key in ('ack_generation','dispatch_intent_at','request_id')}
                try:
                    with patch('time.time', return_value=T+4.1):
                        if name == 'mixed_live_precise_native_floor':
                            from sessionhelper.driver_adapter import DriverAdapter
                            from sessionhelper.contract import SessionEvent
                            event=SessionEvent(kind='state_change',session_id='synthetic-thread',delivery_id=did,turn_id='synthetic-turn',data={'change':'turn_completed','status':'completed','raw_method':'reconcile/native','admitted_at':admission,'processed_at':completed,'native_evidence':{'client_id':did,'turn_id':'synthetic-turn','thread_id':'synthetic-thread','context':expected}})
                            with patch('sessionhelper.driver_adapter.observe') as progress:
                                DriverAdapter._record_event_lifecycle(SimpleNamespace(delivery_lifecycle=inbox,_which='codex',delivery_progress=None),event)
                            assert inbox.lifecycle(did)['state']=='admitted','MIXED_PRECISION_MUST_REMAIN_PENDING'
                            assert inbox.lifecycle(did)['error']=='native_terminal_precedes_acked_injected'
                            assert inbox.lifecycle(did)['processed_at'] is None
                            assert not progress.called,'PENDING_TELEMETRY_MARKED_TERMINAL'
                        else:
                            inbox.record_native_terminal(did,'synthetic-turn',admission,completed,'completed',expected_context=expected)
                except ValueError:
                    rejected = True
                assert rejected == (name in ('native_too_old','native_milliseconds_rejected'))
            reopened = DeliveryInbox(str(Path(tmp)/'inbox.sqlite'), session_name='seat')
            with reopened._lock, reopened._connect() as db:
                wire = [json.loads(r[0]) for r in db.execute('SELECT payload_json FROM delivery_ack_outbox ORDER BY sequence')]
            injected = next((a['occurred_at'] for a in wire if a['state']=='injected'),None)
            before = injected is not None and datetime.fromisoformat(injected.replace('Z','+00:00')).timestamp() < T
            assert before == (name in ('helper_minus_2s','wall_clock_step_back','stale_timestamp_argument','native_seconds_floor','live_stale_timestamp_argument'))
            cases.append({'name':version+'/'+name,'hub_created_at':BASE,'hub_queued_at':stamp(T+.175),'wire':wire,'helper_rejected':rejected,'expected_state':'admission_unknown' if rejected else 'injected' if name=='mixed_live_precise_native_floor' else 'processed','expected_reject_phase':'','injected_before_created':before,'helper_lifecycle_after_restart':dict(reopened.lifecycle(did))})
    precision = {'input':T+.0000003,'wire':utc_stamp(T+.0000003)}
    precision['absolute_roundtrip_error_seconds'] = abs(datetime.fromisoformat(precision['wire'].replace('Z','+00:00')).timestamp()-precision['input'])
    assert precision['absolute_roundtrip_error_seconds'] < .000001
    Path(output).write_text(json.dumps({'version':version,'cases':cases,'utc_stamp_precision':precision},indent=2)+'\n')


def main():
    p=argparse.ArgumentParser();p.add_argument('--output');p.add_argument('--worker');p.add_argument('--source');p.add_argument('--version');p.add_argument('--remove-guard',action='store_true');args=p.parse_args()
    if args.worker:
        worker(args.source,args.worker,args.version);return
    output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=False)
    clean={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_')) and k not in ('CODEX_HOME',)}
    with tempfile.TemporaryDirectory(prefix='ack-clock-repro-') as tmp:
        clean.update(HOME=tmp,CODEX_HOME=tmp+'/codex',PYTHONDONTWRITEBYTECODE='1')
        if args.remove_guard:clean['R55_REMOVE_GUARD']='1'
        original_home=os.environ.get('HOME','')
        clean.setdefault('GOCACHE',original_home+'/.cache/go-build');clean.setdefault('GOMODCACHE',original_home+'/go/pkg/mod')
        all_cases=[];refs={}
        for label,ref in [('r54','6e2c8364'),('r55','7b123ca4')]:
            refs[label]=subprocess.check_output(['git','rev-parse',ref],cwd=ROOT,text=True).strip()
            source=Path(tmp)/label;source.mkdir()
            if label=='r54':
                archive=subprocess.check_output(['git','archive',ref,'tools/sessionhelper/sessionhelper'],cwd=ROOT)
                with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(source,filter='data')
            else:
                # Candidate source is this checkout; no untracked state/config is copied.
                for name in subprocess.check_output(['git','ls-files','tools/sessionhelper/sessionhelper'],cwd=ROOT,text=True).splitlines():
                    if not name.endswith('.py'):continue
                    destination=source/name;destination.parent.mkdir(parents=True,exist_ok=True)
                    shutil.copyfile(ROOT/name,destination)
            target=output/(label+'-helper.json')
            subprocess.run([sys.executable,'-B',str(Path(__file__).resolve()),'--worker',str(target),'--source',str(source),'--version',label],env=clean,cwd=ROOT,check=True,timeout=45)
            all_cases.extend(json.loads(target.read_text())['cases'])
        candidate_hashes={name:hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in ['tools/sessionhelper/sessionhelper/inbox_lifecycle.py','tools/sessionhelper/sessionhelper/driver_adapter.py']}
        matrix=output/'matrix.json';matrix.write_text(json.dumps({'refs':refs,'candidate_source_sha256':candidate_hashes,'cases':all_cases},indent=2)+'\n')
        baseline=json.loads(Path(__file__).with_name('baseline-matrix.json').read_text())
        comparable=[]
        for case in all_cases:
            if case['name'].endswith('mixed_live_precise_native_floor'):continue
            old=next(c for c in baseline['cases'] if c['name']==case['name'])
            for key in ['expected_state','expected_reject_phase','helper_rejected','injected_before_created']:assert case[key]==old[key],(case['name'],key)
            for key in ['state','admitted_at','processed_at','turn_outcome','error']:assert case['helper_lifecycle_after_restart'][key]==old['helper_lifecycle_after_restart'][key],(case['name'],key)
            def stable_wire(c):
                return [{key:value for key,value in a.items() if not (a['state']=='admission_unknown' and key=='occurred_at')} for a in c['wire']]
            assert stable_wire(case)==stable_wire(old),case['name']
            comparable.append(case['name'])
        (output/'CONTROL-COMPARISON.json').write_text(json.dumps({'baseline':'24b4b644f6288a620058fe11023bb397153a7dfa','unchanged_controls':comparable,'count':len(comparable)},indent=2)+'\n')
        with sqlite3.connect(':memory:') as db:
            examples=[]
            for created,injected in [('2026-11-08T16:00:00Z','2026-11-08T16:00:00.100000Z'),('2026-11-08T16:00:00.100000Z','2026-11-08T16:00:00Z')]:
                lexical=bool(db.execute('SELECT ? < ?',(injected,created)).fetchone()[0])
                numeric=datetime.fromisoformat(injected.replace('Z','+00:00')) < datetime.fromisoformat(created.replace('Z','+00:00'))
                assert lexical != numeric
                examples.append({'created':created,'injected':injected,'sqlite_text_less':lexical,'actual_before':numeric})
            (output/'stats-order-caution.json').write_text(json.dumps(examples,indent=2)+'\n')
        clean['ACK_CLOCK_MATRIX']=str(matrix)
        overlay=Path(tmp)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/store/ack_clock_probe_test.go'):str(Path(__file__).with_name('hub_probe_test.go.txt').resolve())}}))
        command=['go','test','-vet=off','-overlay='+str(overlay),'./internal/store','-run','^TestAckClock','-count=1','-v']
        run=subprocess.run(command,cwd=ROOT,env=clean,text=True,capture_output=True,timeout=180)
        (output/'hub.stdout').write_text(run.stdout+run.stderr)
        (output/'RESULT.json').write_text(json.dumps({'refs':refs,'cases':len(all_cases),'negative_injected':sum(c['injected_before_created'] for c in all_cases),'go_rc':run.returncode,'node_access':False,'native_server_started':False},indent=2)+'\n')
        print(run.stdout);print(run.stderr);assert run.returncode==0


if __name__=='__main__':main()
