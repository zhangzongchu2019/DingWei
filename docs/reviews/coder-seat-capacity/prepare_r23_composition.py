import argparse,hashlib,io,json,pathlib,re,subprocess,tarfile
parser=argparse.ArgumentParser();parser.add_argument('--repo',type=pathlib.Path,required=True);parser.add_argument('--lab',type=pathlib.Path,required=True);args=parser.parse_args();repo=args.repo.resolve();lab=args.lab.resolve();lab.mkdir(parents=True,exist_ok=True);(lab/'evidence').mkdir(exist_ok=True)
source='26f506c3ef47014db59b339133943c98585a8c54';r23='fa07ddce2ab239307ccaf5d5ab14fc957dcf4b11'
p=subprocess.run(['git','merge-tree','--write-tree',source,r23],cwd=repo,text=True,capture_output=True)
assert p.returncode==1,p.stdout+p.stderr
virtual=p.stdout.splitlines()[0]
paths=set(re.findall(r'^100644 \w+ [123]\t(.+)$',p.stdout,re.M));assert paths=={'internal/nodeagent/policy.go'},paths
root=lab/'capacity-r23-composition';assert not root.exists();root.mkdir()
with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','archive',virtual],cwd=repo))) as archive:archive.extractall(root)
file=root/'internal/nodeagent/policy.go';raw=file.read_text();pattern=re.compile(r'<<<<<<< [^\n]+\n(.*?)=======\n(.*?)>>>>>>> [^\n]+\n',re.S);resolutions=[]
def resolve(match):
 ours,theirs=match.groups()
 if 'StateWriteBudget' in ours and 'InodeEmergencyReservePercent' in theirs:
  inode=next(x for x in theirs.splitlines(True) if 'InodeEmergencyReservePercent ' in x)
  old=next(x for x in ours.splitlines(True) if 'EmergencyReservePercent ' in x)
  result=ours.replace(old,old+inode)
 elif 'MINIMUM_SEAT_CPU_MILLICORES' in ours and 'INODE_EMERGENCY_RESERVE_PERCENT' in theirs:
  before='"RESERVE_INODE_PERCENT": &p.ReserveInodePercent'
  assert ours.count(before)==1
  result=ours.replace(before,before+', "INODE_EMERGENCY_RESERVE_PERCENT": &p.InodeEmergencyReservePercent')
 else:raise AssertionError('unrecognized conflict')
 resolutions.append({'ours':ours,'theirs':theirs,'combined':result});return result
fixed,count=pattern.subn(resolve,raw);assert count==2,count;assert '<<<<<<<' not in fixed
file.write_text(fixed);subprocess.run(['gofmt','-w',str(file)],check=True)
assert 'InodeEmergencyReservePercent: 50' in file.read_text();assert 'p.InodeEmergencyReservePercent' in file.read_text()
overlay=repo/'docs/reviews/coder-seat-capacity/capacity_n2_compat_test.go.fixture'
(root/'internal/nodeagent/capacity_n2_compat_test.go').write_bytes(overlay.read_bytes())
subprocess.run(['gofmt','-w',str(root/'internal/nodeagent/capacity_n2_compat_test.go')],check=True)
result={'B_source':source,'r23':r23,'virtual_tree_with_conflict_markers':virtual,'conflicts':list(paths),'policy_resolutions':resolutions,'resolved_policy_sha256':hashlib.sha256(file.read_bytes()).hexdigest(),'overlay_sha256':hashlib.sha256((root/'internal/nodeagent/capacity_n2_compat_test.go').read_bytes()).hexdigest(),'not_an_integration_commit':True}
(lab/'evidence'/'capacity-r23-composition.json').write_text(json.dumps(result,indent=2)+'\n');(lab/'evidence'/'capacity-r23-merge-tree.log').write_text(p.stdout+p.stderr)
print(json.dumps({'source':source,'r23':r23,'virtual':virtual,'resolved':list(paths)}))
