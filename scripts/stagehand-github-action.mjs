import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const defaultFailOn = ['behavior_diff', 'assertion_failure'];
const commandTimeoutMS = 10 * 60 * 1000;

function input(name, fallback = '') {
  const key = `INPUT_${name.replace(/ /g, '_').replace(/-/g, '_').toUpperCase()}`;
  const value = process.env[key];
  return value == null || value.trim() === '' ? fallback : value.trim();
}

function splitList(value) {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function isTruthy(value) {
  return ['1', 'true', 'yes', 'on'].includes(String(value).trim().toLowerCase());
}

function commentMarker(commentID) {
  const stableID = String(commentID || 'stagehand-run').replace(/[^a-zA-Z0-9_.-]/g, '-');
  return `<!-- stagehand-pr-report:${stableID} -->`;
}

function shellSplit(command) {
  const args = [];
  let current = '';
  let quote = '';
  let escaped = false;

  for (const char of command) {
    if (escaped) {
      current += char;
      escaped = false;
      continue;
    }
    if (char === '\\') {
      escaped = true;
      continue;
    }
    if (quote) {
      if (char === quote) {
        quote = '';
      } else {
        current += char;
      }
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      continue;
    }
    if (/\s/.test(char)) {
      if (current !== '') {
        args.push(current);
        current = '';
      }
      continue;
    }
    current += char;
  }

  if (escaped) {
    current += '\\';
  }
  if (quote) {
    throw new Error('command input has an unterminated quote');
  }
  if (current !== '') {
    args.push(current);
  }
  return args;
}

function parseTestConfig(path) {
  const defaults = {
    session: '',
    failOn: defaultFailOn,
    reportFormat: 'github-markdown',
  };
  if (!existsSync(path)) {
    return defaults;
  }

  const lines = readFileSync(path, 'utf8').split(/\r?\n/);
  const cfg = { ...defaults, failOn: [...defaults.failOn] };
  let section = '';
  let collectingFailOn = false;
  let collectedFailOn = [];

  function finishFailOn() {
    if (!collectingFailOn) {
      return;
    }
    if (collectedFailOn.length > 0) {
      cfg.failOn = collectedFailOn;
    }
    collectingFailOn = false;
    collectedFailOn = [];
  }

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) {
      continue;
    }
    const topLevel = !line.startsWith(' ') && !line.startsWith('\t');
    if (topLevel && trimmed.endsWith(':')) {
      finishFailOn();
      section = trimmed.slice(0, -1);
      continue;
    }
    if (topLevel && trimmed.startsWith('session:')) {
      finishFailOn();
      cfg.session = unquote(trimmed.slice('session:'.length).trim());
      continue;
    }
    if (topLevel) {
      finishFailOn();
      continue;
    }
    if (section !== 'ci') {
      continue;
    }
    if (trimmed.startsWith('- ')) {
      if (collectingFailOn) {
        collectedFailOn.push(unquote(trimmed.slice(2).trim()));
      }
      continue;
    }
    if (section === 'ci' && trimmed.startsWith('fail_on:')) {
      finishFailOn();
      const value = trimmed.slice('fail_on:'.length).trim();
      if (value === '') {
        collectingFailOn = true;
        collectedFailOn = [];
      } else if (value.startsWith('[') && value.endsWith(']')) {
        const parsed = parseFlowList(value);
        if (parsed.length > 0) {
          cfg.failOn = parsed;
        }
      } else {
        cfg.failOn = splitList(value);
      }
      continue;
    }
    finishFailOn();
    if (trimmed.startsWith('report_format:')) {
      cfg.reportFormat = unquote(trimmed.slice('report_format:'.length).trim());
    }
  }

  finishFailOn();
  return cfg;
}

function parseFlowList(value) {
  return value
    .slice(1, -1)
    .split(',')
    .map((item) => unquote(item.trim()))
    .filter(Boolean);
}

