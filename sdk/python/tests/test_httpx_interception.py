import asyncio
import importlib.util
from pathlib import Path

import httpx
import pytest

import stagehand
from stagehand._runtime import _reset_for_tests


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
