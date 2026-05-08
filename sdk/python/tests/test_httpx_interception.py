import asyncio
import importlib.util
import json
from pathlib import Path

import httpx
import pytest

import stagehand
from stagehand._config import load_service_mappings
from stagehand._runtime import _reset_for_tests


def test_load_service_mappings_parses_ignore_request_and_response_paths(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    ignore:
      request_paths:
        - headers.x-request-id
        - query.cursor
        - body.request_id
      response_paths:
        - body.generated_at
        - body.trace_id
  - name: billing-api
    type: api
    match:
      host: legacy.internal.test
""",
        encoding="utf-8",
    )

    mappings = load_service_mappings(config_path)
    by_name = {mapping.name: mapping for mapping in mappings}

    billing = by_name["internal-billing"]
    assert billing.ignore_request_paths == (
        "headers.x-request-id",
        "query.cursor",
        "body.request_id",
    )
    assert billing.ignore_response_paths == ("body.generated_at", "body.trace_id")

    legacy = by_name["billing-api"]
    assert legacy.ignore_request_paths == ()
    assert legacy.ignore_response_paths == ()


def test_load_service_mappings_drops_blank_ignore_entries(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    ignore:
      request_paths:
        - "  "
        - body.request_id
        - ""
""",
        encoding="utf-8",
    )

    mappings = load_service_mappings(config_path)
    assert len(mappings) == 1
    assert mappings[0].ignore_request_paths == ("body.request_id",)


def test_httpx_sync_response_is_captured() -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url == "https://api.openai.com/v1/chat/completions"
        return httpx.Response(
            status_code=201,
            headers={"x-stagehand-test": "sync"},
            json={"id": "resp_123", "ok": True},
        )

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        response = client.post(
            "https://api.openai.com/v1/chat/completions",
            json={"model": "gpt-5.4", "messages": [{"role": "user", "content": "hi"}]},
        )

    assert response.status_code == 201

    runtime = stagehand.get_runtime()
    interactions = runtime.captured_interactions()
    assert len(interactions) == 1

    interaction = interactions[0]
    assert interaction.run_id == runtime.run_id
    assert interaction.sequence == 1
    assert interaction.service == "openai"
    assert interaction.operation == "chat.completions.create"
    assert interaction.protocol == "https"
    assert interaction.request.method == "POST"
    assert interaction.request.body == {
        "model": "gpt-5.4",
        "messages": [{"role": "user", "content": "hi"}],
    }
    assert interaction.request.headers["content-type"] == ["application/json"]
    assert interaction.events[0].type == "request_sent"
    assert interaction.events[1].type == "response_received"
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["status_code"] == 201
    assert interaction.events[1].data["headers"]["x-stagehand-test"] == ["sync"]
    assert interaction.events[1].data["body"] == {"id": "resp_123", "ok": True}
    assert interaction.latency_ms is not None
    assert interaction.latency_ms >= 0


def test_httpx_async_response_is_captured() -> None:
    async def exercise() -> None:
        stagehand.init(session="demo", mode="passthrough")

        async def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                status_code=200,
                headers={"content-type": "application/json"},
                json={"service": "anthropic"},
            )

        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            response = await client.get("https://api.anthropic.com/v1/messages")

        assert response.status_code == 200

    asyncio.run(exercise())

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert interaction.service == "anthropic"
    assert interaction.operation == "GET /v1/messages"
    assert interaction.events[1].type == "response_received"
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["body"] == {"service": "anthropic"}


def test_httpx_timeout_is_captured_as_timeout_event() -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("read timed out", request=request)

    with pytest.raises(httpx.ReadTimeout):
        with httpx.Client(transport=httpx.MockTransport(handler)) as client:
            client.get("https://api.stripe.com/v1/customers")

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert interaction.service == "stripe"
    assert interaction.events[0].type == "request_sent"
    assert interaction.events[1].type == "timeout"
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["error_class"] == "ReadTimeout"
    assert interaction.events[1].data["message"] == "read timed out"


def test_httpx_generic_error_is_captured_as_error_event() -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused", request=request)

    with pytest.raises(httpx.ConnectError):
        with httpx.Client(transport=httpx.MockTransport(handler)) as client:
            client.get("https://example.internal/v1/ping")

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert interaction.service == "example.internal"
    assert interaction.events[1].type == "error"
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["error_class"] == "ConnectError"
    assert interaction.events[1].data["message"] == "connection refused"