function unquote(value) {
  return value.replace(/^["']|["']$/g, '');
}

function runCommand(binary, args, label) {
  console.log(`$ ${[binary, ...args].join(' ')}`);
  const result = spawnSync(binary, args, {
    encoding: 'utf8',
    env: process.env,
    stdio: ['ignore', 'pipe', 'pipe'],
    timeout: commandTimeoutMS,
  });
  if (result.stderr) {
    process.stderr.write(result.stderr);
  }
  if (result.error) {
    throw new Error(`${label} failed: ${result.error.message}`);
  }
  if (result.status !== 0) {
    const detail = result.stdout ? `\nstdout:\n${result.stdout}` : '';
    throw new Error(`${label} failed with exit code ${result.status}${detail}`);
  }
  return result.stdout;
}

function parseJSONOutput(output, label) {
  try {
    return JSON.parse(output);
  } catch (error) {
    throw new Error(`${label} did not emit valid JSON: ${error.message}\n${output}`);
  }
}

function resolveBaseline({
  binary,
  configPath,
  session,
  baselineSource,
  baselineID,
  baselineRunID,
}) {
  if (baselineSource === 'run-id') {
    if (!baselineRunID) {
      throw new Error('baseline-source=run-id requires baseline-run-id');
    }
    return {
      sourceRunID: baselineRunID,
      diffSelector: ['--base-run-id', baselineRunID],
    };
  }

  if (baselineSource === 'baseline-id') {
    if (!baselineID) {
      throw new Error('baseline-source=baseline-id requires baseline-id');
    }
    const output = runCommand(
      binary,
      ['baseline', 'show', '--baseline-id', baselineID, '--config', configPath],
      'stagehand baseline show'
    );
    const baseline = parseJSONOutput(output, 'stagehand baseline show');
    return {
      sourceRunID: baseline.source_run_id,
      diffSelector: ['--baseline-id', baselineID],
    };
  }

  if (baselineSource !== 'latest') {
    throw new Error(
      `baseline-source must be latest, baseline-id, or run-id; got ${baselineSource}`
    );
  }
  const output = runCommand(
    binary,
    ['baseline', 'show', '--session', session, '--config', configPath],
    'stagehand baseline show'
  );
  const baseline = parseJSONOutput(output, 'stagehand baseline show');
  return {
    sourceRunID: baseline.source_run_id,
    diffSelector: ['--session', session],
  };
}

function reportExtension(format) {
  switch (format) {
    case 'json':
      return 'json';
    case 'terminal':
      return 'txt';
    default:
      return 'md';
  }
}

function failureReasons(report, failOn) {
  const reasons = [];
  const conditions = new Set(failOn);
  for (const change of report.changes ?? []) {
    if (change.type === 'fallback_regression' && conditions.has('fallback_regression')) {
      reasons.push('fallback_regression');
      continue;
    }
    if (
      change.failing &&
      change.type !== 'fallback_regression' &&
      conditions.has('behavior_diff')
    ) {
      reasons.push('behavior_diff');
    }
  }
  return [...new Set(reasons)];
}

function writeOutput(name, value) {
  if (!process.env.GITHUB_OUTPUT) {
    return;
  }
  if (String(value).includes('\n')) {
    const delimiter = `stagehand_${name}_${Date.now()}`;
    writeFileSync(process.env.GITHUB_OUTPUT, `${name}<<${delimiter}\n${value}\n${delimiter}\n`, {
      flag: 'a',
    });
    return;
  }
  writeFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}\n`, { flag: 'a' });
}

function appendStepSummary(markdown) {
  if (!process.env.GITHUB_STEP_SUMMARY) {
    return;
  }
  writeFileSync(process.env.GITHUB_STEP_SUMMARY, markdown, { flag: 'a' });
}

function markdownEscape(value) {
  return String(value).replaceAll('|', '\\|').replaceAll('\n', ' ');
}

function markdownInline(value) {
  return String(value).replaceAll('`', "'");
}

function localInspectionGuidance(summary) {
  const lines = ['### Local inspection'];
  for (const session of summary.sessions ?? []) {
    lines.push('', `For \`${markdownInline(session.session)}\`:`);
    lines.push(
      `- Inspect replay: \`stagehand inspect --run-id ${session.replay_run_id} --show-bodies\``
    );
    lines.push(
      `- Re-run diff: \`stagehand diff --base-run-id ${session.source_run_id} --candidate-run-id ${session.replay_run_id} --format terminal\``
    );
    if (session.assertions_report_path) {
      lines.push(`- Assertion report: \`${session.assertions_report_path}\``);
    }
  }
  return `${lines.join('\n')}\n`;
}

function buildPRComment(reportMarkdown, summary, artifactURL, marker) {
  const status = summary.failed ? 'failed' : 'passed';
  const sessionRows = (summary.sessions ?? [])
    .map((session) =>
      [
        markdownEscape(session.session),
        markdownEscape(session.source_run_id),
        markdownEscape(session.replay_run_id),
        markdownEscape(session.summary?.failing_changes ?? 0),
        markdownEscape(session.assertions_summary?.failed ?? '-'),
        markdownEscape(session.fail_reasons?.join(', ') || '-'),
      ].join(' | ')
    )
    .map((row) => `| ${row} |`)
    .join('\n');
  const table =
    sessionRows === ''
      ? ''
      : [
          '| Session | Base run | Replay run | Failing diffs | Failed assertions | Fail reasons |',
          '| --- | --- | --- | ---: | ---: | --- |',
          sessionRows,
        ].join('\n');
  const artifactLine = artifactURL
    ? `\nArtifact: [Stagehand reports and run data](${artifactURL})\n`
    : '\nArtifact: not uploaded by this action run.\n';
  const body = [
    marker,
    `## Stagehand CI ${status}`,
    artifactLine,
    table,
    localInspectionGuidance(summary),
    reportMarkdown.trim(),
  ]
    .filter((part) => part !== '')
    .join('\n\n');
  if (body.length <= 60000) {
    return body;
  }
  return `${body.slice(0, 59500)}\n\n_Report truncated. See the uploaded artifact or local report paths for full output._\n`;
}

async function githubRequest(method, path, token, body) {
  const apiURL = process.env.GITHUB_API_URL || 'https://api.github.com';
  const response = await fetch(`${apiURL}${path}`, {
    method,
    headers: {
      Accept: 'application/vnd.github+json',
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      'X-GitHub-Api-Version': '2022-11-28',
    },
    body: body == null ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(`GitHub API ${method} ${path} failed: ${response.status} ${text}`);
  }
  if (response.status === 204) {
    return null;
  }
  return response.json();
}

async function postOrUpdatePRComment({ token, reportPath, summaryPath, artifactURL, marker }) {
  if (!token) {
    console.log('Skipping PR comment: github-token input is empty.');
    return;
  }
  if (!process.env.GITHUB_EVENT_PATH || !existsSync(process.env.GITHUB_EVENT_PATH)) {
    console.log('Skipping PR comment: GITHUB_EVENT_PATH is unavailable.');
    return;
  }
  const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
  const number = event.pull_request?.number;
  if (!number) {
    console.log('Skipping PR comment: event is not a pull request.');
    return;
  }
  const repository = process.env.GITHUB_REPOSITORY;
  if (!repository) {
    throw new Error('GITHUB_REPOSITORY is required to post a PR comment');
  }
  const [owner, repo] = repository.split('/');
  const summary = JSON.parse(readFileSync(summaryPath, 'utf8'));
  const reportMarkdown = readFileSync(reportPath, 'utf8');
  const body = buildPRComment(reportMarkdown, summary, artifactURL, marker);
  const comments = await githubRequest(
    'GET',
    `/repos/${owner}/${repo}/issues/${number}/comments?per_page=100`,
    token
  );
  const existing = comments.find((comment) => comment.body?.includes(marker));
  if (existing) {
    await githubRequest('PATCH', `/repos/${owner}/${repo}/issues/comments/${existing.id}`, token, {
      body,
    });
    console.log(`Updated Stagehand PR comment ${existing.id}.`);
    return;
  }
  const created = await githubRequest(
    'POST',
    `/repos/${owner}/${repo}/issues/${number}/comments`,
    token,
    {
      body,
    }
  );
  console.log(`Created Stagehand PR comment ${created.id}.`);
}

function main() {
  const command = input('command');
  if (!command) {
    throw new Error('command input is required');
  }

  const testConfigPath = input('test-config', 'stagehand.test.yml');
  const testConfig = parseTestConfig(testConfigPath);
  const sessions =
    splitList(input('sessions')).length > 0
      ? splitList(input('sessions'))
      : [testConfig.session].filter(Boolean);
  if (sessions.length === 0) {
    throw new Error('sessions input is required when stagehand.test.yml does not define session');
  }

  const binary = input('stagehand-binary', 'bin/stagehand');
  const configPath = input('config', 'stagehand.yml');
  const outputDir = input('output-dir', '.stagehand/action');
  const baselineSource = input('baseline-source', 'latest');
  const baselineID = input('baseline-id');
  const baselineRunID = input('baseline-run-id');
  const reportFormat = input('report-format', testConfig.reportFormat);
  const failOn =
    splitList(input('fail-on')).length > 0 ? splitList(input('fail-on')) : testConfig.failOn;
  const ignoreFields = splitList(input('ignore-fields'));
  const assertionsPath = input('assertions');
  const artifactPaths = [outputDir, ...splitList(input('artifact-paths', '.stagehand/runs'))].join(
    '\n'
  );
  const marker = commentMarker(input('comment-id', input('artifact-name', 'stagehand-run')));
  const commandArgs = shellSplit(command);
  if (commandArgs.length === 0) {
    throw new Error('command input did not contain an executable');
  }

  mkdirSync(outputDir, { recursive: true });

  const combinedReports = [];
  const summaries = [];
  let failed = false;

  for (const session of sessions) {
    const baseline = resolveBaseline({
      binary,
      configPath,
      session,
      baselineSource,
      baselineID,
      baselineRunID,
    });
    const replayOutput = runCommand(
      binary,
      ['replay', '--run-id', baseline.sourceRunID, '--config', configPath, '--', ...commandArgs],
      'stagehand replay'
    );
    const replay = parseJSONOutput(replayOutput, 'stagehand replay');

    const ignoreArgs = ignoreFields.flatMap((field) => ['--ignore-field', field]);
    const diffBaseArgs = [
      ...baseline.diffSelector,
      '--candidate-run-id',
      replay.replay_run_id,
      '--config',
      configPath,
      ...ignoreArgs,
    ];
    const reportOutput = runCommand(
      binary,
      ['diff', ...diffBaseArgs, '--format', reportFormat],
      'stagehand diff'
    );
    const reportPath = join(outputDir, `${session}.${reportExtension(reportFormat)}`);
    writeFileSync(reportPath, reportOutput);

    const jsonOutput =
      reportFormat === 'json'
        ? reportOutput
        : runCommand(binary, ['diff', ...diffBaseArgs, '--format', 'json'], 'stagehand diff json');
    const jsonReport = parseJSONOutput(jsonOutput, 'stagehand diff json');
    const reasons = failureReasons(jsonReport, failOn);

    let assertionsReport = null;
    let assertionsReportPath = '';
    if (assertionsPath) {
      const assertionOutput = runCommand(
        binary,
        [
          'assert',
          '--run-id',
          replay.replay_run_id,
          '--assertions',
          assertionsPath,
          '--format',
          'json',
          '--config',
          configPath,
        ],
        'stagehand assert'
      );
      assertionsReport = parseJSONOutput(assertionOutput, 'stagehand assert');
      assertionsReportPath = join(outputDir, `${session}.assertions.json`);
      writeFileSync(assertionsReportPath, assertionOutput);
      if ((assertionsReport.summary?.failed ?? 0) > 0 && failOn.includes('assertion_failure')) {
        reasons.push('assertion_failure');
      }
    }

    if (reasons.length > 0) {
      failed = true;
    }

    combinedReports.push(`## ${session}\n\n${reportOutput.trim()}\n`);
    summaries.push({
      session,
      source_run_id: baseline.sourceRunID,
      replay_run_id: replay.replay_run_id,
      report_path: reportPath,
      assertions_report_path: assertionsReportPath,
      fail_reasons: [...new Set(reasons)],
      summary: jsonReport.summary,
      assertions_summary: assertionsReport?.summary ?? null,
    });
  }

  const combinedReportPath = join(outputDir, `stagehand-report.${reportExtension(reportFormat)}`);
  const summaryPath = join(outputDir, 'stagehand-summary.json');
  const commentPreviewPath = join(outputDir, 'stagehand-pr-comment.md');
  mkdirSync(dirname(combinedReportPath), { recursive: true });
  const summary = { failed, sessions: summaries };
  writeFileSync(combinedReportPath, `${combinedReports.join('\n')}\n`);
  writeFileSync(summaryPath, `${JSON.stringify(summary, null, 2)}\n`);
  writeFileSync(
    commentPreviewPath,
    buildPRComment(readFileSync(combinedReportPath, 'utf8'), summary, '', marker)
  );

  writeOutput('report-path', combinedReportPath);
  writeOutput('summary-path', summaryPath);
  writeOutput('comment-preview-path', commentPreviewPath);
  writeOutput('failed', String(failed));
  writeOutput('output-dir', outputDir);
  writeOutput('artifact-paths', artifactPaths);
  appendStepSummary(combinedReports.join('\n'));
}

try {
  if (isTruthy(process.env.STAGEHAND_COMMENT_ONLY)) {
    await postOrUpdatePRComment({
      token: input('github-token'),
      reportPath: input('report-path'),
      summaryPath: input('summary-path'),
      artifactURL: input('artifact-url'),
      marker: commentMarker(input('comment-id', input('artifact-name', 'stagehand-run'))),
    });
  } else {
    main();
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
