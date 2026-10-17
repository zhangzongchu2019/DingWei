# S-HOSTED-CLAUDE implementation note — intermediate, not deployable

This file fixes the interface boundary before the launcher, Hub login service, and image
definition are added. It does not authorize an image build, a real login, or a node action.

## Pinned target

The managed Claude CLI target is **2.1.285**. It is the lower of the two versions recorded
for the six-seat maintenance window (opus50tc2k 2.1.285; tosonnet55tc2k 2.1.296), so the
managed contract is not silently based on behaviour unavailable to the older seat. The final
offline lock must pin that exact release and its hashes; a newer release needs a separate
compatibility review.

## Credential ABI

The initial implementation uses `token_file`, not a real device-code login. A Claude account
credential is an opaque JSON object validated as: non-empty, at most `MaxSecretBytes`, no NUL,
valid JSON object, and containing no array/scalar root. It is never logged or included in an
audit detail. This is a synthetic-test ABI only until an owner-approved real Claude login flow
defines the provider-compatible file format; no existing Claude login state is read or copied.

The launcher will accept only `SH_CLI=claude`, `SH_CRED_KIND=token_file`, a regular 0600
UID-10001 credential file, and will atomically write the approved credential payload beneath
the Claude home. It will generate the same restricted `dingwei-send` MCP configuration as the
Codex image: body-send only, no file read/spec/vsapi entry points.

## Hub and image contract

The planned verified combo is `claude/claude/token_file`; it remains `verified:false` until the
launcher and local tests pass. The image context and Dockerfile will be a separate allowlisted
offline definition. It will retain the locked Python venv, UID 10001, nodeagent-enforced
read-only root filesystem, and fail-closed old-helper-state rule. It is only built on the target
host's independent dockerd under separately approved execution.

Kimi is deliberately excluded: it has a different provider/driver and no authorized credential
format or launcher ABI in this change.
