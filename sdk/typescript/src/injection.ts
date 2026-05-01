import fs from 'node:fs';

export type ResponseOverride = {
  status: number;
  body?: Record<string, unknown>;
};

export type InjectionProvenance = {
  rule_index: number;
  name?: string;
  service: string;
  operation: string;
  call_number: number;
  nth_call?: number;
  any_call?: boolean;
  probability?: number;
  library?: string;
  status: number;
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
    nth_call?: unknown;
    any_call?: unknown;
    probability?: unknown;
  };
  inject?: {
    library?: unknown;
    status?: unknown;
    body?: unknown;
  };
};

type Rule = {
  name?: string;
  service: string;
  operation: string;
  nthCall: number;
  anyCall: boolean;
  probability?: number;
  library?: string;
  status: number;
  body: Record<string, unknown>;
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
  }: {
    service: string;
    operation: string;
  }): InjectionDecision | null {
    const normalizedService = service.trim();
    const normalizedOperation = operation.trim();
    const key = `${normalizedService}\0${normalizedOperation}`;
    const callNumber = (this.counts.get(key) ?? 0) + 1;
    this.counts.set(key, callNumber);

    for (const [index, rule] of this.rules.entries()) {
      if (rule.service !== normalizedService || rule.operation !== normalizedOperation) {
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
        service: normalizedService,
        operation: normalizedOperation,
        call_number: callNumber,
        ...(rule.nthCall > 0 ? { nth_call: rule.nthCall } : {}),
        ...(rule.anyCall ? { any_call: rule.anyCall } : {}),
        ...(rule.probability === undefined ? {} : { probability: rule.probability }),
        ...(rule.library === undefined ? {} : { library: rule.library }),
        status: rule.status,
      };
      this.applied.push(provenance);
      return {
        override: {
          status: rule.status,
          body: structuredClone(rule.body),
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
    nthCall: integerValue(match.nth_call),
    anyCall: Boolean(match.any_call),
    probability: optionalNumber(match.probability),
    library: optionalString(inject.library),
    status: integerValue(inject.status),
    body: recordValue(inject.body),
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

function recordValue(value: unknown): Record<string, unknown> {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    return { ...(value as Record<string, unknown>) };
  }
  return {};
}
