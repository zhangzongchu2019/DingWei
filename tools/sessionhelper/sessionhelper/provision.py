"""Secure provision actions for sessionHelper."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import sqlite3
import tarfile
import tempfile
import time
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlparse

from . import __version__
from .config import Config, detect_os


class ProvisionError(RuntimeError):
    pass


@dataclass(frozen=True)
class ProvisionResult:
    action: str
    target: str
    version: str
    ok: bool
    message: str = ""
    from_version: str = ""
    to_version: str = ""

    def to_meta(self) -> dict:
        return {
            "type": "provision_ack",
            "action": self.action,
            "target": self.target,
            "version": self.version,
            "ok": self.ok,
            "message": self.message,
            "from_version": self.from_version,
            "to_version": self.to_version,
            "system": True,
            "no_mirror": True,
        }


def is_provision_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "provision"


class Provisioner:
    def __init__(self, cfg: Config, package_root: str | None = None):
        self.cfg = cfg
        self.package_root = Path(package_root or Path(__file__).resolve().parents[1]).expanduser().resolve()
        self.audit_db = Path(cfg.provision_audit_db or Path.home() / ".dingwei" / "sessionhelper_audit.db").expanduser()
        self.version_file = Path.home() / ".dingwei" / "sessionhelper_versions.json"
        self.update_state_file = Path.home() / ".dingwei" / "sessionhelper_update_state.json"

    def handle(self, env: dict) -> ProvisionResult:
        meta = env.get("meta") or {}
        action = str(meta.get("action") or "").strip()
        target = str(meta.get("target") or "").strip()
        version = str(meta.get("version") or "").strip()
        try:
            self.validate_source(env)
            self.validate_meta(meta)
            if self.is_installed(action, target, version):
                result = ProvisionResult(action, target, version, True, "already installed", __version__, version)
            else:
                artifact = self.download_and_verify(str(meta["url"]), str(meta["sha256"]))
                if action == "update_self":
                    result = self.update_self(artifact, version)
                elif action == "install_skill":
                    result = self.install_skill(artifact, target, version)
                elif action == "install_mcp":
                    result = self.install_mcp(artifact, target, version, meta.get("extra") or {})
                else:
                    raise ProvisionError(f"unsupported action: {action}")
                if result.ok:
                    self.record_version(action, target, version)
        except Exception as exc:  # noqa: BLE001 - provision must audit every failure.
            result = ProvisionResult(action or "unknown", target, version, False, str(exc), __version__, version)
        self.audit(env, result)
        return result

    def validate_source(self, env: dict) -> None:
        meta = env.get("meta") or {}
        expected_from = f"workpulse#{self.cfg.key_id}"
        if str(env.get("from") or "") != expected_from or meta.get("system") is not True:
            raise ProvisionError("provision source denied")

    def validate_meta(self, meta: dict) -> None:
        for key in ("action", "url", "sha256", "version", "target"):
            if not str(meta.get(key) or "").strip():
                raise ProvisionError(f"missing provision field: {key}")
        sha = str(meta.get("sha256") or "").strip().lower()
        if len(sha) != 64 or any(ch not in "0123456789abcdef" for ch in sha):
            raise ProvisionError("invalid sha256")
        host = (urlparse(str(meta.get("url") or "")).hostname or "").lower()
        allowed = {h.lower() for h in self.cfg.provision_allowed_hosts}
        if not host or host not in allowed:
            raise ProvisionError(f"url host not allowed: {host}")

    def download_and_verify(self, url: str, expected_sha: str) -> Path:
        tmpdir = Path(tempfile.mkdtemp(prefix="sessionhelper-provision-"))
        dst = tmpdir / "artifact"
        ctx = None
        cafile = os.environ.get("SSL_CERT_FILE", "").strip()
        if cafile:
            import ssl

            ctx = ssl.create_default_context(cafile=os.path.expanduser(cafile))
        h = hashlib.sha256()
        with urllib.request.urlopen(url, timeout=30, context=ctx) as resp, dst.open("wb") as f:  # noqa: S310 - host allowlist + sha256 gate.
            while True:
                chunk = resp.read(1024 * 1024)
                if not chunk:
                    break
                h.update(chunk)
                f.write(chunk)
        got = h.hexdigest()
        if got.lower() != expected_sha.lower():
            raise ProvisionError(f"sha256 mismatch: {got}")
        return dst

    def is_installed(self, action: str, target: str, version: str) -> bool:
        installed = self.load_versions().get(self.version_key(action, target), "")
        return bool(installed) and compare_versions(installed, version) >= 0

    def record_version(self, action: str, target: str, version: str) -> None:
        data = self.load_versions()
        data[self.version_key(action, target)] = version
        self.version_file.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.version_file.with_suffix(".tmp")
        tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
        os.replace(tmp, self.version_file)

    def load_versions(self) -> dict[str, str]:
        try:
            return json.loads(self.version_file.read_text(encoding="utf-8"))
        except Exception:
            return {}

    def version_key(self, action: str, target: str) -> str:
        return f"{action}:{target or 'self'}"

    def update_self(self, artifact: Path, version: str) -> ProvisionResult:
        if compare_versions(__version__, version) >= 0:
            return ProvisionResult("update_self", "self", version, True, "current version is newer or equal", __version__, version)
        backup = Path(tempfile.mkdtemp(prefix="sessionhelper-backup-")) / "sessionhelper"
        shutil.copytree(self.package_root, backup, ignore=shutil.ignore_patterns(".venv", "__pycache__", "*.pyc"))
        try:
            self.extract_package(artifact, self.package_root)
            self.write_update_state(version, backup)
            self.request_restart()
            return ProvisionResult("update_self", "self", version, True, "installed; restart requested", __version__, version)
        except Exception:
            self.restore_backup(backup)
            raise

    def write_update_state(self, version: str, backup: Path) -> None:
        state = {
            "version": version,
            "backup": str(backup),
            "platform": detect_os(),
            "restart": self.restart_strategy(),
            "ts": int(time.time()),
        }
        self.update_state_file.parent.mkdir(parents=True, exist_ok=True)
        self.update_state_file.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")

    def confirm_update_connected(self) -> None:
        state = self.load_update_state()
        if not state or state.get("status") == "confirmed":
            return
        state["status"] = "confirmed"
        state["connected_at"] = int(time.time())
        self.update_state_file.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")

    def rollback_stale_update_if_needed(self, timeout_seconds: int = 30) -> bool:
        state = self.load_update_state()
        if not state or state.get("status") == "confirmed":
            return False
        ts = int(state.get("ts") or 0)
        if ts <= 0 or int(time.time()) - ts < timeout_seconds:
            return False
        backup = Path(str(state.get("backup") or "")).expanduser()
        self.restore_backup(backup)
        state["status"] = "rolled_back"
        state["rolled_back_at"] = int(time.time())
        self.update_state_file.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
        self.request_restart()
        return True

    def load_update_state(self) -> dict:
        try:
            data = json.loads(self.update_state_file.read_text(encoding="utf-8"))
            return data if isinstance(data, dict) else {}
        except Exception:
            return {}

    def request_restart(self) -> None:
        # Guard/launchd supervise the process. Tests can disable exit by setting the env var.
        if os.environ.get("SH_PROVISION_NO_EXIT") == "1":
            return
        os._exit(0)

    def restart_strategy(self) -> str:
        os_name = detect_os()
        if os_name == "macos":
            return "launchd"
        if os_name == "linux":
            return "cron_guard"
        return "unsupported"

    def restore_backup(self, backup: Path) -> None:
        if not backup.exists():
            return
        for item in self.package_root.iterdir():
            if item.name == ".venv":
                continue
            if item.is_dir():
                shutil.rmtree(item)
            else:
                item.unlink()
        for item in backup.iterdir():
            dst = self.package_root / item.name
            if item.is_dir():
                shutil.copytree(item, dst)
            else:
                shutil.copy2(item, dst)

    def extract_package(self, artifact: Path, target_dir: Path) -> None:
        target_dir = target_dir.expanduser().resolve()
        target_dir.mkdir(parents=True, exist_ok=True)
        if tarfile.is_tarfile(artifact):
            with tarfile.open(artifact) as tf:
                safe_extract(tf, target_dir)
        else:
            raise ProvisionError("artifact must be a tar archive")

    def install_skill(self, artifact: Path, target: str, version: str) -> ProvisionResult:
        base = skill_base_dir(self.cfg.cli_name)
        if not safe_target_name(target):
            raise ProvisionError("invalid skill target")
        target_dir = base / target
        staging = Path(tempfile.mkdtemp(prefix="sessionhelper-skill-")) / target
        self.extract_package(artifact, staging)
        if target_dir.exists():
            shutil.rmtree(target_dir)
        target_dir.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(staging), str(target_dir))
        return ProvisionResult("install_skill", target, version, True, "skill installed", __version__, version)

    def install_mcp(self, artifact: Path, target: str, version: str, extra: dict) -> ProvisionResult:
        if not safe_target_name(target):
            raise ProvisionError("invalid mcp target")
        text = artifact.read_text(encoding="utf-8")
        server = parse_mcp_server(target, text)
        if self.cfg.cli_name == "codex":
            path = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))).expanduser() / "config.toml"
            self.install_codex_mcp(path, target, server, text, version)
        elif self.cfg.cli_name == "claude":
            path = Path.home() / ".claude.json"
            self.install_json_mcp(path, target, server)
        else:
            path = Path.home() / ".config" / self.cfg.cli_name / "mcp.json"
            self.install_json_mcp(path, target, server)
        return ProvisionResult("install_mcp", target, version, True, "mcp installed", __version__, version)

    def install_codex_mcp(self, path: Path, target: str, server: dict, raw_text: str, version: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        existing = path.read_text(encoding="utf-8") if path.exists() else ""
        marker = f"# dingwei-mcp:{target}:{version}"
        if marker in existing:
            return
        snippet = raw_text.rstrip()
        if server:
            snippet = codex_mcp_toml(target, server).rstrip()
        with path.open("a", encoding="utf-8") as f:
            if existing and not existing.endswith("\n"):
                f.write("\n")
            f.write(marker + "\n")
            f.write(snippet + "\n")

    def install_json_mcp(self, path: Path, target: str, server: dict) -> None:
        if not server:
            raise ProvisionError("json mcp artifact must define a server")
        path.parent.mkdir(parents=True, exist_ok=True)
        data: dict = {}
        if path.exists() and path.stat().st_size > 0:
            try:
                loaded = json.loads(path.read_text(encoding="utf-8"))
            except json.JSONDecodeError as exc:
                raise ProvisionError(f"existing mcp config is not json: {path}") from exc
            if not isinstance(loaded, dict):
                raise ProvisionError(f"existing mcp config must be an object: {path}")
            data = loaded
        servers = data.setdefault("mcpServers", {})
        if not isinstance(servers, dict):
            raise ProvisionError("existing mcpServers must be an object")
        servers[target] = server
        tmp = path.with_suffix(path.suffix + ".tmp")
        tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        os.replace(tmp, path)

    def audit(self, env: dict, result: ProvisionResult) -> None:
        self.audit_db.parent.mkdir(parents=True, exist_ok=True)
        with sqlite3.connect(self.audit_db) as conn:
            conn.execute(
                "CREATE TABLE IF NOT EXISTS provision_audit(ts INTEGER, action TEXT, target TEXT, version TEXT, ok INTEGER, message TEXT, source TEXT)"
            )
            conn.execute(
                "INSERT INTO provision_audit VALUES(?,?,?,?,?,?,?)",
                (
                    int(time.time()),
                    result.action,
                    result.target,
                    result.version,
                    1 if result.ok else 0,
                    result.message,
                    str(env.get("from") or ""),
                ),
            )


def safe_extract(tf: tarfile.TarFile, target_dir: Path) -> None:
    base = target_dir.resolve()
    for member in tf.getmembers():
        if member.issym() or member.islnk():
            raise ProvisionError("tar links are not allowed")
        dest = (base / member.name).resolve()
        if not str(dest).startswith(str(base) + os.sep) and dest != base:
            raise ProvisionError("tar path escapes target")
    try:
        tf.extractall(base, filter="data")
    except TypeError:
        tf.extractall(base)


def safe_target_name(target: str) -> bool:
    path = Path(str(target or ""))
    return bool(str(target or "").strip()) and not path.is_absolute() and all(part not in {"", ".", ".."} for part in path.parts)


def skill_base_dir(cli_name: str) -> Path:
    cli = (cli_name or "").lower()
    if cli == "codex":
        return Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))).expanduser() / "skills"
    if cli == "opencode":
        return Path.home() / ".config" / "opencode" / "skills"
    return Path.home() / ".claude" / "skills"


def parse_mcp_server(target: str, text: str) -> dict:
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return {}
    if not isinstance(data, dict):
        raise ProvisionError("mcp artifact json must be an object")
    servers = data.get("mcpServers")
    if isinstance(servers, dict):
        if target in servers and isinstance(servers[target], dict):
            return dict(servers[target])
        if len(servers) == 1:
            only = next(iter(servers.values()))
            if isinstance(only, dict):
                return dict(only)
        raise ProvisionError("mcpServers must contain the target server")
    if target in data and isinstance(data[target], dict):
        return dict(data[target])
    if "command" in data:
        return dict(data)
    raise ProvisionError("mcp artifact must contain command or mcpServers")


def codex_mcp_toml(target: str, server: dict) -> str:
    lines = [f'[mcp_servers."{toml_escape(target)}"]']
    for key in ("command", "args", "env", "cwd"):
        if key not in server:
            continue
        value = server[key]
        if key == "env":
            if not isinstance(value, dict):
                raise ProvisionError("mcp env must be an object")
            lines.append("[mcp_servers.%s.env]" % toml_quote_key(target))
            for env_key in sorted(value):
                lines.append(f'{toml_quote_key(str(env_key))} = {toml_value(value[env_key])}')
        else:
            lines.append(f"{key} = {toml_value(value)}")
    return "\n".join(lines) + "\n"


def toml_quote_key(value: str) -> str:
    return '"' + toml_escape(value) + '"'


def toml_escape(value: str) -> str:
    return str(value).replace("\\", "\\\\").replace('"', '\\"')


def toml_value(value) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int | float):
        return str(value)
    if isinstance(value, list):
        return "[" + ", ".join(toml_value(item) for item in value) + "]"
    if value is None:
        return '""'
    return '"' + toml_escape(str(value)) + '"'


def compare_versions(left: str, right: str) -> int:
    def parts(value: str) -> list[object]:
        out: list[object] = []
        for token in str(value or "").replace("-", ".").split("."):
            if token.isdigit():
                out.append((0, int(token)))
            else:
                out.append((1, token))
        return out

    a, b = parts(left), parts(right)
    return (a > b) - (a < b)
