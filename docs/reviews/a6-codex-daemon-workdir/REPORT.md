# S-A6-CODEX-DAEMON-WORKDIR

Base: integration/spec5ff-r11, b0f449bd7b2883808c3bce322ede635d56aa6a83.

## Source diagnosis

DriverAdapter._build_driver passes SH_CLI_CWD (or its existing home default) as CodexProtocolDriver.workdir. CodexProtocolDriver defaults to transport_mode=daemon_ws and manage_daemon=True. _start_locked invokes _start_daemon and creates UnixWebSocketJsonRpc, which has no owned subprocess _proc. The previous agent_metrics_workdir assumed an owned stdio RPC subprocess and therefore returned None for this default transport.

The actual driver _start_daemon executes the local `codex app-server daemon start` command with cwd=self._workdir. _ensure_thread sends thread/start with cwd=self._workdir (as well as the existing model/sandbox/approval settings). The local start call does not change mount namespaces. Thus, for a helper-managed daemon, the explicit configured thread directory is the filesystem basis, rather than the control socket location or helper process cwd. The test calls these actual methods with a mock subprocess command and recording RPC; it does not start a real Codex daemon or model.

## Correction and boundary

For a started Codex driver whose transport is daemon_ws and manage_daemon is exactly True, agent_metrics_workdir returns the configured absolute string as the sampling candidate. DiskSampler.collect remains the authority for directory validity and sample availability: O_DIRECTORY|O_NOFOLLOW opens the candidate, fstat/fstatvfs samples its real local filesystem, and the existing before/after device/inode and non-symlink named-directory checks must succeed. Missing directory, a symlink or regular file gives unavailable/sample_failed; a relative/non-string/unbound configuration gives unavailable/workdir_unbound. There is no generic os.getcwd/helper-cwd fallback, realpath substitution, or path exported to Hub.

Basis remains cli_workdir_filesystem: **daemon mode uses the driver-configured working directory**, not the daemon process's incidental cwd. This is a configured local-thread directory binding for a managed daemon, not external/remote process namespace discovery. An external daemon (manage_daemon=False) remains unbound even when a configured directory is present. Direct/proxy Codex keep the existing owned-process /proc namespace/cwd checks; Claude, OpenCode and CLI paths are unchanged. The existing publisher rechecks the directory binding after collection and suppresses changed bindings.

No wire schema, sampling cadence, Hub auth, renderer, version, or packaging behavior changed. This is development/review only. A new pilot package and upgrade of oaagent.lbl must be performed later by the authorized executor/prodops; no pilot deployment is done here.

## Verification

See RESULT.json and exact raw log assets in this directory. New tests use a real temporary directory/file/symlink and actual open-fd fstatvfs. Captured fd device/inode matches the configured directory, and published total/used/available bytes exactly match that fd's returned statvfs values. This proves the selected local filesystem without inventing a production disk value.

The withdrawal harness replaces only DriverAdapter.agent_metrics_workdir in its isolated test process with the exact base method; product files are untouched. Managed-daemon binding, real-directory sample and publisher cases fail as unavailable/unbound; unchanged external/other-driver cases remain green. This establishes detection of the original missing-binding defect.


### Local build environment failures retained

The first isolated-HOME canonical build attempt returned rc1 when Go tried to unpack existing dependencies into its new HOME-derived module cache and hit ENOSPC. The second attempt explicitly reused /home/dev/go/pkg/mod and /home/dev/.cache/go-build; Python gates completed, but the shell/Go integration gate could not create TempDir paths (ENOSPC), rc1. Neither is counted as PASS. Both original raw logs are retained. No cache purge or production action was performed. Leader was sent exact source commit 9822df07e770bd249c94eacc05dc185d8d2ffeb7 for an authorized, space-sufficient build supplement. A future supplement must name its executor and exact checked commit; this local report does not claim someone else's run.


### Completed author checks

- Locked Python + PYTHONPATH=tools/sessionhelper, targeted new and existing agent-disk suites: 25 tests, OK.
- Fresh HOME/minimal environment, locked Python full helper discovery: **647 tests, rc0, 337.582 seconds**.
- Exact-parent binding-method withdrawal: rc1 with the expected managed-daemon binding/capacity/publisher assertions failing; external/other-driver controls pass.
- git diff --check: rc0.
- Canonical build remains **environment-blocked**, both rc1 logs preserved; no successful supplement received at evidence capture time. This is not a whole-task PASS or deployment candidate approval.

Raw logs are deterministic gzip assets with original uncompressed SHA256 in RESULT.json. They preserve original whitespace and failures without text normalization. The product change is eight added lines in one helper file; the other source change is the new regression test. This evidence commit does not change those tested source blobs.


### Documentation follow-up

The authoritative A6 `docs/reviews/coder-a6-real-disk/CONTRACT.md` now explicitly documents the managed Codex daemon exception to the owned-PID rule and adds its compatibility-table row. `tools/sessionhelper/README.md` also describes the configured thread directory, unchanged secure collector, external-daemon rejection and no helper-cwd fallback. This follow-up changes documentation only; all product/test blobs remain equal to source commit 9822df07. No duplicate helper/build rerun is claimed for this documentation-only correction. Independent code/test checks and separately attributed build supplement remain separate evidence.
