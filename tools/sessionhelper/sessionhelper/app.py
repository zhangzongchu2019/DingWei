"""sessionHelper runtime."""

from __future__ import annotations

import asyncio
import json
import os
import re
import sys
from collections import deque
from dataclasses import dataclass
from typing import Protocol

from . import __version__
from .config import Config, load_config
from .llm import LLMProvider
from .protocol import AddressBook, envelope, is_feishu_addr, is_mirror_control, reply_target, session_addr


class Adapter(Protocol):
    name: str

    def handle(self, env: dict) -> str:
        ...


class EchoAdapter:
    name = "echo"

    def __init__(self, cfg: Config, book: AddressBook):
        self.cfg = cfg
        self.book = book

    def handle(self, env: dict) -> str:
        return f"echo[{self.book.self_addr} · {__version__}]: {env.get('body', '')}"


class LLMAdapter:
    name = "llm"

    def __init__(self, cfg: Config):
        self.provider = LLMProvider(cfg)

    def handle(self, env: dict) -> str:
        return self.provider.complete(str(env.get("from") or "default"), str(env.get("body") or ""))


@dataclass
class MirrorState:
    enabled: bool = False
    to: str = ""


COMM_SKILL_ACK = "DINGWEI_COMM_SKILL_INSTALLED"
AGENT_ROUTE_RE = re.compile(r"^(#[A-Za-z0-9_.-]+|@[^\s#]+#[A-Za-z0-9_.-]+)\s+(.+)$", re.S)


def truthy_meta(value: object) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "y", "on"}
    return bool(value)


def is_broadcast_envelope(env: dict) -> bool:
    meta = env.get("meta") or {}
    return bool(str(meta.get("broadcast_dedup_key") or "").strip())


def normalized_text(text: str) -> str:
    return " ".join(str(text or "").split())


def contains_comm_skill_ack(text: str) -> bool:
    return COMM_SKILL_ACK in str(text or "")


def comm_skill_ack_envelope(book: AddressBook) -> dict:
    return envelope(
        session_addr("workpulse", book.key_id),
        COMM_SKILL_ACK,
        book.self_addr,
        {"type": "agent_network_skill_ack", "ack_token": COMM_SKILL_ACK, "no_mirror": True},
    )


def agent_route_envelope(text: str, book: AddressBook) -> dict | None:
    text = str(text or "").strip()
    match = AGENT_ROUTE_RE.match(text)
    if not match:
        return None
    to, body = match.group(1).strip(), match.group(2).strip()
    if not body:
        return None
    return envelope(to, body, book.self_addr, {"type": "agent_route", "no_mirror": True})


