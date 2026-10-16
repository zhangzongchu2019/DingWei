#!/usr/bin/env python3
"""Host-root read-only helper inventory. Prints one sanitized JSON line.

No helper imports, secret/env reads, state writes, restart, or network probes.
Only a fixed isolated Python version probe is executed inside running containers.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import shlex
import subprocess

MOUNT = '/opt/dingwei/sessionhelper-src'
PYTHON = '/opt/dingwei/sessionhelper-venv/bin/python'
VERSION_PROBE = 'import json,sys;print(json.dumps(list(sys.version_info[:3])))'
# Select before emitting from Docker: never request Config.Env or full inspect.
INSPECT_FORMAT = '''{"id":{{json .Id}},"name":{{json .Name}},"running":{{json .State.Running}},"mounts":{{json .Mounts}},"entrypoint":{{json .Config.Entrypoint}},"cmd":{{json .Config.Cmd}},"restart_policy":{{json .HostConfig.RestartPolicy.Name}},"pythonpath_entries":[""{{range .Config.Env}}{{if eq (index (split . "=") 0) "PYTHONPATH"}},{{json .}}{{end}}{{end}}]}'''
CONNECTION = re.compile(r'\[sessionHelper\] connected self=\S+ version=(\d+(?:\.\d+){2,3})(?:\s|$)')


class InventoryError(RuntimeError):
    pass


def run(argv):
    try:
        result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
    except (OSError, subprocess.TimeoutExpired):
        raise InventoryError('docker_unavailable_or_timeout') from None
    if result.returncode:
        raise InventoryError('docker_command_failed')
    return result.stdout.decode('utf-8', errors='replace'), result.stderr.decode('utf-8', errors='replace')


def entry_details(inspection):
    """Emit only a canonical recognized entry; never echo arbitrary shell/env."""
    argv = (inspection.get('entrypoint') or []) + (inspection.get('cmd') or [])
    if not all(isinstance(x, str) for x in argv):
        return {'supported': False, 'shape': 'unrecognized_redacted'}
    if not argv or argv[0] not in ('tini', '/usr/bin/tini', '/sbin/tini', '/tini'):
        return {'supported': False, 'shape': 'unrecognized_redacted'}
    tail = argv[1:]
    while tail and tail[0] in ('-g', '--'):
        tail = tail[1:]
    if len(tail) != 3 or tail[0] not in ('bash', '/bin/bash', '/usr/bin/bash') or tail[1] not in ('-c', '-lc'):
        return {'supported': False, 'shape': 'unrecognized_redacted'}
    try:
        lex = shlex.shlex(tail[2], posix=True, punctuation_chars=';&|')
        lex.whitespace_split = True
        commands, current = [], []
        for token in lex:
            if token in (';', '&&'):
                commands.append(current)
                current = []
            else:
                current.append(token)
        commands.append(current)
    except ValueError:
        return {'supported': False, 'shape': 'unrecognized_redacted'}
    sourced, exported = False, None
    for command in commands[:-1]:
        if command in (['source', '/run/secrets/sessionhelper_env'], ['.', '/run/secrets/sessionhelper_env']):
            sourced = True
        elif command in (['set', '-a'], ['set', '+a']):
            pass
        elif command == ['export', 'PYTHONPATH=' + MOUNT]:
            exported = MOUNT
        else:
            return {'supported': False, 'shape': 'unrecognized_redacted'}
    final = commands[-1]
    if final and final[0] == 'exec':
        final = final[1:]
    if not sourced or final != [PYTHON, '-m', 'sessionhelper']:
        return {'supported': False, 'shape': 'unrecognized_redacted'}
    return {'supported': True, 'shape': 'tini -> bash -> source secret file -> venv python -m sessionhelper',
            'tini': argv[0], 'shell': tail[:2], 'commands': commands,
            'python': PYTHON, 'exported_pythonpath': exported}


def source_files(source):
    """List names only. Never follow symlink directories or read file contents."""
    root = Path(source)
    if not root.is_absolute() or root.is_symlink() or not root.is_dir():
        return {'error': 'source_directory_unavailable', 'non_python_files': [], 'symlinks': []}
    files, links, errors = [], [], []
    def walk_error(_):
        errors.append('source_scan_incomplete')
    for parent, dirs, names in os.walk(root, followlinks=False, onerror=walk_error):
        for name in dirs[:]:
            p = Path(parent) / name
            if p.is_symlink():
                links.append(str(p.relative_to(root)))
                dirs.remove(name)
        for name in names:
            p = Path(parent) / name
            relative = p.relative_to(root)
            if p.is_symlink():
                links.append(str(relative))
            elif p.suffix not in ('.py', '.pyc'):
                files.append({'path': str(relative),
                              'blocks_install': relative.parts[0] in ('sessionhelper', 'factory')})
    return {'error': errors[0] if errors else None,
            'non_python_files': sorted(files, key=lambda x: x['path']), 'symlinks': sorted(links)}


def latest_connection(stdout, stderr):
    matches = []
    for line in (stdout + '\n' + stderr).splitlines():
        version = CONNECTION.search(line)
        timestamp = re.match(r'^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{9}Z) ', line)
        if version and timestamp:
            matches.append((timestamp[1], version[1]))
    return max(matches) if matches else (None, None)


def inventory(runner=run, containers=(), log_lines=500):
    stdout, _ = runner(['docker', 'ps', '-a', '-q'])
    ids = stdout.split()
    if not all(re.fullmatch(r'[0-9a-f]{12,64}', value) for value in ids):
        raise InventoryError('invalid_container_listing')
    result = []
    selected_names = set(containers)
    for container_id in ids:
        out, _ = runner(['docker', 'inspect', '--format', INSPECT_FORMAT, container_id])
        item = json.loads(out)
        name = item['name'].removeprefix('/')
        if selected_names and name not in selected_names:
            continue
        mounts = item.get('mounts') or []
        matching = [m for m in mounts if m.get('Destination') == MOUNT]
        argv = (item.get('entrypoint') or []) + (item.get('cmd') or [])
        if not matching and not any('sessionhelper' in str(x) for x in argv):
            continue
        stops = []
        entry = entry_details(item)
        if not entry['supported']:
            stops.append('entry_unsupported')
        pythonpath_entries = [x[len('PYTHONPATH='):] for x in item['pythonpath_entries']
                              if x.startswith('PYTHONPATH=')]
        effective = entry.get('exported_pythonpath') or (pythonpath_entries[0] if len(pythonpath_entries) == 1 else None)
        # Only the supported literal is safe/useful to emit. Never echo odd values.
        if effective != MOUNT:
            stops.append('pythonpath_unverified')
        mount = matching[0] if len(matching) == 1 else None
        if not mount or mount.get('Type') != 'bind' or mount.get('RW') is not False:
            stops.append('readonly_bind_required')
        if item['restart_policy'] != 'unless-stopped':
            stops.append('restart_policy_unsupported')
        scan = source_files(mount['Source']) if mount else {'error': 'source_mount_missing'}
        if scan.get('error') or scan.get('symlinks'):
            stops.append('source_scan_requires_review')
        if any(x['blocks_install'] for x in scan.get('non_python_files', [])):
            stops.append('non_python_source_files_require_retention')
        version, connection_utc, python_version = None, None, None
        if item['running']:
            try:
                if entry['supported']:
                    out, _ = runner(['docker', 'exec', container_id, PYTHON, '-I', '-S', '-B', '-c', VERSION_PROBE])
                    value = json.loads(out)
                    if not isinstance(value, list) or len(value) != 3 or any(type(x) is not int for x in value):
                        raise ValueError()
                    python_version = '.'.join(map(str, value))
                    if value[:2] != [3, 12]:
                        stops.append('python_3_12_required')
                out, err = runner(['docker', 'logs', '--timestamps', '--tail', str(log_lines), container_id])
                connection_utc, version = latest_connection(out, err)
            except (InventoryError, ValueError, TypeError):
                stops.append('runtime_probe_unavailable')
        else:
            stops.append('container_not_running')
        if version is None:
            stops.append('passive_connection_version_unknown')
        record = {'container': name, 'container_id': item['id'], 'running': item['running'],
                  'source_mount': ({'source': mount['Source'], 'destination': mount['Destination'],
                                    'readonly': mount['RW'] is False, 'type': mount['Type']} if mount else None),
                  'entry': entry, 'pythonpath': MOUNT if effective == MOUNT else 'UNVERIFIED_REDACTED',
                  'pythonpath_scope': 'recognized_entry_and_container_config; secret file not read',
                  'venv': str(Path(PYTHON).parent.parent) if entry['supported'] else None,
                  'python_version': python_version, 'restart_policy': item['restart_policy'],
                  'source_scan': scan, 'passive_connection_version': version,
                  'passive_connection_utc': connection_utc,
                  'log_scope': 'last_' + str(log_lines) + '_lines; no match means UNKNOWN',
                  'stop_reasons': sorted(set(stops))}
        result.append(record)
    missing = sorted(selected_names - {x['container'] for x in result})
    return {'format': 1, 'scope': 'this Docker host only; non-container agents not inventoried',
            'containers': result, 'missing_requested_containers': missing,
            'no_changes': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--container', action='append', default=[])
    parser.add_argument('--log-lines', type=int, choices=range(100, 2001), default=500, metavar='100..2000')
    args = parser.parse_args()
    if os.geteuid() != 0:
        print(json.dumps({'error': 'host_root_required'}))
        return 1
    try:
        result = inventory(containers=args.container, log_lines=args.log_lines)
    except (InventoryError, ValueError, KeyError, TypeError):
        print(json.dumps({'error': 'inventory_incomplete; no raw Docker output emitted'}))
        return 1
    print(json.dumps(result, ensure_ascii=True, sort_keys=True))
    return int(bool(result['missing_requested_containers']) or not result['containers'] or
               any(x['stop_reasons'] for x in result['containers']))


if __name__ == '__main__':
    raise SystemExit(main())
