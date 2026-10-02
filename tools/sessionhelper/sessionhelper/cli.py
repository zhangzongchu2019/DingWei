"""PTY-owned AI CLI adapter for SH_MODE=cli."""

from __future__ import annotations

import os
import queue
import re
import shlex
import signal
import sqlite3
import subprocess
import threading
import time
from collections import deque
from dataclasses import dataclass
from datetime import datetime, timezone
import json
from pathlib import Path

import pexpect

from .config import Config, env_get


ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
# CLI 退出时日志里的屏幕尾巴要脱敏：像密钥／令牌的长串一律遮掉（组长 R-B12-CODEX-PKG 第 2 条）。
_SECRET_RE = re.compile(r"(?i)(sk-[A-Za-z0-9_-]{8,}|[A-Za-z0-9_\-]{32,})")
# 认证方案后的令牌（Authorization: Bearer xxx／Basic xxx）：先于键值规则遮，否则键值规则把方案词当成值、令牌外露
# （复验 tosonnet55tc2k 对 b2c91a8a；组长 WORKGROUPS 第十节第 3 条）
_AUTH_SCHEME_RE = re.compile(r"(?i)\b(bearer|basic)(\s+)[^\s\"',;]+")
_SECRET_KEY_NAME = r"[A-Za-z0-9_.-]*(?:key|token|secret|passwd|password|credential|auth)[A-Za-z0-9_.-]*"
# 命令行旗标：--api-key xxx／--api-key=xxx
_SECRET_FLAG_RE = re.compile(r"(?i)(--?" + _SECRET_KEY_NAME + r")(=|\s+)(?!-)(\"[^\"]*\"|'[^']*'|\S+)")
# 「键名=值」「键名: 值」及 JSON「"键名": "值"」形态：键名里带 key／token／secret／password／credential／auth 的，值一律遮
# （短串也遮；复验 tosonnet55tc2k）。值若以认证方案词开头（已由 _AUTH_SCHEME_RE 遮过令牌）则不再动。
_SECRET_KV_RE = re.compile(
    r"(?i)\b(" + _SECRET_KEY_NAME + r"\"?)(\s*[:=]\s*)(?!(?:bearer|basic)\s)(\"[^\"]*\"|'[^']*'|\S+)")
EXIT_TAIL_LINES = 6


class CLIDeliveryFailed(RuntimeError):
    """持久投递在 CLI 侧失败：delivery worker 据 reason／retryable 回 failed ACK（不再当正常结果回 processed）。"""

    reason = "cli_injection_failed"
    retryable = True


class CLIUnavailable(CLIDeliveryFailed):
    """CLI 子进程起不来或已退出，正文没写进 CLI：可重试（组长 R-B12-CODEX-PKG 第 2 条）。"""

    reason = "cli_unavailable"
    retryable = True


class CLIInjectionNotReceived(CLIDeliveryFailed):
    """正文已写进 CLI，但会话文件里始终没出现对应 user 轮：不可重试——重投会把同一任务再敲进 CLI 一遍。"""

    reason = "cli_injection_not_received"
    retryable = False


def _squash(text: str) -> str:
    """去空白、转小写再比：全屏 TUI 常用光标定位代替空格（抓屏里「Provide your own API key」读出来是 ProvideyourownAPIkey）。"""
    return re.sub(r"\s+", "", text).lower()


def redact_tail(lines) -> str:
    out = []
    for ln in lines:
        ln = _AUTH_SCHEME_RE.sub(lambda m: m.group(1) + m.group(2) + "<redacted>", ln)
        ln = _SECRET_FLAG_RE.sub(lambda m: m.group(1) + m.group(2) + "<redacted>", ln)
        ln = _SECRET_KV_RE.sub(lambda m: m.group(1) + m.group(2) + "<redacted>", ln)
        ln = _SECRET_RE.sub("<redacted>", ln)
        out.append(ln[:200])
    return " | ".join(out)
CTRL_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")
BRACKETED_PASTE_START = "\x1b[200~"
BRACKETED_PASTE_END = "\x1b[201~"
BRACKETED_PASTE_CLIS = {"claude", "codex", "aider", "opencode"}


@dataclass(frozen=True)
class CLIProfile:
    command: list[str]
    ready_patterns: tuple[str, ...]
    output_source: str = "pty"
    startup_grace: float = 1.5
    launch: str = "pty"
    # 拦路界面标记：启动期（首次就绪之前）输出里一出现即撤销就绪、启动立即失败（登录选择、包缺失之类，界面上可能同时带着提示符）。
    block_patterns: tuple[str, ...] = ()


