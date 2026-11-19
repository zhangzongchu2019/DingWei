import asyncio,datetime,hashlib,json,os,pathlib,secrets,socket,sqlite3,subprocess,time,urllib.request
import websockets
ROOT=pathlib.Path(__file__).parent
BASE=json.loads((ROOT/'env-compat.json').read_text())
NEW='/data/ai-dev-data/r61-integration/tools/sessionhelper'
OLD='/data/ai-dev-data/wt-coder56-helper-02339/tools/sessionhelper'
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def port():
 s=socket.socket();s.bind(('127.0.0.1',0));v=s.getsockname()[1];s.close();return v
async def case(label,hub,helper,expect_terminal):
 d=ROOT/'compat'/label;d.mkdir(parents=True,exist_ok=True,mode=0o700);d.chmod(0o700)
 secret=secrets.token_hex(24);key='review-key';addr=port();db=d/'hub.db';hubp=hp=None
 env=BASE|{'WP_ADDR':f'127.0.0.1:{addr}','WP_DB_PATH':str(db),'WP_DATA_DIR':str(d/'data'),'WP_ADMIN_INIT_PASSWORD':secrets.token_hex(20),'WP_SECRET_KEY':secrets.token_hex(32),'WP_CONSOLE_AUTH_ENABLED':'false','FEISHU_TRANSPORT':'fake','FCS_ADDR':f'127.0.0.1:{port()}'}
 start=utc()
 try:
  with (d/'hub.log').open('wb') as f:hubp=subprocess.Popen([str(ROOT/'bin'/hub)],cwd=d,env=env,stdout=f,stderr=subprocess.STDOUT)
  for _ in range(200):
   if hubp.poll() is not None:raise AssertionError('Hub exited; see local log')
   try:
    await asyncio.to_thread(urllib.request.urlopen,f'http://127.0.0.1:{addr}/healthz',timeout=1);break
   except Exception:await asyncio.sleep(.1)
  else:raise AssertionError('Hub health timeout')
  with sqlite3.connect(db) as c:c.execute('INSERT INTO service_api_key(id,service_id,key_hash,label,active,created_at) VALUES(?,?,?,?,1,?)',(key,'review',hashlib.sha256(secret.encode()).hexdigest(),'isolated review',utc()))
  config={'SH_SESSION_NAME':'receiver','SH_KEY_ID':key,'SH_SECRET':secret,'SH_WS_BASE':f'ws://127.0.0.1:{addr}','SH_MODE':'driver','SH_COLLECT':'0','SH_BUSY_BUFFER_MAX':'1','SH_INBOX_DB':str(d/'inbox.db'),'SH_STATE_DIR':str(d/'state'),'SH_CLI_CWD':str(d),'SH_CODEX_HOME':str(d/'codex'),'SH_PROXY':'off','SH_PROVISION_STATE_FILE':str(d/'update.json'),'SH_PROVISION_VERSIONS_FILE':str(d/'versions.json'),'SH_PROVISION_NO_EXIT':'1','SH_MCP_IPC_SOCK':'/data/ai-dev-data/r61s5serialt/'+label+'.sock','DINGWEI_MCP_IPC_SOCK':'/data/ai-dev-data/r61s5serialt/'+label+'.sock'}
  hp_env=BASE|{'HOME':str(d/'home')};pathlib.Path(hp_env['HOME']).mkdir(exist_ok=True,mode=0o700)
  p={'helper_root':helper,'config':config,'accepted':str(d/'accepted.json'),'acks':str(d/'acks.jsonl')}
  with (d/'helper.log').open('wb') as f:hp=subprocess.Popen(['/tmp/coder-0232-locked-venv/bin/python3',str(ROOT/'helper_process.py')],cwd=d,env=hp_env,stdin=subprocess.PIPE,stdout=f,stderr=subprocess.STDOUT)
  hp.stdin.write(json.dumps(p).encode());hp.stdin.close()
  await asyncio.sleep(1.5)
  if hp.poll() is not None:raise AssertionError('Helper exited; see local log')
  async with websockets.connect(f'ws://127.0.0.1:{addr}/ws/session/source?key_id={key}&version=0.2.3.39',additional_headers={'Authorization':f'Bearer {secret}'},proxy=None) as ws:
   await ws.send(json.dumps({'id':label+'-caps','to':'workpulse#'+key,'meta':{'type':'session_capabilities','delivery_ack_v1':True,'processing_mode':'driver'}}))
   await asyncio.sleep(.3)
   await ws.send(json.dumps({'id':label+'-message','to':'receiver#'+key,'body':'isolated capacity compatibility request'}))
   for _ in range(200):
    if hp.poll() is not None:raise AssertionError('Helper stopped during receive')
    if (d/'accepted.json').exists():break
    await asyncio.sleep(.05)
   else:raise AssertionError('No actual helper delivery acceptance')
   await asyncio.sleep(.5)
   observed=json.loads((d/'accepted.json').read_text());acks=[json.loads(x) for x in (d/'acks.jsonl').read_text().splitlines()];ack=acks[-1]
   with sqlite3.connect(db) as c:
    c.row_factory=sqlite3.Row
    r=dict(c.execute('SELECT id,state,last_error,ack_protocol,processing_mode,ack_generation FROM session_delivery WHERE id=?',(observed['delivery_id'],)).fetchone())
    notices=c.execute("SELECT count(*) FROM session_delivery WHERE source_type='failure_notice'").fetchone()[0]
   assert notices == (1 if expect_terminal else 0), ('notice-count',notices)
   expected='failed' if expect_terminal else 'busy'
   assert ack['state']==expected,(ack,r)
   assert ack.get('retryable')== (not expect_terminal),(ack,r)
   assert (r['state']=='dead' and r['last_error']=='queue_full') if expect_terminal else r['state']!='dead',(ack,r)
   result={'case':label,'started_utc':start,'ended_utc':utc(),'hub_process':hub,'hub_source_commit':('501e5b8a185b20f5c931c8ebb89c16bfd633a01e' if hub=='old-hub-501' else BASE['SOURCE_COMMIT']),'helper_source':helper,'helper_source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=pathlib.Path(helper).parents[1],text=True).strip(),'actual_handshake':True,'actual_recv_accept':True,'model_consumption':'fixture paused; no CLI/provider','observed':observed,'ack':ack,'hub_row':r,'failure_notice_rows':notices,'rc':0}
   (d/'result.json').write_text(json.dumps(result,indent=2)+'\n');return result
 finally:
  for proc in (hp,hubp):
   if proc and proc.poll() is None:
    proc.terminate()
    try:proc.wait(timeout=10)
    except subprocess.TimeoutExpired:proc.kill();proc.wait()
async def main():
 results=[]
 for args in [('old501-newhelper','old-hub-501',NEW,False),('newhub-newhelper','integrated-hub',NEW,True),('newhub-old39','integrated-hub',OLD,False)]:
  try:r=await case(*args)
  except Exception as e:r={'case':args[0],'rc':1,'error_class':type(e).__name__,'detail':str(e)}
  results.append(r);(ROOT/'compat-results.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(r),flush=True)
asyncio.run(main())
