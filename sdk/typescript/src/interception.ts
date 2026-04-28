import { Readable } from 'node:stream';

import { Dispatcher, getGlobalDispatcher, setGlobalDispatcher } from 'undici';

import type {
  CaptureBuffer,
  CapturedHeaders,
  CapturedInteraction,
  CapturedRequest,
} from './capture.js';
import { isOpenAIHost, openAIOperationFromURL } from './providers.js';
import { ExactReplayStore, ReplayFailureError } from './replay.js';

let originalDispatcher: Dispatcher | undefined;
let installed = false;

const KNOWN_SERVICE_HOSTS: Record<string, string> = {
  'api.openai.com': 'openai',
  'api.anthropic.com': 'anthropic',
  'api.stripe.com': 'stripe',
  'slack.com': 'slack',
  'api.slack.com': 'slack',
};

export function installRequestInterception({
  mode,
  captureBuffer,
  replayStore,
}: {
  mode: 'record' | 'replay' | 'passthrough';
  captureBuffer: CaptureBuffer;
  replayStore: ExactReplayStore;
}): void {
  if (installed) {
    return;
  }

  originalDispatcher = getGlobalDispatcher();
  const wrappedDispatcher = originalDispatcher.compose((dispatch) => {
    return function stagehandDispatch(options, handler) {
      const startedAt = Date.now();
      const request = normalizeCapturedRequest(options);
      const targetURL = safeURL(request.url);
      const service = classifyService(targetURL);
      const operation = classifyOperation({
        method: request.method,
        url: targetURL,
        body: request.body,
      });
      const protocol = targetURL?.protocol.replace(/:$/, '') || 'http';
      const responseChunks: Buffer[] = [];
      let responseStatusCode = 0;
      let responseStatusText = '';
      let responseHeaders: CapturedHeaders = {};
      let recorded = false;
      const abortSignal = (options as { signal?: AbortSignal }).signal;
      let removeAbortListener = (): void => {};

      if (mode === 'replay') {
        try {
          if (abortSignal?.aborted) {
            const capturedAbort = captureBuffer.recordFailure({
              service,
              operation,
              protocol,
              request,
              elapsedMs: elapsedSince(startedAt),
              failureType: 'timeout',
              errorClass: abortReason(abortSignal).name || undefined,
              message: abortReason(abortSignal).message || undefined,
            });
            throw new ReplayFailureError({
              interaction: capturedAbort,
              terminalEventType: 'aborted',
              errorClass: abortReason(abortSignal).name || undefined,
              detail: abortReason(abortSignal).message || undefined,
            });
          }

          const match = replayStore.popMatch(request);
          captureBuffer.recordReplayInteraction(match.interaction, 'exact');
          return dispatchReplayInteraction({
            request,
            interaction: match.interaction,
            handler,
            abortSignal,
          });
        } catch (error) {
          throw wrapReplayError(error, request);
        }
      }

      const finalizeSuccess = (): void => {
        if (recorded) {
          return;
        }
        recorded = true;
        removeAbortListener();
        const elapsedMs = elapsedSince(startedAt);
        const contentType = responseHeaders['content-type']?.[0] ?? '';

        if (
          isStreamingResponse(responseHeaders) &&
          isOpenAIStyleStreamingRequest({
            request,
            targetURL,
          })
        ) {
          captureBuffer.recordOpenAIStreamSuccess({
            service,
            operation,
            protocol,
            request,
            elapsedMs,
            response: {
              statusCode: responseStatusCode,
              statusText: responseStatusText || undefined,
              headers: responseHeaders,
              rawBody: Buffer.concat(responseChunks).toString('utf8'),
            },
          });
          return;
        }

        const responseBody = normalizeResponseBody(responseChunks, contentType);

        captureBuffer.recordSuccess({
          service,
          operation,
          protocol,
          request,
          elapsedMs,
          streaming: isStreamingResponse(responseHeaders),
          response: {
            statusCode: responseStatusCode,
            statusText: responseStatusText || undefined,
            headers: responseHeaders,
            body: responseBody,
          },
        });
      };

      const finalizeFailure = (error: Error): void => {
        if (recorded) {
          return;
        }
        recorded = true;
        removeAbortListener();
        captureBuffer.recordFailure({
          service,
          operation,
          protocol,
          request,
          elapsedMs: elapsedSince(startedAt),
          failureType: timeoutLike(error) ? 'timeout' : 'error',
          errorClass: error.name || undefined,
          message: error.message || undefined,
        });
      };
      const finalizeAbort = (): void => {
        finalizeFailure(abortReason(abortSignal));
      };
      removeAbortListener = (): void => {
        abortSignal?.removeEventListener('abort', finalizeAbort);
      };

      if (abortSignal?.aborted) {
        finalizeAbort();
      } else {
        abortSignal?.addEventListener('abort', finalizeAbort, { once: true });
      }

      const wrappedHandler: Dispatcher.DispatchHandler = {
        onRequestStart(controller, context) {
          handler.onRequestStart?.(controller, context);
        },
        onRequestUpgrade(controller, statusCode, headers, socket) {
          handler.onRequestUpgrade?.(controller, statusCode, headers, socket);
        },
        onResponseStart(controller, statusCode, headers, statusMessage) {
          responseStatusCode = statusCode;
          responseStatusText = statusMessage ?? '';
          responseHeaders = normalizeIncomingHeaders(headers);
          handler.onResponseStart?.(controller, statusCode, headers, statusMessage);
        },
        onResponseData(controller, chunk) {
          responseChunks.push(Buffer.from(chunk));
          handler.onResponseData?.(controller, chunk);
        },
        onResponseEnd(controller, trailers) {
          finalizeSuccess();
          handler.onResponseEnd?.(controller, trailers);
        },
        onResponseError(controller, error) {
          finalizeFailure(error);
          handler.onResponseError?.(controller, error);
        },
        onConnect(abort) {
          handler.onConnect?.(abort);
        },
        onError(error) {
          finalizeFailure(error);
          handler.onError?.(error);
        },
        onUpgrade(statusCode, headers, socket) {
          handler.onUpgrade?.(statusCode, headers, socket);
        },
        onResponseStarted() {
          handler.onResponseStarted?.();
        },
        onHeaders(statusCode, headers, resume, statusText) {
          responseStatusCode = statusCode;
          responseStatusText = statusText;
          responseHeaders = normalizeRawHeaderPairs(headers);
          return handler.onHeaders?.(statusCode, headers, resume, statusText) ?? true;
        },
        onData(chunk) {
          responseChunks.push(Buffer.from(chunk));
          return handler.onData?.(chunk) ?? true;
        },
        onComplete(trailers) {
          finalizeSuccess();
          handler.onComplete?.(trailers);
        },
        onBodySent(chunkSize, totalBytesSent) {
          handler.onBodySent?.(chunkSize, totalBytesSent);
        },
      };

      try {
        return dispatch(options, wrappedHandler);
      } catch (error) {
        finalizeFailure(asError(error));
        throw error;
      }
    };
  });

  setGlobalDispatcher(wrappedDispatcher);
  installed = true;
}

