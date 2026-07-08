"""sessionHelper runtime."""

from __future__ import annotations

import asyncio
import json
import os
import re
import sys
import time
from collections import deque
from dataclasses import dataclass
from typing import Protocol

from . import __version__
from .config import Config, load_config
from .llm import LLMProvider
from .protocol import AddressBook, envelope, is_feishu_addr, is_mirror_control, reply_target, session_addr
from .provision import Provisioner, is_provision_envelope


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


def is_no_mirror_envelope(env: dict) -> bool:
    meta = env.get("meta") or {}
    return truthy_meta(meta.get("no_mirror")) or truthy_meta(meta.get("system"))


def is_online_directory_text(text: str) -> bool:
    text = str(text or "")
    return ("【DingWei在线清单】" in text or "【DingWei 在线清单】" in text) and "**********" in text


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
        self.comm_skill_installed = False
        self.last_online_list = ""
        self.pending_inbound: deque[dict] = deque(maxlen=max(1, cfg.busy_buffer_max))
        self.busy_acked_from: set[str] = set()
        self.adapter = self._build_adapter()
        self.provisioner = Provisioner(cfg)
        self.provisioner.rollback_stale_update_if_needed()
        self.install_send_script()

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
            self.provisioner.confirm_update_connected()
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
                hb = asyncio.create_task(self.online_list_heartbeat_loop())
                try:
                    await asyncio.gather(
                        self.recv_loop(ws),
                        self.pending_drain_loop(ws),
                        self.terminal_loop(ws),
                        self.outbox_loop(ws),
                        self.mirror_loop(ws),
                    )
                finally:
                    hb.cancel()
                    try:
                        await hb
                    except asyncio.CancelledError:
                        pass

    async def recv_loop(self, ws) -> None:
        async for raw in ws:
            try:
                env = json.loads(raw)
            except Exception:
                continue
            if self.is_terminal_input(env):
                await self.handle_terminal_input(env)
                continue
            if self.is_terminal_resize(env):
                await self.handle_terminal_resize(env)
                continue
            if is_mirror_control(env):
                self.apply_mirror_control(env)
                continue
            if is_provision_envelope(env):
                await self.handle_provision(ws, env)
                continue
            from_addr = str(env.get("from") or "")
            print(f"[recv] from={from_addr} to={env.get('to')} body={env.get('body')!r}", flush=True)
            if self.is_agent_network_skill(env):
                if self.comm_skill_installed:
                    await ws.send(json.dumps(comm_skill_ack_envelope(self.book), ensure_ascii=False))
                    print("[agent_skill] already installed; ack sent", flush=True)
                    continue
                injected = await self.inject_agent_network_skill(env)
                if injected:
                    # 注入成功即自动回执（确认“已送达”），不再依赖模型乖乖回标记——
                    # 否则 deepseek 等不老实的模型不回标记，服务端会每2分钟重推、刷屏
                    self.comm_skill_installed = True
                    await ws.send(json.dumps(comm_skill_ack_envelope(self.book), ensure_ascii=False))
                    print("[agent_skill] injected + auto-acked", flush=True)
                continue
            body = str(env.get("body") or "")
            if is_online_directory_text(body):
                self.write_online_list(body)  # 始终写最新到会话专属共享文件，供CLI按需读取（不注入对话、不刷屏）
                continue
            if is_no_mirror_envelope(env):
                continue
            self.remember_broadcast_mirror_decision(env)
            if await self.buffer_if_busy(ws, env):
                continue
            await self.process_inbound_message(ws, env)

    async def handle_provision(self, ws, env: dict) -> None:
        result = await asyncio.to_thread(self.provisioner.handle, env)
        ack = envelope(
            session_addr("workpulse", self.cfg.key_id),
            json.dumps(result.to_meta(), ensure_ascii=False),
            self.book.self_addr,
            result.to_meta(),
        )
        await ws.send(json.dumps(ack, ensure_ascii=False))
        print(f"[provision] action={result.action} target={result.target} ok={result.ok} message={result.message}", flush=True)

    async def buffer_if_busy(self, ws, env: dict) -> bool:
        if self.cfg.mode != "cli" or not hasattr(self.adapter, "is_idle"):
            return False
        try:
            idle = bool(await asyncio.to_thread(self.adapter.is_idle))
        except Exception:
            idle = True
        if idle:
            return False
        if len(self.pending_inbound) >= self.pending_inbound.maxlen:
            dropped = self.pending_inbound.popleft()
            print(f"[busy_buffer] drop oldest from={dropped.get('from')}", flush=True)
        self.pending_inbound.append(env)
        from_addr = str(env.get("from") or "")
        if from_addr not in self.busy_acked_from:
            self.busy_acked_from.add(from_addr)
            await self.send_busy_ack(ws, env)
        print(f"[busy_buffer] queued size={len(self.pending_inbound)} from={from_addr}", flush=True)
        return True

    async def send_busy_ack(self, ws, env: dict) -> None:
        to, meta = reply_target(env, self.book)
        if not to:
            return
        meta = dict(meta)
        meta["no_mirror"] = True
        meta["system"] = True
        await ws.send(json.dumps(envelope(to, self.cfg.busy_reply_text, self.book.self_addr, meta), ensure_ascii=False))

    async def pending_drain_loop(self, ws) -> None:
        while True:
            await asyncio.sleep(0.2)
            if not self.pending_inbound or self.cfg.mode != "cli" or not hasattr(self.adapter, "is_idle"):
                continue
            try:
                idle = bool(await asyncio.to_thread(self.adapter.is_idle))
            except Exception:
                idle = True
            if not idle:
                continue
            env = self.pending_inbound.popleft()
            print(f"[busy_buffer] draining size={len(self.pending_inbound)} from={env.get('from')}", flush=True)
            await self.process_inbound_message(ws, env)
            if not self.pending_inbound:
                self.busy_acked_from.clear()
            await asyncio.sleep(max(0.1, min(self.cfg.cli_settle_seconds, 1.0)))

    async def process_inbound_message(self, ws, env: dict) -> None:
        from_addr = str(env.get("from") or "")
        try:
            reply_body = await asyncio.to_thread(self.adapter.handle, env)
        except Exception as exc:
            reply_body = f"处理失败：{exc}"
        if not reply_body or not is_feishu_addr(from_addr):
            return
        to, meta = reply_target(env, self.book)
        body = self.reply_body(env, reply_body)
        await ws.send(json.dumps(envelope(to, body, self.book.self_addr, meta), ensure_ascii=False))
        print(f"[reply] to={to} meta={meta}", flush=True)

    def reply_body(self, env: dict, reply_body: str) -> str:
        prefix = str((env.get("meta") or {}).get("reply_prefix") or f"【{self.cfg.session_name}】")
        return f"{prefix}{reply_body}"

    def is_agent_network_skill(self, env: dict) -> bool:
        return (env.get("meta") or {}).get("type") == "agent_network_skill"

    def is_terminal_input(self, env: dict) -> bool:
        return (env.get("meta") or {}).get("type") == "terminal_input"

    async def handle_terminal_input(self, env: dict) -> None:
        data = str(env.get("body") or "")
        if not data or self.cfg.mode != "cli" or not hasattr(self.adapter, "write_terminal_input"):
            return
        await asyncio.to_thread(self.adapter.write_terminal_input, data)

    def is_terminal_resize(self, env: dict) -> bool:
        return (env.get("meta") or {}).get("type") == "terminal_resize"

    async def handle_terminal_resize(self, env: dict) -> None:
        meta = env.get("meta") or {}
        try:
            cols = int(meta.get("cols") or 0)
            rows = int(meta.get("rows") or 0)
        except (TypeError, ValueError):
            return
        if cols <= 0 or rows <= 0 or self.cfg.mode != "cli" or not hasattr(self.adapter, "set_winsize"):
            return
        await asyncio.to_thread(self.adapter.set_winsize, rows, cols)

    async def inject_agent_network_skill(self, env: dict) -> bool:
        body = str(env.get("body") or "")
        if not body:
            return False
        if self.cfg.mode == "cli" and hasattr(self.adapter, "start") and hasattr(self.adapter, "inject_text"):
            started = await asyncio.to_thread(self.adapter.start)
            if started:
                await asyncio.to_thread(self.adapter.inject_text, body)
                print("[agent_skill] injected", flush=True)
                return True
            return False  # CLI 未就绪，先不回执，下次重推时再试
        try:
            await asyncio.to_thread(self.adapter.handle, env)
            return True
        except Exception as exc:
            print(f"[agent_skill] inject failed: {exc}", flush=True)
            return False

    def install_send_script(self) -> None:
        """把统一发信脚本装到 ~/.dingwei/send.py，供 CLI 里的 AI 调用（跨会话可靠发信/回信）。"""
        try:
            src = os.path.join(os.path.dirname(os.path.abspath(__file__)), "send_dingwei.py")
            if not os.path.exists(src):
                return
            dst_dir = os.path.expanduser(os.path.join("~", ".dingwei"))
            os.makedirs(dst_dir, exist_ok=True)
            dst = os.path.join(dst_dir, "send.py")
            with open(src, encoding="utf-8") as f:
                content = f.read()
            with open(dst, "w", encoding="utf-8") as f:
                f.write(content)
            os.chmod(dst, 0o755)
        except Exception as exc:
            print(f"[send] install failed: {exc}", flush=True)

    def online_list_path(self) -> str:
        """会话专属的在线清单共享文件路径（会话名前缀，区隔同机多会话）。"""
        override = os.environ.get("SH_ONLINE_LIST_PATH", "").strip()
        if override:
            return override
        base = os.path.expanduser(os.path.join("~", ".dingwei"))
        return os.path.join(base, f"{self.cfg.session_name}.DingWeiOnlineSessions.list")

    def write_online_list(self, body: str) -> None:
        """把最新在线清单原子写入共享文件，供 CLI（经 skill）按需读取。始终最新、不进对话。
        body 非空=更新清单内容；空=仅刷新时间戳（心跳）。文件头带 UTC 更新时间，供 AI 判新鲜度。"""
        if body:
            self.last_online_list = str(body)
        try:
            ts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            header = (
                f"# 更新于 {ts} — sessionHelper 每分钟刷新一次。\n"
                "# 若此时间距当前已超过 2 分钟，说明本机 sessionHelper 可能异常或已断连，\n"
                "# 本清单不可信，请勿据此判断谁在线。\n"
            )
            path = self.online_list_path()
            os.makedirs(os.path.dirname(path), exist_ok=True)
            tmp = f"{path}.tmp"
            with open(tmp, "w", encoding="utf-8") as f:
                f.write(header + (self.last_online_list or ""))
            os.replace(tmp, path)
        except Exception as exc:
            print(f"[roster] write list failed: {exc}", flush=True)

    async def online_list_heartbeat_loop(self) -> None:
        """心跳：连接存续期间每 60s 刷新清单文件时间戳。由连接生命周期管理（见 recv 分支的
        create_task/cancel）——进程死或断连时本 loop 停止 → 文件时间变旧 → AI 据 2 分钟判据识别异常。"""
        while True:
            await asyncio.to_thread(self.write_online_list, "")
            await asyncio.sleep(60)

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
                self.comm_skill_installed = True
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

    async def terminal_loop(self, ws) -> None:
        if self.cfg.mode != "cli" or not hasattr(self.adapter, "next_terminal_chunk"):
            await asyncio.Future()
        if hasattr(self.adapter, "start"):
            # 后台启动 CLI（spawn 后台跑，不等“就绪”），立即进入转发循环——
            # 网页终端从而能实时看到 CLI 启动全过程（含首次 onboarding），可交互过去
            asyncio.create_task(asyncio.to_thread(self.adapter.start))
        while True:
            chunk = await asyncio.to_thread(self.adapter.next_terminal_chunk, 0.5)
            if not chunk:
                continue
            deadline = asyncio.get_running_loop().time() + 0.12
            chunks = [chunk]
            while asyncio.get_running_loop().time() < deadline:
                more = await asyncio.to_thread(self.adapter.next_terminal_chunk, 0.01)
                if not more:
                    break
                chunks.append(more)
            body = "".join(chunks)
            meta = {"type": "terminal_output", "session": self.cfg.session_name, "key_id": self.cfg.key_id, "no_mirror": True}
            await ws.send(json.dumps(envelope(session_addr("workpulse", self.cfg.key_id), body, self.book.self_addr, meta), ensure_ascii=False))


def main() -> None:
    cfg = load_config()
    helper = SessionHelper(cfg)
    try:
        asyncio.run(helper.run_forever())
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
