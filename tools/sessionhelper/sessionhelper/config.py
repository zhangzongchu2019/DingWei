"""Environment-driven sessionHelper configuration."""

from __future__ import annotations

import os
import shlex
import subprocess
from dataclasses import dataclass, field
from urllib.parse import urlencode

from .protocol import required_env


@dataclass
class Config:
    session_name: str
    key_id: str
    secret: str
    ws_base: str = "ws://127.0.0.1:8791"
    bot_name: str = "UnifiedRobot"
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
    busy_reply_text: str = "消息已收到，但此时忙，稍后处理你的请求"
    provision_allowed_hosts: tuple[str, ...] = ("ts.wegoab.com",)
    provision_audit_db: str = ""
    provision_rollback_timeout: int = 300

    @property
    def ws_url(self) -> str:
        query = {"key_id": self.key_id}
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
        return f"{self.ws_base.rstrip('/')}/ws/session/{self.session_name}?{urlencode(query)}"


def load_config(env: dict[str, str] | None = None) -> Config:
    env = env or os.environ
    cli_cmd = shlex.split(env.get("SH_CLI_CMD", ""))
    session_name = required_env("SH_SESSION_NAME", env)
    return Config(
        session_name=session_name,
        key_id=required_env("SH_KEY_ID", env),
        secret=required_env("SH_SECRET", env),
        ws_base=env.get("SH_WS_BASE", "ws://127.0.0.1:8791"),
        bot_name=env.get("SH_BOT_NAME", "UnifiedRobot"),
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
        busy_reply_text=env.get("SH_BUSY_REPLY_TEXT", "消息已收到，但此时忙，稍后处理你的请求"),
        provision_allowed_hosts=tuple(
            item.strip().lower()
            for item in env.get("SH_PROVISION_ALLOWED_HOSTS", "ts.wegoab.com").split(",")
            if item.strip()
        ),
        provision_audit_db=expand_path(env.get("SH_PROVISION_AUDIT_DB", "")),
        provision_rollback_timeout=int(env.get("SH_PROVISION_ROLLBACK_TIMEOUT", "300")),
    )


def truthy(value: str) -> bool:
    return value.strip().lower() in ("1", "true", "yes", "on")


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
