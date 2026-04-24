import { randomUUID } from 'node:crypto';

export const DEFAULT_SCRUB_POLICY_VERSION = 'v0-unredacted';
export const DEFAULT_SESSION_SALT_ID = 'salt_pending';

export type CapturedHeaders = Record<string, string[]>;

export type CapturedRequest = {
  url: string;
  method: string;
  headers: CapturedHeaders;
  body?: unknown;
};

export type CapturedEventType =
  | 'request_sent'
  | 'response_received'
  | 'stream_chunk'
  | 'stream_end'
  | 'tool_call_start'
  | 'tool_call_end'
  | 'error'
  | 'timeout';

export type CapturedEvent = {
  sequence: number;
  t_ms: number;
  sim_t_ms: number;
  type: CapturedEventType;
  data?: Record<string, unknown>;
  nested_interaction_id?: string;
};

export type CapturedScrubReport = {
  scrub_policy_version: string;
  session_salt_id: string;
};

export type CapturedInteraction = {
  run_id: string;
  interaction_id: string;
  sequence: number;
  service: string;
  operation: string;
  protocol: string;
  streaming: boolean;
  request: CapturedRequest;
  events: CapturedEvent[];
  scrub_report: CapturedScrubReport;
  latency_ms?: number;
  fallback_tier?: string;
};

export type SuccessCaptureInput = {
  service: string;
  operation: string;
  protocol: string;
  request: CapturedRequest;
  response: {
    statusCode: number;
    statusText?: string;
    headers: CapturedHeaders;
    body?: unknown;
  };
  elapsedMs: number;
  streaming?: boolean;
};

export type FailureCaptureInput = {
  service: string;
  operation: string;
  protocol: string;
  request: CapturedRequest;
  elapsedMs: number;
  failureType: Extract<CapturedEventType, 'error' | 'timeout'>;
  errorClass?: string;
  message?: string;
};

export type OpenAIStreamCaptureInput = {
  service: string;
  operation: string;
  protocol: string;
  request: CapturedRequest;
  response: {
    statusCode: number;
    statusText?: string;
    headers: CapturedHeaders;
    rawBody: string;
  };
  elapsedMs: number;
};

export class CaptureBuffer {
  private readonly runId: string;
  private readonly scrubPolicyVersion: string;
  private readonly sessionSaltId: string;
  private nextSequence = 1;
  private readonly interactions: CapturedInteraction[] = [];

  constructor({
    runId,
    scrubPolicyVersion = DEFAULT_SCRUB_POLICY_VERSION,
    sessionSaltId = DEFAULT_SESSION_SALT_ID,
  }: {
    runId: string;
    scrubPolicyVersion?: string;
    sessionSaltId?: string;
  }) {
    this.runId = runId;
    this.scrubPolicyVersion = scrubPolicyVersion;
    this.sessionSaltId = sessionSaltId;
  }

  snapshot(): CapturedInteraction[] {
    return this.interactions
      .slice()
      .sort((left, right) => left.sequence - right.sequence)
      .map((interaction) => structuredClone(interaction));
  }

  clear(): void {
    this.interactions.length = 0;
  }

  recordReplayInteraction(
    interaction: CapturedInteraction,
    fallbackTier: string = 'exact'
  ): CapturedInteraction {
    return this.appendInteraction({
      service: interaction.service,
      operation: interaction.operation,
      protocol: interaction.protocol,
      request: structuredClone(interaction.request),
      streaming: interaction.streaming,
      elapsedMs: interaction.latency_ms ?? terminalEventLatency(interaction) ?? 0,
      events: structuredClone(interaction.events),
      fallbackTier,
    });
  }

