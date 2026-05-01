from __future__ import annotations

import json
from dataclasses import dataclass
from threading import Lock
from time import perf_counter
from typing import Any, Callable, Mapping

from ._capture import CaptureBuffer, _decode_body
from ._httpx import suppress_httpx_interception
from ._injection import InjectionEngine
from ._openai import OpenAIReplayStore, ReplayFailureError

_patch_lock = Lock()
_original_request_with_retries: Callable[..., Any] | None = None


@dataclass(frozen=True, slots=True)
class _StripeSDKRequest:
    method: str
    url: str
    headers: Mapping[str, str]
    content: bytes


@dataclass(frozen=True, slots=True)
class _StripeSDKResponse:
    status_code: int
    headers: Mapping[str, str]
    content: bytes


def install_stripe_interception(
    *,
    mode: str,
    capture_buffer: CaptureBuffer,
    replay_store: OpenAIReplayStore,
    injection_engine: InjectionEngine,
) -> None:
    global _original_request_with_retries

    stripe_http_client = _stripe_http_client_module()
    if stripe_http_client is None:
        return

    http_client_cls = getattr(stripe_http_client, "HTTPClient", None)
    if http_client_cls is None:
        return

    with _patch_lock:
        if _original_request_with_retries is not None:
            return

        original = http_client_cls.request_with_retries

        def wrapped_request_with_retries(
            self: Any,
            method: str,
            url: str,
            headers: Mapping[str, str],
            post_data: Any = None,
            max_network_retries: int | None = None,
            *,
            _usage: list[str] | None = None,
        ) -> tuple[Any, int, Mapping[str, str]]:
            started = perf_counter()
            request = _build_request(method, url, headers, post_data)
            injected = _injected_response(
                request=request,
                capture_buffer=capture_buffer,
                injection_engine=injection_engine,
                elapsed_ms=_elapsed_ms(started),
            )
            if injected is not None:
                return injected

            if mode == "replay":
                match = replay_store.pop_match_for_parts(
                    method=request.method,
                    url=request.url,
                    body=_request_body(request),
                )
                return _build_replay_tuple(match.interaction, capture_buffer)

            try:
                with suppress_httpx_interception():
                    response_body, status_code, response_headers = original(
                        self,
                        method,
                        url,
                        headers,
                        post_data,
                        max_network_retries,
                        _usage=_usage,
                    )
            except Exception as exc:
                capture_buffer.record_httpx_failure(
                    request,
                    exc,
                    elapsed_ms=_elapsed_ms(started),
                    timeout=_is_timeout_error(exc),
                )
                raise

            response = _build_response(response_body, status_code, response_headers)
            capture_buffer.record_httpx_response(
                request,
                response,
                elapsed_ms=_elapsed_ms(started),
                streamed=False,
            )
            return response_body, status_code, response_headers

        http_client_cls.request_with_retries = wrapped_request_with_retries
        _original_request_with_retries = original


def uninstall_stripe_interception() -> None:
    global _original_request_with_retries

    stripe_http_client = _stripe_http_client_module()
    if stripe_http_client is None:
        _original_request_with_retries = None
        return

    http_client_cls = getattr(stripe_http_client, "HTTPClient", None)
    if http_client_cls is None:
        _original_request_with_retries = None
        return

    with _patch_lock:
        if _original_request_with_retries is not None:
            http_client_cls.request_with_retries = _original_request_with_retries
            _original_request_with_retries = None


def _stripe_http_client_module() -> Any | None:
    try:
        import stripe._http_client as stripe_http_client
    except Exception:
        return None
    return stripe_http_client


def _build_request(
    method: str,
    url: str,
    headers: Mapping[str, str] | None,
    post_data: Any,
) -> _StripeSDKRequest:
    return _StripeSDKRequest(
        method=str(method).upper(),
        url=str(url),
        headers={str(key).lower(): str(value) for key, value in (headers or {}).items()},
        content=_request_content(post_data),
    )


def _build_response(
    body: Any,
    status_code: int,
    headers: Mapping[str, str] | None,
) -> _StripeSDKResponse:
    return _StripeSDKResponse(
        status_code=int(status_code),
        headers={str(key).lower(): str(value) for key, value in (headers or {}).items()},
        content=_response_content(body),
    )


