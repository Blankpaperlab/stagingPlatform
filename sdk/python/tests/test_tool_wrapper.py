from __future__ import annotations

import asyncio
import json
from typing import Any

import httpx
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


def test_tool_replay_is_deterministic_back_to_back() -> None:
    # Catches: replay serving different captured payloads or sequence metadata across identical replays.
    calls = 0

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(customer_id: str) -> dict[str, Any]:
        nonlocal calls
        calls += 1
        return {"customer_id": customer_id, "live_call": calls}

    record_runtime = stagehand.init(session="tool-determinism-record", mode="record")
    lookup_customer("cus_001")
    recorded = record_runtime.captured_interactions()

    replays: list[tuple[dict[str, Any], ...]] = []
    for index in range(2):
        _reset_for_tests()
        replay_runtime = stagehand.init(session=f"tool-determinism-replay-{index}", mode="replay")
        assert stagehand.seed_replay_interactions(recorded) == 1
        assert lookup_customer("cus_001") == {"customer_id": "cus_001", "live_call": 1}
        replays.append(
            tuple(
                _canonical_tool_interaction(item) for item in replay_runtime.captured_interactions()
            )
        )

    assert replays[0] == replays[1]
    assert calls == 1


def test_tool_replay_does_not_call_live_function_that_would_hit_network() -> None:
    # Catches: replay falling through to the wrapped implementation and making live network calls.
    attempts = 0
    should_attempt_network = False

    def blocked_transport(request: httpx.Request) -> httpx.Response:
        nonlocal attempts
        attempts += 1
        raise AssertionError(f"network blocked: attempted live request to {request.url}")

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(customer_id: str) -> dict[str, str]:
        if should_attempt_network:
            with httpx.Client(transport=httpx.MockTransport(blocked_transport)) as client:
                client.get("https://crm.internal.test/customers/" + customer_id)
        return {"customer_id": customer_id, "source": "live"}

    record_runtime = stagehand.init(session="tool-offline-record", mode="record")
    lookup_customer("cus_001")
    recorded = record_runtime.captured_interactions()

    _reset_for_tests()

    should_attempt_network = True
    stagehand.init(session="tool-offline-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded) == 1
    assert lookup_customer("cus_001") == {"customer_id": "cus_001", "source": "live"}
    assert attempts == 0


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


def test_tool_error_injection_records_typed_error_and_provenance(tmp_path, monkeypatch) -> None:
    injection_path = tmp_path / "injection.json"
    injection_path.write_text(
        json.dumps(
            {
                "schema_version": "v1alpha1",
                "rules": [
                    {
                        "name": "lookup failure on second call",
                        "match": {
                            "tool": "lookup_customer",
                            "nth_call": 2,
                            "probability": 1.0,
                        },
                        "inject": {
                            "error": "not_found",
                            "body": {
                                "message": "customer missing",
                                "error_class": "CustomerNotFoundError",
                            },
                        },
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
    calls = 0

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(email: str) -> dict[str, str]:
        nonlocal calls
        calls += 1
        return {"email": email}

    runtime = stagehand.init(session="tool-injection-record", mode="record")

    assert lookup_customer("jane@example.com") == {"email": "jane@example.com"}
    with pytest.raises(RuntimeError, match="customer missing"):
        lookup_customer("jane@example.com")

    assert calls == 1
    first, injected = runtime.captured_interactions()
    assert first.events[-1].type == "response_received"
    assert injected.events[-1].type == "error"
    assert injected.events[-1].data == {
        "error_class": "CustomerNotFoundError",
        "message": "customer missing",
        "side_effect": "read",
        "replay": "recorded",
        "stagehand_injection": {
            "rule_index": 0,
            "service": "stagehand.tool",
            "operation": "lookup_customer",
            "call_number": 2,
            "tool": "lookup_customer",
            "name": "lookup failure on second call",
            "nth_call": 2,
            "probability": 1.0,
            "error": "not_found",
        },
    }
    applied = runtime.capture_bundle_dict()["metadata"]["error_injection"]["applied"]
    assert applied[0]["tool"] == "lookup_customer"


def test_tool_error_injection_is_deterministic_across_repeated_runs(tmp_path, monkeypatch) -> None:
    # Catches: deterministic nth_call injection behaving probabilistically or resetting counters incorrectly.
    injection_path = tmp_path / "injection.json"
    injection_path.write_text(
        json.dumps(
            {
                "rules": [
                    {
                        "name": "lookup failure on second call",
                        "match": {"tool": "lookup_customer", "nth_call": 2, "probability": 1.0},
                        "inject": {
                            "error": "not_found",
                            "body": {
                                "message": "customer missing",
                                "error_class": "CustomerNotFoundError",
                            },
                        },
                    }
                ]
            }
        ),
        encoding="utf-8",
    )

    for index in range(5):
        _reset_for_tests()
        monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
        calls = 0

        @stagehand.tool(name="lookup_customer")
        def lookup_customer(customer_id: str) -> dict[str, str]:
            nonlocal calls
            calls += 1
            return {"customer_id": customer_id}

        runtime = stagehand.init(session=f"tool-injection-repeat-{index}", mode="record")
        outcomes: list[str] = []
        for customer_id in ("cus_001", "cus_002", "cus_003"):
            try:
                lookup_customer(customer_id)
                outcomes.append("success")
            except RuntimeError as exc:
                outcomes.append(f"{exc.__class__.__name__}:{exc}")

        assert outcomes == ["success", "CustomerNotFoundError:customer missing", "success"]
        assert calls == 2
        interactions = runtime.captured_interactions()
        assert [interaction.events[-1].type for interaction in interactions] == [
            "response_received",
            "error",
            "response_received",
        ]
        assert interactions[1].events[-1].data["stagehand_injection"]["call_number"] == 2


def test_tool_error_injection_control_case_does_not_fire_for_unmatched_tool(
    tmp_path, monkeypatch
) -> None:
    # Catches: injection firing even when the configured tool name does not match.
    injection_path = tmp_path / "injection.json"
    injection_path.write_text(
        json.dumps(
            {
                "rules": [
                    {
                        "name": "different tool failure",
                        "match": {"tool": "delete_customer", "nth_call": 1},
                        "inject": {"error": "not_found", "body": {"message": "should not fire"}},
                    }
                ]
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
    calls = 0

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(customer_id: str) -> dict[str, str]:
        nonlocal calls
        calls += 1
        return {"customer_id": customer_id}

    runtime = stagehand.init(session="tool-injection-control", mode="record")
    assert lookup_customer("cus_001") == {"customer_id": "cus_001"}
    assert lookup_customer("cus_002") == {"customer_id": "cus_002"}
    assert calls == 2
    assert "error_injection" not in runtime.capture_bundle_dict()["metadata"]
    assert [interaction.events[-1].type for interaction in runtime.captured_interactions()] == [
        "response_received",
        "response_received",
    ]


def test_tool_argument_and_result_scrubbing_remove_plaintext_from_capture_bundle() -> None:
    # Catches: scrub metadata being present while persisted tool args/results still contain plaintext secrets.
    sensitive_email = "real.user@example-corp.com"
    sensitive_card = "4242 4242 4242 4242"
    sensitive_token = "secret-live-token"

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(email: str, card_number: str, token: str) -> dict[str, str]:
        return {"email": email, "card_number": card_number, "token": token}

    runtime = stagehand.init(session="tool-scrub-record", mode="record")
    lookup_customer(sensitive_email, sensitive_card, sensitive_token)
    [interaction] = runtime.captured_interactions()
    bundle_text = json.dumps(runtime.capture_bundle_dict(), sort_keys=True)

    assert sensitive_email not in bundle_text
    assert sensitive_card not in bundle_text
    assert sensitive_token not in bundle_text
    assert set(interaction.scrub_report.redacted_paths) >= {
        "request.body.arguments.email",
        "request.body.arguments.card_number",
        "request.body.arguments.token",
        "events[1].data.result.email",
        "events[1].data.result.card_number",
        "events[1].data.result.token",
    }


def test_tool_replay_preserves_distinguishable_call_order() -> None:
    # Catches: replay store serving same-name tool calls in the wrong occurrence order.
    calls: list[str] = []

    @stagehand.tool(name="lookup_customer")
    def lookup_customer(customer_id: str) -> dict[str, str]:
        calls.append(customer_id)
        return {"customer_id": customer_id, "ordinal": str(len(calls))}

    record_runtime = stagehand.init(session="tool-order-record", mode="record")
    for customer_id in ("cus_001", "cus_002", "cus_003"):
        lookup_customer(customer_id)
    recorded = record_runtime.captured_interactions()

    _reset_for_tests()

    stagehand.init(session="tool-order-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded) == 3
    replayed = [lookup_customer(customer_id) for customer_id in ("cus_001", "cus_002", "cus_003")]

    assert replayed == [
        {"customer_id": "cus_001", "ordinal": "1"},
        {"customer_id": "cus_002", "ordinal": "2"},
        {"customer_id": "cus_003", "ordinal": "3"},
    ]
    assert calls == ["cus_001", "cus_002", "cus_003"]


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


def _canonical_tool_interaction(interaction: Any) -> dict[str, Any]:
    terminal = interaction.events[-1]
    return {
        "sequence": interaction.sequence,
        "service": interaction.service,
        "operation": interaction.operation,
        "protocol": interaction.protocol,
        "request": interaction.request.body,
        "terminal_type": terminal.type,
        "terminal_data": terminal.data,
        "fallback_tier": interaction.fallback_tier,
        "fallback_reason": interaction.fallback_reason,
    }
