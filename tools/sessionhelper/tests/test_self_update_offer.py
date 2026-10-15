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
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from sessionhelper.config import Config
from sessionhelper.provision import Provisioner, ProvisionError
from sessionhelper.self_update import SelfUpdater, UpdateRejected, receipt
import sessionhelper.self_update as runtime
import package_release
from tools.sessionhelper.tests import test_update_release_core as core


class OfferTests(unittest.TestCase):
    def setUp(self):
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

    def test_trusted_fetch_stage_duplicate_and_no_credentials(self):
        meta,stage,restart=self.updater.prepare(self.env)
        self.assertEqual((stage,restart),('installed',True))
        self.assertTrue((self.updater.state.slot(meta)/'sessionhelper/__main__.py').is_file())
        self.assertEqual(self.updater.prepare(self.env)[1:],('installed',False))
        self.assertEqual(len(self.requests),1)
        self.assertTrue(all('Authorization' not in h and 'Cookie' not in h for h in self.requests))
        self.assertNotIn(self.cfg.secret,json.dumps(self.requests))
        self.assertFalse((self.mount/'journal.json').exists())

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
            self.updater.confirm(env)
        self.assertEqual(self.updater.state.read()['current']['job_id'],meta['job_id'])
        self.assertIsNone(self.updater.state.read()['pending'])

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

    async def test_old_hub_gets_no_new_health_control(self):
        import asyncio
        from types import SimpleNamespace
        from sessionhelper.self_update import status_loop
        self.ws.response=SimpleNamespace(headers={})
        with self.assertRaises(asyncio.TimeoutError):
            await asyncio.wait_for(status_loop(self.helper,self.ws),0.03)
        self.assertEqual(self.sent,[])


if __name__ == '__main__':
    unittest.main()
