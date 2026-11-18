#!/bin/bash
while [ ! -f /tmp/r62m-chain3.done ]; do sleep 30; done
bash /tmp/r62m-g3-followup2.sh
echo done > /tmp/r62m-chain4.done
