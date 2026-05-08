import { AsyncLocalStorage } from 'node:async_hooks';

import type {
  CaptureBuffer,
  CapturedEvent,
  CapturedInteraction,
  CapturedRequest,
} from './capture.js';
import { ReplayMissError } from './replay.js';
import { getRuntime, type StagehandRuntime } from './runtime.js';

const TOOL_SERVICE = 'stagehand.tool';
const TOOL_PROTOCOL = 'tool';
const TOOL_METHOD = 'CALL';
const EMAIL_PATTERN = /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi;
const CARD_PATTERN = /\b(?:\d[ -]*?){13,19}\b/g;
const JWT_PATTERN = /\beyJ[A-Za-z0-9_-]+?\.[A-Za-z0-9_-]+?\.[A-Za-z0-9_-]+?\b/g;
const SENSITIVE_KEYS = new Set([
  'api_key',
  'apikey',
  'authorization',
  'password',
  'secret',
  'token',
]);
const parentInteractionStorage = new AsyncLocalStorage<string>();

export type ToolOptions = {
  name: string;
  sideEffect?: string;
  replay?: string;
};

export type ToolFunction<Args, Result> = (args: Args) => Result | Promise<Result>;

export class ToolReplayStore {
  private readonly entries = new Map<string, CapturedInteraction[]>();

  seed(interactions: Iterable<CapturedInteraction>): number {
    let count = 0;
    for (const interaction of interactions) {
      if (interaction.service !== TOOL_SERVICE) {
        continue;
      }
      const key = toolKeyFromInteraction(interaction);
      const queue = this.entries.get(key) ?? [];
      queue.push(structuredClone(interaction));
      this.entries.set(key, queue);
      count += 1;
    }
    return count;
  }

  popMatch(name: string, args: unknown): CapturedInteraction {
    const key = toolKey(name, args);
    const queue = this.entries.get(key);
    if (queue === undefined || queue.length === 0) {
      throw new ReplayMissError(toolRequest(name, args));
    }
    const interaction = queue.shift();
    if (queue.length === 0) {
      this.entries.delete(key);
    }
    if (interaction === undefined) {
      throw new ReplayMissError(toolRequest(name, args));
    }
    return interaction;
  }
}

export function tool<Args, Result>(
  options: ToolOptions,
  implementation: ToolFunction<Args, Result>
): ToolFunction<Args, Result> {
  const name = options.name.trim();
  if (name.length === 0) {
    throw new Error('tool name must be non-empty');
  }
  const sideEffect = options.sideEffect?.trim() || 'read';
  const replay = options.replay?.trim() || 'recorded';

  return ((args: Args): Result | Promise<Result> => {
    const runtime = getRuntime();
    if (runtime.mode === 'passthrough') {
      return implementation(args);
    }
    const injected = injectedToolError(runtime, name, args, { name, sideEffect, replay });
    if (injected !== undefined) {
      throw injected;
    }
    if (runtime.mode === 'replay' && replay === 'recorded') {
      return replayToolCall(runtime, name, args) as Result;
    }
    return recordToolCall(runtime, implementation, args, { name, sideEffect, replay });
  }) as ToolFunction<Args, Result>;
}

function injectedToolError(
  runtime: StagehandRuntime,
  name: string,
  args: unknown,
  options: Required<ToolOptions>
): Error | undefined {
  const decision = runtime.toolInjectionEngine().evaluate({
    service: TOOL_SERVICE,
    operation: name,
    tool: name,
  });
  if (decision === null) {
    return undefined;
  }
  const captureBuffer = runtime.toolCaptureBuffer();
  const { sequence, interactionId } = captureBuffer.reserveInteraction();
  const latencyMs = decision.override.latency_ms ?? 0;
  const errorKind = decision.override.error ?? 'injected_tool_error';
  const body = isRecord(decision.override.body) ? decision.override.body : {};
  const error = recordedError(
    injectedErrorClass(errorKind, body),
    injectedErrorMessage(errorKind, body)
  );
  captureBuffer.appendReservedInteraction(
    toolErrorInteraction({
      runtime,
      captureBuffer,
      sequence,
      interactionId,
      parentInteractionId: parentInteractionStorage.getStore(),
      options,
      args,
      elapsedMs: latencyMs,
      error,
      errorClass: error.name,
      metadata: { stagehand_injection: decision.provenance },
    })
  );
  return error;
}

