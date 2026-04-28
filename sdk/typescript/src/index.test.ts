import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { MockAgent, fetch as undiciFetch, getGlobalDispatcher, setGlobalDispatcher } from 'undici';

import {
  AlreadyInitializedError,
  type CapturedInteraction,
  DEFAULT_CONFIG_FILENAME,
  ENV_CONFIG_PATH,
  ENV_MODE,
  ENV_REPLAY_INPUT,
  ENV_SESSION,
  ENV_OPENAI_HOSTS,
  InvalidModeError,
  NotInitializedError,
  ReplayFailureError,
  ReplayMissError,
  getRuntime,
  init,
  initFromEnv,
  isInitialized,
  seedReplayInteractions,
  VALID_MODES,
} from './index.js';
import { isOpenAIHost, openAIOperationFromURL } from './providers.js';
import { _resetForTests } from './runtime.js';

function withCleanRuntime(fn: () => void | Promise<void>): () => void | Promise<void> {
  return async () => {
    _resetForTests();
    try {
      await fn();
    } finally {
      _resetForTests();
      delete process.env.STAGEHAND_SESSION;
      delete process.env.STAGEHAND_MODE;
      delete process.env.STAGEHAND_CONFIG_PATH;
      delete process.env[ENV_OPENAI_HOSTS];
      delete process.env[ENV_REPLAY_INPUT];
    }
  };
}

test(
  'init returns runtime metadata for valid bootstrap inputs',
  withCleanRuntime(() => {
    const runtime = init({ session: 'demo', mode: 'record' });

    assert.equal(runtime.session, 'demo');
    assert.equal(runtime.mode, 'record');
    assert.match(runtime.runId, /^run_[0-9a-f]{12}$/);
    assert.equal(runtime.configPath, undefined);
    assert.equal(isInitialized(), true);
    assert.equal(getRuntime(), runtime);
    assert.equal(runtime.recorderMetadata().session, 'demo');
  })
);

test(
  'init auto-discovers stagehand config from cwd',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-config-'));
    const oldCwd = process.cwd();
    try {
      const configPath = path.join(tempDir, 'stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');
      process.chdir(tempDir);

      const runtime = init({ session: 'demo', mode: 'replay' });
      assert.equal(runtime.mode, 'replay');
      assert.equal(runtime.configPath, configPath);
    } finally {
      process.chdir(oldCwd);
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'init accepts explicit config path',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-explicit-'));
    try {
      const configPath = path.join(tempDir, 'custom.stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');

      const runtime = init({
        session: 'demo',
        mode: 'passthrough',
        configPath,
      });

      assert.equal(runtime.mode, 'passthrough');
      assert.equal(runtime.configPath, configPath);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'initFromEnv reads session mode and config path from environment',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-env-'));
    try {
      const configPath = path.join(tempDir, 'stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');
      process.env.STAGEHAND_SESSION = 'cli-demo';
      process.env.STAGEHAND_MODE = 'record';
      process.env.STAGEHAND_CONFIG_PATH = configPath;

      const runtime = initFromEnv();
      assert.equal(runtime.session, 'cli-demo');
      assert.equal(runtime.mode, 'record');
      assert.equal(runtime.configPath, configPath);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'runtime exports match the shared config parity fixture',
  withCleanRuntime(() => {
    const fixture = loadRuntimeConfigFixture();

    assert.equal(DEFAULT_CONFIG_FILENAME, fixture.default_config_filename);
    assert.deepEqual([...VALID_MODES], fixture.supported_modes);
    assert.equal(ENV_SESSION, fixture.shared_env_keys.session);
    assert.equal(ENV_MODE, fixture.shared_env_keys.mode);
    assert.equal(ENV_CONFIG_PATH, fixture.shared_env_keys.config_path);
    assert.equal(ENV_REPLAY_INPUT, fixture.shared_env_keys.replay_input);
    assert.equal(ENV_OPENAI_HOSTS, fixture.shared_env_keys.openai_hosts);
  })
);

test(
  'initFromEnv requires session and mode',
  withCleanRuntime(() => {
    assert.throws(
      () => initFromEnv(),
      /STAGEHAND_SESSION and STAGEHAND_MODE must be set for initFromEnv\(\)\./
    );

    process.env.STAGEHAND_SESSION = 'demo';
    assert.throws(
      () => initFromEnv(),
      /STAGEHAND_SESSION and STAGEHAND_MODE must be set for initFromEnv\(\)\./
    );

    delete process.env.STAGEHAND_SESSION;
    process.env.STAGEHAND_MODE = 'record';
    assert.throws(
      () => initFromEnv(),
      /STAGEHAND_SESSION and STAGEHAND_MODE must be set for initFromEnv\(\)\./
    );
  })
);

test(
  'record mode captures global fetch responses in shared artifact shape',
  withCleanRuntime(async () => {
    const { close, url } = await createTestServer((request, response) => {
      const chunks: Buffer[] = [];
      request.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
      request.on('end', () => {
        response.writeHead(201, { 'content-type': 'application/json' });
        response.end(
          JSON.stringify({
            ok: true,
            echoedMethod: request.method,
            receivedBody: Buffer.concat(chunks).toString('utf8'),
          })
        );
      });
    });

    try {
      const runtime = init({ session: 'fetch-demo', mode: 'record' });
      const response = await fetch(`${url}/echo`, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'x-stagehand-test': 'global-fetch',
        },
        body: JSON.stringify({ hello: 'world' }),
      });

      assert.equal(response.status, 201);
      assert.deepEqual(await response.json(), {
        ok: true,
        echoedMethod: 'POST',
        receivedBody: JSON.stringify({ hello: 'world' }),
      });

      const [interaction] = runtime.snapshotCapturedInteractions();
      assert.ok(interaction);
      assert.equal(interaction.request.method, 'POST');
      assert.equal(interaction.protocol, 'http');
      assert.equal(interaction.operation, 'POST /echo');
      assert.deepEqual(interaction.request.headers['x-stagehand-test'], ['global-fetch']);
      assert.deepEqual(interaction.request.body, { type: 'object' });

      const terminal = terminalEvent(interaction);
      assert.equal(terminal.type, 'response_received');
      assert.equal(terminal.data?.status_code, 201);
      assert.deepEqual(terminal.data?.body, {
        ok: true,
        echoedMethod: 'POST',
        receivedBody: JSON.stringify({ hello: 'world' }),
      });
    } finally {
      await close();
    }
  })
);

test(
  'record mode captures direct undici fetch traffic',
  withCleanRuntime(async () => {
    const { close, url } = await createTestServer((_request, response) => {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ client: 'undici' }));
    });

    try {
      const runtime = init({ session: 'undici-demo', mode: 'record' });
      const response = await undiciFetch(`${url}/undici`);
      assert.equal(response.status, 200);
      assert.deepEqual(await response.json(), { client: 'undici' });

      const [interaction] = runtime.snapshotCapturedInteractions();
      assert.ok(interaction);
      assert.equal(interaction.request.method, 'GET');
      assert.equal(interaction.operation, 'GET /undici');
      assert.equal(terminalEvent(interaction).type, 'response_received');
    } finally {
      await close();
    }
  })
);

