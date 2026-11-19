"""Actual worker with a deliberately installed production recovery Event.
This isolates gate interleaving, not a whole real transport replacement thread.
"""
import asyncio,contextlib,json,os,tempfile,time,unittest
from pathlib import Path
from unittest import mock
import probe_support as b
from sessionhelper import task_blocker as api

class BlockerRecoveryGate(unittest.TestCase):
 def test_yielded_blocker_does_not_allow_plain_delivery_through_unset_recovery_gate(self):
  with tempfile.TemporaryDirectory() as tmp:
   rpc=b.RPC(tmp);first=b.driver(tmp,rpc,'first');replacement=b.driver(tmp,rpc,'replacement')
   cfg=b.load_config({'SH_SESSION_NAME':'seat','SH_OWNER':'review','SH_KEY_ID':'review-0001','SH_SECRET':'synthetic-local-only','SH_DRIVER':'codex','SH_MODE':'driver','SH_INBOX_DB':str(Path(tmp)/'inbox.db'),'SH_STATE_DIR':str(Path(tmp)/'state'),'SH_CLI_CWD':tmp,'SH_CODEX_HOME':str(Path(tmp)/'codex'),'SH_PROVISION_STATE_FILE':str(Path(tmp)/'update.json'),'SH_PROVISION_VERSIONS_FILE':str(Path(tmp)/'versions.json'),'SH_COLLECT':'0','SH_PROXY':'off'})
   with mock.patch.dict(os.environ,{'SH_DRIVER':'codex'}),mock.patch.object(b.DriverAdapter,'_build_driver',return_value=first):h=b.SessionHelper(cfg)
   h.upload_addendum_due=lambda:'';a=h.adapter;a._build_driver=lambda _:replacement
   plain='dw-dlv_'+'b'*32;wake='dw-dlv_'+'a'*32
   ordinary={'id':'ordinary','from':'peer-review','to':'seat-review','body':'ordinary message','meta':{'delivery_id':plain,'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}}
   h.delivery_inbox.accept(plain,ordinary)
   for ack in h.delivery_inbox.pending_delivery_acks():h.delivery_inbox.confirm_delivery_ack(ack['payload']|{'type':'delivery_ack_confirm'})
   env={'id':'wake-message','from':'workpulse-review','to':'seat-review','body':'wake recheck only','meta':{'delivery_id':wake,'kind':'task_blocker_ready','task_flow_internal':True,'signal_id':'blocker:review','task_id':'task','blocker_id':'blk:review','generation':1,'input_version':1,'assignment_version':1,'automatic_recheck':True}}
   first.start() # a healthy synthetic transport makes a missing gate observable
   recovery=b.threading.Event();entered=b.threading.Event();attempts=[];started=time.monotonic()
   def prepare(env):
    result=api.prepare_wake('ws://127.0.0.1',{},'dev',env)
    attempts.append({'elapsed':time.monotonic()-started,'reason':result[1]})
    if not entered.is_set():
     a._transport_recovery=recovery
     entered.set()
    return result
   a.blocker_prepare=prepare
   try:
    with mock.patch.object(api,'blocker_request',return_value={'status':'error','http_status':503}):
     a.handle_delivery(env)
     self.assertTrue(entered.wait(3),'PROBE_AUTH_PREFLIGHT_NOT_REACHED')
     time.sleep(1.2)
     calls=[p['clientUserMessageId']for m,p in rpc.calls if m=='turn/start']
     before={'elapsed_seconds':time.monotonic()-started,'turn_start_order':calls,'gate_is_set':recovery.is_set(),'authorization_attempts':attempts,'plain_submission_marker_present':h.delivery_inbox.lifecycle(plain)['submission_may_have_started_at']is not None}
     print('R5_CROSS_BEFORE '+json.dumps(before),flush=True)
     self.assertFalse(recovery.is_set(),'PROBE_RECOVERY_RELEASED_EARLY')
     self.assertEqual(calls,[],'R5_ORDINARY_SUBMITTED_BEFORE_TRANSPORT_RECOVERY_GATE')
     recovery.set();deadline=time.monotonic()+5
     while time.monotonic()<deadline:
      calls=[p['clientUserMessageId']for m,p in rpc.calls if m=='turn/start']
      if calls:break
      time.sleep(.01)
     print('R5_CROSS_AFTER '+json.dumps({'turn_start_order':calls,'blocker_state':a._blocker_journal().stage(env)['state'],'plain_submission_marker_present':h.delivery_inbox.lifecycle(plain)['submission_may_have_started_at']is not None}),flush=True)
     self.assertEqual(calls,[plain],'R5_RELEASED_TRANSPORT_GATE_LOST_OR_DUPLICATED_ORDINARY_DELIVERY')
   finally:recovery.set();b.stop(h,first,replacement)

if __name__=='__main__':unittest.main(verbosity=2)
