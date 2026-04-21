export { ARTIFACT_VERSION, SDK_VERSION } from './version.js';

export type StagehandMode = 'record' | 'replay' | 'passthrough';

export type InitOptions = {
  session: string;
  mode: StagehandMode;
  configPath?: string;
};

export function init(_options: InitOptions): never {
  throw new Error('Stagehand TypeScript SDK init will be implemented in Story G1.');
}
