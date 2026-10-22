"""Render one explicitly installed venv .pth; never install into a live venv.

The container command is unchanged. Python's site phase runs this before -m
prepends cwd. The hook re-execs only helper module entries, using the same venv.
"""
from __future__ import annotations

import argparse
import os
import textwrap


def render_hook(stable_root: str, interpreter: str) -> bytes:
    for value in (stable_root, interpreter):
        if (not isinstance(value, str) or not value.startswith('/')
                or os.path.normpath(value) != value or '\x00' in value
                or '\n' in value or '\r' in value):
            raise ValueError('cwd_hook_absolute_path_required')
    body = textwrap.dedent('''\
        def _dw_cwd_import_bootstrap():
            import sys
            if sys.flags.isolated or getattr(sys, '_dw_cwd_import_reexecuted', False):
                return
            args = list(getattr(sys, 'orig_argv', ()))[1:]
            # Only Python module entry switches, never script or -c payload text.
            options = []
            while args and args[0] in ('-B', '-u', '-E', '-s', '-P'):
                options.append(args.pop(0))
            if len(args) < 2 or args[0] != '-m':
                return
            module = args[1]
            if module != 'sessionhelper' and not module.startswith('sessionhelper.'):
                return
            import os
            try:
                if (sys.implementation.name != 'cpython' or sys.version_info < (3, 11)
                        or sys.executable != INTERPRETER
                        or sys.prefix != os.path.dirname(os.path.dirname(INTERPRETER))):
                    raise RuntimeError('interpreter_unverified')
                # cwd stays the business workdir. Only this immutable root is added.
                current = '/'
                for component in ROOT.split('/')[1:]:
                    current = os.path.join(current, component)
                    info = os.lstat(current)
                    sticky = info.st_uid == 0 and bool(info.st_mode & 0o1000)
                    if ((info.st_mode & 0o170000) != 0o040000
                            or info.st_uid not in (0, os.getuid())
                            or (info.st_mode & 0o022 and not sticky)):
                        raise RuntimeError('root_unverified')
                for relative in ('sessionhelper', 'sessionhelper/__init__.py', 'sessionhelper/__main__.py'):
                    info = os.lstat(os.path.join(ROOT, relative))
                    expected = 0o040000 if relative == 'sessionhelper' else 0o100000
                    if ((info.st_mode & 0o170000) != expected
                            or info.st_uid not in (0, os.getuid()) or info.st_mode & 0o022):
                        raise RuntimeError('package_unverified')
                sys._dw_cwd_import_reexecuted = True
                trampoline = ('import sys;sys.path.insert(0,sys.argv.pop(1));import os;'
                              'print("HELPER_CWD_IMPORT_READY isolated="+str(sys.flags.isolated)+" pid="+str(os.getpid()),file=sys.stderr);'
                              'import runpy;runpy.run_module(sys.argv.pop(1),run_name="__main__",alter_sys=True)')
                os.execve(INTERPRETER, [INTERPRETER, '-I', '-B', *options, '-c', trampoline,
                                       ROOT, module, *args[2:]], dict(os.environ))
                raise RuntimeError('exec_returned')
            except BaseException:
                # site.py otherwise prints .pth errors and continues unsafe startup.
                try:
                    os.write(2, b'HELPER_CWD_IMPORT_ERROR startup_refused\\n')
                finally:
                    os._exit(78)
        _dw_cwd_import_bootstrap()
    ''')
    body = 'ROOT = ' + repr(stable_root) + '\nINTERPRETER = ' + repr(interpreter) + '\n' + body
    compile(body, '<dingwei-cwd-import>', 'exec')
    # A .pth executable line must begin with import and occupy exactly one line.
    return ('import sys; exec(' + repr(body) + ", {'__name__': '_dw_cwd_import_hook'})\n").encode('utf-8')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stable-root', required=True)
    parser.add_argument('--interpreter', required=True)
    args = parser.parse_args()
    import sys
    sys.stdout.buffer.write(render_hook(args.stable_root, args.interpreter))


if __name__ == '__main__':
    main()
