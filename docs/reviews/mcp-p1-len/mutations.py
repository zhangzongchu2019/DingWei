import pathlib,subprocess,os,json,hashlib,argparse,tempfile
parser=argparse.ArgumentParser();parser.add_argument('--root',type=pathlib.Path,required=True);parser.add_argument('--out',type=pathlib.Path,required=True);args=parser.parse_args()
root=args.root.resolve();out=args.out.resolve();out.mkdir(exist_ok=True,parents=True)
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_','HUB_TEST_PG_DSN'))};env.update(HOME='/tmp/mcp-len-mutation-home',HUB_TEST_PG_STRICT='1',PYTHONPATH=str(root/'tools/sessionhelper')+':'+str(root/'tools/sessionhelper/tests'));pathlib.Path(env['HOME']).mkdir(exist_ok=True)
results=[]
def run(name,path,change,cmd,marker):
 p=root/path;raw=p.read_bytes();s=raw.decode();new=change(s);assert new!=s
 p.write_text(new)
 try:
  r=subprocess.run(cmd,cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
  (out/(name+'.log')).write_text(r.stdout)
  results.append({'name':name,'rc':r.returncode,'marker':marker,'targeted_red':r.returncode!=0 and marker in r.stdout})
 finally:
  p.write_bytes(raw);assert hashlib.sha256(p.read_bytes()).digest()==hashlib.sha256(raw).digest()
run('characters','internal/extshared/limits.go',lambda s:s.replace('"strings"','"strings"\n"unicode/utf8"').replace('if len(body) > MessageBodyMaxBytes','if utf8.RuneCountInString(body) > MessageBodyMaxBytes'),['go','test','-count=1','./internal/extseat','-run','TestMCPLengthUTF8PG'],'MCP_LEN_OVERSIZE_ZERO_DELIVERY')
old='if e = extshared.CheckMessageBody(extshared.ExternalSenderHeader(fresh.FullName, ownerDisplay, ownerShort, targetName) + "\\n" + body); e != nil {\n\t\t\treturn e\n\t\t}'
new='if e = extshared.CheckMessageBody(extshared.ExternalSenderHeader(fresh.FullName, ownerDisplay, ownerShort, targetName) + "\\n" + body); e != nil {\n body = body[:extshared.MessageBodyMaxBytes-len(extshared.ExternalSenderHeader(fresh.FullName, ownerDisplay, ownerShort, targetName))-1]\n }'
run('truncate-enqueue','internal/extseat/inbox.go',lambda s:s.replace(old,new),['go','test','-count=1','./internal/extseat','-run','TestMCPLengthUTF8PG'],'MCP_LEN_OVERSIZE_ZERO_DELIVERY')
run('helper65536','tools/sessionhelper/sessionhelper/ipc_limits.py',lambda s:s.replace('IPC_REQUEST_MAX_BYTES = 6 * MESSAGE_BODY_MAX_BYTES + (64 << 10)','IPC_REQUEST_MAX_BYTES = 65536'),[os.environ.get('PYTHON','python3'),'-m','unittest','tools/sessionhelper/tests/test_mcp_message_length.py'],'MCP_LEN_EXACT_UTF8')
run('helper-frame-default','tools/sessionhelper/sessionhelper/app.py',lambda s:s.replace("            max_size=16 << 20,  # Match Hub's unchanged frame cap; body plus JSON can exceed 1MiB.\n", ""),[os.environ.get('PYTHON','python3'),'-m','unittest','tools.sessionhelper.tests.test_mcp_message_length.LengthTest.test_production_connector_receives_one_mib_body_plus_json'],'MCP_LEN_EXACT_UTF8')
(out/'summary.json').write_text(json.dumps(results,indent=2));print(json.dumps(results));assert all(r['targeted_red'] for r in results)
