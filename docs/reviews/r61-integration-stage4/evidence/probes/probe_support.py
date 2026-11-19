import asyncio,contextlib,json,os,tempfile,threading,time,unittest

from pathlib import Path

from unittest import mock

from sessionhelper.app import SessionHelper

from sessionhelper.config import load_config

from sessionhelper.driver_adapter import DriverAdapter

from sessionhelper.codexDriver import CodexProtocolDriver,JsonRpcError

class RPC:
 def __init__(self,cwd,errors=()):self.cwd=cwd;self.errors=list(errors);self.calls=[];self.turns={}
 def healthy(self):return True
 def stop(self):pass
 def notify(self,*a):pass
 def request(self,method,params,timeout=0):
  self.calls.append((method,params))
  if method=='initialize' and self.errors:raise self.errors.pop(0)
  if method in ('thread/read','thread/resume'):return {'thread':{'id':'bound-thread','cwd':self.cwd,'status':{'type':'idle'},'canAcceptDirectInput':True}}
  if method=='thread/turns/list':return {'data':[{'id':k,'status':v}for k,v in self.turns.items()]}
  if method=='turn/start':
   callback=getattr(self,'on_turn_start',None)
   if callback:callback(params)
   tid='turn-'+params['clientUserMessageId'];self.turns[tid]='inProgress';return {'turn':{'id':tid,'status':'inProgress'}}
  return {}

class WS:
 def __init__(self):self.sent=[]
 async def send(self,payload):self.sent.append(json.loads(payload))

def driver(tmp,rpc,name):
 d=CodexProtocolDriver(workdir=tmp,store_path=str(Path(tmp)/(name+'.db')),bound_thread_id='bound-thread',require_bound_thread=True)
 def start_locked():d._rpc=rpc;d._initialize()
 d._start_locked=start_locked;return d

def setup(tmp,first_rpc,replacement_rpc):
 first=driver(tmp,first_rpc,'first');new=driver(tmp,replacement_rpc,'replacement')
 cfg=load_config({'SH_SESSION_NAME':'seat','SH_OWNER':'review','SH_KEY_ID':'review-0001','SH_SECRET':'synthetic-local-only','SH_DRIVER':'codex','SH_MODE':'driver','SH_INBOX_DB':str(Path(tmp)/'inbox.db'),'SH_STATE_DIR':str(Path(tmp)/'state'),'SH_CLI_CWD':tmp,'SH_CODEX_HOME':str(Path(tmp)/'codex'),'SH_PROVISION_STATE_FILE':str(Path(tmp)/'update.json'),'SH_PROVISION_VERSIONS_FILE':str(Path(tmp)/'versions.json'),'SH_COLLECT':'0','SH_PROXY':'off'})
 with mock.patch.dict(os.environ,{'SH_DRIVER':'codex'}),mock.patch.object(DriverAdapter,'_build_driver',return_value=first):h=SessionHelper(cfg)
 h.upload_addendum_due=lambda:'';h.adapter._build_driver=lambda _:new
 ids=['dw-dlv_'+'a'*32,'dw-dlv_'+'b'*32]
 envs=[]
 for i,did in enumerate(ids):
  env={'id':'synthetic-'+str(i),'from':'peer-review','to':'seat-review','body':'message-'+str(i),'meta':{'delivery_id':did,'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}};envs.append(env);h.delivery_inbox.accept(did,env)
 for ack in h.delivery_inbox.pending_delivery_acks():h.delivery_inbox.confirm_delivery_ack(ack['payload']|{'type':'delivery_ack_confirm'})
 return h,first,new,ids,envs

def stop(h,*drivers):
 a=h.adapter;a._lane_stop.set();a._lane_closing.set();a._work_available.set();a._bridge_wake.set()
 for n in ['_worker_thread','_bridge_thread','_transport_recovery_thread']:
  t=getattr(a,n,None)
  if t:t.join(2)
 for d in set(drivers+(a.driver,)):d.stop()