def _injected_response(
    *,
    request: _StripeSDKRequest,
    capture_buffer: CaptureBuffer,
    injection_engine: InjectionEngine,
    elapsed_ms: int,
) -> tuple[bytes, int, Mapping[str, str]] | None:
    normalized_request = capture_buffer.normalize_request(request)
    service = capture_buffer.detect_service(normalized_request.url)
    operation = capture_buffer.detect_operation(
        normalized_request.method,
        normalized_request.url,
        normalized_request.body,
    )
    decision = injection_engine.evaluate(service=service, operation=operation)
    if decision is None:
        return None

    headers = {"content-type": ["application/json"], "x-stagehand-injected": ["true"]}
    capture_buffer.record_success_for_request(
        normalized_request=normalized_request,
        service=service,
        operation=operation,
        protocol=capture_buffer.detect_protocol(normalized_request.url),
        elapsed_ms=elapsed_ms,
        streaming=False,
        response={
            "status_code": decision.override.status,
            "headers": headers,
            "body": decision.override.body,
        },
    )
    return (
        _response_content(decision.override.body),
        decision.override.status,
        {key: values[0] for key, values in headers.items() if values},
    )


def _request_body(request: _StripeSDKRequest) -> Any | None:
    return _decode_body(
        content=request.content,
        content_type=_header_content_type(request.headers),
    )


def _request_content(post_data: Any) -> bytes:
    if post_data is None:
        return b""
    if isinstance(post_data, bytes):
        return post_data
    if isinstance(post_data, bytearray):
        return bytes(post_data)
    if isinstance(post_data, str):
        return post_data.encode("utf-8")
    return str(post_data).encode("utf-8")


def _response_content(body: Any) -> bytes:
    if body is None:
        return b""
    if isinstance(body, bytes):
        return body
    if isinstance(body, bytearray):
        return bytes(body)
    if isinstance(body, str):
        return body.encode("utf-8")
    return json.dumps(body).encode("utf-8")


def _build_replay_tuple(
    interaction: Any,
    capture_buffer: CaptureBuffer,
) -> tuple[bytes, int, Mapping[str, str]]:
    def replayed() -> None:
        capture_buffer.record_replay_interaction(interaction, fallback_tier="exact")

    terminal_event = _terminal_replay_event(interaction)
    if terminal_event is None:
        raise ReplayFailureError(
            interaction=interaction,
            terminal_event_type="missing_terminal_event",
            detail="captured interaction is not replayable without a terminal event",
        )

    if terminal_event.type in {"error", "timeout"}:
        replayed()
        terminal_data = terminal_event.data or {}
        raise ReplayFailureError(
            interaction=interaction,
            terminal_event_type=terminal_event.type,
            error_class=_as_str_or_none(terminal_data.get("error_class")),
            detail=_as_str_or_none(terminal_data.get("message")),
        )

    response_event = _response_replay_event(interaction)
    response_data = response_event.data if response_event is not None else {}
    body = _encode_body_bytes(response_data.get("body"))
    status_code = int(response_data.get("status_code", 200))
    headers = _single_value_headers(response_data.get("headers", {}))
    replayed()
    return body, status_code, headers


def _terminal_replay_event(interaction: Any) -> Any | None:
    for event in reversed(interaction.events):
        if event.type in {"response_received", "error", "timeout", "stream_end"}:
            return event
    return None


def _response_replay_event(interaction: Any) -> Any | None:
    for event in interaction.events:
        if event.type == "response_received":
            return event
    return None


def _single_value_headers(headers: Any) -> dict[str, str]:
    if not isinstance(headers, dict):
        return {}

    flattened: dict[str, str] = {}
    for key, value in headers.items():
        if isinstance(value, list):
            if value:
                flattened[str(key)] = str(value[0])
        else:
            flattened[str(key)] = str(value)
    return flattened


def _encode_body_bytes(body: Any) -> bytes:
    if body is None:
        return b""
    if isinstance(body, bytes):
        return body
    if isinstance(body, str):
        return body.encode("utf-8")
    return json.dumps(body).encode("utf-8")


def _header_content_type(headers: Mapping[str, str]) -> str:
    value = headers.get("content-type")
    if value is None:
        return ""
    return str(value).split(";", 1)[0].strip().lower()


def _as_str_or_none(value: Any) -> str | None:
    if value is None:
        return None
    return str(value)


def _elapsed_ms(started: float) -> int:
    elapsed = int((perf_counter() - started) * 1000)
    if elapsed < 0:
        return 0
    return elapsed


def _is_timeout_error(error: Exception) -> bool:
    name = error.__class__.__name__.lower()
    return "timeout" in name or "timedout" in name
