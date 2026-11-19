#!/bin/bash
cd /tmp/wt-r62-cli-view-b/view-service || exit 9
export NODE_PATH=/home/is3/pwrun/node_modules PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright
K=/tmp/cli-view-b-v5
python3 /tmp/followup-evidence/conn_probe.py > "$K/conn-before.json" 2>&1
date -u +%FT%TZ > "$K/start"
CLI_VIEW_SHOT_DIR="$K/shots" node renderer-cli-view-regression.js > "$K/run16.log" 2>&1
echo $? > "$K/run16.rc"
date -u +%FT%TZ > "$K/baseline-end"
echo BASELINE_DONE >> "$K/summary.txt"
