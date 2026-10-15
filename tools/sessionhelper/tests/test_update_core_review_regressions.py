"""Regression tests from independently demonstrated core review cases; no real Docker."""
import json, os, signal, subprocess, sys, tempfile, time, unittest
from pathlib import Path
from unittest.mock import patch
REPO=Path(__file__).resolve().parents[3]
CORE=REPO/'tools/sessionhelper'
sys.path[:0]=[str(CORE),str(CORE/'tests')]
import package_release as package
import bootstrap
from update_launcher.guard import State, UpdateError
import test_update_release_core as core_tests

class ReviewRegressionTests(unittest.TestCase):
    def fixture(self):
        f=core_tests.PackageTests('test_literal_secret_content_rejected'); f.setUp(); self.addCleanup(f.doCleanups); return f

    def test_ignored_forbidden_file_must_reject(self):
        f=self.fixture()
        (f.repo/'.gitignore').write_text('.env\n*.db\n')
        f.commit()
        (f.base/'.env').write_text('SYNTHETIC_REVIEW_ONLY')
        self.assertEqual(f.git('status','--porcelain','--untracked-files=all','--','tools/sessionhelper/'),'')
        with self.assertRaises(package.PackageError):
            package.build(f.repo,f.git('rev-parse','HEAD'),Path(f.tmp.name)/'ignored-forbidden.tgz')

    def preflight_fixture(self):
        f=self.fixture(); artifact=Path(f.tmp.name)/'release.tgz'
        package.build(f.repo,f.git('rev-parse','HEAD'),artifact)
        manifest,files=package.verify(artifact)
        stage=Path(f.tmp.name)/'stage';stage.mkdir()
        for n,b in files.items():
            p=stage/n;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b)
        (stage/'MANIFEST.json').write_text(json.dumps(manifest))
        mount=Path(f.tmp.name)/'mount';mount.mkdir()
        state=Path(f.tmp.name)/'private'
        def docker(*args):
            if args[0]=='ps': return b'synthetic-id'
            if args[0]!='inspect':raise AssertionError('unexpected Docker action')
            return json.dumps([{'Id':'synthetic-id','Mounts':[{'Destination':'/opt/dingwei/sessionhelper-src','Source':str(mount),'RW':False}],
                'HostConfig':{'RestartPolicy':{'Name':'unless-stopped'}},
                'Config':{'Entrypoint':['/usr/bin/tini','--'],'Cmd':['bash','-c',
                'source /run/secrets/sessionhelper_env; exec /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper'],
                'Env':['PYTHONPATH=/opt/dingwei/sessionhelper-src']}}]).encode()
        def local(container,script,*args):
            return subprocess.check_output([sys.executable,'-c',script,*args],env=dict(os.environ,SH_STATE_DIR=str(state),PYTHONPATH='/opt/dingwei/sessionhelper-src'))
        return stage,mount,state,docker,local

    def test_plain_preflight_must_not_create_state(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        with patch.object(bootstrap,'docker',side_effect=docker),patch.object(bootstrap,'in_container',side_effect=local):
            bootstrap.preflight(stage,mount,'synthetic')
        self.assertFalse(state.exists(),'plain preflight created private/update without explicit marker/restart check')

    def test_wrong_container_entry_must_be_rejected(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        valid=json.loads(docker('inspect','synthetic'))[0]
        def wrong(*args):
            if args[0]=='inspect':
                value=dict(valid,Config={'Entrypoint':['/bin/not-the-required-launcher'],'Cmd':['unrelated-service']})
                return json.dumps([value]).encode()
            return docker(*args)
        with patch.object(bootstrap,'docker',side_effect=wrong),patch.object(bootstrap,'in_container',side_effect=local):
            with self.assertRaisesRegex(bootstrap.BootstrapError,'entry_unsupported'):
                bootstrap.preflight(stage,mount,'synthetic')
        self.assertFalse(state.exists())

    def test_shared_source_mount_is_rejected_before_any_probe(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        valid=json.loads(docker('inspect','synthetic'))[0]
        def shared(*args):
            if args[0]=='ps': return b'synthetic-id another-id'
            if args==('inspect','synthetic-id','another-id'):
                return json.dumps([valid,dict(valid,Id='another-id')]).encode()
            return docker(*args)
        with patch.object(bootstrap,'docker',side_effect=shared),patch.object(bootstrap,'in_container') as probe:
            with self.assertRaisesRegex(bootstrap.BootstrapError,'source_mount_shared'):
                bootstrap.preflight(stage,mount,'synthetic')
            probe.assert_not_called()
        self.assertFalse(state.exists())

    def test_entry_environment_and_commands_fail_closed(self):
        cfg=dict(Entrypoint=['/usr/bin/tini','--'],Cmd=['bash','-c',
            'source /run/secrets/sessionhelper_env; exec /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper'],
            Env=['PYTHONPATH=/opt/dingwei/sessionhelper-src'])
        bootstrap.check_entry({'Config':cfg})
        for changes in ({'Env':['PYTHONPATH=/elsewhere']}, {'Cmd':['bash','-c','echo unrelated']},
                        {'Cmd':['bash','-c',cfg['Cmd'][2]+'; echo unexpected']}):
            with self.subTest(changes=changes):
                with self.assertRaisesRegex(bootstrap.BootstrapError,'entry_unsupported'):
                    bootstrap.check_entry({'Config':dict(cfg,**changes)})

    def test_failed_explicit_restart_removes_marker_without_proof(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        def failed(*args):
            if args[0]=='restart': raise subprocess.CalledProcessError(1,['docker','restart'])
            return docker(*args)
        with patch.object(bootstrap,'docker',side_effect=failed),patch.object(bootstrap,'in_container',side_effect=local):
            with self.assertRaises(subprocess.CalledProcessError):
                bootstrap.preflight(stage,mount,'synthetic',restart_check=True)
        self.assertFalse(list((state/'update').glob('.bootstrap-*')))
        self.assertFalse((state/'update/bootstrap-preflight.json').exists())

    def test_all_ignored_private_names_are_rejected(self):
        f=self.fixture()
        (f.repo/'.gitignore').write_text('.env\n*.key\n*.db\nledger.json\ncredentials/\n')
        f.commit()
        for name in ('.env','seat.key','inbox.db','ledger.json','credentials/token'):
            with self.subTest(name=name):
                path=f.base/name;path.parent.mkdir(exist_ok=True)
                path.write_text('SYNTHETIC_REVIEW_ONLY')
                with self.assertRaises(package.PackageError):
                    package.build(f.repo,f.git('rev-parse','HEAD'),Path(f.tmp.name)/'forbidden.tgz')
                path.unlink()

    def running_candidate(self):
        td=tempfile.TemporaryDirectory();self.addCleanup(td.cleanup);root=Path(td.name)
        factory=root/'factory';(factory/'sessionhelper').mkdir(parents=True)
        (factory/'sessionhelper/__init__.py').write_text('')
        (factory/'sessionhelper/__main__.py').write_text('print("SYNTHETIC_FACTORY_FALLBACK")\n')
        st=State(root/'state',factory)
        item=dict(job_id='review-job',release_id='review-release',agent_id='review-agent',owner_id='review-owner',version='0.2.3.1',sha256='a'*64)
        slot=st.root/'slots'/item['sha256'];(slot/'sessionhelper').mkdir(parents=True)
        (slot/'sessionhelper/__init__.py').write_text('')
        ready=root/'ready'
        (slot/'sessionhelper/__main__.py').write_text('import os,time\nfrom pathlib import Path\nPath('+repr(str(ready))+').write_text(str(os.getpid()))\nprint("SYNTHETIC_SLOT_EXECUTED",flush=True)\nwhile True: time.sleep(.05)\n')
        st.arm(item)
        code='import sys;sys.path.insert(0,'+repr(str(CORE))+');from update_launcher.guard import run;raise SystemExit(run(sys.argv[1],sys.argv[2]))'
        log=(root/'guard.log').open('w');self.addCleanup(log.close)
        proc=subprocess.Popen([sys.executable,'-B','-c',code,str(factory),str(st.root)],stdout=log,stderr=log,start_new_session=True)
        deadline=time.monotonic()+4
        while not ready.exists() and time.monotonic()<deadline:time.sleep(.01)
        self.assertTrue(ready.exists(),'actual candidate failed to start')
        deadline=time.monotonic()+1
        while 'SYNTHETIC_SLOT_EXECUTED' not in (root/'guard.log').read_text() and time.monotonic()<deadline:time.sleep(.01)
        self.assertIn('SYNTHETIC_SLOT_EXECUTED',(root/'guard.log').read_text())
        self.assertNotIn('首次运行请在交互终端执行',(root/'guard.log').read_text())
        childpid=int(ready.read_text())
        def cleanup():
            for pid in (childpid,proc.pid):
                try:os.killpg(pid,signal.SIGKILL)
                except ProcessLookupError:pass
            proc.wait(timeout=3)
        self.addCleanup(cleanup)
        return st,item,proc,childpid

    def test_external_rollback_must_stop_failed_candidate(self):
        st,item,proc,childpid=self.running_candidate()
        st.fail(item,'review_rejection')
        self.assertEqual(st.read()['terminal'][item['job_id']]['stage'],'rolled_back')
        try:proc.wait(timeout=1)
        except subprocess.TimeoutExpired:pass
        self.assertEqual(proc.poll(),75,'terminal rollback must stop guard for supervisor restart')
        with self.assertRaises(ProcessLookupError): os.kill(childpid,0)

    def test_supervisor_restart_should_preserve_unspent_trial(self):
        st,item,proc,childpid=self.running_candidate()
        self.assertEqual(st.read()['pending']['starts'],1)
        proc.send_signal(signal.SIGTERM);proc.wait(timeout=3)
        self.assertIsNotNone(st.read()['pending'],'ordinary supervisor stop terminalized trial before second start')
        with self.assertRaises(ProcessLookupError): os.kill(childpid,0)
        # Restart an actual guard/child using the same durable state, not merely
        # select() on a reconstructed State.
        code='import sys;sys.path.insert(0,'+repr(str(CORE))+');from update_launcher.guard import run;raise SystemExit(run(sys.argv[1],sys.argv[2]))'
        ready=st.root.parent/'ready';ready.unlink()
        second=subprocess.Popen([sys.executable,'-B','-c',code,str(st.factory),str(st.root)],start_new_session=True)
        self.addCleanup(lambda: second.poll() is None and second.kill())
        deadline=time.monotonic()+4
        while not ready.exists() and time.monotonic()<deadline:time.sleep(.01)
        self.assertTrue(ready.exists())
        second_child=int(ready.read_text())
        def cleanup_second():
            for pid in (second_child,second.pid):
                try:os.killpg(pid,signal.SIGKILL)
                except ProcessLookupError:pass
            second.wait(timeout=3)
        self.addCleanup(cleanup_second)
        self.assertEqual(st.read()['pending']['starts'],2)
        second.send_signal(signal.SIGTERM);second.wait(timeout=3)
        self.assertIsNotNone(st.read()['pending'])
        third=subprocess.run([sys.executable,'-B','-c',code,str(st.factory),str(st.root)],capture_output=True,timeout=3)
        self.assertEqual(third.returncode,0,third.stderr)
        self.assertIn(b'SYNTHETIC_FACTORY_FALLBACK',third.stdout)
        self.assertNotIn('首次运行请在交互终端执行'.encode(),third.stdout+third.stderr)
        self.assertIsNone(st.read()['pending'])
        self.assertEqual(st.read()['terminal'][item['job_id']]['stage'],'rolled_back')

    def test_exact_confirmation_keeps_live_child_running(self):
        st,item,proc,childpid=self.running_candidate()
        st.confirm(item)
        with self.assertRaises(subprocess.TimeoutExpired): proc.wait(timeout=.2)
        os.kill(childpid,0)
        # A confirmed child must never be terminalized as failure.
        proc.send_signal(signal.SIGTERM);proc.wait(timeout=3)
        self.assertEqual(st.read()['current'],item)
        self.assertEqual(st.read()['terminal'][item['job_id']]['stage'],'confirmed')
        with self.assertRaises(ProcessLookupError): os.kill(childpid,0)


if __name__=='__main__':unittest.main(verbosity=2)
