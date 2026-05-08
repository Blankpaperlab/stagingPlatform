import type { CapturedInteraction, CapturedRequest } from './capture.js';
import { mappedServiceName, type ServiceMapping } from './config.js';

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
  fallbackTier: string;
  fallbackReason?: string;
};

export class ExactReplayStore {
  private readonly entries = new Map<string, CapturedInteraction[]>();
  private readonly available: CapturedInteraction[] = [];
  private readonly usedInteractionIds = new Set<string>();
  private readonly serviceMappings: readonly ServiceMapping[];

  constructor({ serviceMappings = [] }: { serviceMappings?: readonly ServiceMapping[] } = {}) {
    this.serviceMappings = serviceMappings;
  }

  seed(interactions: Iterable<CapturedInteraction>): number {
    let count = 0;
    for (const interaction of interactions) {
      const mapping = this.serviceMappingForURL(interaction.request.url);
      const ignoredRequestPaths = mapping?.ignoreRequestPaths ?? [];
      const includeOpaqueBodyLength = this.serviceMappings.length > 0 && mapping === undefined;
      for (const key of requestKeysFromCapturedRequestForSeed(
        interaction.request,
        ignoredRequestPaths,
        includeOpaqueBodyLength
      )) {
        const queue = this.entries.get(key) ?? [];
        queue.push(structuredClone(interaction));
        this.entries.set(key, queue);
      }
      this.available.push(structuredClone(interaction));
      count += 1;
    }
    return count;
  }

  popMatch(request: CapturedRequest): ExactReplayMatch {
    const mapping = this.serviceMappingForURL(request.url);
    const ignoredRequestPaths = mapping?.ignoreRequestPaths ?? [];
    const includeOpaqueBodyLength = this.serviceMappings.length > 0 && mapping === undefined;
    for (const key of requestKeysFromCapturedRequest(
      request,
      ignoredRequestPaths,
      includeOpaqueBodyLength
    )) {
      const queue = this.entries.get(key);
      if (queue === undefined || queue.length === 0) {
        continue;
      }

      const interaction = queue.shift();
      if (interaction !== undefined) {
        if (queue.length === 0) {
          this.entries.delete(key);
        }

        return { interaction, fallbackTier: 'exact', fallbackReason: 'exact request match' };
      }

      if (queue.length === 0) {
        this.entries.delete(key);
      }
    }

    if (mapping !== undefined && mapping.allowedTiers.includes(1)) {
      const match = this.nearestMatch(request, mapping);
      if (match !== undefined) {
        this.usedInteractionIds.add(match.interaction.interaction_id);
        return {
          interaction: match.interaction,
          fallbackTier: 'nearest_neighbor',
          fallbackReason: nearestReason(match.score, match.reasons),
        };
      }
    }

    throw new ReplayMissError(request);
  }

  private serviceMappingForURL(rawURL: string): ServiceMapping | undefined {
    let url: URL;
    try {
      url = new URL(rawURL);
    } catch {
      return undefined;
    }
    const name = mappedServiceName(url, this.serviceMappings);
    return this.serviceMappings.find((mapping) => mapping.name === name);
  }

  private nearestMatch(
    request: CapturedRequest,
    mapping: ServiceMapping
  ): { interaction: CapturedInteraction; score: number; reasons: string[] } | undefined {
    let best: { interaction: CapturedInteraction; score: number; reasons: string[] } | undefined;
    for (const interaction of this.available) {
      if (this.usedInteractionIds.has(interaction.interaction_id)) {
        continue;
      }
      if (!mappingMatchesURL(mapping, interaction.request.url)) {
        continue;
      }
      if (!compatibleNearestCandidate(interaction, request)) {
        continue;
      }
      const { score, reasons } = nearestScore(interaction, request, mapping.ignoreRequestPaths);
      if (best === undefined || score > best.score) {
        best = { interaction, score, reasons };
      }
    }
    if (best === undefined || best.score < 0.35) {
      return undefined;
    }
    return best;
  }
}