CLI_PROFILES: dict[str, CLIProfile] = {
    "claude": CLIProfile(["claude", "--dangerously-skip-permissions"], ("claude", ">", "❯", "╭", "Welcome"), "transcript"),
    # codex 只认输入提示符，不认「codex」「Welcome」这类横幅字样（组长 R-B12-CODEX-PKG 修订第 4 条：B12 里横幅
    # 「>_ OpenAI Codex (v0.159.2)」把一个随即退出的进程判成了就绪）。横幅本身以「>_」开头，故「>」也不能当标记。
    # 0.159.2 的实际提示符以 prodops 抓屏为准，不符时用 SH_CLI_READY_PATTERNS 覆盖。
    # 拦路界面（组长 R-B12-CODEX-PKG 第 4 条补，样本＝prodops 00e5020e 抓屏）：0.159.2 的登录选择菜单下方同时画着
    # 「› Ask Codex to do anything?」输入框，只看提示符会误判就绪。
    "codex": CLIProfile(["codex"], ("›", "❯"), "transcript", block_patterns=(
        "Sign in with ChatGPT", "Provide your own API key", "Sign in with Device Code",
        "Press enter to continue", "Do you trust", "Trust this folder", "Update available",
        "Detected OPENAI_API_KEY", "no complete local package")),
    "aider": CLIProfile(["aider"], ("aider", ">", "Tokens")),
    "opencode": CLIProfile(["opencode"], ("opencode", ">", "❯", "Welcome"), "opencode_db"),
    "cline": CLIProfile(["cline"], ("cline", ">", "Welcome")),
    "gemini": CLIProfile(["gemini"], ("gemini", ">", "Welcome")),
}


def clean_output(text: str) -> str:
    text = ANSI_RE.sub("", text)
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    text = CTRL_RE.sub("", text)
    lines = [ln.rstrip() for ln in text.splitlines()]
    return "\n".join(ln for ln in lines if ln.strip()).strip()


def is_multiline(text: str) -> bool:
    return "\n" in text or "\r" in text


def flatten_multiline(text: str) -> str:
    return " / ".join(part.strip() for part in text.splitlines() if part.strip())


