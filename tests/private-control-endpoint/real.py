"""Locked native + controlled real PTY gate. Isolated state, no Hub or credentials.

Usage (locked helper venv): real.py VERSION OUTPUT. Binaries must already match
prior independently checked official npm hashes below. No PATH Codex is used.
"""
from concurrent.futures import ThreadPoolExecutor
from dataclasses import asdict
from datetime import datetime,timezone
import hashlib,json,os
from pathlib import Path
import signal,subprocess,sys,tempfile,time
from unittest import mock

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'tools/sessionhelper'))
HASHES={'0.161.0':'9a820c17865fa825d04db416818679a9d63bd72e50835c396f496e5684626c9c','0.163.0':'399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215'}


def helper_once(config):
    from sessionhelper import codexDriver as driver_module
    driver_module._CODEX_BIN=config['command']
    driver=driver_module.CodexProtocolDriver(workdir=config['workdir'],endpoint_mode='private',
        endpoint_state_dir=config['state_dir'],endpoint_seat=config['seat'],endpoint_codex_home=config['codex_home'],
        daemon_identity_check='enforce',daemon_expected_exe=config['expected_exe'],
        store_path=str(Path(config['workdir'])/'driver-ledger.db'))
    methods=[];original=driver._request
    def request(method,*args,**kwargs):
        methods.append(method);return original(method,*args,**kwargs)
    driver._request=request
    try:
        driver.start();driver._validate_bound_thread()
        assert driver._thread_id==config['thread_id'],'FRESH_HELPER_CHANGED_THREAD'
        assert not any(method in ('thread/start','turn/start') for method in methods),'FRESH_HELPER_CREATED_MODEL_WORK'
        print(json.dumps({'thread_id':driver._thread_id,'peer':driver._rpc.peer_identity.metadata(),'methods':methods}))
    finally:driver.stop()


