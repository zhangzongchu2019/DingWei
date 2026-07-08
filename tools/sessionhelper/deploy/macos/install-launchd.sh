#!/usr/bin/env bash
# macOS 下用 launchd 守护一个 sessionHelper 会话（替代 Linux 的 cron+guard）。
# 用法: ./install-launchd.sh <config文件绝对路径> [run.sh绝对路径]
# launchd KeepAlive 会在 sessionHelper 退出时自动拉起（自愈）；卸载见文末。
set -euo pipefail

CONFIG="${1:?用法: install-launchd.sh <config绝对路径> [run.sh绝对路径]}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RUNSH="${2:-$HERE/../../run.sh}"
CONFIG="$(cd "$(dirname "$CONFIG")" && pwd)/$(basename "$CONFIG")"
RUNSH="$(cd "$(dirname "$RUNSH")" && pwd)/$(basename "$RUNSH")"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "本脚本仅用于 macOS。Linux 请用 cron+guard.sh。" >&2
  exit 1
fi

SESSION="$(grep -E '^SH_SESSION_NAME=' "$CONFIG" | head -1 | cut -d= -f2 | tr -d ' ')"
[[ -n "$SESSION" ]] || { echo "config 里没有 SH_SESSION_NAME" >&2; exit 1; }

AGENTS="$HOME/Library/LaunchAgents"
LOGDIR="$HOME/.dingwei/logs"
mkdir -p "$AGENTS" "$LOGDIR"
PLIST="$AGENTS/com.dingwei.sessionhelper.$SESSION.plist"
LOG="$LOGDIR/$SESSION.log"

sed -e "s|__SESSION__|$SESSION|g" \
    -e "s|__CONFIG__|$CONFIG|g" \
    -e "s|__RUNSH__|$RUNSH|g" \
    -e "s|__LOG__|$LOG|g" \
    "$HERE/com.dingwei.sessionhelper.template.plist" > "$PLIST"

launchctl unload "$PLIST" 2>/dev/null || true
launchctl load "$PLIST"
echo "已安装并启动: $PLIST"
echo "查看: launchctl list | grep dingwei ; 日志: $LOG"
echo "停止/卸载: launchctl unload \"$PLIST\" && rm \"$PLIST\""