  recordSuccess(input: SuccessCaptureInput): CapturedInteraction {
    return this.appendInteraction({
      service: input.service,
      operation: input.operation,
      protocol: input.protocol,
      request: input.request,
      streaming: input.streaming ?? false,
      elapsedMs: input.elapsedMs,
      events: [
        {
          sequence: 1,
          t_ms: 0,
          sim_t_ms: 0,
          type: 'request_sent',
        },
        {
          sequence: 2,
          t_ms: input.elapsedMs,
          sim_t_ms: input.elapsedMs,
          type: 'response_received',
          data: {
            status_code: input.response.statusCode,
            ...(input.response.statusText === undefined
              ? {}
              : { status_text: input.response.statusText }),
            headers: input.response.headers,
            body: input.response.body ?? null,
          },
        },
      ],
    });
  }

  recordFailure(input: FailureCaptureInput): CapturedInteraction {
    return this.appendInteraction({
      service: input.service,
      operation: input.operation,
      protocol: input.protocol,
      request: input.request,
      streaming: false,
      elapsedMs: input.elapsedMs,
      events: [
        {
          sequence: 1,
          t_ms: 0,
          sim_t_ms: 0,
          type: 'request_sent',
        },
        {
          sequence: 2,
          t_ms: input.elapsedMs,
          sim_t_ms: input.elapsedMs,
          type: input.failureType,
          data: {
            ...(input.errorClass === undefined ? {} : { error_class: input.errorClass }),
            ...(input.message === undefined ? {} : { message: input.message }),
          },
        },
      ],
    });
  }

  recordOpenAIStreamSuccess(input: OpenAIStreamCaptureInput): CapturedInteraction {
    return this.appendInteraction({
      service: input.service,
      operation: input.operation,
      protocol: input.protocol,
      request: input.request,
      streaming: true,
      elapsedMs: input.elapsedMs,
      events: [
        {
          sequence: 1,
          t_ms: 0,
          sim_t_ms: 0,
          type: 'request_sent',
        },
        {
          sequence: 2,
          t_ms: input.elapsedMs,
          sim_t_ms: input.elapsedMs,
          type: 'response_received',
          data: {
            status_code: input.response.statusCode,
            ...(input.response.statusText === undefined
              ? {}
              : { status_text: input.response.statusText }),
            headers: input.response.headers,
            body: { capture_status: 'stream' },
          },
        },
        ...buildOpenAIStreamEvents({
          rawBody: input.response.rawBody,
          initialElapsedMs: input.elapsedMs,
        }),
      ],
    });
  }

  private appendInteraction({
    service,
    operation,
    protocol,
    request,
    streaming,
    elapsedMs,
    events,
    fallbackTier,
  }: {
    service: string;
    operation: string;
    protocol: string;
    request: CapturedRequest;
    streaming: boolean;
    elapsedMs: number;
    events: CapturedEvent[];
    fallbackTier?: string;
  }): CapturedInteraction {
    const interaction: CapturedInteraction = {
      run_id: this.runId,
      interaction_id: `int_${randomUUID().replaceAll('-', '').slice(0, 12)}`,
      sequence: this.nextSequence,
      service,
      operation,
      protocol,
      streaming,
      request,
      events,
      scrub_report: {
        scrub_policy_version: this.scrubPolicyVersion,
        session_salt_id: this.sessionSaltId,
      },
      latency_ms: elapsedMs,
      ...(fallbackTier === undefined ? {} : { fallback_tier: fallbackTier }),
    };

    this.nextSequence += 1;
    this.interactions.push(interaction);
    return structuredClone(interaction);
  }
}

function terminalEventLatency(interaction: CapturedInteraction): number | undefined {
  const terminalEvent = interaction.events.at(-1);
  return terminalEvent?.t_ms;
}

