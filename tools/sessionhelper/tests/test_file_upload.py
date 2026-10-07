"""R-V2-AGENT-FILE-UPLOAD helper side (plan runbooks/PLAN-R-V2-AGENT-FILE-UPLOAD-opus50tc2k.md §4/§7/§9/§13).

Uploads go through the real IPC socket (serve_ipc + SCM_RIGHTS) and the real upload handler against
a fake Hub ticket channel and a local fake file-service. Addendum cases go through the real
StagedGuideStore file and process_inbound_message.
"""
import array
import asyncio
import contextlib
import hashlib
import http.server
import io
import json
import os
import re
import shutil
import socket
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from sessionhelper import app as app_module
from sessionhelper import dingwei_file, mcp_shim
from sessionhelper.app import UPLOAD_ADDENDUM_ID, SessionHelper
from sessionhelper.config import load_config
from sessionhelper.file_upload import (UPLOAD_MAX_BYTES, RateLimiter, SecretScanner, UploadPolicy, UploadRejected,
                                       iter_exact, scan_and_digest)
from sessionhelper.guide_state import GUIDE_CONTEXT_FOOTER, GUIDE_CONTEXT_HEADER, StagedGuideStore
from sessionhelper.ipc_listener import serve_ipc

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


TICKET = "ticket-value-must-never-leave-helper-7f3a"
SESSION = "u1-uploader-ab12"  # Hub's bound session name: distinct from the local session name below
CRED_TOKEN = "cred-access-token-0123456789abcdefghij"
AUTH_TOKEN = "auth-refresh-token-zyxwvutsrqponmlkjihg"
EXPECTED_TEXT = ("上传文件：运行 dingwei-file upload <路径>，返回可分享链接。仅限 {root} 下、路径各段都不以点开头的工作文件；"
                 "链接公开 7 天，拿到链接的人都能下载。不要上传凭据、令牌、密钥、登录态文件。")


class FakeFileService:
    def __init__(self, status=201):
        self.status = status
        self.requests = []
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_POST(self):
                body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                outer.requests.append((self.path, {k.lower(): v for k, v in self.headers.items()}, body))
                if outer.status != 201:
                    self.send_response(outer.status)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(b'{"error":"backend detail http://internal.example/secret"}')
                    return
                payload = json.dumps({"url": outer.base + "/f/objkey.txt", "name": "stored-name.txt",
                                      "size": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                                      "media_type": "text/plain", "expires_at": "2026-10-14T00:00:00Z",
                                      "range": True}).encode()
                self.send_response(201)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class TicketWS:
    """Hub side of the session connection: answers file_capability_request like the Hub does."""

    def __init__(self, helper, base_url, ok=True, session=SESSION):
        self.helper, self.base_url, self.ok, self.session = helper, base_url, ok, session
        self.requests = []

    async def send(self, raw):
        env = json.loads(raw)
        meta = env.get("meta") or {}
        if meta.get("type") != "file_capability_request":
            return
        self.requests.append(meta)
        response = {"type": "file_capability_response", "request_id": meta["request_id"], "ok": self.ok}
        if self.ok:
            response.update(token=TICKET, public_base_url=self.base_url, expires_at="2026-10-07T00:01:00Z")
            if self.session:
                response["session"] = self.session
        else:
            response["error"] = "file capability unavailable"
        asyncio.get_running_loop().call_soon(self.helper.handle_file_capability_response, {"meta": response})


