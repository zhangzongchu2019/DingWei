#!/bin/bash
E=/tmp/mutant-revive-evidence
export NODE_PATH=/home/is3/pwrun/node_modules
date -u +%FT%TZ > $E/ML.start
python3 /tmp/followup-evidence/conn_probe.py > $E/ML-conn-before.json
date -u +%FT%TZ > $E/ML-load.start
for i in $(seq 1 192); do
  setsid nohup nice -n 19 bash -c 'DWLOAD2XM=1; end=$((SECONDS+5400)); while [ $SECONDS -lt $end ]; do :; done' >/dev/null 2>&1 &
done
sleep 3
pgrep -fc '[D]WLOAD2XM' > $E/ML-load.count
cd $E
setsid nohup python3 monitor_conn_ml.py </dev/null > $E/ML-monitor-driver.log 2>&1 &
( cd /tmp/wt-mutant-revive
  for i in 1 2 3; do
    [ -f $E/ML-STOPPED ] && break
    node view-service/console-browser-regression.js > $E/ML-console-$i.log 2>&1
    echo $? > $E/ML-console-$i.rc
  done ) &
L1=$!
( cd /tmp/wt-mutant-revive
  if [ ! -f $E/ML-STOPPED ]; then
    python3 tools/run-browser-regressions.py --receipt $E/ML-full.receipt > $E/ML-full.log 2>&1
    echo $? > $E/ML-full.rc
  fi ) &
L2=$!
wait $L1 $L2
pkill -f '[D]WLOAD2XM'
sleep 2
pgrep -fc '[D]WLOAD2XM' > $E/ML-load.killed-count
date -u +%FT%TZ > $E/ML-load.end
date -u +%FT%TZ > $E/ML.end
if [ -f $E/ML-STOPPED ]; then echo stopped > $E/ML.stopped-marker; else echo done > $E/ML.done; fi
