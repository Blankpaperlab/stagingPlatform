from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from threading import Lock
from typing import Any, Final
from urllib.parse import urlsplit
from uuid import uuid4

DEFAULT_SCRUB_POLICY_VERSION: Final[str] = "v0-unredacted"
DEFAULT_SESSION_SALT_ID: Final[str] = "salt_pending"

_KNOWN_SERVICE_HOSTS: Final[dict[str, str]] = {
    "api.openai.com": "openai",
    "api.anthropic.com": "anthropic",
    "api.stripe.com": "stripe",
    "slack.com": "slack",
    "api.slack.com": "slack",
}


@dataclass(frozen=True, slots=True)
class CapturedRequest:
    url: str
    method: str
    headers: dict[str, list[str]]
    body: Any | None = None

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CapturedRequest":
        headers = payload.get("headers") or {}
        return cls(
            url=str(payload["url"]),
            method=str(payload["method"]).upper(),
            headers={str(key): [str(value) for value in values] for key, values in headers.items()},
            body=payload.get("body"),
        )

    def to_dict(self) -> dict[str, Any]:
        data: dict[str, Any] = {
            "url": self.url,
            "method": self.method,
        }
        if self.headers:
            data["headers"] = self.headers
        if self.body is not None:
            data["body"] = self.body
        return data


@dataclass(frozen=True, slots=True)
class CapturedEvent:
    sequence: int
    t_ms: int
    sim_t_ms: int
    type: str
    data: dict[str, Any] | None = None
    nested_interaction_id: str | None = None

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CapturedEvent":
        return cls(
            sequence=int(payload["sequence"]),
            t_ms=int(payload["t_ms"]),
            sim_t_ms=int(payload["sim_t_ms"]),
            type=str(payload["type"]),
            data=payload.get("data"),
            nested_interaction_id=payload.get("nested_interaction_id"),
        )

    def to_dict(self) -> dict[str, Any]:
        data: dict[str, Any] = {
            "sequence": self.sequence,
            "t_ms": self.t_ms,
            "sim_t_ms": self.sim_t_ms,
            "type": self.type,
        }
        if self.data is not None:
            data["data"] = self.data
        if self.nested_interaction_id is not None:
            data["nested_interaction_id"] = self.nested_interaction_id
        return data


@dataclass(frozen=True, slots=True)
class CapturedScrubReport:
    scrub_policy_version: str
    session_salt_id: str
    redacted_paths: tuple[str, ...] = ()

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CapturedScrubReport":
        return cls(
            scrub_policy_version=str(payload["scrub_policy_version"]),
            session_salt_id=str(payload["session_salt_id"]),
            redacted_paths=tuple(str(path) for path in payload.get("redacted_paths", [])),
        )

    def to_dict(self) -> dict[str, Any]:
        data: dict[str, Any] = {
            "scrub_policy_version": self.scrub_policy_version,
            "session_salt_id": self.session_salt_id,
        }
        if self.redacted_paths:
            data["redacted_paths"] = list(self.redacted_paths)
        return data


@dataclass(frozen=True, slots=True)
class CapturedInteraction:
    run_id: str
    interaction_id: str
    sequence: int
    service: str
    operation: str
    protocol: str
    request: CapturedRequest
    events: tuple[CapturedEvent, ...]
    scrub_report: CapturedScrubReport
    streaming: bool = False
    parent_interaction_id: str | None = None
    fallback_tier: str | None = None
    extracted_entities: tuple[dict[str, Any], ...] = ()
    latency_ms: int | None = None

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CapturedInteraction":
        return cls(
            run_id=str(payload["run_id"]),
            interaction_id=str(payload["interaction_id"]),
            sequence=int(payload["sequence"]),
            service=str(payload["service"]),
            operation=str(payload["operation"]),
            protocol=str(payload["protocol"]),
            request=CapturedRequest.from_dict(payload["request"]),
            events=tuple(CapturedEvent.from_dict(event) for event in payload["events"]),
            scrub_report=CapturedScrubReport.from_dict(payload["scrub_report"]),
            streaming=bool(payload.get("streaming", False)),
            parent_interaction_id=payload.get("parent_interaction_id"),
            fallback_tier=payload.get("fallback_tier"),
            extracted_entities=tuple(payload.get("extracted_entities", [])),
            latency_ms=payload.get("latency_ms"),
        )

    def to_dict(self) -> dict[str, Any]:
        data: dict[str, Any] = {
            "run_id": self.run_id,
            "interaction_id": self.interaction_id,
            "sequence": self.sequence,
            "service": self.service,
            "operation": self.operation,
            "protocol": self.protocol,
            "streaming": self.streaming,
            "request": self.request.to_dict(),
            "events": [event.to_dict() for event in self.events],
            "scrub_report": self.scrub_report.to_dict(),
        }
        if self.parent_interaction_id is not None:
            data["parent_interaction_id"] = self.parent_interaction_id
        if self.fallback_tier is not None:
            data["fallback_tier"] = self.fallback_tier
        if self.extracted_entities:
            data["extracted_entities"] = list(self.extracted_entities)
        if self.latency_ms is not None:
            data["latency_ms"] = self.latency_ms
        return data

    def clone_for_run(
        self,
        *,
        run_id: str,
        interaction_id: str,
        sequence: int,
        fallback_tier: str | None = None,
    ) -> "CapturedInteraction":
        return CapturedInteraction(
            run_id=run_id,
            interaction_id=interaction_id,
            sequence=sequence,
            service=self.service,
            operation=self.operation,
            protocol=self.protocol,
            request=self.request,
            events=self.events,
            scrub_report=self.scrub_report,
            streaming=self.streaming,
            parent_interaction_id=self.parent_interaction_id,
            fallback_tier=fallback_tier if fallback_tier is not None else self.fallback_tier,
            extracted_entities=self.extracted_entities,
            latency_ms=self.latency_ms,
        )


