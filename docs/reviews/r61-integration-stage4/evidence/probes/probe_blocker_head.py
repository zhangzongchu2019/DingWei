"""Real helper/adapter worker/SQLite and real Codex driver; only model RPC and
current blocker authorization HTTP reply are synthetic. No queue monkeypatch.
"""
import asyncio,contextlib,json,os,tempfile,time,unittest
from pathlib import Path
from unittest import mock
import probe_support as b
from sessionhelper import task_blocker as api

class BlockerHeadProgress(unittest.TestCase):
 def test_transient_blocker_authorization_failure_must_not_starve_plain_delivery(self):
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
   attempts=[];started=time.monotonic()
   def prepare(env):
    result=api.prepare_wake('ws://127.0.0.1',{},'dev',env)
    attempts.append({'elapsed':time.monotonic()-started,'reason':result[1]})
    return result
   a.blocker_prepare=prepare
   try:
    with mock.patch.object(api,'blocker_request',return_value={'status':'error','http_status':503}):
     # handle_delivery enqueues the durable blocker BEFORE starting the actual
     # worker. The worker restores the already-durable ordinary row behind it.
     a.handle_delivery(env)
     while time.monotonic()-started<5.2:
      calls=[p['clientUserMessageId']for m,p in rpc.calls if m=='turn/start']
      if plain in calls:break
      time.sleep(.01)
     elapsed=time.monotonic()-started;row=h.delivery_inbox.lifecycle(plain)
     data={'elapsed_seconds':elapsed,'authorization_attempts':attempts,'turn_start_order':calls,'pending':[c.delivery_id for c in a._commands.pending],'blocker_state':a._blocker_journal().stage(env)['state'],'plain_state':row['state'],'plain_submission_marker_present':row['submission_may_have_started_at'] is not None,'actual_worker_alive':a._worker_thread.is_alive()}
     print('R4_BLOCKER_HEAD '+json.dumps(data),flush=True)
     self.assertGreaterEqual(len(attempts),1,'PROBE_DID_NOT_REACH_BLOCKER_RECHECK')
     self.assertIn(plain,calls,'R4_BLOCKER_AUTH_READ_FAILURE_STARVED_ORDINARY_DELIVERY')
   finally:b.stop(h,first,replacement)

if __name__=='__main__':unittest.main(verbosity=2)
