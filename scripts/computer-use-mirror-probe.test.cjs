const {test} = require('node:test');
const assert = require('node:assert/strict');
const {point, guardPlacement} = require('./computer-use-mirror-probe.cjs');

const target = {
  frame: {x: 1703, y: -1252, width: 446, height: 978},
  display: {x: -411, y: -1440, width: 2560, height: 1440},
};

test('mirror coordinates retain negative multi-display origin', () => {
  assert.deepEqual(point(target.frame, 0.5, 0.5), {x: 1926, y: -763});
  for (const value of [-1, 1, NaN, Infinity, undefined, null, '0.5']) {
    assert.throws(() => point(target.frame, value, 0.5), /normalized coordinates/);
    assert.throws(() => point(target.frame, 0.5, value), /normalized coordinates/);
  }
});

test('guard uses target display bounds, and never moves the mirror', () => {
  assert.deepEqual(guardPlacement(target, true), target.frame);
  assert.deepEqual(guardPlacement(target, false), {x: 1463, y: -1252, width: 220, height: 180});
  const left = {...target, frame: {...target.frame, x: -400}};
  assert.deepEqual(guardPlacement(left, false), {x: 66, y: -1252, width: 220, height: 180});
  assert.throws(() => guardPlacement({...target, display: {...target.frame}}, false), /no room/);
});
