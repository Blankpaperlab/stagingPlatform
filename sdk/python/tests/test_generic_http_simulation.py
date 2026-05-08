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


def test_generic_http_ignore_paths_replay_dynamic_internal_request_offline(tmp_path: Any) -> None:
    with simulated_internal_crm() as crm:
        config_path = tmp_path / "stagehand.yml"
        config_path.write_text(
            f"""
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: {crm.host}
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - query.cursor
        - body.request_id
        - body.created_at
      response_paths:
        - body.generated_at
""",
            encoding="utf-8",
        )
        record_runtime = stagehand.init(
            session="generic-http-ignore-sim-record",
            mode="record",
            config_path=config_path,
        )
        recorded_response = search_customers(
            crm.base_url,
            query="cursor=record&a=1",
            body={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 25,
                "request_id": "record-run",
                "created_at": "record-time",
            },
            request_id="record-run",
        )

        assert recorded_response.status_code == 202
        recorded_interactions = record_runtime.captured_interactions()
        assert crm.request_count == 1

    _reset_for_tests()

    replay_runtime = stagehand.init(
        session="generic-http-ignore-sim-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    replayed_response = search_customers(
        crm.base_url,
        query="a=1&cursor=candidate",
        body={
            "limit": 25,
            "filters": {"active": True, "email": "jane.doe@example.com"},
            "request_id": "candidate-run",
            "created_at": "candidate-time",
        },
        request_id="candidate-run",
    )

    assert replayed_response.status_code == 202
    [replayed_interaction] = replay_runtime.captured_interactions()
    assert replayed_interaction.fallback_tier == "exact"
    assert replayed_interaction.service == "internal-crm"


def test_generic_http_nearest_neighbor_replays_mutable_request_variation_when_allowed(
    tmp_path: Any,
) -> None:
    with simulated_internal_crm() as crm:
        config_path = tmp_path / "stagehand.yml"
        config_path.write_text(
            f"""
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: {crm.host}
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0, 1]
""",
            encoding="utf-8",
        )
        record_runtime = stagehand.init(
            session="generic-http-nearest-record",
            mode="record",
            config_path=config_path,
        )
        recorded_response = search_customers(
            crm.base_url,
            query="cursor=record&page_token=page_1",
            body={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 25,
                "request_id": "record-run",
                "created_at": "2026-05-07T12:00:00Z",
            },
            request_id="req-record",
        )

        assert recorded_response.status_code == 202
        recorded_interactions = record_runtime.captured_interactions()
        assert crm.request_count == 1

    _reset_for_tests()

    replay_runtime = stagehand.init(
        session="generic-http-nearest-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    replayed_response = search_customers(
        crm.base_url,
        query="cursor=candidate&page_token=page_2",
        body={
            "limit": 25,
            "filters": {"active": True, "email": "jane.doe@example.com"},
            "request_id": "candidate-run",
            "created_at": "2026-05-07T12:01:00Z",
        },
        request_id="req-candidate",
    )

    assert replayed_response.status_code == 202
    assert crm.request_count == 1
    [replayed_interaction] = replay_runtime.captured_interactions()
    assert replayed_interaction.fallback_tier == "nearest_neighbor"
    assert replayed_interaction.fallback_reason is not None
    assert "nearest request match score=" in replayed_interaction.fallback_reason
    assert replayed_interaction.service == "internal-crm"


def test_generic_http_nearest_neighbor_does_not_reuse_exactly_consumed_interaction(
    tmp_path: Any,
) -> None:
    with simulated_internal_crm() as crm:
        config_path = tmp_path / "stagehand.yml"
        config_path.write_text(
            f"""
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: {crm.host}
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0, 1]
""",
            encoding="utf-8",
        )
        record_runtime = stagehand.init(
            session="generic-http-consume-record",
            mode="record",
            config_path=config_path,
        )
        recorded_response = search_customers(
            crm.base_url,
            query="cursor=record",
            body={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 25,
                "request_id": "record-run",
            },
            request_id="req-record",
        )

        assert recorded_response.status_code == 202
        recorded_interactions = record_runtime.captured_interactions()
        assert crm.request_count == 1

    _reset_for_tests()

    replay_runtime = stagehand.init(
        session="generic-http-consume-replay",
        mode="replay",
        config_path=config_path,
    )
    assert stagehand.seed_replay_interactions(recorded_interactions) == 1

    exact_response = search_customers(
        crm.base_url,
        query="cursor=record",
        body={
            "filters": {"email": "jane.doe@example.com", "active": True},
            "limit": 25,
            "request_id": "record-run",
        },
        request_id="req-record",
    )

    assert exact_response.status_code == 202
    with pytest.raises(stagehand.ReplayMissError, match="/v1/customers/search"):
        search_customers(
            crm.base_url,
            query="cursor=variant",
            body={
                "filters": {"email": "jane.doe@example.com", "active": True},
                "limit": 50,
                "request_id": "variant-replay",
            },
            request_id="req-variant",
        )

    [replayed_interaction] = replay_runtime.captured_interactions()
    assert replayed_interaction.fallback_tier == "exact"