export function requestKeyFromCapturedRequest(request: CapturedRequest): string {
  return requestKeysFromCapturedRequest(request)[0] ?? '';
}

function requestKeysFromCapturedRequest(
  request: CapturedRequest,
  ignoredRequestPaths: readonly string[] = [],
  includeOpaqueBodyLength = false
): string[] {
  return requestKeyCandidates({
    request,
    includeLegacyKey: true,
    allowHeaderSubsets: true,
    ignoredRequestPaths,
    includeOpaqueBodyLength,
  });
}

function requestKeysFromCapturedRequestForSeed(
  request: CapturedRequest,
  ignoredRequestPaths: readonly string[] = [],
  includeOpaqueBodyLength = false
): string[] {
  return requestKeyCandidates({
    request,
    includeLegacyKey: selectedReplayHeaders(request.headers).size === 0,
    allowHeaderSubsets: false,
    ignoredRequestPaths,
    includeOpaqueBodyLength,
  });
}

function mappingMatchesURL(mapping: ServiceMapping, rawURL: string): boolean {
  try {
    const url = new URL(rawURL);
    return (
      url.hostname.toLowerCase() === mapping.host && url.pathname.startsWith(mapping.pathPrefix)
    );
  } catch {
    return false;
  }
}

function compatibleNearestCandidate(
  candidate: CapturedInteraction,
  request: CapturedRequest
): boolean {
  if (candidate.request.method.toUpperCase() !== request.method.toUpperCase()) {
    return false;
  }
  try {
    const left = new URL(candidate.request.url);
    const right = new URL(request.url);
    return left.host.toLowerCase() === right.host.toLowerCase() && left.pathname === right.pathname;
  } catch {
    return false;
  }
}

function nearestScore(
  candidate: CapturedInteraction,
  request: CapturedRequest,
  ignoredRequestPaths: readonly string[]
): { score: number; reasons: string[] } {
  const normalizedIgnoredPaths = normalizeRequestIgnorePaths(ignoredRequestPaths);
  const candidateQuery = nearestQueryParts(candidate.request.url, normalizedIgnoredPaths);
  const requestQuery = nearestQueryParts(request.url, normalizedIgnoredPaths);
  const candidateBody = nearestBodyParts(candidate.request.body ?? null, normalizedIgnoredPaths);
  const requestBody = nearestBodyParts(request.body ?? null, normalizedIgnoredPaths);
  const candidateHeaders = nearestHeaderParts(candidate.request.headers, normalizedIgnoredPaths);
  const requestHeaders = nearestHeaderParts(request.headers, normalizedIgnoredPaths);

  const queryScore = setSimilarity(candidateQuery, requestQuery);
  const bodyScore = setSimilarity(candidateBody, requestBody);
  const headerScore = setSimilarity(candidateHeaders, requestHeaders);
  let score = 0.5 * bodyScore + 0.35 * queryScore + 0.15 * headerScore;
  if (candidateBody.length === 0 && requestBody.length === 0) {
    score += 0.25;
  }
  if (candidateQuery.length === 0 && requestQuery.length === 0) {
    score += 0.15;
  }
  return {
    score: Math.min(score, 1),
    reasons: nearestReasons({ queryScore, bodyScore, headerScore }),
  };
}

function nearestQueryParts(rawURL: string, ignoredRequestPaths: readonly string[]): string[] {
  try {
    const url = new URL(rawURL);
    const ignoredQueryKeys = new Set(
      ignoredRequestPaths
        .filter((path) => path.startsWith('query.') && path.length > 'query.'.length)
        .map((path) => path.slice('query.'.length).toLowerCase())
    );
    return Array.from(url.searchParams.entries())
      .filter(
        ([key, value]) =>
          !ignoredQueryKeys.has(key.toLowerCase()) && !mutableRequestField(key, value)
      )
      .map(([key, value]) => `${key.toLowerCase()}=${nearestValueSignature(value)}`)
      .sort();
  } catch {
    return [];
  }
}

