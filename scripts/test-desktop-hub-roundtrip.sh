#!/bin/sh
# Isolated cross-runtime gate: real localhost Hub/enrollment, temporary DB/HOME, no AI.
set -eu
task_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$task_root"
task_python="${DW_DESKTOP_INTEGRATION_PYTHON:-$(command -v python3)}"
task_python="$("$task_python" -c 'import sys;print(sys.executable)')"
"$task_python" -c 'import websockets, pexpect; import sys; assert sys.version_info >= (3,11)'
DW_DESKTOP_INTEGRATION_PYTHON="$task_python" go test -p 1 ./internal/m8 \
  -run '^(TestHostedBodySendAcrossRealHub|TestDesktopMCPRoundTripAcrossRealHub|TestDeviceClaimAcrossActualClientAndHub|TestDeviceClaimAcrossActualClientAndPostgres)$' -count=1 -timeout=180s -v
