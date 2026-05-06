#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT_DIR="${STAGEHAND_E2E_OUT:-$REPO_ROOT/.stagehand/e2e}"
PYTHON_BIN="${PYTHON:-python3}"
STAGEHAND_BIN_WAS_SET=1
if [[ -z "${STAGEHAND_BIN:-}" ]]; then
  STAGEHAND_BIN="$REPO_ROOT/bin/stagehand"
  STAGEHAND_BIN_WAS_SET=0
fi
GO_BIN="${GO:-go}"
LIVE=0

if [[ "${1:-}" == "--live" ]]; then
  LIVE=1
fi

if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  PYTHON_BIN=python
fi

mkdir -p "$OUT_DIR"
rm -rf "$OUT_DIR"/*

CONFIG_PATH="$OUT_DIR/stagehand.e2e.yml"
cat >"$CONFIG_PATH" <<YAML
schema_version: v1alpha1
record:
  default_mode: record
  storage_path: $OUT_DIR/runs
  capture:
    max_body_bytes: 1048576
    include_headers:
      - content-type
      - accept
      - authorization
    redact_before_persist: true
replay:
  mode: exact
  clock_mode: wall
  allow_live_network: false
scrub:
  enabled: true
  policy_version: v1
  custom_rules: []
  detectors:
    email: true
    phone: true
    ssn: true
    credit_card: true
    jwt: true
    api_key: true
fallback:
  allowed_tiers:
    - exact
    - nearest_neighbor
    - state_synthesis
  llm_synthesis:
    enabled: false
    provider_env: OPENAI_API_KEY
    model: gpt-5-mini
auth:
  default_mode: permissive
  service_modes:
    stripe: strict-recorded
    openai: permissive
YAML

if [[ "$STAGEHAND_BIN_WAS_SET" == "0" && -x "$STAGEHAND_BIN" && -n "$(command -v "$GO_BIN" 2>/dev/null || true)" ]]; then
  echo "Building current stagehand CLI at $STAGEHAND_BIN"
  mkdir -p "$(dirname "$STAGEHAND_BIN")"
  (cd "$REPO_ROOT" && "$GO_BIN" build -o "$STAGEHAND_BIN" ./cmd/stagehand)
elif [[ ! -x "$STAGEHAND_BIN" ]]; then
  if ! command -v "$GO_BIN" >/dev/null 2>&1; then
    echo "stagehand binary is missing or not executable and Go is not available on PATH" >&2
    echo "Set STAGEHAND_BIN to a runnable stagehand binary or install Go." >&2
    exit 2
  fi
  echo "Building stagehand CLI at $STAGEHAND_BIN"
  mkdir -p "$(dirname "$STAGEHAND_BIN")"
  (cd "$REPO_ROOT" && "$GO_BIN" build -o "$STAGEHAND_BIN" ./cmd/stagehand)
fi

export PYTHONPATH="$REPO_ROOT/sdk/python${PYTHONPATH:+:$PYTHONPATH}"

json_field() {
  local field_path="$1"
  local json_path="${2:-}"
  if [[ -n "$json_path" ]]; then
    "$PYTHON_BIN" - "$field_path" "$json_path" <<'PY'
import json
import sys
from pathlib import Path

value = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
for part in sys.argv[1].split("."):
    value = value[part]
print(value)
PY
    return
  fi

  "$PYTHON_BIN" - "$field_path" <<'PY'
import json
import sys

payload = sys.stdin.read()
if payload.strip() == "":
    raise SystemExit("expected JSON on stdin, got empty input")
value = json.loads(payload)
for part in sys.argv[1].split("."):
    value = value[part]
print(value)
PY
}

assert_json_file() {
  local path="$1"
  local label="$2"
  "$PYTHON_BIN" - "$path" "$label" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
label = sys.argv[2]
payload = path.read_text(encoding="utf-8") if path.exists() else ""
if payload.strip() == "":
    raise SystemExit(f"{label} did not emit JSON: {path} is empty")
try:
    json.loads(payload)
except json.JSONDecodeError as exc:
    raise SystemExit(f"{label} did not emit valid JSON in {path}: {exc}") from exc
PY
}

assert_json() {
  "$PYTHON_BIN" - "$@" <<'PY'
import json
import sys
from pathlib import Path

kind = sys.argv[1]
payload = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))

if kind == "assertions-pass":
    failed = payload["summary"]["failed"]
    unsupported = payload["summary"]["unsupported"]
    if failed != 0 or unsupported != 0:
        raise SystemExit(f"expected assertions to pass, got failed={failed} unsupported={unsupported}")
elif kind == "assertions-fail-evidence":
    results = {item["AssertionID"]: item for item in payload["results"]}
    count = results["bad-openai-count"]["Evidence"]
    payload_field = results["bad-refund-reason"]["Evidence"]
    if count["Count"] != 1 or count["Expected"] != {"equals": 99}:
        raise SystemExit(f"count evidence was not concrete: {count}")
    if payload_field["Actual"] != "requested_by_customer":
        raise SystemExit(f"payload evidence did not show actual value: {payload_field}")
elif kind == "conformance-skipped":
    summary = payload["summary"]
    if summary["skipped"] != 1 or summary["failed"] != 0 or summary["errors"] != 0:
        raise SystemExit(f"expected one skipped conformance case, got {summary}")
elif kind == "injection-output":
    status = payload["status"]
    attempts = payload.get("attempts", 0)
    expected_attempts = int(sys.argv[3])
    if status != "refund_failed" or attempts != expected_attempts:
        raise SystemExit(f"expected refund_failed after {expected_attempts} attempt(s), got {payload}")
elif kind == "diff-openai-only":
    changes = payload["changes"]
    openai_changes = [
        change for change in changes
        if change.get("service") == "openai" and change.get("type") == "modified"
    ]
    stripe_changes = [
        change for change in changes
        if change.get("service") == "stripe"
    ]
    if not openai_changes:
        raise SystemExit(f"expected at least one OpenAI modified change, got {changes}")
    if stripe_changes:
        raise SystemExit(f"expected no untolerated Stripe changes after ignore fields, got {stripe_changes}")
else:
    raise SystemExit(f"unknown assertion kind {kind}")
PY
}

assert_sql() {
  "$PYTHON_BIN" - "$@" <<'PY'
import json
import sqlite3
import sys
from pathlib import Path

kind = sys.argv[1]
db_path = Path(sys.argv[2])

with sqlite3.connect(db_path) as db:
    db.row_factory = sqlite3.Row

    if kind == "run-schema":
        run_id = sys.argv[3]
        row = db.execute(
            "SELECT schema_version, scrub_policy_version FROM runs WHERE run_id = ?",
            (run_id,),
        ).fetchone()
        if row is None:
            raise SystemExit(f"run {run_id} not found")
        if row["schema_version"] != "v1alpha1" or row["scrub_policy_version"] != "v1":
            raise SystemExit(
                f"unexpected run schema/policy: {dict(row)}; want v1alpha1/v1"
            )
    elif kind == "injection-provenance":
        run_id = sys.argv[3]
        expected_call = int(sys.argv[4])
        row = db.execute("SELECT metadata_json FROM runs WHERE run_id = ?", (run_id,)).fetchone()
        if row is None:
            raise SystemExit(f"run {run_id} not found")
        metadata = json.loads(row["metadata_json"])
        applied = metadata.get("error_injection", {}).get("applied", [])
        if not applied:
            raise SystemExit(f"run {run_id} has no injection provenance: {metadata}")
        first = applied[0]
        expected = {
            "service": "stripe",
            "operation": "POST /v1/refunds",
            "call_number": expected_call,
            "status": 402,
        }
        for key, value in expected.items():
            if first.get(key) != value:
                raise SystemExit(f"provenance {first} missing {key}={value!r}")
    elif kind == "salt-isolation":
        first_run, second_run = sys.argv[3], sys.argv[4]
        needle = sys.argv[5]

        def values_for(run_id: str) -> set[str]:
            values: set[str] = set()
            for row in db.execute("SELECT request_json FROM interactions WHERE run_id = ?", (run_id,)):
                text = row["request_json"]
                if needle in text:
                    raise SystemExit(f"plaintext {needle!r} persisted in run {run_id}")
                payload = json.loads(text)
                body = payload.get("body")
                if isinstance(body, dict):
                    for candidate in json.dumps(body, sort_keys=True).split('"'):
                        if candidate.endswith("@scrub.local") or candidate.startswith("hash_"):
                            values.add(candidate)
                url = payload.get("url", "")
                for token in url.replace("%40", "@").split("'"):
                    if token.endswith("@scrub.local") or token.startswith("hash_"):
                        values.add(token)
            return values

        first_values = values_for(first_run)
        second_values = values_for(second_run)
        if not first_values or not second_values:
            raise SystemExit(
                f"could not find scrub replacements for salt isolation: {first_values}, {second_values}"
            )
        if first_values & second_values:
            raise SystemExit(
                f"scrub replacements should differ across sessions, overlap={first_values & second_values}"
            )
    else:
        raise SystemExit(f"unknown sql assertion kind {kind}")
PY
}

run_stagehand_json() {
  local stdout_file="$1"
  local stderr_file="$2"
  local label="${3:-stagehand command}"
  if [[ "$#" -lt 4 ]]; then
    echo "run_stagehand_json requires stdout, stderr, label, and command" >&2
    return 2
  fi
  shift 3
  if ! "$@" >"$stdout_file" 2>"$stderr_file"; then
    echo "Command failed: $*" >&2
    echo "--- stderr ---" >&2
    cat "$stderr_file" >&2 || true
    echo "--- stdout ---" >&2
    cat "$stdout_file" >&2 || true
    return 1
  fi
  if ! assert_json_file "$stdout_file" "$label"; then
    echo "Command did not produce expected JSON: $*" >&2
    echo "--- stderr ---" >&2
    cat "$stderr_file" >&2 || true
    echo "--- stdout ---" >&2
    cat "$stdout_file" >&2 || true
    return 1
  fi
}

echo "Validating example syntax and schemas"
"$PYTHON_BIN" -m py_compile "$SCRIPT_DIR/agent.py" "$SCRIPT_DIR/agent_modified.py"
if command -v "$GO_BIN" >/dev/null 2>&1; then
  (cd "$REPO_ROOT" && "$GO_BIN" test ./internal/analysis/assertions ./internal/analysis/conformance ./internal/services/stripe)
else
  echo "Go is not available on PATH; skipping package-level schema tests."
fi

SKIP_CONFORMANCE_JSON="$OUT_DIR/conformance-skip.json"
(unset STRIPE_SECRET_KEY; "$STAGEHAND_BIN" conformance run \
  --cases "$REPO_ROOT/conformance/stripe-refund-smoke.yml" \
  --config "$CONFIG_PATH" \
  --output-dir "$OUT_DIR/conformance-skip" \
  --format json >"$SKIP_CONFORMANCE_JSON")
assert_json conformance-skipped "$SKIP_CONFORMANCE_JSON"

if [[ "$LIVE" != "1" ]]; then
  cat <<EOF
Dry verification passed.

Live record/replay/diff/assertion/injection checks were skipped.
Run with --live after installing Python dependencies and setting:
  OPENAI_API_KEY
  STRIPE_SECRET_KEY
EOF
  exit 0
fi

if [[ -z "${OPENAI_API_KEY:-}" || -z "${STRIPE_SECRET_KEY:-}" ]]; then
  echo "--live requires OPENAI_API_KEY and STRIPE_SECRET_KEY" >&2
  exit 2
fi

RUN_STAMP="$(date +%Y%m%d%H%M%S)"
BASELINE_CUSTOMER_EMAIL="${STAGEHAND_CUSTOMER_EMAIL:-refund+$RUN_STAMP@example.com}"
ISOLATION_CUSTOMER_EMAIL="refund-isolation+$RUN_STAMP@example.com"
BASELINE_IDEMPOTENCY_KEY="${STAGEHAND_IDEMPOTENCY_KEY:-stagehand-e2e-$RUN_STAMP-baseline}"
ISOLATION_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY-isolation"
export STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL"
export STAGEHAND_REFUND_REASON="${STAGEHAND_REFUND_REASON:-Item arrived damaged, requesting full refund. Contact +15555550100. Card 4242 4242 4242 4242. Token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzdGFnZWhhbmQifQ.signature.}"
export STAGEHAND_SAMPLE_PHONE="${STAGEHAND_SAMPLE_PHONE:-+15555550100}"
export STAGEHAND_SAMPLE_JWT="${STAGEHAND_SAMPLE_JWT:-eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzdGFnZWhhbmQifQ.signature}"
export STAGEHAND_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY"
export STAGEHAND_SCRUB_MATRIX=1
SAMPLE_CARD="4242 4242 4242 4242"
DB_PATH="$OUT_DIR/runs/stagehand.db"

"$PYTHON_BIN" - <<'PY'
import importlib.util
import sys

missing = [name for name in ("openai", "stripe") if importlib.util.find_spec(name) is None]
if missing:
    raise SystemExit("missing Python packages for live verification: " + ", ".join(missing))
PY

echo "Recording live baseline"
BASELINE_STDOUT="$OUT_DIR/baseline-record.json"
run_stagehand_json "$BASELINE_STDOUT" "$OUT_DIR/baseline-record.log" "baseline record" \
  env STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/baseline-output.json" STAGEHAND_EXPECT_STATUS=refunded \
  "$STAGEHAND_BIN" record \
  --session refund-flow-baseline \
  --config "$CONFIG_PATH" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py"
BASELINE_RUN_ID="$(json_field run_id "$BASELINE_STDOUT")"

echo "Checking persisted SQLite data is scrubbed"
if grep -R -a -F "${STRIPE_SECRET_KEY}" "$OUT_DIR/runs" >/dev/null; then
  echo "Stripe secret key appeared in persisted run data" >&2
  exit 1
fi
if grep -R -a -F "$BASELINE_CUSTOMER_EMAIL" "$OUT_DIR/runs" >/dev/null; then
  echo "Customer email appeared in plaintext persisted run data" >&2
  exit 1
fi
if grep -R -a -F "$STAGEHAND_SAMPLE_PHONE" "$OUT_DIR/runs" >/dev/null; then
  echo "Phone number appeared in plaintext persisted run data" >&2
  exit 1
fi
if grep -R -a -F "$STAGEHAND_SAMPLE_JWT" "$OUT_DIR/runs" >/dev/null; then
  echo "JWT appeared in plaintext persisted run data" >&2
  exit 1
fi
if grep -R -a -F "$SAMPLE_CARD" "$OUT_DIR/runs" >/dev/null; then
  echo "Credit card appeared in plaintext persisted run data" >&2
  exit 1
fi
if grep -R -a -F "$STAGEHAND_IDEMPOTENCY_KEY" "$OUT_DIR/runs" >/dev/null; then
  echo "Idempotency key appeared in plaintext persisted run data" >&2
  exit 1
fi
assert_sql run-schema "$DB_PATH" "$BASELINE_RUN_ID"

"$STAGEHAND_BIN" inspect --run-id "$BASELINE_RUN_ID" --show-bodies --config "$CONFIG_PATH" >"$OUT_DIR/inspect-baseline.txt"
for expected in "openai" "stripe" "POST /v1/refunds"; do
  if ! grep -F "$expected" "$OUT_DIR/inspect-baseline.txt" >/dev/null; then
    echo "Inspect output did not contain expected label: $expected" >&2
    exit 1
  fi
done

echo "Recording isolation run to verify session-scoped scrub salts"
ISOLATION_STDOUT="$OUT_DIR/isolation-record.json"
run_stagehand_json "$ISOLATION_STDOUT" "$OUT_DIR/isolation-record.log" "isolation record" \
  env STAGEHAND_CUSTOMER_EMAIL="$ISOLATION_CUSTOMER_EMAIL" STAGEHAND_IDEMPOTENCY_KEY="$ISOLATION_IDEMPOTENCY_KEY" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/isolation-output.json" STAGEHAND_EXPECT_STATUS=refunded \
  "$STAGEHAND_BIN" record \
  --session refund-flow-isolation \
  --config "$CONFIG_PATH" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py"
ISOLATION_RUN_ID="$(json_field run_id "$ISOLATION_STDOUT")"
assert_sql salt-isolation "$DB_PATH" "$BASELINE_RUN_ID" "$ISOLATION_RUN_ID" "$BASELINE_CUSTOMER_EMAIL"

echo "Replaying baseline without live credentials"
REPLAY_STDOUT="$OUT_DIR/replay.json"
run_stagehand_json "$REPLAY_STDOUT" "$OUT_DIR/replay.log" "baseline replay" \
  env -u OPENAI_API_KEY -u STRIPE_SECRET_KEY STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/replay-output.json" STAGEHAND_EXPECT_STATUS=refunded \
  "$STAGEHAND_BIN" replay \
  --run-id "$BASELINE_RUN_ID" \
  --config "$CONFIG_PATH" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py"
cmp "$OUT_DIR/baseline-output.json" "$OUT_DIR/replay-output.json"

echo "Verifying Stripe replay misses fail closed"
if env -u OPENAI_API_KEY -u STRIPE_SECRET_KEY STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_EXTRA_STRIPE_RETRIEVE=1 STAGEHAND_EXPECT_STATUS=refunded \
  "$STAGEHAND_BIN" replay \
  --run-id "$BASELINE_RUN_ID" \
  --config "$CONFIG_PATH" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py" >"$OUT_DIR/replay-miss.json" 2>"$OUT_DIR/replay-miss.log"; then
  echo "Replay miss command unexpectedly succeeded" >&2
  exit 1
fi
if ! grep -E -i "replay.*(miss|failed|failure)" "$OUT_DIR/replay-miss.log" "$OUT_DIR/replay-miss.json" >/dev/null; then
  echo "Replay miss did not report a replay failure clearly" >&2
  cat "$OUT_DIR/replay-miss.log" >&2 || true
  cat "$OUT_DIR/replay-miss.json" >&2 || true
  exit 1
fi

echo "Promoting baseline and recording modified candidate"
BASELINE_ID="base_e2e_refund_$(date +%s)"
run_stagehand_json "$OUT_DIR/baseline-promote.json" "$OUT_DIR/baseline-promote.log" "baseline promote" \
  "$STAGEHAND_BIN" baseline promote \
  --run-id "$BASELINE_RUN_ID" \
  --baseline-id "$BASELINE_ID" \
  --git-sha e2e-local \
  --config "$CONFIG_PATH"

CANDIDATE_STDOUT="$OUT_DIR/candidate-record.json"
run_stagehand_json "$CANDIDATE_STDOUT" "$OUT_DIR/candidate-record.log" "candidate record" \
  env STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/candidate-output.json" STAGEHAND_EXPECT_STATUS=refunded \
  "$STAGEHAND_BIN" record \
  --session refund-flow-baseline \
  --config "$CONFIG_PATH" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent_modified.py"
CANDIDATE_RUN_ID="$(json_field run_id "$CANDIDATE_STDOUT")"

echo "Rendering diffs"
DIFF_IGNORE_ARGS=(
  --ignore-field response.headers
  --ignore-field response.body
  --ignore-field request.body.customer
  --ignore-field request.body.payment_intent
  --ignore-field request.body.metadata
  --ignore-field response.body.id
  --ignore-field response.body.created
  --ignore-field response.body.client_secret
  --ignore-field response.body.balance_transaction
  --ignore-field response.body.customer
  --ignore-field response.body.latest_charge
  --ignore-field response.body.payment_intent
  --ignore-field response.body.payment_method
  --ignore-field response.body.metadata
  --ignore-field response.body.data[*].id
  --ignore-field response.body.data[*].created
  --ignore-field response.body.data[*].client_secret
  --ignore-field response.body.data[*].customer
  --ignore-field response.body.data[*].latest_charge
  --ignore-field response.body.data[*].payment_intent
  --ignore-field response.body.data[*].payment_method
  --ignore-field response.body.data[*].metadata
  --ignore-field events[*].data.body.id
  --ignore-field events[*].data.body.created
  --ignore-field events[*].data.body.client_secret
  --ignore-field events[*].data.body.balance_transaction
  --ignore-field events[*].data.body.customer
  --ignore-field events[*].data.body.latest_charge
  --ignore-field events[*].data.body.payment_intent
  --ignore-field events[*].data.body.payment_method
  --ignore-field events[*].data.body.metadata
  --ignore-field events[*].data.body.data
)
"$STAGEHAND_BIN" diff --candidate-run-id "$CANDIDATE_RUN_ID" --baseline-id "$BASELINE_ID" --config "$CONFIG_PATH" "${DIFF_IGNORE_ARGS[@]}" --format terminal >"$OUT_DIR/diff.txt"
"$STAGEHAND_BIN" diff --candidate-run-id "$CANDIDATE_RUN_ID" --baseline-id "$BASELINE_ID" --config "$CONFIG_PATH" "${DIFF_IGNORE_ARGS[@]}" --format json >"$OUT_DIR/diff.json"
"$STAGEHAND_BIN" diff --candidate-run-id "$CANDIDATE_RUN_ID" --baseline-id "$BASELINE_ID" --config "$CONFIG_PATH" "${DIFF_IGNORE_ARGS[@]}" --format github-markdown >"$OUT_DIR/diff.md"
"$PYTHON_BIN" -m json.tool "$OUT_DIR/diff.json" >/dev/null
assert_json diff-openai-only "$OUT_DIR/diff.json"

echo "Running assertions and intentional assertion failures"
"$STAGEHAND_BIN" assert --run-id "$BASELINE_RUN_ID" --assertions "$SCRIPT_DIR/assertions.yml" --format json --config "$CONFIG_PATH" >"$OUT_DIR/assertions.json"
assert_json assertions-pass "$OUT_DIR/assertions.json"

BAD_ASSERTIONS="$OUT_DIR/bad-assertions.yml"
cat >"$BAD_ASSERTIONS" <<'YAML'
schema_version: v1alpha1
assertions:
  - id: bad-openai-count
    type: count
    match:
      service: openai
      operation: chat.completions.create
    expect:
      count:
        equals: 99
  - id: bad-refund-reason
    type: payload-field
    match:
      service: stripe
      operation: POST /v1/refunds
    expect:
      path: request.body.reason
      equals: duplicate
YAML
"$STAGEHAND_BIN" assert --run-id "$BASELINE_RUN_ID" --assertions "$BAD_ASSERTIONS" --format json --config "$CONFIG_PATH" >"$OUT_DIR/assertions-failing.json"
assert_json assertions-fail-evidence "$OUT_DIR/assertions-failing.json"

echo "Running deterministic error injection checks"
run_stagehand_json "$OUT_DIR/injection-nth1-record.json" "$OUT_DIR/injection-nth1-record.log" "injection nth1 record" \
  env STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/injection-nth1-output.json" STAGEHAND_EXPECT_STATUS=refund_failed \
  "$STAGEHAND_BIN" record \
  --session refund-flow-injection-nth1 \
  --config "$CONFIG_PATH" \
  --error-injection "$SCRIPT_DIR/error-injection.yml" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py"
assert_json injection-output "$OUT_DIR/injection-nth1-output.json" 1
INJECTION_NTH1_RUN_ID="$(json_field run_id "$OUT_DIR/injection-nth1-record.json")"
assert_sql injection-provenance "$DB_PATH" "$INJECTION_NTH1_RUN_ID" 1
"$STAGEHAND_BIN" inspect --run-id "$INJECTION_NTH1_RUN_ID" --show-bodies --config "$CONFIG_PATH" >"$OUT_DIR/inspect-injection-nth1.txt"
if ! grep -F "Error Injection:" "$OUT_DIR/inspect-injection-nth1.txt" >/dev/null; then
  echo "Inspect output did not show injected-failure provenance" >&2
  exit 1
fi

run_stagehand_json "$OUT_DIR/injection-nth3-record.json" "$OUT_DIR/injection-nth3-record.log" "injection nth3 record" \
  env STAGEHAND_CUSTOMER_EMAIL="$BASELINE_CUSTOMER_EMAIL" STAGEHAND_IDEMPOTENCY_KEY="$BASELINE_IDEMPOTENCY_KEY" STAGEHAND_SAMPLE_OUTPUT="$OUT_DIR/injection-nth3-output.json" STAGEHAND_EXPECT_STATUS=refund_failed STAGEHAND_REFUND_ATTEMPTS=3 STAGEHAND_FORCE_REFUND_ATTEMPTS=1 STAGEHAND_REFUND_AMOUNT=100 \
  "$STAGEHAND_BIN" record \
  --session refund-flow-injection-nth3 \
  --config "$CONFIG_PATH" \
  --error-injection "$SCRIPT_DIR/error-injection-nth3.yml" \
  -- "$PYTHON_BIN" "$SCRIPT_DIR/agent.py"
assert_json injection-output "$OUT_DIR/injection-nth3-output.json" 3
INJECTION_NTH3_RUN_ID="$(json_field run_id "$OUT_DIR/injection-nth3-record.json")"
assert_sql injection-provenance "$DB_PATH" "$INJECTION_NTH3_RUN_ID" 3

echo "Running live Stripe refund conformance smoke"
"$STAGEHAND_BIN" conformance run \
  --cases "$REPO_ROOT/conformance/stripe-refund-smoke.yml" \
  --config "$CONFIG_PATH" \
  --output-dir "$OUT_DIR/conformance-live" \
  --format github-markdown >"$OUT_DIR/conformance-live.md"

cat <<EOF
End-to-end verification passed.

Artifacts:
  $OUT_DIR
EOF
