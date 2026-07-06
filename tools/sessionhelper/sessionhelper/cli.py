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
from dataclasses import dataclass
from datetime import datetime, timezone
import json
from pathlib import Path

import pexpect

from .config import Config


ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
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


CLI_PROFILES: dict[str, CLIProfile] = {
    "claude": CLIProfile(["claude", "--dangerously-skip-permissions"], ("claude", ">", "❯", "╭", "Welcome"), "transcript", launch="tmux"),
    "codex": CLIProfile(["codex"], ("codex", ">", "›", "❯", "Welcome"), "transcript", launch="tmux"),
    "aider": CLIProfile(["aider"], ("aider", ">", "Tokens")),
    "opencode": CLIProfile(["opencode"], ("opencode", ">", "❯", "Welcome"), "opencode_db", launch="tmux"),
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
        self.command = cfg.cli_cmd or self.profile.command
        self.cwd = cfg.cli_cwd or os.getcwd()
        self.launch_mode = cfg.cli_launch or self.profile.launch
        self.tmux_session = tmux_session_name(cfg.session_name, cfg.key_id)
        self.child: pexpect.spawn | None = None
        self.ready = threading.Event()
        self.closed = threading.Event()
        self.output_q: queue.Queue[str] = queue.Queue()
        self.mirror_q: queue.Queue[tuple[str, str]] = queue.Queue()
        self.reader: threading.Thread | None = None
        self.started_at = time.time()
        self.last_output_at = 0.0
        self.transcript: TranscriptTailer | None = None
        self.launch_attempts = 0
        self.last_launch_error = ""

    def start(self) -> bool:
        if self.ready.is_set():
            return True
        self.launch_attempts += 1
        try:
            if self.launch_mode == "tmux":
                self.start_tmux()
            else:
                self.start_pty()
        except Exception as exc:
            self.last_launch_error = str(exc)
            self.ready.clear()
            return False
        if self.ready.is_set():
            self.last_launch_error = ""
            return True
        return False

    def start_pty(self) -> None:
        if self.child is not None:
            if self.ready.is_set() or self.child.isalive():
                return
            self.child = None
        self.started_at = time.time()
        self.child = pexpect.spawn(
            self.command[0],
            self.command[1:],
            cwd=self.cwd,
            encoding="utf-8",
            codec_errors="replace",
            echo=False,
            timeout=0.2,
        )
        self.reader = threading.Thread(target=self._read_loop, name="sessionhelper-cli-reader", daemon=True)
        self.reader.start()
        deadline = time.time() + self.cfg.cli_ready_timeout
        while time.time() < deadline:
            if self.ready.is_set() and self.is_idle():
                return
            time.sleep(0.1)
        raise RuntimeError(f"CLI did not become ready: {shlex.join(self.command)}")

    def start_tmux(self) -> None:
        if self.ready.is_set():
            return
        self.started_at = time.time()
        self.last_output_at = 0.0
        subprocess.run(["tmux", "kill-session", "-t", self.tmux_session], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        cmd = ["tmux", "new-session", "-d", "-s", self.tmux_session, "-c", self.cwd, shlex.join(self.command)]
        subprocess.run(cmd, check=True)
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
            has_prompt = any(pat.lower() in cleaned.lower() for pat in self.profile.ready_patterns)
            has_transcript = self.profile.output_source == "transcript" and has_fresh_transcript(self.cli_name, self.cwd, self.started_at, self.cfg.cli_transcript)
            idle = time.time() - last_change >= self.cfg.cli_settle_seconds
            if (has_prompt and idle) or has_transcript:
                self.ready.set()
                return
            time.sleep(0.25)
        raise RuntimeError(f"CLI did not become ready in tmux {self.tmux_session}: {shlex.join(self.command)}")

    def is_idle(self) -> bool:
        if self.last_output_at <= 0:
            return False
        return time.time() - self.last_output_at >= self.cfg.cli_settle_seconds

    def stop(self) -> None:
        self.closed.set()
        if self.launch_mode == "tmux":
            subprocess.run(["tmux", "kill-session", "-t", self.tmux_session], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            return
        child = self.child
        if child is None:
            return
        try:
            child.kill(signal.SIGTERM)
        except Exception:
            pass

    def handle(self, env: dict) -> str:
        for attempt in range(max(1, self.cfg.cli_launch_retries)):
            if self.start():
                break
            if attempt + 1 < max(1, self.cfg.cli_launch_retries):
                time.sleep(1.0)
        else:
            return self.not_ready_message()
        body = str(env.get("body") or "")
        if not self.ready.wait(0.1):
            return self.not_ready_message()
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
                return ""
            status, reply = self.inject_and_collect_transcript_reply(body)
            if status == "ok" and reply:
                return reply
            if status == "no_user":
                return "（注入未被 CLI 接收：transcript 未出现对应 user 轮）"
            return "（答案超时：已确认 user 轮落盘，但未取到 assistant 回答）"
        if self.profile.output_source == "opencode_db":
            injected_at = time.time()
            self.inject_text(body)
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
        self.inject_text(body)
        if self.cfg.async_reply:
            return ""
        chunks = self.collect_output(self.cfg.cli_reply_wait)
        if chunks:
            return "\n".join(chunks)
        return "已注入 CLI，等待模型输出。"

    def not_ready_message(self) -> str:
        reason = self.last_launch_error or "启动超时"
        return f"（CLI未就绪：{reason}；sessionHelper仍在线，稍后可重试）"

    def inject_text(self, text: str) -> None:
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
            subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "Enter"], check=False)
            return
        if self.child is not None:
            self.child.send("\r")

    def send_tmux_text(self, text: str) -> None:
        # send-keys -l treats the text literally, so punctuation and spaces are
        # not interpreted as tmux key names.
        prepared = self.prepare_input_text(text)
        if prepared.startswith(BRACKETED_PASTE_START) and prepared.endswith(BRACKETED_PASTE_END):
            for chunk in (BRACKETED_PASTE_START, text, BRACKETED_PASTE_END):
                subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "-l", chunk], check=True)
            return
        subprocess.run(["tmux", "send-keys", "-t", self.tmux_session, "-l", prepared], check=True)

    def prepare_input_text(self, text: str) -> str:
        if not is_multiline(text):
            return text
        if self.supports_bracketed_paste():
            return f"{BRACKETED_PASTE_START}{text}{BRACKETED_PASTE_END}"
        return flatten_multiline(text)

    def supports_bracketed_paste(self) -> bool:
        configured = (self.cfg.tool or self.cli_name or "").strip().lower()
        return configured in BRACKETED_PASTE_CLIS or self.cli_name in BRACKETED_PASTE_CLIS

    def inject_and_collect_transcript_reply(self, prompt: str) -> tuple[str, str]:
        # Full-screen TUIs can report ready before the first input box is truly
        # able to accept send-keys. Validate the user turn in transcript; if it
        # does not land, resend the full prompt a few times instead of asking the
        # user to manually retry the first cold-start message.
        attempts = 3
        for attempt in range(attempts):
            self.inject_text(prompt)
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
            )
        except subprocess.CalledProcessError:
            return ""

    def collect_output(self, wait_seconds: float) -> list[str]:
        deadline = time.time() + max(0.1, wait_seconds)
        chunks: list[str] = []
        while time.time() < deadline:
            try:
                chunk = self.output_q.get(timeout=0.2)
            except queue.Empty:
                continue
            if chunk:
                chunks.append(chunk)
                if time.time() + 0.5 < deadline:
                    deadline = time.time() + 0.5
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
                self.closed.set()
                return
            cleaned = clean_output(raw)
            if not cleaned:
                continue
            self.last_output_at = time.time()
            buf += "\n" + cleaned
            if any(pat.lower() in cleaned.lower() for pat in self.profile.ready_patterns):
                self.ready.set()
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
    root_env = os.environ.get("SH_CLAUDE_PROJECTS_ROOT", "").strip()
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
    codex_home = os.environ.get("CODEX_HOME", "").strip()
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