class CaptureBuffer:
    """Thread-safe in-memory interaction buffer for Python-side interception."""

    def __init__(
        self,
        *,
        run_id: str,
        scrub_policy_version: str = DEFAULT_SCRUB_POLICY_VERSION,
        session_salt_id: str = DEFAULT_SESSION_SALT_ID,
    ) -> None:
        self._run_id = run_id
        self._scrub_report = CapturedScrubReport(
            scrub_policy_version=scrub_policy_version,
            session_salt_id=session_salt_id,
        )
        self._lock = Lock()
        self._next_sequence = 1
        self._interactions: list[CapturedInteraction] = []

    def snapshot(self) -> tuple[CapturedInteraction, ...]:
        with self._lock:
            return tuple(self._interactions)

    def record_replay_interaction(
        self,
        interaction: CapturedInteraction,
        *,
        fallback_tier: str = "exact",
    ) -> CapturedInteraction:
        sequence, interaction_id = self._next_identity()
        replayed = interaction.clone_for_run(
            run_id=self._run_id,
            interaction_id=interaction_id,
            sequence=sequence,
            fallback_tier=fallback_tier,
        )
        self._append(replayed)
        return replayed

    def record_httpx_response(
        self,
        request: Any,
        response: Any,
        *,
        elapsed_ms: int,
        streamed: bool,
    ) -> CapturedInteraction:
        normalized_request = _normalize_httpx_request(request)
        response_event = CapturedEvent(
            sequence=2,
            t_ms=elapsed_ms,
            sim_t_ms=elapsed_ms,
            type="response_received",
            data={
                "status_code": response.status_code,
                "headers": _normalize_headers(response.headers),
                "body": _normalize_httpx_response_body(response, streamed=streamed),
            },
        )
        interaction = self._build_interaction(
            request=request,
            normalized_request=normalized_request,
            elapsed_ms=elapsed_ms,
            events=(
                CapturedEvent(sequence=1, t_ms=0, sim_t_ms=0, type="request_sent"),
                response_event,
            ),
            streaming=streamed,
        )
        self._append(interaction)
        return interaction

    def record_openai_stream_response(
        self,
        request: Any,
        response: Any,
        *,
        elapsed_ms: int,
        raw_chunks: list[bytes],
    ) -> CapturedInteraction:
        normalized_request = _normalize_httpx_request(request)
        events = [
            CapturedEvent(sequence=1, t_ms=0, sim_t_ms=0, type="request_sent"),
            CapturedEvent(
                sequence=2,
                t_ms=elapsed_ms,
                sim_t_ms=elapsed_ms,
                type="response_received",
                data={
                    "status_code": response.status_code,
                    "headers": _normalize_headers(response.headers),
                    "body": {"capture_status": "stream"},
                },
            ),
        ]
        events.extend(_build_openai_stream_events(raw_chunks=raw_chunks, initial_t_ms=elapsed_ms))
        interaction = self._build_interaction(
            request=request,
            normalized_request=normalized_request,
            elapsed_ms=elapsed_ms,
            events=tuple(events),
            streaming=True,
        )
        self._append(interaction)
        return interaction

    def record_httpx_failure(
        self,
        request: Any,
        error: Exception,
        *,
        elapsed_ms: int,
        timeout: bool,
    ) -> CapturedInteraction:
        normalized_request = _normalize_httpx_request(request)
        event_type = "timeout" if timeout else "error"
        failure_event = CapturedEvent(
            sequence=2,
            t_ms=elapsed_ms,
            sim_t_ms=elapsed_ms,
            type=event_type,
            data={
                "error_class": error.__class__.__name__,
                "message": str(error),
            },
        )
        interaction = self._build_interaction(
            request=request,
            normalized_request=normalized_request,
            elapsed_ms=elapsed_ms,
            events=(
                CapturedEvent(sequence=1, t_ms=0, sim_t_ms=0, type="request_sent"),
                failure_event,
            ),
            streaming=False,
        )
        self._append(interaction)
        return interaction

    def _append(self, interaction: CapturedInteraction) -> None:
        with self._lock:
            self._interactions.append(interaction)

    def _build_interaction(
        self,
        *,
        request: Any,
        normalized_request: CapturedRequest,
        elapsed_ms: int,
        events: tuple[CapturedEvent, ...],
        streaming: bool,
    ) -> CapturedInteraction:
        sequence, interaction_id = self._next_identity()
        return CapturedInteraction(
            run_id=self._run_id,
            interaction_id=interaction_id,
            sequence=sequence,
            service=_detect_service(normalized_request.url),
            operation=detect_operation(
                normalized_request.method,
                normalized_request.url,
                normalized_request.body,
            ),
            protocol=_detect_protocol(normalized_request.url),
            request=normalized_request,
            events=events,
            scrub_report=self._scrub_report,
            streaming=streaming,
            latency_ms=elapsed_ms,
        )

    def _next_identity(self) -> tuple[int, str]:
        with self._lock:
            sequence = self._next_sequence
            self._next_sequence += 1
        return sequence, f"int_{uuid4().hex[:12]}"


