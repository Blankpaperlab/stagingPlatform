import test from 'node:test';
import assert from 'node:assert/strict';

import { init } from './index.js';

test('init throws until Story G1 lands', () => {
  assert.throws(() => init({ session: 'demo', mode: 'record' }), /Story G1/);
});