test(
  'record mode emits stream and tool-call events for OpenAI-style SSE responses',
  withCleanRuntime(async () => {
    const streamBody = [
      'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","function":{"name":"lookup_customer"}}]}}]}',
      'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup_customer"}}]},"finish_reason":"tool_calls"}]}',
      'data: [DONE]',
    ].join('\n\n');

    const { close, url } = await createTestServer((_request, response) => {
      response.writeHead(200, { 'content-type': 'text/event-stream; charset=utf-8' });
      response.end(`${streamBody}\n\n`);
    });

    try {
      const runtime = init({ session: 'ts-stream-demo', mode: 'record' });
      const response = await fetch(`${url}/v1/chat/completions`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          model: 'gpt-4.1-mini',
          stream: true,
          messages: [{ role: 'user', content: 'hello' }],
        }),
      });

      assert.equal(response.status, 200);
      assert.match(await response.text(), /data: \[DONE\]/);

      const [interaction] = runtime.snapshotCapturedInteractions();
      assert.ok(interaction);
      assert.equal(interaction.streaming, true);
      assert.deepEqual(
        interaction.events.map((event) => event.type),
        [
          'request_sent',
          'response_received',
          'stream_chunk',
          'tool_call_start',
          'stream_chunk',
          'tool_call_end',
          'stream_end',
        ]
      );

      assert.deepEqual(interaction.events[1]?.data?.body, { capture_status: 'stream' });
      assert.equal(interaction.events[3]?.nested_interaction_id, 'tool_call_0');
      assert.deepEqual(interaction.events[3]?.data, {
        index: 0,
        tool_call_id: 'call_123',
        name: 'lookup_customer',
      });
      assert.equal(interaction.events[5]?.nested_interaction_id, 'tool_call_0');
      assert.deepEqual(interaction.events[5]?.data, {
        index: 0,
        tool_call_id: null,
        name: 'lookup_customer',
      });
      assert.equal(interaction.events[6]?.data?.chunk, 'data: [DONE]\n\n');
    } finally {
      await close();
    }
  })
);

test(
  'configured custom OpenAI hosts are classified as OpenAI-compatible traffic',
  withCleanRuntime(() => {
    process.env[ENV_OPENAI_HOSTS] = 'gateway.openai.local';

    assert.equal(isOpenAIHost('gateway.openai.local'), true);
    assert.equal(isOpenAIHost('api.openai.com'), true);
    assert.equal(isOpenAIHost('example.internal'), false);
    assert.equal(
      openAIOperationFromURL('POST', new URL('https://gateway.openai.local/v1/chat/completions')),
      'chat.completions.create'
    );
    assert.equal(
      openAIOperationFromURL('POST', new URL('https://gateway.openai.local/v1/responses')),
      'responses.create'
    );
    assert.equal(
      openAIOperationFromURL('GET', new URL('https://gateway.openai.local/v1/chat/completions')),
      undefined
    );
  })
);

