from __future__ import annotations

import json
from functools import wraps
from threading import Lock
from time import perf_counter
from typing import Any, Callable, Iterable

import httpx

from ._capture import CaptureBuffer, CapturedInteraction
from ._openai import OpenAIReplayStore, is_openai_request

_patch_lock = Lock()
_original_client_send: Callable[..., Any] | None = None
_original_async_client_send: Callable[..., Any] | None = None


def install_httpx_interception(
    *,
    mode: str,
    capture_buffer: CaptureBuffer,
    replay_store: OpenAIReplayStore,
) -> None:
    global _original_async_client_send, _original_client_send

    with _patch_lock:
        if _original_client_send is not None or _original_async_client_send is not None:
            return

        original_client_send = httpx.Client.send
        original_async_client_send = httpx.AsyncClient.send

        @wraps(original_client_send)
        def wrapped_client_send(self: Any, request: Any, *args: Any, **kwargs: Any) -> Any:
            started = perf_counter()
            streamed = bool(kwargs.get("stream", False))

            if mode == "replay" and is_openai_request(request):
                match = replay_store.pop_match(request)
                return _build_replay_response(
                    request=request,
                    interaction=match.interaction,
                    capture_buffer=capture_buffer,
                    streamed=streamed,
                    async_mode=False,
                )

            try:
                response = original_client_send(self, request, *args, **kwargs)
            except httpx.TimeoutException as exc:
                capture_buffer.record_httpx_failure(
                    request,
                    exc,
                    elapsed_ms=_elapsed_ms(started),
                    timeout=True,
                )
                raise
            except Exception as exc:
                capture_buffer.record_httpx_failure(
                    request,
                    exc,
                    elapsed_ms=_elapsed_ms(started),
                    timeout=False,
                )
                raise

            if streamed and is_openai_request(request) and _is_sse_response(response):
                response.stream = _RecordingSyncOpenAIStream(
                    original_stream=response.stream,
                    request=request,
                    response=response,
                    capture_buffer=capture_buffer,
                    elapsed_ms=_elapsed_ms(started),
                )
                return response

            capture_buffer.record_httpx_response(
                request,
                response,
                elapsed_ms=_elapsed_ms(started),
                streamed=streamed,
            )
            return response

        @wraps(original_async_client_send)
        async def wrapped_async_client_send(
            self: Any,
            request: Any,
            *args: Any,
            **kwargs: Any,
        ) -> Any:
            started = perf_counter()
            streamed = bool(kwargs.get("stream", False))

            if mode == "replay" and is_openai_request(request):
                match = replay_store.pop_match(request)
                return _build_replay_response(
                    request=request,
                    interaction=match.interaction,
                    capture_buffer=capture_buffer,
                    streamed=streamed,
                    async_mode=True,
                )

            try:
                response = await original_async_client_send(self, request, *args, **kwargs)
            except httpx.TimeoutException as exc:
                capture_buffer.record_httpx_failure(
                    request,
                    exc,
                    elapsed_ms=_elapsed_ms(started),
                    timeout=True,
                )
                raise
            except Exception as exc:
                capture_buffer.record_httpx_failure(
                    request,
                    exc,
                    elapsed_ms=_elapsed_ms(started),
                    timeout=False,
                )
                raise

            if streamed and is_openai_request(request) and _is_sse_response(response):
                response.stream = _RecordingAsyncOpenAIStream(
                    original_stream=response.stream,
                    request=request,
                    response=response,
                    capture_buffer=capture_buffer,
                    elapsed_ms=_elapsed_ms(started),
                )
                return response

            capture_buffer.record_httpx_response(
                request,
                response,
                elapsed_ms=_elapsed_ms(started),
                streamed=streamed,
            )
            return response

        httpx.Client.send = wrapped_client_send
        httpx.AsyncClient.send = wrapped_async_client_send
        _original_client_send = original_client_send
        _original_async_client_send = original_async_client_send


def uninstall_httpx_interception() -> None:
    global _original_async_client_send, _original_client_send

    with _patch_lock:
        if _original_client_send is not None:
            httpx.Client.send = _original_client_send
            _original_client_send = None

        if _original_async_client_send is not None:
            httpx.AsyncClient.send = _original_async_client_send
            _original_async_client_send = None


class _RecordingSyncOpenAIStream(httpx.SyncByteStream):
    def __init__(
        self,
        *,
        original_stream: httpx.SyncByteStream,
        request: httpx.Request,
        response: httpx.Response,
        capture_buffer: CaptureBuffer,
        elapsed_ms: int,
    ) -> None:
        self._original_stream = original_stream
        self._request = request
        self._response = response
        self._capture_buffer = capture_buffer
        self._elapsed_ms = elapsed_ms
        self._chunks: list[bytes] = []
        self._recorded = False

    def __iter__(self) -> Iterable[bytes]:
        try:
            for chunk in self._original_stream:
                self._chunks.append(bytes(chunk))
                yield chunk
        finally:
            self._record_once()

    def close(self) -> None:
        try:
            close = getattr(self._original_stream, "close", None)
            if callable(close):
                close()
        finally:
            self._record_once()

    def _record_once(self) -> None:
        if self._recorded:
            return
        self._recorded = True
        self._capture_buffer.record_openai_stream_response(
            self._request,
            self._response,
            elapsed_ms=self._elapsed_ms,
            raw_chunks=self._chunks,
        )


