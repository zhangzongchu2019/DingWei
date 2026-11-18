#!/bin/bash
export NODE_PATH=/home/is3/pwrun/node_modules
E=/tmp/mutant-revive-evidence
date -u +%FT%TZ > $E/G.start
cd /tmp/wt-mutant-revive
for i in 1 2 3; do
  node view-service/console-browser-regression.js > $E/G-console-$i.log 2>&1
  echo $? > $E/G-console-$i.rc
done
node view-service/console-browser-regression.js --mutate=unhandled-group-action > $E/G-knife.log 2>&1
echo $? > $E/G-knife.rc
node view-service/console-instrumented-mutant-diag.js --mutate=unhandled-group-action > $E/G-knife-instrumented.log 2>&1
echo $? > $E/G-knife-instrumented.rc
cd /tmp/wt-followup
node view-service/console-browser-regression.js --mutate=unhandled-group-action > $E/G-anchor-oldknife.log 2>&1
echo $? > $E/G-anchor-oldknife.rc
cd /tmp/wt-mutant-revive
python3 tools/run-browser-regressions.py --receipt $E/G-full.receipt > $E/G-full.log 2>&1
echo $? > $E/G-full.rc
date -u +%FT%TZ > $E/G.end
echo done > $E/G.done