test(
  'replay mode returns an exact seeded response without hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'application/json', 'x-stagehand': 'live' });
      response.end(JSON.stringify({ source: 'live' }));
    });

    try {
      const recordRuntime = init({ session: 'ts-replay-record', mode: 'record' });
      const recorded = await fetch(`${url}/seed`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ hello: 'world' }),
      });
      assert.equal(recorded.status, 200);
      assert.deepEqual(await recorded.json(), { source: 'live' });

      const seededInteractions = recordRuntime.snapshotCapturedInteractions();
      assert.equal(seededInteractions.length, 1);
      const hitsBeforeReplay = hitCount;

      _resetForTests();

      const replayRuntime = init({ session: 'ts-replay-playback', mode: 'replay' });
      seedReplayInteractions(seededInteractions);

      const replayed = await fetch(`${url}/seed`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ hello: 'world' }),
      });

      assert.equal(replayed.status, 200);
      assert.equal(replayed.headers.get('x-stagehand'), 'live');
      assert.deepEqual(await replayed.json(), { source: 'live' });
      assert.equal(hitCount, hitsBeforeReplay);

      const [capturedReplay] = replayRuntime.snapshotCapturedInteractions();
      assert.ok(capturedReplay);
      assert.equal(capturedReplay.fallback_tier, 'exact');
      assert.equal(terminalEvent(capturedReplay).type, 'response_received');
    } finally {
      await close();
    }
  })
);

test(
  'replay mode raises typed failure for seeded timeout interactions without hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ unexpected: 'live' }));
    });

    try {
      const replayRuntime = init({ session: 'ts-replay-timeout', mode: 'replay' });
      replayRuntime.seedReplayInteractions([
        makeSeededInteraction({
          runId: 'run_seed_timeout',
          url: `${url}/timeout`,
          method: 'GET',
          terminal: {
            type: 'timeout',
            data: {
              error_class: 'AbortError',
              message: 'request timed out',
            },
          },
        }),
      ]);

      const hitsBeforeReplay = hitCount;
      await assertFetchRejectsWithCause(fetch(`${url}/timeout`), ReplayFailureError);
      assert.equal(hitCount, hitsBeforeReplay);

      const [capturedReplay] = replayRuntime.snapshotCapturedInteractions();
      assert.ok(capturedReplay);
      assert.equal(terminalEvent(capturedReplay).type, 'timeout');
      assert.equal(capturedReplay.fallback_tier, 'exact');
    } finally {
      await close();
    }
  })
);

test(
  'replay mode returns exact seeded chat-completions SSE without hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const streamBody = [
      'data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","function":{"name":"lookup_customer"}}]}}]}',
      'data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup_customer"}}]},"finish_reason":"tool_calls"}]}',
      'data: [DONE]',
    ].join('\n\n');

    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.end(`${streamBody}\n\n`);
    });

    try {
      const recordRuntime = init({ session: 'ts-stream-replay-record', mode: 'record' });
      const recorded = await fetch(`${url}/v1/chat/completions`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          model: 'gpt-4.1-mini',
          stream: true,
          messages: [{ role: 'user', content: 'hello' }],
        }),
      });
      assert.equal(recorded.status, 200);
      const recordedBody = await recorded.text();
      const recordedInteractions = recordRuntime.snapshotCapturedInteractions();
      const hitsBeforeReplay = hitCount;

      _resetForTests();

      const replayRuntime = init({ session: 'ts-stream-replay-playback', mode: 'replay' });
      seedReplayInteractions(recordedInteractions);

      const replayed = await fetch(`${url}/v1/chat/completions`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          model: 'gpt-4.1-mini',
          stream: true,
          messages: [{ role: 'user', content: 'hello' }],
        }),
      });

      assert.equal(replayed.status, 200);
      assert.equal(replayed.headers.get('content-type'), 'text/event-stream');
      assert.equal(await replayed.text(), recordedBody);
      assert.equal(hitCount, hitsBeforeReplay);

      const [capturedReplay] = replayRuntime.snapshotCapturedInteractions();
      assert.ok(capturedReplay);
      assert.equal(capturedReplay.fallback_tier, 'exact');
      assert.equal(capturedReplay.streaming, true);
      assert.deepEqual(
        capturedReplay.events.map((event) => event.type),
        [
          'request_sent',
          'response_received',
          'stream_chunk',
          'tool_call_start',
          'stream_chunk',
          'tool_call_end',
          'stream_end',
        ]
      );
      assert.equal(capturedReplay.events[3]?.nested_interaction_id, 'tool_call_0');
      assert.equal(capturedReplay.events[5]?.nested_interaction_id, 'tool_call_0');
      assert.equal(terminalEvent(capturedReplay).type, 'stream_end');
    } finally {
      await close();
    }
  })
);

