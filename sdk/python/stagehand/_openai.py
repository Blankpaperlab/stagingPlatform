from __future__ import annotations

import json
import re
from collections import defaultdict, deque
from dataclasses import dataclass
from threading import Lock
from typing import Any, Iterable, Mapping
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

import httpx

from ._capture import CapturedInteraction, _decode_body
from ._config import ServiceMapping
from ._providers import is_openai_host

_REPLAY_KEY_HEADERS = ("accept", "content-type")
_MINIMUM_NEAREST_SCORE = 0.35


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
    fallback_tier: str = "exact"
    fallback_reason: str | None = "exact request match"


class OpenAIReplayStore:
    def __init__(self, *, service_mappings: Iterable[ServiceMapping] = ()) -> None:
        self._entries: dict[str, deque[CapturedInteraction]] = defaultdict(deque)
        self._available: list[CapturedInteraction] = []
        self._used_interaction_ids: set[str] = set()
        self._service_mappings = tuple(service_mappings)
        self._lock = Lock()

    def seed(self, interactions: Iterable[CapturedInteraction]) -> int:
        count = 0
        with self._lock:
            for interaction in interactions:
                ignored_paths = self._ignored_request_paths_for_url(interaction.request.url)
                for key in _request_keys_from_interaction(
                    interaction,
                    ignored_request_paths=ignored_paths,
                ):
                    self._entries[key].append(interaction)
                self._available.append(interaction)
                count += 1
        return count

    def pop_match(self, request: httpx.Request) -> ExactReplayMatch:
        mapping = self._service_mapping_for_url(str(request.url))
        ignored_paths = mapping.ignore_request_paths if mapping is not None else ()
        keys = request_keys_from_httpx_request(request, ignored_request_paths=ignored_paths)
        return self.pop_match_for_keys(
            keys,
            request_label=f"{request.method} {request.url}",
            request={
                "method": str(request.method),
                "url": str(request.url),
                "body": _decode_httpx_request_body(request),
                "headers": dict(request.headers),
            },
            mapping=mapping,
        )

    def pop_match_for_parts(
        self,
        *,
        method: str,
        url: str,
        body: Any | None,
        headers: Mapping[str, Any] | None = None,
    ) -> ExactReplayMatch:
        mapping = self._service_mapping_for_url(url)
        ignored_paths = mapping.ignore_request_paths if mapping is not None else ()
        keys = request_keys_from_parts(
            method=method,
            url=url,
            body=body,
            headers=headers,
            ignored_request_paths=ignored_paths,
        )
        return self.pop_match_for_keys(
            keys,
            request_label=f"{method} {url}",
            request={
                "method": method,
                "url": url,
                "body": body,
                "headers": headers,
            },
            mapping=mapping,
        )

    def pop_match_for_key(self, key: str, *, request_label: str) -> ExactReplayMatch:
        return self.pop_match_for_keys((key,), request_label=request_label)

    def pop_match_for_keys(
        self,
        keys: Iterable[str],
        *,
        request_label: str,
        request: Mapping[str, Any] | None = None,
        mapping: ServiceMapping | None = None,
    ) -> ExactReplayMatch:
        with self._lock:
            for key in keys:
                matches = self._entries.get(key)
                if not matches:
                    continue

                if matches:
                    interaction = matches.popleft()
                    self._used_interaction_ids.add(interaction.interaction_id)
                    return ExactReplayMatch(interaction=interaction)

            if mapping is not None and 1 in mapping.allowed_tiers and request is not None:
                nearest = self._nearest_match(request=request, mapping=mapping)
                if nearest is not None:
                    interaction, score, reasons = nearest
                    self._used_interaction_ids.add(interaction.interaction_id)
                    reason = _nearest_reason(score=score, reasons=reasons)
                    return ExactReplayMatch(
                        interaction=interaction,
                        fallback_tier="nearest_neighbor",
                        fallback_reason=reason,
                    )

            raise ReplayMissError(f"no exact replay match for request {request_label}")

    def _ignored_request_paths_for_url(self, url: str) -> tuple[str, ...]:
        mapping = self._service_mapping_for_url(url)
        return mapping.ignore_request_paths if mapping is not None else ()

    def _service_mapping_for_url(self, url: str) -> ServiceMapping | None:
        for mapping in self._service_mappings:
            if mapping.matches(url):
                return mapping
        return None

    def _nearest_match(
        self,
        *,
        request: Mapping[str, Any],
        mapping: ServiceMapping,
    ) -> tuple[CapturedInteraction, float, tuple[str, ...]] | None:
        best: tuple[CapturedInteraction, float, tuple[str, ...]] | None = None
        for candidate in self._available:
            if candidate.interaction_id in self._used_interaction_ids:
                continue
            if not mapping.matches(candidate.request.url):
                continue
            if not _compatible_nearest_candidate(candidate, request):
                continue
            score, reasons = _nearest_score(candidate, request, mapping.ignore_request_paths)
            if best is None or score > best[1]:
                best = (candidate, score, reasons)
        if best is None or best[1] < _MINIMUM_NEAREST_SCORE:
            return None
        return best


