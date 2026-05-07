from __future__ import annotations

import json
import threading
from collections.abc import Iterator
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import httpx
import pytest

import stagehand
from stagehand._runtime import _reset_for_tests


def test_generic_http_exact_replay_runs_offline_after_internal_service_stops() -> None:
    with simulated_internal_crm() as crm:
        record_runtime = stagehand.init(session="generic-http-sim-record", mode="record")
        recorded_response = search_customers(
            crm.base_url,
            query="b=2&a=1",
            body={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 25,
            },
            request_id="record-run",
        )

        assert recorded_response.status_code == 202
        assert recorded_response.json() == {
            "customers": [{"id": "cus_internal_123", "email": "jane.doe@example.com"}],
            "next_cursor": None,
        }
        recorded_interactions = record_runtime.captured_interactions()
        assert crm.request_count == 1

    _reset_for_tests()

    replay_runtime = stagehand.init(session="generic-http-sim-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    replayed_response = search_customers(
        crm.base_url,
        query="a=1&b=2",
        body={
            "limit": 25,
            "filters": {"active": True, "email": "jane.doe@example.com"},
        },
        request_id="candidate-run",
    )

    assert replayed_response.status_code == 202
    assert replayed_response.headers["x-crm-version"] == "2026-05-07"
    assert replayed_response.json() == {
        "customers": [{"id": "cus_internal_123", "email": "jane.doe@example.com"}],
        "next_cursor": None,
    }
    [replayed_interaction] = replay_runtime.captured_interactions()
    assert replayed_interaction.fallback_tier == "exact"


def test_generic_http_replay_miss_blocks_live_dispatch_to_stopped_service() -> None:
    with simulated_internal_crm() as crm:
        record_runtime = stagehand.init(session="generic-http-sim-miss-record", mode="record")
        response = search_customers(
            crm.base_url,
            query="a=1&b=2",
            body={"filters": {"email": "jane.doe@example.com"}, "limit": 25},
            request_id="record-run",
        )

        assert response.status_code == 202
        recorded_interactions = record_runtime.captured_interactions()

    _reset_for_tests()

    stagehand.init(session="generic-http-sim-miss-replay", mode="replay")
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    with pytest.raises(stagehand.ReplayMissError, match="/v1/customers/search"):
        search_customers(
            crm.base_url,
            query="a=1&b=2",
            body={"filters": {"email": "jane.doe@example.com"}, "limit": 50},
            request_id="candidate-run",
        )


def search_customers(
    base_url: str,
    *,
    query: str,
    body: dict[str, Any],
    request_id: str,
) -> httpx.Response:
    with httpx.Client(timeout=1.0) as client:
        return client.post(
            f"{base_url}/v1/customers/search?{query}",
            headers={
                "accept": "application/json",
                "content-type": "application/json",
                "x-request-id": request_id,
            },
            json=body,
        )


@contextmanager
def simulated_internal_crm() -> Iterator["_CRMServer"]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _CRMHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    crm = _CRMServer(server=server)
    try:
        yield crm
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


class _CRMServer:
    def __init__(self, *, server: ThreadingHTTPServer) -> None:
        self._server = server

    @property
    def base_url(self) -> str:
        host, port = self._server.server_address
        return f"http://{host}:{port}"

    @property
    def request_count(self) -> int:
        return int(getattr(self._server, "request_count", 0))


class _CRMHandler(BaseHTTPRequestHandler):
    server_version = "StagehandCRM/1.0"

    def do_POST(self) -> None:
        if self.path != "/v1/customers/search?b=2&a=1" and self.path != (
            "/v1/customers/search?a=1&b=2"
        ):
            self.send_error(404)
            return

        setattr(self.server, "request_count", int(getattr(self.server, "request_count", 0)) + 1)
        content_length = int(self.headers.get("content-length", "0"))
        payload = json.loads(self.rfile.read(content_length).decode("utf-8"))
        if not isinstance(payload.get("filters"), dict) or "limit" not in payload:
            self.send_error(400)
            return

        body = json.dumps(
            {
                "customers": [{"id": "cus_internal_123", "email": "jane.doe@example.com"}],
                "next_cursor": None,
            }
        ).encode("utf-8")
        self.send_response(202)
        self.send_header("content-type", "application/json")
        self.send_header("x-crm-version", "2026-05-07")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: Any) -> None:
        return
