"""Client-side enrollment: pairing code -> apply -> poll -> private credential file.

Flow (SPEC-THIN-SH-V1 s.2, DESIGN-THIN-SH s.3.1):
  no credential on disk -> show a one-time 6 digit pairing code -> ask the Hub
  for approval -> poll until the owner approves -> store the dedicated
  credential in a 0600 file -> caller connects with it.

The Hub only ever sees a salted digest of the pairing code and a digest of the
poll secret, never the code itself.  The credential value is never logged or
printed by this module.
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
OWNER_RE = re.compile(r"^[a-z0-9]+$")


class EnrollError(RuntimeError):
    pass


class EnrollRejected(EnrollError):
    """The owner rejected the request (= kicked); do not retry silently."""


class CredentialFileError(EnrollError):
    """Credential file exists but is unsafe or unreadable."""


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
            detail = ""
            try:
                detail = exc.read().decode("utf-8", "replace")[:200]
            except OSError:
                pass
            raise EnrollError(f"hub http {exc.code}: {detail}") from exc
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


def pairing_digest(code: str, salt: str) -> str:
    return hashlib.sha256(f"{salt}:{code}".encode("utf-8")).hexdigest()


def secret_digest(secret: str) -> str:
    return hashlib.sha256(secret.encode("utf-8")).hexdigest()


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


def _load_pending(state_dir: str) -> dict | None:
    path = os.path.join(state_dir, PENDING_FILE)
    try:
        _assert_private(path)
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
        if all(isinstance(data.get(k), str) and data[k] for k in ("code", "salt", "poll_secret", "request_id")):
            return data
    except (OSError, ValueError, CredentialFileError):
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
    poll_interval: float = 3.0,
    sleep: Callable[[float], None] = time.sleep,
    max_polls: int | None = None,
    capabilities: dict | None = None,
) -> Credential:
    """Return a Credential, enrolling first if none is stored.

    Raises EnrollRejected on rejection.  An expired request is replaced by a
    fresh one (new pairing code, shown again).  A restart while waiting resumes
    the same request and re-shows the same code.
    """
    existing = load_credential(state_dir)
    if existing:
        return existing
    if not OWNER_RE.fullmatch(owner or ""):
        raise EnrollError("owner must be lowercase letters and digits")

    polls = 0
    while True:
        pending = _load_pending(state_dir)
        if pending is None:
            pending = _apply(state_dir, owner, transport, capabilities or {})
        show(pending["code"])
        while True:
            reply = transport.get_json(
                f"/api/enroll/{pending['request_id']}",
                {"X-Poll-Secret": pending["poll_secret"]},
            )
            status = reply.get("status")
            if status == "approved":
                name, secret = str(reply.get("agent_name") or ""), str(reply.get("credential") or "")
                if not name or not secret:
                    raise EnrollError("hub approved without a credential")
                cred = Credential(agent_name=name, secret=secret)
                save_credential(state_dir, cred)  # persist before dropping the pending state
                _clear_pending(state_dir)
                return cred
            if status == "rejected":
                _clear_pending(state_dir)
                raise EnrollRejected("owner rejected this client")
            if status == "expired":
                _clear_pending(state_dir)
                break  # outer loop: fresh request, fresh code
            if status != "pending":
                raise EnrollError(f"unexpected enroll status: {status!r}")
            polls += 1
            if max_polls is not None and polls >= max_polls:
                raise EnrollError("gave up waiting for approval")
            sleep(poll_interval)


def _apply(state_dir: str, owner: str, transport: Transport, capabilities: dict) -> dict:
    code = generate_pairing_code()
    salt = secrets.token_hex(8)
    poll_secret = secrets.token_urlsafe(24)
    reply = transport.post_json("/api/enroll/apply", {
        "owner": owner,
        "pairing_salt": salt,
        "pairing_digest": pairing_digest(code, salt),
        "poll_digest": secret_digest(poll_secret),
        "device_digest": device_fingerprint(state_dir),
        "client_version": __version__,
        "capabilities": capabilities,
    })
    request_id = str(reply.get("request_id") or "")
    if not request_id:
        raise EnrollError("hub did not return a request_id")
    pending = {"code": code, "salt": salt, "poll_secret": poll_secret, "request_id": request_id}
    write_private_file(os.path.join(state_dir, PENDING_FILE), json.dumps(pending))
    return pending


def auth_headers(cred: Credential) -> dict[str, str]:
    """Headers for the dedicated-credential WebSocket handshake (draft: Bearer)."""
    return {"Authorization": f"Bearer {cred.secret}"}


AGENT_WS_PATH = "/ws/agent"  # placeholder until the Hub fixes the path (ENROLL-API-DRAFT s.4)


def agent_ws_url(ws_base: str, query: dict[str, str] | None = None) -> str:
    """Connect URL for a dedicated-credential agent.

    Identity (name.owner) is resolved by the Hub from the credential, so the
    URL carries neither the agent name nor any account key: a self-declared
    name is exactly the impersonation entry point.
    """
    q = {"version": __version__, **(query or {})}
    for forbidden in ("key_id", "name", "agent_name", "session_name"):
        q.pop(forbidden, None)
    return f"{ws_base.rstrip('/')}{AGENT_WS_PATH}?{urlencode(q)}"
