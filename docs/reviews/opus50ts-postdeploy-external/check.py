import urllib.request,urllib.error,hashlib,json,datetime,sys
from pathlib import Path
root=Path(__file__).parent
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(NoRedirect)
base='https://dw123.cmjrt.com'
items=[('console','/console',None),('missing','/spec5ff-independent-missing-20261014',None),('old','/f/u_zhangzongchu/opus50ts/26/1013/259d8d5ab17d03784be3257a4af7f683.bin','fdad07d14ae9cf1e01565f782b2fc42c9e5f0b1aabd22a0eace270b0575d3ad8'),('new','/f/u_zhangzongchu/opus50ts/26/1014/ff50eddf20c180055173c1d3fb4c95a8.txt','08abeaa54bc632f95f1b97623095bf81e02c853c014402c545fa89a978956b34'),('invalid','/f/not-a-valid-shape',None),('valid_missing','/f/u_zhangzongchu/opus50ts/26/1014/00000000000000000000000000000000.txt',None)]
if sys.argv[1]=='1':items += [('renderer','/renderer-canary-sse.html','b2867c33661c82f49288d0d86d1d82b1cc5cec82c73998d4a3706387ec64f658'),('health','/healthz',None),('root','/',None)]
results=[]
for name,path,sha in items:
 row={'name':name,'url':base+path,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 try:
  try:r=opener.open(urllib.request.Request(base+path,headers={'User-Agent':'DingWei-independent-readonly-verification'}),timeout=20)
  except urllib.error.HTTPError as e:r=e
  b=r.read();row.update(status=r.code,bytes=len(b),sha256=hashlib.sha256(b).hexdigest(),location=r.headers.get('Location'),commit_marker_2d5bde7=b'2d5bde7' in b)
  if name=='console':row['pass']=r.code in [401,302,303,307,308] and (r.code==401 or 'login' in (r.headers.get('Location') or ''))
  elif name in ['missing','invalid','valid_missing']:row['pass']=r.code==404
  elif name in ['new','old']:row['pass']=r.code==200 and row['sha256']==sha
  elif name=='renderer':row['pass']=row['sha256']==sha if r.code==200 else None
  else:row['pass']=None
 except Exception as e:row.update(error=type(e).__name__+': '+str(e),passed=False)
 results.append(row)
(root/('round-'+sys.argv[1]+'.json')).write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(results,ensure_ascii=False))
