from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from typing import Any, Callable

import httpx

import stagehand
from stagehand._runtime import _reset_for_tests


_EXAMPLE_DIR = Path(__file__).resolve().parents[3] / "examples" / "support-escalation-agent"
_CONFIG_PATH = _EXAMPLE_DIR / "stagehand.yml"


def test_support_escalation_example_records_and_replays_tool_plus_internal_api() -> None:
    agent = _load_support_agent()
    agent.reset_example_state()

    record_runtime = stagehand.init(
        session="support-escalation-record",
        mode="record",
        config_path=_CONFIG_PATH,
    )
    with httpx.Client(transport=_record_transport(agent)) as client:
        recorded = agent.run_support_escalation_agent(
            client,
            customer_email="jane.doe@example.com",
            issue="Customer reports repeated billing failures",
            request_id_factory=_seq_factory("rec_req"),
        )

    assert recorded.to_dict() == {
        "ticket_id": "ticket_support_123",
        "customer_id": "cus_support_123",
        "priority": "urgent",
        "status": "queued",
    }
    assert agent.LOOKUP_CUSTOMER_CALLS == 1

    recorded_interactions = record_runtime.captured_interactions()
    assert [interaction.service for interaction in recorded_interactions] == [
        "stagehand.tool",
        "support-ticket-api",
    ]
    assert [interaction.operation for interaction in recorded_interactions] == [
        "lookup_customer",
        "POST /api/tickets",
    ]

    _reset_for_tests()
    replay_runtime = stagehand.init(
        session="support-escalation-replay",
        mode="replay",
        config_path=_CONFIG_PATH,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 2

    with httpx.Client(transport=_fail_if_network_used()) as client:
        replayed = agent.run_support_escalation_agent(
            client,
            customer_email="jane.doe@example.com",
            issue="Customer reports repeated billing failures",
            request_id_factory=_seq_factory("rpl_req"),
        )

    assert replayed.to_dict() == recorded.to_dict()
    assert agent.LOOKUP_CUSTOMER_CALLS == 1

    replayed_interactions = replay_runtime.captured_interactions()
    assert len(replayed_interactions) == 2
    assert [interaction.fallback_tier for interaction in replayed_interactions] == [
        "exact",
        "exact",
    ]


def _load_support_agent() -> Any:
    agent_path = _EXAMPLE_DIR / "agent.py"
    spec = importlib.util.spec_from_file_location(
        "stagehand_example_support_escalation_agent", agent_path
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _record_transport(agent: Any) -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        if str(request.url) == agent.SUPPORT_TICKET_URL:
            return httpx.Response(status_code=202, json=agent.canned_ticket_response())
        raise AssertionError(f"unexpected URL during support example record: {request.url}")

    return httpx.MockTransport(handler)


def _fail_if_network_used() -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        raise AssertionError(f"support example replay should be offline: {request.url}")

    return httpx.MockTransport(handler)


def _seq_factory(prefix: str) -> Callable[[], str]:
    counter = 0

    def next_value() -> str:
        nonlocal counter
        counter += 1
        return f"{prefix}_{counter:04d}"

    return next_value