def _normalize_httpx_request(request: Any) -> CapturedRequest:
    return CapturedRequest(
        url=str(request.url),
        method=str(request.method).upper(),
        headers=_normalize_headers(request.headers),
        body=_normalize_httpx_request_body(request),
    )


def _normalize_httpx_request_body(request: Any) -> Any | None:
    try:
        content = request.content
    except Exception as exc:
        return {
            "capture_status": "unavailable",
            "reason": exc.__class__.__name__,
        }
    return _decode_body(content=content, content_type=_header_content_type(request.headers))


def _normalize_httpx_response_body(response: Any, *, streamed: bool) -> Any | None:
    if streamed:
        return {"capture_status": "deferred_stream"}

    try:
        content = response.content
    except Exception as exc:
        return {
            "capture_status": "unavailable",
            "reason": exc.__class__.__name__,
        }
    return _decode_body(content=content, content_type=_header_content_type(response.headers))


def _normalize_headers(headers: Any) -> dict[str, list[str]]:
    if headers is None:
        return {}

    if hasattr(headers, "multi_items"):
        items = headers.multi_items()
    else:
        items = headers.items()

    normalized: dict[str, list[str]] = {}
    for raw_name, raw_value in items:
        name = str(raw_name).lower()
        normalized.setdefault(name, []).append(str(raw_value))
    return normalized


def _header_content_type(headers: Any) -> str:
    if headers is None:
        return ""

    value = headers.get("content-type")
    if value is None:
        return ""

    return str(value).split(";", 1)[0].strip().lower()