class _RecordingAsyncOpenAIStream(httpx.AsyncByteStream):
    def __init__(
        self,
        *,
        original_stream: httpx.AsyncByteStream,
        request: httpx.Request,
        response: httpx.Response,
        capture_buffer: CaptureBuffer,
        elapsed_ms: int,
    ) -> None:
        self._original_stream = original_stream
        self._request = request
        self._response = response
        self._capture_buffer = capture_buffer
        self._elapsed_ms = elapsed_ms
        self._chunks: list[bytes] = []
        self._recorded = False

    async def __aiter__(self) -> Any:
        try:
            async for chunk in self._original_stream:
                self._chunks.append(bytes(chunk))
                yield chunk
        finally:
            self._record_once()

    async def aclose(self) -> None:
        try:
            close = getattr(self._original_stream, "aclose", None)
            if callable(close):
                await close()
        finally:
            self._record_once()

    def _record_once(self) -> None:
        if self._recorded:
            return
        self._recorded = True
        self._capture_buffer.record_openai_stream_response(
            self._request,
            self._response,
            elapsed_ms=self._elapsed_ms,
            raw_chunks=self._chunks,
        )


class _ReplaySyncStream(httpx.SyncByteStream):
    def __init__(
        self,
        *,
        chunks: list[bytes],
        finalize: Callable[[], None],
    ) -> None:
        self._chunks = chunks
        self._finalize = finalize
        self._finalized = False

    def __iter__(self) -> Iterable[bytes]:
        try:
            for chunk in self._chunks:
                yield chunk
        finally:
            self._finalize_once()

    def close(self) -> None:
        self._finalize_once()

    def _finalize_once(self) -> None:
        if self._finalized:
            return
        self._finalized = True
        self._finalize()


class _ReplayAsyncStream(httpx.AsyncByteStream):
    def __init__(
        self,
        *,
        chunks: list[bytes],
        finalize: Callable[[], None],
    ) -> None:
        self._chunks = chunks
        self._finalize = finalize
        self._finalized = False

    async def __aiter__(self) -> Any:
        try:
            for chunk in self._chunks:
                yield chunk
        finally:
            self._finalize_once()

    async def aclose(self) -> None:
        self._finalize_once()

    def _finalize_once(self) -> None:
        if self._finalized:
            return
        self._finalized = True
        self._finalize()


def _build_replay_response(
    *,
    request: httpx.Request,
    interaction: CapturedInteraction,
    capture_buffer: CaptureBuffer,
    streamed: bool,
    async_mode: bool,
) -> httpx.Response:
    response_event = next(
        event for event in interaction.events if event.type == "response_received"
    )
    response_data = response_event.data or {}
    status_code = int(response_data.get("status_code", 200))
    headers = httpx.Headers(_flatten_headers(response_data.get("headers", {})))

    def replayed() -> None:
        capture_buffer.record_replay_interaction(interaction, fallback_tier="exact")

    if interaction.streaming or streamed:
        chunks = _extract_stream_chunks(interaction)
        stream: httpx.SyncByteStream | httpx.AsyncByteStream
        if async_mode:
            stream = _ReplayAsyncStream(chunks=chunks, finalize=replayed)
        else:
            stream = _ReplaySyncStream(chunks=chunks, finalize=replayed)
        return httpx.Response(
            status_code=status_code,
            headers=headers,
            request=request,
            stream=stream,
        )

    body = _encode_body_bytes(response_data.get("body"))
    replayed()
    return httpx.Response(
        status_code=status_code,
        headers=headers,
        content=body,
        request=request,
    )


def _extract_stream_chunks(interaction: CapturedInteraction) -> list[bytes]:
    chunks: list[bytes] = []
    for event in interaction.events:
        if event.type == "stream_chunk" and event.data is not None:
            chunk = event.data.get("chunk")
            if isinstance(chunk, str):
                chunks.append(chunk.encode("utf-8"))
        if event.type == "stream_end" and event.data is not None:
            chunk = event.data.get("chunk")
            if isinstance(chunk, str) and chunk:
                chunks.append(chunk.encode("utf-8"))
    return chunks


def _encode_body_bytes(body: Any) -> bytes:
    if body is None:
        return b""
    if isinstance(body, bytes):
        return body
    if isinstance(body, str):
        return body.encode("utf-8")
    return json.dumps(body).encode("utf-8")


def _is_sse_response(response: httpx.Response) -> bool:
    content_type = str(response.headers.get("content-type", "")).split(";", 1)[0].strip().lower()
    return content_type == "text/event-stream"


def _flatten_headers(headers: Any) -> list[tuple[str, str]]:
    if not isinstance(headers, dict):
        return []

    flattened: list[tuple[str, str]] = []
    for key, values in headers.items():
        if isinstance(values, list):
            flattened.extend((str(key), str(value)) for value in values)
        else:
            flattened.append((str(key), str(values)))
    return flattened


def _elapsed_ms(started: float) -> int:
    elapsed = int((perf_counter() - started) * 1000)
    if elapsed < 0:
        return 0
    return elapsed
