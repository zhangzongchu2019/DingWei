#!/usr/bin/env python3
import os, glob, socket, time, json, subprocess

SOCK = '/run/sessionhelper/dingwei-mcp.sock'

def helper_pid():
    out = subprocess.run(['pgrep', '-f', 'python -m sessionhelper'],
                         capture_output=True, text=True).stdout.split()
    return int(out[0]) if out else None

def hub_conns(pid):
    if pid is None:
        return []
    socks = set()
    for fd in glob.glob(f'/proc/{pid}/fd/*'):
        try:
            target = os.readlink(fd)
        except OSError:
            continue
        if target.startswith('socket:['):
            socks.add(target[8:-1])
    rows = []
    for path in ('/proc/net/tcp', '/proc/net/tcp6'):
        try:
            handle = open(path)
        except OSError:
            continue
        with handle:
            next(handle)
            for line in handle:
                parts = line.split()
                if parts[3] == '01' and parts[9] in socks:
                    rows.append({'local': parts[1], 'remote': parts[2], 'inode': parts[9]})
    return rows

def snapshot():
    pid = helper_pid()
    stat = os.stat(SOCK)
    probe = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    probe.settimeout(3)
    rc = probe.connect_ex(SOCK)
    probe.close()
    unix_cnt = sum(1 for line in open('/proc/net/unix') if 'dingwei-mcp.sock' in line)
    return {'utc': time.strftime('%FT%TZ', time.gmtime()), 'helper_pid': pid,
            'established': hub_conns(pid), 'ipc_connect_ex': rc,
            'sock_fs_inode': stat.st_ino, 'sock_mtime': stat.st_mtime,
            'unix_path_entries': unix_cnt}

if __name__ == '__main__':
    print(json.dumps(snapshot(), ensure_ascii=False))