test(
  'replay mode returns exact seeded responses SSE with ordered chunks and final done marker',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const streamBody = [
      'data: {"type":"response.output_text.delta","delta":"Hello"}',
      'data: {"type":"response.output_text.delta","delta":" world"}',
      'data: [DONE]',
    ].join('\n\n');

    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.end(`${streamBody}\n\n`);
    });

    try {
      const recordRuntime = init({ session: 'ts-responses-stream-record', mode: 'record' });
      const recorded = await fetch(`${url}/v1/responses`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          model: 'gpt-4.1-mini',
          stream: true,
          input: [{ role: 'user', content: 'hello' }],
        }),
      });
      assert.equal(recorded.status, 200);
      const recordedBody = await recorded.text();
      const recordedInteractions = recordRuntime.snapshotCapturedInteractions();
      const hitsBeforeReplay = hitCount;

      _resetForTests();

      const replayRuntime = init({ session: 'ts-responses-stream-replay', mode: 'replay' });
      seedReplayInteractions(recordedInteractions);

      const replayed = await fetch(`${url}/v1/responses`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          model: 'gpt-4.1-mini',
          stream: true,
          input: [{ role: 'user', content: 'hello' }],
        }),
      });

      assert.equal(replayed.status, 200);
      assert.equal(replayed.headers.get('content-type'), 'text/event-stream');
      assert.equal(await replayed.text(), recordedBody);
      assert.equal(hitCount, hitsBeforeReplay);

      const [capturedReplay] = replayRuntime.snapshotCapturedInteractions();
      assert.ok(capturedReplay);
      assert.equal(capturedReplay.fallback_tier, 'exact');
      assert.equal(capturedReplay.streaming, true);
      assert.equal(terminalEvent(capturedReplay).type, 'stream_end');
    } finally {
      await close();
    }
  })
);

test(
  'replay mode raises typed failure for seeded streaming timeout interactions without hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.end('data: [DONE]\n\n');
    });

    try {
      const replayRuntime = init({ session: 'ts-stream-timeout', mode: 'replay' });
      replayRuntime.seedReplayInteractions([
        makeSeededStreamingInteraction({
          runId: 'run_seed_stream_timeout',
          url: `${url}/v1/chat/completions`,
          method: 'POST',
          terminal: {
            type: 'timeout',
            data: {
              error_class: 'AbortError',
              message: 'stream request timed out',
            },
          },
        }),
      ]);

      const hitsBeforeReplay = hitCount;
      await assertFetchRejectsWithCause(
        fetch(`${url}/v1/chat/completions`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({
            model: 'gpt-4.1-mini',
            stream: true,
            messages: [{ role: 'user', content: 'hello' }],
          }),
        }),
        ReplayFailureError
      );
      assert.equal(hitCount, hitsBeforeReplay);

      const [capturedReplay] = replayRuntime.snapshotCapturedInteractions();
      assert.ok(capturedReplay);
      assert.equal(capturedReplay.fallback_tier, 'exact');
      assert.equal(terminalEvent(capturedReplay).type, 'timeout');
    } finally {
      await close();
    }
  })
);

test(
  'replay mode fails closed with a typed error for unsupported seeded streaming shapes',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.end('data: [DONE]\n\n');
    });

    try {
      const replayRuntime = init({ session: 'ts-stream-unsupported', mode: 'replay' });
      replayRuntime.seedReplayInteractions([
        makeSeededStreamingInteraction({
          runId: 'run_seed_stream_unsupported',
          url: `${url}/v1/chat/completions`,
          method: 'POST',
          terminal: {
            type: 'stream_end',
            data: {
              chunk: '',
            },
          },
          includeResponseReceived: true,
        }),
      ]);

      const hitsBeforeReplay = hitCount;
      await assertFetchRejectsWithCause(
        fetch(`${url}/v1/chat/completions`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({
            model: 'gpt-4.1-mini',
            stream: true,
            messages: [{ role: 'user', content: 'hello' }],
          }),
        }),
        ReplayFailureError,
        (error) => {
          assert.ok(error instanceof ReplayFailureError);
          assert.equal(error.terminalEventType, 'missing_stream_chunks');
        }
      );
      assert.equal(hitCount, hitsBeforeReplay);
    } finally {
      await close();
    }
  })
);

test(
  'replay mode fails closed on a streaming miss before hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.end('data: [DONE]\n\n');
    });

    try {
      init({ session: 'ts-stream-miss', mode: 'replay' });
      const hitsBeforeReplay = hitCount;

      await assertFetchRejectsWithCause(
        fetch(`${url}/v1/responses`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({
            model: 'gpt-4.1-mini',
            stream: true,
            input: [{ role: 'user', content: 'hello' }],
          }),
        }),
        ReplayMissError
      );
      assert.equal(hitCount, hitsBeforeReplay);
    } finally {
      await close();
    }
  })
);

