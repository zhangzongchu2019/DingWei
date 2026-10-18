# S-HOSTED-CLAUDE-MODEL — developer delivery

Repository `git@git.code.tencent.com:wego-ai/dingwei.git`, ref `review/coder/hosted-claude-model`. Author coder56solzzc.zzc; independent reviewer opus50ts.zzc. This is implementation/test evidence awaiting review, not self-issued review PASS or permission to build/deploy/run Claude.

## Source and rebase

The requested r28 condition occurred during development. Adopted base is **r28 `63208e4256b9285f3fe8fe3a909559e48e3fd933`**. Model source is **`7f1adbb37841256abc5f2ddcd316ba73068678c2`**, tree **`45b82a651e2120ebefc6ab59fa6868ecb6f289eb`**: a mechanical cherry-pick `3bdc17e3` of the original r27 implementation `dbc789c3`, then a documentation-only qualification clarification. All eight changed executable/test blobs equal the original implementation. The owner fence, quota release, generic Console renderer, helper/version/locks, actual combo definition (`verified=false`), image definition and packager map/guard are preserved from r28. SOURCE.json binds these objects; SOURCE-R27.json / RESULTS-R27.json retain the earlier run attribution. The earlier exact dbc source is advertised on `evidence/coder/claude-model-r27-dbc789c3` solely as historical evidence, not an adopted candidate.

## Implementation contract

Claude may now specify nonempty Create.model. Hub retains fresh online node selection and exact membership in authenticated capacity.models; node create independently checks the existing seatModels approval list before Engine work. The historical configuration name is still NODEAGENT_SEAT_CODEX_MODELS: no alternate unapproved model registry/discovery/settings bypass is added. Owner-approved values for that node/intended CLI must be configured separately; no API accepts arbitrary env or model-list overrides. Claude IDs additionally require ASCII alphanumeric first character, then ASCII alphanumeric/dot/underscore/hyphen, at most 64 characters, so option-shaped values cannot reach the CLI even if mislisted. Unsupported CLIs still return model_not_supported. Codex behavior is unchanged.

The exact model persists in Seat.Model and existing async jobs send it in the create command; lifecycle upgrade uses the recorded value. Node supplies CLAUDE_MODEL plus CLAUDE_MODELS (JSON approved list) only for nonempty Claude selection. Launcher rechecks syntax/list membership before common guard, credentials or state writes, and reports only the fixed field diagnostic for invalid values. Model is not copied into credential JSON or MCP config. The sole added control path is these node-created model env keys and a derived fixed CLI argument list.

Official ABI is **`--model <model>`**, built into quoted argv beside --mcp-config. The fake CLI verifies exact arguments including paths with spaces, with no shell or real Claude. Empty/omitted model deliberately preserves the previous CLI default/settings: no model env or flag, no Codex default inheritance. Claude upgrade must still have the saved nonempty value approved before building a replacement; it does not silently fall back. Running/stopped containers retain creation-time env, so removal is not automatic live revocation. Codex recorded-model upgrade behavior stays unchanged.

No production combo/verified value, account login ABI, helper runtime/version, dependency lock, arbitrary env/file override, retries, migration, credentials or real configuration was changed. Matching Hub/nodeagent/launcher qualification is required before enabling this path; no old node/image mixed-version or real provider/account compatibility claim.

## Actual locked ABI (static, no real CLI)

Official reference: [Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference). It documents session model override, but current documentation alone is not used as historical version proof.

The exact official npm wrapper and linux-x64 native **2.1.285** archives were fetched read-only and verified against the existing Git lock's SHA512 integrity plus SHA1. Wrapper package.json declares 2.1.285 and its optional native dependency at that exact version. Static wrapper argv forwarding (`process.argv.slice(2)`) is at byte4354. Native package metadata also declares 2.1.285; ELF SHA256 is `33dad1ec615a2e08cc78b494f05c110e49916de2c79d78ec8799ebf46b233d29`. Main CLI option declaration `.option("--model <model>",` is at byte **212441413**. The requested full model ID also appears in this native payload. None of this establishes that a particular account/provider can serve it.

`abi/*-verification.json` records official URLs, archive hashes, sizes, package metadata and minimal declaration offsets. No package/binary/full source was committed or relayed, no installer/postinstall/Node-on-package/real CLI was run. The inspected 107,536,804-byte native archive and 28,026-byte wrapper archive were deleted after recording metadata. The image lock and packaging pipeline are unchanged.

