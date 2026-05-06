import importlib.util
from typing import Any, Mapping

import pytest

import stagehand
from stagehand._runtime import _reset_for_tests

stripe = pytest.importorskip("stripe")
stripe_http_client = pytest.importorskip("stripe._http_client")


class RecordingStripeHTTPClient(stripe_http_client.HTTPClient):
    name = "stagehand-test"

    def __init__(self) -> None:
        super().__init__()
        self.requests: list[tuple[str, str, Mapping[str, str], Any]] = []

    def request(
        self,
        method: str,
        url: str,
        headers: Mapping[str, str],
        post_data: Any = None,
    ) -> tuple[bytes, int, Mapping[str, str]]:
        self.requests.append((method, url, headers, post_data))
        if "/v1/customers/search" in url:
            return (
                b'{"object":"search_result","data":[],"has_more":false,"url":"/v1/customers/search"}',
                200,
                {"content-type": "application/json", "request-id": "req_search"},
            )
        return (
            b'{"id":"re_123","object":"refund","amount":1000,"payment_intent":"pi_123"}',
            200,
            {"content-type": "application/json", "request-id": "req_123"},
        )


class FailingStripeHTTPClient(stripe_http_client.HTTPClient):
    name = "stagehand-fail"

    def request(
        self,
        method: str,
        url: str,
        headers: Mapping[str, str],
        post_data: Any = None,
    ) -> tuple[bytes, int, Mapping[str, str]]:
        raise AssertionError("Stripe replay should not call the underlying HTTP client")


@pytest.fixture(autouse=True)
def reset_stripe_globals() -> None:
    original_api_key = stripe.api_key
    original_default_http_client = stripe.default_http_client
    yield
    stripe.api_key = original_api_key
    stripe.default_http_client = original_default_http_client


def test_stripe_sdk_refund_create_is_captured_with_canonical_form_body() -> None:
    stagehand.init(session="stripe-sdk-record", mode="record")
    client = RecordingStripeHTTPClient()
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = client

    refund = stripe.Refund.create(
        payment_intent="pi_123",
        reason="requested_by_customer",
    )

    assert refund.id == "re_123"
    assert len(client.requests) == 1

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert interaction.service == "stripe"
    assert interaction.operation == "POST /v1/refunds"
    assert interaction.request.url == "https://api.stripe.com/v1/refunds"
    assert interaction.request.body == {
        "payment_intent": "pi_123",
        "reason": "requested_by_customer",
    }
    assert interaction.request.headers["authorization"] == ["Bearer sk_test_stagehand_1234567890"]
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["body"]["id"] == "re_123"


def test_stripe_sdk_customer_search_operation_uses_endpoint_path() -> None:
    stagehand.init(session="stripe-sdk-search", mode="record")
    client = RecordingStripeHTTPClient()
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = client

    stripe.Customer.search(query="email:'customer@example.com'")

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert interaction.service == "stripe"
    assert interaction.operation == "GET /v1/customers/search"
    assert interaction.request.url.startswith("https://api.stripe.com/v1/customers/search?")


def test_stripe_sdk_replay_returns_exact_response_without_network() -> None:
    record_runtime = stagehand.init(session="stripe-sdk-record", mode="record")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = RecordingStripeHTTPClient()

    recorded_refund = stripe.Refund.create(
        payment_intent="pi_123",
        reason="requested_by_customer",
    )
    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    replay_runtime = stagehand.init(session="stripe-sdk-replay", mode="replay")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = FailingStripeHTTPClient()
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    replayed_refund = stripe.Refund.create(
        payment_intent="pi_123",
        reason="requested_by_customer",
    )

    assert replayed_refund.id == recorded_refund.id
    assert replayed_refund.amount == recorded_refund.amount
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.service == "stripe"
    assert replayed_interaction.fallback_tier == "exact"


def test_stripe_sdk_replay_miss_fails_before_network() -> None:
    stagehand.init(session="stripe-sdk-miss", mode="replay")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = FailingStripeHTTPClient()

    with pytest.raises(stagehand.ReplayMissError, match="/v1/refunds"):
        stripe.Refund.create(
            payment_intent="pi_missing",
            reason="requested_by_customer",
        )


