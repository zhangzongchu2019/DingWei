"""Local file client. It sends references over IPC; it never accepts a bearer."""

from __future__ import annotations

import argparse
import base64
import json
import os
import socket
import sys
import uuid


def request_file(socket_path: str, file_ref: str, method: str, byte_range: str = "") -> dict:
    payload = {
        "type": "file_access_v1",
        "request_id": uuid.uuid4().hex[:12],
        "file_ref": file_ref,
        "method": method,
    }
    if byte_range:
        payload["range"] = byte_range
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(10)
        client.connect(socket_path)
        client.sendall(json.dumps(payload).encode("utf-8") + b"\n")
        data = bytearray()
        while b"\n" not in data:
            chunk = client.recv(4096)
            if not chunk:
                break
            data.extend(chunk)
    response = json.loads(bytes(data).decode("utf-8"))
    if not response.get("ok"):
        raise RuntimeError(response.get("error", "file access failed"))
    return response["result"]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="dingwei-file")
    parser.add_argument("--socket", default=os.environ.get("DINGWEI_MCP_IPC_SOCK", ""))
    sub = parser.add_subparsers(dest="command", required=True)
    head = sub.add_parser("head")
    head.add_argument("file_ref")
    get = sub.add_parser("range")
    get.add_argument("file_ref")
    get.add_argument("byte_range")
    args = parser.parse_args(argv)
    if not args.socket:
        parser.error("--socket or DINGWEI_MCP_IPC_SOCK is required")
    if args.command == "head":
        result = request_file(args.socket, args.file_ref, "HEAD")
        print(json.dumps(result, ensure_ascii=False))
    else:
        result = request_file(args.socket, args.file_ref, "GET", args.byte_range)
        sys.stdout.buffer.write(base64.b64decode(result["content_base64"], validate=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
