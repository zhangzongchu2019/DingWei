"""Interactive sessionHelper setup. Nothing is installed merely by importing it.

Run python -m sessionhelper.setup. Changes use native CLI commands and a private
profile; existing AI authentication, Agent credentials and Codex settings remain
in place. The native desktop auto-notification capability is reported separately.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlsplit

from .config import env_get
from .enroll import write_private_file, EnrollError

TOOLS = {
    "codex": {"package": "@openai/codex@latest", "login": ["codex", "login", "--device-auth"],
              "check": ["codex", "login", "status"], "probe": ["codex", "exec", "--skip-git-repo-check", "Reply exactly DW_MODEL_READY, do not use tools."]},
    "claude": {"package": "@anthropic-ai/claude-code@latest", "login": ["claude", "auth", "login"],
               "check": ["claude", "auth", "status"], "probe": ["claude", "-p", "Reply exactly DW_MODEL_READY, do not use tools."]},
    "opencode": {"package": "opencode-ai@latest", "login": ["opencode", "auth", "login"],
                 "check": None, "probe": ["opencode", "run", "Reply exactly DW_MODEL_READY, do not use tools."]},
}


def profile(mode, tool, hub, owner, short_name, agent, state_dir, model="", workspace=None, enroll_method="feishu"):
    if os.name != "posix":
        raise ValueError("this build requires POSIX local IPC; Windows native IPC is not yet verified")
    if mode not in ("desktop", "driver") or tool not in TOOLS:
        raise ValueError("unsupported connection mode or CLI")
    if mode == "desktop" and tool != "codex":
        raise ValueError("desktop integration uses Codex")
    if enroll_method not in ("feishu", "web"):
        raise ValueError("unsupported enrollment method")
    url = urlsplit(hub)
    if url.scheme != "https" or not url.hostname or url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        raise ValueError("Hub must be an HTTPS origin without credentials or a path")
    for value, maximum in ((owner, 32), (short_name, 16), (agent, 32)):
        if not re.fullmatch(r"[A-Za-z0-9]{2," + str(maximum) + "}", value):
            raise ValueError("user ID, short name and Agent name must be alphanumeric")
    if model and not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_/:.-]{0,127}", model):
        raise ValueError("invalid model identifier")
    if mode == "driver" and tool == "opencode" and model and "/" not in model:
        raise ValueError("OpenCode model must be provider/model")
    state = str(Path(state_dir).expanduser().resolve())
    if not state_dir.strip():
        raise ValueError("an independent state directory is required")
    if not os.path.isabs(state):
        raise ValueError("state directory must be absolute")
    if len(str(Path(state) / "dingwei-mcp.sock").encode("utf-8")) > 103:
        raise ValueError("本地 IPC 路径太长，请选择较短的独立状态目录，例如 ~/.dingwei-pm")
    return {"SH_MODE": mode, "SH_DRIVER": tool, "SH_ENROLL": "1", "SH_ENROLL_METHOD": enroll_method,
            "SH_ENROLL_OWNER": owner, "SH_ENROLL_SHORT_NAME": short_name, "SH_ENROLL_AGENT_NAME": agent,
            "SH_WS_BASE": "wss://" + url.netloc, "SH_STATE_DIR": state,
            "SH_INBOX_DB": str(Path(state) / "deliveries.sqlite"),
            "SH_EMIT_SESSION_EVENT": "1", "DINGWEI_MCP_IPC_SOCK": str(Path(state) / "dingwei-mcp.sock"),
            "SH_CLI_CWD": str(Path(workspace or Path.cwd()).expanduser().resolve()), "SH_MODEL": model,
            "SH_CLI": tool, "SH_TOOL": tool.upper()}


def write_profile(values):
    state = Path(values["SH_STATE_DIR"])
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    if state.stat().st_mode & 0o077:
        raise ValueError("state directory must be private (0700); choose an independent directory")
    path = state / "setup-profile.json"
    if path.exists():
        if path.is_symlink() or path.stat().st_mode & 0o077:
            raise ValueError("unsafe existing setup profile")
        old = json.loads(path.read_text())
        if old != values:
            raise ValueError("profile exists with different settings; preserve it and use explicit migration")
        return path
    write_private_file(str(path), json.dumps(values, ensure_ascii=False, indent=2))
    return path


def configure_mcp(values, config_path=None):
    # Desktop and CLI share Codex-host configuration. Configuring the desktop
    # does not require installing or launching a second Codex CLI session.
    import tomllib
    home = Path(env_get("CODEX_HOME") or "~/.codex").expanduser()
    path = Path(config_path) if config_path else home / "config.toml"
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.is_symlink():
        raise ValueError("Codex config is a symlink; use its managed configuration entry")
    original = path.read_bytes() if path.exists() else b""
    parsed = tomllib.loads(original.decode("utf-8"))
    name = "dingwei-" + values["SH_ENROLL_AGENT_NAME"].lower()
    quote = lambda value: json.dumps(value, ensure_ascii=True)
    frozen = bool(getattr(sys, "frozen", False))
    package_root = str(Path(__file__).resolve().parents[1])
    mcp_args = "[\"--mcp\"]" if frozen else "[\"-m\", \"sessionhelper.mcp_shim\"]"
    stanza = ("\n[mcp_servers." + quote(name) + "]\ncommand = " + quote(sys.executable) +
              "\nargs = " + mcp_args + "\n" +
              "[mcp_servers." + quote(name) + ".env]\n" +
              "DINGWEI_MCP_IPC_SOCK = " + quote(values["DINGWEI_MCP_IPC_SOCK"]) + "\n" +
              ("" if frozen else "PYTHONPATH = " + quote(package_root) + "\n"))
    desired = {"command": sys.executable,
               "args": ["--mcp"] if frozen else ["-m", "sessionhelper.mcp_shim"],
               "env": {"DINGWEI_MCP_IPC_SOCK": values["DINGWEI_MCP_IPC_SOCK"],
                       **({} if frozen else {"PYTHONPATH": package_root})}}
    existing = parsed.get("mcp_servers", {}).get(name)
    if existing is not None:
        if existing == desired:
            return name
        raise ValueError("MCP entry exists with different settings; preserve it for explicit migration")
    combined = original + stanza.encode("utf-8")
    check = tomllib.loads(combined.decode("utf-8"))
    prior = dict(check); prior["mcp_servers"] = dict(prior.get("mcp_servers", {}))
    prior["mcp_servers"].pop(name)
    if "mcp_servers" not in parsed and not prior["mcp_servers"]:
        prior.pop("mcp_servers")
    if prior != parsed:
        raise ValueError("MCP configuration would change existing settings")
    backup = path.with_name(path.name + ".before-" + name + "-0222")
    if backup.exists():
        raise ValueError("MCP backup already exists; preserve it and verify before retry")
    fd = os.open(backup, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "wb") as f:
        f.write(original); f.flush(); os.fsync(f.fileno())
    # Optimistic concurrency check: never overwrite concurrent app/user edits.
    if (path.read_bytes() if path.exists() else b"") != original:
        raise ValueError("Codex config changed concurrently; backup retained, no write")
    write_private_file(str(path), combined.decode("utf-8"))
    return name


def prepare_cli(tool, prompt=input, runner=subprocess.run, model=""):
    spec = TOOLS[tool]
    if prompt("安装/更新所选 CLI 的当前稳定版本？[y/N] ").strip().lower() != "y":
        if not shutil.which(tool):
            raise ValueError("selected CLI is not installed")
    else:
        npm = shutil.which("npm")
        if not npm:
            raise ValueError("Node.js/npm is required for this installation path; install it from its official source")
        runner([npm, "install", "-g", spec["package"]], check=True, timeout=600)
    runner([tool, "--version"], check=True, timeout=15)
    print("模型授权在所选工具自己的界面中完成；DW 不接收模型账号或密钥。")
    if prompt("现在进行原生登录/设备码授权？[y/N] ").strip().lower() == "y":
        runner(spec["login"], check=True, timeout=600)
    if spec["check"]:
        runner(spec["check"], check=True, timeout=30, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if prompt("进行一次真实模型就绪检查（可能计费）？[y/N] ").strip().lower() != "y":
        raise ValueError("model readiness remains unverified; setup was not marked ready")
    probe = list(spec["probe"])
    if model:
        probe.extend(["--model", model])
    result = runner(probe, check=True, timeout=180, capture_output=True, text=True)
    if not any(line.strip() == "DW_MODEL_READY" for line in result.stdout.splitlines()):
        raise ValueError("model readiness check did not return the expected result")
    # Never copy probe stdout/stderr or login output into setup files/logs.
    return "CLI_READY"


def start_background(path, values, popen=subprocess.Popen):
    state = Path(values["SH_STATE_DIR"])
    if os.path.lexists(state / "desktop-pilot.lock"):
        raise ValueError("stop the local desktop pilot before starting a real helper")
    pid_path = state / "helper-process.json"
    if pid_path.exists():
        saved = json.loads(pid_path.read_text())
        pid = saved.get("pid")
        if type(pid) is not int or pid <= 0:
            raise ValueError("invalid helper process record; preserve it for recovery")
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            pid_path.unlink()
        else:
            raise ValueError("recorded helper process exists; do not start a duplicate")
    log_path = state / "helper-private.log"
    if log_path.is_symlink():
        raise ValueError("helper log is a symlink")
    fd = os.open(log_path, os.O_CREAT | os.O_APPEND | os.O_WRONLY, 0o600)
    if os.fstat(fd).st_mode & 0o077:
        os.close(fd)
        raise ValueError("helper log must be private")
    try:
        reservation_fd = os.open(pid_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except BaseException:
        os.close(fd)
        raise
    with os.fdopen(reservation_fd, "w") as reservation:
        json.dump({"pid": os.getpid(), "status": "RESERVING"}, reservation)
        reservation.flush(); os.fsync(reservation.fileno())
    command = ([sys.executable, "--run-profile", str(path)] if getattr(sys, "frozen", False)
               else [sys.executable, "-m", "sessionhelper", "--run-profile", str(path)])
    # Ordinary inherited environment/auth remains with the original process;
    # no credential is inserted into command-line arguments or the profile.
    process = None
    try:
        with os.fdopen(fd, "ab") as log:
            process = popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                            start_new_session=True, close_fds=True,
                            cwd=str(Path(__file__).resolve().parents[1]) if not getattr(sys, "frozen", False) else str(state))
        write_private_file(str(pid_path), json.dumps({"pid": process.pid, "profile": str(path), "status": "STARTING"}))
    except BaseException:
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait(timeout=10)
        pid_path.unlink(missing_ok=True)
        raise
    return process.pid


def main(argv=None):
    parser = argparse.ArgumentParser(description="sessionHelper 安装后配置与飞书配对向导")
    parser.add_argument("--desktop-app", type=Path, help="macOS 上选择实际使用的 Codex.app/ChatGPT.app；仅读版本")
    parser.add_argument("--configure-only", action="store_true", help="仅配置，不申请账号或启动后台")
    parser.add_argument("--run-profile", type=Path, help="使用已保存的私有配置启动，不重复安装或模型授权")
    args = parser.parse_args(argv)
    if args.run_profile:
        path = args.run_profile.expanduser()
        if path.is_symlink() or path.stat().st_mode & 0o077:
            raise ValueError("profile must be a private regular file")
        values = json.loads(path.read_text())
        expected = profile(values["SH_MODE"], values["SH_DRIVER"], "https://" + urlsplit(values["SH_WS_BASE"]).netloc,
                           values["SH_ENROLL_OWNER"], values["SH_ENROLL_SHORT_NAME"], values["SH_ENROLL_AGENT_NAME"], values["SH_STATE_DIR"], values.get("SH_MODEL", ""), values["SH_CLI_CWD"], values["SH_ENROLL_METHOD"])
        if values != expected:
            # CWD is deliberately fixed by the saved profile, not silently changed on restart.
            expected["SH_CLI_CWD"] = values["SH_CLI_CWD"]
            if values != expected:
                raise ValueError("profile contains unsupported or changed settings")
        status_path = Path(values["SH_STATE_DIR"]) / "setup-status.json"
        if not status_path.is_file() or status_path.is_symlink() or status_path.stat().st_mode & 0o077:
            raise ValueError("finish the installer tool-readiness step before starting this profile")
        status = json.loads(status_path.read_text())
        required = "MCP_CONFIGURED" if values["SH_MODE"] == "desktop" else "CLI_READY"
        if status.get("tool_ready") != required or status.get("mode") != values["SH_MODE"]:
            raise ValueError("selected AI tool is not verified ready; resume the setup wizard")
        os.environ.update(values)
        from .app import main as run_helper
        return run_helper()
    print("两种接入方式都需要 sessionHelper。请选择：\n1. Codex 桌面应用 + DW-MCP\n2. CLI 托管（通过 DingWei Web 页面交互）")
    choice = input("选择 [1/2]：").strip()
    if choice not in ("1", "2"):
        raise ValueError("choose 1 or 2")
    mode = "desktop" if choice == "1" else "driver"
    tool = "codex" if mode == "desktop" else input("CLI [codex/claude/opencode]：").strip().lower()
    hub = input("Hub 地址 [https://dw123.cmjrt.com]：").strip() or "https://dw123.cmjrt.com"
    owner = input("希望使用的用户 ID（不是简称；无需预开户）：").strip()
    short = input("用户简称：").strip()
    agent = input("Agent 名称：").strip()
    state = input("独立状态目录：").strip()
    model = "" if mode == "desktop" else input("模型标识（OpenCode 用 provider/model；留空沿用工具默认）：").strip()
    workspace = input("工作目录（留空为当前目录）：").strip() or str(Path.cwd())
    enrollment = input("配对方式 [feishu/Web]（默认飞书一步开户与配对）：").strip().lower() or "feishu"
    values = profile(mode, tool, hub, owner, short, agent, state, model, workspace, enrollment)
    desktop_host = None
    if mode == "desktop":
        from .desktop_host import require_macos_desktop
        desktop_host = require_macos_desktop(selected=args.desktop_app)
    path = write_profile(values)
    if mode == "driver":
        prepare_cli(tool, model=model)
    else:
        print("请先在 Codex 桌面应用完成登录。自动提醒需真机验收；MCP 配置成功不代表自动唤醒已就绪。")
        if desktop_host["applications"]:
            app = desktop_host["applications"][0]
            print("本次桌面应用：", app["name"], "版本：", app["version"], "build：", app["build"])
        configure_mcp(values)
        print("桌面持续监听：在原聊天要求 AI 调用 dw_wait_message；收到消息后用 dw_reply_message 回复再等待。取消或退出后需重新发起，不能自动唤醒空闲会话。")
    write_private_file(str(Path(values["SH_STATE_DIR"]) / "setup-status.json"), json.dumps({
        "mode": mode, "tool_ready": "MCP_CONFIGURED" if mode == "desktop" else "CLI_READY",
        "desktop_host": desktop_host,
        "native_auto_notify_implemented": False,
        "native_auto_notify": "NOT_VERIFIED" if mode == "desktop" else "NOT_APPLICABLE"}))
    print("配置已保存。接下来复制 #pair 指令并私发给当前 Hub 的官方飞书机器人完成开户与配对；Web 方式请在控制台审批。")
    print("启动：", sys.executable, "-m sessionhelper.setup --run-profile", str(path))
    print("请按原生授权提示操作；CLI 模型授权码与 DW 配对码不同。")
    if args.configure_only:
        return
    from .enroll import prepare_client_env
    prepare_client_env(values, show=lambda code: print("配对指令：", code), notify=print)
    pid = start_background(path, values)
    print("sessionHelper 后台进程已启动，PID：", pid, "；当前为 STARTING，连接状态请在 Web 或 DW-MCP 核实。")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, EnrollError) as exc:
        print("配置未完成：", type(exc).__name__, str(exc) if isinstance(exc, ValueError) else "原生命令失败，未改凭据。", file=sys.stderr)
        raise SystemExit(1)
