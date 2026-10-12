"""Feishu claim enrollment, including durable receipt before acknowledgement.

The human pairing code is transient. The independent poll token never leaves
this installer. Existing credentials and legacy Web approval are preserved.
"""
from __future__ import annotations

import hashlib
import json
import os
import secrets
import time

from . import __version__
from .enroll import (CREDENTIAL_FILE, EnrollError, EnrollStopped, HubHTTPError,
                     device_fingerprint, load_credential, write_private_file, Credential)

ALPHABET = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
PENDING = "claim-pending.json"


def claim_enroll(*, state_dir, owner, short_name, agent_name, mode, transport,
                 show, notify, sleep=time.sleep, max_polls=None):
    path = os.path.join(state_dir, PENDING)
    pending = None
    try:
        if os.stat(path).st_mode & 0o077:
            raise EnrollError("claim state must be private (0600)")
        with open(path, encoding="utf-8") as f:
            pending = json.load(f)
        if not isinstance(pending, dict) or not all(isinstance(pending.get(k), str) and pending[k]
                                                    for k in ("claim_id", "poll_token", "code", "owner")):
            raise EnrollError("claim state is incomplete; preserve it for recovery")
        if pending["owner"].lower() != owner.lower():
            raise EnrollError("pending claim belongs to a different requested user")
    except FileNotFoundError:
        pass
    except (OSError, ValueError) as exc:
        raise EnrollError("cannot load private claim state") from exc
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
        if not isinstance(response.get("claim_id"), str) or not isinstance(response.get("poll_token"), str):
            raise EnrollError("claim reply lacks private enrollment identifiers")
        bots = response.get("feishu_bots") or []
        for bot in bots:
            if isinstance(bot, dict) and isinstance(bot.get("app_id"), str):
                import re
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
        import re
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
