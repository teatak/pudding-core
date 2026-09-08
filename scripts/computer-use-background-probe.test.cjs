const {test} = require('node:test');
const assert = require('node:assert/strict');
const {assess} = require('./computer-use-background-probe.cjs');
const monitor = {samples: 40, foregroundPIDs: [123], activations: [], maxCursorDistance: 0};
const events = [{kind: 'event', flags: 0}, {kind: 'effect', effect: 'click', command: false}];

test('delivery requires a matching UI effect, raw input, stable foreground and cursor', () => {
  assert.equal(assess('click', events, monitor, [], true, 123).pass, true);
  assert.equal(assess('click', [], monitor, [], true, 123).pass, false);
  assert.equal(assess('click', [events[1]], monitor, [], true, 123).pass, false);
  assert.equal(assess('click', events, monitor, [], false, 123).pass, false);
  assert.equal(assess('click', [{...events[0], trusted: false}, events[1]], monitor, [], true, 123).pass, false);
  assert.equal(assess('click', [{...events[0], type: 'mousedown'}, events[1]], monitor, [], true, 123).pass, false);
  assert.equal(assess('click', [...events, events[1]], monitor, [], true, 123).pass, false);
});

test('command-click is not accepted as a plain click', () => {
  assert.equal(assess('click', [{...events[0], flags: 0x100000}, events[1]], monitor, [], true, 123).pass, false);
  assert.equal(assess('click', [events[0], {...events[1], command: true}], monitor, [], true, 123).pass, false);
});

test('transient activation, pointer movement and guard effects fail the isolation gate', () => {
  for (const change of [{activations: [456, 123]}, {foregroundPIDs: [123, 456]},
    {maxCursorDistance: 1}, {samples: 0}, {foregroundPIDs: []}]) {
    assert.equal(assess('click', events, {...monitor, ...change}, [], true, 123).pass, false);
  }
  assert.equal(assess('click', events, monitor, events, true, 123).pass, false);
});

test('a received scroll with no actual displacement is not a pass', () => {
  const scroll = [{kind: 'event', flags: 0}, {kind: 'effect', effect: 'scroll', before: 0, after: 0}];
  assert.equal(assess('scroll', scroll, monitor, [], true, 123).pass, false);
  scroll[1].after = 80;
  assert.equal(assess('scroll', scroll, monitor, [], true, 123).pass, true);
});
