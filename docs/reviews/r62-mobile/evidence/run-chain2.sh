#!/bin/bash
while [ ! -f /tmp/r62m-formal/ALL.done ]; do sleep 30; done
bash /tmp/r62m-formal2-run.sh
bash /tmp/r62m-z3-run.sh
echo done > /tmp/r62m-chain2.done