class CLIAdapter:
    name = "cli"

    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.cli_name = cfg.cli_name
        self.profile = CLI_PROFILES.get(cfg.cli_name, CLI_PROFILES["claude"])
        self.ready_patterns = cfg.cli_ready_patterns or self.profile.ready_patterns
        self.block_patterns = self.profile.block_patterns + cfg.cli_block_patterns
        self.blocked_by = ""  # 当前停在的拦路界面（未停则空）
        # 启动期已过（本子进程首次「出现提示符且静默」）：此后正文里出现拦路字样不再撤销就绪（组长 WORKGROUPS 第十节第 1 条：
        # 回答里一句 Sign in with ChatGPT 曾把健康座席判死）。新子进程（start_pty／_respawn_child）重置。
        self.startup_done = False
        self.command = cfg.cli_cmd or self.profile.command
        self.cwd = cfg.cli_cwd or os.getcwd()
        self.launch_mode = cfg.cli_launch or self.profile.launch
        self.tmux_session = tmux_session_name(cfg.session_name, cfg.key_id)
        self.child: pexpect.spawn | None = None
        self.ready = threading.Event()
        self.closed = threading.Event()
        self.stopping = threading.Event()
        self.respawn_count = 0
        self.win_rows = 40
        self.win_cols = 140
        self._start_lock = threading.Lock()
        self.output_q: queue.Queue[str] = queue.Queue()
        self.terminal_q: queue.Queue[str] = queue.Queue()
        self.mirror_q: queue.Queue[tuple[str, str]] = queue.Queue()
        self.reader: threading.Thread | None = None
        self.write_lock = threading.Lock()
        self.web_typing_until = 0.0
        self.started_at = time.time()
        self.last_output_at = 0.0
        self.transcript: TranscriptTailer | None = None
        self.launch_attempts = 0
        self.last_launch_error = ""
        self.output_tail: deque[str] = deque(maxlen=EXIT_TAIL_LINES)  # 最近几行屏幕输出（退出时打日志用）
        self.last_exit = ""  # 最近一次子进程退出的「code=… tail=…」
        # 启动期就退出的退避：连续退出 n 次后 min(2^n, 60) 秒内不再重起（mirror／终端循环会反复调 start()，
        # 不退避就是每两三秒起一次、刷一屏日志）。成功就绪即清零。
        self.exit_streak = 0
        self.start_backoff_until = 0.0

    def _child_gone(self) -> bool:
        """PTY 模式下子进程已退出（tmux 模式不在此判）。"""
        return self.launch_mode != "tmux" and self.child is not None and not self.child.isalive()

    def start(self) -> bool:
        # 就绪标志只在子进程还活着时才算数：原先 ready 一经置位就直接返回真，子进程早退后仍被当成可注入
        # （B12：Codex 打一屏就退出，三次注入写进死进程、142 秒后才以「注入未被接收」收场，全程无日志）。
        if self.ready.is_set() and self._child_gone():
            self._note_exit()
        if self.ready.is_set():
            return True
        # 快路径：子进程已在跑就直接算成功（不抢锁，避免被 ready 等待长期占锁而阻塞其它调用）
        if self.launch_mode != "tmux" and self.child is not None and self.child.isalive():
            return True
        with self._start_lock:
            if self.ready.is_set() and self._child_gone():
                self._note_exit()
            if self.ready.is_set():
                return True
            if self.launch_mode != "tmux" and self.child is not None and self.child.isalive():
                return True
            if time.time() < self.start_backoff_until:
                return False  # 退避中：不重起、不刷日志；原因仍在 last_launch_error
            self.launch_attempts += 1
            try:
                if self.launch_mode == "tmux":
                    self.start_tmux()
                else:
                    self.start_pty()
            except Exception as exc:
                self.last_launch_error = str(exc)
                print(f"[cli] start failed (attempt {self.launch_attempts}): {exc}", flush=True)
                # PTY 模式：子进程可能已起来只是没到 ready，别清空——留给终端转发用
                if self.launch_mode == "tmux" or self.child is None or not self.child.isalive():
                    self.ready.clear()
                    if self._child_gone():
                        self.exit_streak += 1
                        self.start_backoff_until = time.time() + min(2 ** self.exit_streak, 60)
                    return False
                print("[cli] child is alive though not 'ready'; keeping it for terminal streaming", flush=True)
                return True
            if self.ready.is_set():
                self.last_launch_error = ""
                self.exit_streak = 0
                return True
            return self.launch_mode != "tmux" and self.child is not None and self.child.isalive()

    def start_pty(self) -> None:
        if self.child is not None:
            if self.ready.is_set() or self.child.isalive():
                return
            self.child = None
        self.started_at = time.time()
        if not self.stopping.is_set():
            self.closed.clear()
        self.output_tail.clear()
        self.blocked_by = ""
        self.startup_done = False
        self.child = pexpect.spawn(
            self.command[0],
            self.command[1:],
            cwd=self.cwd,
            encoding="utf-8",
            codec_errors="replace",
            echo=False,
            timeout=0.2,
            dimensions=(self.win_rows, self.win_cols),
        )
        self.reader = threading.Thread(target=self._read_loop, name="sessionhelper-cli-reader", daemon=True)
        self.reader.start()
        deadline = time.time() + self.cfg.cli_ready_timeout
        while time.time() < deadline:
            if self._child_gone():
                # 等就绪期间就退出：不再等满 cli_ready_timeout，立刻带退出码与屏幕尾巴报出
                self._note_exit()
                raise RuntimeError(f"CLI exited during startup: {self.last_exit}")
            if self.blocked_by:
                # 启动期遇拦路界面：立即失败返回，不等满就绪超时（helper 自己越不过这些屏）
                raise RuntimeError(f"CLI blocked at screen: {self.blocked_by!r}")
            if self.ready.is_set() and self.is_idle():
                return
            time.sleep(0.1)
        raise RuntimeError(f"CLI did not become ready: {shlex.join(self.command)}")

    def set_winsize(self, rows: int, cols: int) -> None:
        """按浏览器 xterm 实际尺寸调整 PTY 窗口大小，CLI 才会铺满整块显示区。"""
        if rows > 0:
            self.win_rows = rows
        if cols > 0:
            self.win_cols = cols
        child = self.child
        if child is None:
            return
        try:
            child.setwinsize(self.win_rows, self.win_cols)
        except Exception:
            pass

    def start_tmux(self) -> None:
        if self.ready.is_set():
            return
        self.started_at = time.time()
        self.last_output_at = 0.0
        subprocess.run(["tmux", "kill-session", "-t", self.tmux_session], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        cmd = ["tmux", "new-session", "-d", "-s", self.tmux_session, "-c", self.cwd, shlex.join(self.command)]
        subprocess.run(cmd, check=True, timeout=30)
        deadline = time.time() + self.cfg.cli_ready_timeout
        last_pane = ""
        last_change = time.time()
        time.sleep(self.profile.startup_grace)
        while time.time() < deadline:
            pane = self.capture_tmux()
            cleaned = clean_output(pane)
            if cleaned != last_pane:
                last_pane = cleaned
                last_change = time.time()
                self.last_output_at = last_change
            has_prompt = any(pat.lower() in cleaned.lower() for pat in self.ready_patterns)
            has_transcript = self.profile.output_source == "transcript" and has_fresh_transcript(self.cli_name, self.cwd, self.started_at, self.cfg.cli_transcript)
            idle = time.time() - last_change >= self.cfg.cli_settle_seconds
            if (has_prompt and idle) or has_transcript:
                self.ready.set()
                return
            time.sleep(0.25)
        raise RuntimeError(f"CLI did not become ready in tmux {self.tmux_session}: {shlex.join(self.command)}")

    def is_idle(self) -> bool:
        # 子进程已退出：不算「忙」——让投递走到 handle_delivery 以失败上报，而不是永远回 busy
        if self._child_gone():
            return True
        if self.last_output_at <= 0:
            return False
        return time.time() - self.last_output_at >= self.cfg.cli_settle_seconds

    def stop(self) -> None:
        self.stopping.set()
        self.closed.set()
        if self.launch_mode == "tmux":
            subprocess.run(["tmux", "kill-session", "-t", self.tmux_session], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            return
        child = self.child
        if child is None:
            return
        try:
            child.kill(signal.SIGTERM)
        except Exception:
            pass

    def handle(self, env: dict) -> str:
        # 实时（非持久）路径：起不来就把原因作为回复文字给出（与原行为同）；持久投递走 handle_delivery，失败以异常上报。
        try:
            return self.handle_delivery(env, None)
        except CLIDeliveryFailed as exc:
            return str(exc)

    def handle_delivery(self, env: dict, on_injected=None) -> str:
        for attempt in range(max(1, self.cfg.cli_launch_retries)):
            if self.start():
                break
            if attempt + 1 < max(1, self.cfg.cli_launch_retries):
                time.sleep(1.0)
        else:
            raise self._delivery_failed(CLIUnavailable, self.not_ready_message())
        body = str(env.get("body") or "")
        if not self.ready.wait(0.1) or self._child_gone():
            if self._child_gone():
                self._note_exit()
            raise self._delivery_failed(CLIUnavailable, self.not_ready_message())
        if self.profile.output_source == "transcript" and self.transcript is None:
            self.transcript = locate_transcript(
                self.cli_name,
                self.cwd,
                self.started_at,
                1.0,
                self.cfg.cli_transcript,
                from_end=True,
            )
        if self.profile.output_source == "transcript":
            if self.cfg.async_reply:
                confirmer = getattr(self, "inject_and_confirm_transcript_user", None)
                if callable(confirmer):
                    confirmer(body)
                else:
                    self.inject_text(body)
                if on_injected is not None:
                    on_injected()
                return ""
            status, reply = self.inject_and_collect_transcript_reply(body, on_injected)
            if status == "ok" and reply:
                return reply
            if status == "no_user":
                # 失败不再当正常结果回 processed（B12：142 秒后以这句文字收场、被 control 压掉、日志里什么都没有）
                raise self._delivery_failed(CLIInjectionNotReceived, "（注入未被 CLI 接收：transcript 未出现对应 user 轮）")
            return "（答案超时：已确认 user 轮落盘，但未取到 assistant 回答）"
        if self.profile.output_source == "opencode_db":
            injected_at = time.time()
            self.inject_text(body)
            if on_injected is not None:
                on_injected()
            if self.cfg.async_reply:
                return ""
            status, reply = self.collect_opencode_db_reply(body, injected_at, self.cfg.cli_reply_wait)
            if status == "ok" and reply:
                return reply
            if status == "no_user":
                return "（注入未被 CLI 接收：opencode 会话库未出现对应 user 轮）"
            if status == "db_missing":
                return "（答案超时：未找到 opencode 会话库）"
            return "（答案超时：已确认 opencode user 轮落库，但未取到 assistant 回答）"
        self.discard_pending_output()
        self.inject_text(body)
        if on_injected is not None:
            on_injected()
        if self.cfg.async_reply:
            return ""
        chunks = self.collect_output(self.cfg.cli_reply_wait)
        if chunks:
            return "\n".join(chunks)
        return "已注入 CLI，等待模型输出。"

    def discard_pending_output(self) -> None:
        """Establish a reply boundary before writing a new PTY prompt."""
        while True:
            try:
                self.output_q.get_nowait()
            except queue.Empty:
                return

    def not_ready_message(self) -> str:
        reason = self.last_launch_error or (f"CLI 已退出：{self.last_exit}" if self.last_exit else "启动超时")
        if self.blocked_by:
            reason = f"CLI 停在拦路界面（{self.blocked_by}）；{reason}"
        return f"（CLI未就绪：{reason}；sessionHelper仍在线，稍后可重试）"

    @staticmethod
    def _delivery_failed(kind: type, message: str) -> CLIDeliveryFailed:
        """失败原因进 helper 日志（原先只在会被 control 抑制的回信里）。"""
        print(f"[cli] delivery failed reason={kind.reason} retryable={str(kind.retryable).lower()} detail={message}", flush=True)
        return kind(message)

    def _note_exit(self) -> None:
        """记一次子进程退出：打「[cli] exited code=… tail=…」并清就绪（同一子进程只记一次）。"""
        child = self.child
        if child is None or getattr(child, "_sh_exit_logged", False):
            self.ready.clear()
            return
        child._sh_exit_logged = True
        # 子进程退出得快时读线程可能还在 startup_grace 里没读过：把 PTY 里剩下的输出读干净再取尾巴
        for _ in range(64):
            try:
                raw = child.read_nonblocking(size=4096, timeout=0.05)
            except Exception:  # noqa: BLE001  (EOF／TIMEOUT／已关闭)
                break
            if not raw:
                break
            self.terminal_q.put(raw)
            for ln in clean_output(raw).splitlines():
                if ln.strip():
                    self.output_tail.append(ln.strip())
        try:
            child.isalive()  # 回收退出状态
        except Exception:  # noqa: BLE001
            pass
        code = child.exitstatus if child.exitstatus is not None else f"signal:{child.signalstatus}"
        self.last_exit = f"code={code} tail={redact_tail(self.output_tail)!r}"
        self.ready.clear()
        print(f"[cli] exited {self.last_exit}", flush=True)

    def inject_text(self, text: str) -> None:
        self.wait_for_web_idle()
        with self.write_lock:
            if self.launch_mode == "tmux":
                self.send_tmux_text(text)
                time.sleep(0.15)
                self.submit_enter()
                return
            assert self.child is not None
            # Full-screen TUIs can miss sendline during redraw. Submit in two steps:
            # write text, wait for the input widget to settle, then send CR alone.
            self.child.send(self.prepare_input_text(text))
            time.sleep(0.15)
            self.submit_enter()

    def submit_enter(self) -> None:
        if self.launch_mode == "tmux":
            subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "Enter"], check=False, timeout=5)
            return
        if self.child is not None:
            self.child.send("\r")

    def write_terminal_input(self, data: str) -> None:
        if not data:
            return
        if not self.start():
            return
        self.web_typing_until = time.time() + 0.8
        with self.write_lock:
            if self.launch_mode == "tmux":
                subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "-l", data], check=False, timeout=5)
                return
            if self.child is not None:
                self.child.send(data)

    def wait_for_web_idle(self) -> None:
        while True:
            delay = self.web_typing_until - time.time()
            if delay <= 0:
                return
            time.sleep(min(delay, 0.2))

    def next_terminal_chunk(self, timeout: float = 0.2) -> str | None:
        try:
            return self.terminal_q.get(timeout=timeout)
        except queue.Empty:
            return None

    def request_terminal_refresh(self) -> str:
        if not self.start():
            return ""
        if self.launch_mode == "tmux":
            return self.capture_tmux()
        with self.write_lock:
            if self.child is not None:
                self.child.send("\x0c")
        return ""

    def send_tmux_text(self, text: str) -> None:
        # send-keys -l treats the text literally, so punctuation and spaces are
        # not interpreted as tmux key names.
        prepared = self.prepare_input_text(text)
        if prepared.startswith(BRACKETED_PASTE_START) and prepared.endswith(BRACKETED_PASTE_END):
            for chunk in (BRACKETED_PASTE_START, text, BRACKETED_PASTE_END):
                subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "-l", chunk], check=True, timeout=5)
            return
        subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "-l", prepared], check=True, timeout=5)

    def prepare_input_text(self, text: str) -> str:
        if not is_multiline(text):
            return text
        if self.supports_bracketed_paste():
            return f"{BRACKETED_PASTE_START}{text}{BRACKETED_PASTE_END}"
        return flatten_multiline(text)

    def supports_bracketed_paste(self) -> bool:
        configured = (self.cfg.tool or self.cli_name or "").strip().lower()
        return configured in BRACKETED_PASTE_CLIS or self.cli_name in BRACKETED_PASTE_CLIS

    def inject_and_collect_transcript_reply(self, prompt: str, on_injected=None) -> tuple[str, str]:
        # Full-screen TUIs can report ready before the first input box is truly
        # able to accept send-keys. Validate the user turn in transcript; if it
        # does not land, resend the full prompt a few times instead of asking the
        # user to manually retry the first cold-start message.
        attempts = 3
        for attempt in range(attempts):
            self.inject_text(prompt)
            if on_injected is not None:
                on_injected()
            status, reply = self.collect_transcript_reply(prompt, self.cfg.cli_reply_wait)
            if status != "no_user":
                return status, reply
            if attempt + 1 < attempts:
                time.sleep(self.transcript_retry_delay(attempt))
        return "no_user", ""

    def transcript_retry_delay(self, attempt: int) -> float:
        return 3.0 + attempt

    def capture_tmux(self) -> str:
        try:
            return subprocess.check_output(
                ["tmux", "capture-pane", "-p", "-t", self.tmux_session],
                text=True,
                stderr=subprocess.DEVNULL,
                timeout=5,
            )
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
            return ""

    def collect_output(self, wait_seconds: float) -> list[str]:
        deadline = time.time() + max(0.1, wait_seconds)
        quiet_window = max(2.0, self.cfg.cli_settle_seconds)
        chunks: list[str] = []
        while time.time() < deadline:
            try:
                chunk = self.output_q.get(timeout=0.2)
            except queue.Empty:
                continue
            if chunk:
                chunks.append(chunk)
                # A prompt/redraw chunk can precede the actual reply. Keep the
                # transaction open until the PTY has been quiet long enough;
                # otherwise the next delivery can inherit this reply.
                if time.time() + quiet_window < deadline:
                    deadline = time.time() + quiet_window
        return chunks

    def collect_transcript_reply(self, prompt: str, wait_seconds: float) -> tuple[str, str]:
        if self.transcript is None:
            self.transcript = locate_transcript(
                self.cli_name,
                self.cwd,
                self.started_at,
                max(wait_seconds, self.cfg.cli_user_ack_wait),
                self.cfg.cli_transcript,
                from_end=False,
            )
        if self.transcript is None:
            return "no_user", ""
        saw_user = False
        for attempt in range(2):
            user_deadline = time.time() + max(0.1, self.cfg.cli_user_ack_wait)
            while time.time() < user_deadline:
                for role, text in self.transcript.read_events():
                    self.mirror_q.put((role, text))
                    if role == "user" and user_text_matches(prompt, text):
                        saw_user = True
                        continue
                    if saw_user and role in ("claude", "codex"):
                        return "ok", text
                if saw_user:
                    break
                time.sleep(0.25)
            if saw_user:
                break
            if attempt == 0:
                self.submit_enter()
        if not saw_user:
            return "no_user", ""
        answer_deadline = time.time() + max(0.1, wait_seconds)
        while time.time() < answer_deadline:
            for role, text in self.transcript.read_events():
                self.mirror_q.put((role, text))
                if role in ("claude", "codex"):
                    return "ok", text
            time.sleep(0.5)
        return "no_answer", ""

    def collect_opencode_db_reply(self, prompt: str, injected_at: float, wait_seconds: float) -> tuple[str, str]:
        reader = OpenCodeDBReader(self.cfg.opencode_db)
        if not reader.exists():
            return "db_missing", ""
        session_id = ""
        user_created = injected_at
        user_deadline = time.time() + max(0.1, min(self.cfg.cli_user_ack_wait, wait_seconds))
        while time.time() < user_deadline:
            user = reader.find_user_message(prompt, injected_at)
            if user is not None:
                session_id, user_created = user
                break
            time.sleep(0.25)
        if not session_id:
            return "no_user", ""
        answer_deadline = time.time() + max(0.1, wait_seconds)
        while time.time() < answer_deadline:
            reply = reader.find_finished_assistant_reply(session_id, user_created)
            if reply:
                self.mirror_q.put(("opencode", reply))
                return "ok", reply
            time.sleep(0.5)
        return "no_answer", ""

    def next_mirror_event(self, timeout: float = 0.5) -> tuple[str, str] | None:
        if self.profile.output_source == "transcript":
            if self.transcript is None:
                self.transcript = locate_transcript(
                    self.cli_name, self.cwd, self.started_at, timeout, self.cfg.cli_transcript
                )
            if self.transcript is not None:
                for event in self.transcript.read_events():
                    self.mirror_q.put(event)
        try:
            return self.mirror_q.get(timeout=timeout)
        except queue.Empty:
            return None

    def _respawn_child(self) -> bool:
        """PTY 模式下 CLI 子进程退出（如自动更新后要求重启、或崩溃）时自动重新拉起，
        新进程即用新版本，用户侧会话不断。带退避与上限，避免崩溃死循环。"""
        self.respawn_count += 1
        if self.respawn_count > 30:
            self.output_q.put("\r\n[sessionHelper] CLI 反复退出，已停止自动重启（请人工检查）\r\n")
            return False
        note = f"\r\n[sessionHelper] CLI 已退出（可能自动更新），自动重启 #{self.respawn_count} ...\r\n"
        self.output_q.put(note)
        self.terminal_q.put(note)
        time.sleep(min(2 * self.respawn_count, 8))
        if self.stopping.is_set():
            return False
        self.ready.clear()
        self.blocked_by = ""
        self.startup_done = False
        self.last_output_at = 0.0
        self.started_at = time.time()
        try:
            self.child = pexpect.spawn(
                self.command[0],
                self.command[1:],
                cwd=self.cwd,
                encoding="utf-8",
                codec_errors="replace",
                echo=False,
                timeout=0.2,
                dimensions=(self.win_rows, self.win_cols),
            )
        except Exception as exc:  # noqa: BLE001
            self.output_q.put(f"\r\n[sessionHelper] CLI 重启失败: {exc}\r\n")
            return False
        return True

    def _read_loop(self) -> None:
        assert self.child is not None
        buf = ""
        time.sleep(self.profile.startup_grace)
        while not self.closed.is_set():
            try:
                raw = self.child.read_nonblocking(size=4096, timeout=0.2)
            except pexpect.TIMEOUT:
                continue
            except pexpect.EOF:
                if self.stopping.is_set():
                    self.closed.set()
                    return
                # 子进程退出（任何原因）：先留痕再说（原先 transcript 档在此静默结束）
                self._note_exit()
                if self.profile.output_source != "pty":
                    return  # 本读线程结束；下次 start() 会重起子进程与读线程
                # PTY 模式下 CLI 子进程退出（多为自动更新/崩溃）→ 自动重生，拿到新版本、会话不断
                if not self._respawn_child():
                    self.closed.set()
                    return
                continue
            # 启动期在「出现提示符且已静默」时结束——与 start_pty 判启动成功同一判据，在新输出更新静默计时之前判。
            # 放在读线程里：PTY 档自动重生的子进程不经 start_pty，也靠这里结束各自的启动期。
            if not self.startup_done and self.ready.is_set() and self.is_idle():
                self.startup_done = True
            if raw:
                self.terminal_q.put(raw)
            cleaned = clean_output(raw)
            if not cleaned:
                continue
            for ln in cleaned.splitlines():
                if ln.strip():
                    self.output_tail.append(ln.strip())
            self.last_output_at = time.time()
            buf += "\n" + cleaned
            # 启动期（首次就绪之前）拦路界面优先：输出里出现登录选择等标记即撤销就绪并留痕；本子进程此后不再判就绪
            # （全屏 TUI 局部重绘可能单独送来一块只含「›」的输出，而菜单仍在屏上；helper 也无法自行越过登录）。
            # 新子进程才重置。启动期过后不再看拦路标记——那时屏上是模型回答，里面出现这些字样不说明 CLI 卡住。
            # 否则出现输入提示符才置就绪。「子进程还活着」由 start_pty（等静默期间退出即报）与 _note_exit（清就绪）保证。
            squashed = _squash(cleaned)
            blocker = "" if self.startup_done else next((pat for pat in self.block_patterns if _squash(pat) in squashed), "")
            if blocker:
                self.ready.clear()
                if not self.blocked_by:
                    self.blocked_by = blocker
                    print(f"[cli] blocked at screen: {blocker!r}", flush=True)
            elif not self.blocked_by and any(_squash(pat) in squashed for pat in self.ready_patterns):
                self.ready.set()
                self.respawn_count = 0
            if self.profile.output_source == "pty":
                self.output_q.put(cleaned)
                self.mirror_q.put((self.cli_name, cleaned))
            if len(buf) > 20000:
                buf = buf[-10000:]


