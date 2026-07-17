import unittest
import asyncio
import hashlib
import io
import json
import stat
import sqlite3
import subprocess
import tarfile
import tempfile
import time
from unittest import mock
from pathlib import Path

from sessionhelper import __version__
from sessionhelper.app import (
    COMM_SKILL_ACK,
    SessionHelper,
    agent_route_envelope,
    comm_skill_ack_envelope,
    contains_comm_skill_ack,
    is_broadcast_envelope,
    is_no_mirror_envelope,
    is_online_directory_text,
)
from sessionhelper.cli import (
    BRACKETED_PASTE_END,
    BRACKETED_PASTE_START,
    CLIAdapter,
    CLI_PROFILES,
    OpenCodeDBReader,
    TranscriptTailer,
    event_time,
    fresh_jsonl,
    opencode_time,
    parse_claude_event,
    parse_codex_event,
    slug_cwd,
    strip_markdown_code_fence,
    tmux_session_name,
    user_text_matches,
)
from sessionhelper.config import default_inbox_db, detect_full_session_name, detect_os, load_config
from sessionhelper.llm import PROVIDERS
from sessionhelper.protocol import AddressBook, is_mirror_control, reply_target
from sessionhelper.provision import Provisioner, compare_versions
from sessionhelper.send_dingwei import DEFAULT_WS_BASE, temporary_session_name
from sessionhelper.inbox import DeliveryInbox


BASE_ENV = {
    "SH_SESSION_NAME": "home",
    "SH_OWNER": "owner1",
    "SH_KEY_ID": "FB-0000",
    "SH_SECRET": "secret",
}

ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = ROOT.parents[1]