class SessionHelper:
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.book = AddressBook(cfg.session_name, cfg.key_id, cfg.bot_name)
        self.mirror = MirrorState(bool(cfg.mirror_to), cfg.mirror_to)
        self.non_primary_broadcast_texts: deque[str] = deque(maxlen=32)
        self.adapter = self._build_adapter()

    def _build_adapter(self) -> Adapter:
        if self.cfg.mode == "llm":
            return LLMAdapter(self.cfg)
        if self.cfg.mode == "cli":
            from .cli import CLIAdapter

            adapter = CLIAdapter(self.cfg)
            return adapter
        return EchoAdapter(self.cfg, self.book)

    async def run_forever(self) -> None:
        import websockets

        delay = self.cfg.reconnect_min
        while True:
            try:
                await self._run_once()
                if self.cfg.producer:
                    return
                delay = self.cfg.reconnect_min
            except websockets.ConnectionClosedError as exc:
                if "replaced" in str(exc).lower():
                    delay = max(delay, 10.0)
                print(f"[sessionHelper] ws closed: {exc}; reconnect in {delay:.1f}s", flush=True)
            except Exception as exc:
                print(f"[sessionHelper] error: {exc}; reconnect in {delay:.1f}s", flush=True)
            await asyncio.sleep(delay)
            delay = min(self.cfg.reconnect_max, max(self.cfg.reconnect_min, delay * 2))

    def _debug_probe(self) -> None:
        """SH_DEBUG=1 时打详细连接诊断:DNS解析IP/TCP/TLS耗时,方便追踪连不上。"""
        import socket, ssl, time, logging
        from urllib.parse import urlparse
        logging.basicConfig(level=logging.DEBUG)
        logging.getLogger("websockets").setLevel(logging.DEBUG)
        u = urlparse(self.cfg.ws_url)
        host = u.hostname or ""
        port = u.port or (443 if u.scheme == "wss" else 80)
        try:
            infos = socket.getaddrinfo(host, port, proto=socket.IPPROTO_TCP)
            ips = sorted({i[4][0] for i in infos})
            print(f"[sessionHelper][debug] DNS {host} -> {ips} (port {port})", flush=True)
        except Exception as exc:
            print(f"[sessionHelper][debug] DNS 解析失败: {exc}", flush=True)
            return
        try:
            t0 = time.monotonic()
            sock = socket.create_connection((host, port), timeout=self.cfg.open_timeout)
            print(f"[sessionHelper][debug] TCP 连上 {sock.getpeername()} 耗时 {time.monotonic()-t0:.2f}s", flush=True)
            if u.scheme == "wss":
                t1 = time.monotonic()
                ctx = ssl.create_default_context()
                try:
                    ss = ctx.wrap_socket(sock, server_hostname=host)
                    print(f"[sessionHelper][debug] TLS 握手成功 {ss.version()} 耗时 {time.monotonic()-t1:.2f}s", flush=True)
                    ss.close()
                except Exception as exc:
                    print(f"[sessionHelper][debug] TLS 失败: {exc}", flush=True); sock.close()
            else:
                sock.close()
        except Exception as exc:
            print(f"[sessionHelper][debug] TCP 连接失败: {exc}", flush=True)

    async def _run_once(self) -> None:
        import websockets, time

        headers = {"Authorization": f"Bearer {self.cfg.secret}"}
        print(f"[sessionHelper] connect {self.cfg.ws_url} mode={self.cfg.mode}", flush=True)
        # 代理:off/none/direct=强制直连(清掉HTTPS_PROXY等环境变量);URL=用该代理;空=跟随环境变量
        import os as _os
        _p = self.cfg.proxy.strip().lower()
        if _p in ("off", "none", "false", "direct", "no", "0"):
            for _k in ("HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "WS_PROXY", "ws_proxy", "WSS_PROXY", "wss_proxy"):
                _os.environ.pop(_k, None)
            _proxy = None  # 环境已清,None=直连
        elif self.cfg.proxy.strip():
            _proxy = self.cfg.proxy.strip()
        else:
            _proxy = None
        if self.cfg.debug:
            hint = {False: "强制直连(忽略HTTPS_PROXY)", None: "跟随HTTPS_PROXY环境变量(若有)"}.get(_proxy, _proxy)
            print(f"[sessionHelper][debug] 代理: {hint}", flush=True)
            import os as _os
            print(f"[sessionHelper][debug] 环境HTTPS_PROXY={_os.environ.get('HTTPS_PROXY') or _os.environ.get('https_proxy') or '未设'}", flush=True)
            self._debug_probe()
        _t0 = time.monotonic()
        async with websockets.connect(
            self.cfg.ws_url,
            additional_headers=headers,
            ping_interval=20,
            ping_timeout=20,
            open_timeout=self.cfg.open_timeout,
            proxy=_proxy,
        ) as ws:
            if self.cfg.debug:
                print(f"[sessionHelper][debug] WS 握手成功,耗时 {time.monotonic()-_t0:.2f}s", flush=True)
            print(f"[sessionHelper] connected self={self.book.self_addr} version={__version__}", flush=True)
            if self.cfg.producer:
                recv_task = asyncio.create_task(self.recv_control_loop(ws))
                try:
                    await self.producer_stdin_loop(ws)
                    await asyncio.sleep(0.1)
                finally:
                    recv_task.cancel()
                    try:
                        await recv_task
                    except asyncio.CancelledError:
                        pass
            else:
                await asyncio.gather(
                    self.recv_loop(ws),
                    self.outbox_loop(ws),
                    self.mirror_loop(ws),
                )

    async def recv_loop(self, ws) -> None:
        async for raw in ws:
            try:
                env = json.loads(raw)
            except Exception:
                continue
            if is_mirror_control(env):
                self.apply_mirror_control(env)
                continue
            from_addr = str(env.get("from") or "")
            print(f"[recv] from={from_addr} to={env.get('to')} body={env.get('body')!r}", flush=True)
            if self.is_agent_network_skill(env):
                await self.inject_agent_network_skill(env)
                continue
            self.remember_broadcast_mirror_decision(env)
            try:
                reply_body = await asyncio.to_thread(self.adapter.handle, env)
            except Exception as exc:
                reply_body = f"处理失败：{exc}"
            if not reply_body or not is_feishu_addr(from_addr):
                continue
            to, meta = reply_target(env, self.book)
            body = self.reply_body(env, reply_body)
            await ws.send(json.dumps(envelope(to, body, self.book.self_addr, meta), ensure_ascii=False))
            print(f"[reply] to={to} meta={meta}", flush=True)

    def reply_body(self, env: dict, reply_body: str) -> str:
        prefix = str((env.get("meta") or {}).get("reply_prefix") or f"【{self.cfg.session_name}】")
        return f"{prefix}{reply_body}"

    def is_agent_network_skill(self, env: dict) -> bool:
        return (env.get("meta") or {}).get("type") == "agent_network_skill"

    async def inject_agent_network_skill(self, env: dict) -> None:
        body = str(env.get("body") or "")
        if not body:
            return
        if self.cfg.mode == "cli" and hasattr(self.adapter, "start") and hasattr(self.adapter, "inject_text"):
            started = await asyncio.to_thread(self.adapter.start)
            if started:
                await asyncio.to_thread(self.adapter.inject_text, body)
                print("[agent_skill] injected", flush=True)
            return
        try:
            await asyncio.to_thread(self.adapter.handle, env)
        except Exception as exc:
            print(f"[agent_skill] inject failed: {exc}", flush=True)

    def apply_mirror_control(self, env: dict) -> None:
        meta = env.get("meta") or {}
        self.mirror.enabled = bool(meta.get("enabled"))
        if "mirror_to" in meta:
            self.mirror.to = str(meta.get("mirror_to") or "")
        print(f"[mirror_control] enabled={self.mirror.enabled} to={self.mirror.to}", flush=True)

    def remember_broadcast_mirror_decision(self, env: dict) -> None:
        if not is_broadcast_envelope(env):
            return
        meta = env.get("meta") or {}
        if truthy_meta(meta.get("mirror_primary")):
            return
        body = str(env.get("body") or "").strip()
        if body:
            self.non_primary_broadcast_texts.append(body)

    def should_skip_mirror_event(self, role: str, text: str) -> bool:
        if role != "user":
            return False
        candidate = normalized_text(text)
        kept: deque[str] = deque(maxlen=32)
        while self.non_primary_broadcast_texts:
            body = self.non_primary_broadcast_texts.popleft()
            body_norm = normalized_text(body)
            if body_norm and (body_norm == candidate or body_norm in candidate or candidate in body_norm):
                kept.extend(self.non_primary_broadcast_texts)
                self.non_primary_broadcast_texts = kept
                return True
            kept.append(body)
        self.non_primary_broadcast_texts = kept
        return False

    def should_skip_mirror_text(self, text: str) -> bool:
        return self.should_skip_mirror_event("user", text)

    async def recv_control_loop(self, ws) -> None:
        async for raw in ws:
            try:
                env = json.loads(raw)
            except Exception:
                continue
            if is_mirror_control(env):
                self.apply_mirror_control(env)
                continue
            print(f"[producer][recv] ignored system/session msg type={(env.get('meta') or {}).get('type')}", flush=True)

    def producer_envelope(self, body: str, role: str = "producer") -> dict:
        if not self.cfg.target_group:
            raise RuntimeError("SH_TARGET_GROUP is required for producer output")
        meta = {
            "producer": True,
            "target_group": self.cfg.target_group,
            "role": role,
            "no_mirror": True,
        }
        if self.cfg.target_bot:
            meta["target_bot"] = self.cfg.target_bot
        return envelope(self.book.feishu_addr(self.cfg.target_group), body, self.book.self_addr, meta)

    async def producer_stdin_loop(self, ws) -> None:
        if not self.cfg.target_group:
            raise RuntimeError("SH_TARGET_GROUP is required when SH_PRODUCER=1")
        print(f"[producer] stdin -> group {self.cfg.target_group}", flush=True)
        while True:
            line = await asyncio.to_thread(sys.stdin.readline)
            if line == "":
                print("[producer] stdin EOF; exiting", flush=True)
                return
            body = line.strip()
            if not body:
                continue
            await ws.send(json.dumps(self.producer_envelope(body), ensure_ascii=False))
            print(f"[producer] sent target_group={self.cfg.target_group}", flush=True)

    async def outbox_loop(self, ws) -> None:
        if not self.cfg.outbox:
            await asyncio.Future()
        open(self.cfg.outbox, "a", encoding="utf-8").close()
        pos = os.path.getsize(self.cfg.outbox)
        while True:
            await asyncio.sleep(1.0)
            try:
                with open(self.cfg.outbox, encoding="utf-8") as f:
                    f.seek(pos)
                    new = f.read()
                    pos = f.tell()
            except OSError:
                continue
            for line in new.splitlines():
                if "|" not in line:
                    continue
                to, body = line.split("|", 1)
                await ws.send(json.dumps(envelope(to.strip(), body.strip(), self.book.self_addr), ensure_ascii=False))

    async def mirror_loop(self, ws) -> None:
        while True:
            await asyncio.sleep(0.2)
            if not self.cfg.collect and (not self.mirror.enabled or not self.mirror.to):
                continue
            event = None
            if hasattr(self.adapter, "start"):
                await asyncio.to_thread(self.adapter.start)
            if hasattr(self.adapter, "next_mirror_event"):
                event = await asyncio.to_thread(self.adapter.next_mirror_event, 0.5)
            if not event:
                await asyncio.sleep(0.8)
                continue
            role, text = event
            if role != "user" and contains_comm_skill_ack(text):
                await ws.send(json.dumps(comm_skill_ack_envelope(self.book), ensure_ascii=False))
                print("[agent_skill] ack sent", flush=True)
                continue
            if self.cfg.agent_route and role != "user":
                routed = agent_route_envelope(text, self.book)
                if routed is not None:
                    await ws.send(json.dumps(routed, ensure_ascii=False))
                    print(f"[agent_route] to={routed['to']}", flush=True)
                    continue
            if self.cfg.collect:
                meta = {"type": "collect", "role": role, "session": self.cfg.session_name, "key_id": self.cfg.key_id}
                await ws.send(
                    json.dumps(envelope(session_addr("workpulse", self.cfg.key_id), text, self.book.self_addr, meta), ensure_ascii=False)
                )
                print(f"[collect] role={role}", flush=True)
            if self.mirror.enabled and self.mirror.to:
                if self.should_skip_mirror_event(role, text):
                    print(f"[mirror] skipped non-primary broadcast role={role}", flush=True)
                else:
                    body = f"【{self.cfg.session_name}·{role}】{text}"
                    await ws.send(json.dumps(envelope(self.mirror.to, body, self.book.self_addr), ensure_ascii=False))
                    print(f"[mirror] to={self.mirror.to} role={role}", flush=True)
            if self.cfg.target_group and role != "user":
                body = f"【{self.cfg.session_name}·{role}】{text}"
                await ws.send(json.dumps(self.producer_envelope(body, role), ensure_ascii=False))
                print(f"[producer] target_group={self.cfg.target_group} role={role}", flush=True)


def main() -> None:
    cfg = load_config()
    helper = SessionHelper(cfg)
    try:
        asyncio.run(helper.run_forever())
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
