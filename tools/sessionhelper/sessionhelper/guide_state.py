"""Persistent staging for the agent-network onboarding guide.

The guide is transport/control-plane state, not a standalone user turn.  It is
kept until the first real business command is admitted by an adapter.
"""

from __future__ import annotations

import hashlib
import json
import os
import threading
from dataclasses import dataclass


GUIDE_CONTEXT_HEADER = (
    "[DingWei platform context; do not answer or repeat this section. "
    "Use it only while handling the business request below.]"
)
GUIDE_CONTEXT_FOOTER = "[End DingWei platform context; business request follows.]"


@dataclass(frozen=True)
class GuideClaim:
    identity: str
    version: str
    body_hash: str
    body: str


class StagedGuideStore:
    """One staged/consumed guide per helper identity, persisted atomically."""

    def __init__(self, path: str, identity: str):
        self.path = path
        self.identity = identity
        self._lock = threading.RLock()
        self._claimed: GuideClaim | None = None

    def stage(self, body: str, version: str = "") -> bool:
        body = str(body or "")
        if not body:
            return False
        body_hash = hashlib.sha256(body.encode("utf-8")).hexdigest()
        version = str(version or "").strip() or body_hash
        with self._lock:
            current = self._read()
            if (
                current.get("identity") == self.identity
                and current.get("version") == version
                and current.get("body_hash") == body_hash
                and current.get("state") in ("staged", "consumed")
            ):
                return True
            self._write({
                "identity": self.identity,
                "version": version,
                "body_hash": body_hash,
                "body": body,
                "state": "staged",
            })
            self._claimed = None
            return True

    def claim(self) -> GuideClaim | None:
        with self._lock:
            if self._claimed is not None:
                return None
            current = self._read()
            if current.get("identity") != self.identity or current.get("state") != "staged":
                return None
            claim = GuideClaim(
                identity=self.identity,
                version=str(current.get("version") or ""),
                body_hash=str(current.get("body_hash") or ""),
                body=str(current.get("body") or ""),
            )
            if not claim.version or not claim.body_hash or not claim.body:
                return None
            self._claimed = claim
            return claim

    def consume(self, claim: GuideClaim) -> bool:
        with self._lock:
            current = self._read()
            matches = (
                self._claimed == claim
                and current.get("identity") == claim.identity
                and current.get("version") == claim.version
                and current.get("body_hash") == claim.body_hash
                and current.get("state") == "staged"
            )
            if not matches:
                return False
            current["state"] = "consumed"
            self._write(current)
            self._claimed = None
            return True

    def release(self, claim: GuideClaim) -> None:
        with self._lock:
            if self._claimed == claim:
                self._claimed = None

    def state(self) -> dict:
        with self._lock:
            return dict(self._read())

    @staticmethod
    def merge(claim: GuideClaim, business_body: str) -> str:
        return "\n\n".join((
            f"{GUIDE_CONTEXT_HEADER}\nversion={claim.version}\n{claim.body}",
            GUIDE_CONTEXT_FOOTER,
            str(business_body or ""),
        ))

    def _read(self) -> dict:
        try:
            with open(self.path, encoding="utf-8") as stream:
                value = json.load(stream)
            return value if isinstance(value, dict) else {}
        except (OSError, ValueError, TypeError):
            return {}

    def _write(self, value: dict) -> None:
        directory = os.path.dirname(self.path) or "."
        os.makedirs(directory, mode=0o700, exist_ok=True)
        tmp = f"{self.path}.{os.getpid()}.{threading.get_ident()}.tmp"
        try:
            with open(tmp, "w", encoding="utf-8") as stream:
                json.dump(value, stream, ensure_ascii=False, sort_keys=True)
                stream.write("\n")
                stream.flush()
                os.fsync(stream.fileno())
            os.chmod(tmp, 0o600)
            os.replace(tmp, self.path)
        finally:
            try:
                os.unlink(tmp)
            except FileNotFoundError:
                pass