function dispatchReplayInteraction({
  request,
  interaction,
  handler,
  abortSignal,
}: {
  request: CapturedRequest;
  interaction: CapturedInteraction;
  handler: Dispatcher.DispatchHandler;
  abortSignal?: AbortSignal;
}): boolean {
  const terminalEvent = terminalReplayEvent(interaction.events);
  if (terminalEvent === undefined) {
    throw new ReplayFailureError({
      interaction,
      terminalEventType: 'missing_terminal_event',
      detail: 'captured interaction is not replayable without a terminal event',
    });
  }

  if (terminalEvent.type === 'error' || terminalEvent.type === 'timeout') {
    throw new ReplayFailureError({
      interaction,
      terminalEventType: terminalEvent.type,
      errorClass: asOptionalString(terminalEvent.data?.error_class),
      detail: asOptionalString(terminalEvent.data?.message),
    });
  }

  const responseEvent = interaction.events.find((event) => event.type === 'response_received');
  const streaming = interaction.streaming || terminalEvent.type === 'stream_end';
  if (responseEvent === undefined && !streaming) {
    throw new ReplayFailureError({
      interaction,
      terminalEventType: 'missing_response_received',
      detail: 'captured interaction does not contain a response_received event',
    });
  }

  const responseData = responseEvent?.data ?? defaultReplayResponseData({ streaming });
  const statusCode = asStatusCode(responseData.status_code);
  const statusText = asOptionalString(responseData.status_text) ?? '';
  const headers = normalizeReplayHeaders(responseData.headers);
  const replayChunks = streaming ? extractReplayStreamChunks(interaction) : [];

  if (streaming) {
    ensureStreamingContentType(headers);
    if (replayChunks.length === 0) {
      throw new ReplayFailureError({
        interaction,
        terminalEventType: 'missing_stream_chunks',
        detail: 'captured streaming interaction does not contain replayable chunks',
      });
    }
  }

  queueMicrotask(() => {
    const controller = createReplayController();
    const rawHeaders = flattenHeaders(headers);
    const replayAbortReason = (): Error => abortReason(abortSignal);

    try {
      if (abortSignal?.aborted) {
        throw replayAbortReason();
      }

      handler.onRequestStart?.(controller, { replay: true, request });
      handler.onConnect?.(() => controller.abort(new Error('replay request aborted')));
      handler.onResponseStarted?.();
      handler.onHeaders?.(statusCode, rawHeaders, () => {}, statusText);
      handler.onResponseStart?.(controller, statusCode, headers, statusText);

      if (streaming) {
        for (const chunk of replayChunks) {
          if (abortSignal?.aborted) {
            throw replayAbortReason();
          }
          handler.onData?.(chunk);
          handler.onResponseData?.(controller, chunk);
        }
      } else {
        const body = encodeReplayBody(responseData.body, headers['content-type']?.[0] ?? '');
        if (body.length > 0) {
          if (abortSignal?.aborted) {
            throw replayAbortReason();
          }
          handler.onData?.(body);
          handler.onResponseData?.(controller, body);
        }
      }
      handler.onComplete?.([]);
      handler.onResponseEnd?.(controller, {});
    } catch (error) {
      const replayError = asError(error);
      handler.onResponseError?.(controller, replayError);
      handler.onError?.(replayError);
    }
  });

  return true;
}