class SessionHelperTest(unittest.TestCase):
    def test_helper_version_matches_repository_version_file(self):
        self.assertEqual(__version__, (REPO_ROOT / "VERSION").read_text().strip())

    def test_control_route_multiline_positive_and_negative_examples(self):
        book = AddressBook("home", "FB-0000", "ExampleBot")
        routed = agent_route_envelope(
            '[[DW_ROUTE_V1\n{\n  "to": "@owner1#manager",\n  "body": "first\\nsecond"\n}\n]]', book
        )
        self.assertEqual(routed["to"], "@owner1#manager")
        self.assertEqual(routed["body"], "first\nsecond")
        self.assertIsNone(agent_route_envelope("# Markdown title", book))
        self.assertIsNone(agent_route_envelope('[[DW_ROUTE_V1\n{"to":"all","body":"x"}\n]]', book))
        self.assertIsNone(agent_route_envelope('[[DW_ROUTE_V1\n{"to":"#dev","body":"x","from":"fake"}\n]]', book))

    def test_delivery_inbox_deduplicates_and_recovers_processing(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "inbox.db")
            inbox = DeliveryInbox(path, 2)
            env = {"body": "one", "meta": {"type": "delivery", "delivery_id": "dlv_1"}}
            self.assertEqual(inbox.accept("dlv_1", env), ("delivered", 0, 1))
            self.assertEqual(inbox.accept("dlv_1", env), ("delivered", 0, 1))
            self.assertEqual(inbox.claim_next()[0], "dlv_1")
            self.assertEqual(inbox.accept("dlv_1", env)[0], "delivered")  # processing maps to delivered
            recovered = DeliveryInbox(path, 2)
            self.assertEqual(recovered.claim_next()[0], "dlv_1")
            recovered.finish("dlv_1")
            self.assertEqual(recovered.accept("dlv_1", env)[0], "processed")

    def test_delivery_inbox_release_returns_processing_to_received(self):
        with tempfile.TemporaryDirectory() as tmp:
            inbox = DeliveryInbox(str(Path(tmp) / "inbox.db"), 2)
            inbox.accept("dlv_1", {"body": "one"})
            self.assertEqual(inbox.claim_next()[0], "dlv_1")
            inbox.release("dlv_1")
            self.assertEqual(inbox.claim_next()[0], "dlv_1")

    def test_delivery_inbox_failed_can_be_retried_when_allowed(self):
        with tempfile.TemporaryDirectory() as tmp:
            inbox = DeliveryInbox(str(Path(tmp) / "inbox.db"), 2)
            env = {"body": "one"}
            inbox.accept("dlv_1", env)
            self.assertEqual(inbox.claim_next()[0], "dlv_1")
            inbox.finish("dlv_1", "boom")
            self.assertEqual(inbox.accept("dlv_1", env)[0], "failed")
            self.assertEqual(inbox.accept("dlv_1", {"body": "retry"}, retry_failed=True)[0], "delivered")
            delivery_id, retried = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_1")
            self.assertEqual(retried["body"], "retry")

    def test_delivery_inbox_fifo_and_capacity_preserve_oldest(self):
        with tempfile.TemporaryDirectory() as tmp:
            inbox = DeliveryInbox(str(Path(tmp) / "inbox.db"), 2)
            inbox.accept("dlv_1", {"body": "one"})
            inbox.accept("dlv_2", {"body": "two"})
            with self.assertRaises(OverflowError):
                inbox.accept("dlv_3", {"body": "three"})
            self.assertEqual(inbox.claim_next()[0], "dlv_1")

    def test_delivery_inbox_claims_only_current_session_from_shared_db(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "shared.db")
            home = DeliveryInbox(path, 10, "home")
            tester = DeliveryInbox(path, 10, "tester")
            home.accept("dlv_home", {"body": "home"})
            tester.accept("dlv_tester", {"body": "tester"})
            home.accept("dlv_same", {"body": "home same"})
            tester.accept("dlv_same", {"body": "tester same"})

            tester_delivery_id, tester_env = tester.claim_next()
            self.assertEqual(tester_delivery_id, "dlv_tester")
            self.assertEqual(tester_env["body"], "tester")
            tester_delivery_id, tester_env = tester.claim_next()
            self.assertEqual(tester_delivery_id, "dlv_same")
            self.assertEqual(tester_env["body"], "tester same")
            self.assertIsNone(tester.claim_next())

            home_delivery_id, home_env = home.claim_next()
            self.assertEqual(home_delivery_id, "dlv_home")
            self.assertEqual(home_env["body"], "home")
            home_delivery_id, home_env = home.claim_next()
            self.assertEqual(home_delivery_id, "dlv_same")
            self.assertEqual(home_env["body"], "home same")

    def test_delivery_inbox_migrates_legacy_db_and_recovers_processing(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "legacy.db")
            with sqlite3.connect(path) as db:
                db.execute(
                    """CREATE TABLE delivery_inbox (
                        delivery_id TEXT PRIMARY KEY,
                        envelope_json TEXT NOT NULL,
                        state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                        attempts INTEGER NOT NULL DEFAULT 0,
                        error TEXT,
                        received_at REAL NOT NULL,
                        updated_at REAL NOT NULL
                    )"""
                )
                db.execute(
                    """INSERT INTO delivery_inbox(delivery_id,envelope_json,state,received_at,updated_at)
                       VALUES(?,?,'processing',?,?)""",
                    ("dlv_legacy", json.dumps({"body": "legacy"}), 1.0, 1.0),
                )

            inbox = DeliveryInbox(path, 10, "home")
            with sqlite3.connect(path) as db:
                columns = {row[1] for row in db.execute("PRAGMA table_info(delivery_inbox)")}
                self.assertIn("session_name", columns)
                row = db.execute(
                    "SELECT session_name,state FROM delivery_inbox WHERE delivery_id='dlv_legacy'"
                ).fetchone()
                self.assertEqual(row, ("home", "received"))

            delivery_id, env = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_legacy")
            self.assertEqual(env["body"], "legacy")

    def test_delivery_inbox_imports_matching_pending_rows_from_old_shared_db(self):
        with tempfile.TemporaryDirectory() as tmp:
            legacy_path = Path(tmp) / "sessionhelper_inbox.db"
            target_path = Path(tmp) / "sessionhelper_inbox-home.db"
            with sqlite3.connect(legacy_path) as db:
                db.execute(
                    """CREATE TABLE delivery_inbox (
                        delivery_id TEXT PRIMARY KEY,
                        envelope_json TEXT NOT NULL,
                        state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                        attempts INTEGER NOT NULL DEFAULT 0,
                        error TEXT,
                        received_at REAL NOT NULL,
                        updated_at REAL NOT NULL
                    )"""
                )
                db.executemany(
                    """INSERT INTO delivery_inbox(delivery_id,envelope_json,state,attempts,received_at,updated_at)
                       VALUES(?,?,?,?,?,?)""",
                    [
                        (
                            "dlv_home_full",
                            json.dumps({"to": "owner1-home-0000#FB-0000", "body": "home full"}),
                            "processing",
                            2,
                            2.0,
                            2.0,
                        ),
                        ("dlv_tester", json.dumps({"to": "tester#FB-0000", "body": "tester"}), "received", 0, 3.0, 3.0),
                        ("dlv_missing_to", json.dumps({"body": "missing to"}), "received", 0, 4.0, 4.0),
                        ("dlv_processed", json.dumps({"to": "home#FB-0000", "body": "processed"}), "processed", 1, 5.0, 5.0),
                    ],
                )

            stderr = io.StringIO()
            with mock.patch("sys.stderr", stderr):
                inbox = DeliveryInbox(str(target_path), 10, "home", "home", "owner1-home-0000", "FB-0000")
            self.assertIn("[delivery][legacy_inbox] imported 1/1 pending deliveries", stderr.getvalue())
            self.assertIn("[delivery][legacy_inbox][WARN] skipped 1 pending deliveries without parseable target", stderr.getvalue())
            self.assertIn("[delivery][legacy_inbox][WARN] skipped 1 pending deliveries for other sessions", stderr.getvalue())
            delivery_id, env = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_home_full")
            self.assertEqual(env["body"], "home full")
            self.assertIsNone(inbox.claim_next())

    def test_delivery_inbox_imports_only_current_session_from_sessioned_shared_db(self):
        with tempfile.TemporaryDirectory() as tmp:
            legacy_path = Path(tmp) / "sessionhelper_inbox.db"
            target_path = Path(tmp) / "sessionhelper_inbox-home.db"
            with sqlite3.connect(legacy_path) as db:
                db.execute(
                    """CREATE TABLE delivery_inbox (
                        delivery_id TEXT NOT NULL,
                        session_name TEXT NOT NULL,
                        envelope_json TEXT NOT NULL,
                        state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                        attempts INTEGER NOT NULL DEFAULT 0,
                        error TEXT,
                        received_at REAL NOT NULL,
                        updated_at REAL NOT NULL,
                        PRIMARY KEY(session_name, delivery_id)
                    )"""
                )
                db.executemany(
                    """INSERT INTO delivery_inbox(delivery_id,session_name,envelope_json,state,received_at,updated_at)
                       VALUES(?,?,?,?,?,?)""",
                    [
                        ("dlv_home", "home", json.dumps({"body": "home"}), "received", 1.0, 1.0),
                        ("dlv_tester", "tester", json.dumps({"body": "tester"}), "received", 2.0, 2.0),
                        (
                            "dlv_empty_home",
                            "",
                            json.dumps({"to": "owner1-home-0000#FB-0000", "body": "empty home"}),
                            "received",
                            3.0,
                            3.0,
                        ),
                        (
                            "dlv_empty_tester",
                            "",
                            json.dumps({"to": "tester#FB-0000", "body": "empty tester"}),
                            "received",
                            4.0,
                            4.0,
                        ),
                    ],
                )

            inbox = DeliveryInbox(str(target_path), 10, "home", "home", "owner1-home-0000", "FB-0000")
            delivery_id, env = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_home")
            self.assertEqual(env["body"], "home")
            delivery_id, env = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_empty_home")
            self.assertEqual(env["body"], "empty home")
            self.assertIsNone(inbox.claim_next())

    def test_delivery_inbox_warns_when_old_shared_db_cannot_be_imported(self):
        with tempfile.TemporaryDirectory() as tmp:
            legacy_path = Path(tmp) / "sessionhelper_inbox.db"
            target_path = Path(tmp) / "sessionhelper_inbox-home.db"
            with sqlite3.connect(legacy_path) as db:
                db.execute("CREATE TABLE delivery_inbox (delivery_id TEXT PRIMARY KEY)")
                db.execute("INSERT INTO delivery_inbox(delivery_id) VALUES('dlv_bad')")

            stderr = io.StringIO()
            with mock.patch("sys.stderr", stderr):
                inbox = DeliveryInbox(str(target_path), 10, "home")
            self.assertIn("[delivery][legacy_inbox][WARN] failed to import pending deliveries", stderr.getvalue())
            inbox.accept("dlv_new", {"body": "new"})
            self.assertEqual(inbox.claim_next()[0], "dlv_new")

    def test_delivery_inbox_matches_registered_session_shape_by_short_name_and_key_tail(self):
        with tempfile.TemporaryDirectory() as tmp:
            legacy_path = Path(tmp) / "sessionhelper_inbox.db"
            target_path = Path(tmp) / "sessionhelper_inbox-manager.db"
            with sqlite3.connect(legacy_path) as db:
                db.execute(
                    """CREATE TABLE delivery_inbox (
                        delivery_id TEXT PRIMARY KEY,
                        envelope_json TEXT NOT NULL,
                        state TEXT NOT NULL CHECK(state IN ('received','processing','processed','failed')),
                        attempts INTEGER NOT NULL DEFAULT 0,
                        error TEXT,
                        received_at REAL NOT NULL,
                        updated_at REAL NOT NULL
                    )"""
                )
                db.executemany(
                    """INSERT INTO delivery_inbox(delivery_id,envelope_json,state,attempts,received_at,updated_at)
                       VALUES(?,?,?,?,?,?)""",
                    [
                        (
                            "dlv_manager",
                            json.dumps({"to": "renamed-manager-0000#FB-0000", "body": "manager"}),
                            "received",
                            0,
                            1.0,
                            1.0,
                        ),
                        (
                            "dlv_other_tail",
                            json.dumps({"to": "renamed-manager-9999#FB-0000", "body": "other tail"}),
                            "received",
                            0,
                            2.0,
                            2.0,
                        ),
                    ],
                )

            stderr = io.StringIO()
            with mock.patch("sys.stderr", stderr):
                inbox = DeliveryInbox(str(target_path), 10, "manager", "manager", "", "FB-0000")
            self.assertIn("[delivery][legacy_inbox] imported 1/1 pending deliveries", stderr.getvalue())
            self.assertIn("[delivery][legacy_inbox][WARN] skipped 1 pending deliveries for other sessions", stderr.getvalue())
            delivery_id, env = inbox.claim_next()
            self.assertEqual(delivery_id, "dlv_manager")
            self.assertEqual(env["body"], "manager")
            self.assertIsNone(inbox.claim_next())

    def test_load_config_required_and_defaults(self):
        cfg = load_config(BASE_ENV)
        self.assertEqual(cfg.session_name, "home")
        self.assertEqual(cfg.key_id, "FB-0000")
        self.assertEqual(cfg.owner, "owner1")
        self.assertEqual(cfg.registered_session_name, "owner1-home-0000")
        self.assertEqual(cfg.mode, "echo")
        self.assertEqual(cfg.cli_launch, "")
        self.assertEqual(cfg.cli_ready_timeout, 90.0)
        self.assertEqual(cfg.cli_reply_wait, 45.0)
        self.assertTrue(cfg.collect)
        self.assertFalse(cfg.async_reply)
        self.assertFalse(cfg.producer)
        self.assertFalse(cfg.no_directory)
        self.assertTrue(cfg.agent_route)
        self.assertEqual(cfg.provision_allowed_hosts, ("localhost",))
        self.assertEqual(cfg.target_group, "")
        self.assertEqual(cfg.target_bot, "")
        self.assertEqual(cfg.opencode_db, "")
        self.assertTrue(cfg.inbox_db.endswith("/.dingwei/sessionhelper_inbox-home.db"))
        self.assertEqual(cfg.ws_url, f"ws://127.0.0.1:8791/ws/session/home?key_id=FB-0000&version={__version__}&os={detect_os()}")

    def test_load_config_inbox_default_is_session_scoped_and_overridable(self):
        cfg = load_config(dict(BASE_ENV, SH_SESSION_NAME="tester"))
        self.assertTrue(cfg.inbox_db.endswith("/.dingwei/sessionhelper_inbox-tester.db"))

        overridden = load_config(dict(BASE_ENV, SH_INBOX_DB="/tmp/shared-inbox.db"))
        self.assertEqual(overridden.inbox_db, "/tmp/shared-inbox.db")

    def test_provision_state_files_are_session_scoped_and_overridable(self):
        # F3: 单机多 helper 不得共用 versions/update_state 文件（否则陈旧记录会把新下发误判为 already installed）。
        a = load_config(dict(BASE_ENV, SH_SESSION_NAME="tester"))
        b = load_config(dict(BASE_ENV, SH_SESSION_NAME="reviewer"))
        self.assertTrue(a.provision_versions_file.endswith("/.dingwei/sessionhelper_versions-tester.json"))
        self.assertTrue(a.provision_update_state_file.endswith("/.dingwei/sessionhelper_update_state-tester.json"))
        self.assertNotEqual(a.provision_versions_file, b.provision_versions_file)
        self.assertNotEqual(a.provision_update_state_file, b.provision_update_state_file)
        # Provisioner 实际采用按会话隔离的路径
        prov = Provisioner(a)
        self.assertTrue(str(prov.version_file).endswith("sessionhelper_versions-tester.json"))
        self.assertTrue(str(prov.update_state_file).endswith("sessionhelper_update_state-tester.json"))
        # 显式覆盖仍生效
        ov = load_config(dict(BASE_ENV, SH_PROVISION_VERSIONS_FILE="/tmp/v.json", SH_PROVISION_STATE_FILE="/tmp/s.json"))
        self.assertEqual(ov.provision_versions_file, "/tmp/v.json")
        self.assertEqual(ov.provision_update_state_file, "/tmp/s.json")

        self.assertEqual(default_inbox_db("Dev 55/Prod"), "~/.dingwei/sessionhelper_inbox-dev_55_prod.db")

    def test_provision_allowed_hosts_default_is_hub_not_legacy_domain(self):
        env = {k: v for k, v in BASE_ENV.items() if k != "SH_PROVISION_ALLOWED_HOSTS"}
        hosts = load_config(env).provision_allowed_hosts
        self.assertIn("localhost", hosts)
        self.assertNotIn("unconfigured.example", hosts)

    def test_load_config_accepts_missing_owner_and_validates_short_name(self):
        cfg = load_config({k: v for k, v in BASE_ENV.items() if k != "SH_OWNER"})
        self.assertEqual(cfg.session_name, "home")

        with self.assertRaises(SystemExit) as bad_short:
            load_config(dict(BASE_ENV, SH_SESSION_NAME="Dev-1"))
        self.assertIn("短名只能小写字母数字", str(bad_short.exception))

        with self.assertRaises(SystemExit) as bad_owner:
            load_config(dict(BASE_ENV, SH_OWNER="ExampleOwner", SH_SESSION_NAME="owner1-home-0000"))
        self.assertIn("SH_OWNER 不合规", str(bad_owner.exception))

    def test_send_dingwei_temporary_session_name_is_compliant(self):
        self.assertEqual(
            temporary_session_name({"SH_OWNER": "owner1", "SH_SESSION_NAME": "manager", "SH_KEY_ID": "FB-example-e0d10000"}),
            "managernote",
        )
        self.assertEqual(
            temporary_session_name({"SH_SESSION_NAME": "alice-dev1013-3dd6", "SH_KEY_ID": "FB-key-3dd6"}),
            "dev1013note",
        )
        self.assertEqual(
            temporary_session_name({"SH_SESSION_NAME": "Bad-Name", "SH_KEY_ID": "FB-key-1a2b"}),
            "sendernote",
        )

    def test_send_dingwei_default_ws_base_uses_localhost(self):
        self.assertEqual(DEFAULT_WS_BASE, "ws://127.0.0.1:8791")

    def test_ws_url_reports_tool_and_model_when_configured(self):
        cfg = load_config(dict(BASE_ENV, SH_TOOL="CODEX", SH_MODEL="gpt-5.5", SH_SESSION_FULL="sh-home-e0d10000"))
        self.assertEqual(
            cfg.ws_url,
            f"ws://127.0.0.1:8791/ws/session/home?key_id=FB-0000&version={__version__}&tool=CODEX&os={detect_os()}&model=gpt-5.5&full_session_name=sh-home-e0d10000",
        )

    def test_ws_url_reports_producer_target_group(self):
        cfg = load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_ai", SH_TARGET_BOT="bot-test"))
        self.assertTrue(cfg.producer)
        self.assertEqual(cfg.target_group, "oc_ai")
        self.assertEqual(cfg.target_bot, "bot-test")
        self.assertEqual(cfg.ws_url, f"ws://127.0.0.1:8791/ws/session/home?key_id=FB-0000&version={__version__}&os={detect_os()}&producer=1&target_group=oc_ai&target_bot=bot-test")

    def test_ws_url_reports_mirror_to(self):
        cfg = load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_u1#FB-0000#ExampleConnector"))
        self.assertEqual(cfg.ws_url, f"ws://127.0.0.1:8791/ws/session/home?key_id=FB-0000&version={__version__}&os={detect_os()}&mirror_to=ou_u1%23FB-0000%23ExampleConnector")

    def test_ws_url_reports_no_directory(self):
        cfg = load_config(dict(BASE_ENV, SH_NO_DIRECTORY="1"))
        self.assertTrue(cfg.no_directory)
        self.assertEqual(cfg.ws_url, f"ws://127.0.0.1:8791/ws/session/home?key_id=FB-0000&version={__version__}&os={detect_os()}&no_directory=1")

    def test_detect_full_session_name_prefers_tmux_session(self):
        with mock.patch("sessionhelper.config.subprocess.check_output", return_value="sh-developer-e0d10000\n"):
            got = detect_full_session_name("developer", dict(BASE_ENV, TMUX="/tmp/tmux-1000/default,1,0", SH_SESSION_FULL="fallback"))
        self.assertEqual(got, "sh-developer-e0d10000")

    def test_detect_full_session_name_falls_back_to_env_then_short_name(self):
        self.assertEqual(detect_full_session_name("developer", dict(BASE_ENV, SH_SESSION_FULL="sh-developer-e0d10000")), "sh-developer-e0d10000")
        self.assertEqual(detect_full_session_name("developer", dict(BASE_ENV)), "developer")

    def test_cli_launch_env_and_tmux_session_name(self):
        env = dict(BASE_ENV, SH_CLI_LAUNCH="tmux")
        cfg = load_config(env)
        self.assertEqual(cfg.cli_launch, "tmux")
        name = tmux_session_name("home/dev", "FB-0000-key-0000")
        self.assertTrue(name.startswith("sh-home-dev-"))
        self.assertIn("key-0000", name)

    def test_opencode_profile_uses_sqlite_reply_source(self):
        cfg = load_config(dict(BASE_ENV, SH_CLI="opencode", SH_OPENCODE_DB="/tmp/opencode.db"))
        self.assertEqual(cfg.opencode_db, "/tmp/opencode.db")
        self.assertEqual(CLI_PROFILES["opencode"].output_source, "opencode_db")
        self.assertEqual(CLI_PROFILES["opencode"].launch, "pty")
        self.assertEqual(CLI_PROFILES["claude"].launch, "pty")
        self.assertEqual(CLI_PROFILES["codex"].launch, "pty")

    def test_cli_launch_failure_returns_not_ready_reply(self):
        cfg = load_config(
            dict(
                BASE_ENV,
                SH_MODE="cli",
                SH_CLI="claude",
                SH_CLI_LAUNCH="pty",
                SH_CLI_CMD="false",
                SH_CLI_READY_TIMEOUT="0.2",
                SH_CLI_LAUNCH_RETRIES="1",
            )
        )
        reply = CLIAdapter(cfg).handle({"body": "hello"})
        self.assertIn("CLI未就绪", reply)

    def test_cli_async_reply_transcript_confirms_user_without_sync_reply(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="claude", SH_ASYNC_REPLY="1"))
        self.assertTrue(cfg.async_reply)
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        adapter.transcript = object()
        calls = {"confirm": 0, "collect": 0}
        adapter.start = lambda: True

        def fake_confirm(prompt):
            calls["confirm"] += 1
            self.assertEqual(prompt, "长耗时任务")
            return "ok"

        def fake_collect(_prompt):
            calls["collect"] += 1
            return ("ok", "不应同步返回")

        adapter.inject_and_confirm_transcript_user = fake_confirm
        adapter.inject_and_collect_transcript_reply = fake_collect
        self.assertEqual(adapter.handle({"body": "长耗时任务"}), "")
        self.assertEqual(calls, {"confirm": 1, "collect": 0})

    def test_cli_sync_reply_default_still_collects_transcript_reply(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="claude"))
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        adapter.transcript = object()
        calls = {"confirm": 0, "collect": 0}
        adapter.start = lambda: True
        adapter.inject_and_confirm_transcript_user = lambda _prompt: calls.__setitem__("confirm", calls["confirm"] + 1) or "ok"

        def fake_collect(prompt):
            calls["collect"] += 1
            self.assertEqual(prompt, "普通任务")
            return ("ok", "同步答案")

        adapter.inject_and_collect_transcript_reply = fake_collect
        self.assertEqual(adapter.handle({"body": "普通任务"}), "同步答案")
        self.assertEqual(calls, {"confirm": 0, "collect": 1})

    def test_cli_async_reply_pty_injects_without_collect_output(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="aider", SH_ASYNC_REPLY="1"))
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        calls = {"inject": 0, "collect": 0}
        adapter.start = lambda: True

        def fake_inject(prompt):
            calls["inject"] += 1
            self.assertEqual(prompt, "异步命令")

        def fake_collect(_wait):
            calls["collect"] += 1
            return ["不应收集"]

        adapter.inject_text = fake_inject
        adapter.collect_output = fake_collect
        self.assertEqual(adapter.handle({"body": "异步命令"}), "")
        self.assertEqual(calls, {"inject": 1, "collect": 0})

    def test_tmux_multiline_injection_uses_bracketed_paste_then_enter(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="aider", SH_TOOL="AIDER"))
        adapter = CLIAdapter(cfg)
        adapter.launch_mode = "tmux"
        calls = []

        def fake_run(args, check=False, **kwargs):
            calls.append(args)
            return subprocess.CompletedProcess(args, 0)

        with mock.patch("sessionhelper.cli.subprocess.run", side_effect=fake_run), mock.patch("sessionhelper.cli.time.sleep", return_value=None):
            adapter.inject_text("第一行\n第二行")

        sent = [call[-1] for call in calls if call[:3] == ["tmux", "send-keys", "-t"] and "-l" in call]
        self.assertEqual(sent, [BRACKETED_PASTE_START, "第一行\n第二行", BRACKETED_PASTE_END])
        self.assertEqual(calls[-1][-1], "Enter")

    def test_pty_multiline_injection_uses_bracketed_paste(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="opencode", SH_TOOL="OPENCODE"))
        adapter = CLIAdapter(cfg)
        adapter.launch_mode = "pty"
        sent = []

        class FakeChild:
            def send(self, text):
                sent.append(text)

        adapter.child = FakeChild()
        with mock.patch("sessionhelper.cli.time.sleep", return_value=None):
            adapter.inject_text("a\nb")
        self.assertEqual(sent, [f"{BRACKETED_PASTE_START}a\nb{BRACKETED_PASTE_END}", "\r"])

    def test_unsupported_multiline_injection_flattens_to_single_submit(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="gemini", SH_TOOL="GEMINI"))
        adapter = CLIAdapter(cfg)
        self.assertEqual(adapter.prepare_input_text("a\n\nb"), "a / b")

    def test_group_reply_targets_chat_and_at_sender(self):
        book = AddressBook("home", "FB-0000", "ExampleBot")
        to, meta = reply_target(
            {
                "from": "ou_alice#FB-0000#ExampleBot",
                "meta": {
                    "chat_type": "group",
                    "group_chat_id": "oc_group",
                    "sender_open_id": "ou_alice",
                },
            },
            book,
        )
        self.assertEqual(to, "oc_group#FB-0000#ExampleBot")
        self.assertEqual(meta, {"at": ["ou_alice"]})

    def test_reply_target_preserves_source_bot_context(self):
        book = AddressBook("home", "FB-0000", "ExampleBot")
        to, meta = reply_target(
            {
                "from": "ou_u1#FB-0000#ExampleBot",
                "meta": {
                    "source_bot_channel_id": "ExampleConnector",
                    "source_chat_type": "personal",
                    "source_open_id": "ou_u1",
                    "source_chat_id": "ou_u1",
                },
            },
            book,
        )
        self.assertEqual(to, "ou_u1#FB-0000#ExampleConnector")
        self.assertEqual(
            meta,
            {
                "source_bot_channel_id": "ExampleConnector",
                "source_chat_type": "personal",
                "source_open_id": "ou_u1",
                "source_chat_id": "ou_u1",
            },
        )

        to, meta = reply_target(
            {
                "from": "ou_sender#FB-0000#ExampleBot",
                "meta": {
                    "source_bot_channel_id": "ExampleConnector",
                    "source_chat_type": "group",
                    "source_chat_id": "oc_team",
                    "source_sender_openid": "ou_sender",
                },
            },
            book,
        )
        self.assertEqual(to, "oc_team#FB-0000#ExampleConnector")
        self.assertEqual(
            meta,
            {
                "source_bot_channel_id": "ExampleConnector",
                "source_chat_type": "group",
                "source_chat_id": "oc_team",
                "source_sender_openid": "ou_sender",
                "at": ["ou_sender"],
            },
        )

    def test_comm_skill_ack_envelope(self):
        book = AddressBook("home", "FB-0000", "ExampleBot")
        self.assertTrue(contains_comm_skill_ack(f"ok {COMM_SKILL_ACK}"))
        env = comm_skill_ack_envelope(book)
        self.assertEqual(env["to"], "workpulse#FB-0000")
        self.assertEqual(env["from"], "home#FB-0000")
        self.assertEqual(env["body"], COMM_SKILL_ACK)
        self.assertEqual(env["meta"]["type"], "agent_network_skill_ack")

    def test_agent_route_envelope_accepts_selector_output(self):
        book = AddressBook("home", "FB-0000", "ExampleBot")
        env = agent_route_envelope("#developer 请核对X", book)
        self.assertEqual(env["to"], "#developer")
        self.assertEqual(env["from"], "home#FB-0000")
        self.assertEqual(env["body"], "请核对X")
        self.assertEqual(env["meta"]["type"], "agent_route")

        env = agent_route_envelope("@u2#tester 请复测", book)
        self.assertEqual(env["to"], "@u2#tester")
        self.assertEqual(env["body"], "请复测")

        self.assertIsNone(agent_route_envelope("普通回答", book))
        self.assertIsNone(agent_route_envelope("# 中文标题", book))

    def test_agent_network_skill_injects_without_handle(self):
        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.injected = []
                self.handled = []

            def start(self):
                return True

            def inject_text(self, text):
                self.injected.append(text)

            def handle(self, env):
                self.handled.append(env)
                return "should not happen"

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        asyncio.run(helper.inject_agent_network_skill({"body": "指南", "meta": {"type": "agent_network_skill"}}))
        self.assertEqual(fake.injected, ["指南"])
        self.assertEqual(fake.handled, [])

    def test_terminal_input_writes_cli_adapter(self):
        class FakeAdapter:
            def __init__(self):
                self.inputs = []

            def write_terminal_input(self, data):
                self.inputs.append(data)

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        self.assertTrue(helper.is_terminal_input({"meta": {"type": "terminal_input"}}))
        asyncio.run(helper.handle_terminal_input({"body": "abc\r", "meta": {"type": "terminal_input"}}))
        self.assertEqual(fake.inputs, ["abc\r"])

    def test_terminal_resize_sets_cli_winsize(self):
        class FakeAdapter:
            def __init__(self):
                self.sizes = []

            def set_winsize(self, rows, cols):
                self.sizes.append((rows, cols))

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        env = {"meta": {"type": "terminal_resize", "rows": 43, "cols": 132}}
        self.assertTrue(helper.is_terminal_resize(env))
        asyncio.run(helper.handle_terminal_resize(env))
        self.assertEqual(fake.sizes, [(43, 132)])

    def test_terminal_refresh_sends_snapshot(self):
        class FakeAdapter:
            def request_terminal_refresh(self):
                return "current screen"

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        helper.adapter = FakeAdapter()
        ws = FakeWS()
        env = {"meta": {"type": "terminal_refresh"}}
        self.assertTrue(helper.is_terminal_refresh(env))
        asyncio.run(helper.handle_terminal_refresh(ws, env))
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["body"], "current screen")
        self.assertEqual(ws.sent[0]["meta"]["type"], "terminal_output")


    def test_buffer_if_busy_always_enqueues_in_cli_mode_and_never_returns_false(self):
        """Recv-loop hang fix (#4): buffer_if_busy must always return True in CLI
        mode so recv_loop never calls the blocking process_inbound_message directly."""
        class FakeAdapter:
            call_count = 0
            def is_idle(self):
                self.call_count += 1
                return self.call_count <= 2  # idle for first 2 calls, then "busy"

        class FakeWS:
            def __init__(self):
                self.sent = []
            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
            helper.adapter = FakeAdapter()
            ws = FakeWS()
            env = {"body": "test", "from": "alice#FB-0000#ExampleBot",
                   "meta": {"type": "inbound"}}
            results = []
            for _ in range(5):
                results.append(await helper.buffer_if_busy(ws, env))
            return results, helper, ws

        results, helper, ws = asyncio.run(run())
        self.assertTrue(all(results), "buffer_if_busy must always return True in CLI mode")
        self.assertEqual(len(helper.pending_inbound), 5)
        # busy ack only sent once per sender (dedup via busy_acked_from), at most 1 expected

    def test_recv_loop_filters_online_directory_without_handle(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self):
                self.handled = []

            def handle(self, env):
                self.handled.append(env)
                return "should not run"

        body = "\n**********\n【DingWei在线清单】同账号在线AI会话\n1. #home\n**********\n"
        self.assertTrue(is_online_directory_text(body))
        env = {"from": "workpulse#FB-0000", "to": "home#FB-0000", "body": body, "meta": {"type": "online_directory", "no_mirror": True}}
        self.assertTrue(is_no_mirror_envelope(env))
        helper = SessionHelper(load_config(BASE_ENV))
        fake = FakeAdapter()
        helper.adapter = fake
        ws = FakeWS(env)

        asyncio.run(helper.recv_loop(ws))

        self.assertEqual(fake.handled, [])
        self.assertEqual(ws.sent, [])

    def test_recv_loop_duplicate_agent_skill_acks_without_reinject(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.injected = []

            def start(self):
                return True

            def inject_text(self, text):
                self.injected.append(text)

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        helper.comm_skill_installed = True
        ws = FakeWS({"from": "workpulse#FB-0000", "to": "home#FB-0000", "body": "指南", "meta": {"type": "agent_network_skill"}})

        asyncio.run(helper.recv_loop(ws))

        self.assertEqual(fake.injected, [])
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["meta"]["type"], "agent_network_skill_ack")

    def test_busy_buffer_queues_busy_messages_and_drains_fifo(self):
        class FakeWS:
            def __init__(self, envs):
                self.envs = list(envs)
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if not self.envs:
                    raise StopAsyncIteration
                return json.dumps(self.envs.pop(0), ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.idle = False
                self.handled = []

            def is_idle(self):
                return self.idle

            def handle(self, env):
                self.handled.append(env["body"])
                return ""

        envs = [
            {"from": "peer#FB-0000", "to": "home#FB-0000", "body": "one", "meta": {}},
            {"from": "peer#FB-0000", "to": "home#FB-0000", "body": "two", "meta": {}},
        ]
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli", SH_BUSY_BUFFER_MAX="10", SH_CLI_SETTLE_SECONDS="0.1")))
        fake = FakeAdapter()
        helper.adapter = fake
        ws = FakeWS(envs)
        asyncio.run(helper.recv_loop(ws))
        self.assertEqual(fake.handled, [])
        self.assertEqual([item["body"] for item in helper.pending_inbound], ["one", "two"])
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["to"], "peer#FB-0000")
        self.assertIn("忙", ws.sent[0]["body"])

        async def drain_once():
            fake.idle = True
            task = asyncio.create_task(helper.pending_drain_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.pending_inbound:
                await asyncio.sleep(0.05)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass

        asyncio.run(drain_once())
        self.assertEqual(fake.handled, ["one", "two"])
        self.assertEqual(len(helper.pending_inbound), 0)
        self.assertEqual(helper.busy_acked_from, set())

    def test_busy_buffer_drops_oldest_when_full_and_skips_special_messages(self):
        class FakeAdapter:
            name = "cli"

            def is_idle(self):
                return False

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli", SH_BUSY_BUFFER_MAX="2")))
        helper.adapter = FakeAdapter()
        ws = FakeWS()

        async def run_case():
            for body in ["one", "two", "three"]:
                await helper.buffer_if_busy(ws, {"from": "peer#FB-0000", "to": "home#FB-0000", "body": body, "meta": {}})

        asyncio.run(run_case())
        self.assertEqual([item["body"] for item in helper.pending_inbound], ["two", "three"])
        self.assertEqual(len(ws.sent), 1)

    def test_delivery_worker_busy_acks_once_and_defers_injection(self):
        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.idle = False
                self.handled = []

            def is_idle(self):
                return self.idle

            def handle(self, env):
                self.handled.append(env["body"])
                return ""

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            with tempfile.TemporaryDirectory() as tmp:
                helper = SessionHelper(load_config(dict(
                    BASE_ENV, SH_MODE="cli", SH_INBOX_DB=str(Path(tmp) / "inbox.db")
                )))
                fake = FakeAdapter()
                helper.adapter = fake
                ws = FakeWS()
                env = {
                    "from": "peer#FB-0000",
                    "to": "home#FB-0000",
                    "body": "one",
                    "meta": {"type": "delivery", "ack_required": True, "delivery_id": "dlv_1"},
                }
                helper.delivery_inbox.accept("dlv_1", env)
                task = asyncio.create_task(helper.delivery_worker_loop(ws))
                deadline = asyncio.get_running_loop().time() + 1.5
                while asyncio.get_running_loop().time() < deadline and not ws.sent:
                    await asyncio.sleep(0.05)
                await asyncio.sleep(0.1)
                busy_sent = list(ws.sent)
                fake.idle = True
                deadline = asyncio.get_running_loop().time() + 1.5
                while asyncio.get_running_loop().time() < deadline and len(ws.sent) < 2:
                    await asyncio.sleep(0.05)
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass
                return busy_sent, ws.sent, fake.handled

        busy_sent, sent, handled = asyncio.run(run_case())
        self.assertEqual(len(busy_sent), 1)
        self.assertEqual(busy_sent[0]["meta"]["type"], "delivery_ack")
        self.assertEqual(busy_sent[0]["meta"]["state"], "busy")
        self.assertEqual(busy_sent[0]["meta"]["reason"], "cli_busy")
        self.assertEqual(handled, ["one"])
        self.assertEqual(sent[-1]["meta"]["state"], "processed")

    def test_delivery_worker_recovers_when_injection_failure_turns_busy(self):
        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.idle = True
                self.calls = 0
                self.handled = []

            def is_idle(self):
                return self.idle

            def handle(self, env):
                self.calls += 1
                if self.calls == 1:
                    self.idle = False
                    raise RuntimeError("cli busy during injection")
                self.handled.append(env["body"])
                return ""

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            with tempfile.TemporaryDirectory() as tmp:
                helper = SessionHelper(load_config(dict(
                    BASE_ENV, SH_MODE="cli", SH_INBOX_DB=str(Path(tmp) / "inbox.db")
                )))
                fake = FakeAdapter()
                helper.adapter = fake
                ws = FakeWS()
                env = {
                    "from": "peer#FB-0000",
                    "to": "home#FB-0000",
                    "body": "one",
                    "meta": {"type": "delivery", "ack_required": True, "delivery_id": "dlv_1"},
                }
                helper.delivery_inbox.accept("dlv_1", env)
                task = asyncio.create_task(helper.delivery_worker_loop(ws))
                deadline = asyncio.get_running_loop().time() + 1.5
                while asyncio.get_running_loop().time() < deadline and not ws.sent:
                    await asyncio.sleep(0.05)
                busy_sent = list(ws.sent)
                fake.idle = True
                deadline = asyncio.get_running_loop().time() + 1.5
                while asyncio.get_running_loop().time() < deadline and len(ws.sent) < 2:
                    await asyncio.sleep(0.05)
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass
                return busy_sent, ws.sent, fake.handled

        busy_sent, sent, handled = asyncio.run(run_case())
        self.assertEqual(len(busy_sent), 1)
        self.assertEqual(busy_sent[0]["meta"]["state"], "busy")
        self.assertEqual(busy_sent[0]["meta"]["reason"], "cli_busy")
        self.assertNotIn("failed", [item["meta"]["state"] for item in sent])
        self.assertEqual(handled, ["one"])
        self.assertEqual(sent[-1]["meta"]["state"], "processed")

    def test_delivery_worker_high_frequency_busy_window_processes_all(self):
        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.idle = False
                self.handled = []
                self.failed_once = set()
                self.fail_during_handle = {"dlv_2", "dlv_5"}

            def is_idle(self):
                return self.idle

            def handle(self, env):
                delivery_id = env["meta"]["delivery_id"]
                if delivery_id in self.fail_during_handle and delivery_id not in self.failed_once:
                    self.failed_once.add(delivery_id)
                    self.idle = False
                    raise RuntimeError("cli busy during injection")
                self.handled.append(delivery_id)
                return ""

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            with tempfile.TemporaryDirectory() as tmp:
                helper = SessionHelper(load_config(dict(
                    BASE_ENV, SH_MODE="cli", SH_INBOX_DB=str(Path(tmp) / "inbox.db")
                )))
                fake = FakeAdapter()
                helper.adapter = fake
                ws = FakeWS()
                for i in range(8):
                    delivery_id = f"dlv_{i}"
                    env = {
                        "from": "peer#FB-0000",
                        "to": "home#FB-0000",
                        "body": f"body-{i}",
                        "meta": {"type": "delivery", "ack_required": True, "delivery_id": delivery_id},
                    }
                    helper.delivery_inbox.accept(delivery_id, env)
                task = asyncio.create_task(helper.delivery_worker_loop(ws))
                deadline = asyncio.get_running_loop().time() + 6
                busy_seen = 0
                while asyncio.get_running_loop().time() < deadline:
                    states = [item["meta"]["state"] for item in ws.sent]
                    if states.count("busy") > busy_seen:
                        busy_seen = states.count("busy")
                        fake.idle = True
                    if states.count("processed") == 8:
                        break
                    await asyncio.sleep(0.05)
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass
                return ws.sent, fake.handled

        sent, handled = asyncio.run(run_case())
        states = [item["meta"]["state"] for item in sent]
        self.assertNotIn("failed", states)
        self.assertEqual(states.count("processed"), 8)
        self.assertGreaterEqual(states.count("busy"), 3)
        self.assertEqual(sorted(handled), [f"dlv_{i}" for i in range(8)])

    def test_delivery_worker_keeps_true_injection_failure_finite(self):
        class FakeAdapter:
            name = "cli"

            def is_idle(self):
                return True

            def handle(self, env):
                raise RuntimeError("process gone")

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            with tempfile.TemporaryDirectory() as tmp:
                helper = SessionHelper(load_config(dict(
                    BASE_ENV, SH_MODE="cli", SH_INBOX_DB=str(Path(tmp) / "inbox.db")
                )))
                helper.adapter = FakeAdapter()
                ws = FakeWS()
                env = {
                    "from": "peer#FB-0000",
                    "to": "home#FB-0000",
                    "body": "one",
                    "meta": {"type": "delivery", "ack_required": True, "delivery_id": "dlv_1"},
                }
                helper.delivery_inbox.accept("dlv_1", env)
                task = asyncio.create_task(helper.delivery_worker_loop(ws))
                deadline = asyncio.get_running_loop().time() + 1.5
                while asyncio.get_running_loop().time() < deadline and not ws.sent:
                    await asyncio.sleep(0.05)
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass
                return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0]["meta"]["state"], "failed")
        self.assertEqual(sent[0]["meta"]["reason"], "cli_injection_failed")
        self.assertTrue(sent[0]["meta"]["retryable"])

    def test_provision_rejects_bad_source_and_audits(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            result = Provisioner(cfg).handle(
                {
                    "from": "attacker#FB-0000",
                    "to": "home#FB-0000",
                    "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://localhost/x", "sha256": "0" * 64},
                }
            )
            self.assertFalse(result.ok)
            self.assertIn("source denied", result.message)
            with sqlite3.connect(Path(tmp) / "audit.db") as conn:
                rows = conn.execute("SELECT action, ok, source FROM provision_audit").fetchall()
            self.assertEqual(rows, [("install_skill", 0, "attacker#FB-0000")])

    def test_provision_validates_host_and_sha_before_download(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            base = {
                "from": "workpulse#FB-0000",
                "to": "home#FB-0000",
                "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://evil.example/x", "sha256": "0" * 64},
            }
            result = Provisioner(cfg).handle(base)
            self.assertFalse(result.ok)
            self.assertIn("denied", result.message)
            evil = json.loads(json.dumps(base))
            evil["meta"]["url"] = "https://evil.example.com/x.tar"
            result = Provisioner(cfg).handle(evil)
            self.assertFalse(result.ok)
            self.assertIn("denied", result.message)
            bad_sha = json.loads(json.dumps(base))
            bad_sha["meta"]["url"] = "https://localhost/x"
            bad_sha["meta"]["sha256"] = "abc"
            result = Provisioner(cfg).handle(bad_sha)
            self.assertFalse(result.ok)
            self.assertIn("invalid sha256", result.message)

    def test_provision_idempotent_version_skips_download(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            provisioner.version_file = Path(tmp) / "versions.json"
            provisioner.record_version("install_skill", "demo", "2.0")
            env = {
                "from": "workpulse#FB-0000",
                "to": "home#FB-0000",
                "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "demo", "version": "1.0", "url": "https://localhost/x", "sha256": "0" * 64},
            }
            with mock.patch.object(provisioner, "download_and_verify", side_effect=AssertionError("should not download")):
                result = provisioner.handle(env)
            self.assertTrue(result.ok)
            self.assertEqual(result.message, "already installed")

    def test_provision_install_skill_from_verified_tar(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            home.mkdir()
            artifact = Path(tmp) / "skill.tar"
            src = Path(tmp) / "src"
            src.mkdir()
            (src / "SKILL.md").write_text("demo skill", encoding="utf-8")
            with tarfile.open(artifact, "w") as tf:
                tf.add(src / "SKILL.md", arcname="SKILL.md")
            cfg = load_config(dict(BASE_ENV, SH_CLI="codex", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "CODEX_HOME": str(home / ".codex")}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-0000",
                    "to": "home#FB-0000",
                    "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "demo", "version": "1.0", "url": "https://localhost/skill.tar", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                self.assertEqual((home / ".codex" / "skills" / "demo" / "SKILL.md").read_text(encoding="utf-8"), "demo skill")

    def test_provision_install_mcp_merges_json_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            home.mkdir()
            artifact = Path(tmp) / "mcp.json"
            artifact.write_text(json.dumps({"command": "demo-mcp", "args": ["--stdio"], "env": {"TOKEN": "${DEMO_TOKEN}"}}), encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_CLI="claude", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home)}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-0000",
                    "to": "home#FB-0000",
                    "meta": {"type": "provision", "system": True, "action": "install_mcp", "target": "demo", "version": "1.0", "url": "https://localhost/mcp.json", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                data = json.loads((home / ".claude.json").read_text(encoding="utf-8"))
                self.assertEqual(data["mcpServers"]["demo"]["command"], "demo-mcp")
                self.assertEqual(data["mcpServers"]["demo"]["env"]["TOKEN"], "${DEMO_TOKEN}")

    def test_provision_install_mcp_writes_codex_toml_snippet(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            codex_home = home / ".codex"
            home.mkdir()
            artifact = Path(tmp) / "mcp.json"
            artifact.write_text(json.dumps({"mcpServers": {"demo": {"command": "demo-mcp", "args": ["--stdio"]}}}), encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_CLI="codex", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "CODEX_HOME": str(codex_home)}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-0000",
                    "to": "home#FB-0000",
                    "meta": {"type": "provision", "system": True, "action": "install_mcp", "target": "demo", "version": "1.0", "url": "https://localhost/mcp.json", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                text = (codex_home / "config.toml").read_text(encoding="utf-8")
                self.assertIn('# dingwei-mcp:demo:1.0', text)
                self.assertIn('[mcp_servers."demo"]', text)
                self.assertIn('command = "demo-mcp"', text)

    def test_provision_download_requires_matching_sha256(self):
        class FakeResponse:
            def __init__(self, chunks):
                self.chunks = list(chunks)

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _size):
                if not self.chunks:
                    return b""
                return self.chunks.pop(0)

        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            expected = hashlib.sha256(b"abcdef").hexdigest()
            with mock.patch("sessionhelper.provision.urllib.request.urlopen", return_value=FakeResponse([b"abc", b"def"])):
                path = provisioner.download_and_verify("https://localhost/artifact", expected)
            self.assertEqual(path.read_bytes(), b"abcdef")
            with mock.patch("sessionhelper.provision.urllib.request.urlopen", return_value=FakeResponse([b"bad"])):
                with self.assertRaises(Exception):
                    provisioner.download_and_verify("https://localhost/artifact", expected)

    def test_provision_rejects_tar_escape_and_links(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            escape_tar = Path(tmp) / "escape.tar"
            with tarfile.open(escape_tar, "w") as tf:
                info = tarfile.TarInfo("../escape.txt")
                payload = b"x"
                info.size = len(payload)
                tf.addfile(info, io.BytesIO(payload))
            with self.assertRaises(Exception):
                provisioner.extract_package(escape_tar, Path(tmp) / "out")

            link_tar = Path(tmp) / "link.tar"
            with tarfile.open(link_tar, "w") as tf:
                info = tarfile.TarInfo("link")
                info.type = tarfile.SYMTYPE
                info.linkname = "/tmp/escape"
                tf.addfile(info)
            with self.assertRaises(Exception):
                provisioner.extract_package(link_tar, Path(tmp) / "out2")

    def test_provision_update_self_rolls_back_stale_pending_update(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            root = Path(tmp) / "sessionhelper"
            backup = Path(tmp) / "backup"
            home.mkdir()
            root.mkdir()
            backup.mkdir()
            (root / "app.py").write_text("new", encoding="utf-8")
            (backup / "app.py").write_text("old", encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "SH_PROVISION_NO_EXIT": "1"}):
                provisioner = Provisioner(cfg, package_root=str(root))
                provisioner.update_state_file.parent.mkdir(parents=True, exist_ok=True)
                provisioner.update_state_file.write_text(
                    json.dumps({"version": "future", "backup": str(backup), "status": "pending", "ts": int(time.time()) - 100}),
                    encoding="utf-8",
                )
                self.assertTrue(provisioner.rollback_stale_update_if_needed(timeout_seconds=1))
                self.assertEqual((root / "app.py").read_text(encoding="utf-8"), "old")

    def test_provision_restart_strategy_is_platform_specific(self):
        cfg = load_config(BASE_ENV)
        provisioner = Provisioner(cfg)
        with mock.patch("sessionhelper.provision.detect_os", return_value="linux"):
            self.assertEqual(provisioner.restart_strategy(), "cron_guard")
        with mock.patch("sessionhelper.provision.detect_os", return_value="macos"):
            self.assertEqual(provisioner.restart_strategy(), "launchd")

    def test_provision_ack_is_sent_from_recv_loop(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        helper = SessionHelper(load_config(BASE_ENV))
        env = {"from": "workpulse#FB-0000", "to": "home#FB-0000", "body": "", "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://evil.example/x", "sha256": "0" * 64}}
        ws = FakeWS(env)
        asyncio.run(helper.recv_loop(ws))
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["to"], "workpulse#FB-0000")
        self.assertEqual(ws.sent[0]["meta"]["type"], "provision_ack")
        self.assertFalse(ws.sent[0]["meta"]["ok"])

    def test_compare_versions_handles_numbers_and_hashes(self):
        self.assertGreater(compare_versions("2.0", "1.9"), 0)
        self.assertLess(compare_versions("abc", "bcd"), 0)

    def test_mirror_loop_sends_comm_skill_ack_on_marker(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self):
                self.events = [("assistant", f"收到 {COMM_SKILL_ACK}")]

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_COLLECT="1", SH_MIRROR_TO="ou_u1#FB-0000#ExampleBot")))
            helper.adapter = FakeAdapter()
            ws = FakeWS()
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and not ws.sent:
                await asyncio.sleep(0.05)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0]["to"], "workpulse#FB-0000")
        self.assertEqual(sent[0]["body"], COMM_SKILL_ACK)
        self.assertEqual(sent[0]["meta"]["type"], "agent_network_skill_ack")

    def test_mirror_control_updates_state_without_adapter(self):
        helper = SessionHelper(load_config(BASE_ENV))
        env = {
            "meta": {
                "type": "mirror_control",
                "enabled": True,
                "mirror_to": "oc_group#FB-0000#ExampleBot",
            }
        }
        self.assertTrue(is_mirror_control(env))
        helper.apply_mirror_control(env)
        self.assertTrue(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "oc_group#FB-0000#ExampleBot")

    def test_mirror_control_without_target_keeps_configured_mirror_to(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_u1#FB-0000#ExampleConnector")))
        helper.apply_mirror_control({"meta": {"type": "mirror_control", "enabled": True}})
        self.assertTrue(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "ou_u1#FB-0000#ExampleConnector")

        helper.apply_mirror_control({"meta": {"type": "mirror_control", "enabled": False, "mirror_to": ""}})
        self.assertFalse(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "")

    def test_online_directory_broadcast_is_filtered_from_cli(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self, events=()):
                self.seen = []
                self.events = list(events)

            def handle(self, env):
                self.seen.append(env.get("body"))
                return ""

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_mirror(helper, ws):
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.adapter.events:
                await asyncio.sleep(0.05)
            await asyncio.sleep(0.35)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass

        async def run_case():
            helpers = []
            for i, session in enumerate(["home", "developer", "review"]):
                env = {
                    "from": "workpulse#FB-0000",
                    "to": f"{session}#FB-0000",
                    "body": "系统广播内容",
                    "meta": {
                        "type": "online_directory",
                        "system": True,
                        "no_mirror": True,
                        "broadcast_dedup_key": "broadcast:online_directory:alice:m1",
                        "mirror_primary": i == 0,
                    },
                }
                cfg = load_config(dict(BASE_ENV, SH_SESSION_NAME=session, SH_MIRROR_TO="ou_alice#FB-0000#ExampleBot", SH_COLLECT="0"))
                helper = SessionHelper(cfg)
                helper.adapter = FakeAdapter()
                recv_ws = FakeWS(env)
                await helper.recv_loop(recv_ws)
                helpers.append((helper, recv_ws))
            mirror_sends = []
            for helper, _recv_ws in helpers:
                mirror_ws = FakeWS({})
                await run_mirror(helper, mirror_ws)
                mirror_sends.extend(mirror_ws.sent)
            return [helper.adapter.seen for helper, _ in helpers], mirror_sends

        injected, mirrored = asyncio.run(run_case())
        self.assertEqual(injected, [[], [], []])
        self.assertEqual(mirrored, [])

    def test_is_broadcast_envelope_requires_dedup_key(self):
        env = {
            "body": "广播内容",
            "meta": {"type": "online_directory", "broadcast_dedup_key": "broadcast:1"},
        }
        self.assertTrue(is_broadcast_envelope(env))
        self.assertFalse(is_broadcast_envelope({"body": "点对点", "meta": {"system": True, "no_mirror": True}}))

    def test_mirror_loop_keeps_point_to_point_and_ai_output_mirrors(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self, events):
                self.events = list(events)

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_one(events, remembered=""):
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_alice#FB-0000#ExampleBot", SH_COLLECT="0")))
            if remembered:
                helper.remember_broadcast_mirror_decision(
                    {"body": remembered, "meta": {"broadcast_dedup_key": "broadcast:test", "mirror_primary": False}}
                )
            helper.adapter = FakeAdapter(events)
            ws = FakeWS()
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.adapter.events:
                await asyncio.sleep(0.05)
            await asyncio.sleep(0.35)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
            return ws.sent

        point_to_point = asyncio.run(run_one([("user", "点对点输入")]))
        ai_output = asyncio.run(run_one([("claude", "各自 AI 输出")], remembered="广播输入"))
        mirrored_normal = asyncio.run(run_one([("claude", "正常任务回复")]))

        self.assertEqual(len(point_to_point), 1)
        self.assertIn("点对点输入", point_to_point[0]["body"])
        self.assertEqual(len(ai_output), 1)
        self.assertIn("各自 AI 输出", ai_output[0]["body"])
        self.assertEqual(len(mirrored_normal), 1)
        self.assertEqual(mirrored_normal[0]["to"], "ou_alice#FB-0000#ExampleBot")
        self.assertIn("正常任务回复", mirrored_normal[0]["body"])

    def test_producer_envelope_targets_group_and_marks_no_mirror(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group", SH_TARGET_BOT="bot-test")))
        env = helper.producer_envelope("hello", role="alert")
        self.assertEqual(env["to"], "oc_group#FB-0000#ExampleBot")
        self.assertEqual(env["from"], "home#FB-0000")
        self.assertEqual(env["body"], "hello")
        self.assertEqual(env["meta"]["producer"], True)
        self.assertEqual(env["meta"]["target_group"], "oc_group")
        self.assertEqual(env["meta"]["target_bot"], "bot-test")
        self.assertEqual(env["meta"]["role"], "alert")
        self.assertEqual(env["meta"]["no_mirror"], True)

    def test_producer_requires_target_group(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1")))
        with self.assertRaises(RuntimeError):
            helper.producer_envelope("hello")

    def test_producer_stdin_loop_sends_lines_to_group(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            ws = FakeWS()
            lines = iter(["hello\n", ""])

            async def fake_to_thread(_func):
                return next(lines)

            with mock.patch("sessionhelper.app.asyncio.to_thread", side_effect=fake_to_thread):
                await helper.producer_stdin_loop(ws)
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0]["to"], "oc_group#FB-0000#ExampleBot")
        self.assertEqual(sent[0]["body"], "hello")
        self.assertEqual(sent[0]["meta"]["no_mirror"], True)

    def test_producer_stdin_loop_sends_multiple_lines_then_exits_on_eof(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            ws = FakeWS()
            lines = iter(["one\n", "\n", "two\n", ""])

            async def fake_to_thread(_func):
                return next(lines)

            with mock.patch("sessionhelper.app.asyncio.to_thread", side_effect=fake_to_thread):
                await helper.producer_stdin_loop(ws)
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual([item["body"] for item in sent], ["one", "two"])

    def test_producer_run_forever_exits_after_one_run_once(self):
        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            calls = {"n": 0}

            async def fake_run_once():
                calls["n"] += 1

            helper._run_once = fake_run_once
            class FakeWebsockets:
                ConnectionClosedError = RuntimeError

            with mock.patch.dict("sys.modules", {"websockets": FakeWebsockets}):
                await helper.run_forever()
            return calls["n"]

        self.assertEqual(asyncio.run(run_case()), 1)

    def test_reply_body_uses_cross_member_prefix(self):
        helper = SessionHelper(load_config(BASE_ENV))
        body = helper.reply_body({"meta": {"reply_prefix": "【UserTwo·u2】"}}, "干净答案")
        self.assertEqual(body, "【UserTwo·u2】干净答案")

    def test_provider_defaults_include_required_names(self):
        for name in ["deepseek", "qwen", "kimi", "minimax", "glm", "openai", "claude", "gemini"]:
            self.assertIn(name, PROVIDERS)
            self.assertTrue(PROVIDERS[name].base_url)
            self.assertTrue(PROVIDERS[name].model)

    def test_claude_transcript_parser_extracts_clean_text(self):
        event = {
            "type": "assistant",
            "message": {"content": [{"type": "text", "text": "干净答案"}]},
        }
        self.assertEqual(parse_claude_event(event), ("claude", "干净答案"))

    def test_codex_transcript_parser_extracts_agent_message(self):
        event = {
            "type": "event_msg",
            "payload": {"type": "agent_message", "message": "codex answer"},
        }
        self.assertEqual(parse_codex_event(event), ("codex", "codex answer"))

    def test_codex_transcript_parser_extracts_user_message(self):
        event = {
            "type": "response_item",
            "payload": {
                "type": "message",
                "role": "user",
                "content": [{"type": "input_text", "text": "hello codex"}],
            },
        }
        self.assertEqual(parse_codex_event(event), ("user", "hello codex"))

    def test_transcript_tailer_reads_only_new_events(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "session.jsonl"
            path.write_text(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "old"}]}}) + "\n")
            tailer = TranscriptTailer("claude", path)
            with path.open("a", encoding="utf-8") as f:
                f.write(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "new"}]}}) + "\n")
            self.assertEqual(tailer.read_events(), [("claude", "new")])

    def test_transcript_tailer_can_read_existing_events_for_new_session(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "session.jsonl"
            path.write_text(json.dumps({"type": "user", "message": {"content": "hello"}}) + "\n")
            tailer = TranscriptTailer("claude", path, from_end=False)
            self.assertEqual(tailer.read_events(), [("user", "hello")])

    def test_cli_reinjects_when_transcript_user_turn_missing(self):
        cfg = load_config(
            dict(
                BASE_ENV,
                SH_MODE="cli",
                SH_CLI="claude",
                SH_CLI_USER_ACK_WAIT="0.01",
                SH_CLI_REPLY_WAIT="0.01",
            )
        )
        adapter = CLIAdapter(cfg)
        counter = {"injects": 0}

        class FakeTranscript:
            sent = False

            def read_events(self):
                if counter["injects"] < 2:
                    return []
                if self.sent:
                    return []
                self.sent = True
                return [("user", "冷启动第一条"), ("claude", "干净答案")]

        adapter.transcript = FakeTranscript()
        adapter.inject_text = lambda text: counter.__setitem__("injects", counter["injects"] + 1)
        adapter.transcript_retry_delay = lambda attempt: 0

        status, reply = adapter.inject_and_collect_transcript_reply("冷启动第一条")
        self.assertEqual((status, reply), ("ok", "干净答案"))
        self.assertEqual(counter["injects"], 2)

    def test_opencode_db_reader_collects_finished_text_reply(self):
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "opencode.db"
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                CREATE TABLE message(id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT);
                CREATE TABLE part(id TEXT, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
                """
            )
            now = 1_783_000_000_000
            conn.execute(
                "INSERT INTO message VALUES(?,?,?,?,?)",
                ("m-user", "sess-1", now, now, json.dumps({"role": "user"})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-user", "m-user", "sess-1", now, json.dumps({"type": "text", "text": "请总结"})),
            )
            conn.execute(
                "INSERT INTO message VALUES(?,?,?,?,?)",
                ("m-assistant", "sess-1", now + 1000, now + 1000, json.dumps({"role": "assistant", "finish": True})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-reason", "m-assistant", "sess-1", now + 1001, json.dumps({"type": "reasoning", "text": "skip"})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-text", "m-assistant", "sess-1", now + 1002, json.dumps({"type": "text", "text": "```markdown\n干净答案\n```"})),
            )
            conn.commit()
            conn.close()

            reader = OpenCodeDBReader(str(db_path))
            session = reader.find_user_message("请总结", now / 1000 - 0.1)
            self.assertEqual(session, ("sess-1", now / 1000))
            self.assertEqual(reader.find_finished_assistant_reply("sess-1", now / 1000), "干净答案")

    def test_opencode_helpers_parse_time_and_strip_fence(self):
        self.assertEqual(opencode_time(1_783_000_000_000), 1_783_000_000)
        self.assertEqual(strip_markdown_code_fence("```go\nfmt.Println(1)\n```"), "fmt.Println(1)")

    def test_slug_cwd_is_stable_path_fragment(self):
        self.assertIn("tmp-demo", slug_cwd("/tmp/demo"))

    def test_fresh_jsonl_rejects_old_first_timestamp(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "old.jsonl"
            path.write_text(json.dumps({"timestamp": "2026-07-01T00:00:00Z", "type": "mode"}) + "\n")
            self.assertEqual(fresh_jsonl([path], 1782867600), [])

    def test_event_time_parses_iso_and_user_text_matches(self):
        self.assertEqual(event_time({"timestamp": "2026-07-01T00:00:00Z"}), 1782864000)
        self.assertTrue(user_text_matches("hello   world", "prefix hello world suffix"))

    def test_run_sh_is_portable_bootstrap(self):
        path = ROOT / "run.sh"
        self.assertTrue(path.exists())
        mode = stat.S_IMODE(path.stat().st_mode)
        self.assertTrue(mode & stat.S_IXUSR, oct(mode))
        subprocess.run(["bash", "-n", str(path)], check=True)
        text = path.read_text(encoding="utf-8")
        for forbidden in ("declare -A", "${var,,}", "sed -i"):
            self.assertNotIn(forbidden, text)
        self.assertIn("chmod 600", text)
        self.assertIn("trap cleanup_tmux EXIT", text)
        self.assertIn("SH_CLI_READY_TIMEOUT=${SH_CLI_READY_TIMEOUT:-90}", text)
        self.assertIn("SH_CLI_REPLY_WAIT=${SH_CLI_REPLY_WAIT:-45}", text)
        self.assertIn("SH_COLLECT=${SH_COLLECT:-1}", text)
        self.assertIn("SH_ASYNC_REPLY=${SH_ASYNC_REPLY:-0}", text)
        self.assertIn("SH_TARGET_GROUP=${SH_TARGET_GROUP:-}", text)
        self.assertIn("SH_TARGET_BOT=${SH_TARGET_BOT:-}", text)
        self.assertIn("SH_PRODUCER=${SH_PRODUCER:-0}", text)
        self.assertIn("SH_SECRET", text)
        self.assertNotIn("PLACEHOLDER_SECRET@", text)
        self.assertNotIn("test-backend.com", text)


if __name__ == "__main__":
    unittest.main()
