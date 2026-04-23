import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

import {
  AlreadyInitializedError,
  InvalidModeError,
  NotInitializedError,
  getRuntime,
  init,
  initFromEnv,
  isInitialized,
} from './index.js';
import { _resetForTests } from './runtime.js';

function withCleanRuntime(fn: () => void | Promise<void>): () => void | Promise<void> {
  return async () => {
    _resetForTests();
    try {
      await fn();
    } finally {
      _resetForTests();
      delete process.env.STAGEHAND_SESSION;
      delete process.env.STAGEHAND_MODE;
      delete process.env.STAGEHAND_CONFIG_PATH;
    }
  };
}

test(
  'init returns runtime metadata for valid bootstrap inputs',
  withCleanRuntime(() => {
    const runtime = init({ session: 'demo', mode: 'record' });

    assert.equal(runtime.session, 'demo');
    assert.equal(runtime.mode, 'record');
    assert.match(runtime.runId, /^run_[0-9a-f]{12}$/);
    assert.equal(runtime.configPath, undefined);
    assert.equal(isInitialized(), true);
    assert.equal(getRuntime(), runtime);
    assert.equal(runtime.recorderMetadata().session, 'demo');
  })
);

test(
  'init auto-discovers stagehand config from cwd',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-config-'));
    const oldCwd = process.cwd();
    try {
      const configPath = path.join(tempDir, 'stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');
      process.chdir(tempDir);

      const runtime = init({ session: 'demo', mode: 'replay' });
      assert.equal(runtime.mode, 'replay');
      assert.equal(runtime.configPath, configPath);
    } finally {
      process.chdir(oldCwd);
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'init accepts explicit config path',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-explicit-'));
    try {
      const configPath = path.join(tempDir, 'custom.stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');

      const runtime = init({
        session: 'demo',
        mode: 'passthrough',
        configPath,
      });

      assert.equal(runtime.mode, 'passthrough');
      assert.equal(runtime.configPath, configPath);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'initFromEnv reads session mode and config path from environment',
  withCleanRuntime(() => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'stagehand-ts-env-'));
    try {
      const configPath = path.join(tempDir, 'stagehand.yml');
      fs.writeFileSync(configPath, 'schema_version: v1alpha1\n', 'utf8');
      process.env.STAGEHAND_SESSION = 'cli-demo';
      process.env.STAGEHAND_MODE = 'record';
      process.env.STAGEHAND_CONFIG_PATH = configPath;

      const runtime = initFromEnv();
      assert.equal(runtime.session, 'cli-demo');
      assert.equal(runtime.mode, 'record');
      assert.equal(runtime.configPath, configPath);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  })
);

test(
  'double init is rejected',
  withCleanRuntime(() => {
    init({ session: 'demo', mode: 'record' });

    assert.throws(() => init({ session: 'demo-again', mode: 'record' }), AlreadyInitializedError);
  })
);

test(
  'invalid mode is rejected',
  withCleanRuntime(() => {
    assert.throws(() => init({ session: 'demo', mode: 'hybrid' as never }), InvalidModeError);
  })
);

test(
  'empty session is rejected',
  withCleanRuntime(() => {
    assert.throws(
      () => init({ session: '   ', mode: 'record' }),
      /session must be a non-empty string/
    );
  })
);

test(
  'missing config path is rejected',
  withCleanRuntime(() => {
    const missingPath = path.join(os.tmpdir(), 'stagehand-missing-config.yml');
    assert.throws(
      () => init({ session: 'demo', mode: 'record', configPath: missingPath }),
      new RegExp(`Stagehand config file not found: ${escapeForRegExp(path.resolve(missingPath))}`)
    );
  })
);

test(
  'getRuntime requires prior initialization',
  withCleanRuntime(() => {
    assert.throws(() => getRuntime(), NotInitializedError);
  })
);

function escapeForRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