class TranscriptTailer:
    def __init__(self, cli_name: str, path: Path, from_end: bool = True):
        self.cli_name = cli_name
        self.path = path
        self.pos = path.stat().st_size if from_end and path.exists() else 0

    def read_events(self) -> list[tuple[str, str]]:
        if not self.path.exists():
            return []
        try:
            with self.path.open(encoding="utf-8") as f:
                f.seek(self.pos)
                data = f.read()
                self.pos = f.tell()
        except OSError:
            return []
        events: list[tuple[str, str]] = []
        for line in data.splitlines():
            try:
                obj = json.loads(line)
            except Exception:
                continue
            event = parse_transcript_event(self.cli_name, obj)
            if event:
                events.append(event)
        return events


class OpenCodeDBReader:
    def __init__(self, override_path: str = ""):
        self.path = Path(override_path).expanduser() if override_path else Path.home() / ".local/share/opencode/opencode.db"

    def exists(self) -> bool:
        return self.path.exists()

    def connect(self) -> sqlite3.Connection:
        uri = f"file:{self.path}?mode=ro"
        return sqlite3.connect(uri, uri=True, timeout=1.0)

    def recent_messages(self, since: float, limit: int = 200) -> list[dict]:
        try:
            with self.connect() as conn:
                rows = conn.execute(
                    "SELECT id, session_id, time_created, data FROM message ORDER BY time_created DESC LIMIT ?",
                    (limit,),
                ).fetchall()
        except sqlite3.Error:
            return []
        messages: list[dict] = []
        for msg_id, session_id, created, data in rows:
            created_at = opencode_time(created)
            if created_at + 0.5 < since:
                continue
            try:
                payload = json.loads(data or "{}")
            except Exception:
                payload = {}
            messages.append(
                {
                    "id": str(msg_id),
                    "session_id": str(session_id),
                    "created_at": created_at,
                    "data": payload if isinstance(payload, dict) else {},
                }
            )
        messages.sort(key=lambda item: item["created_at"])
        return messages

    def find_user_message(self, prompt: str, since: float) -> tuple[str, float] | None:
        for msg in self.recent_messages(since):
            if msg["data"].get("role") != "user":
                continue
            text = self.message_text(msg["id"])
            if user_text_matches(prompt, text):
                return msg["session_id"], msg["created_at"]
        return None

    def find_finished_assistant_reply(self, session_id: str, since: float) -> str:
        candidates = [
            msg
            for msg in self.recent_messages(since)
            if msg["session_id"] == session_id
            and msg["data"].get("role") == "assistant"
            and bool(msg["data"].get("finish"))
        ]
        candidates.sort(key=lambda item: item["created_at"], reverse=True)
        for msg in candidates:
            text = strip_markdown_code_fence(self.message_text(msg["id"]))
            if text:
                return text
        return ""

    def message_text(self, message_id: str) -> str:
        try:
            with self.connect() as conn:
                rows = conn.execute(
                    "SELECT data FROM part WHERE message_id=? ORDER BY time_created, id",
                    (message_id,),
                ).fetchall()
        except sqlite3.Error:
            return ""
        parts: list[str] = []
        for (data,) in rows:
            try:
                payload = json.loads(data or "{}")
            except Exception:
                continue
            if not isinstance(payload, dict) or payload.get("type") != "text":
                continue
            text = str(payload.get("text") or "").strip()
            if text:
                parts.append(text)
        return "\n\n".join(parts).strip()


