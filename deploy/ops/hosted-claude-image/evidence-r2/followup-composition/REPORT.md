# Claude R2 composition follow-up — author delivery

Scope: close only R2.1 and R2.2 from opus50ts exact review `d3b174ea663636a51aa5c0ce71568768891788e7`. This is author implementation/local synthetic evidence awaiting independent review; not an R3, merge-tree or image/runtime acceptance.

- Repository: `git@git.code.tencent.com:wego-ai/dingwei.git`
- Ref: `review/coder/hosted-claude-packager`
- Previous reviewed delivery: `be1dbb8d659456b494d306596a2790cd1965488c` / tree `1424331dc931bbe391fcda91b140c9a8d7ce62bc`.
- Frozen source: `81f0aa08118e22b0abeafb2d575fe16a603aa460` / tree `6db7c0d4afe045888cb754403cd85038bd13ca3e`.
- Only four source paths changed: Claude `dockerignore`, `prepare_context.py`, `test_claude_start.py`, `PACKAGER-CONTRACT.md`. Delivery tip/tree are reported separately after evidence commit; source-to-delivery changes contain evidence only.

## R2.1: actual COPY / ignore composition

The staged ignore now permits `start.py` selected by the actual Dockerfile and explicitly permits directory ancestors of copied helper, wheel, deb, Claude/native and Node content. The test reads the actual staged Dockerfile/ignore, expands its actual source globs, and checks every copied file and parent directory. The small ignore model covers the literal, segment-star, recursive-star and ordered negation patterns used by this definition; it does not claim a general Docker ignore parser. Removing only `!start.py` makes the unchanged composition assertion fail; restoration passes. No Docker command is invoked.

## R2.2: ordinary COPY followed by UID10001 access

The outer staging root remains private 0700 and is not copied into the image. Every created runtime subdirectory is explicitly fchmod 0755, including under umask 0077. BUILD-INPUTS now records each directory's actual mode and ordinary COPY root:root ownership assumption. Archive regular files are normalized to 0644 or 0755 according to executable intent rather than retaining private publisher modes. Existing nofollow/type/content checks remain.

The faithful permission model applies the ordinary root-owned COPY and final USER10001:10001 contract to the actual generated directory/file tree: all copied directories must be traversable by that user, regular runtime inputs readable, and synthetic executables executable. It checks manifest-versus-filesystem equality, private outer root, umask independence, helper/factory/sessionhelper and native/Claude paths. A separate chmod-failure case verifies descriptor closure and failed-output removal. This is a filesystem/ownership model, NOT a real UID, Docker COPY, built-image or kernel access test.

## Exact local readings (2026-10-17 21:32Z)

| Gate | Result |
| --- | --- |
| Claude full synthetic fixture, frozen source | 19 tests, rc0 |
| Unchanged H3 local fixture regression | 36 tests, rc0 |
| Entrypoint inventory contract | 11 tests, rc0 |
| Reviewer's original two composition probes | 2 tests, rc0 |
| Remove start.py ignore exception | fixed 0 / withdrawal 1 / restored 0 |
| Restore directory mode 0700 | fixed 0 / withdrawal 1 / restored 0 |
| Remove explicit chmod under private umask | fixed 0 / withdrawal 1 / restored 0 |
| Restore publisher-private archive modes | fixed 0 / withdrawal 1 / restored 0 |
| Restore both be1 product files, original reviewer probes | fixed 0 / withdrawal 1 (both original assertions) / restored 0 |

Each withdrawal ran the same assertions with exactly the declared source mutation. Every red is an assertion failure, not discovery, compilation or environment error. `CONTROLS.json` binds per-phase file hashes; raw logs and result files bind commands, times, counts and log hashes. `RESULTS.json` is the consolidated index.

The two reviewer probe assertions are unchanged. Only the hardcoded module path is adapted from the reviewer's temporary checkout to this author's private detached worktree; both scripts and `PROBE-PROVENANCE.json` preserve this distinction. `run.py` is the historical local harness and records absolute private paths; those fixtures have been disposed, so it is not a portable one-command runner. Production unittest tests remain repository-based and self-contained.

## Preserved boundaries and evidence attribution

`SOURCE.json` binds 15 Claude source paths. Git/function-AST comparison confirms no changes to safe_rel, open_dir, read_at, regular, JSON handling, Git anchoring, source_inputs, h3_snapshot or check_wheels. Only prepare output permissions/receipt and unpack_package mode normalization change in the packager. Build script, source map, Dockerfile, launchers, verify_inputs, combo and package lock are unchanged from be1. Previous accepted input-validation controls remain attributed to be1 and are not relabeled new runs. Existing 15 tests remain; four tests are added.

The 19-test run includes the inert Docker-before-contact refusal test. The actual build wrapper continues its unconditional exit2 before any Docker operation; verified remains false. No full canonical, Go, PG, actual Docker/client/daemon, helper/guard/CLI, package download, credential/host/Agent, build or deployment was performed. R3 and the eventual combined tree require their own exact review.

`CLEANUP.json`: detached mutation worktree restored clean then removed; private HOME and initially empty private TMPDIR removed; no owned fixture process remains and no Go cache was used. Main author checkout and evidence are retained. Four-seat freeze and owner execution gates remain unchanged.
