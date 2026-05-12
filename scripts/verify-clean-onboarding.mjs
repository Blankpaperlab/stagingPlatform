import { mkdtempSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const platform = process.platform;
const exe = platform === 'win32' ? '.exe' : '';
const stagehandBin = join(repoRoot, 'bin', `stagehand${exe}`);
const pythonPath = join(repoRoot, 'sdk', 'python');

function run(command, args, options = {}) {
  const completed = spawnSync(command, args, {
    cwd: repoRoot,
    encoding: 'utf8',
    shell: platform === 'win32',
    env: process.env,
    ...options,
  });
  if (completed.status !== 0) {
    throw new Error(
      `${command} ${args.join(' ')} failed with status ${completed.status}\n` +
        `stdout:\n${completed.stdout || ''}\n` +
        `stderr:\n${completed.stderr || ''}`
    );
  }
  return completed;
}

function runStagehand(workdir, args) {
  const env = {
    ...process.env,
    PYTHONPATH: process.env.PYTHONPATH
      ? `${pythonPath}${delimiter}${process.env.PYTHONPATH}`
      : pythonPath,
  };
  return run(stagehandBin, args, { cwd: workdir, env });
}

function main() {
  mkdirSync(join(repoRoot, 'bin'), { recursive: true });
  run('go', ['build', '-o', stagehandBin, './cmd/stagehand']);

  const workdir = mkdtempSync(join(tmpdir(), 'stagehand-clean-onboarding-'));
  const agentPath = join(workdir, 'agent.py');
  writeFileSync(
    agentPath,
    `import json
import os

import stagehand

if os.environ.get("STAGEHAND_PREFLIGHT") == "1":
    print(json.dumps({"status": "preflight_ok"}))
    raise SystemExit(0)

stagehand.init_from_env()


@stagehand.tool(name="lookup_customer", side_effect="read", replay="recorded")
def lookup_customer(customer_id: str) -> dict:
    return {"customer_id": customer_id, "status": "ready"}


result = lookup_customer(os.environ.get("STAGEHAND_TASK_TEXT", "cus_clean_001"))
print(json.dumps(result))
`
  );

  runStagehand(workdir, ['init', '--force']);
  runStagehand(workdir, ['doctor']);
  runStagehand(workdir, ['preflight', '--task', 'cus_clean_001', '--', 'python', agentPath]);
  const record = runStagehand(workdir, ['record-baseline', '--', 'python', agentPath]);
  if (!record.stdout.includes('Stagehand baseline recorded')) {
    throw new Error(`record-baseline did not print success output:\n${record.stdout}`);
  }
  const test = runStagehand(workdir, ['test', '--', 'python', agentPath]);
  if (!test.stdout.includes('Stagehand test: passed')) {
    throw new Error(`stagehand test did not pass:\n${test.stdout}`);
  }

  console.log(`Clean onboarding verification passed in ${workdir}`);
}

main();
