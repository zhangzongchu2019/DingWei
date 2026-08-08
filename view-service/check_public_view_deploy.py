#!/usr/bin/env python3
"""Fail closed when the public View route and its single rewriter drift."""

from __future__ import annotations

import argparse
import pathlib
import re


ACTIVE_SUB_FILTER = re.compile(r"^\s*sub_filter(?:\s|$)")


def validate(routing_sessions: set[str], allowed_sessions: set[str], nginx_text: str) -> None:
    missing = sorted(routing_sessions - allowed_sessions)
    if missing:
        raise ValueError("routing sessions missing from nginx allowlist: " + ", ".join(missing))
    active_sub_filters = [
        line.strip()
        for line in nginx_text.splitlines()
        if not line.lstrip().startswith("#") and ACTIVE_SUB_FILTER.match(line)
    ]
    if active_sub_filters:
        raise ValueError("nginx must not rewrite renderer paths when VS_PUBLIC_VIEW_BASE is enabled")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--routing-session", action="append", default=[], required=True)
    parser.add_argument("--allow-session", action="append", default=[], required=True)
    parser.add_argument("--nginx-config", type=pathlib.Path, required=True)
    args = parser.parse_args()
    validate(set(args.routing_session), set(args.allow_session), args.nginx_config.read_text())
    print("public view deploy preflight PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
