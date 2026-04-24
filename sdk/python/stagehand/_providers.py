from __future__ import annotations

import os
from typing import Final

ENV_OPENAI_HOSTS: Final[str] = "STAGEHAND_OPENAI_HOSTS"
_DEFAULT_OPENAI_HOSTS: Final[frozenset[str]] = frozenset({"api.openai.com"})


def configured_openai_hosts() -> frozenset[str]:
    raw = os.environ.get(ENV_OPENAI_HOSTS, "")
    configured = {host.strip().lower() for host in raw.split(",") if host.strip()}
    return frozenset(_DEFAULT_OPENAI_HOSTS | configured)


def is_openai_host(hostname: str | None) -> bool:
    if hostname is None:
        return False

    return hostname.strip().lower() in configured_openai_hosts()
