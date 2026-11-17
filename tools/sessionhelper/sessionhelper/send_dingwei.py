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
import time
from contextlib import contextmanager
from pathlib import Path

SOCK = os.environ.get("SH_MCP_IPC_SOCK", "/run/sessionhelper/dingwei-mcp.sock")
TIMEOUT = 10.0
try:
    from .ipc_limits import MESSAGE_BODY_MAX_BYTES, IPC_REQUEST_MAX_BYTES
except ImportError:
    # app installs this CLI as standalone ~/.dingwei/send.py, without a package.
    MESSAGE_BODY_MAX_BYTES = 1 << 20
    IPC_REQUEST_MAX_BYTES = 6 * MESSAGE_BODY_MAX_BYTES + (64 << 10)


def complete_addr(to: str) -> str:
    """Preserve the caller destination; the authenticated directory binds its ID.

    A sender's inherited SH_KEY_ID is not the recipient's owner. The old
    implicit-suffix workaround is superseded by the durable helper protocol.
    Explicit verified legacy addresses remain literal inputs.
    """
    return to.strip()


def unsupported_sub_agent_target(to: str) -> bool:
    """Phase 1 does not negotiate child routing; never truncate to the root."""
    name = to.strip().split("#", 1)[0]
    return bool(name) and len(name.split(".")) >= 3


CALLER_RECOVERY_WINDOW = 24 * 3600
CALLER_RETENTION_MARGIN = 2 * 3600
DEFAULT_CALLER_RETENTION = 7 * 24 * 3600
MAX_CALLER_RETENTION = 30 * 24 * 3600
CALLER_MAX_RECORDS = 65536
CALLER_CLEANUP_BATCH = 256


def parse_caller_retention(raw=""):
    try:
        value = DEFAULT_CALLER_RETENTION if str(raw).strip() == "" else int(str(raw).strip())
    except (ValueError, TypeError):
        raise ValueError('SH_OUTBOUND_CALLER_RETENTION_SECONDS must be integer seconds in (93600,2592000]') from None
    if not CALLER_RECOVERY_WINDOW + CALLER_RETENTION_MARGIN < value <= MAX_CALLER_RETENTION:
        raise ValueError('SH_OUTBOUND_CALLER_RETENTION_SECONDS must be integer seconds in (93600,2592000]')
    return value


class CallerJournalRejected(ValueError):
    def __init__(self, reason, request_id, retryable=False):
        self.reason, self.request_id, self.retryable = reason, request_id, retryable
        super().__init__(reason)

    def outcome(self):
        return {'phase':'rejected','accepted_by':'none','outcome':'definitive_failure',
                'reason':self.reason,'request_id':self.request_id,'status_ref':self.request_id,
                'retryable':self.retryable}


@contextmanager
def _caller_db(now):
    directory = os.path.join(os.path.expanduser('~'), '.dingwei')
    os.makedirs(directory, mode=0o700, exist_ok=True)
    path = os.path.join(directory, 'outbound-caller.db')
    fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    os.fchmod(fd, 0o600)
    os.close(fd)
    db = sqlite3.connect(path, timeout=2, isolation_level=None)
    try:
        db.execute('PRAGMA synchronous=FULL')
        db.execute('BEGIN IMMEDIATE')
        db.execute('CREATE TABLE IF NOT EXISTS request(id TEXT PRIMARY KEY,digest TEXT NOT NULL,created REAL NOT NULL)')
        if 'created' not in {row[1] for row in db.execute('PRAGMA table_info(request)')}:
            db.execute('ALTER TABLE request ADD COLUMN created REAL NOT NULL DEFAULT 0')
            db.execute('UPDATE request SET created=?', (now,))
        db.execute('CREATE INDEX IF NOT EXISTS caller_created ON request(created,id)')
        db.execute('CREATE TABLE IF NOT EXISTS caller_outbox(path TEXT PRIMARY KEY)')
        db.execute('CREATE TABLE IF NOT EXISTS caller_policy(id INTEGER PRIMARY KEY CHECK(id=1),retention INTEGER NOT NULL)')
        db.commit()
        yield db
    finally:
        db.close()


def register_caller_outbox(path, retention=DEFAULT_CALLER_RETENTION):
    """Register before IPC/writer startup; callers never guess the outbox path."""
    retention = parse_caller_retention(retention)
    with _caller_db(time.time()) as db:
        db.execute('BEGIN IMMEDIATE')
        db.execute('INSERT OR IGNORE INTO caller_outbox(path) VALUES(?)', (os.path.abspath(path),))
        db.execute('INSERT OR REPLACE INTO caller_policy(id,retention) VALUES(1,?)', (retention,))
        db.commit()


@contextmanager
def _caller_pending(db):
    # main caller BEGIN IMMEDIATE is already held, serializing registration.
    # Outbox writers only lock their own DB; they never acquire the caller lock.
    # Keep all reserved outbox write locks until the caller transaction ends.
    opened = []
    pending = set()
    try:
        paths = [row[0] for row in db.execute('SELECT path FROM caller_outbox ORDER BY path')]
        if not paths:
            yield None  # Unknown association is not proof of no pending work.
            return
        deadline = time.monotonic() + .25
        try:
            for path in paths:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise sqlite3.OperationalError('outbox lock budget exhausted')
                other = sqlite3.connect(Path(path).as_uri()+'?mode=rw',uri=True,
                                        timeout=min(.05,remaining),isolation_level=None)
                opened.append(other)
                other.execute('BEGIN IMMEDIATE')
                pending.update(row[0] for row in other.execute(
                    "SELECT request_id FROM outbound_request WHERE state IN ('local_queued','unknown')"))
        except (sqlite3.Error, OSError, ValueError):
            pending = None  # No cleanup on a missing/locked/corrupt outbox.
        yield pending
    finally:
        for other in reversed(opened):
            other.close()


