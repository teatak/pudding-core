const {test} = require('node:test');
const assert = require('node:assert/strict');
const {summarize} = require('./computer-use-electron-compare.cjs');
const before = {pid: 1, windowID: 2, frame: {x: 0, y: 0, width: 400, height: 432}, buttonText: 'Increment'};
const after = {...before, buttonText: '1'};
const monitor = {samples: 100, foregroundPIDs: [3], activations: [], maxCursorDistance: 0};
const events = ['mousedown', 'mouseup', 'click'].map(type => ({kind: 'event', type, detail: 1, target: 'click', flags: 0, trusted: true}));
const received = [...events, {kind: 'effect', effect: 'click', count: 1}];
const result = (r = received, m = monitor, a = after, g = []) => summarize(before, a, r, m, g, 3);

test('one ordinary trusted DOM click must increment a fresh receiver exactly once', () => {
  assert.equal(result().pass, true);
  assert.equal(result().buttonEffects, 1);
  assert.equal(result([], monitor, before).isolated, true);
  assert.equal(result([], monitor, before).pass, false, 'delivery/isolation is not a UI effect');
  assert.equal(result([...received, received.at(-1)]).pass, false);
  assert.equal(result(received, monitor, {...after, buttonText: '2'}).pass, false);
  assert.equal(result([...received, {kind: 'effect', effect: 'button-double'}]).pass, false);
  assert.equal(result(received.map(e => e.kind === 'event' ? {...e, detail: 2} : e)).pass, false);
  for (const patch of [{flags: 0x100000}, {trusted: false}, {target: 'wrong'}]) {
    assert.equal(result(received.map(e => e.kind === 'event' ? {...e, ...patch} : e)).pass, false);
  }
});

test('focus, cursor, guard input and target-window changes invalidate a comparison sample', () => {
  for (const patch of [{samples: 0}, {foregroundPIDs: [3, 1]}, {activations: [1]}, {maxCursorDistance: 1}]) {
    assert.equal(result(received, {...monitor, ...patch}).isolated, false);
  }
  assert.equal(result(received, monitor, {...after, windowID: 4}).isolated, false);
  assert.equal(result(received, monitor, after, [{kind: 'event'}]).isolated, false);
});
