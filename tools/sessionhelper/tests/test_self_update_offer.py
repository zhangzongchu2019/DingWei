"""Actual loopback HTTP/archive controls; not a real Hub or Docker acceptance test."""
import copy
from datetime import datetime, timedelta, timezone
import functools
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import sys
import threading
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(ROOT), str(ROOT / 'tests')]

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tools.sessionhelper.tests._home_isolation import HomeIsolation
_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


from sessionhelper.config import Config
from sessionhelper.provision import Provisioner, ProvisionError
from sessionhelper.self_update import SelfUpdater, UpdateRejected, receipt
import sessionhelper.self_update as runtime
import package_release
import test_update_release_core as core


class OfferTests(unittest.TestCase):
    def setUp(self):
        # This upgrade fixture is specifically factory 0.2.3.1 -> 0.2.3.2,
        # independent of the checkout under test. Never patch production code.
        factory_version = patch.object(runtime, "__version__", "0.2.3.1")
        factory_version.start(); self.addCleanup(factory_version.stop)
        self.fixture = core.PackageTests(methodName='runTest')
        self.fixture.setUp(); self.addCleanup(self.fixture.doCleanups)
        base = self.fixture.base
        (base/'sessionhelper/__init__.py').write_text('__version__ = "0.2.3.2"\n')
        commit = self.fixture.commit()
        self.artifact = Path(self.fixture.tmp.name)/'upgrade.tgz'
        pin = package_release.build(self.fixture.repo, commit, self.artifact)
        self.pin = pin
        manifest, files = package_release.verify(self.artifact)
        self.mount = Path(self.fixture.tmp.name)/'mounted'; self.mount.mkdir()
        for name, content in files.items():
            p=self.mount/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(content)
        self.requests=[]; outer=self
        class HTTP(BaseHTTPRequestHandler):
            def do_GET(self):
                outer.requests.append(dict(self.headers))
                if self.path == '/redirect':
                    self.send_response(302);self.send_header('Location','/source.tgz');self.end_headers();return
                if self.path == '/slow': time.sleep(0.6)
                body=outer.artifact.read_bytes()
                self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
            def log_message(self,*args): pass
        self.server=ThreadingHTTPServer(('127.0.0.1',0),HTTP)
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()
        self.addCleanup(self.server.server_close);self.addCleanup(self.server.shutdown)
        self.cfg=Config('synthetic','synthetic-key','SYNTHETIC_NOT_TRANSMITTED',
                        ws_base='ws://127.0.0.1:'+str(self.server.server_port),
                        state_dir=str(Path(self.fixture.tmp.name)/'private'))
        self.provisioner=Provisioner(self.cfg)
        self.updater=SelfUpdater(self.cfg,self.provisioner.validate_source,'agent.owner',
                                 launcher=str(self.mount/'helper_update_guard.py'),allow_loopback=True)
        self.env={'from':'workpulse#synthetic-key','meta':{'system':True,'type':'provision','action':'update_self',
            'target':'self','job_id':'job-synthetic','release_id':'release-synthetic','agent_id':'id-synthetic','owner_id':'owner-synthetic',
            'agent_name':'agent.owner','version':'0.2.3.2','sha256':pin['sha256'],'lock_sha256':pin['lock_sha256'],
            'url':'http://127.0.0.1:'+str(self.server.server_port)+'/source.tgz','platform':'linux',
            'expires_at':(datetime.now(timezone.utc)+timedelta(minutes=29)).isoformat()}}

    def test_updater_root_must_match_launcher_update_root_if_bound(self):
        with patch.dict(os.environ,{'SH_UPDATE_ROOT':str(self.updater.state.root/'other')}):
            with self.assertRaisesRegex(UpdateRejected,'^manual_required$',msg='UPDATER_RETAIN_ROOT_MISMATCH_ACCEPTED'):
                SelfUpdater(self.cfg,self.provisioner.validate_source,'agent.owner',
                            launcher=str(self.mount/'helper_update_guard.py'),allow_loopback=True)

    def legacy_prepare_without_retain(self, marker):
        try:
            _, stage, restart = self.updater.prepare(self.env)
        except UpdateRejected as exc:
            self.fail(marker + ': ' + str(exc))
        self.assertEqual((stage,restart),('installed',True),marker)
        self.assertFalse(os.path.lexists(self.updater.state.root/'retain'),marker)
        self.assertIsNotNone(self.updater.state.read()['pending'],marker)

    def test_new_guard_no_pin_prepare_allows_group_writable_parent(self):
        Path(self.fixture.tmp.name).chmod(0o775)
        self.legacy_prepare_without_retain('LEGACY_PREPARE_GROUP_PARENT_REJECTED')
        self.assertFalse((self.updater.state.root/'retain.txn.lock').exists(),'LEGACY_PREPARE_CREATED_RETAIN_LOCK')

    def test_new_guard_no_pin_prepare_ignores_busy_transaction_lock(self):
        import fcntl
        fd=os.open(self.updater.state.root/'retain.txn.lock',os.O_CREAT|os.O_RDWR,0o600)
        try:
            fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
            self.legacy_prepare_without_retain('LEGACY_PREPARE_WAITED_FOR_UNUSED_LOCK')
        finally:os.close(fd)

    def test_old_guard_legacy_empty_retain_allowed_but_any_entry_refused(self):
        retained=self.updater.state.root/'retain'
        with patch.object(self.updater.guard,'RETENTION_API',0):
            with self.updater.retention_transaction():pass
            retained.mkdir(mode=0o700)
            with self.updater.retention_transaction():pass
            (retained/'unknown-record').write_text('{}')
            with self.assertRaisesRegex(UpdateRejected,'^manual_required$',msg='OLD_GUARD_IGNORED_PINS'):
                with self.updater.retention_transaction():pass

    def test_pce_pinned_collision_and_incompatible_candidate_refused(self):
        meta, _, _ = self.updater.prepare(self.env)
        slot = self.updater.state.slot(meta)
        retention = self.updater.guard.Retention(self.updater.state.root)
        pin, handles = retention.register(slot, 'fixture-endpoint')
        self.addCleanup(lambda: [os.close(f) for f in handles])
        before = slot.stat().st_ino
        self.updater.state.fail(meta, 'fixture release pending')
        second = copy.deepcopy(self.env); second['meta']['job_id'] = 'second-job'
        reason = None
        try: self.updater.prepare(second)
        except UpdateRejected as error: reason = str(error)
        self.assertEqual(slot.stat().st_ino, before, 'PINNED_COLLISION_RMTREE')
        self.assertEqual(reason, 'retain_pinned', 'PINNED_COLLISION_RMTREE')
        # A changed candidate must be refused before arm, independently of the
        # collision guard. Build it from a new immutable local fixture commit.
        (self.fixture.base/'sessionhelper/__main__.py').write_text('print("changed-owner-closure")\n')
        commit = self.fixture.commit()
        self.artifact = Path(self.fixture.tmp.name)/'changed-pce.tgz'
        release = package_release.build(self.fixture.repo, commit, self.artifact)
        second['meta'].update(job_id='changed-job', sha256=release['sha256'])
        with self.assertRaisesRegex(UpdateRejected, '^manual_required$', msg='OWNER_UPDATE_CLOSURE_CHANGED'):
            self.updater.prepare(second)
        self.assertIsNone(self.updater.state.read()['pending'], 'OWNER_UPDATE_CLOSURE_CHANGED')
        self.assertFalse((self.updater.state.root/'slots'/release['sha256']).exists(), 'OWNER_UPDATE_CLOSURE_CHANGED')

    def test_mcp_module_and_factory_script_after_actual_self_update(self):
        from test_mcp_forwarder import assert_tools, probe, snapshot
        # The downloaded release contains the real shim, not a tools/list mock.
        shutil.copyfile(ROOT/'sessionhelper/mcp_shim.py', self.fixture.base/'sessionhelper/mcp_shim.py')
        shutil.copyfile(ROOT/'sessionhelper/ipc_permissions.py', self.fixture.base/'sessionhelper/ipc_permissions.py')
        shutil.copyfile(ROOT/'sessionhelper/ipc_limits.py', self.fixture.base/'sessionhelper/ipc_limits.py')
        commit = self.fixture.commit()
        self.artifact = Path(self.fixture.tmp.name)/'mcp-upgrade.tgz'
        pin = package_release.build(self.fixture.repo, commit, self.artifact)
        self.env['meta'].update(sha256=pin['sha256'], lock_sha256=pin['lock_sha256'])
        factory = self.mount/'factory/sessionhelper'
        shutil.copyfile(ROOT/'sessionhelper/mcp_shim.py', factory/'mcp_shim.py')
        shutil.copyfile(ROOT/'sessionhelper/ipc_permissions.py', factory/'ipc_permissions.py')
        shutil.copyfile(ROOT/'sessionhelper/ipc_limits.py', factory/'ipc_limits.py')
        (factory/'__init__.py').write_text('__version__ = "0.2.3.1"\n')
        state = Path(self.cfg.state_dir)
        assert_tools(self, probe(self.mount, state), '0.2.3.1')
        meta, stage, restart = self.updater.prepare(self.env)
        self.assertEqual((stage, restart), ('installed', True))
        # Pending is not current: MCP must not trial-start/consume its budget.
        before = snapshot(state)
        assert_tools(self, probe(self.mount, state), '0.2.3.1')
        self.assertEqual(snapshot(state), before)
        self.updater.state.select()  # isolated guard trial and exact confirmation
        self.updater.state.confirm(meta)
        before = snapshot(state), snapshot(self.mount)
        assert_tools(self, probe(self.mount, state), '0.2.3.2')
        assert_tools(self, probe(self.mount, state, 'factory-script'), '0.2.3.1')
        self.assertEqual((snapshot(state), snapshot(self.mount)), before)
        self.assertEqual(len(self.requests), 1)

    def test_trusted_fetch_stage_duplicate_and_no_credentials(self):
        meta,stage,restart=self.updater.prepare(self.env)
        self.assertEqual((stage,restart),('installed',True))
        self.assertTrue((self.updater.state.slot(meta)/'sessionhelper/__main__.py').is_file())
        self.updater.mark_restart_requested(meta)
        self.assertEqual(self.updater.prepare(self.env)[1:],('installed',False))
        self.assertEqual(len(self.requests),1)
        self.assertTrue(all('Authorization' not in h and 'Cookie' not in h for h in self.requests))
        self.assertNotIn(self.cfg.secret,json.dumps(self.requests))
        self.assertFalse((self.mount/'journal.json').exists())

    def test_lost_installed_ack_can_retry_restart_without_redownload(self):
        meta,_,restart=self.updater.prepare(self.env)
        self.assertTrue(restart)
        # Simulate a WS failure before receipts/restart request were durably completed.
        self.assertTrue(self.updater.prepare(self.env)[2])
        self.assertEqual(len(self.requests),1)
        self.updater.mark_restart_requested(meta)
        self.assertFalse(self.updater.prepare(self.env)[2])

    def test_enrolled_agent_name_matches_hub_canonical_case(self):
        updater=SelfUpdater(self.cfg,self.provisioner.validate_source,'Agent.Owner',
                            launcher=str(self.mount/'helper_update_guard.py'),allow_loopback=True)
        self.assertEqual(updater.validate_offer(self.env)['agent_name'],'agent.owner')
        self.assertEqual(self.requests,[])

    def test_forged_target_expiry_origin_rejected_before_get(self):
        cases=[('source',{'from':'untrusted'}),('target',{'agent_name':'another.owner'}),
               ('expires',{'expires_at':(datetime.now(timezone.utc)-timedelta(seconds=1)).isoformat()}),
               ('origin',{'url':'https://unrelated.invalid/source.tgz'})]
        for name,change in cases:
            with self.subTest(name=name):
                env=copy.deepcopy(self.env)
                if name=='source':env.update(change)
                else:env['meta'].update(change)
                with self.assertRaises((UpdateRejected,ProvisionError)):
                    self.updater.prepare(env)
        self.assertEqual(self.requests,[])
        self.assertIsNone(self.updater.state.read()['pending'])

    def test_sha_failure_preserves_factory_and_does_not_arm(self):
        self.env['meta']['sha256']='0'*64
        with self.assertRaisesRegex(UpdateRejected,'sha256_mismatch'):
            self.updater.prepare(self.env)
        self.assertIsNone(self.updater.state.read()['pending'])
        self.assertEqual(self.updater.state.select(),(self.mount/'factory',None))

    def test_offer_expiring_during_download_never_arms_candidate(self):
        self.env['meta']['expires_at']=(datetime.now(timezone.utc)+timedelta(seconds=0.4)).isoformat()
        self.env['meta']['url']=self.env['meta']['url'].replace('/source.tgz','/slow')
        with self.assertRaisesRegex(UpdateRejected,'offer_expired'):
            self.updater.prepare(self.env)
        self.assertIsNone(self.updater.state.read()['pending'])
        self.assertEqual(self.updater.state.select(),(self.mount/'factory',None))

    def test_redirect_denied_without_followup_fetch(self):
        self.env['meta']['url']=self.env['meta']['url'].replace('/source.tgz','/redirect')
        with self.assertRaisesRegex(UpdateRejected,'redirect_denied'):
            self.updater.prepare(self.env)
        self.assertEqual(len(self.requests),1)
        self.assertIsNone(self.updater.state.read()['pending'])

    def test_lock_changed_rejects_before_download_without_pip(self):
        self.env['meta']['lock_sha256']='0'*64
        with self.assertRaisesRegex(UpdateRejected,'lock_changed'):
            self.updater.prepare(self.env)
        self.assertEqual(self.requests,[])

    def test_no_job_manual_factory_health_is_none(self):
        self.assertIsNone(self.updater.health())
        self.assertIsNone(self.updater.state.read()['pending'])

    def test_health_is_job_version_and_fresh_nonce_bound(self):
        meta,_,_=self.updater.prepare(self.env)
        self.updater.state.select()
        with patch.object(runtime,'__version__','0.2.3.2'):
            first=self.updater.health();second=self.updater.health()
            self.assertNotEqual(first['health_nonce'],second['health_nonce'])
            env={'from':self.env['from'],'meta':{**first,'type':'helper_update_confirm','system':True}}
            with self.assertRaisesRegex(UpdateRejected,'confirmation_mismatch'):
                self.updater.confirm(env)
            env['meta'].update(second,type='helper_update_confirm',system=True)
            confirmed=self.updater.confirm(env)
            self.assertEqual(receipt(confirmed,'confirmed')['from_version'],'0.2.3.1')
        self.assertEqual(self.updater.state.read()['current']['job_id'],meta['job_id'])
        self.assertIsNone(self.updater.state.read()['pending'])

    def test_health_and_receipts_pin_immutable_runtime_lock_without_journal_expansion(self):
        meta,_,_=self.updater.prepare(self.env)
        self.updater.state.select()
        with patch.object(runtime,'__version__','0.2.3.2'):
            health=self.updater.health()
            self.assertEqual(health['lock_sha256'],self.pin['lock_sha256'])
            self.assertEqual(receipt(meta,'installed')['lock_sha256'],self.pin['lock_sha256'])
            self.assertNotIn('lock_sha256',self.updater.state.read()['pending'])
            for field in (*runtime.FIELDS,'lock_sha256','health_nonce'):
                with self.subTest(field=field):
                    bad={**health,field:'changed','type':'helper_update_confirm','system':True}
                    with self.assertRaisesRegex((UpdateRejected,self.updater.guard.UpdateError),'confirmation_mismatch'):
                        self.updater.confirm({'from':self.env['from'],'meta':bad})
                    self.assertIsNotNone(self.updater.state.read()['pending'])
            lock=self.mount/'requirements-runtime.lock'; original=lock.read_bytes()
            lock.write_bytes(original+b'\n# changed after health\n')
            with self.assertRaisesRegex(UpdateRejected,'lock_changed'):
                self.updater.confirm({'from':self.env['from'],'meta':{**health,'type':'helper_update_confirm','system':True}})
            lock.write_bytes(original)
            confirmed=self.updater.confirm({'from':self.env['from'],'meta':{**health,'type':'helper_update_confirm','system':True}})
            self.assertEqual(receipt(confirmed,'confirmed')['lock_sha256'],self.pin['lock_sha256'])

    def test_restarted_health_fails_closed_if_job_lock_binding_changes(self):
        meta,_,_=self.updater.prepare(self.env)
        self.updater.state.select()
        marker=self.updater.state.slot(meta)/'.helper-update-binding.json'
        original=marker.read_bytes()
        lock=self.mount/'requirements-runtime.lock'; original_lock=lock.read_bytes()
        with patch.object(runtime,'__version__','0.2.3.2'):
            def reopened():
                return SelfUpdater(self.cfg,self.provisioner.validate_source,'agent.owner',
                                   launcher=str(self.mount/'helper_update_guard.py'),allow_loopback=True)
            lock.write_bytes(original_lock+b'\n# changed while stopped\n')
            with self.assertRaisesRegex(UpdateRejected,'lock_changed'):reopened().health()
            lock.write_bytes(original_lock)
            lock.unlink()
            with self.assertRaisesRegex(UpdateRejected,'lock_changed'):reopened().health()
            lock.write_bytes(original_lock)
            for field in ('job_id','lock_sha256'):
                binding=json.loads(original);binding[field]='changed';marker.write_text(json.dumps(binding))
                with self.assertRaisesRegex(UpdateRejected,'lock_changed'):reopened().health()
            marker.unlink()
            with self.assertRaisesRegex(UpdateRejected,'lock_changed'):reopened().health()
            marker.write_bytes(original)
            self.assertEqual(reopened().health()['lock_sha256'],self.pin['lock_sha256'])
            self.assertIsNotNone(self.updater.state.read()['pending'])

    def test_authenticated_update_source_pin_does_not_relax_other_provision(self):
        self.provisioner.trusted_update_source='workpulse#u_internalowner'
        env=copy.deepcopy(self.env);env['from']='workpulse#u_internalowner'
        self.provisioner.validate_source(env)
        with self.assertRaises(ProvisionError):
            self.provisioner.validate_source(self.env)
        env['meta']['action']='install_skill'
        with self.assertRaises(ProvisionError):
            self.provisioner.validate_source(env)
        legacy=copy.deepcopy(self.env);legacy['meta']['action']='install_skill'
        self.provisioner.validate_source(legacy)
        env['meta']['action']='update_self';env['meta']['system']=False
        with self.assertRaises(ProvisionError):
            self.provisioner.validate_source(env)

    def test_no_bootstrap_shim_reports_manual_required(self):
        with patch.dict(os.environ,{},clear=True):
            with self.assertRaisesRegex(UpdateRejected,'manual_required'):
                SelfUpdater(self.cfg,self.provisioner.validate_source,'agent.owner')



class AppDispatchTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        import asyncio
        from types import SimpleNamespace
        from sessionhelper.app import SessionHelper
        self.fixture = OfferTests(methodName='runTest')
        self.fixture.setUp();self.addCleanup(self.fixture.doCleanups)
        self.sent=[];self.restarts=[]
        async def send(value): self.sent.append(json.loads(value))
        self.ws=SimpleNamespace(send=send)
        helper=SimpleNamespace(provisioner=self.fixture.provisioner,adapter_handle_lock=asyncio.Lock(),
            pending_inbound=[],file_inbound_queue=asyncio.Queue(),delivery_busy_acked=set(),
            adapter=SimpleNamespace(is_idle=lambda:True),_helper_update_preparing=False,
            cfg=self.fixture.cfg,book=SimpleNamespace(self_addr='agent.owner'))
        helper._get_self_updater=lambda:self.fixture.updater
        helper._send_update_receipt=SessionHelper._send_update_receipt.__get__(helper)
        self.fixture.provisioner.request_restart=lambda:self.restarts.append(True)
        self.helper=helper;self.app=SessionHelper

    async def test_actual_handler_stages_and_requests_restart_once(self):
        await self.app.handle_helper_update(self.helper,self.ws,self.fixture.env)
        self.assertEqual([x['meta']['stage'] for x in self.sent],['downloaded','installed'])
        self.assertEqual(len(self.restarts),1)
        await self.app.handle_helper_update(self.helper,self.ws,self.fixture.env)
        self.assertEqual(len(self.restarts),1)
        self.assertEqual(len(self.fixture.requests),1)

    async def test_busy_defers_without_fetch_or_restart(self):
        self.helper.adapter.is_idle=lambda:False
        await self.app.handle_helper_update(self.helper,self.ws,self.fixture.env)
        self.assertEqual(self.sent[0]['meta']['reason'],'busy')
        self.assertEqual(self.fixture.requests,[])
        self.assertEqual(self.restarts,[])

    async def test_untrusted_offer_is_not_reflected(self):
        env=copy.deepcopy(self.fixture.env);env['from']='untrusted'
        await self.app.handle_helper_update(self.helper,self.ws,env)
        self.assertEqual(self.sent,[])
        self.assertEqual(self.fixture.requests,[])

    async def test_dropped_confirmation_retries_then_reconnect_uses_new_nonce(self):
        import asyncio
        from types import SimpleNamespace
        meta,_,_=self.fixture.updater.prepare(self.fixture.env)
        self.fixture.updater.state.select()
        self.helper.enroll_credential='synthetic'
        self.ws.response=SimpleNamespace(headers={'X-DW-Helper-Update':'helper-update/v1'})
        first_retry=asyncio.Event()
        async def send(value):
            self.sent.append(json.loads(value))
            if len(self.sent)>=2:first_retry.set()
        self.ws.send=send
        with patch.object(runtime,'__version__','0.2.3.2'), patch.object(runtime,'HEALTH_RETRY_SECONDS',0.01):
            task=asyncio.create_task(runtime.status_loop(self.helper,self.ws))
            try:
                await asyncio.wait_for(first_retry.wait(),1)
            finally:
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):await task
            first,second=[x['meta'] for x in self.sent[:2]]
            self.assertNotEqual(first['health_nonce'],second['health_nonce'])
            # A reconnect constructs a fresh updater over the SAME durable pending job.
            reconnected=SelfUpdater(self.fixture.cfg,self.fixture.provisioner.validate_source,'agent.owner',
                                   launcher=str(self.fixture.mount/'helper_update_guard.py'),allow_loopback=True)
            health=reconnected.health()
            self.assertEqual(health['job_id'],meta['job_id'])
            self.assertNotIn(health['health_nonce'],(first['health_nonce'],second['health_nonce']))
            with self.assertRaisesRegex(UpdateRejected,'confirmation_mismatch'):
                reconnected.confirm({'from':self.fixture.env['from'],'meta':{**second,'type':'helper_update_confirm','system':True}})
            reconnected.confirm({'from':self.fixture.env['from'],'meta':{**health,'type':'helper_update_confirm','system':True}})
            self.assertIsNone(reconnected.state.read()['pending'])

    async def test_rollback_receipt_uses_same_stable_runtime_lock(self):
        import asyncio
        from types import SimpleNamespace
        meta,_,_=self.fixture.updater.prepare(self.fixture.env)
        self.fixture.updater.state.fail(meta)
        self.helper.enroll_credential='synthetic'
        self.ws.response=SimpleNamespace(headers={'X-DW-Helper-Update':'helper-update/v1'})
        delivered=asyncio.Event()
        async def send(value):self.sent.append(json.loads(value));delivered.set()
        self.ws.send=send
        task=asyncio.create_task(runtime.status_loop(self.helper,self.ws))
        try:await asyncio.wait_for(delivered.wait(),1)
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):await task
        self.assertEqual(self.sent[0]['meta']['stage'],'rolled_back')
        self.assertEqual(self.sent[0]['meta']['lock_sha256'],self.fixture.pin['lock_sha256'])

    async def test_old_hub_gets_no_new_health_control(self):
        import asyncio
        from types import SimpleNamespace
        from sessionhelper.self_update import status_loop
        self.ws.response=SimpleNamespace(headers={})
        with self.assertRaises(asyncio.TimeoutError):
            await asyncio.wait_for(status_loop(self.helper,self.ws),0.03)
        self.assertEqual(self.sent,[])


