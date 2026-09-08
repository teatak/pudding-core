const {test} = require('node:test');
const assert = require('node:assert/strict');
const {assessLifecycle, scenarios} = require('./computer-use-focus-lifecycle-probe.cjs');

function sample(scenario) {
  const before = {pid: 11, windowID: 12, active: false, keyWindow: false,
    frame: {x: 80, y: 140, width: 400, height: 432}};
  const takeover = scenario === 'target-foreground';
  const click = ['sender-killed-after-down', 'worker-killed-after-down', 'worker-killed-after-up', 'output-closed', 'front-input'].includes(scenario);
  return {scenario, before, after: {...before, active: takeover, keyWindow: takeover,
    foregroundPID: takeover ? 11 : 20, count: click ? 1 : 0,
    frame: {...before.frame, x: scenario === 'window-moved' ? 100 : 80}},
  monitor: {pid: 20, samples: 20, foregroundPIDs: takeover ? [20, 11] : [20],
    activations: takeover ? [11] : [], maxCursorDistance: 0},
  guardAfter: {inputText: scenario === 'front-input' ? 'abc' : ''},
  guardEvents: scenario === 'front-input' ? ['a', 'b', 'c'].map(characters => ({kind: 'event', type: 10, characters})) : [],
  received: click ? [{kind: 'event', type: 1, flags: 0, clickCount: 1},
    {kind: 'button-received', type: 1, flags: 0, clickCount: 1},
    {kind: 'effect', effect: 'click', clickCount: 1, command: false}] : [],
    events: scenario === 'worker-killed-after-down' ? [{kind: 'cleanup', event: 'button-up'}] : [],
    ...(scenario.startsWith('worker-killed') ? {workerKilled: 40, exit: [1, null]} : {})};
}

test('lifecycle samples require restored state, exact effects and the intended foreground', () => {
  for (const scenario of scenarios) {
    const r = sample(scenario);
    if (scenario.startsWith('worker-killed')) r.received.push({kind: 'focus-event', phase: 'after', subtype: 1, active: true});
    assert.equal(assessLifecycle(r).pass, true, scenario);
    assert.equal(assessLifecycle({...r, after: {...r.after, active: !r.after.active}}).pass, false);
    assert.equal(assessLifecycle({...r, monitor: {...r.monitor, foregroundPIDs: [999]}}).pass, false);
    assert.equal(assessLifecycle({...r, monitor: {...r.monitor, samples: 0}}).pass, false);
    assert.equal(assessLifecycle({...r, monitor: {...r.monitor, maxCursorDistance: 2}}).pass, false);
    assert.equal(assessLifecycle({...r, after: {...r.after, count: 2}}).pass, false);
    assert.equal(assessLifecycle({...r, after: {...r.after, windowID: 99}}).pass, false);
  }
});

test('recovery releases a pending down exactly once and never repeats an already delivered up', () => {
  const down = sample('worker-killed-after-down');
  assert.equal(assessLifecycle(down).pairedCleanup, true);
  assert.equal(assessLifecycle({...down, events: []}).pairedCleanup, false);
  assert.equal(assessLifecycle({...down, events: [...down.events, ...down.events]}).pairedCleanup, false);
  const up = sample('worker-killed-after-up');
  assert.equal(assessLifecycle(up).pairedCleanup, true);
  assert.equal(assessLifecycle({...up, events: down.events}).pairedCleanup, false);
});

test('worker crash recovery cannot pass unless activation actually reached the receiver', () => {
  const r = sample('worker-killed');
  assert.equal(assessLifecycle(r).crashInjected, false);
  r.received.push({kind: 'focus-event', phase: 'after', subtype: 1, active: true});
  assert.equal(assessLifecycle(r).crashInjected, true);
  assert.equal(assessLifecycle({...r, workerKilled: undefined}).crashInjected, false);
  assert.equal(assessLifecycle({...r, exit: [0, null]}).crashInjected, false);
});

test('taking the target foreground must preserve the user activation, not restore false', () => {
  const r = sample('target-foreground');
  assert.equal(assessLifecycle({...r, after: {...r.after, active: false, keyWindow: false}}).restored, false);
  assert.equal(assessLifecycle({...r, received: sample('front-input').received}).correctEffect, false);
});

test('foreground keyboard routing must preserve all three keys and never send them to the target', () => {
  const r = sample('front-input');
  assert.equal(assessLifecycle({...r, guardAfter: {inputText: 'ab'}}).guardUntouched, false);
  assert.equal(assessLifecycle({...r, guardEvents: r.guardEvents.slice(0, 2)}).guardUntouched, false);
  assert.equal(assessLifecycle({...r, received: [...r.received, {kind: 'event', type: 10, characters: 'a'}]}).ordinary, false);
  assert.equal(assessLifecycle({...r, received: [...r.received, r.received[0]]}).onePress, false);
  assert.equal(assessLifecycle({...r, received: r.received.filter(e => e.kind !== 'button-received')}).onePress, false);
  assert.equal(assessLifecycle({...r, events: [{kind: 'input-posted', count: 3}]}).noReplay, false);
});