function recordToolCall<Args, Result>(
  runtime: StagehandRuntime,
  implementation: ToolFunction<Args, Result>,
  args: Args,
  options: Required<ToolOptions>
): Result | Promise<Result> {
  const captureBuffer = runtime.toolCaptureBuffer();
  const { sequence, interactionId } = captureBuffer.reserveInteraction();
  const parentInteractionId = parentInteractionStorage.getStore();
  const started = performance.now();

  const completeSuccess = (result: unknown): unknown => {
    captureBuffer.appendReservedInteraction(
      toolSuccessInteraction({
        runtime,
        captureBuffer,
        sequence,
        interactionId,
        parentInteractionId,
        options,
        args,
        elapsedMs: elapsedMs(started),
        result,
      })
    );
    return result;
  };

  const completeError = (error: unknown): never => {
    captureBuffer.appendReservedInteraction(
      toolErrorInteraction({
        runtime,
        captureBuffer,
        sequence,
        interactionId,
        parentInteractionId,
        options,
        args,
        elapsedMs: elapsedMs(started),
        error,
      })
    );
    throw error;
  };

  try {
    const value = parentInteractionStorage.run(interactionId, () => implementation(args));
    if (isPromiseLike(value)) {
      return value.then(completeSuccess, completeError) as Result | Promise<Result>;
    }
    return completeSuccess(value as Awaited<Result>) as Result;
  } catch (error) {
    completeError(error);
  }
  throw new Error('unreachable');
}

function replayToolCall(runtime: StagehandRuntime, name: string, args: unknown): unknown {
  const scrubbedArgs = scrubValue(args, 'request.body.arguments').value;
  const interaction = runtime.toolReplayStore().popMatch(name, scrubbedArgs);
  const replayed = runtime
    .toolCaptureBuffer()
    .recordReplayInteraction(interaction, 'exact', 'recorded tool call');
  const terminal = replayed.events.at(-1);
  if (terminal?.type === 'error') {
    const data = terminal.data ?? {};
    throw recordedError(
      String(data.error_class ?? 'Error'),
      String(data.message ?? 'recorded tool error')
    );
  }
  if (terminal?.type !== 'response_received' || terminal.data === undefined) {
    throw new ReplayMissError(toolRequest(name, scrubbedArgs));
  }
  return terminal.data.result;
}

function toolSuccessInteraction({
  runtime,
  captureBuffer,
  sequence,
  interactionId,
  parentInteractionId,
  options,
  args,
  elapsedMs,
  result,
}: {
  runtime: StagehandRuntime;
  captureBuffer: CaptureBuffer;
  sequence: number;
  interactionId: string;
  parentInteractionId: string | undefined;
  options: Required<ToolOptions>;
  args: unknown;
  elapsedMs: number;
  result: unknown;
}): CapturedInteraction {
  const scrubbedResult = scrubValue(result, 'events[1].data.result');
  return toolInteraction({
    runtime,
    captureBuffer,
    sequence,
    interactionId,
    parentInteractionId,
    options,
    args,
    elapsedMs,
    redactedEventPaths: scrubbedResult.paths,
    events: [
      requestSentEvent(),
      {
        sequence: 2,
        t_ms: elapsedMs,
        sim_t_ms: elapsedMs,
        type: 'response_received',
        data: {
          result: scrubbedResult.value,
          side_effect: options.sideEffect,
          replay: options.replay,
        },
      },
    ],
  });
}

function toolErrorInteraction({
  runtime,
  captureBuffer,
  sequence,
  interactionId,
  parentInteractionId,
  options,
  args,
  elapsedMs,
  error,
  errorClass,
  metadata,
}: {
  runtime: StagehandRuntime;
  captureBuffer: CaptureBuffer;
  sequence: number;
  interactionId: string;
  parentInteractionId: string | undefined;
  options: Required<ToolOptions>;
  args: unknown;
  elapsedMs: number;
  error: unknown;
  errorClass?: string;
  metadata?: Record<string, unknown>;
}): CapturedInteraction {
  const scrubbedMessage = scrubValue(errorMessage(error), 'events[1].data.message');
  return toolInteraction({
    runtime,
    captureBuffer,
    sequence,
    interactionId,
    parentInteractionId,
    options,
    args,
    elapsedMs,
    redactedEventPaths: scrubbedMessage.paths,
    events: [
      requestSentEvent(),
      {
        sequence: 2,
        t_ms: elapsedMs,
        sim_t_ms: elapsedMs,
        type: 'error',
        data: {
          error_class: errorClass ?? errorClassForValue(error),
          message: scrubbedMessage.value,
          side_effect: options.sideEffect,
          replay: options.replay,
          ...(metadata ?? {}),
        },
      },
    ],
  });
}

function toolInteraction({
  runtime,
  captureBuffer,
  sequence,
  interactionId,
  parentInteractionId,
  options,
  args,
  elapsedMs,
  events,
  redactedEventPaths,
}: {
  runtime: StagehandRuntime;
  captureBuffer: CaptureBuffer;
  sequence: number;
  interactionId: string;
  parentInteractionId: string | undefined;
  options: Required<ToolOptions>;
  args: unknown;
  elapsedMs: number;
  events: CapturedEvent[];
  redactedEventPaths: string[];
}): CapturedInteraction {
  const scrubbedArgs = scrubValue(args, 'request.body.arguments');
  const request: CapturedRequest = {
    url: `stagehand://tool/${encodeURIComponent(options.name)}`,
    method: TOOL_METHOD,
    headers: {},
    body: {
      name: options.name,
      arguments: scrubbedArgs.value,
      side_effect: options.sideEffect,
      replay: options.replay,
    },
  };
  return {
    run_id: runtime.runId,
    interaction_id: interactionId,
    ...(parentInteractionId === undefined ? {} : { parent_interaction_id: parentInteractionId }),
    sequence,
    service: TOOL_SERVICE,
    operation: options.name,
    protocol: TOOL_PROTOCOL,
    streaming: false,
    request,
    events,
    scrub_report: captureBuffer.scrubReport([...scrubbedArgs.paths, ...redactedEventPaths]),
    latency_ms: elapsedMs,
  };
}

