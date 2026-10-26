from pathlib import Path
import subprocess,json,hashlib,shutil,re
root=Path('/data/ai-dev-data/r39-1-integration-02313');out=Path('/data/ai-dev-data/r39-1-integration-02313-evidence');binding=json.loads((out/'SOURCE-BINDING.json').read_text())
assert subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True)==''
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==binding['source_commit']
assert all(hashlib.sha256((root/f).read_bytes()).hexdigest()==v for f,v in binding['sha256'].items())
gates={n:json.loads((out/(n+'.json')).read_text()) for n in ['version','helper-root','helper-full','canonical','go-all','vet','browser-version-pin']}
for n,d in gates.items():assert d['rc']==0 and d['source_commit']==binding['source_commit'] and d['source_sha256']==binding['sha256'],n
assert re.search(r'Ran 989 tests.*?\nOK',(out/'helper-full.stderr').read_text(),re.S)
assert re.search(r'Ran 9 tests.*?\nOK',(out/'helper-root.stderr').read_text(),re.S)
assert 'BUILD_TESTS_OK python=66 shell=2' in (out/'canonical.stdout').read_text()
assert 'TOTAL 14 Chromium helper-update UI checks' in (out/'browser-version-pin.stdout').read_text()
go=(out/'go-all.stdout').read_text().splitlines();counts={'tested':sum(s.startswith('ok') for s in go),'no_tests':sum(s.startswith('?') for s in go)};assert counts=={'tested':53,'no_tests':13}
assert (out/'version.stdout').read_text().count('--- PASS:')==4
w=json.loads((out/'withdraw-summary.json').read_text());assert w['restored'];assert [r['rc'] for r in w['records']]==[0,1,0]*5
m4=(out/'M4-coherent-rollback-withdrawn.stdout').read_text();assert 'FAIL: TestHelper02313ReleasePins' in m4;assert m4.count('--- PASS:')==3
verify={'source_commit':binding['source_commit'],'source_tree':binding['source_tree'],'version':'0.2.3.13','base':binding['base'],'approved_d2_tip':binding['approved_d2_tip'],'original_d2_history_preserved':True,'d2_product_implementation_unchanged':True,'all_gate_rc_zero':True,'helper_tests':989,'helper_root_tests':9,'version_tests':4,'browser_checks':14,'canonical_python':66,'canonical_shell':2,'go_packages':counts,'vet_rc':0,'version_withdrawals':5,'withdrawal_rcs':[0,1,0]*5,'source_sha_after_gates_unchanged':True,'deployed':False,'mandatory_rollout_order':'Hub first; verify authenticated X-DW-Agent-ID; then helper seat by seat','known_limitations':['S-HUB-PROCESSED-AS-INJECTED: missing identity processed ACK is not model admission despite View cli_injected event'],'gates':{n:{k:d[k] for k in ['command','start_utc','end_utc','elapsed_seconds','rc','source_commit']} for n,d in gates.items()}}
(out/'VERIFY.json').write_text(json.dumps(verify,indent=2)+'\n')
report=(out/'REPORT-draft.txt').read_text()+'\n最终门结果（全部绑定上述同一冻结源）\n'
for n,d in gates.items():report+=n+': rc='+str(d['rc'])+', '+str(d['elapsed_seconds'])+'s; '+d['start_utc']+' -> '+d['end_utc']+'\n'
report+='版本4项、helper989＋根目录9、canonical python66/shell2、浏览器14格、Go53测试包＋13无测试包、vet全部通过。五项版本撤修分别0→1→0，恢复后的版本/依赖锁SHA均一致。\n'
dest=root/'docs/reviews/coder-r39-1-integration-02313-20261026';raw=dest/'raw';raw.mkdir(parents=True,exist_ok=True)
for p in out.iterdir():
 if p.is_file() and p.suffix in ('.json','.stdout','.stderr','.py'):shutil.copyfile(p,raw/p.name)
(dest/'REPORT.txt').write_text(report)
items=[p for p in dest.rglob('*') if p.is_file() and p.name!='SHA256SUMS']
(dest/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(dest))+'\n' for p in sorted(items)))
# Retained D2 evidence must survive the fast-forward without missing ignored names.
prior=root/'docs/reviews/coder-d2-wake-agent-id-20261026'
for name in ['SHA256SUMS','SHA256SUMS-R1-R2']:
 for line in (prior/name).read_text().splitlines():
  h,n=line.split('  ',1);assert hashlib.sha256((prior/n).read_bytes()).hexdigest()==h,n
print(json.dumps({'manifest_entries':len(items),'verification':verify},ensure_ascii=False))
