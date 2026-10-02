"""起真 sessionhelper 子进程的 smoke 共用：环境隔离＋IPC 路径机械闸（组长 R-SMOKE-IPC-HIJACK-20261002154248）。

事故：smoke 以 os.environ 为底给 helper 配环境；在有在线 helper 的席位上，os.environ 带着在线 helper 的
SH_MCP_IPC_SOCK（/run/sessionhelper/dingwei-mcp.sock）。没被覆盖的那个 helper 继承了真路径，而 helper 启动时
先删后绑（sessionhelper/ipc_listener.py），于是抢走在线 helper 的 IPC 路径；它退出后只剩死 socket，
该席 send.py 发不出信，只能重启在线 helper 恢复。同时 helper 还会往 HOME 下的 ~/.dingwei 装 send.py、写在线清单。

本模块：
- isolated_helper_env：剔除继承的 SH_ 与 DINGWEI_ 前缀变量，HOME 与 IPC 路径都指向本次临时目录；
- spawn_helper：启动前断言 IPC 路径与 HOME 都在本次临时目录内，否则直接退出、不启动（机械闸）。
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

INHERITED_PREFIXES = ("SH_", "DINGWEI_")
IPC_ENV_KEYS = ("SH_MCP_IPC_SOCK", "DINGWEI_MCP_IPC_SOCK")  # config.py：后者是前者的回落


def isolated_helper_env(tmp: Path, name: str, overrides: dict[str, str]) -> dict[str, str]:
    base = {k: v for k, v in os.environ.items() if not k.startswith(INHERITED_PREFIXES)}
    home = Path(tmp) / f"home-{name}"
    home.mkdir(parents=True, exist_ok=True)
    env = {**base, "HOME": str(home), "SH_MCP_IPC_SOCK": str(Path(tmp) / f"{name}-mcp.sock")}
    env.update(overrides)
    return env


def _inside(path: str, root: Path) -> bool:
    real = os.path.realpath(path)
    top = os.path.realpath(root)
    return real != top and os.path.commonpath([real, top]) == top


def check_helper_env_isolated(env: dict[str, str], tmp: Path) -> None:
    """机械闸：IPC 路径（含回落键）与 HOME 必须在本次临时目录内；不满足即 SystemExit，调用方不得启动 helper。"""
    sock = env.get("SH_MCP_IPC_SOCK", "")
    if not sock:
        raise SystemExit("smoke 闸：helper 没有显式 SH_MCP_IPC_SOCK（会回落到继承值或默认真路径），拒绝启动")
    for key in IPC_ENV_KEYS:
        val = env.get(key)
        if val is not None and not _inside(val, tmp):
            raise SystemExit(f"smoke 闸：{key}={val!r} 不在本次临时目录 {tmp} 内，拒绝启动 helper")
    home = env.get("HOME", "")
    if not home or not _inside(home, tmp):
        raise SystemExit(f"smoke 闸：HOME={home!r} 不在本次临时目录 {tmp} 内，拒绝启动 helper")


def spawn_helper(cmd: list[str], tmp: Path, env: dict[str, str], **popen_kw) -> subprocess.Popen:
    check_helper_env_isolated(env, tmp)
    return subprocess.Popen(cmd, env=env, **popen_kw)


def real_ipc_state(path: str = "/run/sessionhelper/dingwei-mcp.sock") -> str:
    """跑前跑后各打一次：真路径的 inode／mtime。只 stat，不连接、不发信。"""
    try:
        st = os.stat(path)
    except OSError as exc:
        return f"real-ipc {path}: {type(exc).__name__}"
    return f"real-ipc {path}: inode={st.st_ino} mtime_ns={st.st_mtime_ns}"


def print_real_ipc_state(when: str) -> str:
    state = real_ipc_state()
    print(f"[smoke] {when} {state}", file=sys.stderr, flush=True)
    return state
