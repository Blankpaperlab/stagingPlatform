import fs from 'node:fs';

export type ResponseOverride = {
  status?: number;
  body?: unknown;
  error?: string;
  latency_ms?: number;
};

export type InjectionProvenance = {
  rule_index: number;
  name?: string;
  tool?: string;
  service: string;
  operation: string;
  call_number: number;
  nth_call?: number;
  any_call?: boolean;
  probability?: number;
  library?: string;
  status?: number;
  error?: string;
  latency_ms?: number;
};

export type InjectionDecision = {
  override: ResponseOverride;
  provenance: InjectionProvenance;
};

type RawBundle = {
  rules?: RawRule[];
};

type RawRule = {
  name?: unknown;
  match?: {
    service?: unknown;
    operation?: unknown;
    tool?: unknown;
    nth_call?: unknown;
    any_call?: unknown;
    probability?: unknown;
  };
  inject?: {
    library?: unknown;
    status?: unknown;
    error?: unknown;
    latency_ms?: unknown;
    body?: unknown;
  };
};

type Rule = {
  name?: string;
  service: string;
  operation: string;
  tool?: string;
  nthCall: number;
  anyCall: boolean;
  probability?: number;
  library?: string;
  status: number;
  error?: string;
  latencyMs: number;
  body: unknown;
};

export class InjectionEngine {
  private readonly rules: Rule[];
  private readonly counts = new Map<string, number>();
  private readonly applied: InjectionProvenance[] = [];
  private rngState = 1;

  constructor(rules: Rule[]) {
    this.rules = rules;
  }

  evaluate({
    service,
    operation,
    tool,
  }: {
    service: string;
    operation: string;
    tool?: string;
  }): InjectionDecision | null {
    const normalizedTool = tool?.trim() ?? '';
    const normalizedService = normalizedTool === '' ? service.trim() : 'stagehand.tool';
    const normalizedOperation = normalizedTool === '' ? operation.trim() : normalizedTool;
    const key = `${normalizedService}\0${normalizedOperation}`;
    const callNumber = (this.counts.get(key) ?? 0) + 1;
    this.counts.set(key, callNumber);

    for (const [index, rule] of this.rules.entries()) {
      const ruleService = rule.tool === undefined ? rule.service : 'stagehand.tool';
      const ruleOperation = rule.tool === undefined ? rule.operation : rule.tool;
      if (ruleService !== normalizedService || ruleOperation !== normalizedOperation) {
        continue;
      }
      if (rule.nthCall > 0 && rule.nthCall !== callNumber) {
        continue;
      }
      if (rule.probability !== undefined && rule.probability <= 0) {
        continue;
      }
      if (
        rule.probability !== undefined &&
        rule.probability < 1 &&
        this.nextRandom() >= rule.probability
      ) {
        continue;
      }

      const provenance: InjectionProvenance = {
        rule_index: index,
        ...(rule.name === undefined ? {} : { name: rule.name }),
        ...(rule.tool === undefined ? {} : { tool: rule.tool }),
        service: normalizedService,
        operation: normalizedOperation,
        call_number: callNumber,
        ...(rule.nthCall > 0 ? { nth_call: rule.nthCall } : {}),
        ...(rule.anyCall ? { any_call: rule.anyCall } : {}),
        ...(rule.probability === undefined ? {} : { probability: rule.probability }),
        ...(rule.library === undefined ? {} : { library: rule.library }),
        ...(rule.status === 0 ? {} : { status: rule.status }),
        ...(rule.error === undefined ? {} : { error: rule.error }),
        ...(rule.latencyMs === 0 ? {} : { latency_ms: rule.latencyMs }),
      };
      this.applied.push(provenance);
      return {
        override: {
          ...(rule.status === 0 ? {} : { status: rule.status }),
          body: structuredClone(rule.body),
          ...(rule.error === undefined ? {} : { error: rule.error }),
          ...(rule.latencyMs === 0 ? {} : { latency_ms: rule.latencyMs }),
        },
        provenance,
      };
    }

    return null;
  }

  metadata(): Record<string, unknown> {
    if (this.applied.length === 0) {
      return {};
    }
    return {
      error_injection: {
        applied: this.applied.map((item) => ({ ...item })),
      },
    };
  }

  private nextRandom(): number {
    this.rngState = (Math.imul(1664525, this.rngState) + 1013904223) >>> 0;
    return this.rngState / 0x100000000;
  }
}

export function loadInjectionEngine(pathValue: string | undefined): InjectionEngine {
  if (pathValue === undefined || pathValue.trim() === '') {
    return new InjectionEngine([]);
  }

  const payload = JSON.parse(fs.readFileSync(pathValue, 'utf8')) as RawBundle;
  const rules = Array.isArray(payload.rules) ? payload.rules.map(normalizeRule) : [];
  return new InjectionEngine(rules);
}

function normalizeRule(raw: RawRule): Rule {
  const match = raw.match ?? {};
  const inject = raw.inject ?? {};
  return {
    name: optionalString(raw.name),
    service: stringValue(match.service).trim(),
    operation: stringValue(match.operation).trim(),
    tool: optionalString(match.tool),
    nthCall: integerValue(match.nth_call),
    anyCall: Boolean(match.any_call),
    probability: optionalNumber(match.probability),
    library: optionalString(inject.library),
    status: integerValue(inject.status),
    error: optionalString(inject.error),
    latencyMs: integerValue(inject.latency_ms),
    body: cloneValue(inject.body),
  };
}

function stringValue(value: unknown): string {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value);
}

function optionalString(value: unknown): string | undefined {
  const normalized = stringValue(value).trim();
  return normalized === '' ? undefined : normalized;
}

function integerValue(value: unknown): number {
  if (typeof value === 'number' && Number.isInteger(value)) {
    return value;
  }
  const parsed = Number.parseInt(String(value ?? '0'), 10);
  return Number.isInteger(parsed) ? parsed : 0;
}

function optionalNumber(value: unknown): number | undefined {
  if (value === null || value === undefined) {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function cloneValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => cloneValue(item));
  }
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, cloneValue(item)])
    );
  }
  return value;
}