def is_openai_request(request: httpx.Request) -> bool:
    return is_openai_host(request.url.host)


def request_key_from_httpx_request(request: httpx.Request) -> str:
    return request_keys_from_httpx_request(request)[0]


def request_keys_from_httpx_request(
    request: httpx.Request,
    *,
    ignored_request_paths: Iterable[str] = (),
) -> tuple[str, ...]:
    body = _decode_httpx_request_body(request)
    return request_keys_from_parts(
        method=str(request.method),
        url=str(request.url),
        body=body,
        headers=dict(request.headers),
        ignored_request_paths=ignored_request_paths,
    )


def _compatible_nearest_candidate(
    candidate: CapturedInteraction,
    request: Mapping[str, Any],
) -> bool:
    if candidate.request.method.upper() != str(request.get("method", "")).upper():
        return False
    left = urlsplit(candidate.request.url)
    right = urlsplit(str(request.get("url", "")))
    return left.netloc.lower() == right.netloc.lower() and (left.path or "/") == (right.path or "/")


def _nearest_score(
    candidate: CapturedInteraction,
    request: Mapping[str, Any],
    ignored_request_paths: Iterable[str],
) -> tuple[float, tuple[str, ...]]:
    ignored_paths = _normalize_request_ignore_paths(ignored_request_paths)
    candidate_query = _nearest_query_parts(candidate.request.url, ignored_paths)
    request_query = _nearest_query_parts(str(request.get("url", "")), ignored_paths)
    candidate_body = _nearest_body_parts(candidate.request.body, ignored_paths)
    request_body = _nearest_body_parts(request.get("body"), ignored_paths)
    candidate_headers = _nearest_header_parts(candidate.request.headers, ignored_paths)
    request_headers = _nearest_header_parts(request.get("headers"), ignored_paths)

    query_score = _set_similarity(candidate_query, request_query)
    body_score = _set_similarity(candidate_body, request_body)
    header_score = _set_similarity(candidate_headers, request_headers)
    score = (0.5 * body_score) + (0.35 * query_score) + (0.15 * header_score)
    if not candidate_body and not request_body:
        score += 0.25
    if not candidate_query and not request_query:
        score += 0.15
    return min(score, 1.0), _nearest_reasons(
        query_score=query_score,
        body_score=body_score,
        header_score=header_score,
    )


def _nearest_query_parts(url: str, ignored_request_paths: tuple[str, ...]) -> list[str]:
    parsed = urlsplit(url)
    ignored_query_keys = {
        path.removeprefix("query.").lower()
        for path in ignored_request_paths
        if path.startswith("query.") and path != "query."
    }
    parts = []
    for key, value in parse_qsl(parsed.query, keep_blank_values=True):
        normalized_key = key.lower()
        if normalized_key in ignored_query_keys or _mutable_request_field(normalized_key, value):
            continue
        parts.append(f"{normalized_key}={_nearest_value_signature(value)}")
    return sorted(parts)


def _nearest_body_parts(value: Any, ignored_request_paths: tuple[str, ...]) -> list[str]:
    if "body" in ignored_request_paths:
        return []
    body = _remove_ignored_body_paths(value, ignored_request_paths)
    parts: list[str] = []
    _flatten_nearest_body("", body, parts)
    return sorted(parts)


def _flatten_nearest_body(path: str, value: Any, parts: list[str]) -> None:
    if isinstance(value, dict):
        for key in sorted(value):
            child_path = f"{path}.{key}" if path else str(key)
            _flatten_nearest_body(child_path, value[key], parts)
        return
    if isinstance(value, list):
        for index, item in enumerate(value):
            _flatten_nearest_body(f"{path}[{index}]", item, parts)
        return
    field_name = path.rsplit(".", 1)[-1].split("[", 1)[0].lower()
    if _mutable_request_field(field_name, value):
        return
    parts.append(f"{path}={_nearest_value_signature(value)}")


