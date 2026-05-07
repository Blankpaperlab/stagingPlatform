import type { CapturedInteraction, CapturedRequest } from './capture.js';

const REPLAY_KEY_HEADERS = ['accept', 'content-type'] as const;

export class ReplayMissError extends Error {
  constructor(request: CapturedRequest) {
    super(`no exact replay match for request ${request.method} ${request.url}`);
    this.name = 'ReplayMissError';
  }
}

export class ReplayFailureError extends Error {
  readonly interaction: CapturedInteraction;
  readonly terminalEventType: string;
  readonly errorClass?: string;
  readonly detail?: string;

  constructor({
    interaction,
    terminalEventType,
    errorClass,
    detail,
  }: {
    interaction: CapturedInteraction;
    terminalEventType: string;
    errorClass?: string;
    detail?: string;
  }) {
    const parts = ['matched exact replay interaction ended with', terminalEventType];
    if (errorClass !== undefined) {
      parts.push(errorClass);
    }
    if (detail !== undefined && detail.length > 0) {
      parts.push(`(${detail})`);
    }

    super(parts.join(' '));
    this.name = 'ReplayFailureError';
    this.interaction = interaction;
    this.terminalEventType = terminalEventType;
    this.errorClass = errorClass;
    this.detail = detail;
  }
}

export type ExactReplayMatch = {
  interaction: CapturedInteraction;
};

export class ExactReplayStore {
  private readonly entries = new Map<string, CapturedInteraction[]>();

  seed(interactions: Iterable<CapturedInteraction>): number {
    let count = 0;
    for (const interaction of interactions) {
      for (const key of requestKeysFromCapturedRequestForSeed(interaction.request)) {
        const queue = this.entries.get(key) ?? [];
        queue.push(structuredClone(interaction));
        this.entries.set(key, queue);
      }
      count += 1;
    }
    return count;
  }

  popMatch(request: CapturedRequest): ExactReplayMatch {
    for (const key of requestKeysFromCapturedRequest(request)) {
      const queue = this.entries.get(key);
      if (queue === undefined || queue.length === 0) {
        continue;
      }

      const interaction = queue.shift();
      if (interaction === undefined) {
        throw new ReplayMissError(request);
      }

      if (queue.length === 0) {
        this.entries.delete(key);
      }

      return { interaction };
    }

    throw new ReplayMissError(request);
  }
}

export function requestKeyFromCapturedRequest(request: CapturedRequest): string {
  return requestKeysFromCapturedRequest(request)[0] ?? '';
}

function requestKeysFromCapturedRequest(request: CapturedRequest): string[] {
  return requestKeyCandidates({
    request,
    includeLegacyKey: true,
    allowHeaderSubsets: true,
  });
}

function requestKeysFromCapturedRequestForSeed(request: CapturedRequest): string[] {
  return requestKeyCandidates({
    request,
    includeLegacyKey: selectedReplayHeaders(request.headers).size === 0,
    allowHeaderSubsets: false,
  });
}

function requestKeyCandidates({
  request,
  includeLegacyKey,
  allowHeaderSubsets,
}: {
  request: CapturedRequest;
  includeLegacyKey: boolean;
  allowHeaderSubsets: boolean;
}): string[] {
  const headers = selectedReplayHeaders(request.headers);
  if (includeLegacyKey && headers.size === 0) {
    return [requestKeyPayload({ request, includeHeaders: false })];
  }

  const keys: string[] = [];
  const headerCandidates = allowHeaderSubsets ? headerSubsets(headers) : [headers];
  for (const headerSubset of headerCandidates) {
    const key = requestKeyPayload({ request, headers: headerSubset });
    if (!keys.includes(key)) {
      keys.push(key);
    }
  }
  if (includeLegacyKey) {
    const legacy = requestKeyPayload({ request, includeHeaders: false });
    if (!keys.includes(legacy)) {
      keys.push(legacy);
    }
  }
  return keys;
}

function requestKeyPayload({
  request,
  includeHeaders,
  headers,
}: {
  request: CapturedRequest;
  includeHeaders?: boolean;
  headers?: Map<string, string[]>;
}): string {
  const payload: Record<string, unknown> = {
    method: request.method.toUpperCase(),
    url: replayKeyURL(request.url),
    body: request.body ?? null,
  };
  if (includeHeaders !== false) {
    payload.headers = Object.fromEntries(headers ?? selectedReplayHeaders(request.headers));
  }
  return stableStringify({
    ...payload,
  });
}

function selectedReplayHeaders(headers: CapturedRequest['headers'] | undefined): Map<string, string[]> {
  const selected = new Map<string, string[]>();
  if (headers === undefined) {
    return selected;
  }

  for (const name of REPLAY_KEY_HEADERS) {
    const values = headers[name];
    if (values === undefined) {
      continue;
    }
    const normalized = values.map((value) => String(value).trim()).filter((value) => value.length > 0);
    if (normalized.length > 0) {
      selected.set(name, normalized.sort());
    }
  }

  return selected;
}

function headerSubsets(headers: Map<string, string[]>): Array<Map<string, string[]>> {
  if (headers.size === 0) {
    return [new Map()];
  }

  const names = Array.from(headers.keys()).sort();
  const subsets: Array<Map<string, string[]>> = [];
  for (let mask = (1 << names.length) - 1; mask > 0; mask -= 1) {
    const subset = new Map<string, string[]>();
    names.forEach((name, index) => {
      if ((mask & (1 << index)) !== 0) {
        const value = headers.get(name);
        if (value !== undefined) {
          subset.set(name, value);
        }
      }
    });
    subsets.push(subset);
  }
  return subsets;
}

function replayKeyURL(rawURL: string): string {
  try {
    const url = new URL(rawURL);
    const pairs = Array.from(url.searchParams.entries()).sort(([leftKey, leftValue], [rightKey, rightValue]) => {
      const keyOrder = leftKey.localeCompare(rightKey);
      if (keyOrder !== 0) {
        return keyOrder;
      }
      return leftValue.localeCompare(rightValue);
    });
    url.search = '';
    for (const [key, value] of pairs) {
      url.searchParams.append(key, value);
    }
    url.hash = '';
    url.protocol = url.protocol.toLowerCase();
    url.hostname = url.hostname.toLowerCase();
    return url.toString();
  } catch {
    return rawURL.trim();
  }
}

function stableStringify(value: unknown): string {
  return JSON.stringify(normalizeForStableStringify(value));
}

function normalizeForStableStringify(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => normalizeForStableStringify(item));
  }

  if (value !== null && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).sort(([left], [right]) =>
      left.localeCompare(right)
    );

    return Object.fromEntries(
      entries.map(([key, item]) => [key, normalizeForStableStringify(item)])
    );
  }

  return value;
}
