"""Read only desktop app bundle metadata; never inspect account/session state.

Bundle presence is not signer verification, login proof, or native notification
support. Codex.app and the current ChatGPT.app host are reported separately.
"""
from __future__ import annotations

import json
from pathlib import Path
import platform
import plistlib
import re
import sys


APP_NAMES = ("Codex.app", "ChatGPT.app")


def _safe(value, label):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+ -]{0,95}", value):
        raise ValueError("invalid desktop app " + label + " metadata")
    return value


def macos_app_metadata(path):
    path = Path(path).expanduser().resolve(strict=True)
    if path.name not in APP_NAMES or not path.is_dir():
        raise ValueError("select the installed Codex.app or ChatGPT.app bundle")
    info = path / "Contents" / "Info.plist"
    if info.is_symlink() or not info.resolve().is_relative_to(path) or not info.is_file() or info.stat().st_size > 1024 * 1024:
        raise ValueError("desktop app Info.plist is absent or oversized")
    try:
        with info.open("rb") as source:
            values = plistlib.load(source)
    except (OSError, ValueError, plistlib.InvalidFileException) as exc:
        raise ValueError("cannot read desktop app bundle metadata") from exc
    if not isinstance(values, dict):
        raise ValueError("invalid desktop app bundle metadata")
    return {"name": path.name, "path": str(path),
            "version": _safe(values.get("CFBundleShortVersionString"), "version"),
            "build": _safe(values.get("CFBundleVersion"), "build"),
            "bundle_id": _safe(values.get("CFBundleIdentifier"), "bundle ID"),
            "signature_verified": False, "account_state": "NOT_READ"}


def desktop_probe(*, system=None, roots=None, selected=None):
    system = system or platform.system()
    result = {"platform": system, "architecture": platform.machine(),
              "python_version": platform.python_version(), "applications": [],
              "account_state": "NOT_READ", "native_auto_notify": "NOT_VERIFIED",
              "native_auto_notify_implemented": False}
    if system != "Darwin":
        result["status"] = "MACOS_PROBE_NOT_APPLICABLE"
        return result
    if selected:
        result["applications"] = [macos_app_metadata(selected)]
    else:
        roots = roots if roots is not None else (Path("/Applications"), Path.home() / "Applications")
        seen = set()
        for root in roots:
            for name in APP_NAMES:
                path = Path(root) / name
                if path.is_dir():
                    metadata = macos_app_metadata(path)
                    if metadata["path"] not in seen:
                        seen.add(metadata["path"])
                        result["applications"].append(metadata)
    result["status"] = ("DESKTOP_APP_METADATA_FOUND" if len(result["applications"]) == 1
                        else "CHOOSE_DESKTOP_APP" if result["applications"] else "DESKTOP_APP_NOT_FOUND")
    return result


def require_macos_desktop(*, selected=None, system=None):
    result = desktop_probe(system=system, selected=selected)
    if result["platform"] == "Darwin":
        if not result["applications"]:
            raise ValueError("请先安装 Codex 桌面应用：https://learn.chatgpt.com/docs/app")
        if len(result["applications"]) != 1:
            raise ValueError("检测到多个桌面应用，请用 --desktop-app 指定本次使用的 .app；不自动选择或启动其他会话。")
    return result


def main(argv=None):
    import argparse
    parser = argparse.ArgumentParser(description="只读桌面应用版本；不读登录、会话或凭据")
    parser.add_argument("--desktop-app", type=Path)
    parser.add_argument("--require-app", action="store_true", help="原生构建前要求 macOS 且唯一明确选择的桌面应用")
    args = parser.parse_args(argv)
    try:
        result = desktop_probe(selected=args.desktop_app)
        if args.require_app and (result["platform"] != "Darwin" or len(result["applications"]) != 1):
            raise ValueError("native build requires macOS and one selected installed desktop application")
    except (OSError, ValueError):
        raise SystemExit("桌面应用预检未通过：请在 Mac 上指定已安装的 Codex.app/ChatGPT.app；不读取登录或会话。") from None
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
