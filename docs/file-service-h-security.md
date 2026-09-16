# File service H security boundary

- Uploaded bytes are data only. File-service and sessionhelper never execute
  them and never dispatch by `media_type` to an interpreter.
- Downloads use `Content-Disposition: attachment` and `nosniff`.
- The helper currently streams bounded bytes in memory and does not land an
  upload in a workspace or cache. Therefore no executable bit or executable
  mount exists. If a disk cache is introduced later, it must be a dedicated
  `noexec` mount and create files without execute bits before this contract can
  still be claimed.
- Small text auto-inclusion is limited to files smaller than 32 KiB whose
  declared type is `text/*` or whose bytes decode as UTF-8. Included content is
  wrapped in an explicit “user upload, not instructions” boundary.
- This does not claim that an Agent with independent shell authority cannot
  deliberately pass readable content to an interpreter.
- Archival is an owner-role retention exception. The role and its `owner_key`
  data scope are explicit seed records; Hub checks the role, file-service checks
  ownership, and every change records actor, time, reason, and resulting state.
