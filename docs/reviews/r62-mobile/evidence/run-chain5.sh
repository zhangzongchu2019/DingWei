#!/bin/bash
while [ ! -f /tmp/r62m-chain4.done ]; do sleep 20; done
bash /tmp/r62m-z3-focused2.sh
echo done > /tmp/r62m-chain5.done
