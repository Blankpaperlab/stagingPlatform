from __future__ import annotations

import asyncio
from typing import Any

import pytest

import stagehand
from stagehand._runtime import _reset_for_tests


def test_tool_records_success_and_replays_recorded_result_without_calling_real_function() -> None:
    calls: list[str] = []

    @stagehand.tool(name="lookup_customer", side_effect="read", replay="recorded")
    def lookup_customer(email: str) -> dict[str, Any]:
        calls.append(email)
        return {"id": "cus_123", "email": email}

    record_runtime = stagehand.init(session="tool-success-record", mode="record")
    result = lookup_customer("jane@example.com")

    assert result == {"id": "cus_123", "email": "jane@example.com"}
    [recorded] = record_runtime.captured_interactions()
    assert recorded.service == "stagehand.tool"
    assert recorded.operation == "lookup_customer"
    assert recorded.protocol == "tool"
    assert recorded.request.method == "CALL"
    assert recorded.request.body == {
        "name": "lookup_customer",
        "arguments": {"email": "user_scrubbed@scrub.local"},
        "side_effect": "read",
        "replay": "recorded",
    }
    assert recorded.events[-1].type == "response_received"
    assert recorded.events[-1].data == {
        "result": {"id": "cus_123", "email": "user_scrubbed@scrub.local"},
        "side_effect": "read",
        "replay": "recorded",
    }
    assert recorded.scrub_report.redacted_paths == (
        "request.body.arguments.email",
        "events[1].data.result.email",
    )

    _reset_for_tests()

    replay_runtime = stagehand.init(session="tool-success-replay", mode="replay")
    assert stagehand.seed_replay_interactions(record_runtime.captured_interactions()) == 1

    replayed = lookup_customer("jane@example.com")

    assert replayed == {"id": "cus_123", "email": "user_scrubbed@scrub.local"}
    assert calls == ["jane@example.com"]
    [replay_interaction] = replay_runtime.captured_interactions()
    assert replay_interaction.fallback_tier == "exact"
    assert replay_interaction.fallback_reason == "recorded tool call"


def test_tool_records_and_replays_errors_without_calling_real_function() -> None:
    calls = 0

    @stagehand.tool(name="charge_card")
    def charge_card(card_number: str) -> str:
        nonlocal calls
        calls += 1
        raise ValueError(f"declined card {card_number}")

    record_runtime = stagehand.init(session="tool-error-record", mode="record")
    with pytest.raises(ValueError, match="declined card"):
        charge_card("4242 4242 4242 4242")

    [recorded] = record_runtime.captured_interactions()
    assert recorded.events[-1].type == "error"
    assert recorded.events[-1].data == {
        "error_class": "ValueError",
        "message": "declined card card_scrubbed",
        "side_effect": "read",
        "replay": "recorded",
    }
    assert "request.body.arguments.card_number" in recorded.scrub_report.redacted_paths
    assert "events[1].data.message" in recorded.scrub_report.redacted_paths

    _reset_for_tests()

    stagehand.init(session="tool-error-replay", mode="replay")
    assert stagehand.seed_replay_interactions((recorded,)) == 1

    with pytest.raises(ValueError, match="declined card card_scrubbed"):
        charge_card("4242 4242 4242 4242")
    assert calls == 1


def test_tool_async_function_records_and_replays() -> None:
    calls = 0

    @stagehand.tool(name="async_lookup")
    async def async_lookup(customer_id: str) -> dict[str, str]:
        nonlocal calls
        calls += 1
        await asyncio.sleep(0)
        return {"id": customer_id, "status": "active"}

    async def scenario() -> None:
        record_runtime = stagehand.init(session="tool-async-record", mode="record")
        assert await async_lookup("cus_123") == {"id": "cus_123", "status": "active"}
        recorded = record_runtime.captured_interactions()

        _reset_for_tests()

        stagehand.init(session="tool-async-replay", mode="replay")
        assert stagehand.seed_replay_interactions(recorded) == 1
        assert await async_lookup("cus_123") == {"id": "cus_123", "status": "active"}

    asyncio.run(scenario())
    assert calls == 1


def test_tool_nested_calls_preserve_call_order_and_parent_link() -> None:
    @stagehand.tool(name="inner_tool")
    def inner_tool(value: str) -> str:
        return value.upper()

    @stagehand.tool(name="outer_tool")
    def outer_tool(value: str) -> str:
        return f"outer:{inner_tool(value)}"

    runtime = stagehand.init(session="tool-nested-record", mode="record")
    assert outer_tool("abc") == "outer:ABC"

    outer, inner = runtime.captured_interactions()
    assert outer.operation == "outer_tool"
    assert inner.operation == "inner_tool"
    assert outer.sequence < inner.sequence
    assert inner.parent_interaction_id == outer.interaction_id


def test_tool_replay_fails_closed_on_missing_recorded_call() -> None:
    @stagehand.tool(name="lookup_customer")
    def lookup_customer(customer_id: str) -> dict[str, str]:
        return {"customer_id": customer_id}

    record_runtime = stagehand.init(session="tool-miss-record", mode="record")
    lookup_customer("cus_123")
    recorded = record_runtime.captured_interactions()

    _reset_for_tests()

    stagehand.init(session="tool-miss-replay", mode="replay")
    stagehand.seed_replay_interactions(recorded)

    with pytest.raises(stagehand.ReplayMissError, match="lookup_customer"):
        lookup_customer("cus_456")


def test_tool_can_be_used_without_parentheses() -> None:
    @stagehand.tool
    def calculate(value: int) -> int:
        return value * 2

    runtime = stagehand.init(session="tool-bare-record", mode="record")
    assert calculate(21) == 42

    [interaction] = runtime.captured_interactions()
    assert interaction.operation == "calculate"
    assert interaction.events[-1].data == {
        "result": 42,
        "side_effect": "read",
        "replay": "recorded",
    }
