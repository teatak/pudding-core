const {test} = require('node:test');
const assert = require('node:assert/strict');
const {cases, summarize} = require('./computer-use-click-factor-probe.cjs');

test('factor matrix independently varies pair count, final click count and event number', () => {
  const matrix = cases();
  assert.equal(matrix.length, 8);
  assert.equal(new Set(matrix.map(c => JSON.stringify(c.clicks))).size, 8);
  for (const c of matrix) {
    const [, pairs, count, number] = c.id.match(/^pairs(\d)-count(\d)-number(\d)$/);
    assert.equal(c.clicks.length, Number(pairs));
    assert.deepEqual(c.clicks.at(-1), {clickCount: Number(count), eventNumber: Number(number)});
    if (c.clicks.length === 2) assert.deepEqual(c.clicks[0], {clickCount: 1, eventNumber: 1});
  }
});

test('receiver summary distinguishes arrival, button effects and changed double-click semantics', () => {
  assert.equal(summarize([{kind: 'event', type: 'mouseup', detail: 1}]).buttonEffects, 0);
  const summary = summarize([
    {kind: 'event', type: 1, clickCount: 2, eventNumber: 1},
    {kind: 'effect', effect: 'click'}, {kind: 'effect', effect: 'click'},
    {kind: 'effect', effect: 'button-double'}, {kind: 'first-mouse', accepted: false},
  ]);
  assert.equal(summary.buttonEffects, 2);
  assert.equal(summary.buttonDoubles, 1);
  assert.equal(summary.events[0].clickCount, 2);
  assert.equal(summary.events[0].eventNumber, 1);
  assert.equal(summary.firstMouseChecks[0].accepted, false);
  assert.equal({...{firstMouse: false}, ...summary}.firstMouse, false);
});
