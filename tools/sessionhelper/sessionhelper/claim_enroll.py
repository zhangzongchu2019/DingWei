"""Feishu claim enrollment, including durable receipt before acknowledgement.

The human pairing code is transient. The independent poll token never leaves
this installer. Existing credentials and legacy Web approval are preserved.
"""
from __future__ import annotations

import hashlib
from contextlib import contextmanager
import fcntl
import stat
import json
import os
import secrets
import re
import time

from . import __version__
from .enroll import (CREDENTIAL_FILE, EnrollError, EnrollStopped, HubHTTPError,
                     device_fingerprint, load_credential, write_private_file, Credential)

ALPHABET = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
PENDING = "claim-pending.json"


@contextmanager
def claim_operation(state_dir):
    os.makedirs(state_dir, mode=0o700, exist_ok=True)
    directory = os.lstat(state_dir)
    if not stat.S_ISDIR(directory.st_mode) or directory.st_uid != os.geteuid() or directory.st_mode & 0o077:
        raise EnrollError("claim state directory must be owned and private (0700)")
    fd = os.open(os.path.join(state_dir, "claim-operation.lock"),
                 os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        metadata = os.fstat(fd)
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid() or metadata.st_mode & 0o077 or metadata.st_nlink != 1:
            raise EnrollError("claim operation lock must be an owned private regular file")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise EnrollStopped("配对操作正在运行；先取消当前等待或退出安装向导，再操作同一状态目录。") from None
        yield
    finally:
        os.close(fd)


def load_claim_pending(path):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    except FileNotFoundError:
        return None
    except OSError:
        raise EnrollError("cannot open private claim state; preserve it for recovery") from None
    with os.fdopen(fd, encoding="utf-8") as stream:
        metadata = os.fstat(stream.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid() or metadata.st_mode & 0o077 or metadata.st_nlink != 1:
            raise EnrollError("claim state must be an owned private regular file (0600)")
        try:
            raw = stream.read(4097)
            if len(raw) > 4096:
                raise EnrollError("private claim state exceeds its limit; preserve it for recovery")
            pending = json.loads(raw)
        except (OSError, ValueError):
            raise EnrollError("cannot load private claim state") from None
    if not isinstance(pending, dict) or not all(isinstance(pending.get(k), str) and pending[k]
                                               for k in ("claim_id", "poll_token", "code", "owner")):
        raise EnrollError("claim state is incomplete; preserve it for recovery")
    if (not re.fullmatch(r"[0-9a-f]{32}", pending["claim_id"])
            or not re.fullmatch(r"[0-9a-f]{64}", pending["poll_token"])
            or len(pending["code"]) != 12 or any(ch not in ALPHABET for ch in pending["code"])
            or not re.fullmatch(r"[A-Za-z0-9]{2,32}", pending["owner"])):
        raise EnrollError("private claim state has invalid fields; preserve it for recovery")
    return pending


def claim_enroll(*, state_dir, owner, short_name, agent_name, mode, transport,
                 show, notify, sleep=time.sleep, max_polls=None):
    with claim_operation(state_dir):
        return _claim_enroll(state_dir=state_dir, owner=owner, short_name=short_name,
                             agent_name=agent_name, mode=mode, transport=transport,
                             show=show, notify=notify, sleep=sleep, max_polls=max_polls)


def _claim_enroll(*, state_dir, owner, short_name, agent_name, mode, transport,
                 show, notify, sleep=time.sleep, max_polls=None):
    path = os.path.join(state_dir, PENDING)
    pending = load_claim_pending(path)
    if pending and pending["owner"].lower() != owner.lower():
        raise EnrollError("pending claim belongs to a different requested user")
    existing = load_credential(state_dir)
    if existing:
        if pending:
            with open(os.path.join(state_dir, CREDENTIAL_FILE), encoding="utf-8") as f:
                saved = json.load(f)
            if saved.get("claim_id") != pending["claim_id"]:
                raise EnrollError("existing credential does not belong to pending claim; preserve both")
            _ack(transport, pending)
            os.unlink(path)
        return existing
    if pending is None:
        code = "".join(secrets.choice(ALPHABET) for _ in range(12))
        response = transport.post_json("/api/enroll/claims", {
            "requested_user_id": owner, "requested_short_name": short_name,
            "agent_name": agent_name, "code_digest": hashlib.sha256(code.encode("ascii")).hexdigest(),
            "device_fingerprint": device_fingerprint(state_dir), "client_version": __version__, "mode": mode,
        })
        if (not isinstance(response.get("claim_id"), str) or not re.fullmatch(r"[0-9a-f]{32}", response["claim_id"])
                or not isinstance(response.get("poll_token"), str) or not re.fullmatch(r"[0-9a-f]{64}", response["poll_token"])):
            raise EnrollError("claim reply lacks private enrollment identifiers")
        bots = response.get("feishu_bots") or []
        for bot in bots:
            if isinstance(bot, dict) and isinstance(bot.get("app_id"), str):
                if re.fullmatch(r"cli_[A-Za-z0-9_]{6,64}", bot["app_id"]):
                    notify("打开当前 Hub 的飞书机器人： https://applink.feishu.cn/client/bot/open?appId=" + bot["app_id"])
        pending = {"claim_id": response["claim_id"], "poll_token": response["poll_token"],
                   "expires_at": response.get("expires_at", ""), "owner": owner, "code": code}
        write_private_file(path, json.dumps(pending))
    show("#pair " + pending["code"])
    polls = 0
    while max_polls is None or polls < max_polls:
        polls += 1
        try:
            result = transport.get_json("/api/enroll/claims/" + pending["claim_id"],
                                        {"Authorization": "Bearer " + pending["poll_token"]})
        except HubHTTPError as exc:
            if exc.status == 429:
                sleep(max(2.0, exc.retry_after_s))
                continue
            if exc.status == 410:
                raise EnrollStopped("pairing expired; start a new claim explicitly") from None
            raise
        if result.get("last_error") in ("name_conflict", "owner_mismatch"):
            raise EnrollStopped("账号/简称/Agent 名称冲突或飞书归属不符；保留申请，在安装端明确重新申请。")
        state = result.get("state")
        if state == "awaiting_feishu":
            sleep(2.0)
            continue
        if state == "completed":
            raise EnrollStopped("credential was acknowledged but is absent locally; preserve state")
        if state != "credential_ready":
            raise EnrollStopped("claim stopped; no credential issued")
        secret, name = result.get("credential"), result.get("agent_name")
        if not isinstance(secret, str) or not re.fullmatch(r"dwa_[0-9a-f]{64}", secret) or not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9]{2,32}", name):
            raise EnrollError("invalid claim credential response")
        user = result.get("user_id")
        if not isinstance(user, str) or (user.lower() != owner.lower() and result.get("owner_reused") is not True):
            raise EnrollError("claim response owner mismatch")
        if user.lower() != owner.lower():
            notify("当前飞书身份已有账号，复用实际用户 ID：" + user)
        # Commit to disk first: a lost response/ack is safely resumed using the same credential.
        write_private_file(os.path.join(state_dir, CREDENTIAL_FILE), json.dumps({
            "agent_name": name, "secret": secret, "user_id": user, "claim_id": pending["claim_id"],
            "agent_full_name": result.get("agent_full_name", ""),
        }))
        _ack(transport, pending)
        os.unlink(path)
        notify("飞书身份与设备配对完成。")
        return Credential(agent_name=name, secret=secret)
    raise EnrollError("claim polling budget exhausted; private state preserved")


def _ack(transport, pending):
    try:
        transport.post_authenticated("/api/enroll/claims/" + pending["claim_id"] + "/ack", {},
                                     {"Authorization": "Bearer " + pending["poll_token"]})
    except HubHTTPError as exc:
        # Only called after this claim's credential was durably saved. Expiry
        # closes future retrieval; it does not revoke the already saved agent.
        if exc.status != 410:
            raise


def cancel_pending_claim(*, state_dir, owner, transport):
    """Explicitly close an unclaimed server application before archiving locally.

    Unknown/rejected server outcomes preserve pending state. Never delete or
    rotate an issued device credential, user, or Agent.
    """
    with claim_operation(state_dir):
        if os.path.lexists(os.path.join(state_dir, CREDENTIAL_FILE)):
            raise EnrollStopped("已有设备凭据，不能取消或重新配对；请保留现场并核实连接状态。")
        path = os.path.join(state_dir, PENDING)
        pending = load_claim_pending(path)
        if pending is None:
            return None
        if pending["owner"].lower() != owner.lower():
            raise EnrollError("pending claim belongs to a different requested user")
        try:
            result = transport.post_authenticated("/api/enroll/claims/" + pending["claim_id"] + "/cancel", {},
                                                  {"Authorization": "Bearer " + pending["poll_token"]})
        except HubHTTPError as exc:
            if exc.status == 403:
                raise EnrollStopped("服务器已发放凭据或申请不可取消；保留状态，先在控制台核实 Agent，不重复开户。") from None
            if exc.status in (401, 404):
                raise EnrollStopped("无法确认取消权限或此 Hub 尚不支持取消；保留状态，不重新申请。") from None
            raise
        if not isinstance(result, dict) or result.get("state") != "cancelled" or result.get("claim_id") != pending["claim_id"]:
            raise EnrollStopped("服务器未确认此申请尚未认领并已取消；保留状态，不重新申请。")
        archive = os.path.join(state_dir, "claim-cancelled-" + secrets.token_hex(16) + ".json")
        fd = os.open(archive, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(pending, stream)
            stream.flush()
            os.fsync(stream.fileno())
        directory = os.open(state_dir, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
            os.unlink(path)
            os.fsync(directory)
        finally:
            os.close(directory)
        return archive