test(
  'replay mode fails closed on a miss before hitting the network',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ source: 'live' }));
    });

    try {
      init({ session: 'ts-replay-miss', mode: 'replay' });
      const hitsBeforeReplay = hitCount;

      await assertFetchRejectsWithCause(fetch(`${url}/miss`), ReplayMissError);
      assert.equal(hitCount, hitsBeforeReplay);
    } finally {
      await close();
    }
  })
);

test(
  'initFromEnv loads replay interactions from bundle input',
  withCleanRuntime(async () => {
    let hitCount = 0;
    const { close, url } = await createTestServer((_request, response) => {
      hitCount += 1;
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ source: 'live' }));
    });

    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-replay-env-'));
    try {
      const bundlePath = path.join(tempDir, 'replay.json');
      fs.writeFileSync(
        bundlePath,
        JSON.stringify({
          interactions: [
            makeSeededInteraction({
              runId: 'run_env_seed',
              url: `${url}/env-seed`,
              method: 'GET',
              terminal: {
                type: 'response_received',
                data: {
                  status_code: 202,
                  headers: {
                    'content-type': ['application/json'],
                    'x-stagehand': ['seeded'],
                  },
                  body: { source: 'seeded' },
                },
              },
            }),
          ],
        }),
        'utf8'
      );

      process.env.STAGEHAND_SESSION = 'ts-env-replay';
      process.env.STAGEHAND_MODE = 'replay';
      process.env[ENV_REPLAY_INPUT] = bundlePath;

      initFromEnv();

      const hitsBeforeReplay = hitCount;
      const response = await fetch(`${url}/env-seed`);
      assert.equal(response.status, 202);
      assert.equal(response.headers.get('x-stagehand'), 'seeded');
      assert.deepEqual(await response.json(), { source: 'seeded' });
      assert.equal(hitCount, hitsBeforeReplay);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
      await close();
    }
  })
);

test(
  'record mode captures connection failures as error interactions',
  withCleanRuntime(async () => {
    const { close, url } = await createTestServer((request) => {
      request.socket.destroy(new Error('simulated socket close'));
    });

    try {
      const runtime = init({ session: 'fetch-error-demo', mode: 'record' });

      await assert.rejects(fetch(`${url}/broken`));

      const [interaction] = runtime.snapshotCapturedInteractions();
      assert.ok(interaction);
      const terminal = terminalEvent(interaction);
      assert.equal(terminal.type, 'error');
      assert.equal(interaction.request.method, 'GET');
      assert.equal(interaction.operation, 'GET /broken');
      assert.match(String(terminal.data?.error_class ?? ''), /error/i);
    } finally {
      await close();
    }
  })
);

test(
  'record mode captures aborted requests as timeout interactions',
  withCleanRuntime(async () => {
    const { close, url } = await createTestServer((_request, response) => {
      setTimeout(() => {
        response.writeHead(200, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ slow: true }));
      }, 200);
    });

    try {
      const runtime = init({ session: 'fetch-timeout-demo', mode: 'record' });
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 20);

      try {
        await assert.rejects(fetch(`${url}/slow`, { signal: controller.signal }));
      } finally {
        clearTimeout(timer);
      }

      const [interaction] = runtime.snapshotCapturedInteractions();
      assert.ok(interaction);
      const terminal = terminalEvent(interaction);
      assert.equal(terminal.type, 'timeout');
      assert.equal(interaction.operation, 'GET /slow');
    } finally {
      await close();
    }
  })
);

test(
  'TypeScript capture matches the shared success parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('http-get-success.json');
  })
);

test(
  'TypeScript capture matches the shared timeout parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('http-get-timeout.json');
  })
);

test(
  'TypeScript capture matches the shared default-host OpenAI parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('openai-chat-success-default-host.json');
  })
);

test(
  'TypeScript capture matches the shared custom-host OpenAI parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('openai-chat-success-custom-host.json');
  })
);

test(
  'TypeScript capture matches the shared OpenAI SSE parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('openai-sse-success.json');
  })
);

test(
  'TypeScript capture matches the shared scrub parity fixture',
  withCleanRuntime(async () => {
    await assertTypeScriptFixtureParity('http-post-scrub-fixture.json');
  })
);

test(
  'double init is rejected',
  withCleanRuntime(() => {
    init({ session: 'demo', mode: 'record' });

    assert.throws(() => init({ session: 'demo-again', mode: 'record' }), AlreadyInitializedError);
  })
);

test(
  'invalid mode is rejected',
  withCleanRuntime(() => {
    assert.throws(() => init({ session: 'demo', mode: 'hybrid' as never }), InvalidModeError);
  })
);

test(
  'empty session is rejected',
  withCleanRuntime(() => {
    assert.throws(
      () => init({ session: '   ', mode: 'record' }),
      /session must be a non-empty string/
    );
  })
);

test(
  'missing config path is rejected',
  withCleanRuntime(() => {
    const missingPath = path.join(os.tmpdir(), 'stagehand-missing-config.yml');
    assert.throws(
      () => init({ session: 'demo', mode: 'record', configPath: missingPath }),
      new RegExp(`Stagehand config file not found: ${escapeForRegExp(path.resolve(missingPath))}`)
    );
  })
);