def _decode_body(*, content: Any, content_type: str) -> Any | None:
    if content is None:
        return None

    if isinstance(content, str):
        return content

    if not isinstance(content, (bytes, bytearray)):
        return str(content)

    payload = bytes(content)
    if len(payload) == 0:
        return None

    if content_type == "application/json":
        try:
            return json.loads(payload.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            pass

    if content_type.startswith("text/") or content_type in {
        "application/x-www-form-urlencoded",
        "application/xml",
    }:
        try:
            return payload.decode("utf-8")
        except UnicodeDecodeError:
            pass

    try:
        return payload.decode("utf-8")
    except UnicodeDecodeError:
        return {
            "encoding": "base64",
            "data": base64.b64encode(payload).decode("ascii"),
        }


def _detect_protocol(url: str) -> str:
    scheme = urlsplit(url).scheme.lower()
    return scheme if scheme in {"http", "https"} else "https"


def _detect_service(url: str) -> str:
    hostname = (urlsplit(url).hostname or "unknown").lower()
    if hostname in _KNOWN_SERVICE_HOSTS:
        return _KNOWN_SERVICE_HOSTS[hostname]
    return hostname


def _detect_operation(method: str, url: str) -> str:
    path = urlsplit(url).path or "/"
    return f"{method.upper()} {path}"


def detect_operation(method: str, url: str, body: Any | None = None) -> str:
    path = urlsplit(url).path or "/"
    hostname = (urlsplit(url).hostname or "").lower()
    if hostname == "api.openai.com" and method.upper() == "POST" and isinstance(body, dict):
        if path == "/v1/chat/completions" and "model" in body and "messages" in body:
            return "chat.completions.create"
        if path == "/v1/responses" and "model" in body and "input" in body:
            return "responses.create"
    return f"{method.upper()} {path}"


def _build_openai_stream_events(
    *, raw_chunks: list[bytes], initial_t_ms: int
) -> list[CapturedEvent]:
    events: list[CapturedEvent] = []
    raw_text = b"".join(raw_chunks).decode("utf-8")
    blocks = [block for block in raw_text.split("\n\n") if block]
    open_tool_calls: dict[int, dict[str, Any]] = {}
    next_sequence = 3
    current_t_ms = initial_t_ms

    for block in blocks:
        chunk = f"{block}\n\n"
        parsed_payload = _parse_openai_sse_payload(block)
        if parsed_payload == "[DONE]":
            for index, tool_info in list(open_tool_calls.items()):
                events.append(
                    CapturedEvent(
                        sequence=next_sequence,
                        t_ms=current_t_ms,
                        sim_t_ms=current_t_ms,
                        type="tool_call_end",
                        data=tool_info,
                        nested_interaction_id=f"tool_call_{index}",
                    )
                )
                next_sequence += 1
            open_tool_calls.clear()
            events.append(
                CapturedEvent(
                    sequence=next_sequence,
                    t_ms=current_t_ms,
                    sim_t_ms=current_t_ms,
                    type="stream_end",
                    data={"chunk": chunk},
                )
            )
            next_sequence += 1
            continue

        events.append(
            CapturedEvent(
                sequence=next_sequence,
                t_ms=current_t_ms,
                sim_t_ms=current_t_ms,
                type="stream_chunk",
                data={"chunk": chunk, "payload": parsed_payload},
            )
        )
        next_sequence += 1

        finish_reason = None
        if isinstance(parsed_payload, dict):
            for choice in parsed_payload.get("choices", []):
                finish_reason = choice.get("finish_reason") or finish_reason
                delta = choice.get("delta") or {}
                for tool_call in delta.get("tool_calls", []):
                    index = int(tool_call.get("index", 0))
                    function = tool_call.get("function") or {}
                    tool_info = {
                        "index": index,
                        "tool_call_id": tool_call.get("id"),
                        "name": function.get("name"),
                    }
                    if index not in open_tool_calls:
                        events.append(
                            CapturedEvent(
                                sequence=next_sequence,
                                t_ms=current_t_ms,
                                sim_t_ms=current_t_ms,
                                type="tool_call_start",
                                data=tool_info,
                                nested_interaction_id=f"tool_call_{index}",
                            )
                        )
                        next_sequence += 1
                    open_tool_calls[index] = tool_info

        if finish_reason == "tool_calls":
            for index, tool_info in list(open_tool_calls.items()):
                events.append(
                    CapturedEvent(
                        sequence=next_sequence,
                        t_ms=current_t_ms,
                        sim_t_ms=current_t_ms,
                        type="tool_call_end",
                        data=tool_info,
                        nested_interaction_id=f"tool_call_{index}",
                    )
                )
                next_sequence += 1
            open_tool_calls.clear()

        current_t_ms += 1

    if not any(event.type == "stream_end" for event in events):
        events.append(
            CapturedEvent(
                sequence=next_sequence,
                t_ms=current_t_ms,
                sim_t_ms=current_t_ms,
                type="stream_end",
                data={"chunk": "", "finish_reason": "stream_closed"},
            )
        )
    return events


def _parse_openai_sse_payload(block: str) -> dict[str, Any] | str:
    data_lines = [line[5:].strip() for line in block.splitlines() if line.startswith("data:")]
    if not data_lines:
        return {"raw": block}

    payload = "\n".join(data_lines)
    if payload == "[DONE]":
        return payload

    try:
        return json.loads(payload)
    except json.JSONDecodeError:
        return {"raw": payload}
