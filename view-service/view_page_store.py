from __future__ import annotations

import hashlib
import hmac
import json
import secrets
import sqlite3
import string
import threading
from datetime import datetime, timezone


def _utc_iso(epoch: float) -> str:
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat().replace("+00:00", "Z")


class ViewPageStore:
    def __init__(self, path: str, *, now, disconnect_grace=300, locked_ttl=1800,
                 grant_ttl=28800, replay_ttl=300, audit_ttl=86400, max_replay_rows=10000):
        self.path = path
        self.now = now
        self.disconnect_grace = disconnect_grace
        self.locked_ttl = locked_ttl
        self.grant_ttl = grant_ttl
        self.replay_ttl = replay_ttl
        self.audit_ttl = audit_ttl
        self.max_replay_rows = max_replay_rows
        self._lock = threading.RLock()
        self._db = sqlite3.connect(path, check_same_thread=False, isolation_level="DEFERRED")
        self._db.row_factory = sqlite3.Row
        self._migrate()

    def close(self):
        with self._lock:
            self._db.close()

    def _migrate(self):
        with self._lock:
            self._db.executescript("""
                PRAGMA journal_mode=WAL;
                PRAGMA synchronous=FULL;
                CREATE TABLE IF NOT EXISTS view_pages (
                    page_id TEXT PRIMARY KEY,
                    session_name TEXT NOT NULL,
                    code TEXT NOT NULL UNIQUE COLLATE BINARY,
                    page_token_hash TEXT NOT NULL,
                    state TEXT NOT NULL CHECK(state IN ('locked','unlocked','revoked','expired')),
                    created_at TEXT NOT NULL,
                    last_seen_at TEXT NOT NULL,
                    unlocked_at TEXT,
                    unlock_expires_at TEXT,
                    last_write_at TEXT,
                    disconnected_at TEXT,
                    grant_owner TEXT NOT NULL DEFAULT ''
                );
                CREATE INDEX IF NOT EXISTS idx_view_pages_session ON view_pages(session_name);
                CREATE TABLE IF NOT EXISTS view_control_commands (
                    command_id TEXT PRIMARY KEY,
                    request_id TEXT NOT NULL UNIQUE,
                    payload_hash TEXT NOT NULL,
                    status INTEGER NOT NULL,
                    created_at REAL NOT NULL
                );
                CREATE TABLE IF NOT EXISTS view_control_nonces (
                    target TEXT NOT NULL,
                    nonce TEXT NOT NULL,
                    created_at REAL NOT NULL,
                    PRIMARY KEY(target, nonce)
                );
                CREATE INDEX IF NOT EXISTS idx_view_control_nonces_created ON view_control_nonces(created_at);
            """)

    def create_page(self, session_name: str):
        now = self.now()
        for _ in range(16):
            page_id = secrets.token_hex(16)
            page_token = secrets.token_hex(32)
            suffix = "".join(secrets.choice(string.ascii_letters + string.digits) for _ in range(8))
            code = f"{session_name}-{suffix}"
            try:
                with self._lock, self._db:
                    self._db.execute("""
                        INSERT INTO view_pages(page_id,session_name,code,page_token_hash,state,created_at,last_seen_at)
                        VALUES(?,?,?,?,?,?,?)
                    """, (page_id, session_name, code, self._token_hash(page_token), "locked",
                          _utc_iso(now), _utc_iso(now)))
                return {"page_id": page_id, "page_token": page_token, "code": code, "state": "locked"}
            except sqlite3.IntegrityError:
                continue
        raise RuntimeError("unable to allocate unique page identity")

    def claim_nonce(self, target: str, nonce: str) -> bool:
        now = self.now()
        with self._lock, self._db:
            self._cleanup_replay_locked(now)
            try:
                self._db.execute("INSERT INTO view_control_nonces(target,nonce,created_at) VALUES(?,?,?)",
                                 (target, nonce, now))
                self._trim_replay_locked()
                return True
            except sqlite3.IntegrityError:
                return False

    def unlock(self, command: dict) -> int:
        command_id = str(command.get("command_id", "")).strip()
        request_id = str(command.get("request_id", "")).strip()
        session = str(command.get("session", "")).strip()
        code = str(command.get("code", "")).strip()
        owner = str(command.get("owner_key", "")).strip()
        if not all((command_id, request_id, session, code, owner)):
            return 400
        payload_hash = hashlib.sha256(json.dumps(command, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        now = self.now()
        with self._lock, self._db:
            self._cleanup_replay_locked(now)
            previous = self._db.execute("""
                SELECT payload_hash,status FROM view_control_commands
                WHERE command_id=? OR request_id=?
            """, (command_id, request_id)).fetchone()
            if previous:
                return previous["status"] if hmac.compare_digest(previous["payload_hash"], payload_hash) else 409
            page = self._db.execute("SELECT page_id,state FROM view_pages WHERE code=? AND session_name=?",
                                    (code, session)).fetchone()
            status = 404
            if page:
                if page["state"] == "locked":
                    changed = self._db.execute("""
                        UPDATE view_pages SET state='unlocked',unlocked_at=?,unlock_expires_at=?,grant_owner=?,disconnected_at=NULL
                        WHERE page_id=? AND state='locked'
                    """, (_utc_iso(now), _utc_iso(now + self.grant_ttl), owner, page["page_id"])).rowcount
                    status = 200 if changed == 1 else 409
                elif page["state"] == "unlocked":
                    status = 200
                else:
                    status = 409
            self._db.execute("""
                INSERT INTO view_control_commands(command_id,request_id,payload_hash,status,created_at)
                VALUES(?,?,?,?,?)
            """, (command_id, request_id, payload_hash, status, now))
            self._trim_replay_locked()
            return status

    def authorize_page(self, page_id: str, page_token: str, session_name: str) -> bool:
        now = self.now()
        with self._lock, self._db:
            page = self._db.execute("SELECT * FROM view_pages WHERE page_id=? AND session_name=?",
                                    (page_id, session_name)).fetchone()
            if not page or not hmac.compare_digest(page["page_token_hash"], self._token_hash(page_token)):
                return False
            if page["state"] != "unlocked" or self._expired(page["unlock_expires_at"], now):
                if page["state"] == "unlocked":
                    self._db.execute("UPDATE view_pages SET state='expired' WHERE page_id=? AND state='unlocked'", (page_id,))
                return False
            self._db.execute("UPDATE view_pages SET last_seen_at=?,disconnected_at=NULL WHERE page_id=?",
                             (_utc_iso(now), page_id))
            return True

    def resume_page(self, page_id: str, page_token: str, session_name: str):
        now = self.now()
        with self._lock, self._db:
            page = self._db.execute("SELECT * FROM view_pages WHERE page_id=? AND session_name=?",
                                    (page_id, session_name)).fetchone()
            if not page or not hmac.compare_digest(page["page_token_hash"], self._token_hash(page_token)):
                return None
            self._db.execute("UPDATE view_pages SET last_seen_at=?,disconnected_at=NULL WHERE page_id=?",
                             (_utc_iso(now), page_id))
            return {"page_id": page_id, "code": page["code"], "state": page["state"],
                    "can_write": page["state"] == "unlocked" and not self._expired(page["unlock_expires_at"], now)}

    def mark_disconnected(self, page_id: str, page_token: str) -> bool:
        now = self.now()
        with self._lock, self._db:
            page = self._db.execute("SELECT page_token_hash FROM view_pages WHERE page_id=?", (page_id,)).fetchone()
            if not page or not hmac.compare_digest(page["page_token_hash"], self._token_hash(page_token)):
                return False
            self._db.execute("UPDATE view_pages SET disconnected_at=? WHERE page_id=?", (_utc_iso(now), page_id))
            return True

    def record_successful_write(self, page_id: str, page_token: str) -> bool:
        now = self.now()
        with self._lock, self._db:
            page = self._db.execute("SELECT page_token_hash,state FROM view_pages WHERE page_id=?", (page_id,)).fetchone()
            if not page or page["state"] != "unlocked" or not hmac.compare_digest(
                    page["page_token_hash"], self._token_hash(page_token)):
                return False
            self._db.execute("""
                UPDATE view_pages SET last_write_at=?,last_seen_at=?,unlock_expires_at=?
                WHERE page_id=? AND state='unlocked'
            """, (_utc_iso(now), _utc_iso(now), _utc_iso(now + self.grant_ttl), page_id))
            return True

    def sweep(self):
        now = self.now()
        with self._lock, self._db:
            self._db.execute("""
                UPDATE view_pages SET state='expired'
                WHERE state='locked' AND julianday(?) - julianday(last_seen_at) >= ? / 86400.0
            """, (_utc_iso(now), self.locked_ttl))
            self._db.execute("""
                UPDATE view_pages SET state='revoked'
                WHERE state='unlocked' AND disconnected_at IS NOT NULL
                  AND julianday(?) - julianday(disconnected_at) >= ? / 86400.0
            """, (_utc_iso(now), self.disconnect_grace))
            self._db.execute("""
                UPDATE view_pages SET state='expired'
                WHERE state='unlocked' AND unlock_expires_at <= ?
            """, (_utc_iso(now),))
            self._cleanup_replay_locked(now)

    def page(self, page_id: str):
        with self._lock:
            row = self._db.execute("SELECT * FROM view_pages WHERE page_id=?", (page_id,)).fetchone()
            return dict(row) if row else None

    def replay_counts(self):
        with self._lock:
            commands = self._db.execute("SELECT count(*) FROM view_control_commands").fetchone()[0]
            nonces = self._db.execute("SELECT count(*) FROM view_control_nonces").fetchone()[0]
            return commands, nonces

    def _cleanup_replay_locked(self, now: float):
        self._db.execute("DELETE FROM view_control_nonces WHERE created_at < ?", (now - self.replay_ttl,))
        self._db.execute("DELETE FROM view_control_commands WHERE created_at < ?", (now - self.audit_ttl,))
        self._trim_replay_locked()

    def _trim_replay_locked(self):
        for table in ("view_control_nonces", "view_control_commands"):
            self._db.execute(f"""
                DELETE FROM {table} WHERE rowid IN (
                    SELECT rowid FROM {table} ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET ?
                )
            """, (self.max_replay_rows,))

    @staticmethod
    def _token_hash(token: str) -> str:
        return hashlib.sha256(token.encode()).hexdigest()

    @staticmethod
    def _expired(value: str | None, now: float) -> bool:
        if not value:
            return True
        return value <= _utc_iso(now)