function toolKeyFromInteraction(interaction: CapturedInteraction): string {
  const body = isRecord(interaction.request.body) ? interaction.request.body : {};
  return toolKey(String(body.name ?? interaction.operation), body.arguments);
}

function toolRequest(name: string, args: unknown): CapturedRequest {
  return {
    url: `stagehand://tool/${encodeURIComponent(name)}`,
    method: TOOL_METHOD,
    headers: {},
    body: {
      name,
      arguments: args,
    },
  };
}

function toolKey(name: string, args: unknown): string {
  return stableStringify({ name, arguments: safeValue(args) });
}

function scrubValue(value: unknown, path: string): { value: unknown; paths: string[] } {
  if (Array.isArray(value)) {
    const paths: string[] = [];
    const items = value.map((item, index) => {
      const scrubbed = scrubValue(item, `${path}[${index}]`);
      paths.push(...scrubbed.paths);
      return scrubbed.value;
    });
    return { value: safeValue(items), paths };
  }
  if (isRecord(value)) {
    const paths: string[] = [];
    const record: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value)) {
      const childPath = `${path}.${key}`;
      if (SENSITIVE_KEYS.has(key.toLowerCase())) {
        record[key] = 'secret_scrubbed';
        paths.push(childPath);
        continue;
      }
      const scrubbed = scrubValue(item, childPath);
      record[key] = scrubbed.value;
      paths.push(...scrubbed.paths);
    }
    return { value: safeValue(record), paths };
  }
  if (typeof value === 'string') {
    let scrubbed = value;
    const paths: string[] = [];
    for (const [pattern, replacement] of [
      [JWT_PATTERN, 'jwt_scrubbed'],
      [EMAIL_PATTERN, 'user_scrubbed@scrub.local'],
      [CARD_PATTERN, 'card_scrubbed'],
    ] as const) {
      pattern.lastIndex = 0;
      if (pattern.test(scrubbed)) {
        pattern.lastIndex = 0;
        scrubbed = scrubbed.replace(pattern, replacement);
        paths.push(path);
      }
    }
    return { value: scrubbed, paths };
  }
  return { value: safeValue(value), paths: [] };
}

function safeValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => safeValue(item));
  }
  if (isRecord(value)) {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, safeValue(item)]));
  }
  if (
    value === null ||
    typeof value === 'string' ||
    typeof value === 'number' ||
    typeof value === 'boolean'
  ) {
    return value;
  }
  if (value === undefined) {
    return null;
  }
  return String(value);
}

function recordedError(name: string, message: string): Error {
  switch (name) {
    case 'TypeError':
      return new TypeError(message);
    case 'RangeError':
      return new RangeError(message);
    case 'ReferenceError':
      return new ReferenceError(message);
    case 'SyntaxError':
      return new SyntaxError(message);
    default: {
      const error = new Error(message);
      error.name = name;
      return error;
    }
  }
}

function injectedErrorClass(errorKind: string, body: Record<string, unknown>): string {
  const explicit = stringValue(body.error_class).trim();
  if (explicit !== '') {
    return explicit;
  }
  const parts = errorKind.split(/[^A-Za-z0-9]+/).filter((part) => part.length > 0);
  return `${parts.map((part) => part[0]?.toUpperCase() + part.slice(1)).join('')}Error`;
}

function injectedErrorMessage(errorKind: string, body: Record<string, unknown>): string {
  const explicit = stringValue(body.message).trim();
  if (explicit !== '') {
    return explicit;
  }
  return `Injected Stagehand tool error: ${errorKind}.`;
}

function requestSentEvent(): CapturedEvent {
  return { sequence: 1, t_ms: 0, sim_t_ms: 0, type: 'request_sent' };
}

function elapsedMs(started: number): number {
  return Math.max(0, Math.floor(performance.now() - started));
}

function errorClassForValue(error: unknown): string {
  return error instanceof Error ? error.name : 'Error';
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function stringValue(value: unknown): string {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value);
}

function isPromiseLike(value: unknown): value is Promise<unknown> {
  return isRecord(value) && typeof value.then === 'function';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function stableStringify(value: unknown): string {
  return JSON.stringify(normalizeForStableStringify(value));
}

function normalizeForStableStringify(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => normalizeForStableStringify(item));
  }
  if (isRecord(value)) {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, normalizeForStableStringify(value[key])])
    );
  }
  return value;
}
