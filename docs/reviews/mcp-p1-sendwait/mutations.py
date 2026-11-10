import pathlib,subprocess,os,json,argparse,time
parser=argparse.ArgumentParser();parser.add_argument('--root',type=pathlib.Path,required=True);parser.add_argument('--out',type=pathlib.Path,required=True);args=parser.parse_args();root=args.root;out=args.out;out.mkdir(exist_ok=True,parents=True)
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_','HUB_TEST_PG_DSN'))};env.update(HOME='/tmp/sendwait-mut-home',HUB_TEST_PG_STRICT='1',GOPATH=subprocess.check_output(['go','env','GOPATH'],text=True).strip(),GOCACHE=subprocess.check_output(['go','env','GOCACHE'],text=True).strip());pathlib.Path(env['HOME']).mkdir(exist_ok=True)
reports=[]
def run(name,path,change,package,test,marker):
 p=root/path;original=p.read_bytes();text=original.decode();new=change(text);assert new!=text,name
 p.write_text(new);started=time.time()
 try:
  r=subprocess.run(['go','test','-count=1',package,'-run',test],cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
  (out/(name+'.log')).write_text(r.stdout);v={'mutation':name,'rc':r.returncode,'marker':marker,'targeted_red':r.returncode!=0 and marker in r.stdout,'seconds':round(time.time()-started,2)};reports.append(v);print(json.dumps(v),flush=True)
 finally:p.write_bytes(original);assert p.read_bytes()==original
run('remove-warning','internal/extshared/external_reply.go',lambda s:s.replace('header += "\\n" + warning','_ = warning'),'./internal/m8','TestExternalSeatSenderReplyRoundTripPGAndHub','MCP_EXTERNAL_REPLY_WARNING_ENABLED')
run('disable-generator-switch','internal/m8/reply_to_origin.go',lambda s:s.replace('!ok || !extshared.ExternalAutoReplyAllowed(p.hub.Identity.FullName(ctx, target))','!ok || (false && !extshared.ExternalAutoReplyAllowed(p.hub.Identity.FullName(ctx, target)))'),'./internal/m8','TestExternalSeatSenderReplyRoundTripPGAndHub','MCP_EXTERNAL_REPLY_SWITCH')
run('bypass-exit-switch','internal/m8/extseat.go',lambda s:s.replace('if metaBool(env.Meta, metaKeyReplyOriginReturn) && !extshared.ExternalAutoReplyAllowed(h.Identity.FullName(ctx, source)) {','if false {'),'./internal/m8','TestExternalSeatSenderReplyRoundTripPGAndHub','MCP_EXTERNAL_REPLY_BYPASS')
run('check-claims','internal/extseat/inbox_controls.go',lambda s:s.replace('return s.readPending(ctx, a, nil, false)','_, _ = s.claim(ctx, a); return s.readPending(ctx, a, nil, false)'),'./internal/extseat','TestCheckClearReadOnlyScopePG','MCP_CHECK_READ_ONLY')
run('remove-clear-confirm','internal/extseat/inbox_controls.go',lambda s:s.replace('if !confirm {','if false {'),'./internal/extseat','TestCheckClearReadOnlyScopePG','MCP_CLEAR_CONFIRM')
run('missing-scope-allowed','internal/mcpgw/tools.go',lambda s:s.replace('case "recv", "check", "wait_for_messages":','case "recv", "wait_for_messages":').replace('func knownTool(name string) bool {','func knownTool(name string) bool { if name=="check" { return true }'),'./internal/mcpgw','TestSendWaitDefaultsReplyTimeoutAndAliases','MCP_SHORT_SCOPE_MAPPING')
run('alias-divergence','internal/mcpgw/tools.go',lambda s:s.replace('case "recv", "wait_for_messages":\n\t\tvar a struct','case "recv", "wait_for_messages":\n if name=="recv"{return map[string]any{"messages":[]any{}},nil}\n\t\tvar a struct'),'./internal/mcpgw','TestSendWaitDefaultsReplyTimeoutAndAliases','MCP_ALIAS_EQUIVALENT')
run('write-every-wake','internal/extseat/inbox_controls.go',lambda s:s.replace('ready, e := s.readPending(ctx, a, &since, true)','_ = s.presence(ctx,a)\n ready, e := s.readPending(ctx, a, &since, true)'),'./internal/extseat','TestSendWaitNewReplyAndReadOnlyPollPG','MCP_SENDWAIT_READ_ONLY_IDLE')
run('identity-lookup-failopen','internal/extshared/external_reply.go',lambda s:s.replace('if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(owner) == "" {\n\t\treturn false','if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(owner) == "" {\n\t\treturn true'),'./internal/extshared','TestExternalReplyDefaultAndExactConfig','MCP_EXTERNAL_REPLY_IDENTITY_FAIL_CLOSED')
(out/'summary.json').write_text(json.dumps(reports,indent=2));assert all(v['targeted_red'] for v in reports)
