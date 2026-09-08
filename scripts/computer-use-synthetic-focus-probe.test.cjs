const {test} = require('node:test');
const assert = require('node:assert/strict');
const {evaluate} = require('./computer-use-synthetic-focus-probe.cjs');
const before = {pid: 1, windowID: 2, frame: {x: 0, y: 0, width: 400, height: 432},
  active: false, keyWindow: false, count: 0, buttonText: 'Increment'};
const after = {...before, count: 1, buttonText: '1'};
const monitor = {samples: 100, foregroundPIDs: [3], activations: [], maxCursorDistance: 0};
const native = [{kind: 'event', type: 1, clickCount: 1, flags: 0},
  {kind: 'button-received', type: 1, clickCount: 1, flags: 0}];
const sent = {kind: 'sent', count: 2, variant: 'appkit-window-local', command: false};
const sender = {ok: true, output: JSON.stringify(sent)};
const dom = ['mousedown', 'mouseup', 'click'].map(type => ({kind: 'event', type, detail: 1,
  target: 'click', flags: 0, trusted: true}));

for (const [receiver, input] of [['appkit', native], ['electron', dom]]) {
  test(`${receiver}: activation must produce one ordinary click and restore receiver state`, () => {
    const events = [...input, {kind: 'effect', effect: 'click', count: 1, clickCount: 1}];
    const result = (a = after, r = events, m = monitor, s = sender) => evaluate(before, a, r, m, [], s, 3, receiver);
    assert.equal(result().pass, true);
    assert.equal(result({...after, active: true}).pass, false);
    assert.equal(result({...after, keyWindow: true}).focusRestored, false);
    assert.equal(result({...after, windowID: 4}).isolated, false);
    assert.equal(result(after, [], monitor).pass, false, 'notification alone is not a click');
    assert.equal(result(after, [...events, ...input]).pass, false, 'no hidden extra clicks');
    assert.equal(result(after, events, {...monitor, foregroundPIDs: [1, 3]}).isolated, false);
    assert.equal(result(after, events, {...monitor, maxCursorDistance: 20}).isolated, false);
    assert.equal(result(after, events, monitor, {...sender, ok: false}).pass, false);
    assert.equal(result(after, events, monitor, {ok: true}).pass, false, 'missing delivery evidence');
    assert.equal(result(after, events, monitor, {ok: true, output: JSON.stringify({...sent, count: 4})}).pass, false);
    assert.equal(result(after, events, monitor, {ok: true, output: sender.output + '\n' + JSON.stringify({kind: 'cleanup'})}).pass, false);
    assert.equal(result(after, events.map(e => e.kind === 'event' ? {...e, flags: 0x100000} : e)).pass, false);
    if (receiver === 'appkit') {
      assert.equal(result(after, [...events, {kind: 'event', type: 2, clickCount: 1, flags: 0}]).pass, true,
        'mouseUp may be observed at sendEvent or consumed by NSButton tracking');
      assert.equal(result(after, events.filter(e => e.kind !== 'button-received')).pass, false);
      assert.equal(result(after, events.map(e => e.kind === 'effect' ? {...e, clickCount: 2} : e)).pass, false);
    }
  });
}