def parse_transcript_event(cli_name: str, obj: dict) -> tuple[str, str] | None:
    if cli_name == "claude":
        return parse_claude_event(obj)
    if cli_name == "codex":
        return parse_codex_event(obj)
    return None


def parse_claude_event(obj: dict) -> tuple[str, str] | None:
    typ = obj.get("type")
    if typ == "assistant":
        parts = []
        for item in obj.get("message", {}).get("content", []):
            if isinstance(item, dict) and item.get("type") == "text" and str(item.get("text", "")).strip():
                parts.append(str(item["text"]).strip())
        text = "\n\n".join(parts).strip()
        return ("claude", text) if text else None
    if typ == "user":
        content = obj.get("message", {}).get("content")
        if isinstance(content, str):
            text = content.strip()
        elif isinstance(content, list):
            text = " ".join(
                str(item.get("text", ""))
                for item in content
                if isinstance(item, dict) and item.get("type") == "text"
            ).strip()
        else:
            text = ""
        return ("user", text) if text else None
    return None


def parse_codex_event(obj: dict) -> tuple[str, str] | None:
    payload = obj.get("payload", {})
    if not isinstance(payload, dict):
        return None
    if obj.get("type") == "event_msg" and payload.get("type") == "agent_message":
        text = str(payload.get("message") or "").strip()
        return ("codex", text) if text else None
    if obj.get("type") == "response_item" and payload.get("type") == "message" and payload.get("role") == "user":
        parts = []
        for item in payload.get("content", []):
            if isinstance(item, dict) and item.get("type") in ("input_text", "text") and str(item.get("text", "")).strip():
                parts.append(str(item["text"]).strip())
        text = "\n".join(parts).strip()
        return ("user", text) if text else None
    return None


