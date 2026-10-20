"""Durable, session-local recheck commands; ambiguous dispatch never replays RPC."""
from __future__ import annotations
import json
import os
import urllib.parse
import sqlite3
import threading
import time
from .sqlite_utils import connection
from .task_blocker import wake_identity

class BlockerJournal:
    def __init__(self,path,session):
        self.path=path;self.session=session;self.lock=threading.RLock()
        os.makedirs(os.path.dirname(path) or ".",exist_ok=True)
        with self._db() as db:
            db.execute('''CREATE TABLE IF NOT EXISTS blocker_wake_journal(
                session TEXT NOT NULL,signal_id TEXT NOT NULL,delivery_id TEXT NOT NULL,
                envelope_json TEXT NOT NULL,state TEXT NOT NULL,detail TEXT NOT NULL DEFAULT '',
                updated_at REAL NOT NULL,PRIMARY KEY(session,signal_id))''')
    @staticmethod
    def exists(path):
        if not path or not os.path.isfile(path):return False
        db=sqlite3.connect('file:'+urllib.parse.quote(os.path.abspath(path),safe='/')+'?mode=ro',uri=True)
        try:return db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='blocker_wake_journal'").fetchone() is not None
        finally:db.close()
    def _db(self):return connection(self.path,timeout=5,row_factory=sqlite3.Row)
    def stage(self,env):
        m=wake_identity(env);did=(env.get('meta') or {}).get('delivery_id')
        if not isinstance(did,str) or not did:raise ValueError('wake requires durable delivery id')
        with self.lock,self._db() as db:
            db.execute('''INSERT OR IGNORE INTO blocker_wake_journal(session,signal_id,delivery_id,envelope_json,state,updated_at)
                VALUES(?,?,?,?,'pending',?)''',(self.session,m['signal_id'],did,json.dumps(env,ensure_ascii=False),time.time()))
            row=db.execute('SELECT * FROM blocker_wake_journal WHERE session=? AND signal_id=?',(self.session,m['signal_id'])).fetchone()
            return dict(row)
    def recover(self):
        with self.lock,self._db() as db:
            return [json.loads(r['envelope_json']) for r in db.execute("SELECT envelope_json FROM blocker_wake_journal WHERE session=? AND state IN('pending','dispatching','admitted') ORDER BY updated_at,signal_id LIMIT 100",(self.session,))]
    def start(self,signal_id):
        with self.lock,self._db() as db:
            db.execute('BEGIN IMMEDIATE')
            row=db.execute('SELECT state FROM blocker_wake_journal WHERE session=? AND signal_id=?',(self.session,signal_id)).fetchone()
            if row is None:raise ValueError('unstaged wake')
            state=row['state']
            if state=='pending':db.execute("UPDATE blocker_wake_journal SET state='dispatching',updated_at=? WHERE session=? AND signal_id=?",(time.time(),self.session,signal_id))
            return state
    def finish(self,signal_id,state,detail=''):
        if state not in ('pending','dispatching','admitted','done','failed','cancelled','unknown'):raise ValueError('invalid wake journal state')
        with self.lock,self._db() as db:
            db.execute('UPDATE blocker_wake_journal SET state=?,detail=?,updated_at=? WHERE session=? AND signal_id=?',(state,str(detail)[:256],time.time(),self.session,signal_id))
