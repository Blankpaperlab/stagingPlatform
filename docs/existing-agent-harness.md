# Existing Agent Harness

Use a thin harness when your agent already lives inside an app, worker, or test suite. The harness should call the existing business entrypoint, print one JSON result object to stdout, and send logs to stderr.

## Contract

- stdout: exactly one JSON object with the business result for the scenario.
- stderr: logs, progress messages, warnings, and debug output.
- task input: read `STAGEHAND_TASK_TEXT` so the same harness can run different scenarios.
- preflight: when `STAGEHAND_PREFLIGHT=1`, return a cheap deterministic result and avoid live provider/API calls.

Validate the harness before recording:

```bash
stagehand preflight --task "Refund order 123" -- python stagehand_harness.py
stagehand record-baseline -- python stagehand_harness.py
stagehand test -- python stagehand_harness.py
```

## Python

```python
import json
import os
import sys

from my_app.agent import run_agent


def main() -> None:
    task = os.environ.get("STAGEHAND_TASK_TEXT", "Handle the default support case")

    if os.environ.get("STAGEHAND_PREFLIGHT") == "1":
        print(json.dumps({"status": "ok", "mode": "preflight"}))
        return

    print(f"running task: {task}", file=sys.stderr)
    result = run_agent(task)
    print(json.dumps({"status": "ok", "result": result}))


if __name__ == "__main__":
    main()
```

## TypeScript

```ts
import { runAgent } from './src/agent';

async function main() {
  const task = process.env.STAGEHAND_TASK_TEXT ?? 'Handle the default support case';

  if (process.env.STAGEHAND_PREFLIGHT === '1') {
    console.log(JSON.stringify({ status: 'ok', mode: 'preflight' }));
    return;
  }

  console.error(`running task: ${task}`);
  const result = await runAgent(task);
  console.log(JSON.stringify({ status: 'ok', result }));
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
```

Keep the harness small. It should adapt inputs and outputs for Stagehand, not restructure your app.