def remember_request(request_id, target, body, meta=None):
    """Persist caller identity before IPC; retain pending IDs across bounded GC."""
    import hashlib
    import re
    rid = request_id or uuid.uuid4().hex
    if not isinstance(rid,str) or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}',rid):
        raise ValueError('invalid_request_id')
    digest = hashlib.sha256(json.dumps([target,body,meta or {}],ensure_ascii=False,
                          sort_keys=True,separators=(',',':')).encode()).hexdigest()
    now = time.time()
    with _caller_db(now) as db:
        db.execute('BEGIN IMMEDIATE')
        old = db.execute('SELECT digest FROM request WHERE id=?',(rid,)).fetchone()
        if old and old[0] != digest:
            raise CallerJournalRejected('idempotency_conflict',rid,False)
        if old:
            db.commit()
            return rid
        policy = db.execute('SELECT retention FROM caller_policy WHERE id=1').fetchone()
        retention = parse_caller_retention(policy[0] if policy else os.environ.get('SH_OUTBOUND_CALLER_RETENTION_SECONDS',''))
        cutoff = now - retention
        with _caller_pending(db) as pending:
            if pending is not None:
                expired = []
                for row in db.execute('SELECT id FROM request WHERE created<? ORDER BY created,id',(cutoff,)):
                    if row[0] not in pending:
                        expired.append(row[0])
                        if len(expired) == CALLER_CLEANUP_BATCH:
                            break
                db.executemany('DELETE FROM request WHERE id=? AND created<?', ((ident,cutoff) for ident in expired))
            count = db.execute('SELECT count(*) FROM request WHERE created>=?',(cutoff,)).fetchone()[0]
            if count >= CALLER_MAX_RECORDS:
                raise CallerJournalRejected('caller_journal_full',rid,True)
            db.execute('INSERT INTO request(id,digest,created) VALUES(?,?,?)',(rid,digest,now))
            db.commit()
    return rid


def ipc_send(to: str, body: str, request_id=None, *, status=False) -> dict:
    if len(body.encode('utf-8'))>MESSAGE_BODY_MAX_BYTES:
        return {'ok':False,'error':'body_too_large','outcome':{'outcome':'definitive_failure','accepted_by':'none','phase':'rejected','reason':'body_too_large','retryable':False}}
    try:
        rid=request_id if status else remember_request(request_id,to,body,{'ipc_body_amr_v1':True})
    except CallerJournalRejected as exc:
        return {'ok':False,'request_id':exc.request_id,'outcome':exc.outcome()}
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
        resp['_caller_request_id']=rid
        return resp
    except (OSError,ValueError):
        return {'ok':False,'outcome':unknown,'request_id':rid}
    finally:sock.close()


def checked_outcome(out, request_id):
    """Accept only the responsibility protocol, never an IPC or sent boolean."""
    import re
    valid=isinstance(out,dict)
    if valid and out.get('outcome')=='accepted':
        valid=((out.get('phase'),out.get('accepted_by')) in (('local_queued','local_helper'),('hub_durable_queued','hub'))
               and isinstance(out.get('request_id'),str) and out['request_id']==request_id
               and isinstance(out.get('source_message_id'),str) and re.fullmatch(r'[0-9a-f]{32}',out['source_message_id']) is not None)
        if valid and out['accepted_by']=='hub':
            valid=isinstance(out.get('delivery_id'),str) and re.fullmatch(r'dw-dlv_[0-9a-f]{32}',out['delivery_id']) is not None
    elif valid:
        valid=((out.get('outcome'),out.get('phase'),out.get('accepted_by')) in (('unknown','unknown','unknown'),('definitive_failure','rejected','none')))
    if valid:return out
    return {'phase':'unknown','accepted_by':'unknown','outcome':'unknown','request_id':request_id,'status_ref':request_id,'reason':'unconfirmed_legacy_receipt','retryable':False}


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
        rid = rid or uuid.uuid4().hex
        try:resp=ipc_send(recipient,body,rid)
        except (ValueError,OSError,sqlite3.Error):
            print(json.dumps({'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':'caller_journal_unavailable','request_id':rid,'status_ref':rid,'retryable':True}));sys.exit(1)
    expected=resp.get('_caller_request_id') or resp.get('request_id') or rid
    out=resp.get('outcome')
    if resp.get('ok') is False and not isinstance(out,dict):
        out={'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':('helper_unavailable' if isinstance(resp.get('error'),str) and resp['error'].startswith('unavailable:') else 'helper_rejected'),'request_id':expected,'retryable':False}
    out=checked_outcome(out,expected)
    print(json.dumps(out,ensure_ascii=False))
    if out['outcome']=='definitive_failure':print('发送失败：'+out.get('reason','rejected'),file=sys.stderr)
    if out['outcome']!='accepted':sys.exit(2 if out['outcome']=='unknown' else 1)


if __name__=='__main__':main()
