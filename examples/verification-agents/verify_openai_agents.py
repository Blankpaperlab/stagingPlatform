from __future__ import annotations

import argparse
import importlib.metadata
import json
import os
import shutil
import sqlite3
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
EXAMPLE_DIR = Path(__file__).resolve().parent
CONFIG_PATH = EXAMPLE_DIR / "stagehand.verification.yml"
STORAGE_ROOT = EXAMPLE_DIR / ".stagehand"
DB_PATH = STORAGE_ROOT / "runs" / "stagehand.db"
OPENAI_VERSION = "2.32.0"

SENSITIVE_VALUES = [
    "john.doe@example.com",
    "+1-415-555-2671",
    "415-555-2671",
    "4532-1488-0343-6464",
    "123-45-6789",
    "sk-proj-abc123def456ghi789jkl012mno345",
    "hunter2",
]


@dataclass(frozen=True)
class AgentSpec:
    session: str
    script: str
    expected_record_interactions: int | None
    require_exact_output_replay: bool


AGENTS = [
    AgentSpec(
        session="test-1-simple",
        script="test_1_simple.py",
        expected_record_interactions=1,
        require_exact_output_replay=True,
    ),
    AgentSpec(
        session="test-2-tools",
        script="test_2_tools.py",
        expected_record_interactions=3,
        require_exact_output_replay=True,
    ),
    AgentSpec(
        session="test-3-sensitive",
        script="test_3_sensitive.py",
        expected_record_interactions=1,
        require_exact_output_replay=False,
    ),
]


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Run the Stagehand OpenAI verification agents."
    )
    parser.add_argument(
        "--keep-db",
        action="store_true",
        help="Keep examples/verification-agents/.stagehand before running.",
    )
    args = parser.parse_args()

    check_environment()
    if not args.keep_db:
        shutil.rmtree(STORAGE_ROOT, ignore_errors=True)

    for spec in AGENTS:
        print(f"\n== {spec.session} ==")
        run_agent_verification(spec)

    print("\nAll verification agents passed.")
    print(f"SQLite database: {DB_PATH}")
    return 0


def check_environment() -> None:
    if not os.environ.get("OPENAI_API_KEY"):
        raise SystemExit("OPENAI_API_KEY must be set for record-mode verification.")

    installed = importlib.metadata.version("openai")
    if installed != OPENAI_VERSION:
        raise SystemExit(
            f"OpenAI Python SDK version must be {OPENAI_VERSION}, found {installed}. "
            f"Run: python -m pip install -r {EXAMPLE_DIR / 'requirements.txt'}"
        )


def run_agent_verification(spec: AgentSpec) -> None:
    record = run_stagehand(
        [
            "record",
            "--session",
            spec.session,
            "--config",
            str(CONFIG_PATH),
            "--",
            sys.executable,
            str(EXAMPLE_DIR / spec.script),
        ],
        replay=False,
    )
    record_result = json.loads(record.stdout)
    record_output = normalize_agent_output(record.stderr)
    print(f"record run: {record_result['run_id']}")

    record_run = latest_run(spec.session, "record")
    assert_equal(
        record_result["run_id"], record_run["run_id"], "record run id mismatch"
    )
    assert_equal(record_result["status"], "complete", "record run did not complete")

    record_interactions = interactions_for_run(record_run["run_id"])
    if spec.expected_record_interactions is not None:
        assert_equal(
            len(record_interactions),
            spec.expected_record_interactions,
            f"{spec.session} interaction count mismatch",
        )

    verify_common_recording(spec, record_output, record_interactions)
    if spec.session == "test-2-tools":
        verify_tool_agent_recording(record_interactions)
    if spec.session == "test-3-sensitive":
        verify_sensitive_recording(record_interactions)

    replay = run_stagehand(
        [
            "replay",
            "--session",
            spec.session,
            "--config",
            str(CONFIG_PATH),
            "--",
            sys.executable,
            str(EXAMPLE_DIR / spec.script),
        ],
        replay=True,
    )
    replay_result = json.loads(replay.stdout)
    replay_output = normalize_agent_output(replay.stderr)
    print(f"replay run: {replay_result['replay_run_id']}")

    assert_equal(replay_result["status"], "complete", "replay run did not complete")
    assert_equal(
        replay_result["source_interaction_count"],
        len(record_interactions),
        "replay source interaction count mismatch",
    )
    if spec.require_exact_output_replay:
        assert_equal(replay_output, record_output, "replayed output changed")
    elif replay_output == "":
        raise AssertionError("replay produced no agent output")

    inspect = run_stagehand(
        [
            "inspect",
            "--session",
            spec.session,
            "--config",
            str(CONFIG_PATH),
            "--show-bodies",
        ],
        replay=False,
    )
    verify_inspect_output(spec, inspect.stdout)


