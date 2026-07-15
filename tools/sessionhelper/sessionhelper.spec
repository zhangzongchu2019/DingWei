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
