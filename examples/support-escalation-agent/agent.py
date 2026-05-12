"""Support escalation example for Stagehand.

The flow combines a local business tool with an internal ticket API:

1. ``lookup_customer`` is wrapped with ``@stagehand.tool``.
2. The agent decides whether the case needs urgent escalation.
3. The agent creates a ticket through a mapped internal HTTP service.

Tests drive this module with ``httpx.MockTransport`` so the example is
deterministic while still using the real Stagehand Python interception paths.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from typing import Any, Callable

import httpx
import stagehand


SUPPORT_TICKET_URL = "https://support.internal.acme.test/api/tickets"
LOOKUP_CUSTOMER_CALLS = 0


@stagehand.tool(name="lookup_customer", side_effect="read", replay="recorded")
def lookup_customer(email: str) -> dict[str, Any]:
    global LOOKUP_CUSTOMER_CALLS
    LOOKUP_CUSTOMER_CALLS += 1
    return {
        "customer_id": "cus_support_123",
        "tier": "enterprise",
        "open_tickets": 3,
    }


@dataclass(frozen=True)
class EscalationResult:
    ticket_id: str
    customer_id: str
    priority: str
    status: str

    def to_dict(self) -> dict[str, str]:
        return {
            "ticket_id": self.ticket_id,
            "customer_id": self.customer_id,
            "priority": self.priority,
            "status": self.status,
        }


def run_support_escalation_agent(
    client: httpx.Client,
    *,
    customer_email: str,
    issue: str,
    request_id_factory: Callable[[], str] | None = None,
) -> EscalationResult:
    new_request_id = request_id_factory or _default_request_id
    customer = lookup_customer(customer_email)
    priority = (
        "urgent"
        if customer["tier"] == "enterprise" or customer["open_tickets"] >= 3
        else "normal"
    )

    response = client.post(
        SUPPORT_TICKET_URL,
        headers={
            "content-type": "application/json",
            "x-request-id": new_request_id(),
        },
        json={
            "customer_id": customer["customer_id"],
            "issue": issue,
            "priority": priority,
            "request_id": new_request_id(),
        },
    )
    response.raise_for_status()
    ticket = response.json()
    return EscalationResult(
        ticket_id=str(ticket["id"]),
        customer_id=str(customer["customer_id"]),
        priority=priority,
        status=str(ticket["status"]),
    )


def canned_ticket_response() -> dict[str, Any]:
    return {
        "id": "ticket_support_123",
        "status": "queued",
        "trace_id": "trace_ticket_record",
        "generated_at": "2026-05-12T12:00:00Z",
    }


def reset_example_state() -> None:
    global LOOKUP_CUSTOMER_CALLS
    LOOKUP_CUSTOMER_CALLS = 0


def _default_request_id() -> str:
    return f"req_{uuid.uuid4().hex[:12]}"