def _nearest_header_parts(
    headers: Mapping[str, Any] | None,
    ignored_request_paths: tuple[str, ...],
) -> list[str]:
    selected = _remove_ignored_header_paths(_replay_key_headers(headers), ignored_request_paths)
    parts: list[str] = []
    for key, values in selected.items():
        if _mutable_header(key):
            continue
        for value in values:
            parts.append(f"{key.lower()}={str(value).strip()}")
    return sorted(parts)


def _mutable_request_field(name: str, value: Any) -> bool:
    normalized = name.lower().replace("-", "_")
    if normalized in {
        "request_id",
        "trace_id",
        "correlation_id",
        "idempotency_key",
        "cursor",
        "next_cursor",
        "page_token",
        "pagination_token",
        "starting_after",
        "ending_before",
        "created_at",
        "updated_at",
        "generated_at",
        "timestamp",
        "time",
    }:
        return True
    return isinstance(value, str) and _timestamp_like(value)


def _mutable_header(name: str) -> bool:
    normalized = name.lower()
    return normalized in {
        "x-request-id",
        "x-trace-id",
        "x-correlation-id",
        "idempotency-key",
        "date",
    }


def _timestamp_like(value: str) -> bool:
    return bool(
        re.match(r"^\d{4}-\d{2}-\d{2}(?:[tT ][0-9:.+-]+(?:Z)?)?$", value)
        or re.match(r"^\d{10}(?:\.\d+)?$", value)
        or re.match(r"^\d{13}$", value)
    )


def _nearest_value_signature(value: Any) -> str:
    if isinstance(value, str):
        return value
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def _set_similarity(left: list[str], right: list[str]) -> float:
    if not left and not right:
        return 1.0
    if not left or not right:
        return 0.0
    left_set = set(left)
    right_set = set(right)
    union = left_set | right_set
    if not union:
        return 1.0
    return len(left_set & right_set) / len(union)


def _nearest_reasons(
    *, query_score: float, body_score: float, header_score: float
) -> tuple[str, ...]:
    reasons = []
    if body_score < 1:
        reasons.append("body differed only within nearest-neighbor tolerance")
    if query_score < 1:
        reasons.append("query differed only within nearest-neighbor tolerance")
    if header_score < 1:
        reasons.append("headers differed only within nearest-neighbor tolerance")
    return tuple(reasons) or ("request differed only by mutable fields",)


def _nearest_reason(*, score: float, reasons: tuple[str, ...]) -> str:
    return f"nearest request match score={score:.2f}; " + "; ".join(reasons)


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
    ignored_request_paths: Iterable[str] = (),
) -> tuple[str, ...]:
    ignored_paths = _normalize_request_ignore_paths(ignored_request_paths)
    selected_headers = _remove_ignored_header_paths(_replay_key_headers(headers), ignored_paths)
    return _request_key_candidates(
        method=method,
        url=url,
        body=body,
        headers=selected_headers,
        include_legacy_key=True,
        allow_header_subsets=True,
        ignored_request_paths=ignored_paths,
    )


def _request_key_payload(
    *,
    method: str,
    url: str,
    body: Any | None,
    headers: dict[str, list[str]] | None,
    ignored_request_paths: tuple[str, ...] = (),
) -> dict[str, Any]:
    payload = {
        "method": str(method).upper(),
        "url": _replay_key_url(str(url), ignored_request_paths=ignored_request_paths),
        "body": _remove_ignored_body_paths(
            _replay_key_body_for_url(str(url), body),
            ignored_request_paths,
        ),
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
    ignored_request_paths: tuple[str, ...] = (),
) -> tuple[str, ...]:
    if include_legacy_key and len(headers) == 0:
        legacy = _request_key_payload(
            method=method,
            url=url,
            body=body,
            headers=None,
            ignored_request_paths=ignored_request_paths,
        )
        return (json.dumps(legacy, sort_keys=True, separators=(",", ":")),)

    keys: list[str] = []
    header_candidates = _header_subsets(headers) if allow_header_subsets else [headers]
    for header_subset in header_candidates:
        payload = _request_key_payload(
            method=method,
            url=url,
            body=body,
            headers=header_subset,
            ignored_request_paths=ignored_request_paths,
        )
        key = json.dumps(payload, sort_keys=True, separators=(",", ":"))
        if key not in keys:
            keys.append(key)
    if include_legacy_key:
        legacy = _request_key_payload(
            method=method,
            url=url,
            body=body,
            headers=None,
            ignored_request_paths=ignored_request_paths,
        )
        legacy_key = json.dumps(legacy, sort_keys=True, separators=(",", ":"))
        if legacy_key not in keys:
            keys.append(legacy_key)
    return tuple(keys)


