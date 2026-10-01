"""Client-side enrollment: pairing code -> apply -> poll -> private credential file.

Flow (SPEC-THIN-SH-V1 s.2, DESIGN-THIN-SH s.3.1):
  no credential on disk -> show a one-time 6 digit pairing code -> ask the Hub
  for approval -> poll until the owner approves -> store the dedicated
  credential in a 0600 file -> caller connects with it.

Interface: runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md v1 (sections 2.1-2.3).  The Hub only ever sees a salted
digest of the pairing code, never the code itself.  The credential and the
poll token are never logged or printed by this module.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import secrets
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from urllib.parse import urlencode
from typing import Callable, Protocol

from . import __version__

CREDENTIAL_FILE = "credential.json"
PENDING_FILE = "enroll-pending.json"
DEVICE_FILE = "enroll-device.id"
CREDENTIAL_RE = re.compile(r"^dwa_[0-9a-f]{64}$")  # format self-check only; never log a value
MIN_POLL_INTERVAL = 2.0  # Hub allows one poll per 2 s per application (429 beyond that)
REVOKE_CLOSE_CODE = 4401
WS_QUERY_ALLOWED = ("version", "tool", "os")
OWNER_RE = re.compile(r"^[A-Za-z0-9]{2,32}$")  # user-ID rule (ledger c5981300); the Hub compares case-insensitively


class EnrollError(RuntimeError):
    pass


class EnrollRejected(EnrollError):
    """The owner rejected the request (= kicked); do not retry silently."""


class EnrollStopped(EnrollError):
    """Application ended without a credential (expired / superseded); do not retry silently."""


class CredentialAlreadyIssued(EnrollError):
    """Hub says approved but the one-time credential was already handed out."""


class CredentialFileError(EnrollError):
    """Credential file exists but is unsafe or unreadable."""


class HubHTTPError(EnrollError):
    """Hub answered with an HTTP error; only the machine code is kept, never a body text."""

    def __init__(self, status: int, code: str = "", retry_after_s: float = 0.0):
        super().__init__(f"hub http {status}" + (f": {code}" if code else ""))
        self.status, self.code, self.retry_after_s = status, code, retry_after_s


class CredentialInvalid(EnrollError):
    """Handshake: credential unknown (maybe the wrong Hub). Stop; keep the file."""


class CredentialRevoked(EnrollError):
    """Handshake refused or connection closed because the agent was revoked."""


class CredentialSuspended(EnrollError):
    """The owner paused (disabled) this hosted seat: the credential is refused for now.

    Reversible, unlike revocation: the local credential file is KEPT, so after the seat is enabled
    again the restarted client simply reconnects with it (ledger 4ecd2246)."""


class AlreadyConnected(EnrollError):
    """Handshake: the same credential already has a live connection. Stop; keep the file."""


FATAL_CONNECT_ERRORS = (CredentialInvalid, CredentialRevoked, CredentialSuspended, AlreadyConnected)


@dataclass(frozen=True)
class Credential:
    agent_name: str
    secret: str

    def __repr__(self) -> str:  # keep the secret out of tracebacks and logs
        return f"Credential(agent_name={self.agent_name!r}, secret=<redacted>)"


class Transport(Protocol):
    def post_json(self, path: str, body: dict) -> dict: ...

    def get_json(self, path: str, headers: dict[str, str]) -> dict: ...


class UrllibTransport:
    """Thin HTTP layer; the only part that depends on the Hub's final paths."""

    def __init__(self, base_url: str, timeout: float = 15.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def _call(self, req: urllib.request.Request) -> dict:
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:  # noqa: S310
                return json.loads(resp.read().decode("utf-8") or "{}")
        except urllib.error.HTTPError as exc:
            code, retry = "", 0.0
            try:
                parsed = json.loads(exc.read().decode("utf-8", "replace") or "{}")
                code = str(parsed.get("error") or "")[:64]
                retry = float(parsed.get("retry_after_s") or 0)
            except (OSError, ValueError, AttributeError, TypeError):
                pass
            raise HubHTTPError(exc.code, code, retry) from exc
        except (urllib.error.URLError, OSError, ValueError) as exc:
            raise EnrollError(f"hub unreachable: {type(exc).__name__}") from exc

    def post_json(self, path: str, body: dict) -> dict:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(
            self.base_url + path, data=data, method="POST",
            headers={"Content-Type": "application/json"},
        )
        return self._call(req)

    def get_json(self, path: str, headers: dict[str, str]) -> dict:
        return self._call(urllib.request.Request(self.base_url + path, headers=headers))


# ---------------------------------------------------------------- primitives

def generate_pairing_code() -> str:
    """6 digit one-time code; CSPRNG, leading zeros kept."""
    return f"{secrets.randbelow(10**6):06d}"


def pairing_digest(code: str, salt_hex: str) -> str:
    """sha256(salt bytes + ASCII code), hex (contract item 1)."""
    return hashlib.sha256(bytes.fromhex(salt_hex) + code.encode("ascii")).hexdigest()


def write_private_file(path: str, data: str) -> None:
    """Atomic write; the file is created 0600 from the start (no chmod window)."""
    directory = os.path.dirname(path) or "."
    os.makedirs(directory, mode=0o700, exist_ok=True)
    tmp = f"{path}.{os.getpid()}.{secrets.token_hex(4)}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(data)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp, path)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def _assert_private(path: str) -> None:
    mode = os.stat(path).st_mode & 0o777
    if mode & 0o077:
        raise CredentialFileError(
            f"{path} permission is {mode:o}; expected 600. Refusing to use it."
        )


def device_fingerprint(state_dir: str) -> str:
    """Stable per-install id, persisted; sent only as a digest."""
    path = os.path.join(state_dir, DEVICE_FILE)
    try:
        with open(path, encoding="utf-8") as fh:
            value = fh.read().strip()
        if re.fullmatch(r"[0-9a-f]{32}", value):
            return hashlib.sha256(value.encode()).hexdigest()
    except OSError:
        pass
    value = secrets.token_hex(16)
    write_private_file(path, value + "\n")
    return hashlib.sha256(value.encode()).hexdigest()


# ---------------------------------------------------------------- state files

def load_credential(state_dir: str) -> Credential | None:
    path = os.path.join(state_dir, CREDENTIAL_FILE)
    try:
        _assert_private(path)
        with open(path, encoding="utf-8") as fh:
            raw = json.load(fh)
    except FileNotFoundError:
        return None
    except (OSError, ValueError) as exc:
        raise CredentialFileError(f"cannot read credential file: {type(exc).__name__}") from exc
    name, secret = str(raw.get("agent_name") or ""), str(raw.get("secret") or "")
    if not name or not secret:
        raise CredentialFileError("credential file is incomplete")
    return Credential(agent_name=name, secret=secret)


def save_credential(state_dir: str, cred: Credential) -> str:
    path = os.path.join(state_dir, CREDENTIAL_FILE)
    write_private_file(path, json.dumps({"agent_name": cred.agent_name, "secret": cred.secret}))
    return path


def _load_pending(state_dir: str, kind: str = "selfhost") -> dict | None:
    """The saved in-flight application of this kind (a file without 'kind' is a self-hosted one)."""
    path = os.path.join(state_dir, PENDING_FILE)
    try:
        _assert_private(path)
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
        need = ("application_id", "poll_token") + (("code",) if kind == "selfhost" else ())
        if data.get("kind", "selfhost") == kind and all(isinstance(data.get(k), str) and data[k] for k in need):
            return data
    except (OSError, ValueError, CredentialFileError, AttributeError):
        pass
    return None


def _clear_pending(state_dir: str) -> None:
    try:
        os.unlink(os.path.join(state_dir, PENDING_FILE))
    except FileNotFoundError:
        pass


# ---------------------------------------------------------------- orchestration

def enroll(
    *,
    state_dir: str,
    owner: str,
    transport: Transport,
    show: Callable[[str], None],
    notify: Callable[[str], None] = lambda _m: None,
    poll_interval: float = 4.0,
    sleep: Callable[[float], None] = time.sleep,
    max_polls: int | None = None,
) -> Credential:
    """Return a Credential, enrolling first if none is stored.

    Raises EnrollRejected on rejection and EnrollStopped on expired/superseded:
    the screen is told why (notify) and nothing is re-applied silently.  A
    restart while waiting resumes the same application and re-shows its code.
    """
    existing = load_credential(state_dir)
    if existing:
        return existing
    if not OWNER_RE.fullmatch(owner or ""):
        raise EnrollError("owner_user_id is missing or malformed")

    poll_interval = max(poll_interval, MIN_POLL_INTERVAL)
    try:
        pending = _load_pending(state_dir) or _apply(state_dir, owner, transport)
    except HubHTTPError as exc:
        if exc.status == 404 and exc.code == "unknown_owner":
            reason = "owner_user_id does not exist or is not active; check the user ID you entered"
        elif exc.status == 429:
            reason = f"too many applications; try again in about {int(exc.retry_after_s) or 60} seconds"
        else:
            raise
        notify(reason)
        raise EnrollStopped(reason) from exc
    show(pending["code"])
    return _await_credential(state_dir, pending, transport, notify, poll_interval, sleep, max_polls)


_TERMINAL_REASONS = {
    "selfhost": {
        "rejected": "the owner rejected this client",
        "expired": "the application expired; restart the client to apply again",
        "superseded": "another application from this device was approved; this one is void",
    },
    "hosted": {
        "rejected": "the platform withdrew this enrollment token",
        "expired": "the enrollment token expired; the platform has to issue a new one",
        "superseded": "this enrollment token was replaced; the platform has to issue a new one",
    },
}


def _await_credential(state_dir: str, pending: dict, transport: Transport, notify: Callable[[str], None],
                      poll_interval: float, sleep: Callable[[float], None], max_polls: int | None) -> Credential:
    """Poll one application until the Hub hands out the credential (shared by both enrollment routes)."""
    kind = pending.get("kind", "selfhost")
    polls = 0
    while True:
        try:
            reply = transport.get_json(
                f"/api/enroll/applications/{pending['application_id']}",
                {"Authorization": f"Bearer {pending['poll_token']}"},
            )
        except HubHTTPError as exc:
            if exc.status == 429:  # polled too fast: wait as told, not counted as a failed poll
                sleep(max(exc.retry_after_s, poll_interval))
                continue
            if exc.status == 404:  # unknown application or wrong poll token: the saved request is useless
                _clear_pending(state_dir)
                reason = ("this application no longer exists; restart the client to apply again" if kind == "selfhost"
                          else "the enrollment token is unknown to the Hub; the platform has to issue a new one")
                notify(reason)
                raise EnrollStopped(reason) from exc
            raise
        state = reply.get("state")
        if state == "approved":
            name, secret = str(reply.get("agent_name") or ""), str(reply.get("credential") or "")
            if not name:
                raise EnrollError("hub approved without an agent_name")
            if not secret:
                _clear_pending(state_dir)
                raise CredentialAlreadyIssued(
                    "approved, but the one-time credential was already issued; "
                    + ("ask the owner to revoke this agent and apply again" if kind == "selfhost"
                       else "the platform has to issue a new enrollment token"))
            if not CREDENTIAL_RE.fullmatch(secret):
                # Saved anyway: it is handed out only once, so refusing it would lose it for good.
                notify("the credential has an unexpected format; saved, but report this to the platform team")
            cred = Credential(agent_name=name, secret=secret)
            save_credential(state_dir, cred)  # persist before dropping the pending state
            _clear_pending(state_dir)  # poll_token is discarded with it
            return cred
        if state in ("rejected", "expired", "superseded"):
            _clear_pending(state_dir)
            reason = _TERMINAL_REASONS[kind][state]
            notify(reason)
            raise (EnrollRejected if state == "rejected" else EnrollStopped)(reason)
        if state != "pending":
            raise EnrollError(f"unexpected enroll state: {state!r}")
        polls += 1
        if max_polls is not None and polls >= max_polls:
            raise EnrollError("gave up waiting for approval")
        sleep(poll_interval)


# ---------------------------------------------------------------- hosted route

TOKEN_FILE = "enroll-token.json"
TOKEN_EXPIRY_GRACE_S = 300  # tolerate clock skew between the container and the Hub before refusing locally


def _parse_rfc3339(value: str) -> float | None:
    from datetime import datetime, timezone
    try:
        dt = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return (dt if dt.tzinfo else dt.replace(tzinfo=timezone.utc)).timestamp()


def enroll_with_token(
    *,
    state_dir: str,
    token_file: str,
    transport: Transport,
    notify: Callable[[str], None] = lambda _m: None,
    poll_interval: float = 4.0,
    sleep: Callable[[float], None] = time.sleep,
    max_polls: int | None = None,
    now: Callable[[], float] = time.time,
) -> Credential:
    """Hosted-seat enrollment: the Hub already issued a one-time token (an approved application).

    The container receives {application_id, poll_token, expires_at} as a file (never via the
    environment or an image layer).  This reads it, saves the in-flight state (0600), deletes the
    file, then polls exactly like the self-hosted route — no pairing code, no application step.

      * token file present  -> use it (also when a credential exists: that is a rotation, the old
        credential stays until the new one is actually taken);
      * else a saved hosted application -> resume polling it;
      * else a stored credential -> return it.
    An expired or unknown token fails clearly and leaves nothing half-done.
    """
    poll_interval = max(poll_interval, MIN_POLL_INTERVAL)
    if os.path.exists(token_file):
        pending = _read_token_file(state_dir, token_file)
        exp = _parse_rfc3339(pending.get("expires_at", ""))
        if exp is not None and now() > exp + TOKEN_EXPIRY_GRACE_S:
            _clear_pending(state_dir)
            reason = "the enrollment token expired; the platform has to issue a new one"
            notify(reason)
            raise EnrollStopped(reason)
    else:
        pending = _load_pending(state_dir, "hosted")
        if pending is None:
            existing = load_credential(state_dir)
            if existing:
                return existing
            raise EnrollError("no enrollment token file and no stored credential")
    return _await_credential(state_dir, pending, transport, notify, poll_interval, sleep, max_polls)


def _read_token_file(state_dir: str, token_file: str) -> dict:
    """Read the one-time token file, keep the state in a 0600 file, then delete the token file.

    The file is deleted as soon as its content is safely held (or found unusable), so the token does
    not sit on disk twice.  If the state cannot be saved the file is left for another try.
    """
    try:
        with open(token_file, encoding="utf-8") as fh:
            raw = json.load(fh)
        app_id, tok = str(raw.get("application_id") or ""), str(raw.get("poll_token") or "")
        exp = str(raw.get("expires_at") or "")
    except (OSError, ValueError, AttributeError) as exc:
        _remove_quietly(token_file)  # unusable: do not leave a half-written secret behind
        raise EnrollError(f"enrollment token file is unreadable: {type(exc).__name__}") from exc
    if not app_id or not tok:
        _remove_quietly(token_file)
        raise EnrollError("enrollment token file lacks application_id or poll_token")
    pending = {"kind": "hosted", "application_id": app_id, "poll_token": tok, "expires_at": exp}
    write_private_file(os.path.join(state_dir, PENDING_FILE), json.dumps(pending))
    os.unlink(token_file)
    return pending


def _remove_quietly(path: str) -> None:
    try:
        os.unlink(path)
    except OSError:
        pass


def _apply(state_dir: str, owner: str, transport: Transport) -> dict:
    code = generate_pairing_code()
    salt = secrets.token_hex(16)  # 16 bytes
    reply = transport.post_json("/api/enroll/applications", {
        "owner_user_id": owner,
        "pairing_salt": salt,
        "pairing_digest": pairing_digest(code, salt),
        "device_fingerprint": device_fingerprint(state_dir),
        "client_version": __version__,
    })
    application_id = str(reply.get("application_id") or "")
    poll_token = str(reply.get("poll_token") or "")
    if not application_id or not poll_token:
        raise EnrollError("hub reply lacks application_id or poll_token")
    pending = {"code": code, "application_id": application_id, "poll_token": poll_token,
               "expires_at": str(reply.get("expires_at") or "")}
    write_private_file(os.path.join(state_dir, PENDING_FILE), json.dumps(pending))
    return pending


def revoke_local(state_dir: str, notify: Callable[[str], None]) -> None:
    """Hub says this agent was revoked: drop the credential, tell the user,
    and do NOT re-apply on our own (contract item 5)."""
    for name in (CREDENTIAL_FILE, PENDING_FILE):
        try:
            os.unlink(os.path.join(state_dir, name))
        except FileNotFoundError:
            pass
    notify("this agent was revoked by its owner; the local credential was removed. "
           "Apply again manually if you want to re-bind.")


def run_enrolled(*, connect: Callable[[Credential], object], **kw):
    """Credential in hand -> hand it to the connect function (stub-able)."""
    return connect(enroll(**kw))


def auth_headers(cred: Credential) -> dict[str, str]:
    """Headers for the dedicated-credential WebSocket handshake (Bearer)."""
    return {"Authorization": f"Bearer {cred.secret}"}


AGENT_WS_PATH = "/ws/agent"


def agent_ws_url(ws_base: str, query: dict[str, str] | None = None) -> str:
    """Connect URL for a dedicated-credential agent (design 2.3).

    Identity (name.owner) is resolved by the Hub from the credential, so the URL
    carries neither the agent name nor any account key; only capability
    parameters (version, tool, os) are allowed through.
    """
    q = {"version": __version__, **(query or {})}
    q = {k: v for k, v in q.items() if k in WS_QUERY_ALLOWED and v}
    return f"{ws_base.rstrip('/')}{AGENT_WS_PATH}?{urlencode(q)}"


def connection_params(cred: Credential, ws_base: str, query: dict[str, str] | None = None) -> tuple[str, dict[str, str]]:
    return agent_ws_url(ws_base, query), auth_headers(cred)


def on_handshake_failure(state_dir: str, status: int, body: bytes | str, notify: Callable[[str], None]) -> None:
    """Interpret a refused WebSocket handshake. Raises for the three fatal cases,
    returns for anything else (ordinary reconnect logic then applies)."""
    code = ""
    try:
        raw = body.decode("utf-8", "replace") if isinstance(body, (bytes, bytearray)) else str(body or "")
        code = str(json.loads(raw or "{}").get("error") or "")
    except (ValueError, AttributeError):
        pass
    if status == 401 and code == "credential_revoked":
        revoke_local(state_dir, notify)
        raise CredentialRevoked("credential revoked")
    if status == 401 and code == "credential_suspended":
        notify("本 Agent 已被停用 (this seat is paused by its owner); stopping. The local credential was kept")
        raise CredentialSuspended("credential suspended")
    if status == 401 and code == "credential_invalid":
        notify("the Hub does not know this credential (wrong Hub?); the local credential file was kept")
        raise CredentialInvalid("credential invalid")
    if status == 409 and code == "already_connected":
        notify("this credential is already connected elsewhere; stopping. The local credential file was kept")
        raise AlreadyConnected("already connected")


def on_ws_closed(state_dir: str, code: int, reason: str, notify: Callable[[str], None]) -> None:
    """A live connection was closed by the Hub: 4401 + credential_revoked = revoked (credential removed);
    4401 + credential_suspended = paused (credential kept)."""
    if code == REVOKE_CLOSE_CODE and reason == "credential_revoked":
        revoke_local(state_dir, notify)
        raise CredentialRevoked("credential revoked")
    if code == REVOKE_CLOSE_CODE and reason == "credential_suspended":
        notify("本 Agent 已被停用 (this seat is paused by its owner); stopping. The local credential was kept")
        raise CredentialSuspended("credential suspended")


def enabled_for_env(environ) -> bool:
    return str(environ.get("SH_ENROLL", "")).strip().lower() in ("1", "true", "yes", "on")


def hub_http_base(ws_base: str) -> str:
    """wss://host -> https://host ; ws:// -> http:// (enroll endpoints are plain HTTP)."""
    if ws_base.startswith("wss://"):
        return "https://" + ws_base[len("wss://"):]
    if ws_base.startswith("ws://"):
        return "http://" + ws_base[len("ws://"):]
    return ws_base


def prepare_client_env(environ, *, show: Callable[[str], None], notify: Callable[[str], None],
                       transport: Transport | None = None) -> tuple[dict[str, str], Credential, str]:
    """SH_ENROLL=1 startup: enroll if needed, then return (env for load_config, credential, state_dir).

    config.py stays untouched (R1 owns it): load_config still wants SH_KEY_ID/SH_SECRET, so they get
    inert placeholders here; the connection layer ignores them whenever a credential is present.
    """
    env = dict(environ)
    state_dir = os.path.expanduser(env.get("SH_STATE_DIR", "~/.dingwei"))
    ws_base = env.get("SH_WS_BASE", "")
    if not ws_base:
        raise EnrollError("SH_WS_BASE is required")
    transport = transport or UrllibTransport(hub_http_base(ws_base))
    token_file = os.path.expanduser(env.get("SH_ENROLL_TOKEN_FILE", "") or os.path.join(state_dir, TOKEN_FILE))
    if os.path.exists(token_file) or _load_pending(state_dir, "hosted"):
        # hosted seat: the platform put a one-time token file in the container; no owner, no pairing code
        cred = enroll_with_token(state_dir=state_dir, token_file=token_file, transport=transport, notify=notify)
    else:
        cred = enroll(
            state_dir=state_dir, owner=env.get("SH_ENROLL_OWNER", ""),
            transport=transport, show=show, notify=notify,
        )
    env["SH_SESSION_NAME"] = cred.agent_name.lower()
    env.setdefault("SH_KEY_ID", "enrolled")
    env["SH_SECRET"] = "enrolled"
    return env, cred, state_dir
