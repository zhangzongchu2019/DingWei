# -*- mode: python ; coding: utf-8 -*-

from pathlib import Path


root = Path(SPECPATH).parents[1]
src = root / "tools" / "sessionhelper"

a = Analysis(
    [str(src / "sessionhelper.py")],
    pathex=[str(src)],
    binaries=[],
    datas=[],
    hiddenimports=[
        "sessionhelper.app",
        "sessionhelper.launch",
        "sessionhelper.setup",
        "sessionhelper.desktop",
        "sessionhelper.desktop_host",
        "sessionhelper.desktop_endpoint",
        "sessionhelper.desktop_pilot",
        "sessionhelper.claim_enroll",
        "sessionhelper.mcp_shim",
        "sessionhelper.cli",
        "sessionhelper.config",
        "sessionhelper.contract",
        "sessionhelper.driver_adapter",
        "sessionhelper.claudeDriver",
        "sessionhelper.codexDriver",
        "sessionhelper.openCodeDriver",
        "sessionhelper.sidecar",
        "sessionhelper.unified_queue",
        "sessionhelper.llm",
        "sessionhelper.protocol",
        # 🔴 显式登记(组长裁定 2026-09-29):hub_meta.py 是 S-HELPER-NOTICE-NO-INJECT 的生成件,
        #    由 sessionhelper/app.py 静态 import。打包后若漏收,症状是运行时 ModuleNotFoundError
        #    —— 那个发现成本远高于现在这一行。
        # 🔵 本列表其余仍靠静态 import 侥幸成立的项(inbox / provision / reply_origin 等)
        #    **本次不动**:没量过就不改(组长明令,另一件事)。
        "sessionhelper.hub_meta",
    ],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[],
    noarchive=False,
)
pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name="sessionhelper",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    distpath=str(root / "dist"),
)
