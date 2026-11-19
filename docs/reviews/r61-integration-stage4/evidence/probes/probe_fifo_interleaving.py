"""Deterministic actual worker schedule; no product function is bypassed.
The hook pauses AFTER a real defer_current releases its own lock, allowing
recovery to finish while the persisted head has rotated behind its follower.
"""
import asyncio,contextlib,json,threading,tempfile,time,unittest,os
import probe_support as b
class RecoveryFIFOInterleaving(unittest.TestCase):
 def test_durable_first_delivery_must_remain_before_follower_after_recovery(self):
  with tempfile.TemporaryDirectory()as tmp:
   error=TimeoutError('initialize timeout') if os.environ.get('R3_ERROR_KIND','timeout')=='timeout' else b.JsonRpcError('initialize',{'code':-32603,'message':'temporary initialize unavailable'});rpc=b.RPC(tmp,[error,error]);h,first,new,ids,envs=b.setup(tmp,rpc,rpc);a=h.adapter;paused=threading.Event();release=threading.Event();trace=[];original=a._commands.defer_current
   def defer(did, **kwargs):
    result=original(did, **kwargs)
    recovery=a._transport_recovery
    if result:
     trace.append({'deferred':did,'pending':[c.delivery_id for c in a._commands.pending],'recovering':recovery is not None and not recovery.is_set()})
    if result and did==ids[0] and recovery is not None and not recovery.is_set() and not paused.is_set():
     paused.set()
     if not release.wait(8):raise AssertionError('PROBE_RELEASE_TIMEOUT')
    return result
   a._commands.defer_current=defer
   async def run():
    task=asyncio.create_task(h.delivery_worker_loop(b.WS()))
    try:
     for _ in range(300):
      if paused.is_set():break
      await asyncio.sleep(.01)
     self.assertTrue(paused.is_set(),'PROBE_DID_NOT_REACH_DEFERRAL_INTERLEAVING')
     for _ in range(500):
      if a._transport_recovery is not None and a._transport_recovery.is_set():break
      await asyncio.sleep(.01)
     self.assertTrue(a._transport_recovery is not None and a._transport_recovery.is_set(),'PROBE_RECOVERY_DID_NOT_FINISH')
     release.set()
     for _ in range(400):
      calls=[p['clientUserMessageId']for m,p in rpc.calls if m=='turn/start']
      if calls:break
      await asyncio.sleep(.01)
     print('R3_FIFO_INTERLEAVING '+json.dumps({'deferrals':trace,'turn_start_order':calls,'rows':[{'id':did,'state':h.delivery_inbox.lifecycle(did)['state'],'submitted':h.delivery_inbox.lifecycle(did)['submission_may_have_started_at']is not None,'admitted':h.delivery_inbox.lifecycle(did)['admitted_at']is not None}for did in ids]}),flush=True)
     self.assertTrue(calls,'PROBE_NO_SUBMISSION_OBSERVED')
     self.assertEqual(calls[0],ids[0],'R3_FIFO_HEAD_OVERTAKEN_AFTER_SAFE_RECOVERY')
    finally:
     release.set();task.cancel()
     with contextlib.suppress(asyncio.CancelledError):await task
     b.stop(h,first,new)
   asyncio.run(run())
if __name__=='__main__':unittest.main(verbosity=2)