class Layout:
    """Hosted-like workspace: dot dirs for CODEX_HOME / state; or plain dirs (dot=False) to exercise
    the protected-path and inode layers on their own."""

    def __init__(self, tmp: Path, dot=True, root_name="seatroot-7c1"):
        self.tmp = tmp
        self.root = tmp / root_name
        self.root.mkdir()
        self.codex = self.root / (".codex" if dot else "codexhome")
        self.state = self.root / (".dingwei" if dot else "statedir")
        self.codex.mkdir()
        self.state.mkdir()
        # Plain layout keeps the credential outside every protected directory so the exact-path layer
        # is what refuses it (not the directory prefix).
        creds_dir = self.state if dot else self.root / "creds"
        creds_dir.mkdir(exist_ok=True)
        self.cred = creds_dir / "account-credential"
        self.cred.write_text(json.dumps({"tokens": {"access_token": CRED_TOKEN}}))
        self.auth = self.codex / "auth.json"
        self.auth.write_text(json.dumps({"tokens": {"access_token": CRED_TOKEN}}))
        self.token = self.state / "enroll-token.json"
        self.token.write_text('{"token":"enroll-one-time-token-abcdefghijkl"}')
        self.inbox = self.state / "inbox.sqlite3"  # created (WAL) by the helper itself
        self.ipc_dir = Path(tempfile.mkdtemp(prefix="ipc-", dir="/tmp"))
        self.sock = str(self.ipc_dir / "s")

    def env(self, **overrides):
        env = {
            "SH_SESSION_NAME": "uploader", "SH_OWNER": "zzc", "SH_KEY_ID": "FB-2642", "SH_SECRET": "secret",
            "SH_PROVISION_NO_EXIT": "1", "SH_MODE": "driver",
            "SH_CLI_CWD": str(self.root), "SH_MCP_IPC_SOCK": self.sock,
            "SH_STATE_DIR": str(self.state), "CODEX_HOME": str(self.codex),
            "SH_CRED_FILE": str(self.cred), "SH_ENROLL_TOKEN_FILE": str(self.token),
            "SH_INBOX_DB": str(self.inbox),
            "SH_PROVISION_STATE_FILE": str(self.tmp / "provision.json"),
            "SH_PROVISION_VERSIONS_FILE": str(self.tmp / "versions.json"),
        }
        env.update(overrides)
        return env

    def cleanup(self):
        shutil.rmtree(self.ipc_dir, ignore_errors=True)


class CountingAdapter:
    def __init__(self, *, fail=False):
        self.fail = fail
        self.calls = []

    def handle(self, env):
        self.calls.append(dict(env))
        return ""

    def handle_delivery(self, env, on_injected=None, on_admitted=None, on_admission_failed=None):
        self.calls.append(dict(env))
        if self.fail:
            if on_admission_failed:
                on_admission_failed()
            raise RuntimeError("fixture admission failure")
        if on_injected:
            on_injected()
        if on_admitted:
            on_admitted()
        return ""


def make_helper(layout, adapter=None, **env_overrides):
    cfg = load_config(layout.env(**env_overrides))
    with mock.patch.object(SessionHelper, "_build_adapter", return_value=adapter or CountingAdapter()):
        return SessionHelper(cfg)


def raw_ipc(sock_path, payload, fd=None):
    data = json.dumps(payload).encode() + b"\n"
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(30)
        client.connect(sock_path)
        if fd is None:
            client.sendall(data)
        else:
            client.sendmsg([data], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array("i", [fd]))])
        reply = bytearray()
        while b"\n" not in reply:
            chunk = client.recv(4096)
            if not chunk:
                break
            reply.extend(chunk)
    return json.loads(bytes(reply))


class UploadHarness:
    """Runs serve_ipc with the helper's real upload handler on an event loop thread."""

    def __init__(self, helper, ws):
        self.helper, self.ws = helper, ws
        self.loop = asyncio.new_event_loop()
        self.thread = threading.Thread(target=self.loop.run_forever, daemon=True)
        self.thread.start()

        async def start():
            async def no_send(*_a):
                raise AssertionError("send handler must not be used")
            self.task = asyncio.ensure_future(serve_ipc(
                helper.cfg.mcp_ipc_sock, no_send,
                upload_handler=helper._make_upload_handler(ws) if helper.upload_policy.enabled else None,
                bound=helper.ipc_bound))
            await asyncio.wait_for(helper.ipc_bound.wait(), 5)

        asyncio.run_coroutine_threadsafe(start(), self.loop).result(10)

    def close(self):
        async def stop():
            self.task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await self.task
        asyncio.run_coroutine_threadsafe(stop(), self.loop).result(10)
        self.loop.call_soon_threadsafe(self.loop.stop)
        self.thread.join(5)
        self.loop.close()


