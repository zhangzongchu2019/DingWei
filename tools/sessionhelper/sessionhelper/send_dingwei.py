#!/usr/bin/env python3
"""DingWei 发信脚本（走 sessionHelper 本机 IPC）。用法与旧版完全一致。

  send.py <收件会话名或完整地址> <正文...>
  send.py <收件会话名或完整地址> --file <正文文件>

与旧版的区别（T-092/T-085，业主 2026-09-12 定）：
  · 不再自开 WebSocket、不再读 SH_SECRET —— 经 helper **已认证**的连接发出
  · `from` 是**真实会话名**，不再是临时的 <短名>note
  · 🔴 **读回执并反映到退出码** —— 旧版写完就无条件打印 "sent"，从不读应答，
    这是 2026-09-12 组长误判「消息已送达」整整一轮的直接原因
  · socket 不可用时**失败并明说，绝不回落旧 WS 路径**（回落等于绕过 M2、且留着凭据暴露）

回执分 accepted／unknown／definitive_failure；local_queued 只承诺本地持久受理。
unknown 保留 request_id，使用 --status 查询，不自动重发或换新 ID。
"""
import json
import os
import socket
import sqlite3
import sys
import uuid

SOCK = os.environ.get("SH_MCP_IPC_SOCK", "/run/sessionhelper/dingwei-mcp.sock")
TIMEOUT = 10.0
try:
    from .ipc_limits import MESSAGE_BODY_MAX_BYTES, IPC_REQUEST_MAX_BYTES
except ImportError:
    # app installs this CLI as standalone ~/.dingwei/send.py, without a package.
    MESSAGE_BODY_MAX_BYTES = 1 << 20
    IPC_REQUEST_MAX_BYTES = 6 * MESSAGE_BODY_MAX_BYTES + (64 << 10)


def complete_addr(to: str) -> str:
    """裸会话名补成 <名>#<key_id>。

    helper 侧本应补全，但 defb424f 误删了该逻辑（T-092，修复未部署）⇒ 此处自补，
    使本脚本在修复上线前后都正确。SH_KEY_ID 是标识符不是密钥，读它不构成凭据暴露；
    **SH_SECRET 全程不读**。
    """
    to = to.strip()
    if "#" in to or not to:
        return to
    key = os.environ.get("SH_KEY_ID", "").strip()
    return f"{to}#{key}" if key else to


def unsupported_sub_agent_target(to: str) -> bool:
    """Phase 1 does not negotiate child routing; never truncate to the root."""
    name = to.strip().split("#", 1)[0]
    return bool(name) and len(name.split(".")) >= 3


