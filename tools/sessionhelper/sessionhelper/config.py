"""Environment-driven sessionHelper configuration."""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import time
from dataclasses import dataclass, field
from urllib.parse import urlencode

from . import __version__
from .protocol import required_env


NAME_PART_RE = re.compile(r"^[a-z0-9]+$")
SESSION_NAME_RE = re.compile(r"^[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4}$")
INBOX_FILENAME_RE = re.compile(r"[^a-z0-9_-]+")


@dataclass
class Config:
    session_name: str
    key_id: str
    secret: str
    owner: str = ""
    ws_base: str = "ws://127.0.0.1:8791"
    bot_name: str = "ExampleBot"
    mode: str = "echo"
    provider: str = "deepseek"
    tool: str = ""
    os_name: str = ""
    session_full_name: str = ""
    api_key: str = ""
    base_url: str = ""
    model: str = ""
    system_prompt: str = "You are a helpful assistant in a WorkPulse session."
    history_turns: int = 12
    cli_name: str = "claude"
    cli_launch: str = ""
    cli_cmd: list[str] = field(default_factory=list)
    cli_cwd: str = ""
    cli_transcript: str = ""
    opencode_db: str = ""
    cli_ready_timeout: float = 90.0
    cli_reply_wait: float = 45.0
    cli_settle_seconds: float = 1.2
    cli_user_ack_wait: float = 10.0
    cli_launch_retries: int = 3
    outbox: str = ""
    reconnect_min: float = 1.0
    reconnect_max: float = 30.0
    open_timeout: float = 30.0
    proxy: str = ""
    debug: bool = False
    mirror_to: str = ""
    collect: bool = True
    target_group: str = ""
    target_bot: str = ""
    producer: bool = False
    no_directory: bool = False
    async_reply: bool = False
    agent_route: bool = True
    busy_buffer_max: int = 100
    turn_terminal_timeout_seconds: float = 1800.0
    inbox_db: str = ""
    busy_reply_text: str = "消息已收到，但此时忙，稍后处理你的请求"
    provision_allowed_hosts: tuple[str, ...] = ("localhost",)
    provision_audit_db: str = ""
    provision_versions_file: str = ""
    provision_update_state_file: str = ""
    provision_rollback_timeout: int = 300
    device_id: str = ""
    device_id_v1: bool = False

    @property
    def registered_session_name(self) -> str:
        if not self.owner:
            return ""
        return build_session_name(self.owner, self.session_name, self.key_id)

    @property
    def ws_url(self) -> str:
        query = {"key_id": self.key_id, "version": __version__}
        if self.tool:
            query["tool"] = self.tool
        if self.os_name:
            query["os"] = self.os_name
        if self.model:
            query["model"] = self.model
        if self.session_full_name and self.session_full_name != self.session_name:
            query["full_session_name"] = self.session_full_name
        if self.producer:
            query["producer"] = "1"
        if self.no_directory:
            query["no_directory"] = "1"
        if self.cli_launch == "pty":
            query["terminal"] = "1"  # 仅 PTY 客户端支持网页终端,清单据此决定是否给 /view 入口
        if self.target_group:
            query["target_group"] = self.target_group
        if self.target_bot:
            query["target_bot"] = self.target_bot
        if self.mirror_to:
            query["mirror_to"] = self.mirror_to
        if self.device_id_v1 and self.device_id:
            query["device_id_v1"] = "1"
            query["device_id"] = self.device_id
        return f"{self.ws_base.rstrip('/')}/ws/session/{self.session_name}?{urlencode(query)}"


