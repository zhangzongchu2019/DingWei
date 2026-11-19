#!/bin/bash
cd /tmp/wt-r62-mobile || exit 9
E=/tmp/oldplat-slim-evidence
export NODE_PATH=/home/is3/pwrun/node_modules PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright
python3 /tmp/followup-evidence/conn_probe.py > "$E/conn-browser-before.json" 2>&1
date -u +%FT%TZ > "$E/browser-start"
python3 tools/run-browser-regressions.py --receipt "$E/G2-browser.receipt" > "$E/G2-browser.log" 2>&1
echo $? > "$E/G2-browser.rc"
date -u +%FT%TZ > "$E/browser-end"
python3 /tmp/followup-evidence/conn_probe.py > "$E/conn-browser-after.json" 2>&1
echo DONE > "$E/browser-done"