def remember_request(request_id, target, body, meta=None):
    """Persist the caller identity before the first IPC write; never store body."""
    import hashlib
    import re
    rid=request_id or uuid.uuid4().hex
    if not isinstance(rid,str) or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}',rid):
        raise ValueError('invalid_request_id')
    digest=hashlib.sha256(json.dumps([target,body,meta or {}],ensure_ascii=False,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    directory=os.path.join(os.path.expanduser('~'),'.dingwei');os.makedirs(directory,mode=0o700,exist_ok=True)
    path=os.path.join(directory,'outbound-caller.db')
    fd=os.open(path,os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600);os.fchmod(fd,0o600);os.close(fd)
    with sqlite3.connect(path,timeout=2) as db:
        db.execute('PRAGMA synchronous=FULL')
        db.execute('CREATE TABLE IF NOT EXISTS request(id TEXT PRIMARY KEY,digest TEXT NOT NULL)')
        db.execute('BEGIN IMMEDIATE')
        old=db.execute('SELECT digest FROM request WHERE id=?',(rid,)).fetchone()
        if old and old[0]!=digest:raise ValueError('idempotency_conflict')
        if not old:
            if db.execute('SELECT count(*) FROM request').fetchone()[0]>=65536:raise ValueError('caller_journal_full')
            db.execute('INSERT INTO request VALUES(?,?)',(rid,digest))
        db.commit()
    return rid


def ipc_send(to: str, body: str, request_id=None, *, status=False) -> dict:
    if len(body.encode('utf-8'))>MESSAGE_BODY_MAX_BYTES:
        return {'ok':False,'error':'body_too_large','outcome':{'outcome':'definitive_failure','accepted_by':'none','phase':'rejected','reason':'body_too_large','retryable':False}}
    rid=request_id if status else remember_request(request_id,to,body,{'ipc_body_amr_v1':True})
    payload={'type':'mcp_send_status','request_id':rid} if status else {'type':'mcp_send','to':to,'body':body,'request_id':rid,'ipc_body_amr_v1':True}
    raw=json.dumps(payload,ensure_ascii=False).encode()+b'\n'
    unknown={'phase':'unknown','accepted_by':'unknown','outcome':'unknown','request_id':rid,'status_ref':rid,'retryable':False,'reason':'ipc_confirmation_unavailable'}
    if len(raw)-1>IPC_REQUEST_MAX_BYTES:return {'ok':False,'outcome':dict(unknown,outcome='definitive_failure',accepted_by='none',phase='rejected',reason='request_too_large')}
    sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);sock.settimeout(TIMEOUT)
    try:
        sock.connect(SOCK);sock.sendall(raw);buf=b''
        while b'\n' not in buf:
            ch=sock.recv(4096)
            if not ch:break
            buf+=ch
            if len(buf)>65536:raise ValueError('invalid_response')
        resp=json.loads(buf.split(b'\n',1)[0])
        if not isinstance(resp,dict) or type(resp.get('ok')) is not bool:raise ValueError('invalid_response')
        return resp
    except (OSError,ValueError):
        return {'ok':False,'outcome':unknown,'request_id':rid}
    finally:sock.close()


def main() -> None:
    args=sys.argv[1:];rid=None
    if '--request-id' in args:
        i=args.index('--request-id')
        if i+1>=len(args):sys.exit('--request-id requires an ID')
        rid=args[i+1];del args[i:i+2]
    if len(args)==2 and args[0]=='--status':
        resp=ipc_send('','',args[1],status=True)
    else:
        if len(args)<2:sys.exit('usage: send.py target [--request-id ID] body | --file PATH; send.py --status ID')
        if unsupported_sub_agent_target(args[0]):sys.exit('发送失败：三段及以上子 Agent 目标当前不受支持；未降级为父会话，未发送。')
        recipient=complete_addr(args[0])
        try:
            body=open(args[2],encoding='utf-8').read() if args[1]=='--file' and len(args)>2 else ' '.join(args[1:])
        except (OSError,ValueError):
            print(json.dumps({'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':'input_unavailable','retryable':False}));sys.exit(1)
        if not body.strip():sys.exit('empty body')
        try:resp=ipc_send(recipient,body,rid)
        except (ValueError,OSError,sqlite3.Error):
            print(json.dumps({'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':'caller_journal_rejected','retryable':False}));sys.exit(1)
    out=resp.get('outcome')
    if resp.get('ok') is False and not isinstance(out,dict):
        out={'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':('helper_unavailable' if isinstance(resp.get('error'),str) and resp['error'].startswith('unavailable:') else 'helper_rejected'),'request_id':resp.get('request_id',rid),'retryable':False}
    if not isinstance(out,dict) or out.get('outcome') not in ('accepted','unknown','definitive_failure'):
        out={'phase':'unknown','accepted_by':'unknown','outcome':'unknown','request_id':resp.get('request_id',rid),'reason':'unconfirmed_legacy_receipt','retryable':False}
    print(json.dumps(out,ensure_ascii=False))
    if out['outcome']=='definitive_failure':print('发送失败：'+out.get('reason','rejected'),file=sys.stderr)
    if out['outcome']!='accepted':sys.exit(2 if out['outcome']=='unknown' else 1)


if __name__=='__main__':main()