def load_config(env: dict[str, str] | None = None) -> Config:
    env = env or os.environ
    cli_cmd = shlex.split(env.get("SH_CLI_CMD", ""))
    short_session_name = required_session_env("SH_SESSION_NAME", env).strip()
    key_id = required_env("SH_KEY_ID", env).strip()
    session_name = normalize_client_session_name(short_session_name)
    owner = env.get("SH_OWNER", "").strip()
    if owner and not NAME_PART_RE.fullmatch(owner):
        raise SystemExit("SH_OWNER 不合规: 只能使用小写字母数字。")
    return Config(
        session_name=session_name,
        key_id=key_id,
        secret=required_env("SH_SECRET", env),
        owner=owner,
        device_id_v1=env.get("SH_DEVICE_ID_V1", "").strip().lower() in ("1", "true", "yes", "on"),
        device_id=load_device_id(short_session_name, key_id, env) if env.get("SH_DEVICE_ID_V1", "").strip().lower() in ("1", "true", "yes", "on") else "",
        ws_base=env.get("SH_WS_BASE", "ws://127.0.0.1:8791"),
        bot_name=env.get("SH_BOT_NAME", "ExampleBot"),
        mode=env.get("SH_MODE", env.get("SH_ADAPTER", "echo")).lower(),
        provider=env.get("SH_PROVIDER", "deepseek").lower(),
        tool=env.get("SH_TOOL", env.get("SH_CLI", "")).upper(),
        os_name=detect_os(),
        session_full_name=detect_full_session_name(session_name, env),
        api_key=env.get("SH_API_KEY", ""),
        base_url=env.get("SH_BASE_URL", ""),
        model=env.get("SH_MODEL", ""),
        system_prompt=env.get("SH_SYSTEM_PROMPT", "You are a helpful assistant in a WorkPulse session."),
        history_turns=int(env.get("SH_HISTORY_TURNS", "12")),
        cli_name=env.get("SH_CLI", "claude").lower(),
        cli_launch=env.get("SH_CLI_LAUNCH", "").lower(),
        cli_cmd=cli_cmd,
        cli_cwd=expand_path(env.get("SH_CLI_CWD", "")),
        cli_transcript=expand_path(env.get("SH_CLI_TRANSCRIPT", "")),
        opencode_db=expand_path(env.get("SH_OPENCODE_DB", "")),
        cli_ready_timeout=float(env.get("SH_CLI_READY_TIMEOUT", "90")),
        cli_reply_wait=float(env.get("SH_CLI_REPLY_WAIT", env.get("SH_OUT_WAIT", "45"))),
        cli_settle_seconds=float(env.get("SH_CLI_SETTLE_SECONDS", "1.2")),
        cli_user_ack_wait=float(env.get("SH_CLI_USER_ACK_WAIT", "10")),
        cli_launch_retries=int(env.get("SH_CLI_LAUNCH_RETRIES", "3")),
        outbox=expand_path(env.get("SH_OUTBOX", "")),
        reconnect_min=float(env.get("SH_RECONNECT_MIN", "1")),
        reconnect_max=float(env.get("SH_RECONNECT_MAX", "30")),
        open_timeout=float(env.get("SH_OPEN_TIMEOUT", "30")),
        proxy=env.get("SH_PROXY", "").strip(),
        debug=env.get("SH_DEBUG", "").strip().lower() in ("1", "true", "yes", "on"),
        mirror_to=env.get("SH_MIRROR_TO", ""),
        collect=truthy(env.get("SH_COLLECT", "1")),
        target_group=env.get("SH_TARGET_GROUP", "").strip(),
        target_bot=env.get("SH_TARGET_BOT", "").strip(),
        producer=truthy(env.get("SH_PRODUCER", "0")),
        no_directory=truthy(env.get("SH_NO_DIRECTORY", "0")),
        async_reply=truthy(env.get("SH_ASYNC_REPLY", "0")),
        agent_route=truthy(env.get("SH_AGENT_ROUTE", "1")),
        busy_buffer_max=int(env.get("SH_BUSY_BUFFER_MAX", "100")),
        turn_terminal_timeout_seconds=max(0.1, float(env.get("SH_TURN_TERMINAL_TIMEOUT_SECONDS", "1800"))),
        inbox_db=expand_path(env.get("SH_INBOX_DB", default_inbox_db(session_name))),
        busy_reply_text=env.get("SH_BUSY_REPLY_TEXT", "消息已收到，但此时忙，稍后处理你的请求"),
        provision_allowed_hosts=tuple(
            item.strip().lower()
            for item in env.get("SH_PROVISION_ALLOWED_HOSTS", "localhost").split(",")
            if item.strip()
        ),
        provision_audit_db=expand_path(env.get("SH_PROVISION_AUDIT_DB", "")),
        provision_versions_file=expand_path(env.get("SH_PROVISION_VERSIONS_FILE", default_provision_versions_file(session_name))),
        provision_update_state_file=expand_path(env.get("SH_PROVISION_STATE_FILE", default_provision_update_state_file(session_name))),
        provision_rollback_timeout=int(env.get("SH_PROVISION_ROLLBACK_TIMEOUT", "300")),
    )


def load_device_id(session_name: str, key_id: str, env: dict[str, str]) -> str:
    """Persist a 64-bit startup identity so legitimate restarts reclaim the session."""
    configured = env.get("SH_DEVICE_ID", "").strip().lower()
    if re.fullmatch(r"[0-9a-f]{16}", configured):
        return configured
    root = os.path.expanduser(env.get("SH_STATE_DIR", "~/.dingwei"))
    safe = INBOX_FILENAME_RE.sub("-", f"{session_name}-{key_id}").strip("-")
    path = os.path.join(root, f"device-{safe}.id")
    try:
        with open(path, encoding="utf-8") as fh:
            existing = fh.read().strip().lower()
        if re.fullmatch(r"[0-9a-f]{16}", existing):
            return existing
    except OSError:
        pass
    value = f"{time.time_ns() & ((1 << 64) - 1):016x}"
    try:
        os.makedirs(root, mode=0o700, exist_ok=True)
        tmp = path + f".{os.getpid()}.tmp"
        with open(tmp, "w", encoding="utf-8") as fh:
            fh.write(value + "\n")
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)
    except OSError:
        pass
    return value


def truthy(value: str) -> bool:
    return value.strip().lower() in ("1", "true", "yes", "on")


