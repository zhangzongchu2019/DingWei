#!/bin/bash
count=$1; seconds=$2
for i in $(seq 1 "$count"); do node -e "const end=Date.now()+${seconds}*1000; while(Date.now()<end){}" & done
wait
