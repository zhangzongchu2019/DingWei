#!/bin/bash
cd /tmp/wt-r62-cli-view-b/view-service || exit 9
export NODE_PATH=/home/is3/pwrun/node_modules PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright
K=/tmp/cli-view-b-v5
for m in cli-view-offstate-add cli-view-toggle-wire cli-view-persist cli-view-trust-bypass cli-view-fail-newline cli-view-longtext-full cli-view-nowrap cli-view-dom-depend cli-view-override-flip cli-view-threshold cli-view-tool-drop cli-view-toggle-residue cli-view-attach-drop; do
  MUTATION=$m node renderer-cli-view-regression.js > "$K/knife-$m.log" 2>&1
  rc=$?
  first=$(grep -m1 -E "  FAIL |AssertionError" "$K/knife-$m.log" | head -c 400)
  printf '%s rc=%s | %s\n' "$m" "$rc" "$first" >> "$K/summary.txt"
done
date -u +%FT%TZ > "$K/knives-end"
for i in 17 18; do
  CLI_VIEW_SHOT_DIR="$K/shots" node renderer-cli-view-regression.js > "$K/run$i.log" 2>&1
  printf 'run%s rc=%s | %s\n' "$i" "$?" "$(tail -1 "$K/run$i.log")" >> "$K/summary.txt"
done
date -u +%FT%TZ > "$K/greens-end"
cd /tmp/wt-r62-cli-view-b || exit 9
python3 tools/run-browser-regressions.py --receipt "$K/fullrunner-receipt.json" > "$K/fullrunner.log" 2>&1
printf 'fullrunner rc=%s\n' "$?" >> "$K/summary.txt"
python3 /tmp/followup-evidence/conn_probe.py > "$K/conn-after.json" 2>&1
date -u +%FT%TZ > "$K/end"
echo ALL_DONE >> "$K/summary.txt"
