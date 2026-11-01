"""Credential boundary target mutations via Go overlay, never host credentials."""
import argparse,json,os,pathlib,subprocess,tempfile,sys
ROOT=pathlib.Path(__file__).resolve().parents[2]
FILE='internal/specread/github.go'
SCOPE='systemd := filepath.IsAbs(directory) && filepath.Dir(filepath.Clean(g.CredentialFile)) == directory'
CASES=[
 ('restore-0077',SCOPE,'systemd := false','directory_0440'),
 ('outside-scope',SCOPE,'systemd := filepath.IsAbs(directory)','outside_directory_0440'),
 ('file-other-bits','st.Mode().Perm()&0007 != 0','false','other_read_0444'),
 ('file-symlink','os.Lstat(g.CredentialFile)','os.Stat(g.CredentialFile)','symlink'),
 ('parent-other-write','parent.Mode().Perm()&0022 != 0','false','parent_other_write'),
 ('parent-group-write','parent.Mode().Perm()&0022 != 0','parent.Mode().Perm()&0002 != 0','parent_group_write'),
 ('environment-mismatch',SCOPE,'systemd := filepath.IsAbs(directory)','environment_elsewhere'),
 ('strict-fallback','else if st.Mode().Perm()&0077 != 0','else if false','unset_environment'),
 ('legacy-private-normal','else if st.Mode().Perm()&0077 != 0','else if true','outside_private_0600'),
 ('parent-symlink','os.Lstat(directory)','os.Stat(directory)','parent_symlink'),
 ('size-cap','st.Size() > 4096','false','oversized_content'),
 ('minimum-length','len(v) < 10','false','short_content'),
 ('content-whitespace','strings.ContainsAny(v, "\\r\\n\\x00 \\t")','false','embedded_whitespace'),
]
def main():
 p=argparse.ArgumentParser();p.add_argument('--output',type=pathlib.Path,required=True);a=p.parse_args();a.output.mkdir(parents=True,exist_ok=True);results=[]
 with tempfile.TemporaryDirectory(prefix='r5-cred-mut-') as scratch:
  scratch=pathlib.Path(scratch);env={k:v for k,v in os.environ.items() if k in ('PATH','LANG','TZ','GOCACHE','GOMODCACHE','GOPATH')};env['HOME']=str(scratch/'home');env['HUB_TEST_PG_STRICT']='1'
  for name,old,new,case in CASES:
   text=(ROOT/FILE).read_text();assert text.count(old)==1,(name,text.count(old));mutant=scratch/(name+'.go');mutant.write_text(text.replace(old,new,1));overlay=scratch/(name+'.json');overlay.write_text(json.dumps({'Replace':{str(ROOT/FILE):str(mutant)}}))
   command=['go','test','-overlay='+str(overlay),'./internal/specread','-count=1','-run','^TestGitHubSystemdCredentialBoundary$/^'+case+'$','-timeout=30s'];r=subprocess.run(command,cwd=ROOT,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);marker='R5_CRED_'+case+' accepted='
   ok=r.returncode!=0 and marker in r.stdout and '[build failed]' not in r.stdout;(a.output/(name+'.log')).write_text(r.stdout);results.append({'mutation':name,'rc':r.returncode,'assertion':marker,'matched':ok});print(json.dumps(results[-1]),flush=True)
   if not ok:break
 (a.output/'results.json').write_text(json.dumps(results,indent=2)+'\n');return 0 if len(results)==len(CASES) and all(r['matched'] for r in results) else 1
if __name__=='__main__':sys.exit(main())
