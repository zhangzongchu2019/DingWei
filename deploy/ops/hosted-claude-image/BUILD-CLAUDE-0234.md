# Hosted Claude image definition

This definition pins Claude CLI **2.1.285**, the lower version found by the
metadata-only inventory for the two Claude seats.  That preserves compatibility
with the older recorded seat; it is not evidence that either binary is approved
or that a real login has been exercised.

The image follows BUILD-0234's offline wheel inputs, UID/GID 10001 and
nodeagent-enforced read-only-rootfs contract.  It uses the same factory-only
update-state guard and refuses a non-regular, non-0600, wrong-owner, oversized
or non-object JSON token file.  The token is copied atomically to `/work/.claude.json`;
it is never put in an environment variable or an image layer.  The helper uses
the existing body-only `dingwei-send` IPC settings (`SH_IPC_SEND_BODY_ONLY=1`,
`SH_IPC_UPLOAD_ONLY=0`); actual MCP configuration/CLI compatibility remains a
required isolated validation before a combo may become verified.

`claude-package-lock.json` intentionally has no invented artifact hash.  The
context builder must remain fail-closed until a reviewed offline Claude package
lock and native artifact are supplied.  Per owner ruling, any future build is
only on 43.172.73.6's independent dockerd; this change does not build it.

Kimi is not included: its Claude-Kimi driver and credential format require a
separate verified combo and image ABI.
