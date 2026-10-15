"""Isolated reviewer assertions against exact04a core; no real Docker or Agent."""
import json, os, signal, subprocess, sys, tempfile, time, unittest
from pathlib import Path
from unittest.mock import patch
REPO=Path(os.environ.get('DW_REVIEW_REPO', str(Path.cwd())))
CORE=REPO/'tools/sessionhelper'
sys.path[:0]=[str(CORE),str(CORE/'tests')]
import package_release as package
import bootstrap
from update_launcher.guard import State, UpdateError
from test_update_release_core import PackageTests

class ReviewerProbes(unittest.TestCase):
    def fixture(self):
        f=PackageTests('test_literal_secret_content_rejected'); f.setUp(); self.addCleanup(f.doCleanups); return f

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
            if args[0]!='inspect':raise AssertionError('unexpected Docker action')
            return json.dumps([{'Id':'synthetic-id','Mounts':[{'Destination':'/opt/dingwei/sessionhelper-src','Source':str(mount),'RW':False}],
                'HostConfig':{'RestartPolicy':{'Name':'unless-stopped'}},
                'Config':{'Entrypoint':['/bin/not-the-required-launcher'],'Cmd':['unrelated-service']}}]).encode()
        def local(container,script,*args):
            return subprocess.check_output([sys.executable,'-c',script,*args],env=dict(os.environ,SH_STATE_DIR=str(state)))
        return stage,mount,state,docker,local

    def test_plain_preflight_must_not_create_state(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        with patch.object(bootstrap,'docker',side_effect=docker),patch.object(bootstrap,'in_container',side_effect=local):
            bootstrap.preflight(stage,mount,'synthetic')
        self.assertFalse(state.exists(),'plain preflight created private/update without explicit marker/restart check')

    def test_wrong_container_entry_must_be_rejected(self):
        stage,mount,state,docker,local=self.preflight_fixture()
        with patch.object(bootstrap,'docker',side_effect=docker),patch.object(bootstrap,'in_container',side_effect=local):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.preflight(stage,mount,'synthetic')

    def running_candidate(self):
        td=tempfile.TemporaryDirectory();self.addCleanup(td.cleanup);root=Path(td.name)
        factory=root/'factory';(factory/'sessionhelper').mkdir(parents=True)
        (factory/'sessionhelper/__main__.py').write_text('pass\n')
        st=State(root/'state',factory)
        item=dict(job_id='review-job',release_id='review-release',agent_id='review-agent',owner_id='review-owner',version='0.2.3.1',sha256='a'*64)
        slot=st.root/'slots'/item['sha256'];(slot/'sessionhelper').mkdir(parents=True)
        ready=root/'ready'
        (slot/'sessionhelper/__main__.py').write_text('import os,time\nfrom pathlib import Path\nPath('+repr(str(ready))+').write_text(str(os.getpid()))\nwhile True: time.sleep(.05)\n')
        st.arm(item)
        code='import sys;sys.path.insert(0,'+repr(str(CORE))+');from update_launcher.guard import run;raise SystemExit(run(sys.argv[1],sys.argv[2]))'
        log=(root/'guard.log').open('w');self.addCleanup(log.close)
        proc=subprocess.Popen([sys.executable,'-B','-c',code,str(factory),str(st.root)],stdout=log,stderr=log,start_new_session=True)
        deadline=time.monotonic()+4
        while not ready.exists() and time.monotonic()<deadline:time.sleep(.01)
        self.assertTrue(ready.exists(),'actual candidate failed to start')
        childpid=int(ready.read_text())
        def cleanup():
            for pid in (childpid,proc.pid):
                try:os.killpg(pid,signal.SIGKILL)
                except ProcessLookupError:pass
            proc.wait(timeout=3)
        self.addCleanup(cleanup)
        return st,item,proc

    def test_external_rollback_must_stop_failed_candidate(self):
        st,item,proc=self.running_candidate()
        st.fail(item,'review_rejection')
        self.assertEqual(st.read()['terminal'][item['job_id']]['stage'],'rolled_back')
        try:proc.wait(timeout=1)
        except subprocess.TimeoutExpired:pass
        self.assertIsNotNone(proc.poll(),'journal is rolled_back but guard/candidate still running')

    def test_supervisor_restart_should_preserve_unspent_trial(self):
        st,item,proc=self.running_candidate()
        self.assertEqual(st.read()['pending']['starts'],1)
        proc.send_signal(signal.SIGTERM);proc.wait(timeout=3)
        self.assertIsNotNone(st.read()['pending'],'ordinary supervisor stop terminalized trial before second start')

if __name__=='__main__':unittest.main(verbosity=2)
