#!/bin/bash
E=/tmp/oldplat-slim-evidence/z3
mkdir -p "$E"
for anchor in current base; do
  if [ "$anchor" = current ]; then W=/tmp/wt-r62-mobile; else W=/tmp/oldplat-base-7d3035c6; fi
  cd "$W" || exit 9
  for i in 1 2 3 4 5 6 7 8 9 10; do
    go test ./internal/m8/ -run 'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent|TestViewMentionRoute' -count=1 > "$E/$anchor-$i.log" 2>&1
    echo $? > "$E/$anchor-$i.rc"
  done
done
echo DONE > "$E/done"