test(
  'getRuntime requires prior initialization',
  withCleanRuntime(() => {
    assert.throws(() => getRuntime(), NotInitializedError);
  })
);

test(
  'reset and re-init produces an isolated capture buffer',
  withCleanRuntime(async () => {
    const firstRuntime = init({ session: 'first', mode: 'record' });

    const handler = (_req: http.IncomingMessage, res: http.ServerResponse) => {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ ok: true }));
    };
    const server = http.createServer(handler);
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') {
      server.close();
      throw new Error('failed to bind test server');
    }
    const baseURL = `http://127.0.0.1:${address.port}`;

    try {
      await undiciFetch(`${baseURL}/first`);
      assert.equal(firstRuntime.snapshotCapturedInteractions().length, 1);

      _resetForTests();

      const secondRuntime = init({ session: 'second', mode: 'record' });
      assert.equal(secondRuntime.snapshotCapturedInteractions().length, 0);

      await undiciFetch(`${baseURL}/second`);
      const interactions = secondRuntime.snapshotCapturedInteractions();
      assert.equal(interactions.length, 1);
      assert.equal(interactions[0].request.url.endsWith('/second'), true);
      assert.equal(interactions[0].run_id, secondRuntime.runId);
      assert.notEqual(interactions[0].run_id, firstRuntime.runId);
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  })
);

test(
  'concurrent record-mode fetches assign unique strictly-increasing sequences',
  withCleanRuntime(async () => {
    const runtime = init({ session: 'concurrent-record', mode: 'record' });

    const handler = (_req: http.IncomingMessage, res: http.ServerResponse) => {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ ok: true }));
    };
    const server = http.createServer(handler);
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') {
      server.close();
      throw new Error('failed to bind test server');
    }
    const baseURL = `http://127.0.0.1:${address.port}`;

    try {
      const total = 16;
      await Promise.all(
        Array.from({ length: total }, (_, idx) => undiciFetch(`${baseURL}/req-${idx}`))
      );

      const interactions = runtime.snapshotCapturedInteractions();
      assert.equal(interactions.length, total);

      const sequences = interactions.map((interaction) => interaction.sequence);
      const sorted = [...sequences].sort((a, b) => a - b);
      assert.deepEqual(sequences, sorted, 'snapshot must already be sorted by sequence');

      const seen = new Set<number>();
      for (const sequence of sequences) {
        if (seen.has(sequence)) {
          throw new Error(`duplicate sequence ${sequence}`);
        }
        seen.add(sequence);
      }
      assert.equal(seen.size, total);
      assert.deepEqual(sorted, Array.from({ length: total }, (_, idx) => idx + 1));
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  })
);

function escapeForRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

type ParityFixture = {
  env?: {
    openai_hosts?: string[];
  };
  request: {
    method: string;
    path?: string;
    url?: string;
    headers: Record<string, string>;
    body?: unknown;
  };
  response?: {
    status_code: number;
    headers: Record<string, string>;
    body?: unknown;
    stream_body?: string;
  };
  server_delay_ms?: number;
  expected_canonical: Record<string, unknown>;
};

type RuntimeConfigParityFixture = {
  default_config_filename: string;
  supported_modes: string[];
  shared_env_keys: {
    session: string;
    mode: string;
    config_path: string;
    replay_input: string;
    openai_hosts: string;
  };
};

function terminalEvent(interaction: CapturedInteraction) {
  const event = interaction.events.at(-1);
  assert.ok(event, 'expected captured interaction to have a terminal event');
  return event;
}

function makeSeededInteraction({
  runId,
  url,
  method,
  terminal,
}: {
  runId: string;
  url: string;
  method: string;
  terminal: {
    type: 'response_received' | 'error' | 'timeout';
    data: Record<string, unknown>;
  };
}): CapturedInteraction {
  return {
    run_id: runId,
    interaction_id: 'int_seeded0001',
    sequence: 1,
    service: new URL(url).hostname,
    operation: `${method} ${new URL(url).pathname}`,
    protocol: new URL(url).protocol.replace(/:$/, ''),
    streaming: false,
    request: {
      url,
      method,
      headers: {},
      body: null,
    },
    events: [
      {
        sequence: 1,
        t_ms: 0,
        sim_t_ms: 0,
        type: 'request_sent',
      },
      {
        sequence: 2,
        t_ms: 10,
        sim_t_ms: 10,
        type: terminal.type,
        data: terminal.data,
      },
    ],
    scrub_report: {
      scrub_policy_version: 'v0-unredacted',
      session_salt_id: 'salt_pending',
    },
    latency_ms: 10,
  };
}