function buildOpenAIStreamEvents({
  rawBody,
  initialElapsedMs,
}: {
  rawBody: string;
  initialElapsedMs: number;
}): CapturedEvent[] {
  const events: CapturedEvent[] = [];
  const blocks = rawBody.split('\n\n').filter((block) => block.length > 0);
  const openToolCalls = new Map<number, Record<string, unknown>>();
  let nextSequence = 3;
  let currentElapsedMs = initialElapsedMs;

  for (const block of blocks) {
    const chunk = `${block}\n\n`;
    const parsedPayload = parseOpenAISSEPayload(block);

    if (parsedPayload === '[DONE]') {
      for (const [index, toolInfo] of openToolCalls.entries()) {
        events.push({
          sequence: nextSequence,
          t_ms: currentElapsedMs,
          sim_t_ms: currentElapsedMs,
          type: 'tool_call_end',
          data: toolInfo,
          nested_interaction_id: `tool_call_${index}`,
        });
        nextSequence += 1;
      }
      openToolCalls.clear();

      events.push({
        sequence: nextSequence,
        t_ms: currentElapsedMs,
        sim_t_ms: currentElapsedMs,
        type: 'stream_end',
        data: { chunk },
      });
      nextSequence += 1;
      continue;
    }

    events.push({
      sequence: nextSequence,
      t_ms: currentElapsedMs,
      sim_t_ms: currentElapsedMs,
      type: 'stream_chunk',
      data: { chunk, payload: parsedPayload },
    });
    nextSequence += 1;

    let finishReason: string | undefined;
    if (isRecord(parsedPayload) && Array.isArray(parsedPayload.choices)) {
      for (const choice of parsedPayload.choices) {
        if (!isRecord(choice)) {
          continue;
        }

        const choiceFinishReason = asOptionalString(choice.finish_reason);
        if (choiceFinishReason !== undefined) {
          finishReason = choiceFinishReason;
        }

        const delta = isRecord(choice.delta) ? choice.delta : undefined;
        const toolCalls = Array.isArray(delta?.tool_calls) ? delta.tool_calls : [];
        for (const toolCall of toolCalls) {
          if (!isRecord(toolCall)) {
            continue;
          }

          const rawIndex = toolCall.index;
          const index =
            typeof rawIndex === 'number' && Number.isInteger(rawIndex)
              ? rawIndex
              : Number.parseInt(String(rawIndex ?? '0'), 10) || 0;
          const fn = isRecord(toolCall.function) ? toolCall.function : {};
          const toolInfo = {
            index,
            tool_call_id: asOptionalString(toolCall.id) ?? null,
            name: asOptionalString(fn.name) ?? null,
          };

          if (!openToolCalls.has(index)) {
            events.push({
              sequence: nextSequence,
              t_ms: currentElapsedMs,
              sim_t_ms: currentElapsedMs,
              type: 'tool_call_start',
              data: toolInfo,
              nested_interaction_id: `tool_call_${index}`,
            });
            nextSequence += 1;
          }

          openToolCalls.set(index, toolInfo);
        }
      }
    }

    if (finishReason === 'tool_calls') {
      for (const [index, toolInfo] of openToolCalls.entries()) {
        events.push({
          sequence: nextSequence,
          t_ms: currentElapsedMs,
          sim_t_ms: currentElapsedMs,
          type: 'tool_call_end',
          data: toolInfo,
          nested_interaction_id: `tool_call_${index}`,
        });
        nextSequence += 1;
      }
      openToolCalls.clear();
    }

    currentElapsedMs += 1;
  }

  if (!events.some((event) => event.type === 'stream_end')) {
    events.push({
      sequence: nextSequence,
      t_ms: currentElapsedMs,
      sim_t_ms: currentElapsedMs,
      type: 'stream_end',
      data: {
        chunk: '',
        finish_reason: 'stream_closed',
      },
    });
  }

  return events;
}

function parseOpenAISSEPayload(block: string): Record<string, unknown> | string {
  const dataLines = block
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim());

  if (dataLines.length === 0) {
    return { raw: block };
  }

  const payload = dataLines.join('\n');
  if (payload === '[DONE]') {
    return payload;
  }

  try {
    return JSON.parse(payload) as Record<string, unknown>;
  } catch {
    return { raw: payload };
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function asOptionalString(value: unknown): string | undefined {
  if (value === null || value === undefined) {
    return undefined;
  }

  return String(value);
}
