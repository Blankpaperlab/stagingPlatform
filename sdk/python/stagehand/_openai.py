from __future__ import annotations

import json
from collections import defaultdict, deque
from dataclasses import dataclass
from typing import Any, Iterable
from urllib.parse import urlsplit

import httpx

from ._capture import CapturedInteraction


class ReplayMissError(RuntimeError):
    """Raised when replay mode cannot find an exact OpenAI interaction match."""


@dataclass(frozen=True, slots=True)
class ExactReplayMatch:
    interaction: CapturedInteraction


class OpenAIReplayStore:
    def __init__(self) -> None:
        self._entries: dict[str, deque[CapturedInteraction]] = defaultdict(deque)

    def seed(self, interactions: Iterable[CapturedInteraction]) -> int:
        count = 0
        for interaction in interactions:
            if interaction.service != "openai":
                continue
            self._entries[_request_key_from_interaction(interaction)].append(interaction)
            count += 1
        return count

    def pop_match(self, request: httpx.Request) -> ExactReplayMatch:
        key = request_key_from_httpx_request(request)
        matches = self._entries.get(key)
        if not matches:
            raise ReplayMissError(
                f"no exact replay match for OpenAI request {request.method} {request.url}"
            )

        return ExactReplayMatch(interaction=matches.popleft())


def is_openai_request(request: httpx.Request) -> bool:
    return (request.url.host or "").lower() == "api.openai.com"


def request_key_from_httpx_request(request: httpx.Request) -> str:
    body = _decode_httpx_request_body(request)
    payload = {
        "method": str(request.method).upper(),
        "url": str(request.url),
        "body": body,
    }
    return json.dumps(payload, sort_keys=True, separators=(",", ":"))


def _request_key_from_interaction(interaction: CapturedInteraction) -> str:
    payload = {
        "method": interaction.request.method.upper(),
        "url": interaction.request.url,
        "body": interaction.request.body,
    }
    return json.dumps(payload, sort_keys=True, separators=(",", ":"))


def _decode_httpx_request_body(request: httpx.Request) -> Any | None:
    content_type = str(request.headers.get("content-type", "")).split(";", 1)[0].strip().lower()
    payload = request.content
    if not payload:
        return None

    if content_type == "application/json":
        return json.loads(payload.decode("utf-8"))

    try:
        return payload.decode("utf-8")
    except UnicodeDecodeError:
        return payload.hex()


def openai_operation_from_request(request: httpx.Request) -> str:
    path = urlsplit(str(request.url)).path or "/"
    if request.method.upper() == "POST":
        body = _decode_httpx_request_body(request)
        if isinstance(body, dict):
            if path == "/v1/chat/completions" and "model" in body and "messages" in body:
                return "chat.completions.create"
            if path == "/v1/responses" and "model" in body and "input" in body:
                return "responses.create"
    return f"{request.method.upper()} {path}"
