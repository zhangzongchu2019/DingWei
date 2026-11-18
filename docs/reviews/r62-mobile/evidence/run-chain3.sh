#!/bin/bash
while [ ! -f /tmp/r62m-chain2.done ]; do sleep 30; done
bash /tmp/r62m-r15-followup.sh
echo done > /tmp/r62m-chain3.done
