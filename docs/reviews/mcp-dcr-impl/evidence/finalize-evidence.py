from pathlib import Path
import subprocess,json,hashlib,shutil,re,datetime
A=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/mcp-dcr');D=W/'docs/reviews/mcp-dcr-impl'
gates=json.loads((A/'gate-summary.json').read_text());z=json.loads((A/'z3-summary.json').read_text())
assert len(gates)==6 and all(g['rc']==0 or g['phase']=='go-packages-count3' for g in gates)
assert z['complete'] and len(z['runs'])==5 and z['green']>=3
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W,text=True).strip()==z['source']
subprocess.run(['python3',str(A/'summarize-go.py'),'migration-full','cmd-focused','focused-race'],check=True)
subprocess.run(['python3',str(A/'render-gate-tables.py')],check=True)
subprocess.run(['python3',str(A/'package-evidence.py')],check=True)
D.mkdir(parents=True,exist_ok=True);shutil.copytree(A/'delivery-evidence',D/'evidence',dirs_exist_ok=True)
for a,b in [('VERIFY-draft.py','VERIFY.py'),('MUTATIONS-draft.md','MUTATIONS.md'),('GATES-draft.md','GATES.md'),('SKIPS-draft.md','SKIPS.md'),('EVIDENCE-README-draft.md','EVIDENCE.md')]:shutil.copyfile(A/a,D/b)
s=(A/'REPORT-draft.md').read_text();lines=['固定源5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4，普通用户dev，env白名单，go1.22.2、Python3.12.3、PostgreSQL18.3。命令/UTC/rc/SHA见evidence，逐包表见GATES.md，逐名SKIP条件见SKIPS.md。','', '- canonical-final rc0：BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；两profile各1290、inventory97，无helper SKIP。','- 三包-count=3原始rc1：mcpgw 285顶层/1149含子项PASS、6/18 SKIP；extseat69/162 PASS、0 SKIP；m8 2990/5035 PASS、33/87 SKIP，唯一失败叶项TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to（view_mention_route_test.go:420 回执=[]）及其父项。同段3行lease release interrupted(9)清理诊断，GoTest日志归属不当因果证明。原始非绿未修改。','- 基线4a349ee5 TestViewMentionRoute -count=20也有同一子格/同一断言失败一次（19顶层/238含子项PASS），同段3行interrupted(9)；只作为继承形态对照，不拿不同命令的小样本估计失败率或证明增量因果。','- 依现行Z-3口径，在同源全新串行五轮，不沿用上面三遍，不择优：']
for run in z['runs']:lines.append(f"  - {run['phase']}：rc={run['rc']}，top PASS={run['top_pass']}，all PASS={run['all_pass']}，FAIL={run['failures']}，interrupted行={run['interrupted_lines']}。")
lines.append(f"- 五轮全绿{z['green']}/5，门要求至少3；允许名单外失败/非允许形态停报，未扩名单、未改store。初始门rc1与补证结果分列，不宣称整段测试零失败。")
for name in ['migration-full','cmd-focused','focused-race']:
 r=json.loads((A/(name+'.result.json')).read_text());t=json.loads((A/(name+'.summary.json')).read_text())['totals'];lines.append(f"- {name}：rc={r['rc']}，top PASS/SKIP={t['top_pass']}/{t['top_skip']}，all PASS/SKIP={t['all_pass']}/{t['all_skip']}；完整包/失败见GATES。")
lines.append('- cmd-focused已设DW_DCR_OLD_BINARY，TestDCRAdditiveMigrationOldBinaryAndForward须实跑PASS；focused-race未出现DATA RACE。')
strict=(A/'strict-final.log').read_text();lines.append('- strict-final rc0；原文结尾：'+strict.strip().splitlines()[-1])
s=s.replace('待填固定源各门实际结果、逐名SKIP与PG版本，不能用开发定向代替。','\n'.join(lines));s+='\n交件记录UTC：'+datetime.datetime.now(datetime.timezone.utc).isoformat()+'；已完成实现及作者门（含按口径Z-3补证），待qwen独立复核和组长终审。\n';(D/'REPORT.md').write_text(s)
readme=D/'EVIDENCE.md';readme.write_text(readme.read_text().replace('真实cookie/token、env或私有数据库','真实cookie/token、运行席位凭据环境或私有数据库'))
t=W/'openspec/changes/mcp-dcr/tasks.md';body=t.read_text()
for n in range(4,9):body=body.replace(f'- [ ] T-{n:02}',f'- [x] T-{n:02}')
body+='\n- 2026-11-19；coder56solzzc；完成T04～08：冻结源`5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4`（可git cat-file）；新增PG0115、公共DCR/绑定/限额/回退，V01～V07及41刀业务红/还原绿。固定源canonical和其它门通过；原三遍m8非绿及基线同格复现保留，按既定Z-3全新五轮结果详见`docs/reviews/mcp-dcr-impl/REPORT.md`。阻塞：T09独立复核/终审，真实IDE回调未测。下一步Git交qwen审阅，不推main、不部署、不出发布件。\n';t.write_text(body)
files=sorted(p for p in D.rglob('*') if p.is_file() and p.name!='SHA256SUMS')
(D/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(W))+'\n' for p in files))
print('FINAL_EVIDENCE',len(files),'files')