def required_session_env(name: str, env: dict[str, str]) -> str:
    try:
        return required_env(name, env)
    except (RuntimeError, SystemExit) as exc:
        raise SystemExit(
            f"{name} is required。\n"
            "正确格式: SH_SESSION_NAME=<短名>。owner_key 与完整注册名由服务端按 key 绑定自动派生。\n"
            "本机示例: SH_SESSION_NAME=manager"
        ) from exc


def normalize_client_session_name(value: str) -> str:
    value = (value or "").strip()
    parsed = SESSION_NAME_RE.fullmatch(value)
    if parsed:
        value = value.split("-")[1]
    if not NAME_PART_RE.fullmatch(value):
        raise SystemExit(
            "SH_SESSION_NAME 不合规: 短名只能小写字母数字, 如 manager。\n"
            "正确格式: SH_SESSION_NAME=<短名>。owner_key 与完整注册名由服务端按 key 绑定自动派生。\n"
            f"本机示例: SH_SESSION_NAME={value or 'manager'}"
        )
    return value


def build_session_name(owner: str, short_name: str, key_id: str) -> str:
    owner = (owner or "").strip()
    short_name = (short_name or "").strip()
    key_id = (key_id or "").strip()
    key_tail = key_id[-4:].lower()
    example = f"{owner or 'owner1'}-{short_name or 'manager'}-{key_tail or '0000'}"
    if not NAME_PART_RE.fullmatch(owner):
        raise SystemExit(
            "SH_OWNER 不合规: 只能使用小写字母数字。\n"
            "正确格式: SH_OWNER=<owner_key>, SH_SESSION_NAME=<短名>, 客户端自动注册为 <owner_key>-<短名>-<key末4位>。\n"
            f"本机示例: SH_OWNER={owner or 'owner1'} SH_SESSION_NAME={short_name or 'manager'} -> {example}"
        )
    if not NAME_PART_RE.fullmatch(short_name):
        raise SystemExit(
            "SH_SESSION_NAME 不合规: 短名只能小写字母数字, 如 manager。\n"
            "正确格式: SH_OWNER=<owner_key>, SH_SESSION_NAME=<短名>, 客户端自动注册为 <owner_key>-<短名>-<key末4位>。\n"
            f"本机示例: SH_OWNER={owner} SH_SESSION_NAME={short_name or 'manager'} -> {example}"
        )
    session_name = f"{owner}-{short_name}-{key_tail}"
    if not SESSION_NAME_RE.fullmatch(session_name):
        raise SystemExit(
            "会话注册名不合规: 须为 <owner_key>-<短名>-<key末4位>。\n"
            "正确格式: SH_OWNER=<owner_key>, SH_SESSION_NAME=<短名>, 客户端自动注册为 <owner_key>-<短名>-<key末4位>。\n"
            f"本机示例: SH_OWNER={owner} SH_SESSION_NAME={short_name} -> {example}"
        )
    return session_name


def detect_os() -> str:
    """标识运行平台：linux / macos / windows（供 DingWei 端识别每个会话所在系统）。"""
    import platform

    sysname = platform.system().lower()
    if sysname == "darwin":
        return "macos"
    if sysname.startswith("win") or sysname == "cygwin":
        return "windows"
    return sysname or "linux"


def expand_path(value: str) -> str:
    """展开 ~ 和 $VAR，使配置里的路径跨 Linux/macOS 通用
    （~ 在 macOS 展开为 /Users/<名>，在 Linux 为 /home/<名>）。"""
    value = (value or "").strip()
    if not value:
        return value
    return os.path.expanduser(os.path.expandvars(value))


def safe_inbox_session_name(session_name: str) -> str:
    value = (session_name or "").strip().lower()
    value = INBOX_FILENAME_RE.sub("_", value).strip("_")
    return value or "session"


def default_inbox_db(session_name: str) -> str:
    return f"~/.dingwei/sessionhelper_inbox-{safe_inbox_session_name(session_name)}.db"


def default_provision_versions_file(session_name: str) -> str:
    # F3: 按会话隔离，避免单机多 helper 共用一份 ~/.dingwei/sessionhelper_versions.json 时互相串味
    # （陈旧记录会把新下发误判成 already installed）。
    return f"~/.dingwei/sessionhelper_versions-{safe_inbox_session_name(session_name)}.json"


def default_provision_update_state_file(session_name: str) -> str:
    return f"~/.dingwei/sessionhelper_update_state-{safe_inbox_session_name(session_name)}.json"


def detect_full_session_name(session_name: str, env: dict[str, str] | None = None) -> str:
    env = env or os.environ
    if env.get("TMUX"):
        try:
            name = subprocess.check_output(
                ["tmux", "display-message", "-p", "#{session_name}"],
                text=True,
                stderr=subprocess.DEVNULL,
                timeout=1.0,
            ).strip()
            if name:
                return name
        except Exception:
            pass
    configured = env.get("SH_SESSION_FULL", "").strip()
    if configured:
        return configured
    return session_name