def locate_transcript(
    cli_name: str, cwd: str, started_at: float, timeout: float, override_path: str = "", from_end: bool = True
) -> TranscriptTailer | None:
    if override_path:
        path = Path(override_path).expanduser()
        if path.exists():
            return TranscriptTailer(cli_name, path, from_end=from_end)
    deadline = time.time() + max(0.1, timeout)
    while time.time() < deadline:
        candidates = transcript_candidates(cli_name, cwd, started_at)
        if candidates:
            return TranscriptTailer(cli_name, candidates[0], from_end=from_end)
        time.sleep(0.5)
    return None


def has_fresh_transcript(cli_name: str, cwd: str, started_at: float, override_path: str = "") -> bool:
    if override_path:
        path = Path(override_path).expanduser()
        return path.exists()
    return bool(transcript_candidates(cli_name, cwd, started_at))


def transcript_candidates(cli_name: str, cwd: str, started_at: float) -> list[Path]:
    if cli_name == "claude":
        return claude_transcript_candidates(cwd, started_at)
    if cli_name == "codex":
        return codex_transcript_candidates(started_at)
    return []


def claude_transcript_candidates(cwd: str, started_at: float) -> list[Path]:
    # 支持 SH_CLAUDE_PROJECTS_ROOT 覆盖 claude 变体/fork 的 transcript 根目录
    # (如 claude-deepseek 用 ~/.claude-deepseek/projects);未设则回落默认。
    root_env = env_get("SH_CLAUDE_PROJECTS_ROOT", "").strip()
    root = Path(root_env).expanduser() if root_env else Path.home() / ".claude" / "projects"
    if not root.exists():
        return []
    slug = slug_cwd(cwd)
    paths: list[Path] = []
    for project_dir in root.iterdir():
        if not project_dir.is_dir():
            continue
        if slug and slug not in project_dir.name:
            continue
        paths.extend(project_dir.glob("*.jsonl"))
    return fresh_jsonl(paths, started_at)


