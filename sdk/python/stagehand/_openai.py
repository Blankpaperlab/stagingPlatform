from __future__ import annotations

import json
from collections import defaultdict, deque
from dataclasses import dataclass
from threading import Lock
from typing import Any, Iterable, Mapping
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

import httpx

from ._capture import CapturedInteraction, _decode_body
from ._providers import is_openai_host

_REPLAY_KEY_HEADERS = ("accept", "content-type")


class ReplayMissError(RuntimeError):
    """Raised when replay mode cannot find an exact interaction match."""


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
            "matched replay interaction ended with",
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
                for key in _request_keys_from_interaction(interaction):
                    self._entries[key].append(interaction)
                count += 1
        return count

    def pop_match(self, request: httpx.Request) -> ExactReplayMatch:
        keys = request_keys_from_httpx_request(request)
        return self.pop_match_for_keys(keys, request_label=f"{request.method} {request.url}")

    def pop_match_for_parts(
        self,
        *,
        method: str,
        url: str,
        body: Any | None,
        headers: Mapping[str, Any] | None = None,
    ) -> ExactReplayMatch:
        keys = request_keys_from_parts(method=method, url=url, body=body, headers=headers)
        return self.pop_match_for_keys(keys, request_label=f"{method} {url}")

    def pop_match_for_key(self, key: str, *, request_label: str) -> ExactReplayMatch:
        return self.pop_match_for_keys((key,), request_label=request_label)

    def pop_match_for_keys(
        self,
        keys: Iterable[str],
        *,
        request_label: str,
    ) -> ExactReplayMatch:
        with self._lock:
            for key in keys:
                matches = self._entries.get(key)
                if not matches:
                    continue

                return ExactReplayMatch(interaction=matches.popleft())

            raise ReplayMissError(f"no exact replay match for request {request_label}")


def is_openai_request(request: httpx.Request) -> bool:
    return is_openai_host(request.url.host)


def request_key_from_httpx_request(request: httpx.Request) -> str:
    return request_keys_from_httpx_request(request)[0]


def request_keys_from_httpx_request(request: httpx.Request) -> tuple[str, ...]:
    body = _decode_httpx_request_body(request)
    return request_keys_from_parts(
        method=str(request.method),
        url=str(request.url),
        body=body,
        headers=dict(request.headers),
    )


def request_key_from_parts(
    *,
    method: str,
    url: str,
    body: Any | None,
    headers: Mapping[str, Any] | None = None,
) -> str:
    return request_keys_from_parts(method=method, url=url, body=body, headers=headers)[0]


def request_keys_from_parts(
    *,
    method: str,
    url: str,
    body: Any | None,
    headers: Mapping[str, Any] | None = None,
) -> tuple[str, ...]:
    selected_headers = _replay_key_headers(headers)
    return _request_key_candidates(
        method=method,
        url=url,
        body=body,
        headers=selected_headers,
        include_legacy_key=True,
        allow_header_subsets=True,
    )


def _request_key_payload(
    *,
    method: str,
    url: str,
    body: Any | None,
    headers: dict[str, list[str]] | None,
) -> dict[str, Any]:
    payload = {
        "method": str(method).upper(),
        "url": _replay_key_url(str(url)),
        "body": _replay_key_body_for_url(str(url), body),
    }
    if headers is not None:
        payload["headers"] = headers
    return payload


def _request_key_candidates(
    *,
    method: str,
    url: str,
    body: Any | None,
    headers: dict[str, list[str]],
    include_legacy_key: bool,
    allow_header_subsets: bool,
) -> tuple[str, ...]:
    if include_legacy_key and len(headers) == 0:
        legacy = _request_key_payload(method=method, url=url, body=body, headers=None)
        return (json.dumps(legacy, sort_keys=True, separators=(",", ":")),)

    keys: list[str] = []
    header_candidates = _header_subsets(headers) if allow_header_subsets else [headers]
    for header_subset in header_candidates:
        payload = _request_key_payload(method=method, url=url, body=body, headers=header_subset)
        key = json.dumps(payload, sort_keys=True, separators=(",", ":"))
        if key not in keys:
            keys.append(key)
    if include_legacy_key:
        legacy = _request_key_payload(method=method, url=url, body=body, headers=None)
        legacy_key = json.dumps(legacy, sort_keys=True, separators=(",", ":"))
        if legacy_key not in keys:
            keys.append(legacy_key)
    return tuple(keys)