async def capture_wire_packet_fixtures():
    """Actual helper serializers/dispatch; synthetic Hub echo, not Hub E2E."""
    import asyncio
    from types import SimpleNamespace
    from sessionhelper.protocol import envelope

    async def capture_status(case):
        case.helper.enroll_credential='synthetic'
        case.ws.response=SimpleNamespace(headers={'X-DW-Helper-Update':'helper-update/v1'})
        delivered=asyncio.Event()
        async def send(value):case.sent.append(json.loads(value));delivered.set()
        case.ws.send=send
        task=asyncio.create_task(runtime.status_loop(case.helper,case.ws))
        try:await asyncio.wait_for(delivered.wait(),1)
        finally:
            task.cancel()
            try:await task
            except asyncio.CancelledError:pass
        return copy.deepcopy(case.sent[-1])

    case=AppDispatchTests(methodName='runTest')
    await case.asyncSetUp()
    try:
        await case.app.handle_helper_update(case.helper,case.ws,case.fixture.env)
        downloaded,installed=copy.deepcopy(case.sent)
        case.fixture.updater.state.select()
        with patch.object(runtime,'__version__','0.2.3.2'):
            health=await capture_status(case)
            confirm=envelope(case.helper.book.self_addr,'',case.fixture.env['from'],
                             {**health['meta'],'type':'helper_update_confirm','system':True})
            negatives={}
            for label in ('missing_lock','wrong_lock'):
                packet=copy.deepcopy(confirm)
                if label=='missing_lock':packet['meta'].pop('lock_sha256')
                else:packet['meta']['lock_sha256']='0'*64
                before=len(case.sent)
                try:case.fixture.updater.confirm(packet)
                except UpdateRejected as exc:reason=str(exc)
                else:raise AssertionError('invalid lock confirmation accepted')
                if reason!='confirmation_mismatch':raise AssertionError('unexpected rejection code')
                await case.app.handle_update_confirmation(case.helper,case.ws,packet)
                if len(case.sent)!=before:raise AssertionError('invalid confirmation emitted a receipt')
                if case.fixture.updater.state.read()['pending'] is None:raise AssertionError('invalid confirmation mutated pending')
                negatives[label]={'packet':packet,'rejected':reason,'emitted_receipts':0,'pending_preserved':True}
            await case.app.handle_update_confirmation(case.helper,case.ws,confirm)
            confirmed=copy.deepcopy(case.sent[-1])
            if confirmed['meta'].get('stage')!='confirmed':raise AssertionError('valid echo not confirmed')
    finally:case.doCleanups()

    rollback_case=AppDispatchTests(methodName='runTest')
    await rollback_case.asyncSetUp()
    try:
        rollback_case.fixture.env['meta']['job_id']='job-synthetic-rollback'
        await rollback_case.app.handle_helper_update(rollback_case.helper,rollback_case.ws,rollback_case.fixture.env)
        rollback_meta=rollback_case.fixture.env['meta']
        rollback_case.fixture.updater.state.fail(rollback_meta)
        rolled_back=await capture_status(rollback_case)
        if rolled_back['meta'].get('stage')!='rolled_back':raise AssertionError('rollback serializer missing')
    finally:rollback_case.doCleanups()
    return {'provenance':{'boundary':'real helper dispatch/serializer + loopback package HTTP; synthetic trusted Hub confirm',
                          'runtime_version_fixture':'0.2.3.2','real_hub':False,'real_docker':False},
            'downloaded_receipt':downloaded,'installed_receipt':installed,
            'health':health,'synthetic_hub_confirmation':confirm,'confirmed_receipt':confirmed,
            'rollback_receipt':rolled_back,'negative_confirmations':negatives}