def codex_transcript_candidates(started_at: float) -> list[Path]:
    # codex 用 CODEX_HOME 重定位配置目录(默认 ~/.codex);对齐它, 以免多实例/迁移时采集失效。
    codex_home = env_get("CODEX_HOME", "").strip()
    base = Path(codex_home).expanduser() if codex_home else Path.home() / ".codex"
    root = base / "sessions"
    if not root.exists():
        return []
    return fresh_jsonl(root.glob("**/rollout-*.jsonl"), started_at)


def fresh_jsonl(paths, started_at: float) -> list[Path]:
    fresh = []
    for path in paths:
        try:
            stat = path.stat()
        except OSError:
            continue
        if stat.st_mtime + 2 < started_at:
            continue
        first_ts = first_event_time(path)
        if first_ts is not None and first_ts + 2 < started_at:
            continue
        fresh.append(path)
    fresh.sort(key=lambda p: p.stat().st_mtime, reverse=True)
    return fresh


def slug_cwd(cwd: str) -> str:
    resolved = str(Path(cwd or os.getcwd()).resolve())
    return re.sub(r"[^A-Za-z0-9._-]+", "-", resolved).strip("-")


def tmux_session_name(session_name: str, key_id: str) -> str:
    base = re.sub(r"[^A-Za-z0-9_-]+", "-", session_name or "session").strip("-") or "session"
    suffix = re.sub(r"[^A-Za-z0-9_-]+", "", key_id or "")[-8:] or "local"
    return ("sh-" + base + "-" + suffix)[:80]


