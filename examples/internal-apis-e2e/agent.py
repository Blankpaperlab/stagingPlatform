"""Realistic new-hire provisioning agent that orchestrates internal HTTP APIs.

The agent calls three internal services that every call generates fresh dynamic
fields for (trace ids, request ids, idempotency keys, timestamps). Stagehand
captures the interactions in record mode and replays them offline in replay
mode while ignoring those dynamic fields per-service. See
``examples/internal-apis-e2e/stagehand.yml`` for the mapped-service config and
``sdk/python/tests/test_internal_apis_e2e.py`` for the end-to-end driver.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Callable

import httpx


HR_LOOKUP_URL = "https://hr.internal.acme.test/v1/employees/lookup"
BILLING_SEATS_URL = "https://billing.internal.acme.test/api/seats"
NOTIFY_EMAIL_URL = "https://notify.internal.acme.test/api/email"


@dataclass
class ProvisioningResult:
    employee_id: str
    seat_id: str
    notify_status: str

    def to_dict(self) -> dict[str, str]:
        return {
            "employee_id": self.employee_id,
            "seat_id": self.seat_id,
            "notify_status": self.notify_status,
        }


def run_provisioning_agent(
    client: httpx.Client,
    *,
    employee_email: str,
    plan: str = "starter",
    request_id_factory: Callable[[], str] | None = None,
    trace_id_factory: Callable[[], str] | None = None,
    now_factory: Callable[[], str] | None = None,
) -> ProvisioningResult:
    """Provision a new hire across HR, billing, and notification services.

    All four ``*_factory`` callables are deliberately injectable so a test can
    exercise the same agent twice with different per-call values for the
    dynamic fields that Stagehand is configured to ignore.
    """

    new_request_id = request_id_factory or _default_request_id
    new_trace_id = trace_id_factory or _default_trace_id
    now_iso = now_factory or _default_now_iso

    hr_response = client.post(
        f"{HR_LOOKUP_URL}?cursor={new_request_id()}",
        headers={
            "content-type": "application/json",
            "x-request-id": new_request_id(),
        },
        json={
            "email": employee_email,
            "request_id": new_request_id(),
            "created_at": now_iso(),
        },
    )
    hr_response.raise_for_status()
    employee = hr_response.json()["employee"]

    billing_response = client.post(
        BILLING_SEATS_URL,
        headers={
            "content-type": "application/json",
            "idempotency-key": f"seat-{employee['id']}-{new_request_id()}",
            "x-trace-id": new_trace_id(),
        },
        json={
            "employee_id": employee["id"],
            "plan": plan,
            "request_id": new_request_id(),
            "created_at": now_iso(),
        },
    )
    billing_response.raise_for_status()
    seat = billing_response.json()

    notify_response = client.post(
        NOTIFY_EMAIL_URL,
        headers={
            "content-type": "application/json",
            "x-trace-id": new_trace_id(),
        },
        json={
            "to": employee_email,
            "template": "welcome",
            "context": {
                "employee_name": employee["name"],
                "seat_id": seat["id"],
            },
            "request_id": new_request_id(),
            "created_at": now_iso(),
        },
    )
    notify_response.raise_for_status()

    return ProvisioningResult(
        employee_id=str(employee["id"]),
        seat_id=str(seat["id"]),
        notify_status=str(notify_response.json()["status"]),
    )


def _default_request_id() -> str:
    return f"req_{uuid.uuid4().hex[:12]}"


def _default_trace_id() -> str:
    return f"trace_{uuid.uuid4().hex[:12]}"


def _default_now_iso() -> str:
    return datetime.now(tz=timezone.utc).isoformat()


def canned_hr_response(*, email: str) -> dict[str, Any]:
    """Deterministic HR response shape used by record-mode test transports."""

    return {
        "employee": {
            "id": "emp_jane_doe",
            "name": "Jane Doe",
            "email": email,
            "department": "engineering",
            "start_date": "2026-05-12",
        },
        "trace_id": "trace_hr_record",
        "generated_at": "2026-05-07T12:00:00Z",
        "next_cursor": None,
    }


def canned_billing_response() -> dict[str, Any]:
    return {
        "id": "seat_emp_jane_doe",
        "status": "provisioned",
        "plan": "starter",
        "trace_id": "trace_billing_record",
        "generated_at": "2026-05-07T12:00:01Z",
    }


def canned_notify_response() -> dict[str, Any]:
    return {
        "status": "queued",
        "message_id": "msg_welcome_emp_jane_doe",
        "trace_id": "trace_notify_record",
        "generated_at": "2026-05-07T12:00:02Z",
    }