def run(version,output):
    binary=Path('/tmp/coder-stale-official-followup')/version/'codex'
    sha=hashlib.sha256(binary.read_bytes()).hexdigest();assert sha==HASHES[version],'PINNED_BINARY_MISMATCH'
    out=Path(output).resolve();out.mkdir(parents=True,exist_ok=False)
    root=Path(tempfile.mkdtemp(prefix='dwpe-real-'));root.chmod(0o700)
    home=root/'h';home.mkdir(mode=0o700);ch=home/'ch';ch.mkdir(mode=0o700)
    (ch/'config.toml').write_text('''model_provider="local_probe"
model="probe-no-inference"
check_for_update_on_startup=false
[model_providers.local_probe]
name="local probe"
base_url="http://127.0.0.1:9/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
'''+ '\n[projects.'+json.dumps(str(root))+']\ntrust_level="trusted"\n')
    env={'HOME':str(home),'CODEX_HOME':str(ch),'PATH':str(Path(sys.executable).parent)+':/usr/bin:/bin',
         'PYTHONPATH':str(ROOT/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1',
         'LANG':'C.UTF-8','TERM':'xterm-256color','RUST_LOG':'error','OTEL_SDK_DISABLED':'true'}
    result={'version':version,'binary_sha256':sha,'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
            'start':datetime.now(timezone.utc).isoformat(),'scope':'ISOLATED_NATIVE_OWNER_AND_CONTROLLED_PTY','passed':False}
    clients=[];pidfds=[];driver=None;tui=None;producer=None;producer_handle=None
    with mock.patch.dict(os.environ,env,clear=True):
        import pexpect
        from sessionhelper import private_endpoint as ep,codexDriver as driver_module
        from sessionhelper.daemon_identity import OwnedDaemonIdentity
        try:
            assert subprocess.check_output([str(binary),'--version'],env=env,text=True).strip()=='codex-cli '+version
            producer=driver_module.StdioJsonRpc([str(binary),'app-server','--stdio'],cwd=str(root),env=env)
            producer.start();producer_handle=producer._proc
            producer.request('initialize',{'clientInfo':{'name':'private-persistence-fixture','version':'1'},'capabilities':{'experimentalApi':True}},timeout=5)
            producer.notify('initialized')
            thread=producer.request('thread/start',{'cwd':str(root),'ephemeral':False,'sandbox':'read-only','approvalPolicy':'never'},timeout=5)['thread'];tid=thread['id']
            producer.request('turn/start',{'threadId':tid,'input':[{'type':'text','text':'LOCAL_ENDPOINT_PERSISTENCE_ONLY'}]},timeout=5)
            rollout=Path(thread['path']);deadline=time.monotonic()+10
            while not (rollout.exists() and rollout.stat().st_size>0):
                assert time.monotonic()<deadline,'NO_LOCAL_ROLLOUT';time.sleep(.02)
            producer.stop();producer_handle.wait(timeout=5);producer=None
            result['fixture_producer_reaped']=True;result['fixture_local_turns']=1;result['thread_id']=tid
            spec=ep.Spec(str(root/'s'),'phase1-native',str(ch),str(root),str(binary),str(binary))
            clients=[ep.Client(spec),ep.Client(spec)]
            with ThreadPoolExecutor(2) as pool:
                futures=[pool.submit(client.ensure) for client in clients]
                rows=[future.result(timeout=20) for future in futures]
            assert rows[0]['child']==rows[1]['child'],'DOUBLE_START_TWO_CHILDREN'
            launchers=[proc for client in clients for proc in client._launchers]
            assert len(launchers)==1,'DOUBLE_START_TWO_OWNERS'
            owner=launchers[0];initial=rows[0]
            children=Path('/proc/'+str(owner.pid)+'/task/'+str(owner.pid)+'/children').read_text().split()
            assert children==[str(initial['child']['pid'])],'OWNED_CHILD_COUNT_WRONG'
            result['double_start_single_owner_single_child']=True;result['owner_pid']=owner.pid
            driver_module._CODEX_BIN=str(binary)
            driver=driver_module.CodexProtocolDriver(workdir=str(root),endpoint_mode='private',
                endpoint_state_dir=spec.state_dir,endpoint_seat=spec.seat,endpoint_codex_home=spec.codex_home,
                daemon_identity_check='enforce',daemon_expected_exe=str(binary),store_path=str(root/'driver-ledger.db'))
            driver.start()
            # Explicit fixture current binding, obtained from this test's real
            # native producer. No loaded-list/latest/history inference.
            driver._thread_id=driver._session_id=tid
            driver._validate_bound_thread();driver._private_endpoint.save_thread(tid)
            driver.stop();driver=None
            assert clients[0].status()['child']==initial['child'],'HELPER_STOP_KILLED_CHILD'
            config={**asdict(spec),'thread_id':tid}
            def fresh(label):
                with (out/(label+'.stdout')).open('wb') as stdout,(out/(label+'.stderr')).open('wb') as stderr:
                    p=subprocess.run([sys.executable,'-B',str(Path(__file__).resolve()),'--helper-once'],
                        input=json.dumps(config).encode(),env=env,cwd=ROOT,stdout=stdout,stderr=stderr,timeout=25)
                (out/(label+'.rc')).write_text(str(p.returncode)+'\n')
                assert p.returncode==0,'FRESH_HELPER_FAILED '+label
                return json.loads((out/(label+'.stdout')).read_text())
            first=fresh('helper-before-crash');assert first['thread_id']==tid
            assert clients[0].status()['child']==initial['child'],'FRESH_HELPER_REPLACED_OWNER_CHILD'
            # Bind a pidfd while the same-FD owned peer is still verified. The
            # injected abnormal exit affects only this test-created native child.
            sock=clients[0].open_app(initial)
            child_pidfd=os.pidfd_open(initial['child']['pid']);pidfds.append(child_pidfd)
            assert ep.process_starttime(initial['child']['pid'])==initial['child']['starttime']
            signal.pidfd_send_signal(child_pidfd,signal.SIGKILL);sock.close()
            deadline=time.monotonic()+20
            while True:
                try:
                    replacement=clients[0].status()
                    if replacement['child']!=initial['child']:break
                except ep.DaemonIdentityError:pass
                assert time.monotonic()<deadline,'CHILD_RESTART_TIMEOUT';time.sleep(.03)
            assert replacement['generation']==2 and replacement['boot']==initial['boot'],'RESTART_CHANGED_OWNER'
            second=fresh('helper-after-crash')
            assert second['thread_id']==tid and 'thread/resume' in second['methods'],'RESTART_DID_NOT_RESUME_SAME_ID'
            guard=clients[0].open_app(replacement)
            pidfds.append(os.pidfd_open(replacement['child']['pid']));guard.close()
            result['helper_fresh_process_reuses_owner']=True;result['child_exit_same_id_resumed']=True
            result['initial']=initial;result['replacement']=replacement
            cli=[sys.executable,'-B','-m','sessionhelper.private_endpoint','--state-dir',spec.state_dir,'--seat',spec.seat,
                 '--codex-home',spec.codex_home,'--workdir',spec.workdir,'--command',spec.command,
                 '--expected-exe',spec.expected_exe,'--action','resume','--thread-id',tid]
            result['controlled_resume_command']=clients[0].resume_command(tid)
            tui=pexpect.spawn(cli[0],cli[1:],cwd=str(root),env=env,encoding='utf-8',codec_errors='replace',timeout=1,dimensions=(36,120))
            seen=False
            with (out/'controlled-pty.txt').open('w') as log:
                tui.logfile_read=log;deadline=time.monotonic()+25
                while time.monotonic()<deadline:
                    n=tui.expect(['\x1b\\[6n','\x1b\\[c','probe-no-inference','LOCAL_ENDPOINT_PERSISTENCE_ONLY',pexpect.EOF,pexpect.TIMEOUT],timeout=1)
                    if n==0:tui.send('\x1b[1;1R')
                    elif n==1:tui.send('\x1b[?1;2c')
                    elif n in (2,3):seen=True;time.sleep(1);break
                    elif n==4:break
                assert seen and tui.isalive(),'CONTROLLED_PTY_NOT_READY'
                current=clients[0].native(lambda request:request('thread/read',{'threadId':tid,'includeTurns':False}))['thread']
                assert current['id']==tid and current['status']['type']=='idle','PTY_CHANGED_THREAD_OR_STARTED_WORK'
                tui.sendcontrol('c');time.sleep(.3)
                if tui.isalive():tui.sendcontrol('c')
                tui.expect(pexpect.EOF,timeout=8);tui.close()
                assert tui.exitstatus==0 and tid in (out/'controlled-pty.txt').read_text(),'CONTROLLED_PTY_FAILED'
            assert clients[0].status()['child']==replacement['child'],'PTY_STOP_KILLED_NATIVE'
            result['controlled_real_pty_resume_same_id']=True;result['pty_exitstatus']=tui.exitstatus
            result['stop']=clients[0].stop_owner(human_disconnected=True)
            assert owner.wait(timeout=8)==0,'OWNER_EXIT_FAILED'
            for name in (ep.APP,ep.CONTROL,ep.REGISTRY):
                assert not os.path.lexists(spec.root/name),'OWNER_LEFT_OBJECT '+name
            parent,name=ep.native_location(spec);assert not os.path.lexists(parent/name),'OWNER_LEFT_OWN_NATIVE_TARGET'
            result['owner_clean_stop']=True;result['passed']=True
        finally:
            if tui is not None and tui.isalive():tui.terminate(force=True);tui.close()
            if driver is not None:driver.stop()
            if producer is not None:producer.stop()
            if producer_handle is not None:
                producer_handle.wait(timeout=8)
                for pipe in (producer_handle.stdin,producer_handle.stdout,producer_handle.stderr):
                    if pipe is not None:pipe.close()
            for client in clients:client.release_writer()
            for client in clients:
                for proc in client._launchers:
                    if proc.poll() is None:
                        try:client.stop_owner(human_disconnected=True);proc.wait(timeout=5)
                        except Exception:
                            if proc.poll() is None:
                                os.killpg(proc.pid,signal.SIGKILL);proc.wait(timeout=5)
                                result['fixture_force_cleanup']=True
            for pidfd in pidfds:
                if not result['passed']:
                    try:signal.pidfd_send_signal(pidfd,signal.SIGKILL)
                    except ProcessLookupError:pass
                os.close(pidfd)
            result['end']=datetime.now(timezone.utc).isoformat()
            (out/'RESULT.json').write_text(json.dumps(result,indent=2)+'\n')
    print('PRIVATE_ENDPOINT_NATIVE_PASS '+version)


if __name__=='__main__':
    if sys.argv[1]=='--helper-once':helper_once(json.loads(sys.stdin.buffer.read(16384)))
    else:run(sys.argv[1],sys.argv[2])