function nearestBodyParts(value: unknown, ignoredRequestPaths: readonly string[]): string[] {
  if (ignoredRequestPaths.includes('body')) {
    return [];
  }
  const body = removeIgnoredBodyPaths(value, ignoredRequestPaths);
  const parts: string[] = [];
  flattenNearestBody('', body, parts);
  return parts.sort();
}

function flattenNearestBody(path: string, value: unknown, parts: string[]): void {
  if (Array.isArray(value)) {
    value.forEach((item, index) => flattenNearestBody(`${path}[${index}]`, item, parts));
    return;
  }
  if (value !== null && typeof value === 'object') {
    for (const key of Object.keys(value as Record<string, unknown>).sort()) {
      const childPath = path.length === 0 ? key : `${path}.${key}`;
      flattenNearestBody(childPath, (value as Record<string, unknown>)[key], parts);
    }
    return;
  }
  const fieldName = path.split('.').at(-1)?.split('[')[0] ?? '';
  if (mutableRequestField(fieldName, value)) {
    return;
  }
  parts.push(`${path}=${nearestValueSignature(value)}`);
}

function nearestHeaderParts(
  headers: CapturedRequest['headers'] | undefined,
  ignoredRequestPaths: readonly string[]
): string[] {
  return Array.from(
    removeIgnoredHeaderPaths(selectedReplayHeaders(headers), ignoredRequestPaths).entries()
  )
    .filter(([key]) => !mutableHeader(key))
    .flatMap(([key, values]) => values.map((value) => `${key.toLowerCase()}=${value.trim()}`))
    .sort();
}

function mutableRequestField(name: string, value: unknown): boolean {
  const normalized = name.toLowerCase().replaceAll('-', '_');
  if (
    [
      'request_id',
      'trace_id',
      'correlation_id',
      'idempotency_key',
      'cursor',
      'next_cursor',
      'page_token',
      'pagination_token',
      'starting_after',
      'ending_before',
      'created_at',
      'updated_at',
      'generated_at',
      'timestamp',
      'time',
    ].includes(normalized)
  ) {
    return true;
  }
  return typeof value === 'string' && timestampLike(value);
}

function mutableHeader(name: string): boolean {
  return ['x-request-id', 'x-trace-id', 'x-correlation-id', 'idempotency-key', 'date'].includes(
    name.toLowerCase()
  );
}

function timestampLike(value: string): boolean {
  return (
    /^\d{4}-\d{2}-\d{2}(?:[tT ][0-9:.+-]+(?:Z)?)?$/.test(value) ||
    /^\d{10}(?:\.\d+)?$/.test(value) ||
    /^\d{13}$/.test(value)
  );
}

function nearestValueSignature(value: unknown): string {
  return typeof value === 'string' ? value : stableStringify(value);
}

function setSimilarity(left: string[], right: string[]): number {
  if (left.length === 0 && right.length === 0) {
    return 1;
  }
  if (left.length === 0 || right.length === 0) {
    return 0;
  }
  const leftSet = new Set(left);
  const rightSet = new Set(right);
  const union = new Set([...leftSet, ...rightSet]);
  let intersection = 0;
  for (const value of leftSet) {
    if (rightSet.has(value)) {
      intersection += 1;
    }
  }
  return union.size === 0 ? 1 : intersection / union.size;
}

function nearestReasons({
  queryScore,
  bodyScore,
  headerScore,
}: {
  queryScore: number;
  bodyScore: number;
  headerScore: number;
}): string[] {
  const reasons: string[] = [];
  if (bodyScore < 1) {
    reasons.push('body differed only within nearest-neighbor tolerance');
  }
  if (queryScore < 1) {
    reasons.push('query differed only within nearest-neighbor tolerance');
  }
  if (headerScore < 1) {
    reasons.push('headers differed only within nearest-neighbor tolerance');
  }
  return reasons.length > 0 ? reasons : ['request differed only by mutable fields'];
}