def test_custom_openai_host_is_classified_and_replayed(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv(stagehand.ENV_OPENAI_HOSTS, "gateway.openai.local")
    record_runtime = stagehand.init(session="custom-openai-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        assert request.url == "https://gateway.openai.local/v1/chat/completions"
        return httpx.Response(
            status_code=200,
            headers={"content-type": "application/json"},
            json={"id": "resp_custom", "ok": True},
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        response = client.post(
            "https://gateway.openai.local/v1/chat/completions",
            json={"model": "gpt-5.4", "messages": [{"role": "user", "content": "hi"}]},
        )

    assert response.status_code == 200
    captured = record_runtime.captured_interactions()[0]
    assert captured.service == "openai"
    assert captured.operation == "chat.completions.create"

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    monkeypatch.setenv(stagehand.ENV_OPENAI_HOSTS, "gateway.openai.local")
    replay_runtime = stagehand.init(session="custom-openai-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("custom-host replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed = client.post(
            "https://gateway.openai.local/v1/chat/completions",
            json={"model": "gpt-5.4", "messages": [{"role": "user", "content": "hi"}]},
        )

    assert replayed.status_code == 200
    assert replayed.json() == {"id": "resp_custom", "ok": True}
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"


def test_captured_interaction_dicts_match_artifact_shape() -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=204)

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        client.delete("https://api.stripe.com/v1/customers/cus_123")

    payload = stagehand.get_runtime().captured_interaction_dicts()[0]
    assert payload["run_id"].startswith("run_")
    assert payload["interaction_id"].startswith("int_")
    assert payload["sequence"] == 1
    assert payload["service"] == "stripe"
    assert payload["operation"] == "DELETE /v1/customers/cus_123"
    assert payload["protocol"] == "https"
    assert payload["request"]["method"] == "DELETE"
    assert payload["events"][0]["type"] == "request_sent"
    assert payload["events"][1]["type"] == "response_received"
    assert payload["scrub_report"]["scrub_policy_version"] == "v0-unredacted"
    assert payload["scrub_report"]["session_salt_id"] == "salt_pending"


def test_openai_sse_capture_preserves_chunk_order_and_tool_boundaries() -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        stream = httpx.ByteStream(
            b"".join(
                [
                    b'data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant"},"index":0}]}\n\n',
                    b'data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup_workspace","arguments":"{\\"workspace_id\\":\\"ws_123\\"}"}}]},"index":0}]}\n\n',
                    b'data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"tool_calls","index":0}]}\n\n',
                    b"data: [DONE]\n\n",
                ]
            )
        )
        return httpx.Response(
            status_code=200,
            headers={"content-type": "text/event-stream"},
            stream=stream,
        )

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        with client.stream(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "stream": True,
                "messages": [{"role": "user", "content": "hello"}],
            },
        ) as response:
            lines = [line for line in response.iter_lines() if line]

    assert lines[-1] == "data: [DONE]"

    interaction = stagehand.get_runtime().captured_interactions()[0]
    event_types = [event.type for event in interaction.events]
    assert interaction.operation == "chat.completions.create"
    assert interaction.streaming is True
    assert event_types == [
        "request_sent",
        "response_received",
        "stream_chunk",
        "stream_chunk",
        "tool_call_start",
        "stream_chunk",
        "tool_call_end",
        "stream_end",
    ]
    assert interaction.events[4].data is not None
    assert interaction.events[4].data["name"] == "lookup_workspace"
    assert interaction.events[7].data is not None
    assert interaction.events[7].data["chunk"] == "data: [DONE]\n\n"


def test_openai_replay_returns_exact_stream_through_same_httpx_surface() -> None:
    record_runtime = stagehand.init(session="demo", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status_code=200,
            headers={"content-type": "text/event-stream"},
            stream=httpx.ByteStream(
                b"".join(
                    [
                        b'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}\n\n',
                        b'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"index":0}]}\n\n',
                        b"data: [DONE]\n\n",
                    ]
                )
            ),
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        with client.stream(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "stream": True,
                "messages": [{"role": "user", "content": "say hello"}],
            },
        ) as response:
            recorded_lines = [line for line in response.iter_lines() if line]

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    replay_runtime = stagehand.init(session="demo-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        with client.stream(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "stream": True,
                "messages": [{"role": "user", "content": "say hello"}],
            },
        ) as response:
            replayed_lines = [line for line in response.iter_lines() if line]

    assert replayed_lines == recorded_lines
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"
    assert replayed_interaction.streaming is True


def test_openai_replay_returns_exact_non_stream_response() -> None:
    record_runtime = stagehand.init(session="demo-non-stream-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status_code=201,
            headers={"content-type": "application/json", "x-stagehand-replay": "exact"},
            json={"id": "resp_456", "ok": True},
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        recorded_response = client.post(
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "messages": [{"role": "user", "content": "say hello"}],
            },
        )

    assert recorded_response.status_code == 201
    assert recorded_response.json() == {"id": "resp_456", "ok": True}

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    replay_runtime = stagehand.init(session="demo-non-stream-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed_response = client.post(
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "messages": [{"role": "user", "content": "say hello"}],
            },
        )

    assert replayed_response.status_code == 201
    assert replayed_response.headers["x-stagehand-replay"] == "exact"
    assert replayed_response.json() == {"id": "resp_456", "ok": True}

    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"
    assert replayed_interaction.streaming is False


