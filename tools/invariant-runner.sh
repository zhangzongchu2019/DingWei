#!/usr/bin/env bash
# 巡检跑手:定时执行 invariant-check.py;**只在不成立时出声**。
# 🔴 设计取向(业主 2026-09-16 定的形状):成功无声,失败才显现。
#    但"失败才显现"的前提是**失败显现得出来** —— 故:
#      · 失败 ⇒ 发消息给组长(经既有 send.py 通路)
#      · 🔴 **发消息本身失败也要留痕** —— 否则就成了"失败没有声音"的又一例
#      · 成功 ⇒ 只更新心跳时间戳,不发任何消息
set -uo pipefail

CHECK="${DW_CHECK:-/opt/dingwei/tools/invariant-check.py}"
CONF="${DW_CONF:-/opt/dingwei/deploy/invariants-dingwei-gray.json}"
STATE="${DW_STATE:-/var/lib/dingwei/invariant-state.json}"
LOG="${DW_LOG:-/var/log/dingwei/invariant.log}"
SEND="${DW_SEND:-}"                 # 为空则只写日志,不发消息
TO="${DW_NOTIFY_TO:-zzc-opus50ts-2642}"
MAXAGE="${DW_MAX_AGE:-5400}"        # 心跳超过 90 分钟未成功即告警
INTERVAL="${DW_INTERVAL:-1800}"     # 30 分钟一轮

mkdir -p "$(dirname "$STATE")" "$(dirname "$LOG")" 2>/dev/null || true

run_once() {
  local out rc ts
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  out="$(python3 "$CHECK" --config "$CONF" --state "$STATE" --max-age "$MAXAGE" 2>&1)"
  rc=$?
  # 🔴 无论成败都写日志:日志是"它还活着"的证据,不只是"出事了"的证据。
  printf '%s rc=%s\n%s\n' "$ts" "$rc" "$out" >> "$LOG"
  if [ "$rc" -ne 0 ]; then
    if [ -n "$SEND" ] && [ -x "$SEND" ] 2>/dev/null || [ -n "$SEND" ]; then
      local body; body="$(mktemp)"
      { echo "🔴 巡检发现 $( [ "$rc" = 3 ] && echo '【巡检器本身不可判定】' || echo '断言不成立' )（$ts, rc=$rc）"
        echo
        echo "$out"
        echo
        echo "判读:rc=2 有断言不成立或心跳过期;rc=3 巡检器自身不可判定(配置读不到/缺哨兵/哨兵失效)。"
        echo "🔴 rc=3 不是"没问题",是"查不了"。"
      } > "$body"
      if ! python3 "$SEND" "$TO" --file "$body" >>"$LOG" 2>&1; then
        # 🔴 告警发不出去,必须在日志里留下**显眼**的一行,否则这就是"失败没有声音"本身
        printf '%s 🔴🔴 ALERT_DELIVERY_FAILED —— 巡检告警未能送出,上面那条结论无人知晓\n' \
               "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$LOG"
      fi
      rm -f "$body"
    fi
  fi
  return "$rc"
}

case "${1:-loop}" in
  once) run_once ;;
  loop) while true; do run_once; sleep "$INTERVAL"; done ;;
  *) echo "用法: $0 [once|loop]" >&2; exit 2 ;;
esac