## Validation and exact attribution

Final r28 readings are in RESULTS.json and raw/r28-* result/logs. Earlier r27 long gates and serial canonical also passed but remain separately attributed; no earlier readout is relabelled as r28. All required long package commands use `-race -count=1 -timeout=45m`; canonical is original `./build.sh --test-only`, env-i with locked Python and short private TMPDIR, no bytecode suppression or test exclusion.

| Check on final r28 source | Result | Raw evidence |
|---|---|---|
| Original canonical | rc0; **49 Python +2 shell**, PG parent+5 all pass;109.737s | raw/r28-canonical-serial.log |
| hosted full race /45m | 175 top +161 subcases; rc0,0fail/skip;448.925s | raw/r28-full-hosted.jsonl |
| workpulse full race /45m | 127 top +38 subcases; rc0,0fail/skip;242.353s | raw/r28-full-workpulse.jsonl |
| nodeagent full race /45m | 170 top +95 subcases; rc0,0fail/skip;18.582s | raw/r28-full-nodeagent.jsonl |
| Existing model browser | 8 pass /0fail, existing installed browser | raw/r28-browser-model.log |
| Launcher/packager Python | 30 tests rc0, included in original canonical | raw/r28-canonical-serial.log |

Coverage: real Hub Store/Create/Seat/job command path, actual HTTP async creation/status, unknown/malformed/empty node list and option-shaped model denial before issue/Engine, unverified combo stays closed, empty/default preservation, actual node create spec/env and upgrade, original Codex model/default/upgrade assertions, launcher fake argv/credential-before-read refusal, existing model browser behavior. Test combos are explicitly synthetic true fixtures; committed combo stays false. The old unsupported-CLI negative now uses opencode in place of Claude, retaining its same error/priority assertion; existing Codex cases are unchanged.

Seven actual source withdrawals in an isolated dbc checkout: remove Claude Hub permission, Hub node membership, node membership, Claude env injection, node format guard, launcher model flag, launcher membership. Each produces rc1 from the unchanged relevant assertion, restored original source rc0. raw/withdrawals.json records commands and original/mutated SHA256. All targeted product/test blobs are identical at final r28 source, independently bound in SOURCE.json; these controls are not falsely labelled as rerun on a different checkout. No full gate was run on mutated source.

Initial development fixture correction: a new positive node test attempted an empty proxy, which the pre-existing mandatory-proxy rule correctly rejected. The actual-create positive now uses configured proxy; empty-proxy early-return coverage is only an env-construction unit probe. The product proxy prerequisite was not weakened.

The first r27 canonical ran concurrently with other temporary-PG suites and failed only the global PostgreSQL cleanup-increment check: PID742479 was briefly reported as a new zombie although all business claim cases passed. Subsequent direct /proc check found the PID absent; the private-process snapshot was empty. The exact original full canonical passed when serialized without other PG suites (101.408s). Both initial failure and serialized result are retained; no reaper/product/test fix or claim that the cleanup observer was repaired. Final r28 canonical is also serialized after the requested package suites.

## Scope, cleanup and review

All identities/accounts/credentials/Engines/CLI invocations in tests are synthetic local fixtures. No actual host, Docker daemon, real PG/Agent, key/login state, provider/model call, image/release build, offer, job or deployment. Existing dataconn four-seat freeze and owner gates remain.

Owned test/PG children and postmaster markers: zero. Private Go cache 1,002,818,265 bytes → go clean 232 bytes → removed; private HOME 83,357,303 bytes and TMP 2,462,959 bytes removed. Temporary mutant and r27 worktrees removed; inspected vendor archives deleted; no shared cache cleaned. The r28 source/evidence worktree is retained. See CLEANUP.json.

Configuration and empty/upgrade/activation semantics are documented in `docs/runbooks/hosted-claude-model.md`. The relative-key MANIFEST omits itself; SOURCE.json binds final code and preserved r28 fence/locks. Evidence Python/shell helpers use non-executable-discovery fixture suffixes. Record scripts retain author-local sandbox paths as exact audit records; independent reruns should adapt only private sandbox paths. Source exchange remains Git-only.