def test_replay_returns_exact_non_openai_response_without_network() -> None:
    record_runtime = stagehand.init(session="stripe-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status_code=200,
            headers={"content-type": "application/json"},
            json={"id": "cus_123", "object": "customer"},
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        recorded_response = client.get("https://api.stripe.com/v1/customers/cus_123")

    assert recorded_response.status_code == 200
    recorded_interactions = record_runtime.captured_interactions()
    assert recorded_interactions[0].service == "stripe"
    _reset_for_tests()

    replay_runtime = stagehand.init(session="stripe-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("non-OpenAI replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed_response = client.get("https://api.stripe.com/v1/customers/cus_123")

    assert replayed_response.status_code == 200
    assert replayed_response.json() == {"id": "cus_123", "object": "customer"}
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.service == "stripe"
    assert replayed_interaction.fallback_tier == "exact"


def test_generic_http_replay_matches_internal_json_api_exactly() -> None:
    record_runtime = stagehand.init(session="internal-api-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://crm.internal.test/v1/customers/search?b=2&a=1"
        assert request.headers["accept"] == "application/json"
        return httpx.Response(
            status_code=202,
            headers={
                "content-type": "application/json",
                "x-crm-version": "2026-05-07",
            },
            json={
                "customers": [
                    {
                        "id": "cus_internal_123",
                        "email": "jane.doe@example.com",
                    }
                ],
                "next_cursor": None,
            },
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        recorded_response = client.post(
            "https://crm.internal.test/v1/customers/search?b=2&a=1",
            headers={
                "accept": "application/json",
                "content-type": "application/json",
                "x-request-id": "record-run",
            },
            json={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 25,
            },
        )

    assert recorded_response.status_code == 202
    recorded_interactions = record_runtime.captured_interactions()
    captured = recorded_interactions[0]
    assert captured.service == "crm.internal.test"
    assert captured.operation == "POST /v1/customers/search"
    assert captured.request.body == {
        "filters": {"email": "jane.doe@example.com", "active": True},
        "limit": 25,
    }
    assert captured.events[1].data is not None
    assert captured.events[1].data["body"] == {
        "customers": [{"id": "cus_internal_123", "email": "jane.doe@example.com"}],
        "next_cursor": None,
    }
    _reset_for_tests()

    replay_runtime = stagehand.init(session="internal-api-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("generic HTTP exact replay should not call the transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed_response = client.post(
            "https://crm.internal.test/v1/customers/search?a=1&b=2",
            headers={
                "accept": "application/json",
                "content-type": "application/json",
                "x-request-id": "candidate-run",
            },
            json={
                "limit": 25,
                "filters": {"active": True, "email": "jane.doe@example.com"},
            },
        )

    assert replayed_response.status_code == 202
    assert replayed_response.headers["x-crm-version"] == "2026-05-07"
    assert replayed_response.json() == {
        "customers": [{"id": "cus_internal_123", "email": "jane.doe@example.com"}],
        "next_cursor": None,
    }
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"


def test_generic_http_replay_miss_for_changed_json_body_fails_before_network() -> None:
    record_runtime = stagehand.init(session="internal-api-body-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"ok": True})

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        client.post(
            "https://crm.internal.test/v1/customers/search",
            headers={"accept": "application/json"},
            json={"filters": {"email": "jane.doe@example.com"}, "limit": 25},
        )

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    stagehand.init(session="internal-api-body-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("generic HTTP replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError, match="crm.internal.test"):
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://crm.internal.test/v1/customers/search",
                headers={"accept": "application/json"},
                json={"filters": {"email": "jane.doe@example.com"}, "limit": 50},
            )


def test_generic_http_replay_miss_for_selected_header_change_fails_before_network() -> None:
    record_runtime = stagehand.init(session="internal-api-header-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"ok": True})

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        client.post(
            "https://crm.internal.test/v1/customers/search",
            headers={"accept": "application/json"},
            json={"filters": {"active": True}},
        )

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    stagehand.init(session="internal-api-header-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("generic HTTP replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError, match="crm.internal.test"):
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://crm.internal.test/v1/customers/search",
                headers={"accept": "application/xml"},
                json={"filters": {"active": True}},
            )


def test_configured_service_mapping_labels_generic_http_interactions(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.test
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0, 1]
  - name: billing-api
    type: api
    match:
      host: billing.internal.test
  - name: admin-api
    type: api
    match:
      host: admin.internal.test
      path_prefix: /admin
""",
        encoding="utf-8",
    )
    stagehand.init(session="mapped-service-record", mode="record", config_path=config_path)

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"ok": True})

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        client.post(
            "https://crm.internal.test/v1/customers/search",
            headers={"content-type": "application/json"},
            json={"email": "jane.doe@example.com"},
        )
        client.get("https://billing.internal.test/invoices/in_123")
        client.get("https://admin.internal.test/admin/users/u_123")
        client.get("https://admin.internal.test/public/status")

    interactions = stagehand.get_runtime().captured_interactions()
    assert [interaction.service for interaction in interactions] == [
        "internal-crm",
        "billing-api",
        "admin-api",
        "admin.internal.test",
    ]
    assert interactions[0].operation == "POST /v1/customers/search"


def test_configured_service_ignore_paths_allow_exact_replay_variation(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - headers.x-request-id
        - query.cursor
        - body.request_id
        - body.created_at
      response_paths:
        - body.generated_at
        - body.trace_id
""",
        encoding="utf-8",
    )
    stagehand.init(
        session="mapped-service-ignore-record",
        mode="record",
        config_path=config_path,
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status_code=200,
            json={"id": "inv_123", "generated_at": "record-time", "trace_id": "trace-record"},
        )

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        recorded = client.post(
            "https://billing.internal.test/api/invoices?cursor=record",
            headers={"content-type": "application/json", "x-request-id": "req-record"},
            json={"amount": 1000, "request_id": "req-record", "created_at": "record-time"},
        )

    assert recorded.status_code == 200
    recorded_interactions = stagehand.get_runtime().captured_interactions()

    _reset_for_tests()
    stagehand.init(
        session="mapped-service-ignore-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("ignored-path exact replay should not call live transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed = client.post(
            "https://billing.internal.test/api/invoices?cursor=candidate",
            headers={"content-type": "application/json", "x-request-id": "req-candidate"},
            json={
                "amount": 1000,
                "request_id": "req-candidate",
                "created_at": "candidate-time",
            },
        )

    assert replayed.status_code == 200
    assert replayed.json()["id"] == "inv_123"


def test_service_ignore_paths_do_not_leak_into_unmapped_url(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - body.request_id
""",
        encoding="utf-8",
    )
    stagehand.init(
        session="mapped-service-ignore-isolation-record",
        mode="record",
        config_path=config_path,
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"ok": True})

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        recorded = client.post(
            "https://billing.internal.test/other/path",
            headers={"content-type": "application/json"},
            json={"request_id": "req-record"},
        )
    assert recorded.status_code == 200
    recorded_interactions = stagehand.get_runtime().captured_interactions()

    _reset_for_tests()
    stagehand.init(
        session="mapped-service-ignore-isolation-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("unmapped URL replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError):
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://billing.internal.test/other/path",
                headers={"content-type": "application/json"},
                json={"request_id": "req-different"},
            )


def test_service_ignore_paths_support_nested_body_and_array_wildcards(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - body.metadata.trace_id
        - body.line_items[*].request_id
""",
        encoding="utf-8",
    )
    stagehand.init(
        session="ignore-nested-record",
        mode="record",
        config_path=config_path,
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"id": "inv_123"})

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        recorded = client.post(
            "https://billing.internal.test/api/invoices",
            headers={"content-type": "application/json"},
            json={
                "amount": 100,
                "metadata": {"trace_id": "trace-record", "region": "us-east-1"},
                "line_items": [
                    {"sku": "A", "request_id": "li-record-a"},
                    {"sku": "B", "request_id": "li-record-b"},
                ],
            },
        )
    assert recorded.status_code == 200
    recorded_interactions = stagehand.get_runtime().captured_interactions()

    _reset_for_tests()
    stagehand.init(
        session="ignore-nested-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("nested-ignore exact replay should not call live transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed = client.post(
            "https://billing.internal.test/api/invoices",
            headers={"content-type": "application/json"},
            json={
                "amount": 100,
                "metadata": {"trace_id": "trace-candidate", "region": "us-east-1"},
                "line_items": [
                    {"sku": "A", "request_id": "li-candidate-a"},
                    {"sku": "B", "request_id": "li-candidate-b"},
                ],
            },
        )

    assert replayed.status_code == 200
    assert replayed.json()["id"] == "inv_123"
    [replayed_interaction] = stagehand.get_runtime().captured_interactions()
    assert replayed_interaction.fallback_tier == "exact"


def test_service_ignore_paths_still_miss_on_non_ignored_body_change(tmp_path: Path) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text(
        """
schema_version: v1alpha1
services:
  - name: internal-billing
    type: api
    match:
      host: billing.internal.test
      path_prefix: /api
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - body.request_id
""",
        encoding="utf-8",
    )
    stagehand.init(
        session="ignore-miss-record",
        mode="record",
        config_path=config_path,
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"id": "inv_123"})

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        recorded = client.post(
            "https://billing.internal.test/api/invoices",
            headers={"content-type": "application/json"},
            json={"amount": 100, "request_id": "req-record"},
        )
    assert recorded.status_code == 200
    recorded_interactions = stagehand.get_runtime().captured_interactions()

    _reset_for_tests()
    stagehand.init(
        session="ignore-miss-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("non-ignored body change replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError):
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://billing.internal.test/api/invoices",
                headers={"content-type": "application/json"},
                json={"amount": 250, "request_id": "req-candidate"},
            )


def test_replay_miss_for_non_openai_request_fails_before_network() -> None:
    stagehand.init(session="stripe-miss", mode="replay")

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("non-OpenAI replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError, match="api.stripe.com"):
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.get("https://api.stripe.com/v1/customers/cus_missing")


def test_error_injection_record_mode_fires_before_live_dispatch(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    injection_path = tmp_path / "injection.json"
    injection_path.write_text(
        json.dumps(
            {
                "schema_version": "v1alpha1",
                "rules": [
                    {
                        "name": "stripe refund fails first",
                        "match": {
                            "service": "stripe",
                            "operation": "POST /v1/refunds",
                            "nth_call": 1,
                        },
                        "inject": {
                            "status": 402,
                            "body": {"error": {"code": "card_declined"}},
                        },
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
    runtime = stagehand.init(session="stripe-inject-record", mode="record")

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("injected record request should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        response = client.post(
            "https://api.stripe.com/v1/refunds",
            data={"payment_intent": "pi_123", "reason": "requested_by_customer"},
        )

    assert response.status_code == 402
    assert response.json() == {"error": {"code": "card_declined"}}

    [interaction] = runtime.captured_interactions()
    assert interaction.service == "stripe"
    assert interaction.operation == "POST /v1/refunds"
    assert interaction.events[1].data is not None
    assert interaction.events[1].data["status_code"] == 402
    assert interaction.events[1].data["headers"]["x-stagehand-injected"] == ["true"]

    metadata = runtime.capture_bundle_dict()["metadata"]
    assert metadata["error_injection"]["applied"] == [
        {
            "rule_index": 0,
            "service": "stripe",
            "operation": "POST /v1/refunds",
            "call_number": 1,
            "status": 402,
            "name": "stripe refund fails first",
            "nth_call": 1,
        }
    ]


def test_error_injection_replay_mode_can_target_third_call(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    injection_path = tmp_path / "injection.json"
    injection_path.write_text(
        json.dumps(
            {
                "schema_version": "v1alpha1",
                "rules": [
                    {
                        "name": "third refund fails",
                        "match": {
                            "service": "stripe",
                            "operation": "POST /v1/refunds",
                            "nth_call": 3,
                        },
                        "inject": {
                            "status": 402,
                            "body": {"error": {"code": "card_declined"}},
                        },
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
    runtime = stagehand.init(session="stripe-inject-replay", mode="replay")
    stagehand.seed_replay_interactions(
        [
            _stripe_refund_seed(sequence=1, refund_id="re_1"),
            _stripe_refund_seed(sequence=2, refund_id="re_2"),
        ]
    )

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay with injection should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        responses = [
            client.post("https://api.stripe.com/v1/refunds", data={"payment_intent": "pi_123"})
            for _ in range(3)
        ]

    assert [response.status_code for response in responses] == [200, 200, 402]
    assert [responses[0].json()["id"], responses[1].json()["id"]] == ["re_1", "re_2"]
    assert responses[2].json() == {"error": {"code": "card_declined"}}

    interactions = runtime.captured_interactions()
    assert len(interactions) == 3
    assert interactions[0].fallback_tier == "exact"
    assert interactions[1].fallback_tier == "exact"
    assert interactions[2].fallback_tier is None

    applied = runtime.capture_bundle_dict()["metadata"]["error_injection"]["applied"]
    assert applied[0]["call_number"] == 3


def test_openai_replay_strips_transport_encoding_headers() -> None:
    stagehand.init(session="demo-encoded-replay", mode="replay")
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "openai",
                    "operation": "chat.completions.create",
                    "protocol": "https",
                    "request": {
                        "url": "https://api.openai.com/v1/chat/completions",
                        "method": "POST",
                        "body": {
                            "model": "gpt-5.4",
                            "messages": [{"role": "user", "content": "say hello"}],
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
                                "headers": {
                                    "content-type": ["application/json"],
                                    "content-encoding": ["gzip"],
                                    "transfer-encoding": ["chunked"],
                                },
                                "body": {"id": "resp_456", "ok": True},
                            },
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v0-unredacted",
                        "session_salt_id": "salt_pending",
                    },
                }
            ]
        )
        == 1
    )

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed_response = client.post(
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "messages": [{"role": "user", "content": "say hello"}],
            },
        )

    assert replayed_response.status_code == 200
    assert "content-encoding" not in replayed_response.headers
    assert "transfer-encoding" not in replayed_response.headers
    assert replayed_response.json() == {"id": "resp_456", "ok": True}


def test_openai_replay_matches_scrubbed_sensitive_prompt_shape() -> None:
    stagehand.init(session="demo-sensitive-replay", mode="replay")
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "openai",
                    "operation": "chat.completions.create",
                    "protocol": "https",
                    "request": {
                        "url": "https://api.openai.com/v1/chat/completions",
                        "method": "POST",
                        "body": {
                            "model": "gpt-5.4",
                            "messages": [
                                {"role": "system", "content": "Summarize tickets."},
                                {
                                    "role": "user",
                                    "content": "Customer email: user_abcd@scrub.local",
                                },
                            ],
                            "temperature": 0,
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
                                "body": {"id": "resp_sensitive", "ok": True},
                            },
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v1",
                        "session_salt_id": "salt_test",
                        "detector_kinds": ["email"],
                    },
                }
            ]
        )
        == 1
    )

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("scrubbed replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        replayed_response = client.post(
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "messages": [
                    {"role": "system", "content": "Summarize tickets."},
                    {"role": "user", "content": "Customer email: john.doe@example.com"},
                ],
                "temperature": 0,
            },
        )

    assert replayed_response.status_code == 200
    assert replayed_response.json() == {"id": "resp_sensitive", "ok": True}


def test_openai_replay_miss_raises_concrete_error() -> None:
    stagehand.init(session="demo", mode="replay")

    def fail_if_called(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay miss should fail before transport use")

    with pytest.raises(stagehand.ReplayMissError):
        with httpx.Client(transport=httpx.MockTransport(fail_if_called)) as client:
            client.post(
                "https://api.openai.com/v1/chat/completions",
                json={
                    "model": "gpt-5.4",
                    "messages": [{"role": "user", "content": "unseeded"}],
                },
            )


def test_openai_replay_timeout_raises_typed_failure() -> None:
    record_runtime = stagehand.init(session="demo-timeout-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("upstream timed out", request=request)

    with pytest.raises(httpx.ReadTimeout):
        with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
            client.post(
                "https://api.openai.com/v1/chat/completions",
                json={
                    "model": "gpt-5.4",
                    "messages": [{"role": "user", "content": "say hello"}],
                },
            )

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    replay_runtime = stagehand.init(session="demo-timeout-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with pytest.raises(stagehand.ReplayFailureError, match="timeout ReadTimeout") as exc_info:
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://api.openai.com/v1/chat/completions",
                json={
                    "model": "gpt-5.4",
                    "messages": [{"role": "user", "content": "say hello"}],
                },
            )

    assert exc_info.value.terminal_event_type == "timeout"
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"
    assert replayed_interaction.events[1].type == "timeout"


def test_openai_replay_error_raises_typed_failure() -> None:
    record_runtime = stagehand.init(session="demo-error-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused", request=request)

    with pytest.raises(httpx.ConnectError):
        with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
            client.post(
                "https://api.openai.com/v1/chat/completions",
                json={
                    "model": "gpt-5.4",
                    "messages": [{"role": "user", "content": "say hello"}],
                },
            )

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    replay_runtime = stagehand.init(session="demo-error-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with pytest.raises(stagehand.ReplayFailureError, match="error ConnectError") as exc_info:
        with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
            client.post(
                "https://api.openai.com/v1/chat/completions",
                json={
                    "model": "gpt-5.4",
                    "messages": [{"role": "user", "content": "say hello"}],
                },
            )

    assert exc_info.value.terminal_event_type == "error"
    replayed_interaction = replay_runtime.captured_interactions()[0]
    assert replayed_interaction.fallback_tier == "exact"
    assert replayed_interaction.events[1].type == "error"


def test_runtime_can_write_capture_bundle(tmp_path: Path) -> None:
    stagehand.init(session="demo", mode="record")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=200, json={"ok": True})

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        client.post(
            "https://api.openai.com/v1/chat/completions",
            json={"model": "gpt-5.4", "messages": [{"role": "user", "content": "hi"}]},
        )

    bundle_path = tmp_path / "capture.json"
    written = stagehand.get_runtime().write_capture_bundle(bundle_path)
    payload = json.loads(Path(written).read_text(encoding="utf-8"))

    assert payload["bundle_version"] == "v1alpha1"
    assert payload["metadata"]["session"] == "demo"
    assert len(payload["interactions"]) == 1


def test_replay_bundle_env_is_loaded_during_init(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    bundle_path = tmp_path / "replay.json"
    bundle_path.write_text(
        json.dumps(
            {
                "bundle_version": "v1alpha1",
                "interactions": [
                    {
                        "run_id": "run_seed",
                        "interaction_id": "int_seed_001",
                        "sequence": 1,
                        "service": "openai",
                        "operation": "chat.completions.create",
                        "protocol": "https",
                        "streaming": True,
                        "request": {
                            "url": "https://api.openai.com/v1/chat/completions",
                            "method": "POST",
                            "body": {
                                "model": "gpt-5.4",
                                "stream": True,
                                "messages": [{"role": "user", "content": "say hello"}],
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
                                    "headers": {"content-type": ["text/event-stream"]},
                                    "body": {"capture_status": "stream"},
                                },
                            },
                            {
                                "sequence": 3,
                                "t_ms": 1,
                                "sim_t_ms": 1,
                                "type": "stream_chunk",
                                "data": {
                                    "chunk": 'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}\n\n',
                                    "payload": {
                                        "id": "chatcmpl-1",
                                        "choices": [{"delta": {"content": "Hello"}, "index": 0}],
                                    },
                                },
                            },
                            {
                                "sequence": 4,
                                "t_ms": 2,
                                "sim_t_ms": 2,
                                "type": "stream_end",
                                "data": {"chunk": "data: [DONE]\n\n"},
                            },
                        ],
                        "scrub_report": {
                            "scrub_policy_version": "v0-unredacted",
                            "session_salt_id": "salt_pending",
                        },
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("STAGEHAND_REPLAY_INPUT", str(bundle_path))

    stagehand.init(session="demo", mode="replay")

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay bundle should prevent transport use")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        with client.stream(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "stream": True,
                "messages": [{"role": "user", "content": "say hello"}],
            },
        ) as response:
            lines = [line for line in response.iter_lines() if line]

    assert lines == [
        'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}',
        "data: [DONE]",
    ]


def test_openai_replay_handles_stream_terminal_without_response_received() -> None:
    stagehand.init(session="demo-stream-terminal", mode="replay")
    assert (
        stagehand.seed_replay_interactions(
            [
                {
                    "run_id": "run_seed",
                    "interaction_id": "int_seed_001",
                    "sequence": 1,
                    "service": "openai",
                    "operation": "chat.completions.create",
                    "protocol": "https",
                    "streaming": True,
                    "request": {
                        "url": "https://api.openai.com/v1/chat/completions",
                        "method": "POST",
                        "body": {
                            "model": "gpt-5.4",
                            "stream": True,
                            "messages": [{"role": "user", "content": "say hello"}],
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
                            "type": "stream_chunk",
                            "data": {
                                "chunk": 'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}\n\n',
                            },
                        },
                        {
                            "sequence": 3,
                            "t_ms": 2,
                            "sim_t_ms": 2,
                            "type": "stream_end",
                            "data": {"chunk": "data: [DONE]\n\n"},
                        },
                    ],
                    "scrub_report": {
                        "scrub_policy_version": "v0-unredacted",
                        "session_salt_id": "salt_pending",
                    },
                }
            ]
        )
        == 1
    )

    def fail_if_network_is_used(request: httpx.Request) -> httpx.Response:
        raise AssertionError("replay should not call the underlying transport")

    with httpx.Client(transport=httpx.MockTransport(fail_if_network_is_used)) as client:
        with client.stream(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "stream": True,
                "messages": [{"role": "user", "content": "say hello"}],
            },
        ) as response:
            lines = [line for line in response.iter_lines() if line]

    assert response.status_code == 200
    assert response.headers["content-type"] == "text/event-stream"
    assert lines == [
        'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}',
        "data: [DONE]",
    ]


def test_sample_onboarding_agent_replays_offline() -> None:
    agent = _load_onboarding_agent_example()
    record_runtime = stagehand.init(session="onboarding-record", mode="record")

    def record_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status_code=200,
            headers={"content-type": "text/event-stream"},
            stream=httpx.ByteStream(
                b"".join(
                    [
                        b'data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup_workspace","arguments":"{\\"workspace_id\\":\\"ws_123\\"}"}}]},"index":0}]}\n\n',
                        b'data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"tool_calls","index":0}]}\n\n',
                        b"data: [DONE]\n\n",
                    ]
                )
            ),
        )

    with httpx.Client(transport=httpx.MockTransport(record_handler)) as client:
        recorded_result = agent.run_onboarding_agent(client)

    recorded_interactions = record_runtime.captured_interactions()
    _reset_for_tests()

    stagehand.init(session="onboarding-replay", mode="replay")
    stagehand.seed_replay_interactions(recorded_interactions)

    def fail_if_called(request: httpx.Request) -> httpx.Response:
        raise AssertionError("sample agent replay should run fully offline")

    with httpx.Client(transport=httpx.MockTransport(fail_if_called)) as client:
        replayed_result = agent.run_onboarding_agent(client)

    assert replayed_result == recorded_result


def _load_onboarding_agent_example() -> object:
    agent_path = (
        Path(__file__).resolve().parents[3]
        / "examples"
        / "onboarding-agent"
        / "openai_demo_agent.py"
    )
    spec = importlib.util.spec_from_file_location("stagehand_example_onboarding_agent", agent_path)
    assert spec is not None
    assert spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _stripe_refund_seed(*, sequence: int, refund_id: str) -> dict[str, object]:
    return {
        "run_id": "run_seed_refunds",
        "interaction_id": f"int_seed_refund_{sequence}",
        "sequence": sequence,
        "service": "stripe",
        "operation": "POST /v1/refunds",
        "protocol": "https",
        "streaming": False,
        "request": {
            "url": "https://api.stripe.com/v1/refunds",
            "method": "POST",
            "headers": {"content-type": ["application/x-www-form-urlencoded"]},
            "body": {"payment_intent": "pi_123"},
        },
        "events": [
            {"sequence": 1, "t_ms": 0, "sim_t_ms": 0, "type": "request_sent"},
            {
                "sequence": 2,
                "t_ms": 1,
                "sim_t_ms": 1,
                "type": "response_received",
                "data": {
                    "status_code": 200,
                    "headers": {"content-type": ["application/json"]},
                    "body": {"id": refund_id, "object": "refund"},
                },
            },
        ],
        "scrub_report": {
            "scrub_policy_version": "v0-unredacted",
            "session_salt_id": "salt_pending",
        },
        "latency_ms": 1,
    }
