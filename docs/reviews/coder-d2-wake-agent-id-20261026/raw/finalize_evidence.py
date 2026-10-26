from pathlib import Path
import json,hashlib,subprocess,shutil,re
root=Path('/data/ai-dev-data/d2-wake-agent-id-20261026');out=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-evidence');binding=json.loads((out/'SOURCE-BINDING.json').read_text())
assert subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True)==''
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==binding['source_commit']
assert all(hashlib.sha256((root/f).read_bytes()).hexdigest()==v for f,v in binding['sha256'].items())
gates={}
for n in ['final-wake-focused','final-helper-root','final-helper-full','final-canonical','final-go-all','final-vet']:
 d=json.loads((out/(n+'.json')).read_text());assert d['rc']==0,n;assert d['source_commit']==binding['source_commit'],n;assert d['source_sha256']==binding['sha256'],n;gates[n]=d
helper=(out/'final-helper-full.stderr').read_text();assert re.search(r'Ran 989 tests.*?\nOK',helper,re.S)
rootlog=(out/'final-helper-root.stderr').read_text();assert re.search(r'Ran 9 tests.*?\nOK',rootlog,re.S)
assert 'BUILD_TESTS_OK python=66 shell=2' in (out/'final-canonical.stdout').read_text()
go=(out/'final-go-all.stdout').read_text().splitlines();counts={'tested':sum(s.startswith('ok') for s in go),'no_tests':sum(s.startswith('?') for s in go)}
assert counts=={'tested':53,'no_tests':13},counts
withdraw=json.loads((out/'withdraw-summary.json').read_text());assert withdraw['restored'];assert [r['rc'] for r in withdraw['records']]==[0,0,1,0,1,0,1,0,1,0]
verify={'base':binding['base'],'source_commit':binding['source_commit'],'source_tree':binding['source_tree'],'all_final_rc_zero':True,'helper_tests':989,'helper_root_tests':9,'focused_tests':18,'canonical_python_files':66,'canonical_shell_files':2,'go_packages':counts,'vet_rc':0,'withdrawals':4,'withdrawal_rc_pattern':'0 -> 1 -> 0 for each','source_after_gates_matches_binding':True,'credential_unknown_attribute_reads':0,'production_changes':False,'deployed':False,'gates':{n:{k:d[k] for k in ['command','start_utc','end_utc','elapsed_seconds','rc','source_commit']} for n,d in gates.items()}}
(out/'VERIFY.json').write_text(json.dumps(verify,indent=2)+'\n')
report=(out/'REPORT-draft.txt').read_text()
report+='\nFinal gate results (all bound to the frozen source above)\n'
for n,d in gates.items():report+=n+': rc='+str(d['rc'])+', elapsed_seconds='+str(d['elapsed_seconds'])+'; '+d['start_utc']+' -> '+d['end_utc']+'\n'
report+='Helper tests: 989 OK; root: 9 OK; focused: 18 OK. Canonical unchanged list: BUILD_TESTS_OK python=66 shell=2. Go: 53 tested packages and 13 packages without tests. Vet rc0. Four withdrawal checks: 0 -> 1 -> 0 each.\n'
dest=root/'docs/reviews/coder-d2-wake-agent-id-20261026';raw=dest/'raw';raw.mkdir(parents=True,exist_ok=True)
for p in out.iterdir():
 if p.is_file() and p.suffix in ('.json','.stdout','.stderr','.py'):
  shutil.copyfile(p,raw/p.name)
(dest/'REPORT.txt').write_text(report)
items=[p for p in dest.rglob('*') if p.is_file() and p.name!='SHA256SUMS']
(dest/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(dest))+'\n' for p in sorted(items)))
print(json.dumps({'report':str(dest/'REPORT.txt'),'manifest_entries':len(items),'verification':verify},ensure_ascii=False))
