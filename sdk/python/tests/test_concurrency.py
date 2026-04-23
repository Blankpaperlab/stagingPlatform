from __future__ import annotations

from threading import Barrier, Event, Lock, Thread
import time
from typing import Any

import httpx

from stagehand._capture import CaptureBuffer, CapturedInteraction
from stagehand._openai import OpenAIReplayStore, ReplayMissError


def test_capture_buffer_keeps_snapshot_order_under_parallel_writes() -> None:
    buffer = CaptureBuffer(run_id="run_parallel")
    request_count = 64
    barrier = Barrier(request_count)
    created_sequences: list[int] = []
    created_lock = Lock()
    errors: list[BaseException] = []

    def worker(index: int) -> None:
        request = httpx.Request(
            "POST",
            "https://api.openai.com/v1/chat/completions",
            json={
                "model": "gpt-5.4",
                "messages": [{"role": "user", "content": f"hello {index}"}],
            },
        )
        response = httpx.Response(
            status_code=200,
            request=request,
            json={"id": f"resp_{index}", "ok": True},
        )

        try:
            barrier.wait()
            interaction = buffer.record_httpx_response(
                request,
                response,
                elapsed_ms=index,
                streamed=False,
            )
        except BaseException as exc:  # pragma: no cover - test plumbing
            with created_lock:
                errors.append(exc)
            return

        with created_lock:
            created_sequences.append(interaction.sequence)

    threads = [Thread(target=worker, args=(index,)) for index in range(request_count)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert errors == []
    assert sorted(created_sequences) == list(range(1, request_count + 1))
    assert [interaction.sequence for interaction in buffer.snapshot()] == list(
        range(1, request_count + 1)
    )


def test_openai_replay_store_does_not_lose_or_duplicate_entries_during_concurrent_seed_and_pop() -> (
    None
):
    store = OpenAIReplayStore()
    interaction_count = 64
    seeded = [
        _openai_interaction(
            sequence=index + 1,
            request_body={"messages": [{"role": "user", "content": "same"}]},
        )
        for index in range(interaction_count)
    ]

    request = httpx.Request(
        "POST",
        "https://api.openai.com/v1/chat/completions",
        json={
            "model": "gpt-5.4",
            "messages": [{"role": "user", "content": "same"}],
        },
    )

    seeder_count = 4
    popper_count = 8
    start_barrier = Barrier(seeder_count + popper_count)
    seeding_complete = Event()
    popped_ids: list[str] = []
    pop_lock = Lock()
    errors: list[BaseException] = []
    seeded_count = 0
    completed_seeders = 0

    def seed_worker(worker_index: int) -> None:
        nonlocal completed_seeders, seeded_count
        try:
            start_barrier.wait()
            local_seeded = seeded[worker_index::seeder_count]
            for interaction in local_seeded:
                assert store.seed([interaction]) == 1
                with pop_lock:
                    seeded_count += 1
                time.sleep(0.0005)
        except BaseException as exc:  # pragma: no cover - test plumbing
            with pop_lock:
                errors.append(exc)
        finally:
            with pop_lock:
                completed_seeders += 1
                if completed_seeders == seeder_count:
                    seeding_complete.set()

    def pop_worker() -> None:
        try:
            start_barrier.wait()
            while True:
                with pop_lock:
                    if len(popped_ids) == interaction_count:
                        return

                try:
                    match = store.pop_match(request)
                except ReplayMissError:
                    if seeding_complete.is_set():
                        with pop_lock:
                            if len(popped_ids) == interaction_count:
                                return
                    time.sleep(0.0005)
                    continue

                with pop_lock:
                    popped_ids.append(match.interaction.interaction_id)
                    if len(popped_ids) == interaction_count:
                        return
        except BaseException as exc:  # pragma: no cover - test plumbing
            with pop_lock:
                errors.append(exc)

    threads = [
        Thread(target=seed_worker, args=(worker_index,)) for worker_index in range(seeder_count)
    ] + [Thread(target=pop_worker) for _ in range(popper_count)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert errors == []
    assert seeded_count == interaction_count
    assert len(popped_ids) == interaction_count
    assert len(set(popped_ids)) == interaction_count


def _openai_interaction(*, sequence: int, request_body: dict[str, Any]) -> CapturedInteraction:
    return CapturedInteraction.from_dict(
        {
            "run_id": "run_seed",
            "interaction_id": f"int_{sequence:04d}",
            "sequence": sequence,
            "service": "openai",
            "operation": "chat.completions.create",
            "protocol": "https",
            "streaming": False,
            "request": {
                "url": "https://api.openai.com/v1/chat/completions",
                "method": "POST",
                "headers": {"content-type": ["application/json"]},
                "body": {
                    "model": "gpt-5.4",
                    **request_body,
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
                        "body": {"id": f"resp_{sequence}"},
                    },
                },
            ],
            "scrub_report": {
                "scrub_policy_version": "v0-unredacted",
                "session_salt_id": "salt_pending",
            },
        }
    )
