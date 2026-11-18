"""Unmodified helper transport/handshake/receive/accept; fixture pauses model consumption."""
import asyncio,json,sys,pathlib,types
p=json.load(sys.stdin)
sys.path.insert(0,p['helper_root'])
import sessionhelper.app as module
from sessionhelper.config import load_config
from sessionhelper import __version__
async def paused(*args,**kwargs): await asyncio.Event().wait()
module.update_status_loop=paused
class ReviewHelper(module.SessionHelper):
 def _build_adapter(self): return types.SimpleNamespace(name='review-paused-model')
 async def accept_delivery(self,ws,env):
  await super().accept_delivery(ws,env)
  if (env.get('meta') or {}).get('delivery_id'):
   pathlib.Path(p['accepted']).write_text(json.dumps({'version':__version__,'negotiated':getattr(self,'_connection_queue_full_ack',False),'wire_protocol':env['meta'].get('delivery_ack_protocol'),'delivery_id':env['meta']['delivery_id']}))
 def delivery_ack(self,did,state,**extra):
  ack=super().delivery_ack(did,state,**extra)
  if did!='prefill':
   with open(p['acks'],'a') as f:f.write(json.dumps(ack['meta'])+'\n')
  return ack
for name in ['delivery_worker_loop','delivery_ack_outbox_loop','pending_drain_loop','file_inbound_worker_loop','terminal_loop','session_event_loop','agent_disk_metrics_loop','inbox_progress_loop','outbox_loop','mirror_loop','online_list_heartbeat_loop']:
 setattr(ReviewHelper,name,paused)
async def run():
 h=ReviewHelper(load_config(p['config']))
 h.delivery_inbox.accept('prefill',{'id':'prefill','from':'source#review','to':'receiver#review','body':'fixture','meta':{'type':'delivery','delivery_id':'prefill'}})
 await h._run_once()
asyncio.run(run())
