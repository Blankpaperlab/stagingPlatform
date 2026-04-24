import fs from 'node:fs';
import path from 'node:path';
import { randomUUID } from 'node:crypto';

import {
  CaptureBuffer,
  DEFAULT_SCRUB_POLICY_VERSION,
  DEFAULT_SESSION_SALT_ID,
  type CapturedInteraction,
} from './capture.js';
import { ExactReplayStore } from './replay.js';
import { installRequestInterception, resetRequestInterceptionForTests } from './interception.js';
import { ARTIFACT_VERSION, SDK_VERSION } from './version.js';

export { ENV_OPENAI_HOSTS } from './providers.js';

export const DEFAULT_CONFIG_FILENAME = 'stagehand.yml';
export const ENV_SESSION = 'STAGEHAND_SESSION';
export const ENV_MODE = 'STAGEHAND_MODE';
export const ENV_CONFIG_PATH = 'STAGEHAND_CONFIG_PATH';
export const ENV_REPLAY_INPUT = 'STAGEHAND_REPLAY_INPUT';

export const VALID_MODES = ['record', 'replay', 'passthrough'] as const;

export type StagehandMode = (typeof VALID_MODES)[number];

export type InitOptions = {
  session: string;
  mode: StagehandMode;
  configPath?: string;
};

export type RecorderMetadata = {
  session: string;
  mode: StagehandMode;
  run_id: string;
  config_path: string | null;
  sdk_version: string;
  artifact_version: string;
  initialized_at: string;
};

export type RuntimeMetadata = {
  session: string;
  mode: StagehandMode;
  runId: string;
  configPath?: string;
  sdkVersion: string;
  artifactVersion: string;
  initializedAt: Date;
};

export class StagehandError extends Error {
  constructor(message: string) {
    super(message);
    this.name = new.target.name;
  }
}

export class AlreadyInitializedError extends StagehandError {}

export class InvalidModeError extends StagehandError {}

export class NotInitializedError extends StagehandError {}

export class StagehandRuntime {
  readonly metadata: RuntimeMetadata;
  private readonly captureBuffer: CaptureBuffer;
  private readonly replayStore: ExactReplayStore;

  constructor(
    metadata: RuntimeMetadata,
    captureBuffer: CaptureBuffer,
    replayStore: ExactReplayStore
  ) {
    this.metadata = metadata;
    this.captureBuffer = captureBuffer;
    this.replayStore = replayStore;
  }

  get session(): string {
    return this.metadata.session;
  }

  get mode(): StagehandMode {
    return this.metadata.mode;
  }

  get runId(): string {
    return this.metadata.runId;
  }

  get configPath(): string | undefined {
    return this.metadata.configPath;
  }

  recorderMetadata(): RecorderMetadata {
    return {
      session: this.metadata.session,
      mode: this.metadata.mode,
      run_id: this.metadata.runId,
      config_path: this.metadata.configPath ?? null,
      sdk_version: this.metadata.sdkVersion,
      artifact_version: this.metadata.artifactVersion,
      initialized_at: this.metadata.initializedAt.toISOString(),
    };
  }

  snapshotCapturedInteractions(): CapturedInteraction[] {
    return this.captureBuffer.snapshot();
  }

  clearCapturedInteractions(): void {
    this.captureBuffer.clear();
  }

  seedReplayInteractions(
    interactions: Iterable<CapturedInteraction | Record<string, unknown>>
  ): number {
    const normalized: CapturedInteraction[] = [];
    for (const interaction of interactions) {
      normalized.push(structuredClone(interaction as CapturedInteraction));
    }
    return this.replayStore.seed(normalized);
  }
}

let currentRuntime: StagehandRuntime | undefined;

