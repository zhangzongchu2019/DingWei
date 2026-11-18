#!/bin/bash
E=/tmp/r62-evidence
export NODE_PATH=/home/is3/pwrun/node_modules
export PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright
date -u +%FT%TZ > $E/R15.start
for anchor in r62 main; do
  if [ "$anchor" = r62 ]; then W=/tmp/wt-r62-slim; else W=/tmp/wt-cli-survey; fi
  cd "$W" || exit 9
  git rev-parse HEAD > $E/R15-$anchor.anchor
  for i in 1 2 3 4 5; do
    node view-service/r15-session-history-browser-regression.js > $E/R15-$anchor-pg-$i.log 2>&1
    echo $? > $E/R15-$anchor-pg-$i.rc
  done
  node view-service/r15-session-history-mutations.js > $E/R15-$anchor-mut-1.log 2>&1
  echo $? > $E/R15-$anchor-mut-1.rc
done
date -u +%FT%TZ > $E/R15.end
echo done > $E/R15.done
