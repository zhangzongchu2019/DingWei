from pathlib import Path
import hashlib
base=Path(__file__).resolve().parent.parent/'retirement'
a=(base/'doorA-parent.txt').read_bytes();b=(base/'doorA-current.txt').read_bytes()
assert hashlib.sha256(a).hexdigest()=='ea1766cb001d2cc60f0235d3b3b4725f4f18776847b1102af88651785f4356cd'
assert hashlib.sha256(b).hexdigest()=='4c7b45af8b8b0f4f950a1eae23ddda12175a5ae4daf0d67110910e87a456f859'
left=a.decode().split('=== ')[1:];right=b.decode().split('=== ')[1:]
assert len(left)==len(right)==20
changed=[x.splitlines()[0] for x,y in zip(left,right) if x!=y]
assert changed==['AUTHED GET /admin/provision'],changed
expected=a.decode().replace('<form method=post action=/admin/provision enctype="multipart/form-data">','<p>公开上传与在线打包已退役（public_upload_retired）。helper 升级请使用 Console 中运维登记的版本；已有 skill/MCP 制品地址下发不变。</p>\n<form method=post action=/admin/provision>').replace('<p>上传制品 <input type=file name=artifact> <button name=op value=send>下发</button> <button name=op value=package_self>打包当前 sessionHelper</button></p>','<p><button name=op value=send>下发已有制品地址</button></p>')
assert expected==b.decode(),'unauthorized additional dump difference'
print('20 records retained;19 identical; exact authorized producer retirement delta only')
