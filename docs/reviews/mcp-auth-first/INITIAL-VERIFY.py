from pathlib import Path
import json,gzip,hashlib,subprocess
p=Path(__file__).resolve().parent
def sha(b):return hashlib.sha256(b).hexdigest()
for x in json.loads((p/'RAW-INDEX.json').read_text()):
 b=(p/x['stored_path']).read_bytes();assert sha(b)==x['stored_sha256'];b=gzip.decompress(b) if x['stored_path'].endswith('.gz') else b;assert sha(b)==x['raw_sha256']
w=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=p).decode().strip());m=json.loads((p/'product-manifest.json').read_text())
for x in m['files']:
 b=subprocess.check_output(['git','show',m['source']+':'+x['path']],cwd=w);assert sha(b)==x['sha256'] and b==(w/x['path']).read_bytes()
print('INITIAL_VERIFY_OK code-fixed; full m8 Z3 pending, NOT_FULL_PASS')