class PacketFixtureTests(unittest.IsolatedAsyncioTestCase):
    async def test_actual_serializers_and_missing_wrong_lock_confirmations(self):
        packets=await capture_wire_packet_fixtures()
        lock=packets['health']['meta']['lock_sha256']
        self.assertRegex(lock,r'^[0-9a-f]{64}$')
        for label in ('downloaded_receipt','installed_receipt','confirmed_receipt'):
            meta=packets[label]['meta']
            self.assertEqual(meta['lock_sha256'],lock)
            for field in runtime.FIELDS:self.assertEqual(meta[field],packets['health']['meta'][field])
        self.assertEqual(packets['synthetic_hub_confirmation']['meta']['health_nonce'],packets['health']['meta']['health_nonce'])
        self.assertRegex(packets['rollback_receipt']['meta']['lock_sha256'],r'^[0-9a-f]{64}$')
        self.assertEqual(packets['rollback_receipt']['meta']['stage'],'rolled_back')
        for value in packets['negative_confirmations'].values():
            self.assertEqual(value['rejected'],'confirmation_mismatch')
            self.assertEqual(value['emitted_receipts'],0)
            self.assertTrue(value['pending_preserved'])
        self.assertNotIn('SYNTHETIC_NOT_TRANSMITTED',json.dumps(packets))


if __name__ == '__main__':
    unittest.main()
