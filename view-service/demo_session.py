"""Demo 会话 —— 连本地 hub,发 session_event(富内容),并把网页输入回声成事件。
演示 hub↔View Service 全链路,不需要真 driver。"""
import asyncio
import json
import os

import websockets

HUB = os.environ.get("DEMO_HUB_WS", "ws://127.0.0.1:8791")
KEY_ID = os.environ["DEMO_KEY_ID"]
SECRET = os.environ["DEMO_SECRET"]
SESSION = os.environ.get("DEMO_SESSION", "demosess")


async def main():
    url = f"{HUB}/ws/session/{SESSION}?key_id={KEY_ID}"
    me = f"{SESSION}#{KEY_ID}"
    wp = f"workpulse#{KEY_ID}"
    async with websockets.connect(url, additional_headers={"Authorization": "Bearer " + SECRET}) as ws:
        seq = [0]

        async def emit(kind, text="", data=None):
            seq[0] += 1
            body = json.dumps({"kind": kind, "text": text, "data": data or {},
                               "session_id": SESSION, "cursor": str(seq[0]),
                               "delivery_id": f"d{seq[0]}", "turn_id": ""}, ensure_ascii=False)
            env = {"id": f"e{seq[0]}", "to": wp, "from": me, "body": body, "ts": 0,
                   "meta": {"type": "session_event", "session": SESSION, "key_id": KEY_ID, "no_mirror": True}}
            await ws.send(json.dumps(env, ensure_ascii=False))

        # 开场:富内容(表格/代码/工具调用)
        await emit("state_change", data={"change": "session_init", "session_id": SESSION, "model": "demo-model"})
        await emit("assistant_text", text="# 新版事件流页 · 离线全链路演示\n\n这条链路是 **demo会话 → hub(C1中继)→ View Service(C2订阅)→ 你的浏览器**,真协议、真回执,不是 mock。\n")
        await emit("tool_call", data={"name": "Bash", "input": {"command": "echo 'hub↔ViewService 打通'"}})
        await emit("assistant_text", text="下面几种内容都能结构化渲染:\n\n| 对接口 | 作用 | 状态 |\n|---|---|---|\n| C1 | session_event 中继 | ✅ |\n| C2 | 内网事件订阅 | ✅ |\n| C3 | 鉴权+输入回投 | ✅ |\n| C4 | 入口灰度路由 | ✅ |\n\n```python\ndef hello():\n    return '在底部输入框打字回车,会经 C3 回投到我这里'\n```\n")
        await emit("state_change", data={"change": "turn_completed", "num_turns": 1, "cost_usd": 0.0123})

        # 循环:收到网页输入(terminal_input)→ 回声成事件(证明输入回投链路)
        async for raw in ws:
            try:
                msg = json.loads(raw)
            except Exception:
                continue
            if msg.get("meta", {}).get("type") == "terminal_input":
                text = msg.get("body", "")
                await emit("assistant_text", text=f"✅ 收到你的输入:**{text}**\n\n(它经过:浏览器 → View Service `/input` → hub `/internal/view/{SESSION}/input`(C3)→ demo会话 → 回声事件流回你眼前)")
                await emit("state_change", data={"change": "turn_completed", "num_turns": 2, "cost_usd": 0.0246})


if __name__ == "__main__":
    asyncio.run(main())
