#!/usr/bin/env python3
"""DingWei 发信脚本（走 sessionHelper 本机 IPC）。用法与旧版完全一致。

  send.py <收件会话名或完整地址> <正文...>
  send.py <收件会话名或完整地址> --file <正文文件>

与旧版的区别（T-092/T-085，业主 2026-09-12 定）：
  · 不再自开 WebSocket、不再读 SH_SECRET —— 经 helper **已认证**的连接发出
  · `from` 是**真实会话名**，不再是临时的 <短名>note
  · 🔴 **读回执并反映到退出码** —— 旧版写完就无条件打印 "sent"，从不读应答，
    这是 2026-09-12 组长误判「消息已送达」整整一轮的直接原因
  · socket 不可用时**失败并明说，绝不回落旧 WS 路径**（回落等于绕过 M2、且留着凭据暴露）

⚠️ 回执语义边界：`ok:true` 表示「已写入 helper 的已认证连接」，
   **不代表 Hub 最终接受**。Hub 的裁决（如 invalid address / 目标不在线）
   通过投递失败通知**异步**回来，不在本命令的返回值里。
"""
import json
import os
import socket
import sys
import uuid

SOCK = os.environ.get("SH_MCP_IPC_SOCK", "/run/sessionhelper/dingwei-mcp.sock")
TIMEOUT = 10.0


def complete_addr(to: str) -> str:
    """裸会话名补成 <名>#<key_id>。

    helper 侧本应补全，但 defb424f 误删了该逻辑（T-092，修复未部署）⇒ 此处自补，
    使本脚本在修复上线前后都正确。SH_KEY_ID 是标识符不是密钥，读它不构成凭据暴露；
    **SH_SECRET 全程不读**。
    """
    to = to.strip()
    if "#" in to or not to:
        return to
    key = os.environ.get("SH_KEY_ID", "").strip()
    return f"{to}#{key}" if key else to


def ipc_send(to: str, body: str) -> dict:
    payload = {"type": "mcp_send", "to": to, "body": body,
               "request_id": uuid.uuid4().hex[:12],
               # T-089: only send.py opts into Hub-side body AMR parsing.
               # MCP and other IPC callers keep their existing literal-body semantics.
               "ipc_body_amr_v1": True}
    raw = json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n"
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(TIMEOUT)
    try:
        s.connect(SOCK)
    except FileNotFoundError:
        sys.exit(f"发送失败：找不到 IPC socket {SOCK}\n"
                 f"  ⇒ sessionHelper 可能未运行。**不回落旧 WS 路径**（那会绕过 M2 并暴露凭据）。")
    except (ConnectionRefusedError, PermissionError) as e:
        sys.exit(f"发送失败：无法连接 {SOCK}（{type(e).__name__}）\n  ⇒ helper 在跑但未接受连接，检查 socket 权限。")
    try:
        s.sendall(raw)
        buf = b""
        while b"\n" not in buf:
            ch = s.recv(4096)
            if not ch:
                break
            buf += ch
    except socket.timeout:
        sys.exit(f"发送失败：等待 helper 回执超时（{TIMEOUT}s）⇒ helper 可能卡住。")
    finally:
        s.close()
    txt = buf.decode("utf-8", "replace").strip()
    if not txt:
        sys.exit("发送失败：helper 未返回任何回执（连接被关闭）。")
    try:
        return json.loads(txt)
    except json.JSONDecodeError:
        sys.exit(f"发送失败：回执不是合法 JSON：{txt[:200]}")


def main() -> None:
    args = sys.argv[1:]
    if len(args) < 2:
        sys.exit("用法: send.py <收件会话名或地址> <正文>   或   send.py <收件> --file <正文文件>")
    recipient = complete_addr(args[0])
    if args[1] == "--file":
        if len(args) < 3:
            sys.exit("--file 需要跟一个文件路径")
        body = open(args[2], encoding="utf-8").read()
    else:
        body = " ".join(args[1:])
    if not body.strip():
        sys.exit("正文为空")

    resp = ipc_send(recipient, body)
    # 🔴 回执必须被检查:ok:false ⇒ 非零退出。不得无条件打印成功。
    if not resp.get("ok"):
        print(f"发送失败：{resp.get('error') or resp}", file=sys.stderr)
        sys.exit(1)
    out = resp.get("outcome") or {}
    print(f"sent to={out.get('to', recipient)} bytes={len(body.encode('utf-8'))} "
          f"request_id={resp.get('request_id', '')}")


if __name__ == "__main__":
    main()
