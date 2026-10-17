# Claude packager R2 contract

This defines an executable, explicitly gated offline build entry. Developing or
testing it does not authorize package acquisition, actual Engine access, an image
build, reading a seat's private state, registry publication or combo activation.
The reviewed packager no longer has an unconditional post-prepare refusal.
`verified=false` stays unchanged. R3 owns the Claude launcher, credential,
enrollment/IPC/CLI ABI and staged-guard fixtures; entrypoint fixtures do not claim
real image/runtime acceptance.

## Git trust and composition

`context-map.json` is the reviewed, committed map. Its H3 source anchor is r25
`031c4284cab5c0401ae09d0180eeede1a32c7833`, tree
`73b73d9c5d78f33116d2d6023450955734aa810e`, runtime lock SHA256
`5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`.
The object must be available locally via Git; no source downloads or fallback to
an arbitrary manifest-declared commit occur.

All source inputs come from regular Git blobs. `--repo` and `--commit` identify
an exact, clean HEAD approved by the leader. The Claude Dockerfile, launchers,
`.dockerignore` and Claude package lock come from that commit, so an independently
reviewed R3 launcher merged into the final candidate is selected without changing
an embedded launcher hash. Do not silently change the H3 anchor to that candidate:
a change to common guard/helper or its package mapping requires a reviewed map
update. `CLAUDE_SOURCE_COMMIT` is mandatory in the wrapper; inputs and the new
output path must be absolute, with no redirected ancestor components.

The H3 input must use its **real** BUILD-INPUTS format: commit/tree/lock_sha256,
and files mapping relative names to SHA256. The root has `Dockerfile`, `start.py`,
`codex.py`, `dingwei-file`, `verify_runtime.py`, `.dockerignore`, helper/, wheels/,
runtime-debs/, security-debs/ and Codex native/ files. It does not have
`codex_start.py`, `claude-start.py` or `claude.py`.

Validation binds the header to the Git anchor, compares the complete regular file
inventory and every byte hash with the receipt, compares H3 source and the entire
new-layout helper export with immutable Git bytes, and checks wheel identities,
versions and SHA256 against the anchored runtime lock and both deb sets against
the anchored H3 deb locks. A self-edited receipt cannot authorize changed consumed
materials. The helper file-map reconstruction is checked by the real H3 producer
in synthetic fixtures; it does not execute helper, guard or any input Python.

Only common `start.py`, helper/, wheels/ and both deb trees are retained from H3.
The Codex Dockerfile/launcher/verification/upload scripts and **all Codex native
files are discarded**. The unused Codex native files are checked for normal file
type, canonical scope/count and agreement with the H3 receipt; this is not a
publisher signature for those discarded bytes. They cannot enter the output.
The Claude native package is independently verified by the Git-pinned SHA512/SHA1
lock, then safely extracted, replacing the entire native namespace.

The map explicitly places H3 `start.py` at output `start.py`; the Git Claude
Dockerfile installs it as `/opt/dingwei/codex_start.py` for the shared guard.
The Git `dockerignore` is installed as **`.dockerignore`**. No base Dockerfile or
invented common-guard filename is reused as a Claude definition.

## Filesystem and output

Every input path component is opened through dirfds with O_DIRECTORY|O_NOFOLLOW;
files use O_NOFOLLOW|O_NONBLOCK, fstat regular/unaliased checks, bounded reads and
before/after metadata checks. No symlinks (including directory/root/ancestor or
archive links), special nodes, hardlinks, extra empty directories, traversal,
normalized aliases or duplicate JSON/archive names are accepted. Expected H3
source/helper/wheel/deb modes are the H3 producer's 0644. Inputs are captured once;
output uses those same verified bytes, never a recursive directory copy or a
second unchecked read. Codex native bytes are stream-hashed and not retained.

Output must not exist or be inside the source/input tree. A new private 0700 root
is created through a checked parent dirfd. Only allowlisted files are written,
with relative no-follow directory opens and exclusive file creation. Existing
output data is preserved on refusal; on failure only this invocation's new root
is removed through the already-open parent. All descriptors close on errors.
The new receipt records source commit/tree, H3 anchor and receipt hash, map and
Claude-lock SHA256, and every output regular file's SHA256/mode. This describes a
context, not an image digest, proven venv, actual CLI behavior or runtime PASS.

## Local qualification and later gates

The fixture seeds a private **synthetic Git repository**, synthetic wheels/debs
and tar/Node bytes, then calls the actual H3 prepare_context producer. Only its
literal production lock SHA is replaced by the synthetic lock's SHA; the Git,
helper export, wheel/deb/tar and receipt validators are real. This proves the
actual H3 shape and composition contract without real package bodies. Negative
cases rehash receipts after altering guard/Dockerfile/helper/wheel/deb bytes,
and exercise missing/extra/type/link/mode/ancestor/JSON/tar/source/lock pollution.
Subprocess fixtures drive the real wrapper and real packager against a text-only
fake Docker executable. Valid, independently approved fixture Engine identity
permits the build/inspect sequence; polluted inputs stop before any fake Docker
call. This is not a real Engine test. Narrow source withdrawals must turn these
same assertions red.

