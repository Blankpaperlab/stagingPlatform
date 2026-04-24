import type { CapturedInteraction, CapturedRequest } from './capture.js';

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
      const key = requestKeyFromCapturedRequest(interaction.request);
      const queue = this.entries.get(key) ?? [];
      queue.push(structuredClone(interaction));
      this.entries.set(key, queue);
      count += 1;
    }
    return count;
  }

  popMatch(request: CapturedRequest): ExactReplayMatch {
    const key = requestKeyFromCapturedRequest(request);
    const queue = this.entries.get(key);
    if (queue === undefined || queue.length === 0) {
      throw new ReplayMissError(request);
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
}

export function requestKeyFromCapturedRequest(request: CapturedRequest): string {
  return stableStringify({
    method: request.method.toUpperCase(),
    url: request.url,
    body: request.body ?? null,
  });
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