def run_stagehand(args: list[str], *, replay: bool) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    if replay:
        env["OPENAI_API_KEY"] = "stagehand-replay-offline-key"

    completed = subprocess.run(
        ["go", "run", "./cmd/stagehand", *args],
        cwd=REPO_ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise AssertionError(
            "stagehand command failed\n"
            f"args: {' '.join(args)}\n"
            f"stdout:\n{completed.stdout}\n"
            f"stderr:\n{completed.stderr}"
        )
    return completed


def verify_common_recording(
    spec: AgentSpec,
    record_output: str,
    interactions: list[dict[str, Any]],
) -> None:
    if not record_output:
        raise AssertionError(f"{spec.session} produced no record-mode output")

    raw_db = DB_PATH.read_bytes()
    api_key = os.environ.get("OPENAI_API_KEY", "").encode()
    if api_key and api_key in raw_db:
        raise AssertionError(
            "OPENAI_API_KEY is present in the persisted SQLite database"
        )

    for interaction in interactions:
        scrub_report = json.loads(interaction["scrub_report_json"])
        if "request.headers.authorization" not in scrub_report.get(
            "redacted_paths", []
        ):
            raise AssertionError(
                "authorization header was not listed in scrub_report.redacted_paths"
            )

        request = json.loads(interaction["request_json"])
        if "authorization" in {key.lower() for key in request.get("headers", {})}:
            raise AssertionError("authorization header persisted in request_json")

    if spec.session == "test-1-simple":
        body = response_body_for_interaction(interactions[0]["interaction_id"])
        content = body["choices"][0]["message"]["content"]
        assert_equal(
            content,
            record_output,
            "persisted response body did not match printed output",
        )


def verify_tool_agent_recording(interactions: list[dict[str, Any]]) -> None:
    message_counts: list[int] = []
    tool_result_seen = False
    for interaction in interactions:
        request = json.loads(interaction["request_json"])
        body = request.get("body", {})
        messages = body.get("messages", [])
        message_counts.append(len(messages))
        if any(
            message.get("role") == "tool"
            for message in messages
            if isinstance(message, dict)
        ):
            tool_result_seen = True

    if message_counts != sorted(message_counts) or len(set(message_counts)) != len(
        message_counts
    ):
        raise AssertionError(
            f"tool-agent message history did not grow by interaction: {message_counts}"
        )
    if not tool_result_seen:
        raise AssertionError(
            "tool-agent request bodies do not contain accumulated tool result messages"
        )


def verify_sensitive_recording(interactions: list[dict[str, Any]]) -> None:
    raw_db_text = DB_PATH.read_text(encoding="utf-8", errors="ignore")
    for secret in SENSITIVE_VALUES:
        if secret in raw_db_text:
            raise AssertionError(
                f"sensitive value leaked into SQLite database: {secret}"
            )

    detector_kinds: set[str] = set()
    redacted_paths: set[str] = set()
    for interaction in interactions:
        scrub_report = json.loads(interaction["scrub_report_json"])
        detector_kinds.update(scrub_report.get("detector_kinds", []))
        redacted_paths.update(scrub_report.get("redacted_paths", []))

    required = {"email", "phone", "credit_card", "ssn", "api_key"}
    missing = required - detector_kinds
    if missing:
        raise AssertionError(f"scrub_report.detector_kinds missing: {sorted(missing)}")
    if not redacted_paths:
        raise AssertionError(
            "scrub_report.redacted_paths is empty for sensitive recording"
        )


def verify_inspect_output(spec: AgentSpec, output: str) -> None:
    if f"Session: {spec.session}" not in output:
        raise AssertionError("inspect output does not show the expected session")
    if "Interactions:" not in output or "openai" not in output:
        raise AssertionError(
            "inspect output does not show readable OpenAI interactions"
        )
    if spec.session == "test-3-sensitive":
        for secret in SENSITIVE_VALUES:
            if secret in output:
                raise AssertionError(f"inspect output leaked sensitive value: {secret}")


def latest_run(session: str, mode: str) -> dict[str, Any]:
    with sqlite3.connect(DB_PATH) as db:
        db.row_factory = sqlite3.Row
        row = db.execute(
            """
            SELECT run_id, session_name, mode, status, started_at
            FROM runs
            WHERE session_name = ? AND mode = ?
            ORDER BY started_at DESC, run_id DESC
            LIMIT 1
            """,
            (session, mode),
        ).fetchone()
    if row is None:
        raise AssertionError(f"missing {mode} run for session {session}")
    return dict(row)


def interactions_for_run(run_id: str) -> list[dict[str, Any]]:
    with sqlite3.connect(DB_PATH) as db:
        db.row_factory = sqlite3.Row
        rows = db.execute(
            """
            SELECT interaction_id, sequence, request_json, scrub_report_json
            FROM interactions
            WHERE run_id = ?
            ORDER BY sequence ASC
            """,
            (run_id,),
        ).fetchall()
    return [dict(row) for row in rows]


def response_body_for_interaction(interaction_id: str) -> dict[str, Any]:
    with sqlite3.connect(DB_PATH) as db:
        row = db.execute(
            """
            SELECT data_json
            FROM events
            WHERE interaction_id = ? AND type = 'response_received'
            ORDER BY sequence DESC
            LIMIT 1
            """,
            (interaction_id,),
        ).fetchone()
    if row is None or row[0] is None:
        raise AssertionError(f"missing response_received event for {interaction_id}")

    data = json.loads(row[0])
    body = data.get("body")
    if not isinstance(body, dict):
        raise AssertionError(f"response body for {interaction_id} is not an object")
    return body


def normalize_agent_output(value: str) -> str:
    return "\n".join(
        line.rstrip() for line in value.splitlines() if line.strip()
    ).strip()


def assert_equal(got: Any, want: Any, message: str) -> None:
    if got != want:
        raise AssertionError(f"{message}: got {got!r}, want {want!r}")


if __name__ == "__main__":
    raise SystemExit(main())