Future package acquisition, target-specific verification, independent Engine
approval, image/venv/UID/CA/runtime/CLI/MCP tests and digest publication remain
separate owner-authorized work. None is authorized by this packager change.

## R2 composition follow-up: COPY and runtime permissions

The actual staged Dockerfile/ignore pair is tested together: the common source
is `start.py`, so `.dockerignore` includes `!start.py`, with explicit included
parent directories for copied trees. The pure-code model checks each actual
context COPY (excluding builder-stage COPY), wildcard source expansion, all
copied descendants and their ancestors under this definition's literal/*/**/
negation rules. Removing the guard inclusion must fail that same assertion.

The outer staging root stays 0700 and is not copied into the image. Every nested
context directory is explicitly fchmod'ed to 0755 through its checked dirfd,
independent of a private 0077 umask. Output `directories` records actual modes
and ordinary Docker COPY's root:root ownership contract; `staging_root` records
its private mode and non-runtime scope. New package files canonicalize to 0644,
or 0755 when the pinned archive marks any executable bit, so publisher-private
0600/0700 modes cannot strand data/code behind root-only access. Other source
files retain the already-checked 0644 mode. UID/GID10001 can traverse/read these
root-owned trees but does not gain write permission to their code.

Tests model ordinary COPY assigning root:root then final USER10001:10001,
using actual staged modes, the directory receipt and OTHER read/execute bits.
They deliberately corrupt a directory back to 0700 and require rejection. The
model excludes the private outer host staging root and does not claim a real
Docker COPY/image test. A directory fchmod error must close all descriptors and
remove only the newly created output. Invalid inputs still exit 2 before Docker;
R3 and final combined image/runtime acceptance remain separate.

## S-CLAUDE-BUILD-ENTRY (r26 follow-up)

The entry requires `HOSTED_BUILD_ENGINE`, independently inventoried
`HOSTED_BUILD_ENGINE_ID`, local `HOSTED_IMAGE_TAG`, exact clean
`CLAUDE_SOURCE_COMMIT`, the existing five offline input paths and a new absolute
`HOSTED_BUILD_CONTEXT`. It adds no fetch/download step. Operator approval of the
actual isolated Engine/build remains external to these variables and this code.

1. Required variables and unix URI shape are checked before work. Python resolves
   lexical/symlink aliases, refuses both default host socket spellings and their
   aliases, and requires an existing Unix socket using metadata only. All Docker
   commands use that same canonical endpoint. No socket is connected by this
   metadata check.
2. The already-reviewed packager produces the context. It alone selects source
   inputs; no recursive source-tree copy or alternative context path is added.
   The wrapper invokes its child Python with `-B` so imports do not dirty a clean
   source tree even without ignore rules. The calling test/canonical environment
   requires no bytecode flag or environment workaround.
3. Only after material validation succeeds does `docker --host ... info --format
   '{{.ID}}'` read the daemon ID; it must be nonempty and exactly equal the approved
   ID. Mismatch is fixed exit2, and failed info propagates nonzero with no retry.
   Unlike H3's identity-before-generation ordering, the offline Claude context
   is prepared first to preserve its stricter invalid-input-before-ANY-Docker
   contract. An identity mismatch can leave that verified context for inspection;
   it cannot run build/inspect or delete existing inputs.
4. Build uses exactly `--platform=linux/amd64 --network=none --pull=false`, the
   source commit build argument, explicit local tag and verified context. Failure
   stops the sequence; no retry, push or implicit publication is performed.
5. `image inspect --format '{{.Id}}'` obtains the local immutable image ID. Only a
   `sha256:` plus 64 lowercase hex digits is emitted on success for later pinning.
   Inspect failure or malformed/empty ID is nonzero. Registry digest generation,
   push and registration require their own authorization.

Tests use an unlistening bound Unix socket for metadata and a fake Docker program
which only logs argv and reads the actual generated receipt. Positive assertions
bind exact info→build→inspect order/arguments, receipt bytes and clean source;
negative cases cover missing variables, default/lexical/symlink aliases, absent
or regular socket paths, invalid locks/hashes/dirty source, wrong/empty Engine
IDs, info/build/inspect failure, malformed inspected IDs, no retries and no false
success. A separate pure metadata model makes the default socket appear to exist
without binding or connecting it, so removing the alias guard is detectable even
on a machine where no default Docker socket exists. `verified=false` is tested.
