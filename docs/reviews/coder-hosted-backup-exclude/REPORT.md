# H2 hosted backup exclusion

Base r19 `8a3daeafddaf0fa2548127889b255be7528b6226`; independent ref `review/coder/hosted-backup-exclude`. Only nodeagent backup code, its tests and documentation/evidence change. No version change on this independent r19 branch; integration receives H1 release pins.

The tar command now excludes root CLI-private trees and helper state, including enrollment credentials, auth/refresh/config copies, update journal/slots and inbox/WAL/SHM. Anchoring preserves business files with the same names in project subdirectories. CLI-private histories/configuration are intentionally omitted along with credentials; restore is business-data recovery, fresh authorized enrollment and owner LLM login, not identity/session resurrection. See `docs/runbooks/hosted-backup-restore-boundary.md` for exact loss/scope and historical archive limitations.

Protected inode hardlinks are refused using metadata before obtaining the node secret or starting upload. No credential content scan or read is performed. Existing lifecycle stop-before-backup and writer fencing remain prerequisites; no hostile-host/concurrent-writer security claim. Arbitrary user copies of credentials into unrelated business data are outside path filtering. No tar symlink dereference is enabled.

Local nodeagent package tests rc0 (98 top-level tests listed, 3.846 seconds). Actual gzip/tar/loopback HTTP fixture inspection checks decompressed member contents and absent paths, preserved work files and restored absence of login/helper identity; the old compressed-bytes sentinel test remains but is not our exclusion proof. Two separate actual source withdrawals return rc1 with assertions unchanged: restore original two-name exclusion list exposes private tar members; remove preflight hardlink guard reaches forbidden secret access. Temporary shared local clone removed automatically. No full product/runtime integration, image builds, Docker/hosts/PG/key/real Agent/backup actions.

UTC 2026-10-17T00:53:53.331914+00:00. Design/implementation review requested from opus50ts; no deployment authorization.
