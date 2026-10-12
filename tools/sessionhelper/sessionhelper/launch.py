"""Shared source/package entry point; no enrollment on an MCP stdio invocation."""
from __future__ import annotations

import sys

from .config import env_get


def main():
    if sys.argv[1:2] == ["--desktop-stop-hook"]:
        from .desktop_stop_hook import main as hook
        return hook(sys.argv[2:])
    if sys.argv[1:] == ["--version"]:
        from . import __version__
        print("sessionHelper " + __version__)
        return
    if "--desktop-endpoint-probe" in sys.argv[1:]:
        from .desktop_endpoint import main as probe
        return probe([arg for arg in sys.argv[1:] if arg != "--desktop-endpoint-probe"])
    if "--desktop-pilot" in sys.argv[1:]:
        from .desktop_pilot import main as pilot
        return pilot([arg for arg in sys.argv[1:] if arg != "--desktop-pilot"])
    if "--desktop-probe" in sys.argv[1:]:
        from .desktop_host import main as probe
        return probe([arg for arg in sys.argv[1:] if arg != "--desktop-probe"])
    if sys.argv[1:] == ["--mcp"]:
        from .mcp_shim import serve
        return serve()
    if "--setup" in sys.argv[1:] or "--run-profile" in sys.argv[1:] or "--cancel-pairing" in sys.argv[1:]:
        from .setup import main as setup
        return setup([arg for arg in sys.argv[1:] if arg != "--setup"])
    configured = env_get("SH_ENROLL") or (env_get("SH_KEY_ID") and env_get("SH_SECRET"))
    if not configured:
        if not sys.stdin.isatty():
            raise SystemExit("首次运行请在交互终端执行 sessionHelper --setup，或 python -m sessionhelper --setup。")
        from .setup import main as setup
        return setup([])
    from .app import main as helper
    return helper()