function defaultReplayResponseData({ streaming }: { streaming: boolean }): Record<string, unknown> {
  const headers: Record<string, string[]> = {};
  if (streaming) {
    headers['content-type'] = ['text/event-stream'];
  }

  return {
    status_code: 200,
    headers,
    body: null,
  };
}

function ensureStreamingContentType(headers: CapturedHeaders): void {
  const current = headers['content-type']?.[0]?.split(';', 1)[0].trim().toLowerCase();
  if (current === 'text/event-stream') {
    return;
  }

  headers['content-type'] = ['text/event-stream'];
}

function extractReplayStreamChunks(interaction: CapturedInteraction): Buffer[] {
  const chunks: Buffer[] = [];
  for (const event of interaction.events) {
    if (
      (event.type === 'stream_chunk' || event.type === 'stream_end') &&
      event.data !== undefined
    ) {
      const chunk = event.data.chunk;
      if (typeof chunk === 'string' && chunk.length > 0) {
        chunks.push(Buffer.from(chunk, 'utf8'));
      }
    }
  }

  return chunks;
}

function terminalReplayEvent(
  events: Array<{ type: string; data?: Record<string, unknown> }>
): { type: string; data?: Record<string, unknown> } | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (
      event.type === 'response_received' ||
      event.type === 'error' ||
      event.type === 'timeout' ||
      event.type === 'stream_end'
    ) {
      return event;
    }
  }

  return undefined;
}

function createReplayController(): Dispatcher.DispatchController {
  let aborted = false;
  let paused = false;
  let reason: Error | null = null;

  return {
    get aborted() {
      return aborted;
    },
    get paused() {
      return paused;
    },
    get reason() {
      return reason;
    },
    abort(nextReason: Error) {
      aborted = true;
      reason = nextReason;
    },
    pause() {
      paused = true;
    },
    resume() {
      paused = false;
    },
  };
}

function normalizeReplayHeaders(value: unknown): CapturedHeaders {
  const normalized: CapturedHeaders = {};
  if (value === null || value === undefined || typeof value !== 'object') {
    return normalized;
  }

  for (const [name, raw] of Object.entries(value as Record<string, unknown>)) {
    if (Array.isArray(raw)) {
      normalized[name.toLowerCase()] = raw.map(String);
      continue;
    }
    if (raw !== undefined) {
      normalized[name.toLowerCase()] = [String(raw)];
    }
  }

  return normalized;
}

