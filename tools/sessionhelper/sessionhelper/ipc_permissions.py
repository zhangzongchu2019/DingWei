"""Explicit opt-in hosted IPC permissions; default and legacy upload-only stay unchanged."""


def send_body_only(env):
    value = env.get("SH_IPC_SEND_BODY_ONLY", "").strip().lower()
    if value not in ("", "0", "false", "no", "off", "1", "true", "yes", "on"):
        raise ValueError("invalid SH_IPC_SEND_BODY_ONLY")
    return value in ("1", "true", "yes", "on")
