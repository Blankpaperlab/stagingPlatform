from __future__ import annotations

import json
from collections import defaultdict, deque
from dataclasses import dataclass
from threading import Lock
from typing import Any, Iterable
from urllib.parse import urlsplit

import httpx

from ._capture import CapturedInteraction
from ._providers import is_openai_host


class ReplayMissError(RuntimeError):
    """Raised when replay mode cannot find an exact OpenAI interaction match."""


class ReplayFailureError(RuntimeError):
    """Raised when exact replay matches an interaction that recorded a terminal failure."""

    def __init__(
        self,
        *,
        interaction: CapturedInteraction,
        terminal_event_type: str,
        error_class: str | None = None,
        detail: str | None = None,
    ) -> None:
        self.interaction = interaction
        self.terminal_event_type = terminal_event_type
        self.error_class = error_class
        self.detail = detail

        parts = [
            "matched OpenAI replay interaction ended with",
            terminal_event_type,
        ]
        if error_class:
            parts.append(error_class)
        if detail:
            parts.append(f"({detail})")

        super().__init__(" ".join(parts))


@dataclass(frozen=True, slots=True)
class ExactReplayMatch:
    interaction: CapturedInteraction


class OpenAIReplayStore:
    def __init__(self) -> None:
        self._entries: dict[str, deque[CapturedInteraction]] = defaultdict(deque)
        self._lock = Lock()

    def seed(self, interactions: Iterable[CapturedInteraction]) -> int:
        count = 0
        with self._lock:
            for interaction in interactions:
                if interaction.service != "openai":
                    continue
                self._entries[_request_key_from_interaction(interaction)].append(interaction)
                count += 1
        return count

    def pop_match(self, request: httpx.Request) -> ExactReplayMatch:
        key = request_key_from_httpx_request(request)
        with self._lock:
            matches = self._entries.get(key)
            if not matches:
                raise ReplayMissError(
                    f"no exact replay match for OpenAI request {request.method} {request.url}"
                )

            return ExactReplayMatch(interaction=matches.popleft())


def is_openai_request(request: httpx.Request) -> bool:
    return is_openai_host(request.url.host)


def request_key_from_httpx_request(request: httpx.Request) -> str:
    body = _decode_httpx_request_body(request)
    payload = {
        "method": str(request.method).upper(),
        "url": str(request.url),
        "body": _replay_key_body(body),
    }
    return json.dumps(payload, sort_keys=True, separators=(",", ":"))


def _request_key_from_interaction(interaction: CapturedInteraction) -> str:
    payload = {
        "method": interaction.request.method.upper(),
        "url": interaction.request.url,
        "body": _replay_key_body(interaction.request.body),
    }
    return json.dumps(payload, sort_keys=True, separators=(",", ":"))


def _replay_key_body(body: Any) -> Any:
    if not isinstance(body, dict):
        return body

    if isinstance(body.get("messages"), list) and "model" in body:
        key_body: dict[str, Any] = {}
        for field in (
            "model",
            "stream",
            "temperature",
            "top_p",
            "n",
            "max_tokens",
            "response_format",
            "tool_choice",
            "parallel_tool_calls",
        ):
            if field in body:
                key_body[field] = body[field]
        if isinstance(body.get("tools"), list):
            key_body["tools"] = [_tool_signature(tool) for tool in body["tools"]]
        key_body["messages"] = [_message_signature(message) for message in body["messages"]]
        return key_body

    if "input" in body and "model" in body:
        key_body = {"model": body["model"], "input": _content_signature(body.get("input"))}
        if "stream" in body:
            key_body["stream"] = body["stream"]
        return key_body

    return body


def _message_signature(message: Any) -> Any:
    if not isinstance(message, dict):
        return _content_signature(message)

    signature: dict[str, Any] = {}
    for field in ("role", "name", "tool_call_id"):
        if field in message:
            signature[field] = message[field]
    if "content" in message:
        signature["content"] = _content_signature(message["content"])
    if isinstance(message.get("tool_calls"), list):
        signature["tool_calls"] = [
            _tool_call_signature(tool_call) for tool_call in message["tool_calls"]
        ]
    return signature


def _tool_call_signature(tool_call: Any) -> Any:
    if not isinstance(tool_call, dict):
        return _content_signature(tool_call)

    signature: dict[str, Any] = {}
    for field in ("id", "type"):
        if field in tool_call:
            signature[field] = tool_call[field]

    function = tool_call.get("function")
    if isinstance(function, dict):
        signature["function"] = {
            "name": function.get("name"),
            "arguments": _content_signature(function.get("arguments")),
        }

    return signature


def _tool_signature(tool: Any) -> Any:
    if not isinstance(tool, dict):
        return _content_signature(tool)

    signature: dict[str, Any] = {"type": tool.get("type")}
    function = tool.get("function")
    if isinstance(function, dict):
        signature["function"] = {
            "name": function.get("name"),
            "parameters": _content_signature(function.get("parameters")),
        }
    return signature


def _content_signature(value: Any) -> Any:
    if value is None:
        return None
    if isinstance(value, str):
        return {"type": "string"}
    if isinstance(value, bool):
        return {"type": "bool", "value": value}
    if isinstance(value, (int, float)):
        return {"type": "number"}
    if isinstance(value, list):
        return [_content_signature(item) for item in value]
    if isinstance(value, dict):
        return {str(key): _content_signature(value[key]) for key in sorted(value)}
    return {"type": type(value).__name__}


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