function flattenHeaders(headers: CapturedHeaders): Buffer[] {
  const flattened: Buffer[] = [];
  for (const [name, values] of Object.entries(headers)) {
    for (const value of values) {
      flattened.push(Buffer.from(name, 'utf8'), Buffer.from(value, 'utf8'));
    }
  }
  return flattened;
}

function encodeReplayBody(body: unknown, contentType: string): Buffer {
  if (body === null || body === undefined) {
    return Buffer.alloc(0);
  }

  if (typeof body === 'string') {
    return Buffer.from(body, 'utf8');
  }

  if (Buffer.isBuffer(body)) {
    return body;
  }

  if (body instanceof Uint8Array) {
    return Buffer.from(body);
  }

  const mediaType = contentType.split(';', 1)[0].trim().toLowerCase();
  if (mediaType === 'application/json' || typeof body === 'object') {
    return Buffer.from(JSON.stringify(body), 'utf8');
  }

  return Buffer.from(String(body), 'utf8');
}

function asOptionalString(value: unknown): string | undefined {
  if (value === null || value === undefined) {
    return undefined;
  }
  return String(value);
}

function asStatusCode(value: unknown): number {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 100) {
    return value;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseInt(value, 10);
    if (Number.isInteger(parsed) && parsed >= 100) {
      return parsed;
    }
  }
  return 200;
}

export function resetRequestInterceptionForTests(): void {
  if (originalDispatcher !== undefined) {
    setGlobalDispatcher(originalDispatcher);
  }
  originalDispatcher = undefined;
  installed = false;
}

function normalizeCapturedRequest(options: Dispatcher.DispatchOptions): CapturedRequest {
  const url = buildRequestURL(options);
  return {
    url,
    method: options.method.toUpperCase(),
    headers: normalizeRequestHeaders(options.headers),
    body: normalizeRequestBody(
      options.body,
      normalizeRequestHeaders(options.headers)['content-type']?.[0] ?? ''
    ),
  };
}

function buildRequestURL(options: Dispatcher.DispatchOptions): string {
  const path = options.path || '/';
  if (options.origin === undefined) {
    return path;
  }

  return new URL(path, String(options.origin)).toString();
}

function normalizeRequestHeaders(headers: Dispatcher.DispatchOptions['headers']): CapturedHeaders {
  const normalized: CapturedHeaders = {};
  if (headers == null) {
    return normalized;
  }

  if (Array.isArray(headers)) {
    for (let index = 0; index < headers.length; index += 2) {
      const name = headers[index];
      const value = headers[index + 1];
      if (typeof name !== 'string' || typeof value !== 'string') {
        continue;
      }
      normalized[name.toLowerCase()] = [value];
    }
    return normalized;
  }

  if (isHeaderEntryIterable(headers)) {
    for (const [name, value] of headers) {
      if (value === undefined) {
        continue;
      }
      normalized[name.toLowerCase()] = Array.isArray(value) ? value.map(String) : [String(value)];
    }
    return normalized;
  }

  for (const [name, value] of Object.entries(headers)) {
    if (value === undefined) {
      continue;
    }
    normalized[name.toLowerCase()] = Array.isArray(value) ? value.map(String) : [String(value)];
  }
  return normalized;
}

function normalizeIncomingHeaders(
  headers: Record<string, string | string[] | undefined>
): CapturedHeaders {
  const normalized: CapturedHeaders = {};
  for (const [name, value] of Object.entries(headers)) {
    if (value === undefined) {
      continue;
    }
    normalized[name.toLowerCase()] = Array.isArray(value) ? value.map(String) : [String(value)];
  }
  return normalized;
}

function normalizeRawHeaderPairs(headers: Buffer[]): CapturedHeaders {
  const normalized: CapturedHeaders = {};
  for (let index = 0; index < headers.length; index += 2) {
    const name = headers[index]?.toString('utf8').toLowerCase();
    const value = headers[index + 1]?.toString('utf8');
    if (!name || value === undefined) {
      continue;
    }
    normalized[name] ??= [];
    normalized[name].push(value);
  }
  return normalized;
}