function makeSeededStreamingInteraction({
  runId,
  url,
  method,
  terminal,
  includeResponseReceived = false,
}: {
  runId: string;
  url: string;
  method: string;
  terminal: {
    type: 'error' | 'timeout' | 'stream_end';
    data: Record<string, unknown>;
  };
  includeResponseReceived?: boolean;
}): CapturedInteraction {
  return {
    run_id: runId,
    interaction_id: 'int_seeded_stream_0001',
    sequence: 1,
    service: 'openai',
    operation: `${method} ${new URL(url).pathname}`,
    protocol: new URL(url).protocol.replace(/:$/, ''),
    streaming: true,
    request: {
      url,
      method,
      headers: {},
      body: { type: 'object' },
    },
    events: [
      {
        sequence: 1,
        t_ms: 0,
        sim_t_ms: 0,
        type: 'request_sent',
      },
      ...(includeResponseReceived
        ? [
            {
              sequence: 2,
              t_ms: 5,
              sim_t_ms: 5,
              type: 'response_received' as const,
              data: {
                status_code: 200,
                headers: {
                  'content-type': ['text/event-stream'],
                },
                body: { capture_status: 'stream' },
              },
            },
          ]
        : []),
      {
        sequence: includeResponseReceived ? 3 : 2,
        t_ms: 10,
        sim_t_ms: 10,
        type: terminal.type,
        data: terminal.data,
      },
    ],
    scrub_report: {
      scrub_policy_version: 'v0-unredacted',
      session_salt_id: 'salt_pending',
    },
    latency_ms: 10,
  };
}

async function assertFetchRejectsWithCause(
  promise: Promise<unknown>,
  expectedCause: new (...args: never[]) => Error,
  inspectCause?: (error: Error) => void
): Promise<void> {
  await assert.rejects(async () => {
    try {
      await promise;
    } catch (error) {
      assert.ok(error instanceof TypeError);
      assert.ok(
        error.cause instanceof expectedCause,
        `expected fetch error cause to be ${expectedCause.name}, got ${String(error.cause)}`
      );
      inspectCause?.(error.cause);
      throw error;
    }
  });
}

function loadParityFixture(name: string): ParityFixture {
  const repoRoot = path.resolve(process.cwd(), '..', '..');
  const fixturePath = path.join(repoRoot, 'testdata', 'sdk-parity', name);
  return JSON.parse(fs.readFileSync(fixturePath, 'utf8')) as ParityFixture;
}

function loadRuntimeConfigFixture(): RuntimeConfigParityFixture {
  const repoRoot = path.resolve(process.cwd(), '..', '..');
  const fixturePath = path.join(
    repoRoot,
    'testdata',
    'sdk-config-parity',
    'runtime-bootstrap.json'
  );
  return JSON.parse(fs.readFileSync(fixturePath, 'utf8')) as RuntimeConfigParityFixture;
}

async function assertTypeScriptFixtureParity(name: string): Promise<void> {
  const fixture = loadParityFixture(name);
  applyFixtureEnv(fixture);

  const target = await createParityTarget(fixture);
  try {
    const runtime = init({ session: `ts-parity-${slugifyFixtureName(fixture)}`, mode: 'record' });
    const requestURL = fixtureTargetURL(fixture, target.url);
    const requestInit = buildFixtureRequestInit(fixture);

    if (fixtureUsesStreamingResponse(fixture)) {
      const response = await fetch(requestURL, requestInit);
      assert.equal(response.status, fixture.response?.status_code);
      assert.match(await response.text(), /data:/);
    } else if (fixture.response !== undefined) {
      const response = await fetch(requestURL, requestInit);
      assert.equal(response.status, fixture.response.status_code);
      await response.text();
    } else {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 50);
      try {
        await assert.rejects(fetch(requestURL, { ...requestInit, signal: controller.signal }));
      } finally {
        clearTimeout(timer);
      }
    }

    const [interaction] = runtime.snapshotCapturedInteractions();
    assert.ok(interaction);
    assert.deepEqual(canonicalizeInteraction(interaction), fixture.expected_canonical);
  } finally {
    if (target.usesCustomDispatcher) {
      _resetForTests();
    }
    await target.close();
  }
}

function canonicalizeInteraction(interaction: CapturedInteraction): Record<string, unknown> {
  const requestURL = new URL(interaction.request.url);
  return {
    service: interaction.service,
    operation: interaction.operation,
    protocol: interaction.protocol,
    streaming: interaction.streaming,
    fallback_tier: interaction.fallback_tier ?? null,
    request: {
      method: interaction.request.method,
      path: requestURL.pathname + requestURL.search,
      headers: selectHeaders(interaction.request.headers, [
        'authorization',
        'content-type',
        'x-customer-email',
        'x-stagehand-parity',
      ]),
      body: canonicalizeRequestBody(interaction.request.body),
    },
    events: interaction.events.map((event) => ({
      sequence: event.sequence,
      type: event.type,
      ...(event.type === 'response_received'
        ? {
            data: {
              status_code: event.data?.status_code,
              headers: selectHeaders(
                (event.data?.headers as Record<string, string[] | undefined> | undefined) ?? {},
                ['content-type', 'x-parity', 'x-response-token']
              ),
              body: event.data?.body ?? null,
            },
          }
        : event.type === 'stream_chunk' || event.type === 'stream_end'
          ? {
              data: {
                chunk: event.data?.chunk ?? null,
              },
            }
          : event.type === 'tool_call_start' || event.type === 'tool_call_end'
            ? {
                ...(event.nested_interaction_id === undefined
                  ? {}
                  : { nested_interaction_id: event.nested_interaction_id }),
                data: {
                  index: event.data?.index ?? null,
                  tool_call_id: event.data?.tool_call_id ?? null,
                  name: event.data?.name ?? null,
                },
              }
            : {}),
    })),
    scrub_report: {
      scrub_policy_version: interaction.scrub_report.scrub_policy_version,
      session_salt_id: interaction.scrub_report.session_salt_id,
    },
  };
}

