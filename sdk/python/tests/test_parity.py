from __future__ import annotations

import json
import threading
import time
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Iterator
from urllib.parse import urlsplit

import httpx
import pytest

import stagehand
from stagehand._capture import CapturedInteraction

SHARED_PARITY_FIXTURES = (
    "http-get-success.json",
    "http-get-timeout.json",
    "openai-chat-success-default-host.json",
    "openai-chat-success-custom-host.json",
    "openai-sse-success.json",
    "http-post-scrub-fixture.json",
)


@pytest.mark.parametrize("fixture_name", SHARED_PARITY_FIXTURES)
def test_python_capture_matches_shared_fixture(
    fixture_name: str,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    fixture = load_parity_fixture(fixture_name)
    apply_fixture_env(fixture, monkeypatch)

    stagehand.init(session=f"python-parity-{fixture['name']}", mode="record")

    with parity_transport(fixture) as client:
        request = fixture["request"]
        request_args = fixture_request_args(fixture)

        if fixture_uses_streaming_response(fixture):
            with client.stream(
                request["method"],
                fixture_target_url(fixture, client.base_url),
                **request_args,
            ) as response:
                if fixture["response"].get("stream_body"):
                    assert any(response.iter_lines())
        elif "response" not in fixture:
            with pytest.raises(httpx.ReadTimeout):
                client.request(
                    request["method"],
                    fixture_target_url(fixture, client.base_url),
                    **request_args,
                )
        else:
            response = client.request(
                request["method"],
                fixture_target_url(fixture, client.base_url),
                **request_args,
            )

        if "response" in fixture and not fixture_uses_streaming_response(fixture):
            assert response.status_code == fixture["response"]["status_code"]

    interaction = stagehand.get_runtime().captured_interactions()[0]
    assert canonicalize_interaction(interaction) == fixture["expected_canonical"]


def load_parity_fixture(name: str) -> dict[str, Any]:
    path = Path(__file__).resolve().parents[3] / "testdata" / "sdk-parity" / name
    return json.loads(path.read_text(encoding="utf-8"))


def apply_fixture_env(
    fixture: dict[str, Any],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    openai_hosts = fixture.get("env", {}).get("openai_hosts", [])
    if openai_hosts:
        monkeypatch.setenv(stagehand.ENV_OPENAI_HOSTS, ",".join(str(host) for host in openai_hosts))


def fixture_target_url(fixture: dict[str, Any], base_url: str | None) -> str:
    request = fixture["request"]
    if "url" in request:
        return str(request["url"])
    assert base_url is not None
    return f"{base_url}{request['path']}"


def fixture_request_headers(fixture: dict[str, Any]) -> dict[str, str]:
    headers = fixture["request"].get("headers", {})
    return {str(name): str(value) for name, value in headers.items()}


def fixture_request_args(fixture: dict[str, Any]) -> dict[str, Any]:
    args: dict[str, Any] = {
        "headers": fixture_request_headers(fixture),
    }
    if "body" in fixture["request"]:
        args["json"] = fixture["request"]["body"]
    if "response" not in fixture:
        args["timeout"] = 0.05
    return args


def fixture_uses_streaming_response(fixture: dict[str, Any]) -> bool:
    response = fixture.get("response", {})
    return "stream_body" in response


def canonicalize_interaction(interaction: CapturedInteraction) -> dict[str, Any]:
    request_url = urlsplit(interaction.request.url)
    canonical_events: list[dict[str, Any]] = []
    for event in interaction.events:
        payload: dict[str, Any] = {
            "sequence": event.sequence,
            "type": event.type,
        }
        if event.type == "response_received" and event.data is not None:
            payload["data"] = {
                "status_code": event.data["status_code"],
                "headers": select_headers(
                    event.data.get("headers"),
                    ("content-type", "x-parity", "x-response-token"),
                ),
                "body": event.data.get("body"),
            }
        elif event.type in {"stream_chunk", "stream_end"} and event.data is not None:
            payload["data"] = {
                "chunk": event.data.get("chunk"),
            }
        elif event.type in {"tool_call_start", "tool_call_end"} and event.data is not None:
            payload["nested_interaction_id"] = event.nested_interaction_id
            payload["data"] = {
                "index": event.data.get("index"),
                "tool_call_id": event.data.get("tool_call_id"),
                "name": event.data.get("name"),
            }
        canonical_events.append(payload)

    return {
        "service": interaction.service,
        "operation": interaction.operation,
        "protocol": interaction.protocol,
        "streaming": interaction.streaming,
        "fallback_tier": interaction.fallback_tier,
        "request": {
            "method": interaction.request.method,
            "path": request_url.path + (f"?{request_url.query}" if request_url.query else ""),
            "headers": select_headers(
                interaction.request.headers,
                ("authorization", "content-type", "x-customer-email", "x-stagehand-parity"),
            ),
            "body": canonicalize_request_body(interaction.request.body),
        },
        "events": canonical_events,
        "scrub_report": {
            "scrub_policy_version": interaction.scrub_report.scrub_policy_version,
            "session_salt_id": interaction.scrub_report.session_salt_id,
        },
    }


def canonicalize_request_body(body: Any | None) -> Any | None:
    if body is None:
        return None
    return {"capture_kind": "present"}


def select_headers(
    headers: dict[str, Any] | None,
    allowed: tuple[str, ...],
) -> dict[str, list[str]]:
    if not headers:
        return {}

    selected: dict[str, list[str]] = {}
    for name in allowed:
        value = headers.get(name)
        if value is None:
            continue
        selected[name] = [str(item) for item in value]
    return selected


class FixtureRequestHandler(BaseHTTPRequestHandler):
    fixture: dict[str, Any]

    def do_GET(self) -> None:  # noqa: N802
        self._handle_request()

    def do_POST(self) -> None:  # noqa: N802
        self._handle_request()

    def _handle_request(self) -> None:
        request_fixture = self.fixture["request"]
        if self.command != request_fixture["method"]:
            self.send_error(405)
            return
        if self.path != request_fixture["path"]:
            self.send_error(404)
            return

        expected_headers = fixture_request_headers(self.fixture)
        for name, value in expected_headers.items():
            if self.headers.get(name) != value:
                self.send_error(400)
                return

        if "body" in request_fixture:
            content_length = int(self.headers.get("content-length", "0"))
            payload = self.rfile.read(content_length)
            decoded = json.loads(payload.decode("utf-8")) if payload else None
            if decoded != request_fixture["body"]:
                self.send_error(400)
                return

        if "response" in self.fixture:
            response_fixture = self.fixture["response"]
            if "stream_body" in response_fixture:
                body = str(response_fixture["stream_body"]).encode("utf-8")
                self.send_response(response_fixture["status_code"])
                for name, value in response_fixture["headers"].items():
                    self.send_header(name, value)
                self.send_header("content-length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return

            body = json.dumps(response_fixture["body"]).encode("utf-8")
            self.send_response(response_fixture["status_code"])
            for name, value in response_fixture["headers"].items():
                self.send_header(name, value)
            self.send_header("content-length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        time.sleep(float(self.fixture["server_delay_ms"]) / 1000)
        body = json.dumps({"slow": True}).encode("utf-8")
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionAbortedError):
            return

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        return


@contextmanager
def parity_transport(fixture: dict[str, Any]) -> Iterator[httpx.Client]:
    request = fixture["request"]
    if "url" in request:

        def handler(httpx_request: httpx.Request) -> httpx.Response:
            assert httpx_request.method == request["method"]
            assert str(httpx_request.url) == request["url"]
            for name, value in fixture_request_headers(fixture).items():
                assert httpx_request.headers.get(name) == value
            if "body" in request:
                assert json.loads(httpx_request.content.decode("utf-8")) == request["body"]
            return build_mock_response(fixture)

        with httpx.Client(transport=httpx.MockTransport(handler)) as client:
            yield client
        return

    server = parity_server(fixture)
    try:
        base_url = server.__enter__()
        with httpx.Client(base_url=base_url) as client:
            yield client
    finally:
        server.__exit__(None, None, None)


def build_mock_response(fixture: dict[str, Any]) -> httpx.Response:
    response_fixture = fixture["response"]
    if "stream_body" in response_fixture:
        return httpx.Response(
            status_code=response_fixture["status_code"],
            headers=response_fixture["headers"],
            stream=httpx.ByteStream(str(response_fixture["stream_body"]).encode("utf-8")),
        )
    return httpx.Response(
        status_code=response_fixture["status_code"],
        headers=response_fixture["headers"],
        json=response_fixture["body"],
    )


class parity_server:
    def __init__(self, fixture: dict[str, Any]) -> None:
        self.fixture = fixture
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), FixtureRequestHandler)
        self.server.RequestHandlerClass.fixture = fixture
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> str:
        self.thread.start()
        host, port = self.server.server_address
        return f"http://{host}:{port}"

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
