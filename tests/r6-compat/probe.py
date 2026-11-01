"""Four-cell local compatibility: actual helper/MCP/IPC/HTTP and an enrolled WS."""
import asyncio,json,os,sys,tempfile,time,urllib.request,urllib.parse
from pathlib import Path

async def main():
    c=json.loads(Path(sys.argv[1]).read_text());u=urllib.parse.urlsplit(c['origin'])
    assert u.scheme=='http' and u.hostname=='127.0.0.1','loopback fixture only'
    with tempfile.TemporaryDirectory(prefix='r6-cmp-') as scratch:
        root=Path(scratch);home=root/'home';home.mkdir();state=root/'state';state.mkdir();os.environ['HOME']=str(home)
        from sessionhelper.app import SessionHelper
        from sessionhelper.config import Config
        from sessionhelper.enroll import Credential
        from sessionhelper import __version__
        import websockets
        cfg=Config('fixture','enrolled','SYNTHETIC',mode='driver',cli_cwd=str(root),state_dir=str(state),codex_home=str(home/'.codex'),inbox_db=str(state/'inbox.sqlite'),mcp_ipc_sock=str(root/'mcp.sock'),ws_base=c['origin'].replace('http:','ws:',1))
        helper=SessionHelper(cfg);helper.enroll_credential=Credential(c['agent_id'],c['credential'])
        ipc=asyncio.create_task(helper._ipc_server(None));child=None;reader=None;incoming=[];sequence=0
        async def rpc(method,params):
            nonlocal sequence
            sequence+=1;child.stdin.write((json.dumps({'jsonrpc':'2.0','id':sequence,'method':method,'params':params})+'\n').encode());await child.stdin.drain()
            raw=await asyncio.wait_for(child.stdout.readline(),8);reply=json.loads(raw);assert reply.get('id')==sequence,'MCP continuity lost';return reply
        async def tool(name,args):
            reply=await rpc('tools/call',{'name':name,'arguments':args})
            if 'error' in reply:return {'rpc_error':reply['error']['code']}
            result=reply['result'];return json.loads(result['content'][0]['text'])
        async def tick(minutes):
            def request():
                req=urllib.request.Request(c['origin']+'/fixture/tick',data=json.dumps({'minutes':minutes}).encode(),headers={'Content-Type':'application/json'},method='POST')
                with urllib.request.urlopen(req,timeout=10) as response:return json.load(response)
            return await asyncio.to_thread(request)
        async def consume(ws):
            async for raw in ws:
                env=json.loads(raw)
                if (env.get('meta') or {}).get('coordinate_review_type')=='coordinate_review_request':incoming.append(env)
        try:
            await asyncio.wait_for(helper.ipc_bound.wait(),5)
            env=dict(os.environ);env['DINGWEI_MCP_IPC_SOCK']=cfg.mcp_ipc_sock
            child=await asyncio.create_subprocess_exec(sys.executable,'-B','-m','sessionhelper','--mcp',stdin=asyncio.subprocess.PIPE,stdout=asyncio.subprocess.PIPE,stderr=asyncio.subprocess.PIPE,env=env,cwd=root)
            await rpc('initialize',{});listed=await rpc('tools/list',{});names={t['name'] for t in listed['result']['tools']}
            new_helper=c['case']!='old-helper-new-on';assert ('dw_coordinate_review' in names)==new_helper,'R6_COMPAT_TOOL_VISIBILITY'
            before=await tool('dw_task_list',{'limit':1});assert before.get('status')=='ok','R6_COMPAT_OTHER_TOOL_BEFORE'
            ws_url,headers=helper._connection()
            async with websockets.connect(ws_url,additional_headers=headers,proxy=None,open_timeout=5,ping_interval=None) as ws:
                helper._bind_connection_agent_identity(ws.response.headers)
                caps=helper.session_capabilities_envelope();assert bool(caps['meta'].get('coordinate_review_v1'))==new_helper,'R6_COMPAT_CAPABILITY'
                await ws.send(json.dumps(caps));reader=asyncio.create_task(consume(ws));await asyncio.sleep(.08)
                request_id=c['request_id'];begin=time.monotonic();answer=await tool('dw_coordinate_review',{'action':'get','request_id':request_id});duration_ms=round((time.monotonic()-begin)*1000,2)
                if c['case'] in ('new-helper-new-off','new-helper-old-hub'):
                    assert answer.get('error')=='coordinate_review_not_enabled' and '坐标派审未启用' in answer.get('message',''),'R6_COMPAT_CLEAR_DISABLED '+str(answer)
                    assert answer['http_status']==(404 if c['case']=='new-helper-old-hub' else 503),'R6_COMPAT_HTTP_CODE'
                    assert duration_ms<5000 and answer['outcome']=='rejected','R6_COMPAT_TIMEOUT_OR_AMBIGUOUS'
                    notice_count=None;opened=False;notice_minutes=None;private_sends=None;open_error=None
                else:
                    if new_helper:assert answer.get('status')=='ok' and answer['request']['id']==request_id,'R6_COMPAT_ENABLED_TOOL'
                    else:
                        assert 'rpc_error' in answer,'R6_COMPAT_OLD_HELPER_GET_EXISTS'
                        unavailable=await tool('dw_coordinate_review',{'action':'open','request_id':request_id})
                        assert unavailable.get('rpc_error')==-32601,'R6_COMPAT_OLD_HELPER_OPEN_EXISTS'
                    at0=await tick(0);at29=await tick(29);assert at0['notices']==at29['notices']==0,'R6_COMPAT_EARLY_NOTICE'
                    at30=await tick(30);assert at30['notices']==1 and at30['sent']==1,'R6_COMPAT_FIRST_NOTICE'
                    if new_helper:
                        result=await tool('dw_coordinate_review',{'action':'open','request_id':request_id});assert result.get('status')=='ok','R6_COMPAT_OPEN_FAILED'
                    at90=await tick(90);at150=await tick(150);at210=await tick(210)
                    notice_count=at210['notices'];opened=at210['opened'];notice_minutes=at210['notice_minutes'];private_sends=at210['sent'];open_error=None if new_helper else unavailable['rpc_error']
                    assert notice_count==(1 if new_helper else 3) and opened==new_helper,'R6_COMPAT_REMIND_OPEN_SEMANTICS'
                    assert at210['completed'] is False,'R6_COMPAT_OPEN_IS_REVIEW'
                    for left,right in zip(at210['notice_minutes'],at210['notice_minutes'][1:]):assert right-left>=60,'R6_COMPAT_INTERVAL'
                    await asyncio.sleep(.08)
                    assert bool(incoming)==new_helper,'R6_COMPAT_UNSUPPORTED_HELPER_DELIVERY'
                after=await tool('dw_task_list',{'limit':1});assert after.get('status')=='ok' and child.returncode is None,'R6_COMPAT_OTHER_TOOL_AFTER'
                pong=await ws.ping();await asyncio.wait_for(pong,2)
                print(json.dumps({'result':'R6_COMPAT_PASS','case':c['case'],'helper_version':__version__,'tools':len(names),'coordinate_tool':new_helper,'r6_capability':new_helper,'http_status':answer.get('http_status'),'error':answer.get('error'),'response_ms':duration_ms,'other_tool_before_after':'ok','same_mcp_process':'alive','same_ws':'pong','create_http_status':int(c['create_status']) if c.get('create_status') else None,'open_error':open_error,'notices':notice_count,'notice_minutes':notice_minutes,'private_sends':private_sends,'opened':opened,'coordinate_deliveries':len(incoming)},ensure_ascii=False),flush=True)
                reader.cancel()
                try:await reader
                except asyncio.CancelledError:pass
        finally:
            if reader and not reader.done():reader.cancel()
            if child:
                child.stdin.close();await asyncio.wait_for(child.wait(),5);err=await child.stderr.read();assert child.returncode==0 and not err,'R6_COMPAT_MCP_EXIT'
            ipc.cancel()
            try:await ipc
            except asyncio.CancelledError:pass

if __name__=='__main__':asyncio.run(main())
