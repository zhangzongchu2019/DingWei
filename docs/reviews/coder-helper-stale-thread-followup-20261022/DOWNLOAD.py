import urllib.request,json,pathlib,hashlib,base64,tarfile,concurrent.futures,datetime,os
r=pathlib.Path(__file__).parent

def run(v):
 d=json.loads((r/(v+'-metadata.json')).read_text()); a=r/(v+'.tgz')
 with urllib.request.urlopen(d['dist']['tarball'],timeout=90) as f,a.open('wb') as o:
  while b:=f.read(1024*1024):o.write(b)
 data=a.read_bytes();assert hashlib.sha1(data).hexdigest()==d['dist']['shasum'];assert 'sha512-'+base64.b64encode(hashlib.sha512(data).digest()).decode()==d['dist']['integrity']
 dest=r/v;dest.mkdir(mode=0o700)
 with tarfile.open(a) as t:
  members=[m for m in t.getmembers() if m.isfile() and m.name.endswith('/bin/codex')];assert len(members)==1,[m.name for m in members]
  p=dest/'codex';p.write_bytes(t.extractfile(members[0]).read());p.chmod(0o700)
  pkg=json.load(t.extractfile('package/package.json'));assert pkg['version']==v+'-linux-x64'
 out={'version':v,'registry_metadata_url':'https://registry.npmjs.org/@openai%2fcodex/'+v+'-linux-x64','tarball_url':d['dist']['tarball'],'archive_sha256':hashlib.sha256(data).hexdigest(),'archive_bytes':len(data),'sha1_matches':True,'sha512_integrity_matches':True,'binary_member':members[0].name,'binary_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'binary_bytes':p.stat().st_size,'package_json':pkg,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'not_from_PATH':True,'signature_verification':'Registry signatures recorded, not independently cryptographically verified'}
 (r/(v+'-PROVENANCE.json')).write_text(json.dumps(out,indent=2)+'\n');return out
with concurrent.futures.ThreadPoolExecutor(2) as e:
 for out in e.map(run,['0.161.0','0.163.0']):print(json.dumps(out))
