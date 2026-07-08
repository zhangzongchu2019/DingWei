#!/usr/bin/env python3
"""DingWei 统一发信脚本：给指定会话发一条消息，即发即断。无第三方依赖（纯标准库）。

用法：
  send.py <收件会话名或完整地址> <正文...>
  send.py <收件会话名或完整地址> --file <正文文件>       # 长正文用文件

凭据从环境变量读（sessionHelper 已把它们注入到 CLI 运行环境）：
  SH_KEY_ID / SH_SECRET / SH_WS_BASE / SH_SESSION_NAME

收件地址：只给会话名会自动补成 <会话名>#<SH_KEY_ID>；也可直接给完整 <会话名>#<key>。
⚠️ 会话名必须用【在线清单文件】里的精确名字（如 zzc-manager），不要用简称/旧名。
"""
import base64
import json
import os
import socket
import ssl
import sys
import time
from urllib.parse import urlparse


def send(recipient: str, body: str) -> None:
    key = os.environ.get("SH_KEY_ID", "").strip()
    secret = os.environ.get("SH_SECRET", "").strip()
    ws_base = os.environ.get("SH_WS_BASE", "wss://ts.wegoab.com/dingwei").strip()
    me = os.environ.get("SH_SESSION_NAME", "sender").strip() or "sender"
    if not key or not secret:
        sys.exit("缺少 SH_KEY_ID/SH_SECRET 环境变量（应由 sessionHelper 注入到 CLI 环境）")
    to = recipient if "#" in recipient else f"{recipient}#{key}"
    name = f"{me}-note"  # 临时发信会话名，不占用/顶掉真实会话

    u = urlparse(ws_base)
    host = u.hostname
    port = u.port or (443 if u.scheme == "wss" else 80)
    path = u.path.rstrip("/") + f"/ws/session/{name}?key_id={key}"

    raw = socket.create_connection((host, port), timeout=15)
    if u.scheme == "wss":
        try:
            ctx = ssl.create_default_context()
            sock = ctx.wrap_socket(raw, server_hostname=host)
        except ssl.SSLError:
            ctx = ssl._create_unverified_context()
            sock = ctx.wrap_socket(raw, server_hostname=host)
    else:
        sock = raw

    handshake_key = base64.b64encode(os.urandom(16)).decode()
    req = (
        f"GET {path} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {handshake_key}\r\nSec-WebSocket-Version: 13\r\n"
        f"Authorization: Bearer {secret}\r\n\r\n"
    )
    sock.sendall(req.encode())
    resp = sock.recv(2048).decode(errors="replace")
    first = resp.split("\r\n", 1)[0] if resp else ""
    if " 101 " not in first:
        sys.exit(f"WS 握手失败：{first or '无响应'}")

    env = {
        "id": os.urandom(8).hex(),
        "to": to,
        "from": f"{name}#{key}",
        "body": body,
        "ts": int(time.time()),
        "meta": {},
    }
    payload = json.dumps(env, ensure_ascii=False).encode("utf-8")

    frame = bytearray([0x81])  # FIN + text opcode
    n = len(payload)
    if n < 126:
        frame.append(0x80 | n)
    elif n < 65536:
        frame.append(0x80 | 126)
        frame += n.to_bytes(2, "big")
    else:
        frame.append(0x80 | 127)
        frame += n.to_bytes(8, "big")
    mask = os.urandom(4)
    frame += mask
    frame += bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    sock.sendall(frame)

    time.sleep(1.0)  # linger，让 Hub 处理投递后再断
    try:
        sock.close()
    except Exception:
        pass
    print(f"sent to={to} bytes={n}")


def main() -> None:
    args = sys.argv[1:]
    if len(args) < 2:
        sys.exit("用法: send.py <收件会话名或地址> <正文>   或   send.py <收件> --file <正文文件>")
    recipient = args[0]
    if args[1] == "--file":
        if len(args) < 3:
            sys.exit("--file 需要跟一个文件路径")
        body = open(args[2], encoding="utf-8").read()
    else:
        body = " ".join(args[1:])
    if not body.strip():
        sys.exit("正文为空")
    send(recipient, body)


if __name__ == "__main__":
    main()
