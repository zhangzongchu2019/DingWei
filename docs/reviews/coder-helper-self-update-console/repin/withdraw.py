from pathlib import Path
import tempfile,json,subprocess
root=Path.cwd();p=root/'internal/admin/admin.go';s=p.read_text()
anchor='<p><button name=op value=send>下发已有制品地址</button></p>'
assert s.count(anchor)==1
with tempfile.TemporaryDirectory(prefix='coder-repin-upload-') as td:
 q=Path(td)/'admin.go'
 q.write_text(s.replace(anchor,'<p>上传制品 <input type=file name=artifact> <button name=op value=send>下发</button> <button name=op value=package_self>打包当前 sessionHelper</button></p>'))
 overlay=Path(td)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(p):str(q)}}))
 result=subprocess.run(['go','test','-race','-overlay='+str(overlay),'./internal/admin','-run','^TestGateDoorABytesMatchThePinnedBaseline$','-count=1'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
 print(result.stdout,end='')
 assert result.returncode==1 and '输出字节与已批准基线' in result.stdout
 print('Expected byte-pin assertion rc1 confirmed')
