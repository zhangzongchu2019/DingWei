# S-CLAUDE-IMAGE-XZ — author delivery

Base: adopted r29 `b82dfbcd8da9dd7eebd67328f65a3cd03140f411`.
Source/test pin: `30f22a3cb0ffdf15829415cf6ed96b8d2cf6c1ad`, tree
`bcdd0476e045ec046b1fc489c3d70fd8ce908c7f`. Final delivery adds evidence only.
Ref: `review/coder/claude-image-xz` in `git@git.code.tencent.com:wego-ai/dingwei.git`.
Independent review: opus50ts; this report is author evidence, not review PASS.

## Change and input integrity

The only executable definition change is one Dockerfile line: replace GNU tar's
external xz subprocess with Python 3.12 `tarfile.open(..., mode="r:xz")`, explicitly
`extractall(..., filter="data")`. Keep the existing mkdir/mv/rm flow. No system
package, base digest, npm/Node/helper lock, combo, launcher, packager or build
wrapper change. `SOURCE.json` binds changed paths and unchanged critical blobs.

The Node SHA256 is rechecked inside the RUN against the original unchanged lock.
Extraction consumes the same bytes that were hashed; it does not reopen an
unchecked pathname. The lock-binding assertion guards against accidental digest
drift. The earlier prepare_context hash check remains intact.

## Tests and withdrawal evidence

Four added cases execute the actual Dockerfile RUN with a small synthetic Node
archive, Python 3.12.3, and a restricted PATH containing tar but no xz. They check
relative bin/npm symlink preservation and target contents/executable mode,
parent-traversal refusal, escaping-symlink refusal, and hash refusal before any
archive member is extracted. Existing Claude packager/launcher assertions stay.

- Claude test file: **34 tests passed** in the canonical raw log.
- Full canonical: **rc0, BUILD_TESTS_OK python=49 shell=2**, 172.686s; local temporary PG parent plus all five claim subcases passed; log SHA256 `f22d335f0b9d1cfd285214f5db9e9cd0f8b0616ddc76ca94d02f863b763d3dad`.
- Source controls in detached source worktree, tests unmodified: fixed rc0;
  replace Dockerfile with original r29 rc1; replace data filter with fully_trusted
  rc1; remove hash check rc1; restore fixed rc0. The original-source test red first
  catches the now-required lock binding; the separate direct RUN probe below
  proves the old executable dependency independently, without that assertion.
- `raw/old-tool-probe.json`: original r29 RUN with tar/no xz produces the exact
  `tar (child): xz: Cannot exec: No such file or directory`, rc2/no installed Node;
  fixed exact RUN succeeds rc0 with the same environment and synthetic archive.
  Fixture hash replaces the locked digest only in the executed synthetic command,
  never in the Dockerfile or lock.

`environment.json` is the explicit empty-environment allowlist used for canonical:
private HOME/GOCACHE, shared existing module cache, offline Go module resolution,
locked runtime venv, short private TMPDIR, no -B or bytecode-disable variable.
Canonical executes the normal `./build.sh --test-only`, not a scoped wrapper.

## Executable audit and remaining target gates

See `deploy/ops/hosted-claude-image/NODE-XZ-CONTRACT.md` for every RUN command.
Previous real-build progress, as reported by prodops, establishes that its earlier
Python/venv/pip, dpkg/dpkg-query/test/rm, and mkdir commands passed. Coder did not
independently inspect the target base or execute an Engine. Final mv/chmod and
base Python lzma/_lzma are explicitly unconfirmed target capabilities; prodops was
asked for approved read-only exact-base/network=none resolution/import evidence.
Local Python `_lzma` success is local fixture evidence only. If target evidence
arrives later it must retain its own source/Engine/base attribution.

No real Node or Claude CLI execution, actual image build, Docker daemon, host,
production credential, Hub, external PG, Agent or notification was used. Canonical
uses only its disposable local Hub/PG fixtures. No deployment/retry authorization
is implied. Existing Codex image and failed Claude builder are untouched.

## Cleanup

No owned fixture processes remained. Private cache/HOME/TMP were removed; detached mutant worktree removed. See CLEANUP.json for measured bytes. Source worktree retained for review; no shared cache cleanup.