function normalizeRequestBody(
  body: Dispatcher.DispatchOptions['body'],
  contentType: string
): unknown {
  if (body == null) {
    return null;
  }

  if (typeof body === 'string') {
    return decodePayload(Buffer.from(body, 'utf8'), contentType);
  }

  if (Buffer.isBuffer(body)) {
    return decodePayload(body, contentType);
  }

  if (body instanceof Uint8Array) {
    return decodePayload(Buffer.from(body), contentType);
  }

  if (body instanceof Readable) {
    return { type: 'stream' };
  }

  if (isWebReadableStream(body)) {
    return { type: 'stream' };
  }

  if (isFormData(body)) {
    return { type: 'form-data' };
  }

  return { type: typeof body };
}

function normalizeResponseBody(chunks: Buffer[], contentType: string): unknown {
  if (chunks.length === 0) {
    return null;
  }

  return decodePayload(Buffer.concat(chunks), contentType);
}

function decodePayload(payload: Buffer, contentType: string): unknown {
  if (payload.length === 0) {
    return null;
  }

  const mediaType = contentType.split(';', 1)[0].trim().toLowerCase();
  const decoded = payload.toString('utf8');

  if (mediaType === 'application/json') {
    try {
      return JSON.parse(decoded) as unknown;
    } catch {
      return { raw: decoded };
    }
  }

  return decoded;
}

function classifyService(url: URL | undefined): string {
  if (url === undefined) {
    return 'http';
  }

  const hostname = url.hostname.toLowerCase();
  if (isOpenAIHost(hostname)) {
    return 'openai';
  }
  if (hostname in KNOWN_SERVICE_HOSTS) {
    return KNOWN_SERVICE_HOSTS[hostname];
  }
  return hostname || 'http';
}

function classifyOperation({
  method,
  url,
  body,
}: {
  method: string;
  url: URL | undefined;
  body: unknown;
}): string {
  if (url === undefined) {
    return `${method} /`;
  }

  const openAIOperation = openAIOperationFromURL(method, url);
  if (openAIOperation !== undefined) {
    return openAIOperation;
  }

  return `${method} ${url.pathname}`;
}

function safeURL(rawURL: string): URL | undefined {
  try {
    return new URL(rawURL);
  } catch {
    return undefined;
  }
}

function isStreamingResponse(headers: CapturedHeaders): boolean {
  return (
    (headers['content-type']?.[0] ?? '').split(';', 1)[0].trim().toLowerCase() ===
    'text/event-stream'
  );
}

function isOpenAIStyleStreamingRequest({
  request,
  targetURL,
}: {
  request: CapturedRequest;
  targetURL: URL | undefined;
}): boolean {
  if (request.method !== 'POST') {
    return false;
  }

  const path = targetURL?.pathname ?? safeURL(request.url)?.pathname ?? '/';
  return path === '/v1/chat/completions' || path === '/v1/responses';
}

function timeoutLike(error: Error): boolean {
  const name = error.name.toLowerCase();
  const message = error.message.toLowerCase();
  return (
    name.includes('timeout') ||
    name.includes('abort') ||
    message.includes('timeout') ||
    message.includes('aborted')
  );
}

function abortReason(signal: AbortSignal | undefined): Error {
  const reason = signal?.reason;
  if (reason instanceof Error) {
    return reason;
  }
  if (reason !== undefined) {
    return new DOMException(String(reason), 'AbortError');
  }
  return new DOMException('This operation was aborted', 'AbortError');
}

function elapsedSince(startedAt: number): number {
  return Math.max(0, Date.now() - startedAt);
}

function asError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }

  return new Error(String(error));
}

function wrapReplayError(error: unknown, request: CapturedRequest): Error {
  if (error instanceof ReplayFailureError || error instanceof Error) {
    return new TypeError(
      `Stagehand replay blocked live request for ${request.method} ${request.url}`,
      { cause: error }
    );
  }

  return new TypeError(
    `Stagehand replay blocked live request for ${request.method} ${request.url}`,
    { cause: asError(error) }
  );
}

function isHeaderEntryIterable(
  headers: Dispatcher.DispatchOptions['headers']
): headers is Iterable<[string, string | string[] | undefined]> {
  return (
    headers !== null &&
    typeof headers === 'object' &&
    !Array.isArray(headers) &&
    Symbol.iterator in headers
  );
}

function isFormData(value: Dispatcher.DispatchOptions['body']): boolean {
  return typeof FormData !== 'undefined' && value instanceof FormData;
}

function isWebReadableStream(value: Dispatcher.DispatchOptions['body']): boolean {
  return typeof ReadableStream !== 'undefined' && value instanceof ReadableStream;
}