function nearestReason(score: number, reasons: string[]): string {
  return `nearest request match score=${score.toFixed(2)}; ${reasons.join('; ')}`;
}

function requestKeyCandidates({
  request,
  includeLegacyKey,
  allowHeaderSubsets,
  ignoredRequestPaths,
  includeOpaqueBodyLength,
}: {
  request: CapturedRequest;
  includeLegacyKey: boolean;
  allowHeaderSubsets: boolean;
  ignoredRequestPaths: readonly string[];
  includeOpaqueBodyLength: boolean;
}): string[] {
  const normalizedIgnoredPaths = normalizeRequestIgnorePaths(ignoredRequestPaths);
  const headers = removeIgnoredHeaderPaths(
    selectedReplayHeaders(request.headers),
    normalizedIgnoredPaths
  );
  if (includeLegacyKey && headers.size === 0) {
    return [
      requestKeyPayload({
        request,
        includeHeaders: false,
        ignoredRequestPaths: normalizedIgnoredPaths,
        includeOpaqueBodyLength,
      }),
    ];
  }

  const keys: string[] = [];
  const headerCandidates = allowHeaderSubsets ? headerSubsets(headers) : [headers];
  for (const headerSubset of headerCandidates) {
    const key = requestKeyPayload({
      request,
      headers: headerSubset,
      ignoredRequestPaths: normalizedIgnoredPaths,
      includeOpaqueBodyLength,
    });
    if (!keys.includes(key)) {
      keys.push(key);
    }
  }
  if (includeLegacyKey) {
    const legacy = requestKeyPayload({
      request,
      includeHeaders: false,
      ignoredRequestPaths: normalizedIgnoredPaths,
      includeOpaqueBodyLength,
    });
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
  ignoredRequestPaths = [],
  includeOpaqueBodyLength = false,
}: {
  request: CapturedRequest;
  includeHeaders?: boolean;
  headers?: Map<string, string[]>;
  ignoredRequestPaths?: readonly string[];
  includeOpaqueBodyLength?: boolean;
}): string {
  const body = requestKeyBody(request, ignoredRequestPaths, includeOpaqueBodyLength);
  const payload: Record<string, unknown> = {
    method: request.method.toUpperCase(),
    url: replayKeyURL(request.url, ignoredRequestPaths),
    body,
  };
  if (includeHeaders !== false) {
    payload.headers = Object.fromEntries(headers ?? selectedReplayHeaders(request.headers));
  }
  return stableStringify({
    ...payload,
  });
}

function requestKeyBody(
  request: CapturedRequest,
  ignoredRequestPaths: readonly string[] = [],
  includeOpaqueBodyLength = false
): unknown {
  const body = removeIgnoredBodyPaths(request.body ?? null, ignoredRequestPaths);
  if (
    includeOpaqueBodyLength &&
    isOpaqueBodySignature(body) &&
    !ignoredRequestPaths.some((path) => path === 'body' || path.startsWith('body.'))
  ) {
    const contentLength = request.headers['content-length']?.[0];
    return contentLength === undefined ? body : { ...body, content_length: contentLength };
  }
  return body;
}

