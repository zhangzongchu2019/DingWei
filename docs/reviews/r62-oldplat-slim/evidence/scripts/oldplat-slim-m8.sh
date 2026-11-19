#!/bin/bash
cd /tmp/wt-r62-mobile || exit 9
E=/tmp/oldplat-slim-evidence
python3 /tmp/followup-evidence/conn_probe.py > "$E/conn-m8-before.json" 2>&1
date -u +%FT%TZ > "$E/m8-start"
go test ./internal/m8/ -count=1 -v > "$E/G1-m8.log" 2>&1
echo $? > "$E/G1-m8.rc"
date -u +%FT%TZ > "$E/m8-end"
python3 /tmp/followup-evidence/conn_probe.py > "$E/conn-m8-after.json" 2>&1
echo DONE > "$E/m8-done"