export function init(options: InitOptions): StagehandRuntime {
  if (currentRuntime !== undefined) {
    throw new AlreadyInitializedError(
      `Stagehand is already initialized for session ${JSON.stringify(currentRuntime.session)} in mode ${JSON.stringify(currentRuntime.mode)}.`
    );
  }

  const session = normalizeSession(options.session);
  const mode = normalizeMode(options.mode);
  const configPath = resolveConfigPath(options.configPath);
  const runId = generateRunId();
  const captureBuffer = new CaptureBuffer({
    runId,
    scrubPolicyVersion: DEFAULT_SCRUB_POLICY_VERSION,
    sessionSaltId: DEFAULT_SESSION_SALT_ID,
  });
  const replayStore = new ExactReplayStore();

  const runtime = new StagehandRuntime(
    {
      session,
      mode,
      runId,
      configPath,
      sdkVersion: SDK_VERSION,
      artifactVersion: ARTIFACT_VERSION,
      initializedAt: new Date(),
    },
    captureBuffer,
    replayStore
  );

  if (mode !== 'passthrough') {
    installRequestInterception({
      mode,
      captureBuffer,
      replayStore,
    });
  }

  configureEnvIntegrations(runtime);

  currentRuntime = runtime;
  return runtime;
}

export function initFromEnv(): StagehandRuntime {
  const session = process.env[ENV_SESSION];
  const mode = process.env[ENV_MODE];
  const configPath = process.env[ENV_CONFIG_PATH];

  if (session === undefined || mode === undefined) {
    throw new Error(`${ENV_SESSION} and ${ENV_MODE} must be set for initFromEnv().`);
  }

  return init({
    session,
    mode: mode as StagehandMode,
    configPath,
  });
}

export function getRuntime(): StagehandRuntime {
  if (currentRuntime === undefined) {
    throw new NotInitializedError('Stagehand has not been initialized in this process.');
  }

  return currentRuntime;
}

export function isInitialized(): boolean {
  return currentRuntime !== undefined;
}

export function seedReplayInteractions(
  interactions: Iterable<CapturedInteraction | Record<string, unknown>>
): number {
  return getRuntime().seedReplayInteractions(interactions);
}

export function _resetForTests(): void {
  resetRequestInterceptionForTests();
  currentRuntime = undefined;
}

function normalizeSession(session: string): string {
  if (typeof session !== 'string') {
    throw new TypeError('session must be a string');
  }

  const normalized = session.trim();
  if (normalized.length === 0) {
    throw new Error('session must be a non-empty string');
  }

  return normalized;
}

function normalizeMode(mode: string): StagehandMode {
  if (typeof mode !== 'string') {
    throw new TypeError('mode must be a string');
  }

  const normalized = mode.trim().toLowerCase();
  if (!VALID_MODES.includes(normalized as StagehandMode)) {
    const allowed = VALID_MODES.map((value) => JSON.stringify(value)).join(', ');
    throw new InvalidModeError(`mode must be one of ${allowed}`);
  }

  return normalized as StagehandMode;
}

function resolveConfigPath(configPath: string | undefined): string | undefined {
  if (configPath === undefined) {
    const defaultPath = path.resolve(process.cwd(), DEFAULT_CONFIG_FILENAME);
    if (!isRegularFile(defaultPath)) {
      return undefined;
    }

    return defaultPath;
  }

  const resolvedPath = path.resolve(configPath);
  if (!isRegularFile(resolvedPath)) {
    throw new Error(`Stagehand config file not found: ${resolvedPath}`);
  }

  return resolvedPath;
}

function isRegularFile(filePath: string): boolean {
  if (!fs.existsSync(filePath)) {
    return false;
  }

  return fs.statSync(filePath).isFile();
}

function generateRunId(): string {
  return `run_${randomUUID().replaceAll('-', '').slice(0, 12)}`;
}

function configureEnvIntegrations(runtime: StagehandRuntime): void {
  const replayInput = process.env[ENV_REPLAY_INPUT];
  if (runtime.mode === 'replay' && replayInput !== undefined) {
    runtime.seedReplayInteractions(loadReplayBundle(replayInput));
  }
}

function loadReplayBundle(pathValue: string): Array<Record<string, unknown>> {
  const raw = fs.readFileSync(pathValue, 'utf8');
  const payload = JSON.parse(raw) as unknown;

  if (Array.isArray(payload)) {
    return payload.map((item) => ({ ...(item as Record<string, unknown>) }));
  }

  if (payload !== null && typeof payload === 'object') {
    const interactions = (payload as { interactions?: unknown }).interactions;
    if (Array.isArray(interactions)) {
      return interactions.map((item) => ({ ...(item as Record<string, unknown>) }));
    }
  }

  throw new Error(`replay bundle at ${pathValue} does not contain an interactions list`);
}
