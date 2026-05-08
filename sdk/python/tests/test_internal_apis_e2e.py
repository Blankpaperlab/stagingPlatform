"""End-to-end Stagehand workflow for the internal-APIs e2e example.

This drives the realistic new-hire provisioning agent in
``examples/internal-apis-e2e/agent.py`` through the full Stagehand workflow:

1. Record mode against fake transports for HR, billing, and notifier services.
2. Replay mode where every dynamic field (request id, trace id, idempotency
   key, timestamp, cursor) is freshly generated -- exact replay still hits
   because ``stagehand.yml`` lists those fields under ``ignore.request_paths``
   for each mapped service.
3. Replay mode with a non-ignored business input change - Stagehand fails
   closed instead of silently returning a stale response.

The agent uses a real ``httpx.Client`` so this is a true e2e through the SDK's
HTTP interception path, not a unit test of the replay store.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from typing import Any, Iterator

import httpx
import pytest

import stagehand
from stagehand._runtime import _reset_for_tests


_SESSION_RECORD = "internal-apis-e2e-record"
_SESSION_REPLAY = "internal-apis-e2e-replay"


# --------------------------------------------------------------------------- #
# Agent loading
# --------------------------------------------------------------------------- #

_EXAMPLE_DIR = Path(__file__).resolve().parents[3] / "examples" / "internal-apis-e2e"
_CONFIG_PATH = _EXAMPLE_DIR / "stagehand.yml"


def _load_agent_module() -> Any:
    agent_path = _EXAMPLE_DIR / "agent.py"
    spec = importlib.util.spec_from_file_location(
        "stagehand_example_internal_apis_e2e_agent", agent_path
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def agent_module() -> Any:
    return _load_agent_module()


# --------------------------------------------------------------------------- #
# Test transports
# --------------------------------------------------------------------------- #


def _record_transport(agent: Any) -> httpx.MockTransport:
    """Fake versions of HR, billing, and notifier services for record mode."""

    def handler(request: httpx.Request) -> httpx.Response:
        url = str(request.url)
        if url.startswith(agent.HR_LOOKUP_URL):
            return httpx.Response(
                status_code=200,
                json=agent.canned_hr_response(email=_request_json(request)["email"]),
            )
        if url.startswith(agent.BILLING_SEATS_URL):
            return httpx.Response(status_code=201, json=agent.canned_billing_response())
        if url.startswith(agent.NOTIFY_EMAIL_URL):
            return httpx.Response(status_code=202, json=agent.canned_notify_response())
        raise AssertionError(f"unexpected URL during record: {url}")

    return httpx.MockTransport(handler)


def _fail_if_network_used(message: str) -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        raise AssertionError(f"{message}: {request.method} {request.url}")

    return httpx.MockTransport(handler)


def _request_json(request: httpx.Request) -> dict[str, Any]:
    import json

    return json.loads(request.content.decode("utf-8")) if request.content else {}


# --------------------------------------------------------------------------- #
# Helpers
# --------------------------------------------------------------------------- #


def _seq_factory(prefix: str) -> Iterator[str]:
    counter = 0
    while True:
        counter += 1
        yield f"{prefix}_{counter:04d}"


def _record_dynamic_fields() -> dict[str, Any]:
    return {
        "request_id_factory": (lambda gen=_seq_factory("rec_req"): next(gen)),
        "trace_id_factory": (lambda gen=_seq_factory("rec_trace"): next(gen)),
        "now_factory": (lambda: "2026-05-07T12:00:00Z"),
    }


def _replay_dynamic_fields() -> dict[str, Any]:
    """Brand-new per-call values that must be ignored during exact replay."""

    return {
        "request_id_factory": (lambda gen=_seq_factory("rpl_req"): next(gen)),
        "trace_id_factory": (lambda gen=_seq_factory("rpl_trace"): next(gen)),
        "now_factory": (lambda: "2026-05-08T18:30:00Z"),
    }


# --------------------------------------------------------------------------- #
# Tests
# --------------------------------------------------------------------------- #


def test_record_mode_labels_three_internal_services_with_mapped_operation_names(
    agent_module: Any,
) -> None:
    runtime = stagehand.init(session=_SESSION_RECORD, mode="record", config_path=_CONFIG_PATH)

    with httpx.Client(transport=_record_transport(agent_module)) as client:
        result = agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            **_record_dynamic_fields(),
        )

    assert result.employee_id == "emp_jane_doe"
    assert result.seat_id == "seat_emp_jane_doe"
    assert result.notify_status == "queued"

    interactions = runtime.captured_interactions()
    assert len(interactions) == 3
    assert [i.service for i in interactions] == [
        "internal-hr",
        "internal-billing",
        "internal-notifier",
    ]
    assert [i.operation for i in interactions] == [
        "POST /v1/employees/lookup",
        "POST /api/seats",
        "POST /api/email",
    ]


def test_replay_mode_runs_offline_despite_per_call_dynamic_field_variation(
    agent_module: Any,
) -> None:
    record_runtime = stagehand.init(
        session=_SESSION_RECORD, mode="record", config_path=_CONFIG_PATH
    )
    with httpx.Client(transport=_record_transport(agent_module)) as client:
        recorded = agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            **_record_dynamic_fields(),
        )
    recorded_interactions = record_runtime.captured_interactions()

    _reset_for_tests()

    replay_runtime = stagehand.init(
        session=_SESSION_REPLAY, mode="replay", config_path=_CONFIG_PATH
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 3

    with httpx.Client(
        transport=_fail_if_network_used(
            "exact replay must not reach the live transport even with new dynamic fields"
        )
    ) as client:
        replayed = agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            **_replay_dynamic_fields(),
        )

    assert replayed.to_dict() == recorded.to_dict()

    replayed_interactions = replay_runtime.captured_interactions()
    assert len(replayed_interactions) == 3
    assert all(i.fallback_tier == "exact" for i in replayed_interactions)
    assert [i.service for i in replayed_interactions] == [
        "internal-hr",
        "internal-billing",
        "internal-notifier",
    ]


def test_replay_mode_falls_through_to_nearest_neighbor_for_tier_1_service(
    agent_module: Any,
) -> None:
    """``internal-billing`` opts into tier 1 -- a plan variation is allowed to
    nearest-neighbor match against the recorded run while ``internal-hr`` and
    ``internal-notifier`` remain exact-only."""

    record_runtime = stagehand.init(
        session=_SESSION_RECORD, mode="record", config_path=_CONFIG_PATH
    )
    with httpx.Client(transport=_record_transport(agent_module)) as client:
        agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            **_record_dynamic_fields(),
        )
    recorded_interactions = record_runtime.captured_interactions()

    _reset_for_tests()
    replay_runtime = stagehand.init(
        session=_SESSION_REPLAY, mode="replay", config_path=_CONFIG_PATH
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 3

    with httpx.Client(
        transport=_fail_if_network_used("tier-1 nearest-neighbor must keep the agent offline")
    ) as client:
        result = agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            plan="enterprise",
            **_replay_dynamic_fields(),
        )

    assert result.seat_id == "seat_emp_jane_doe"

    interactions_by_service = {i.service: i for i in replay_runtime.captured_interactions()}
    assert interactions_by_service["internal-hr"].fallback_tier == "exact"
    assert interactions_by_service["internal-billing"].fallback_tier == "nearest_neighbor"
    assert interactions_by_service["internal-billing"].fallback_reason
    assert interactions_by_service["internal-notifier"].fallback_tier == "exact"


def test_exact_only_service_still_fails_closed_when_tier_1_neighbor_exists(
    agent_module: Any,
) -> None:
    """``internal-hr`` is exact-only (allowed_tiers: [0]) so even an obvious
    near match like a different email must miss instead of nearest-neighbor."""

    record_runtime = stagehand.init(
        session=_SESSION_RECORD, mode="record", config_path=_CONFIG_PATH
    )
    with httpx.Client(transport=_record_transport(agent_module)) as client:
        agent_module.run_provisioning_agent(
            client,
            employee_email="jane.doe@acme.test",
            **_record_dynamic_fields(),
        )
    recorded_interactions = record_runtime.captured_interactions()

    _reset_for_tests()
    stagehand.init(session=_SESSION_REPLAY, mode="replay", config_path=_CONFIG_PATH)
    assert stagehand.seed_replay_interactions(recorded_interactions) == 3

    fail_transport = _fail_if_network_used(
        "exact-only service must not fall through to live network"
    )
    with pytest.raises(stagehand.ReplayMissError):
        with httpx.Client(transport=fail_transport) as client:
            agent_module.run_provisioning_agent(
                client,
                employee_email="someone.else@acme.test",
                **_replay_dynamic_fields(),
            )