class UploadCase(unittest.TestCase):
    dot_layout = True
    service_status = 201
    ticket_ok = True
    ticket_session = SESSION

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.layout = Layout(Path(self._tmp.name), dot=self.dot_layout)
        self.service = FakeFileService(self.service_status)
        self.helper = make_helper(self.layout)
        self.ws = TicketWS(self.helper, self.service.base, ok=self.ticket_ok, session=self.ticket_session)
        self.out = io.StringIO()
        self._redirect = contextlib.redirect_stdout(self.out)
        self._redirect.__enter__()
        self.harness = UploadHarness(self.helper, self.ws)

    def tearDown(self):
        self.harness.close()
        self._redirect.__exit__(None, None, None)
        self.service.close()
        self.layout.cleanup()
        self._tmp.cleanup()

    def work_file(self, rel="report.txt", data=b"hello upload\n"):
        path = self.layout.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def upload_fd(self, path, flags=os.O_RDONLY):
        fd = os.open(path, flags | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            return raw_ipc(self.layout.sock, {"type": "file_upload_v1", "request_id": "r1"}, fd)
        finally:
            os.close(fd)

    def assertRejected(self, response, *reasons):
        self.assertFalse(response["ok"], response)
        self.assertEqual(response.get("error"), "file upload rejected", response)
        self.assertIn(response.get("reason"), reasons, response)
        self.assertEqual(self.ws.requests, [], "a refused file must not consume an upload ticket")
        self.assertEqual(self.service.requests, [])


class UploadContractTest(UploadCase):
    def test_upload_returns_contract_and_sends_complete_headers(self):
        data = b"quarterly numbers\n" * 3
        path = self.work_file("out/report.txt", data)
        result = dingwei_file.upload_file(self.layout.sock, str(path))
        digest = hashlib.sha256(data).hexdigest()
        self.assertEqual(set(result), {"url", "name", "size", "sha256", "expires_at"})
        self.assertEqual((result["size"], result["sha256"]), (len(data), digest))
        self.assertTrue(result["url"].startswith(self.service.base + "/f/"))
        self.assertEqual(len(self.ws.requests), 1)
        self.assertEqual((self.ws.requests[0]["operation"], self.ws.requests[0]["max_bytes"]), ("upload", len(data)))
        req_path, headers, body = self.service.requests[0]
        self.assertEqual((req_path, body), ("/files/upload", data))
        self.assertEqual(headers["authorization"], "Bearer " + TICKET)
        self.assertEqual(headers["x-dingwei-session"], SESSION)
        self.assertEqual(headers["x-content-sha256"], digest)
        self.assertEqual(headers["content-length"], str(len(data)))
        self.assertEqual(headers["x-file-name"], "UTF-8''report.txt")

    def test_ticket_and_authorization_never_leave_helper(self):
        path = self.work_file()
        response = raw_ipc(self.layout.sock, {"type": "file_upload_v1", "request_id": "r1"},
                           os.open(path, os.O_RDONLY))
        self.assertTrue(response["ok"], response)
        for blob in (json.dumps(response), self.out.getvalue()):
            self.assertNotIn(TICKET, blob)
            self.assertNotIn("Authorization", blob)
            self.assertNotIn("Bearer", blob)
        self.assertIn("[file] upload ok", self.out.getvalue())
        self.assertNotIn(str(self.layout.root), self.out.getvalue(), "audit line logs the basename, not the path")

    def test_over_limit_is_refused_before_any_ticket(self):
        path = self.layout.root / "big.bin"
        with open(path, "wb") as fh:
            fh.truncate(UPLOAD_MAX_BYTES + 1)
        self.assertRejected(self.upload_fd(path), "too_large")

    def test_path_only_request_is_refused(self):
        path = self.work_file()
        for key in ("path", "file", "file_path"):
            response = raw_ipc(self.layout.sock, {"type": "file_upload_v1", key: str(path)})
            self.assertFalse(response["ok"])
            self.assertIn("paths are not accepted", response["error"])
        response = raw_ipc(self.layout.sock, {"type": "file_upload_v1"})
        self.assertEqual(response["error"], "exactly one file descriptor is required")
        self.assertEqual(self.ws.requests, [])

    def test_descriptor_on_other_request_types_is_refused(self):
        path = self.work_file()
        fd = os.open(path, os.O_RDONLY)
        try:
            response = raw_ipc(self.layout.sock, {"type": "mcp_send", "to": "x", "body": "y"}, fd)
        finally:
            os.close(fd)
        self.assertFalse(response["ok"])

    @unittest.skipIf(os.geteuid() == 0, "root bypasses file permissions")
    def test_unreadable_file_fails_in_client_and_never_reaches_helper(self):
        path = self.work_file("locked.txt")
        path.chmod(0)
        with self.assertRaises(PermissionError):
            dingwei_file.upload_file(self.layout.sock, str(path))
        self.assertEqual(self.ws.requests, [])

    def test_non_regular_files_are_refused(self):
        directory = self.layout.root / "adir"
        directory.mkdir()
        self.assertRejected(self.upload_fd(directory), "not_regular_file")
        fifo = self.layout.root / "pipe"
        os.mkfifo(fifo)
        self.assertRejected(self.upload_fd(fifo), "not_regular_file")
        link = self.layout.root / "link.txt"
        link.symlink_to(self.work_file())
        with self.assertRaises(OSError):
            dingwei_file.upload_file(self.layout.sock, str(link))  # O_NOFOLLOW in the client
        self.assertEqual(self.ws.requests, [])

    def test_deleted_file_is_refused(self):
        path = self.work_file("gone.txt")
        fd = os.open(path, os.O_RDONLY)
        try:
            path.unlink()
            response = raw_ipc(self.layout.sock, {"type": "file_upload_v1"}, fd)
        finally:
            os.close(fd)
        self.assertRejected(response, "deleted")

    def test_outside_workspace_and_dot_segments_are_refused(self):
        outside = Path(self._tmp.name) / "outside.txt"
        outside.write_text("not in the workspace")
        self.assertRejected(self.upload_fd(outside), "outside_workspace")
        self.assertRejected(self.upload_fd(Path("/etc/hostname")) if Path("/etc/hostname").exists()
                            else self.upload_fd(outside), "outside_workspace")
        git_config = self.work_file(".git/config", b"[core]\n")
        self.assertRejected(self.upload_fd(git_config), "dot_segment")
        nested = self.work_file("src/.secrets/notes.txt", b"x")
        self.assertRejected(self.upload_fd(nested), "dot_segment")

    def test_protected_files_in_hosted_layout_are_refused(self):
        for path in (self.layout.auth, self.layout.cred, self.layout.token,
                     Path(str(self.layout.inbox) + "-wal"), self.layout.state / "anything.txt"):
            if not path.exists():
                path.write_text("x")
            with self.subTest(path=path.name):
                self.assertRejected(self.upload_fd(path), "dot_segment", "protected_path", "protected_name")

    def test_symlinked_directory_into_codex_home_is_refused(self):
        alias = self.layout.root / "alias"
        alias.symlink_to(self.layout.codex, target_is_directory=True)
        self.assertRejected(self.upload_fd(alias / "auth.json"), "dot_segment", "protected_path")

    def test_protected_name_patterns(self):
        for name in ("prod.env", "server.pem", "tls.key", "id_rsa", "id_ed25519.pub", "inbox.sqlite3-shm"):
            with self.subTest(name=name):
                self.assertRejected(self.upload_fd(self.work_file(name, b"x")), "protected_name")

    def test_copied_auth_json_is_refused_by_content(self):
        copy = self.work_file("auth-copy.json", self.layout.auth.read_bytes())
        self.assertRejected(self.upload_fd(copy), "secret_content")

    def test_renewed_auth_token_is_picked_up_without_restart(self):
        # Codex renews auth.json in place; the scan set must follow it (start-up snapshot would miss it).
        self.assertRejected(self.upload_fd(self.work_file("pre.json", b'{"t":"' + CRED_TOKEN.encode() + b'"}')),
                            "secret_content")
        self.layout.auth.write_text(json.dumps({"tokens": {"refresh_token": AUTH_TOKEN}}))
        leaked = self.work_file("renewed.txt", b"prefix " + AUTH_TOKEN.encode() + b" suffix")
        self.assertRejected(self.upload_fd(leaked), "secret_content")

    def test_token_straddling_a_read_chunk_boundary_is_found(self):
        size = 4 * 1024 * 1024
        data = bytearray(b"a" * size)
        start = 1024 * 1024 - len(CRED_TOKEN) // 2
        data[start:start + len(CRED_TOKEN)] = CRED_TOKEN.encode()
        self.assertRejected(self.upload_fd(self.work_file("big.txt", bytes(data))), "secret_content")

    def test_rate_limit(self):
        self.helper.upload_limiter = RateLimiter(max_count=1, max_bytes=10 ** 9)
        self.assertTrue(self.upload_fd(self.work_file("one.txt"))["ok"])
        self.ws.requests.clear()
        self.service.requests.clear()
        self.assertRejected(self.upload_fd(self.work_file("two.txt")), "rate_limited")
        self.helper.upload_limiter = RateLimiter(max_count=10, max_bytes=5)
        self.assertRejected(self.upload_fd(self.work_file("three.txt", b"123456")), "rate_limited")

    def test_cli_entry_point_end_to_end(self):
        path = self.work_file("cli.txt", b"via cli")
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            rc = dingwei_file.main(["--socket", self.layout.sock, "upload", str(path)])
        self.assertEqual(rc, 0)
        printed = [line for line in stdout.getvalue().splitlines() if line.startswith("{")]  # helper audit lines share stdout here
        self.assertEqual(len(printed), 1, stdout.getvalue())
        self.assertEqual(json.loads(printed[0])["sha256"], hashlib.sha256(b"via cli").hexdigest())

    def test_mcp_tool_end_to_end(self):
        path = self.work_file("mcp.txt", b"via mcp")
        with mock.patch.object(mcp_shim, "_IPC_SOCK", self.layout.sock):
            result = mcp_shim._handlers["upload_file"]({"path": str(path)})
        self.assertEqual(result["sha256"], hashlib.sha256(b"via mcp").hexdigest())
        self.assertNotIn(TICKET, json.dumps(result))


class PlainLayoutProtectedTest(UploadCase):
    """No dot dirs: the protected-path, name and inode layers must hold on their own."""

    dot_layout = False

    def test_protected_paths_without_dot_segments(self):
        for path in (self.layout.auth, self.layout.cred, self.layout.token, self.layout.state / "x.txt"):
            if not path.exists():
                path.write_text("x")
            with self.subTest(path=path.name):
                self.assertRejected(self.upload_fd(path), "protected_path")

    def test_hard_links_to_protected_files_are_refused(self):
        for target in (self.layout.auth, self.layout.cred):
            link = self.layout.root / f"innocent-{target.name}.txt"
            os.link(target, link)
            with self.subTest(target=target.name):
                self.assertRejected(self.upload_fd(link), "protected_inode")


class ReviewFollowupTest(UploadCase):
    """tosonnet55tc2k 复核 S1／S2。"""

    dot_layout = False

    def test_S1_protected_name_patterns_ignore_case(self):
        for name in ("k.PEM", "Id_Rsa", "server.KEY", "Prod.Env", "ID_ED25519.pub", "INBOX.sqlite3-WAL"):
            with self.subTest(name=name):
                self.assertRejected(self.upload_fd(self.work_file(name, b"x")), "protected_name")

    def renew_auth(self, token):
        renewed = self.layout.codex / "auth.json.tmp"
        renewed.write_text(json.dumps({"tokens": {"access_token": token}}))
        os.replace(renewed, self.layout.auth)  # Codex renews with a new inode

    def test_S2_hard_link_before_renewal_is_refused(self):
        stale = self.layout.root / "stale.txt"
        os.link(self.layout.auth, stale)
        self.assertRejected(self.upload_fd(stale), "protected_inode", "hard_linked")

    def test_S2_stale_hard_link_after_renewal_is_refused_by_remembered_token(self):
        # 复核方场景：链接建在续期前，续期后旧 inode 只剩 stale.txt 一个链接（st_nlink 回到 1），
        # (dev, ino) 与现行令牌集合都认不出——要靠 helper 启动时记下的旧令牌按内容拦。
        old_token = "old-auth-access-token-0123456789abcdef"
        self.layout.auth.write_text(json.dumps({"tokens": {"access_token": old_token}}))
        self.helper.secret_scanner.needles()  # helper 在续期前已运行（启动时即读）
        stale = self.layout.root / "stale.txt"
        os.link(self.layout.auth, stale)
        self.renew_auth(AUTH_TOKEN)
        self.assertEqual(os.stat(stale).st_nlink, 1)
        self.assertNotEqual(os.stat(stale).st_ino, os.stat(self.layout.auth).st_ino)
        self.assertNotIn(old_token, self.layout.cred.read_text() + self.layout.auth.read_text())
        self.assertRejected(self.upload_fd(stale), "secret_content")

    def test_S2_helper_reads_tokens_at_start(self):
        old_token = "start-time-token-abcdefghijklmnopqrstuv"
        self.layout.auth.write_text(json.dumps({"tokens": {"access_token": old_token}}))
        fresh = make_helper(self.layout)
        self.renew_auth(AUTH_TOKEN)  # no needles() call in between: only the start-up read can know old_token
        needles = fresh.secret_scanner.needles()
        self.assertIn(old_token.encode(), needles)
        self.assertIn(AUTH_TOKEN.encode(), needles)

    def test_S2_any_multiply_linked_work_file_is_refused(self):
        original = self.work_file("plain.txt", b"ordinary work output")
        os.link(original, self.layout.root / "plain-copy.txt")
        self.assertRejected(self.upload_fd(original), "hard_linked")


class BackendFailureTicketDeniedTest(UploadCase):
    ticket_ok = False

    def test_ticket_denied_is_uniform_failure(self):
        response = self.upload_fd(self.work_file())
        self.assertEqual(response, {"ok": False, "error": "file upload failed"})
        self.assertEqual(self.service.requests, [])


class BackendFailureServiceErrorTest(UploadCase):
    service_status = 500

    def test_file_service_error_is_uniform_failure(self):
        response = self.upload_fd(self.work_file())
        self.assertEqual(response, {"ok": False, "error": "file upload failed"})
        self.assertNotIn("internal.example", json.dumps(response) + self.out.getvalue())


class BackendMissingSessionTest(UploadCase):
    ticket_session = ""

    def test_ticket_without_bound_session_is_not_used(self):
        response = self.upload_fd(self.work_file())
        self.assertEqual(response, {"ok": False, "error": "file upload failed"})
        self.assertEqual(self.service.requests, [])


class PolicyUnitTest(unittest.TestCase):
    def test_workspace_root_slash_disables_upload(self):
        policy = UploadPolicy.build("/")
        self.assertFalse(policy.enabled)
        with self.assertRaises(UploadRejected) as ctx:
            policy.check("/home/x/a.txt", os.stat(__file__))
        self.assertEqual(ctx.exception.code, "upload_disabled")
        with tempfile.TemporaryDirectory() as tmp:
            layout = Layout(Path(tmp))
            try:
                helper = make_helper(layout, SH_CLI_CWD="/")
                self.assertFalse(helper.upload_configured())
                self.assertFalse(helper.session_capabilities_envelope()["meta"]["file_upload_v1"])
            finally:
                layout.cleanup()

    def test_size_changes_between_fstat_and_read_abort(self):
        with tempfile.TemporaryFile() as fh:
            fh.write(b"abcdef")
            fh.flush()
            with self.assertRaises(UploadRejected) as shrunk:
                scan_and_digest(fh.fileno(), 10, ())
            self.assertEqual(shrunk.exception.code, "size_changed")
            with self.assertRaises(UploadRejected) as grew:
                scan_and_digest(fh.fileno(), 3, ())
            self.assertEqual(grew.exception.code, "size_changed")
            with self.assertRaises(UploadRejected):
                list(iter_exact(fh.fileno(), 3))
            self.assertEqual(scan_and_digest(fh.fileno(), 6, ()), hashlib.sha256(b"abcdef").hexdigest())

    def test_scanner_rereads_only_on_change(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = Path(tmp) / "auth.json"
            src.write_text(json.dumps({"a": CRED_TOKEN, "short": "tiny"}))
            scanner = SecretScanner((str(src),))
            self.assertIn(CRED_TOKEN.encode(), scanner.needles())
            self.assertNotIn(b"tiny", scanner.needles())
            src.write_text(json.dumps({"b": AUTH_TOKEN}))
            self.assertIn(AUTH_TOKEN.encode(), scanner.needles())
            self.assertIn(CRED_TOKEN.encode(), scanner.needles(), "tokens seen earlier stay in the set (S2)")

    def test_seen_token_history_is_capped(self):
        # 复核 V4：去掉上限仍绿 ⇒ 补格。逐次续期灌入远超上限的不同令牌，历史条数不得超过 HISTORY_MAX，
        # 且淘汰的是最旧的、最新的仍在。
        with tempfile.TemporaryDirectory() as tmp:
            src = Path(tmp) / "auth.json"
            scanner = SecretScanner((str(src),))
            total = SecretScanner.HISTORY_MAX + 40
            tokens = [f"rotating-token-{i:05d}-abcdefghijklmnop" for i in range(total)]
            for i, token in enumerate(tokens):
                src.write_text(json.dumps({"t": token}))
                os.utime(src, ns=(i + 1, (i + 1) * 1_000_000_000))  # 每次签名不同，保证重读
                needles = scanner.needles()
            self.assertLessEqual(len(scanner._history), SecretScanner.HISTORY_MAX)
            self.assertIn(tokens[-1].encode(), needles)
            self.assertNotIn(tokens[0].encode(), needles)

    def test_upload_limit_matches_go_filepolicy(self):
        go = (Path(__file__).resolve().parents[3] / "internal/filepolicy/policy.go").read_text()
        match = re.search(r"MaxUploadBytes\s+int64\s*=\s*(\d+)\s*<<\s*(\d+)", go)
        self.assertIsNotNone(match, "MaxUploadBytes not found in filepolicy")
        self.assertEqual(int(match.group(1)) << int(match.group(2)), UPLOAD_MAX_BYTES)


# ── §13 addenda (附注独立记账) ────────────────────────────────────────

def guide_env(body="GUIDE-BODY", version="v1"):
    return {"id": "guide-frame", "from": "workpulse#FB-2642", "to": "uploader#FB-2642", "body": body,
            "meta": {"type": "agent_network_skill", "system": True, "guide_version": version}}


def business_env(body="DO WORK", eid="business-1"):
    return {"id": eid, "from": "peer#FB-2642", "to": "uploader#FB-2642", "body": body,
            "meta": {"source_kind": "agent", "reply_policy": "no_reply"}}


class NullWS:
    async def send(self, _raw):
        pass


class AddendaTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.layout = Layout(Path(self._tmp.name), root_name="addenda-root-91f")

    def tearDown(self):
        self.layout.cleanup()
        self._tmp.cleanup()

    def capable(self, adapter=None, **env):
        helper = make_helper(self.layout, adapter or CountingAdapter(), **env)
        helper.ipc_bound.set()
        self.assertTrue(helper.upload_capable(), "fixture must be capable")
        return helper

    def run_business(self, helper, body="DO WORK", eid="business-1"):
        asyncio.run(helper.process_inbound_message(NullWS(), business_env(body, eid)))
        return helper.adapter.calls[-1]["body"]

    def text(self):
        return EXPECTED_TEXT.format(root=str(self.layout.root))

    def test_A1_A2_existing_seat_consumed_guide_gets_addendum_once_without_guide(self):
        old = make_helper(self.layout)  # old helper behaviour: guide staged and consumed, no ledger
        asyncio.run(old.inject_agent_network_skill(guide_env()))
        claim = old.guide_store.claim()
        self.assertTrue(old.guide_store.consume(claim))
        helper = self.capable()
        asyncio.run(helper.inject_agent_network_skill(guide_env()))  # Hub re-pushes the same guide
        first = self.run_business(helper, "FIRST")
        self.assertEqual(first.count(self.text()), 1)
        self.assertNotIn("GUIDE-BODY", first, "the whole guide must not be replayed")
        self.assertEqual(first, f"{GUIDE_CONTEXT_HEADER}\n{self.text()}\n\n{GUIDE_CONTEXT_FOOTER}\n\nFIRST")
        self.assertEqual(self.run_business(helper, "SECOND", "business-2"), "SECOND")

    def test_A3_incapable_helper_neither_attaches_nor_records(self):
        helper = make_helper(self.layout, SH_MCP_IPC_SOCK="")
        asyncio.run(helper.inject_agent_network_skill(guide_env()))
        first = self.run_business(helper, "FIRST")
        self.assertNotIn("dingwei-file upload", first)
        self.assertNotIn("addenda_delivered", helper.guide_store.state())

    def test_A4_lost_ledger_means_at_most_one_more_attachment(self):
        helper = self.capable()
        self.run_business(helper, "FIRST")
        os.unlink(helper.guide_store.path)
        self.assertEqual(self.run_business(helper, "AGAIN", "business-2").count(self.text()), 1)
        self.assertEqual(self.run_business(helper, "THIRD", "business-3"), "THIRD")

    def test_A5_ledger_write_failure_does_not_block_injection(self):
        helper = self.capable()
        out = io.StringIO()
        with mock.patch.object(helper.guide_store, "mark_addendum_delivered", side_effect=OSError("read-only")), \
                contextlib.redirect_stdout(out):
            body = self.run_business(helper, "FIRST")
        self.assertIn(self.text(), body)
        self.assertTrue(body.endswith("FIRST"))
        self.assertIn("addendum ledger write failed", out.getvalue())
        self.assertEqual(self.run_business(helper, "NEXT", "business-2").count(self.text()), 1)

    def test_A6_new_seat_guide_and_addendum_once_then_new_guide_version_without_addendum(self):
        helper = self.capable()
        asyncio.run(helper.inject_agent_network_skill(guide_env("GUIDE-V1", "v1")))
        first = self.run_business(helper, "FIRST")
        self.assertEqual((first.count("GUIDE-V1"), first.count(self.text())), (1, 1))
        self.assertLess(first.index(self.text()), first.index(GUIDE_CONTEXT_FOOTER))
        self.assertEqual(self.run_business(helper, "SECOND", "business-2"), "SECOND")
        asyncio.run(helper.inject_agent_network_skill(guide_env("GUIDE-V2", "v2")))
        third = self.run_business(helper, "THIRD", "business-3")
        self.assertIn("GUIDE-V2", third)
        self.assertNotIn(self.text(), third)

    def test_A7_capability_truth_table(self):
        cases = {
            "all": ({}, True, True),
            "no_command": ({}, False, True),
            "ipc_not_bound": ({}, True, False),
            "root_is_slash": ({"SH_CLI_CWD": "/"}, True, True),
            "no_socket_configured": ({"SH_MCP_IPC_SOCK": ""}, True, True),
        }
        for name, (env, command, bound) in cases.items():
            with self.subTest(case=name):
                helper = make_helper(self.layout, CountingAdapter(), **env)
                if bound:
                    helper.ipc_bound.set()
                which = shutil.which if command else (lambda _n: None)
                with mock.patch.object(app_module.shutil, "which", side_effect=which):
                    body = self.run_business(helper, "WORK")
                if name == "all":
                    self.assertIn(self.text(), body)
                else:
                    self.assertEqual(body, "WORK")
                    self.assertNotIn("addenda_delivered", helper.guide_store.state())
                os.path.exists(helper.guide_store.path) and os.unlink(helper.guide_store.path)

    def test_A8_admission_failure_does_not_record(self):
        helper = self.capable(CountingAdapter(fail=True))
        asyncio.run(helper.process_inbound_message(NullWS(), business_env("FIRST")))
        self.assertFalse(helper.guide_store.addendum_delivered(UPLOAD_ADDENDUM_ID))
        helper.adapter = CountingAdapter()
        self.assertIn(self.text(), self.run_business(helper, "RETRY", "business-2"))
        self.assertTrue(helper.guide_store.addendum_delivered(UPLOAD_ADDENDUM_ID))

    def test_A9_text_verbatim_body_untouched_and_staged_guide_unchanged(self):
        self.assertNotEqual(str(self.layout.root), "/work")
        helper = self.capable()
        asyncio.run(helper.inject_agent_network_skill(guide_env()))
        before = dict(helper.guide_store.state())
        reference = StagedGuideStore(str(Path(self._tmp.name) / "ref.json"), "ref")
        reference.stage("GUIDE-BODY", "v1")
        body = self.run_business(helper, "BUSINESS BODY 原样")
        self.assertIn(self.text(), body)
        self.assertIn(f"仅限 {self.layout.root} 下", body)
        self.assertIn("不要上传凭据、令牌、密钥、登录态文件。", body)
        self.assertTrue(body.endswith(f"{GUIDE_CONTEXT_FOOTER}\n\nBUSINESS BODY 原样"))
        after = helper.guide_store.state()
        self.assertEqual((after["version"], after["body_hash"]), (before["version"], before["body_hash"]))
        self.assertEqual(after["body_hash"], reference.state()["body_hash"])

    def test_only_real_business_input_consumes_the_addendum(self):
        helper = self.capable()
        system_env = dict(business_env("SYSTEM NOTE"), meta={"system": True, "source_kind": "agent"})
        asyncio.run(helper.process_inbound_message(NullWS(), system_env))
        self.assertEqual(helper.adapter.calls[-1]["body"], "SYSTEM NOTE")
        self.assertFalse(helper.guide_store.addendum_delivered(UPLOAD_ADDENDUM_ID))
        self.assertIn(self.text(), self.run_business(helper, "REAL", "business-2"))

    def test_admitted_callback_errors_cost_at_most_one_more_attachment(self):
        helper = self.capable()
        asyncio.run(helper.inject_agent_network_skill(guide_env()))
        with mock.patch.object(helper.guide_store, "consume", side_effect=RuntimeError("boom")), \
                mock.patch.object(helper.guide_store, "mark_addendum_delivered", side_effect=RuntimeError("boom")), \
                contextlib.redirect_stdout(io.StringIO()):
            first = self.run_business(helper, "FIRST")
        self.assertIn(self.text(), first)
        self.assertEqual(self.run_business(helper, "SECOND", "business-2").count(self.text()), 1)
        self.assertEqual(self.run_business(helper, "THIRD", "business-3"), "THIRD")


if __name__ == "__main__":
    unittest.main()