function canonicalizeRequestBody(body: unknown): unknown {
  if (body === null || body === undefined) {
    return null;
  }

  return { capture_kind: 'present' };
}

function selectHeaders(
  headers: Record<string, string[] | undefined>,
  allowed: string[]
): Record<string, string[]> {
  const selected: Record<string, string[]> = {};
  for (const name of allowed) {
    const value = headers[name];
    if (value === undefined) {
      continue;
    }
    selected[name] = value;
  }
  return selected;
}

async function createTestServer(
  handler: (request: http.IncomingMessage, response: http.ServerResponse) => void
): Promise<{ url: string; close: () => Promise<void> }> {
  const server = http.createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');

  return {
    url: `http://127.0.0.1:${address.port}`,
    close: async () => {
      await new Promise<void>((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    },
  };
}

function applyFixtureEnv(fixture: ParityFixture): void {
  const hosts = fixture.env?.openai_hosts;
  if (hosts !== undefined && hosts.length > 0) {
    process.env[ENV_OPENAI_HOSTS] = hosts.join(',');
  }
}

function fixtureTargetURL(fixture: ParityFixture, baseURL: string): string {
  return fixture.request.url ?? `${baseURL}${fixture.request.path ?? '/'}`;
}

function buildFixtureRequestInit(fixture: ParityFixture): RequestInit {
  const init: RequestInit = {
    method: fixture.request.method,
    headers: fixture.request.headers,
  };

  if (fixture.request.body !== undefined) {
    init.body = JSON.stringify(fixture.request.body);
  }

  return init;
}

function fixtureUsesStreamingResponse(fixture: ParityFixture): boolean {
  return fixture.response?.stream_body !== undefined;
}

function slugifyFixtureName(fixture: ParityFixture): string {
  return (
    (fixture as { name?: string }).name?.replace(/[^a-z0-9]+/gi, '-').toLowerCase() ?? 'parity'
  );
}

async function createParityTarget(
  fixture: ParityFixture
): Promise<{ url: string; close: () => Promise<void>; usesCustomDispatcher: boolean }> {
  if (fixture.request.url !== undefined) {
    const previousDispatcher = getGlobalDispatcher();
    const mockAgent = new MockAgent();
    mockAgent.disableNetConnect();

    const requestURL = new URL(fixture.request.url);
    const mockPool = mockAgent.get(requestURL.origin);
    const pathWithQuery = requestURL.pathname + requestURL.search;
    const replyBody =
      fixture.response?.stream_body ??
      JSON.stringify((fixture.response?.body ?? { ok: true }) as Record<string, unknown>);

    mockPool
      .intercept({
        path: pathWithQuery,
        method: fixture.request.method,
      })
      .reply(fixture.response?.status_code ?? 200, replyBody, {
        headers: fixture.response?.headers ?? { 'content-type': 'application/json' },
      });

    setGlobalDispatcher(mockAgent);
    return {
      url: requestURL.origin,
      usesCustomDispatcher: true,
      close: async () => {
        await mockAgent.close();
        setGlobalDispatcher(previousDispatcher);
      },
    };
  }

  const server = await createFixtureServer(fixture);
  return {
    ...server,
    usesCustomDispatcher: false,
  };
}

async function createFixtureServer(
  fixture: ParityFixture
): Promise<{ url: string; close: () => Promise<void> }> {
  return createTestServer((request, response) => {
    assert.equal(request.method, fixture.request.method);
    assert.equal(decodeURIComponent(request.url ?? ''), fixture.request.path);
    request.on('end', () => {
      if (fixture.response !== undefined) {
        if (fixture.response.stream_body !== undefined) {
          response.writeHead(fixture.response.status_code, fixture.response.headers);
          response.end(fixture.response.stream_body);
          return;
        }

        response.writeHead(fixture.response.status_code, fixture.response.headers);
        response.end(JSON.stringify(fixture.response.body));
        return;
      }

      setTimeout(() => {
        response.writeHead(200, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ slow: true }));
      }, fixture.server_delay_ms ?? 200);
    });

    request.resume();
  });
}
