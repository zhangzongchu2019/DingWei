"""Loopback integration driver of the pinned, unchanged 0.2.3.39 helper package.
Only the CLI adapter is synthetic; no CLI/model/account outside this fixture.
"""
import asyncio, hashlib, json, os, queue, sys, threading, time
from pathlib import Path
from sessionhelper.app import SessionHelper
from sessionhelper.config import Config
from sessionhelper.enroll import Credential
from sessionhelper import __version__

class Adapter:
    name='synthetic-no-model'
    def __init__(self):
        self.events=queue.Queue(); self.boundary=threading.Event(); self.release=threading.Event()
    def next_session_event(self, timeout):
        try: item=self.events.get(timeout=timeout)
        except queue.Empty: return None
        if item[0]==501:
            self.boundary.set(); self.release.wait(30)
        return item[1]
    def next_terminal_chunk(self, timeout):
        time.sleep(min(timeout,.05)); return ''
    def handle(self,*args,**kwargs):return ''
    def is_busy(self):return False

async def main():
    seed=json.loads(sys.stdin.readline()); home=Path(seed['home'])
    assert Path.home()==home and seed['base'].startswith('ws://127.0.0.1:')
    assert __version__=='0.2.3.39'
    cfg=Config('legacy','u_zzc',seed['secret'],mode='echo',ws_base=seed['base'],collect=False,
        cli_cwd=str(home),state_dir=str(home/'.dingwei'),inbox_db=str(home/'inbox.db'),
        mcp_ipc_sock=seed['ipc'],ipc_send_body_only=True,provision_audit_db=str(home/'audit'),
        provision_versions_file=str(home/'versions'),provision_update_state_file=str(home/'provision'))
    helper=SessionHelper(cfg); helper.enroll_credential=Credential('legacy',seed['secret'])
    adapter=Adapter(); helper.adapter=adapter; cfg.mode='driver'
    os.environ['SH_EMIT_SESSION_EVENT']='1'  # Explicit fixture input, never inherited.
    wsref=[]; frame_stats={"peak_data_frame":0,"control_pings":0}; original_ipc=helper._ipc_server
    async def capture(ws):
        wsref.append(ws)
        original_frame=ws.protocol.recv_frame
        def on_frame(frame):
            if int(frame.opcode) in (0,1,2): frame_stats['peak_data_frame']=max(frame_stats['peak_data_frame'],len(frame.data))
            if int(frame.opcode)==9: frame_stats['control_pings']+=1
            return original_frame(frame)
        ws.protocol.recv_frame=on_frame
        await original_ipc(ws)
    helper._ipc_server=capture
    original_recv=helper.recv_loop
    async def tapped(ws):
        iterator=ws.__aiter__()
        class Proxy:
            def __getattr__(self,k):return getattr(ws,k)
            def __aiter__(self):return self
            async def __anext__(self):
                raw=await iterator.__anext__(); env=json.loads(raw); meta=env.get('meta') or {}
                if meta.get('type')=='delivery_ack_confirm' and meta.get('delivery_id')=='rif-ack-wire':
                    print('RIF_ACK_CONFIRM',flush=True)
                if meta.get('rif_fixture'):
                    body=env.get('body') or ''
                    expected=('rif-wire' if '界' in body else 'rif-large') if body else ('rif-control','rif-small-paused')
                    assert env.get('id') == expected if isinstance(expected,str) else env.get('id') in expected, 'logical envelope ID changed'
                    print('RIF_LARGE '+hashlib.sha256(body.encode()).hexdigest() if body else 'RIF_SMALL',flush=True)
                return raw
        await original_recv(Proxy())
    helper.recv_loop=tapped
    task=asyncio.create_task(helper._run_once())
    try:
        async with asyncio.timeout(10):
            while not helper.ipc_bound.is_set():
                if task.done():task.result();raise RuntimeError('ended before IPC')
                await asyncio.sleep(.01)
        print('RIF_READY39',flush=True)
        while True:
            line=await asyncio.to_thread(sys.stdin.readline)
            if not line:break
            command=json.loads(line); op=command['op'];ws=wsref[0]
            if op=='stop':break
            if op=='burst':
                for i in range(1,1001):
                    adapter.events.put((i,json.dumps({'kind':'assistant_text','text':'x'*4096,'sequence':i},separators=(',',':'))))
                assert await asyncio.to_thread(adapter.boundary.wait,15)
                ack=command['ack']; await ws.send(json.dumps(helper.delivery_ack('rif-ack-wire','queued',**ack)))
                # Real body/handler sentinel between events500 and501, same socket/order.
                await ws.send(json.dumps({'id':'rif-body-barrier','body':'rif body barrier','meta':{'control_task_id':'rif-order-barrier'}}))
                print('RIF_ACK_SENT',flush=True);adapter.release.set()
            elif op=='work':
                await ws.send(json.dumps({'id':'rif-default-work','body':'long business gate','meta':{'control_task_id':'rif-default-work'}}))
                print('RIF_WORK_SENT',flush=True)
            elif op=='pause':
                ws.transport.pause_reading()
                async def resume():await asyncio.sleep(3);ws.transport.resume_reading()
                asyncio.create_task(resume());print('RIF_PAUSED',flush=True)
            elif op=='frames':
                print('RIF_FRAMES '+json.dumps(frame_stats),flush=True)
            elif op=='state':
                print('RIF_STATE '+json.dumps({'alive':not task.done()}),flush=True)
    finally:
        adapter.release.set();task.cancel();await asyncio.gather(task,return_exceptions=True)
    print('RIF_STOPPED',flush=True)

asyncio.run(main())
