const {test} = require('node:test');
const assert = require('node:assert/strict');
const {sevenPoint, summarize} = require('./computer-use-calculator-probe.cjs');
const target = {pid: 10, windowID: 20, text: ['0'], frame: {x: -300, y: -600, width: 230, height: 409},
  buttons: [{id: 'Seven', x: 34, y: 211}]};
const monitor = {samples: 100, foregroundPIDs: [30], activations: [], maxCursorDistance: 0};

test('Calculator single-click target is unambiguous and preserves negative display origin', () => {
  assert.deepEqual(sevenPoint(target), {x: -266, y: -389});
  for (const buttons of [[], [...target.buttons, ...target.buttons], [{id: 'Seven', x: NaN, y: 1}], [{id: 'Seven', x: 230, y: 1}]]) {
    assert.throws(() => sevenPoint({...target, buttons}));
  }
});

test('Calculator routing evidence is distinct from isolation and UI success', () => {
  const s = summarize(target, target, monitor, [], [{kind: 'listener-stopped'}], 30);
  assert.equal(s.isolated, true);
  assert.equal(s.clickMetadataObserved, false);
  assert.equal(s.tapHealthy, true);
  assert.deepEqual(s.before, s.after); // No invented success from a delivered event.
  for (const changed of [{...monitor, samples: 0}, {...monitor, foregroundPIDs: [30, 10]},
    {...monitor, activations: [10]}, {...monitor, maxCursorDistance: 1}]) {
    assert.equal(summarize(target, target, changed, [], [], 30).isolated, false);
  }
  assert.equal(summarize(target, {...target, windowID: 21}, monitor, [], [], 30).isolated, false);
  assert.equal(summarize(target, target, monitor, [{kind: 'event'}], [], 30).isolated, false);
  assert.equal(summarize(target, target, monitor, [], [{kind: 'tap-disabled'}, {kind: 'listener-stopped'}], 30).tapHealthy, false);
  assert.equal(summarize(target, target, monitor, [], [{kind: 'listener-deadline'}, {kind: 'listener-stopped'}], 30).tapHealthy, false);
});
