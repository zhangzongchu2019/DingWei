"""Byte limits shared by the MCP shim and the local IPC receiver."""
import json

MESSAGE_BODY_MAX_BYTES = 1 << 20  # UTF-8 business body, no truncation.
IPC_REQUEST_MAX_BYTES = 6 * MESSAGE_BODY_MAX_BYTES + (64 << 10)  # Worst JSON escaping + metadata, excluding newline.
IPC_BODY_ONLY_MAX_BYTES = IPC_REQUEST_MAX_BYTES
SPEC_BODY_MAX_BYTES = 2 * 1024 * 1024  # Match Hub's HTTP request-body limit.
SPEC_WRITE_IPC_MAX_BYTES = max(SPEC_BODY_MAX_BYTES + (64 << 10), IPC_REQUEST_MAX_BYTES)


def encode_spec_body(body):
    # Use the SAME bytes for preflight and HTTP. Escaping Chinese as ASCII
    # would otherwise make the HTTP body larger than the shim's measurement.
    return json.dumps(body, ensure_ascii=False).encode("utf-8")


def size_rejection(actual_bytes, limit_bytes, scope):
    return {"status": "error", "code": "request_too_large", "outcome": "rejected",
            "retryable": False, "actual_bytes": actual_bytes, "limit_bytes": limit_bytes,
            "scope": scope,
            "message": f"{scope} is {actual_bytes} bytes; limit is {limit_bytes} bytes; request not dispatched"}


def spec_body_rejection(body):
    actual = len(encode_spec_body(body))
    return size_rejection(actual, SPEC_BODY_MAX_BYTES, "spec_body") if actual > SPEC_BODY_MAX_BYTES else None


def ipc_size_rejection(result):
    """Fail closed for legacy IPC clients while retaining deterministic details."""
    return {"ok": False, "error": "request_too_large", **result}
