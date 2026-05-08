from __future__ import annotations

import builtins
import asyncio
import functools
import inspect
import json
import re
import time
from collections import defaultdict, deque
from collections.abc import Callable, Iterable, Mapping
from contextvars import ContextVar
from dataclasses import replace
from typing import Any, TypeVar, overload
from urllib.parse import quote

from ._capture import (
    CapturedEvent,
    CapturedInteraction,
    CapturedRequest,
    CapturedScrubReport,
)
from ._openai import ReplayMissError

F = TypeVar("F", bound=Callable[..., Any])

_TOOL_SERVICE = "stagehand.tool"
_TOOL_PROTOCOL = "tool"
_TOOL_METHOD = "CALL"
_EMAIL_PATTERN = re.compile(r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", re.IGNORECASE)
_CARD_PATTERN = re.compile(r"\b(?:\d[ -]*?){13,19}\b")
_JWT_PATTERN = re.compile(r"\beyJ[A-Za-z0-9_-]+?\.[A-Za-z0-9_-]+?\.[A-Za-z0-9_-]+?\b")
_SENSITIVE_KEYS = {"api_key", "apikey", "authorization", "password", "secret", "token"}
_current_tool_interaction_id: ContextVar[str | None] = ContextVar(
    "stagehand_current_tool_interaction_id",
    default=None,
)


class ToolReplayStore:
    def __init__(self) -> None:
        self._entries: dict[str, deque[CapturedInteraction]] = defaultdict(deque)

    def seed(self, interactions: Iterable[CapturedInteraction]) -> int:
        count = 0
        for interaction in interactions:
            if interaction.service != _TOOL_SERVICE:
                continue
            self._entries[_tool_key_from_interaction(interaction)].append(interaction)
            count += 1
        return count

    def pop_match(self, *, name: str, arguments: Mapping[str, Any]) -> CapturedInteraction:
        key = _tool_key(name=name, arguments=arguments)
        matches = self._entries.get(key)
        if not matches:
            raise ReplayMissError(f"no recorded tool call for {name}")
        interaction = matches.popleft()
        if not matches:
            self._entries.pop(key, None)
        return interaction


@overload
def tool(func: F, /) -> F: ...


@overload
def tool(
    *,
    name: str | None = None,
    side_effect: str = "read",
    replay: str = "recorded",
) -> Callable[[F], F]: ...


def tool(
    func: F | None = None,
    /,
    *,
    name: str | None = None,
    side_effect: str = "read",
    replay: str = "recorded",
) -> F | Callable[[F], F]:
    def decorate(target: F) -> F:
        tool_name = (name or target.__name__).strip()
        if not tool_name:
            raise ValueError("tool name must be non-empty")
        side_effect_value = side_effect.strip() or "read"
        replay_value = replay.strip() or "recorded"
        signature = inspect.signature(target)

        if inspect.iscoroutinefunction(target):

            @functools.wraps(target)
            async def async_wrapper(*args: Any, **kwargs: Any) -> Any:
                from ._runtime import get_runtime

                runtime = get_runtime()
                arguments = _bound_arguments(signature, args, kwargs)
                if runtime.mode == "passthrough":
                    return await target(*args, **kwargs)
                injected = await _injected_async_tool_error(
                    runtime,
                    name=tool_name,
                    arguments=arguments,
                    side_effect=side_effect_value,
                    replay=replay_value,
                )
                if injected is not None:
                    raise injected
                if runtime.mode == "replay" and replay_value == "recorded":
                    return _replay_tool_call(runtime, name=tool_name, arguments=arguments)
                return await _record_async_tool_call(
                    runtime,
                    target,
                    args,
                    kwargs,
                    name=tool_name,
                    arguments=arguments,
                    side_effect=side_effect_value,
                    replay=replay_value,
                )

            return async_wrapper  # type: ignore[return-value]

        @functools.wraps(target)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            from ._runtime import get_runtime

            runtime = get_runtime()
            arguments = _bound_arguments(signature, args, kwargs)
            if runtime.mode == "passthrough":
                return target(*args, **kwargs)
            injected = _injected_sync_tool_error(
                runtime,
                name=tool_name,
                arguments=arguments,
                side_effect=side_effect_value,
                replay=replay_value,
            )
            if injected is not None:
                raise injected
            if runtime.mode == "replay" and replay_value == "recorded":
                return _replay_tool_call(runtime, name=tool_name, arguments=arguments)
            return _record_sync_tool_call(
                runtime,
                target,
                args,
                kwargs,
                name=tool_name,
                arguments=arguments,
                side_effect=side_effect_value,
                replay=replay_value,
            )

        return wrapper  # type: ignore[return-value]

    if func is not None:
        return decorate(func)
    return decorate


def _injected_sync_tool_error(
    runtime: Any,
    *,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
) -> Exception | None:
    decision = runtime._injection_engine.evaluate_tool(tool=name)
    if decision is None:
        return None
    if decision.override.latency_ms > 0:
        time.sleep(decision.override.latency_ms / 1000)
    return _record_injected_tool_error(
        runtime,
        name=name,
        arguments=arguments,
        side_effect=side_effect,
        replay=replay,
        elapsed_ms=decision.override.latency_ms,
        error_kind=decision.override.error or "injected_tool_error",
        body=decision.override.body,
        provenance=decision.provenance.to_dict(),
    )


async def _injected_async_tool_error(
    runtime: Any,
    *,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
) -> Exception | None:
    decision = runtime._injection_engine.evaluate_tool(tool=name)
    if decision is None:
        return None
    if decision.override.latency_ms > 0:
        await asyncio.sleep(decision.override.latency_ms / 1000)
    return _record_injected_tool_error(
        runtime,
        name=name,
        arguments=arguments,
        side_effect=side_effect,
        replay=replay,
        elapsed_ms=decision.override.latency_ms,
        error_kind=decision.override.error or "injected_tool_error",
        body=decision.override.body,
        provenance=decision.provenance.to_dict(),
    )


def _record_injected_tool_error(
    runtime: Any,
    *,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
    elapsed_ms: int,
    error_kind: str,
    body: Any,
    provenance: dict[str, Any],
) -> Exception:
    sequence, interaction_id = runtime._capture_buffer.reserve_interaction()
    parent_id = _current_tool_interaction_id.get()
    error_class = _injected_error_class(error_kind, body)
    message = _injected_error_message(error_kind, body)
    error = _recorded_error(error_class, message)
    runtime._capture_buffer.append_reserved_interaction(
        _tool_error_interaction(
            runtime,
            sequence=sequence,
            interaction_id=interaction_id,
            parent_id=parent_id,
            name=name,
            arguments=arguments,
            side_effect=side_effect,
            replay=replay,
            elapsed_ms=elapsed_ms,
            error=error,
            error_class=error_class,
            metadata={"stagehand_injection": provenance},
        )
    )
    return error


def _record_sync_tool_call(
    runtime: Any,
    target: Callable[..., Any],
    args: tuple[Any, ...],
    kwargs: dict[str, Any],
    *,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
) -> Any:
    sequence, interaction_id = runtime._capture_buffer.reserve_interaction()
    parent_id = _current_tool_interaction_id.get()
    token = _current_tool_interaction_id.set(interaction_id)
    started = time.perf_counter()
    try:
        result = target(*args, **kwargs)
    except Exception as exc:
        elapsed_ms = _elapsed_ms(started)
        runtime._capture_buffer.append_reserved_interaction(
            _tool_error_interaction(
                runtime,
                sequence=sequence,
                interaction_id=interaction_id,
                parent_id=parent_id,
                name=name,
                arguments=arguments,
                side_effect=side_effect,
                replay=replay,
                elapsed_ms=elapsed_ms,
                error=exc,
            )
        )
        raise
    finally:
        _current_tool_interaction_id.reset(token)

    elapsed_ms = _elapsed_ms(started)
    runtime._capture_buffer.append_reserved_interaction(
        _tool_success_interaction(
            runtime,
            sequence=sequence,
            interaction_id=interaction_id,
            parent_id=parent_id,
            name=name,
            arguments=arguments,
            side_effect=side_effect,
            replay=replay,
            elapsed_ms=elapsed_ms,
            result=result,
        )
    )
    return result


async def _record_async_tool_call(
    runtime: Any,
    target: Callable[..., Any],
    args: tuple[Any, ...],
    kwargs: dict[str, Any],
    *,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
) -> Any:
    sequence, interaction_id = runtime._capture_buffer.reserve_interaction()
    parent_id = _current_tool_interaction_id.get()
    token = _current_tool_interaction_id.set(interaction_id)
    started = time.perf_counter()
    try:
        result = await target(*args, **kwargs)
    except Exception as exc:
        elapsed_ms = _elapsed_ms(started)
        runtime._capture_buffer.append_reserved_interaction(
            _tool_error_interaction(
                runtime,
                sequence=sequence,
                interaction_id=interaction_id,
                parent_id=parent_id,
                name=name,
                arguments=arguments,
                side_effect=side_effect,
                replay=replay,
                elapsed_ms=elapsed_ms,
                error=exc,
            )
        )
        raise
    finally:
        _current_tool_interaction_id.reset(token)

    elapsed_ms = _elapsed_ms(started)
    runtime._capture_buffer.append_reserved_interaction(
        _tool_success_interaction(
            runtime,
            sequence=sequence,
            interaction_id=interaction_id,
            parent_id=parent_id,
            name=name,
            arguments=arguments,
            side_effect=side_effect,
            replay=replay,
            elapsed_ms=elapsed_ms,
            result=result,
        )
    )
    return result


def _replay_tool_call(runtime: Any, *, name: str, arguments: dict[str, Any]) -> Any:
    scrubbed_arguments, _ = _scrub_value(arguments, "request.body.arguments")
    interaction = runtime._tool_replay_store.pop_match(name=name, arguments=scrubbed_arguments)
    replayed = runtime._capture_buffer.record_replay_interaction(
        interaction,
        fallback_tier="exact",
        fallback_reason="recorded tool call",
    )
    terminal = replayed.events[-1]
    if terminal.type == "error":
        data = terminal.data or {}
        raise _recorded_error(
            str(data.get("error_class", "RuntimeError")),
            str(data.get("message", "recorded tool error")),
        )
    if terminal.type != "response_received" or not isinstance(terminal.data, dict):
        raise ReplayMissError(f"recorded tool call for {name} has no replayable result")
    return terminal.data.get("result")


def _tool_success_interaction(
    runtime: Any,
    *,
    sequence: int,
    interaction_id: str,
    parent_id: str | None,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
    elapsed_ms: int,
    result: Any,
) -> CapturedInteraction:
    scrubbed_result, result_paths = _scrub_value(result, "events[1].data.result")
    event_data = {
        "result": scrubbed_result,
        "side_effect": side_effect,
        "replay": replay,
    }
    return _tool_interaction(
        runtime,
        sequence=sequence,
        interaction_id=interaction_id,
        parent_id=parent_id,
        name=name,
        arguments=arguments,
        side_effect=side_effect,
        replay=replay,
        elapsed_ms=elapsed_ms,
        events=(
            CapturedEvent(sequence=1, t_ms=0, sim_t_ms=0, type="request_sent"),
            CapturedEvent(
                sequence=2,
                t_ms=elapsed_ms,
                sim_t_ms=elapsed_ms,
                type="response_received",
                data=event_data,
            ),
        ),
        redacted_event_paths=result_paths,
    )


def _tool_error_interaction(
    runtime: Any,
    *,
    sequence: int,
    interaction_id: str,
    parent_id: str | None,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
    elapsed_ms: int,
    error: Exception,
    error_class: str | None = None,
    metadata: Mapping[str, Any] | None = None,
) -> CapturedInteraction:
    scrubbed_message, message_paths = _scrub_value(str(error), "events[1].data.message")
    error_data = {
        "error_class": error_class or error.__class__.__name__,
        "message": scrubbed_message,
        "side_effect": side_effect,
        "replay": replay,
    }
    if metadata:
        error_data.update(metadata)
    return _tool_interaction(
        runtime,
        sequence=sequence,
        interaction_id=interaction_id,
        parent_id=parent_id,
        name=name,
        arguments=arguments,
        side_effect=side_effect,
        replay=replay,
        elapsed_ms=elapsed_ms,
        events=(
            CapturedEvent(sequence=1, t_ms=0, sim_t_ms=0, type="request_sent"),
            CapturedEvent(
                sequence=2,
                t_ms=elapsed_ms,
                sim_t_ms=elapsed_ms,
                type="error",
                data=error_data,
            ),
        ),
        redacted_event_paths=message_paths,
    )


def _injected_error_class(error_kind: str, body: Any) -> str:
    if isinstance(body, Mapping):
        class_name = str(body.get("error_class", "")).strip()
        if class_name:
            return class_name
    return (
        "".join(part.capitalize() for part in re.split(r"[^A-Za-z0-9]+", error_kind) if part)
        + "Error"
    )


def _injected_error_message(error_kind: str, body: Any) -> str:
    if isinstance(body, Mapping):
        message = str(body.get("message", "")).strip()
        if message:
            return message
    return f"Injected Stagehand tool error: {error_kind}."


def _tool_interaction(
    runtime: Any,
    *,
    sequence: int,
    interaction_id: str,
    parent_id: str | None,
    name: str,
    arguments: dict[str, Any],
    side_effect: str,
    replay: str,
    elapsed_ms: int,
    events: tuple[CapturedEvent, ...],
    redacted_event_paths: tuple[str, ...],
) -> CapturedInteraction:
    scrubbed_arguments, argument_paths = _scrub_value(arguments, "request.body.arguments")
    request = CapturedRequest(
        url=f"stagehand://tool/{quote(name, safe='')}",
        method=_TOOL_METHOD,
        headers={},
        body={
            "name": name,
            "arguments": scrubbed_arguments,
            "side_effect": side_effect,
            "replay": replay,
        },
    )
    scrub_report = _scrub_report(runtime, argument_paths + redacted_event_paths)
    return CapturedInteraction(
        run_id=runtime.run_id,
        interaction_id=interaction_id,
        sequence=sequence,
        service=_TOOL_SERVICE,
        operation=name,
        protocol=_TOOL_PROTOCOL,
        request=request,
        events=events,
        scrub_report=scrub_report,
        streaming=False,
        parent_interaction_id=parent_id,
        latency_ms=elapsed_ms,
    )


def _scrub_report(runtime: Any, redacted_paths: tuple[str, ...]) -> CapturedScrubReport:
    base = runtime._capture_buffer.scrub_report
    return replace(base, redacted_paths=tuple(dict.fromkeys(redacted_paths)))


def _bound_arguments(
    signature: inspect.Signature,
    args: tuple[Any, ...],
    kwargs: dict[str, Any],
) -> dict[str, Any]:
    bound = signature.bind(*args, **kwargs)
    bound.apply_defaults()
    return dict(bound.arguments)


def _tool_key_from_interaction(interaction: CapturedInteraction) -> str:
    body = interaction.request.body if isinstance(interaction.request.body, dict) else {}
    return _tool_key(
        name=str(body.get("name") or interaction.operation),
        arguments=body.get("arguments") if isinstance(body.get("arguments"), dict) else {},
    )


def _tool_key(*, name: str, arguments: Mapping[str, Any]) -> str:
    return json.dumps(
        {"name": name, "arguments": _safe_value(arguments)},
        sort_keys=True,
        separators=(",", ":"),
    )


def _safe_value(value: Any) -> Any:
    if isinstance(value, dict):
        return {str(key): _safe_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_safe_value(item) for item in value]
    if value is None or isinstance(value, (bool, int, float, str)):
        return value
    return repr(value)


def _scrub_value(value: Any, path: str) -> tuple[Any, tuple[str, ...]]:
    if isinstance(value, dict):
        scrubbed: dict[str, Any] = {}
        paths: list[str] = []
        for key, item in value.items():
            child_path = f"{path}.{key}"
            if str(key).lower() in _SENSITIVE_KEYS:
                scrubbed[key] = "secret_scrubbed"
                paths.append(child_path)
                continue
            scrubbed_item, child_paths = _scrub_value(item, child_path)
            scrubbed[key] = scrubbed_item
            paths.extend(child_paths)
        return _safe_value(scrubbed), tuple(paths)
    if isinstance(value, (list, tuple)):
        scrubbed_items = []
        paths = []
        for index, item in enumerate(value):
            scrubbed_item, child_paths = _scrub_value(item, f"{path}[{index}]")
            scrubbed_items.append(scrubbed_item)
            paths.extend(child_paths)
        return _safe_value(scrubbed_items), tuple(paths)
    if isinstance(value, str):
        scrubbed = value
        paths = []
        for pattern, replacement in (
            (_JWT_PATTERN, "jwt_scrubbed"),
            (_EMAIL_PATTERN, "user_scrubbed@scrub.local"),
            (_CARD_PATTERN, "card_scrubbed"),
        ):
            if pattern.search(scrubbed):
                scrubbed = pattern.sub(replacement, scrubbed)
                paths.append(path)
        return scrubbed, tuple(paths)
    return value, ()


def _recorded_error(error_class: str, message: str) -> Exception:
    candidate = getattr(builtins, error_class, None)
    if isinstance(candidate, type) and issubclass(candidate, Exception):
        return candidate(message)
    dynamic_error = type(error_class, (RuntimeError,), {})
    return dynamic_error(message)


def _elapsed_ms(started: float) -> int:
    return max(0, int((time.perf_counter() - started) * 1000))