def _request_key_from_interaction(interaction: CapturedInteraction) -> str:
    return _request_keys_from_interaction(interaction)[0]


def _request_keys_from_interaction(
    interaction: CapturedInteraction,
    *,
    ignored_request_paths: Iterable[str] = (),
) -> tuple[str, ...]:
    ignored_paths = _normalize_request_ignore_paths(ignored_request_paths)
    headers = _remove_ignored_header_paths(
        _replay_key_headers(interaction.request.headers),
        ignored_paths,
    )
    include_legacy_key = len(headers) == 0
    return _request_key_candidates(
        method=interaction.request.method,
        url=interaction.request.url,
        body=interaction.request.body,
        headers=headers,
        include_legacy_key=include_legacy_key,
        allow_header_subsets=False,
        ignored_request_paths=ignored_paths,
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


def _replay_key_url(url: str, *, ignored_request_paths: Iterable[str] = ()) -> str:
    parsed = urlsplit(url)
    ignored_query_keys = {
        path.removeprefix("query.").lower()
        for path in _normalize_request_ignore_paths(ignored_request_paths)
        if path.startswith("query.") and path != "query."
    }
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
        if key.lower() in ignored_query_keys:
            continue
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


def _normalize_request_ignore_paths(paths: Iterable[str]) -> tuple[str, ...]:
    normalized: list[str] = []
    for path in paths:
        item = str(path).strip().removeprefix("$.").removeprefix(".")
        if item.startswith("request."):
            item = item.removeprefix("request.")
        if item and item not in normalized:
            normalized.append(item)
    return tuple(normalized)


def _remove_ignored_header_paths(
    headers: dict[str, list[str]],
    ignored_request_paths: tuple[str, ...],
) -> dict[str, list[str]]:
    if not headers:
        return headers
    ignored = {
        path.removeprefix("headers.").lower()
        for path in ignored_request_paths
        if path.startswith("headers.") and path != "headers."
    }
    if "headers" in ignored_request_paths:
        return {}
    return {key: value for key, value in headers.items() if key.lower() not in ignored}


def _remove_ignored_body_paths(value: Any, ignored_request_paths: tuple[str, ...]) -> Any:
    if "body" in ignored_request_paths:
        return None
    body_paths = [
        path.removeprefix("body.")
        for path in ignored_request_paths
        if path.startswith("body.") and path != "body."
    ]
    if not body_paths:
        return value
    cloned = _clone_replay_value(value)
    for path in body_paths:
        _remove_path(cloned, _split_ignore_path(path))
    return cloned


def _clone_replay_value(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: _clone_replay_value(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_clone_replay_value(item) for item in value]
    return value


def _split_ignore_path(path: str) -> list[str]:
    parts: list[str] = []
    for raw in path.split("."):
        while raw:
            bracket = raw.find("[")
            if bracket < 0:
                parts.append(raw)
                break
            if bracket > 0:
                parts.append(raw[:bracket])
            close_bracket = raw.find("]", bracket)
            if close_bracket < 0:
                parts.append(raw[bracket:])
                break
            selector = raw[bracket + 1 : close_bracket]
            parts.append("*" if selector == "*" else selector)
            raw = raw[close_bracket + 1 :]
    return [part for part in parts if part]


def _remove_path(value: Any, parts: list[str]) -> None:
    if not parts:
        return
    if isinstance(value, list):
        segment = parts[0]
        if segment == "*":
            for item in value:
                _remove_path(item, parts[1:])
            return
        if segment.isdigit():
            index = int(segment)
            if 0 <= index < len(value):
                _remove_path(value[index], parts[1:])
            return
        for item in value:
            _remove_path(item, parts)
        return
    if not isinstance(value, dict):
        return
    if len(parts) == 1:
        value.pop(parts[0], None)
        return
    _remove_path(value.get(parts[0]), parts[1:])


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