def _request_key_from_interaction(interaction: CapturedInteraction) -> str:
    return _request_keys_from_interaction(interaction)[0]


def _request_keys_from_interaction(interaction: CapturedInteraction) -> tuple[str, ...]:
    headers = _replay_key_headers(interaction.request.headers)
    include_legacy_key = len(headers) == 0
    return _request_key_candidates(
        method=interaction.request.method,
        url=interaction.request.url,
        body=interaction.request.body,
        headers=headers,
        include_legacy_key=include_legacy_key,
        allow_header_subsets=False,
    )


def _replay_key_headers(headers: Mapping[str, Any] | None) -> dict[str, list[str]]:
    if not headers:
        return {}

    selected: dict[str, list[str]] = {}
    for name in _REPLAY_KEY_HEADERS:
        value = _mapping_get_case_insensitive(headers, name)
        if value is None:
            continue
        if isinstance(value, list):
            values = [str(item).strip() for item in value if str(item).strip()]
        else:
            values = [str(value).strip()] if str(value).strip() else []
        if values:
            selected[name] = sorted(values)
    return selected


def _header_subsets(headers: dict[str, list[str]]) -> list[dict[str, list[str]]]:
    if not headers:
        return [{}]

    names = sorted(headers)
    subsets: list[dict[str, list[str]]] = []
    for mask in range((1 << len(names)) - 1, 0, -1):
        subset: dict[str, list[str]] = {}
        for index, name in enumerate(names):
            if mask & (1 << index):
                subset[name] = headers[name]
        subsets.append(subset)
    return subsets


def _mapping_get_case_insensitive(headers: Mapping[str, Any], name: str) -> Any | None:
    for key, value in headers.items():
        if str(key).lower() == name:
            return value
    return None


def _replay_key_url(url: str) -> str:
    parsed = urlsplit(url)
    if not parsed.query:
        return urlunsplit(
            (
                parsed.scheme.lower(),
                parsed.netloc.lower(),
                parsed.path or "/",
                "",
                "",
            )
        )

    pairs = []
    for key, value in parse_qsl(parsed.query, keep_blank_values=True):
        if parsed.hostname == "api.stripe.com" and key.lower() in {"email", "query"}:
            pairs.append((key, json.dumps(_content_signature(value), sort_keys=True)))
            continue
        pairs.append((key, value))

    pairs.sort(key=lambda pair: (pair[0], pair[1]))
    return urlunsplit(
        (
            parsed.scheme.lower(),
            parsed.netloc.lower(),
            parsed.path or "/",
            urlencode(pairs),
            "",
        )
    )


def _replay_key_body_for_url(url: str, body: Any) -> Any:
    if urlsplit(url).hostname == "api.stripe.com":
        return _stripe_replay_key_body(body)
    return _openai_replay_key_body(body)


def _openai_replay_key_body(body: Any) -> Any:
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


def _stripe_replay_key_body(body: Any) -> Any:
    if isinstance(body, dict):
        keyed: dict[str, Any] = {}
        for key in sorted(body):
            if _stripe_sensitive_key(str(key)):
                keyed[str(key)] = _content_signature(body[key])
                continue
            keyed[str(key)] = _stripe_replay_key_body(body[key])
        return keyed
    if isinstance(body, list):
        return [_stripe_replay_key_body(item) for item in body]
    return body


def _stripe_sensitive_key(key: str) -> bool:
    normalized = key.lower().replace("-", "_")
    if normalized in {"email", "query", "phone", "jwt", "token", "api_key", "secret"}:
        return True
    return any(
        marker in normalized
        for marker in ("_email", "_phone", "_jwt", "_token", "_api_key", "_secret")
    )


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
    return _decode_body(
        content=request.content,
        content_type=str(request.headers.get("content-type", "")).split(";", 1)[0].strip().lower(),
    )


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