function selectedReplayHeaders(
  headers: CapturedRequest['headers'] | undefined
): Map<string, string[]> {
  const selected = new Map<string, string[]>();
  if (headers === undefined) {
    return selected;
  }

  for (const name of REPLAY_KEY_HEADERS) {
    const values = headers[name];
    if (values === undefined) {
      continue;
    }
    const normalized = values
      .map((value) => String(value).trim())
      .filter((value) => value.length > 0);
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

function replayKeyURL(rawURL: string, ignoredRequestPaths: readonly string[] = []): string {
  try {
    const url = new URL(rawURL);
    const ignoredQueryKeys = new Set(
      normalizeRequestIgnorePaths(ignoredRequestPaths)
        .filter((path) => path.startsWith('query.') && path.length > 'query.'.length)
        .map((path) => path.slice('query.'.length).toLowerCase())
    );
    const pairs = Array.from(url.searchParams.entries())
      .filter(([key]) => !ignoredQueryKeys.has(key.toLowerCase()))
      .sort(([leftKey, leftValue], [rightKey, rightValue]) => {
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

function normalizeRequestIgnorePaths(paths: readonly string[]): string[] {
  const normalized: string[] = [];
  for (const rawPath of paths) {
    let path = String(rawPath).trim();
    if (path.startsWith('$.')) {
      path = path.slice(2);
    }
    if (path.startsWith('.')) {
      path = path.slice(1);
    }
    if (path.startsWith('request.')) {
      path = path.slice('request.'.length);
    }
    if (path.length > 0 && !normalized.includes(path)) {
      normalized.push(path);
    }
  }
  return normalized;
}

function removeIgnoredHeaderPaths(
  headers: Map<string, string[]>,
  ignoredRequestPaths: readonly string[]
): Map<string, string[]> {
  if (ignoredRequestPaths.includes('headers')) {
    return new Map();
  }
  const ignored = new Set(
    ignoredRequestPaths
      .filter((path) => path.startsWith('headers.') && path.length > 'headers.'.length)
      .map((path) => path.slice('headers.'.length).toLowerCase())
  );
  return new Map(Array.from(headers.entries()).filter(([key]) => !ignored.has(key.toLowerCase())));
}

function removeIgnoredBodyPaths(value: unknown, ignoredRequestPaths: readonly string[]): unknown {
  if (ignoredRequestPaths.includes('body')) {
    return null;
  }
  const bodyPaths = ignoredRequestPaths
    .filter((path) => path.startsWith('body.') && path.length > 'body.'.length)
    .map((path) => path.slice('body.'.length));
  if (bodyPaths.length === 0) {
    return value;
  }
  if (isOpaqueBodySignature(value)) {
    return { type: value.type };
  }
  const cloned = cloneReplayValue(value);
  for (const path of bodyPaths) {
    removePath(cloned, splitIgnorePath(path));
  }
  return cloned;
}

function isOpaqueBodySignature(value: unknown): value is { type: string } {
  return (
    value !== null &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    typeof (value as Record<string, unknown>).type === 'string' &&
    Object.keys(value as Record<string, unknown>).every((key) =>
      ['type', 'content_length'].includes(key)
    )
  );
}

function cloneReplayValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => cloneReplayValue(item));
  }
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, item]) => [
        key,
        cloneReplayValue(item),
      ])
    );
  }
  return value;
}

function removePath(value: unknown, parts: string[]): void {
  if (parts.length === 0) {
    return;
  }
  if (Array.isArray(value)) {
    const [segment, ...rest] = parts;
    if (segment === '*') {
      value.forEach((item) => removePath(item, rest));
      return;
    }
    const index = Number.parseInt(segment, 10);
    if (String(index) === segment && index >= 0 && index < value.length) {
      removePath(value[index], rest);
      return;
    }
    value.forEach((item) => removePath(item, parts));
    return;
  }
  if (value === null || typeof value !== 'object') {
    return;
  }
  const record = value as Record<string, unknown>;
  const [segment, ...rest] = parts;
  if (rest.length === 0) {
    delete record[segment];
    return;
  }
  removePath(record[segment], rest);
}

function splitIgnorePath(path: string): string[] {
  const parts: string[] = [];
  for (let raw of path.split('.')) {
    while (raw.length > 0) {
      const bracket = raw.indexOf('[');
      if (bracket < 0) {
        parts.push(raw);
        break;
      }
      if (bracket > 0) {
        parts.push(raw.slice(0, bracket));
      }
      const closeBracket = raw.indexOf(']', bracket);
      if (closeBracket < 0) {
        parts.push(raw.slice(bracket));
        break;
      }
      const selector = raw.slice(bracket + 1, closeBracket);
      parts.push(selector === '*' ? '*' : selector);
      raw = raw.slice(closeBracket + 1);
    }
  }
  return parts.filter((part) => part.length > 0);
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
