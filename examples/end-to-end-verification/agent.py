from __future__ import annotations

import json
import os
import sys
from typing import Any

import openai
import stagehand
import stripe


DEFAULT_EMAIL = "refund-conformance@example.com"
DEFAULT_REASON = "Item arrived damaged, requesting full refund"


def run_refund_agent(customer_email: str, refund_reason: str) -> dict[str, Any]:
    client = openai.OpenAI(api_key=os.environ.get("OPENAI_API_KEY", "sk-stagehand-replay-placeholder"))
    stripe.api_key = os.environ.get("STRIPE_SECRET_KEY", "sk_test_stagehand_replay_placeholder")

    fixture = ensure_refundable_payment(customer_email)

    classification = client.chat.completions.create(
        model=os.environ.get("STAGEHAND_OPENAI_MODEL", "gpt-4o-mini"),
        temperature=0,
        messages=[
            {"role": "system", "content": classification_prompt()},
            {"role": "user", "content": f"Reason: {refund_reason}"},
        ],
    )
    decision = str(classification.choices[0].message.content or "").strip().upper()

    if not decision.startswith("APPROVE"):
        return {
            "status": "denied",
            "reason": refund_reason,
            "decision": decision,
            "fixture_customer_id": fixture["customer_id"],
            "fixture_payment_intent_id": fixture["payment_intent_id"],
        }

    customers = stripe.Customer.search(query=f"email:'{customer_email}'", limit=1)
    if not customers.data:
        return {"status": "error", "reason": "customer_not_found"}
    customer = customers.data[0]

    intents = stripe.PaymentIntent.list(customer=customer.id, limit=1)
    if not intents.data:
        return {"status": "error", "reason": "no_payments_found", "customer_id": customer.id}
    intent = intents.data[0]

    attempts = int(os.environ.get("STAGEHAND_REFUND_ATTEMPTS", "1"))
    force_attempts = os.environ.get("STAGEHAND_FORCE_REFUND_ATTEMPTS") == "1"
    refund_amount = int(os.environ.get("STAGEHAND_REFUND_AMOUNT", str(intent.amount)))
    refunds: list[dict[str, Any]] = []
    last_error: dict[str, Any] | None = None

    for attempt in range(1, attempts + 1):
        try:
            refund = stripe.Refund.create(
                payment_intent=intent.id,
                amount=refund_amount,
                reason="requested_by_customer",
                metadata={"stagehand_example": "end_to_end_verification"},
            )
        except stripe.StripeError as exc:
            error = getattr(exc, "error", None)
            last_error = {
                "attempt": attempt,
                "type": getattr(error, "type", exc.__class__.__name__),
                "code": getattr(error, "code", None),
                "message": str(exc),
            }
            break

        refunds.append(
            {
                "attempt": attempt,
                "refund_id": refund.id,
                "amount": refund.amount,
                "status": refund.status,
            }
        )
        if not force_attempts:
            break

    if last_error is not None:
        return {
            "status": "refund_failed",
            "customer_id": customer.id,
            "payment_intent_id": intent.id,
            "attempts": len(refunds) + 1,
            "refunds": refunds,
            "error": last_error,
        }

    if not refunds:
        return {
            "status": "error",
            "reason": "refund_not_created",
            "customer_id": customer.id,
            "payment_intent_id": intent.id,
        }

    if os.environ.get("STAGEHAND_EXTRA_STRIPE_RETRIEVE") == "1":
        stripe.Customer.retrieve(customer.id)

    return {
        "status": "refunded",
        "customer_id": customer.id,
        "payment_intent_id": intent.id,
        "refund_id": refunds[-1]["refund_id"],
        "amount": refunds[-1]["amount"],
        "attempts": len(refunds),
        "refunds": refunds,
    }


def ensure_refundable_payment(customer_email: str) -> dict[str, str]:
    if os.environ.get("STAGEHAND_SKIP_REFUND_FIXTURE") == "1":
        return {"customer_id": "", "payment_intent_id": ""}

    request_options: dict[str, Any] = {}
    idempotency_key = os.environ.get("STAGEHAND_IDEMPOTENCY_KEY")
    if idempotency_key:
        request_options["idempotency_key"] = idempotency_key

    metadata = {
        "stagehand_example": "end_to_end_verification",
    }
    if os.environ.get("STAGEHAND_SCRUB_MATRIX") == "1":
        metadata.update(
            {
                "stagehand_customer_email": customer_email,
                "stagehand_phone": os.environ.get("STAGEHAND_SAMPLE_PHONE", "+15555550100"),
                "stagehand_jwt": os.environ.get(
                    "STAGEHAND_SAMPLE_JWT",
                    "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzdGFnZWhhbmQifQ.signature",
                ),
            }
        )

    customer_params: dict[str, Any] = {
        "email": customer_email,
        "name": "Stagehand E2E Verification",
        "metadata": metadata,
    }
    if os.environ.get("STAGEHAND_SCRUB_MATRIX") == "1":
        customer_params["phone"] = os.environ.get("STAGEHAND_SAMPLE_PHONE", "+15555550100")

    customer = stripe.Customer.create(**customer_params, **request_options)
    intent = stripe.PaymentIntent.create(
        amount=int(os.environ.get("STAGEHAND_PAYMENT_AMOUNT", "1000")),
        currency="usd",
        customer=customer.id,
        payment_method="pm_card_visa",
        confirm=True,
        automatic_payment_methods={"enabled": True, "allow_redirects": "never"},
        metadata={"stagehand_example": "end_to_end_verification"},
    )
    return {"customer_id": customer.id, "payment_intent_id": intent.id}


def classification_prompt() -> str:
    style = os.environ.get("STAGEHAND_REFUND_PROMPT_STYLE", "baseline")
    if style == "careful":
        return "Carefully classify refund requests as APPROVE or DENY. Respond with only the word."
    return "Classify refund requests as APPROVE or DENY. Respond with only the word."


def main() -> int:
    stagehand.init_from_env()
    result = run_refund_agent(
        customer_email=os.environ.get("STAGEHAND_CUSTOMER_EMAIL", DEFAULT_EMAIL),
        refund_reason=os.environ.get("STAGEHAND_REFUND_REASON", DEFAULT_REASON),
    )
    output = json.dumps(result, indent=2, sort_keys=True)
    print(output)

    output_path = os.environ.get("STAGEHAND_SAMPLE_OUTPUT")
    if output_path:
        with open(output_path, "w", encoding="utf-8") as handle:
            handle.write(output)
            handle.write("\n")

    expected_status = os.environ.get("STAGEHAND_EXPECT_STATUS", "refunded")
    return 0 if result.get("status") == expected_status else 1


if __name__ == "__main__":
    sys.exit(main())