def test_generic_http_error_injection_timeout_status_latency_and_provenance(
    tmp_path: Any,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    with simulated_internal_crm() as crm:
        config_path = tmp_path / "stagehand.yml"
        config_path.write_text(
            f"""
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: {crm.host}
      path_prefix: /v1
""",
            encoding="utf-8",
        )
        injection_path = tmp_path / "injection.json"
        injection_path.write_text(
            json.dumps(
                {
                    "schema_version": "v1alpha1",
                    "rules": [
                        {
                            "name": "CRM timeout on third lookup",
                            "match": {
                                "service": "internal-crm",
                                "operation": "POST /v1/customers/search",
                                "nth_call": 3,
                            },
                            "inject": {"error": "timeout", "latency_ms": 5},
                        },
                        {
                            "name": "Billing API malformed 500",
                            "match": {
                                "service": "internal-crm",
                                "operation": "POST /v1/refunds",
                                "probability": 1.0,
                            },
                            "inject": {
                                "status": 500,
                                "latency_ms": 5,
                                "body": "{malformed-json",
                            },
                        },
                    ],
                }
            ),
            encoding="utf-8",
        )
        monkeypatch.setenv("STAGEHAND_ERROR_INJECTION_INPUT", str(injection_path))
        runtime = stagehand.init(
            session="generic-http-injection-record",
            mode="record",
            config_path=config_path,
        )

        assert (
            search_customers(
                crm.base_url,
                query="a=1",
                body={"filters": {"email": "jane.doe@example.com"}, "limit": 25},
                request_id="first",
            ).status_code
            == 202
        )
        assert (
            search_customers(
                crm.base_url,
                query="a=1",
                body={"filters": {"email": "jane.doe@example.com"}, "limit": 25},
                request_id="second",
            ).status_code
            == 202
        )
        with pytest.raises(httpx.TimeoutException, match="Injected Stagehand timeout"):
            search_customers(
                crm.base_url,
                query="a=1",
                body={"filters": {"email": "jane.doe@example.com"}, "limit": 25},
                request_id="third",
            )

        refund_response = httpx.post(
            f"{crm.base_url}/v1/refunds",
            headers={"content-type": "application/json"},
            json={"payment_id": "pay_123"},
            timeout=1.0,
        )

        assert refund_response.status_code == 500
        assert refund_response.text == "{malformed-json"
        assert crm.request_count == 2
        interactions = runtime.captured_interactions()
        assert interactions[2].events[-1].type == "timeout"
        timeout_provenance = interactions[2].events[-1].data["stagehand_injection"]
        assert timeout_provenance["name"] == "CRM timeout on third lookup"
        assert timeout_provenance["latency_ms"] == 5
        response_event = interactions[3].events[-1]
        assert response_event.data["stagehand_injection"]["name"] == "Billing API malformed 500"
        assert response_event.data["body"] == "{malformed-json"
        applied = runtime.capture_bundle_dict()["metadata"]["error_injection"]["applied"]
        assert [item["name"] for item in applied] == [
            "CRM timeout on third lookup",
            "Billing API malformed 500",
        ]


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
    def host(self) -> str:
        host, _ = self._server.server_address
        return str(host)

    @property
    def request_count(self) -> int:
        return int(getattr(self._server, "request_count", 0))


class _CRMHandler(BaseHTTPRequestHandler):
    server_version = "StagehandCRM/1.0"

    def do_POST(self) -> None:
        if not self.path.startswith("/v1/customers/search?"):
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
