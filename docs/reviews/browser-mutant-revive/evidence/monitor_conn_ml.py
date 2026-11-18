#!/usr/bin/env python3
import json, time, subprocess, os, sys
sys.path.insert(0, '/tmp/followup-evidence')
import conn_probe as probe

EVID = '/tmp/mutant-revive-evidence'
MON = EVID + '/ML-conn-monitor.log'
EVT = EVID + '/ML-reconnect-events.log'

def log(path, obj):
    with open(path, 'a') as handle:
        handle.write(json.dumps(obj, ensure_ascii=False) + '\n')

def stop_segment(reason):
    with open(EVID + '/ML-STOPPED', 'w') as handle:
        handle.write(reason + '\n')
    subprocess.run(['pkill', '-f', '[D]WLOAD2XM'])
    subprocess.run(['pkill', '-f', 'view-service/.*regression'])
    subprocess.run(['pkill', '-f', 'run-browser-regressions.py'])

def main():
    start = probe.snapshot()
    with open(EVID + '/ML-conn-monitor-start.json', 'w') as handle:
        json.dump(start, handle, ensure_ascii=False, indent=1)
    last_pid = start['helper_pid']
    last_seen = {conn['inode'] for conn in start['established']}
    reconnects = 0
    log(MON, {'event': 'monitor_start', **start})
    while not (os.path.exists(EVID + '/ML.done') or os.path.exists(EVID + '/ML-STOPPED')):
        time.sleep(10)
        snap = probe.snapshot()
        log(MON, snap)
        pid = snap['helper_pid']
        ids = {conn['inode'] for conn in snap['established']}
        if pid != last_pid:
            reconnects += 1
            log(EVT, {'event': 'helper_process_restart', 'reconnects': reconnects, **snap})
            stop_segment('helper process restart during ML')
            break
        if ids:
            if last_seen and ids != last_seen:
                reconnects += 1
                log(EVT, {'event': 'hub_tcp_identity_change', 'reconnects': reconnects,
                          'prev': sorted(last_seen), 'now': sorted(ids), **snap})
                if reconnects > 2:
                    stop_segment(f'helper reconnects {reconnects} > 2 during ML')
                    break
            last_seen = ids
        elif last_seen:
            log(EVT, {'event': 'hub_tcp_gap', 'prev': sorted(last_seen), **snap})
        if snap['ipc_connect_ex'] != 0:
            log(EVT, {'event': 'ipc_down', 'rc': snap['ipc_connect_ex'], **snap})
    after = probe.snapshot()
    with open(EVID + '/ML-conn-after.json', 'w') as handle:
        json.dump(after, handle, ensure_ascii=False, indent=1)
    log(MON, {'event': 'monitor_end', 'reconnects': reconnects, **after})

if __name__ == '__main__':
    main()
