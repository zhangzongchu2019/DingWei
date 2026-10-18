# Node archive extraction on the locked Python base

The r29 build failed because GNU tar's `-J` option invokes an external `xz`
program absent from the final Python image. The replacement uses Python 3.12
`tarfile` in `r:xz` mode (stdlib `lzma` / `_lzma`) and explicitly uses
`extractall(filter='data')`. No system package, base digest or dependency lock
changes. Legitimate relative npm symlinks inside the extraction destination
remain supported; parent traversal and links outside that destination fail the
build. A rejected archive can leave partial files in the failed builder layer;
no completed image is produced by that RUN.

`prepare_context.py` already reads Node input without following aliases and
verifies its basename and SHA256 against the selected Git lock before emitting
the context (lines 275–281 on r29). The Dockerfile repeats the same immutable
Node SHA256 immediately before extraction. It hashes the bytes once, then opens
those exact bytes through BytesIO; it does not reread an unverified archive.
The fixture binds the repeated digest to `claude-package-lock.json`. The digest
is not a second configurable source of trust. Changing the Node lock requires
updating this binding and rerunning its test.

## Every RUN executable in the final definition

| RUN | Invocations | Static/available evidence | Target status |
| --- | --- | --- | --- |
| builder venv | Python, venv Python, `python -m pip` | Same locked base as r29; prodops reports the build passed this RUN | Reported successful; no independent target execution by coder |
| final deb install | `dpkg`, `dpkg-query`, shell builtin `test`, `rm` | r29 build reached the later Node RUN, so these succeeded in that build | Reported successful |
| Node unpack | `mkdir`, Python with `hashlib/io/tarfile/lzma/_lzma`, `mv`, `rm` | Old RUN reached tar after mkdir; local Python 3.12 executes exact new RUN with restricted PATH and no xz | Base Python/lzma and final mv require target read-only confirmation; local fixture is not image evidence |
| final entry setup | shell builtin `test`, `chmod` | Shell test previously succeeded; later chmod not reached by failed Claude build | chmod requires target read-only confirmation |

All RUN instructions are shell form and also depend on the base `/bin/sh` and
its PATH. `test` is a shell builtin; it need not be a standalone executable.
Python is inherited from the base PATH, not assumed to live in `/usr/bin`.
`mkdir`, `mv`, `rm`, `chmod`, `dpkg`, and `dpkg-query` must resolve in that PATH.
No `tar`, `xz`, package manager download or network command remains in Node RUN.

Prodops was asked to record command resolution (or equivalent `/bin`, `/usr/bin`
plus the inherited Python location) and Python 3.12 `tarfile/lzma/_lzma` import
using the exact base digest on the approved isolated Engine, network=none,
read-only with no private mounts or installs. Coder has not run that inspection;
unconfirmed rows remain explicit target gates, not assumed capabilities.

Tests execute the actual Dockerfile RUN against a tiny synthetic tar.xz with
only Python/tar/mkdir/mv/rm in PATH (no xz). They preserve relative bin/npm,
reject traversal and escaping symlinks, and reject a wrong hash before creating
extracted members. No real Node/Claude CLI, Docker daemon or image is executed.
