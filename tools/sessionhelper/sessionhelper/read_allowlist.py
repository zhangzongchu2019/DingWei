"""Persistent, read-only attachment host policy for Antigravity sessions."""
from __future__ import annotations

import json
import ipaddress
import os
import re
from pathlib import Path

from .config import env_get


DEFAULT_HOSTS = (
    "cmjrt.com", "szuma.met", "szuma.net", "lmjzzc.com", "ruihtang.com",
    "szwego.com", "wegoab.com", "myqcloud.com", "qq.com", "google.com",
    "aws.com", "amazonaws.com",
)
COMMAND = "/read-allowlist"


def authorized_envelope(env: dict, key_id: str, mode: str) -> bool:
    meta = env.get("meta") or {}
    return (mode == "driver" and env.get("from") == f"workpulse#{key_id}"
            and meta.get("source_kind") == "user" and meta.get("auth_mode") == "view_page_v2"
            and meta.get("console_principal_id") == "zhangzongchu")


def policy_path() -> Path:
    return Path(env_get("SH_READ_ALLOWLIST_FILE") or Path.home() / ".dingwei" / "read-allowlist.json")


def valid_host(host: str) -> bool:
    if not isinstance(host, str) or len(host) > 253 or "." not in host:
        return False
    try:
        ipaddress.ip_address(host)
        return False
    except ValueError:
        pass
    return all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", part)
               for part in host.split("."))


def load(path: Path | None = None) -> set[str]:
    path = path or policy_path()
    if not path.exists():
        return set(DEFAULT_HOSTS)
    data = json.loads(path.read_text(encoding="utf-8"))
    hosts = data.get("hosts") if isinstance(data, dict) else None
    if not isinstance(hosts, list) or not all(isinstance(item, str) and valid_host(item) for item in hosts):
        raise ValueError("invalid read allowlist")
    return set(hosts)


def matches(host: str, path: Path | None = None) -> bool:
    host = host.lower().rstrip(".")
    return any(host == domain or host.endswith("." + domain) for domain in load(path))


def update(action: str, host: str, path: Path | None = None) -> set[str]:
    path = path or policy_path()
    host = host.lower().strip().rstrip(".")
    if not valid_host(host):
        raise ValueError("invalid domain")
    hosts = load(path)
    if action == "add":
        hosts.add(host)
    elif action == "remove":
        hosts.discard(host)
    else:
        raise ValueError("invalid action")
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump({"hosts": sorted(hosts)}, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    return hosts


def execute(command: str, path: Path | None = None) -> str:
    parts = command.strip().split()
    if not parts or parts[0] != COMMAND:
        raise ValueError("unknown command")
    if len(parts) == 2 and parts[1] == "list":
        hosts = load(path)
    elif len(parts) == 3 and parts[1] in ("add", "remove"):
        hosts = update(parts[1], parts[2], path)
    else:
        raise ValueError("用法：/read-allowlist list | add 域名 | remove 域名")
    return "附件只读白名单：\n" + "\n".join(sorted(hosts))


def main(argv=None):
    import sys
    try:
        print(execute(" ".join([COMMAND, *(sys.argv[1:] if argv is None else argv)])))
    except (ValueError, OSError) as exc:
        raise SystemExit(str(exc)) from exc


if __name__ == "__main__":
    main()