def first_event_time(path: Path) -> float | None:
    try:
        with path.open(encoding="utf-8") as f:
            for idx, line in enumerate(f):
                if idx > 80:
                    break
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                ts = event_time(obj)
                if ts is not None:
                    return ts
    except OSError:
        return None
    return None


def event_time(obj: dict) -> float | None:
    for key in ("timestamp", "created_at", "time"):
        if key not in obj:
            continue
        value = obj.get(key)
        if isinstance(value, (int, float)):
            return float(value) / 1000 if value > 10_000_000_000 else float(value)
        if isinstance(value, str):
            parsed = parse_timestamp(value)
            if parsed is not None:
                return parsed
    payload = obj.get("payload")
    if isinstance(payload, dict):
        return event_time(payload)
    return None


def parse_timestamp(value: str) -> float | None:
    text = value.strip()
    if not text:
        return None
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(text)
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.timestamp()


def user_text_matches(prompt: str, text: str) -> bool:
    p = normalize_user_text(prompt)
    t = normalize_user_text(text)
    return bool(p) and (p in t or t in p)


def normalize_user_text(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def opencode_time(value) -> float:
    if isinstance(value, (int, float)):
        numeric = float(value)
        if numeric > 10_000_000_000_000:
            return numeric / 1_000_000
        if numeric > 10_000_000_000:
            return numeric / 1000
        return numeric
    if isinstance(value, str):
        parsed = parse_timestamp(value)
        if parsed is not None:
            return parsed
        try:
            return opencode_time(float(value))
        except ValueError:
            return 0.0
    return 0.0


def strip_markdown_code_fence(text: str) -> str:
    stripped = text.strip()
    lines = stripped.splitlines()
    if len(lines) >= 2 and re.match(r"^```[\w+-]*\s*$", lines[0].strip()) and lines[-1].strip() == "```":
        return "\n".join(lines[1:-1]).strip()
    return stripped