def test_stripe_sdk_replay_matches_scrubbed_email_body_shape() -> None:
    stagehand.init(session="stripe-sdk-scrubbed-body", mode="replay")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = FailingStripeHTTPClient()
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "stripe",
                    "operation": "POST /v1/customers",
                    "protocol": "https",
                    "request": {
                        "url": "https://api.stripe.com/v1/customers",
                        "method": "POST",
                        "body": {
                            "email": "user_scrubbed@scrub.local",
                            "name": "Example Customer",
                        },
                    },
                    "events": [
                        {
                            "sequence": 1,
                            "t_ms": 0,
                            "sim_t_ms": 0,
                            "type": "request_sent",
                        },
                        {
                            "sequence": 2,
                            "t_ms": 1,
                            "sim_t_ms": 1,
                            "type": "response_received",
                            "data": {
                                "status_code": 200,
                                "headers": {"content-type": ["application/json"]},
                                "body": {"id": "cus_123", "object": "customer"},
                            },
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v1",
                        "session_salt_id": "salt_test",
                        "redacted_paths": ["request.body.email"],
                    },
                }
            ]
        )
        == 1
    )

    customer = stripe.Customer.create(
        email="customer@example.com",
        name="Example Customer",
    )

    assert customer.id == "cus_123"


def test_stripe_sdk_replay_matches_scrubbed_sensitive_metadata_shape() -> None:
    stagehand.init(session="stripe-sdk-scrubbed-sensitive-metadata", mode="replay")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = FailingStripeHTTPClient()
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "stripe",
                    "operation": "POST /v1/customers",
                    "protocol": "https",
                    "request": {
                        "url": "https://api.stripe.com/v1/customers",
                        "method": "POST",
                        "body": {
                            "email": "user_scrubbed@scrub.local",
                            "name": "Example Customer",
                            "phone": "hash_phone_scrubbed",
                            "metadata": {
                                "stagehand_customer_email": "user_scrubbed@scrub.local",
                                "stagehand_jwt": "hash_jwt_scrubbed",
                            },
                        },
                    },
                    "events": [
                        {
                            "sequence": 1,
                            "t_ms": 0,
                            "sim_t_ms": 0,
                            "type": "request_sent",
                        },
                        {
                            "sequence": 2,
                            "t_ms": 1,
                            "sim_t_ms": 1,
                            "type": "response_received",
                            "data": {
                                "status_code": 200,
                                "headers": {"content-type": ["application/json"]},
                                "body": {"id": "cus_123", "object": "customer"},
                            },
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v1",
                        "session_salt_id": "salt_test",
                        "redacted_paths": [
                            "request.body.email",
                            "request.body.phone",
                            "request.body.metadata.stagehand_customer_email",
                            "request.body.metadata.stagehand_jwt",
                        ],
                    },
                }
            ]
        )
        == 1
    )

    customer = stripe.Customer.create(
        email="customer@example.com",
        name="Example Customer",
        phone="+15555550100",
        metadata={
            "stagehand_customer_email": "customer@example.com",
            "stagehand_jwt": "eyJhbGciOiJIUzI1NiJ9.payload.signature",
        },
    )

    assert customer.id == "cus_123"


def test_stripe_sdk_replay_matches_scrubbed_search_query_shape() -> None:
    stagehand.init(session="stripe-sdk-scrubbed-query", mode="replay")
    stripe.api_key = "sk_test_stagehand_1234567890"
    stripe.default_http_client = FailingStripeHTTPClient()
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "stripe",
                    "operation": "GET /v1/customers/search",
                    "protocol": "https",
                    "request": {
                        "url": "https://api.stripe.com/v1/customers/search?limit=1&query=email%3A%27user_scrubbed%40scrub.local%27",
                        "method": "GET",
                    },
                    "events": [
                        {
                            "sequence": 1,
                            "t_ms": 0,
                            "sim_t_ms": 0,
                            "type": "request_sent",
                        },
                        {
                            "sequence": 2,
                            "t_ms": 1,
                            "sim_t_ms": 1,
                            "type": "response_received",
                            "data": {
                                "status_code": 200,
                                "headers": {"content-type": ["application/json"]},
                                "body": {
                                    "object": "search_result",
                                    "data": [{"id": "cus_123", "object": "customer"}],
                                    "has_more": False,
                                    "url": "/v1/customers/search",
                                },
                            },
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v1",
                        "session_salt_id": "salt_test",
                        "redacted_paths": ["request.query.query"],
                    },
                }
            ]
        )
        == 1
    )

    customers = stripe.Customer.search(query="email:'customer@example.com'", limit=1)

    assert customers.data[0].id == "cus_123"


def test_stripe_package_is_available_for_z1_regression_coverage() -> None:
    assert importlib.util.find_spec("stripe") is not None
