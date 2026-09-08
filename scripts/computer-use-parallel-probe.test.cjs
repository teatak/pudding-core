const {test} = require('node:test');
const assert = require('node:assert/strict');
const {assessParallel} = require('./computer-use-parallel-probe.cjs');

function sample(mode = 'contention') {
  const before = {pid: 1, windowID: 2, frame: {x: 80}, count: 0};
  return {mode, before, durationMs: 30_000,
    after: {...before, count: 2, active: false, keyWindow: false, foregroundPID: 3},
    monitor: {pid: 3, samples: 50, foregroundPIDs: [3], activations: [], maxCursorDistance: mode === 'human' ? 50 : 0},
    guardAfter: {active: true, keyWindow: true, inputText: mode === 'human' ? 'pudding 1234567890' : ''},
    senders: [0, 2].map(t => ({index: t / 2 + 1, exit: [0, null], events: [
      ...(t === 2 ? [{kind: 'lease-wait'}] : []),
      {kind: 'focus-notification', activated: true, monotonicTime: t},
      {kind: 'input-posted', type: 1}, {kind: 'input-posted', type: 2},
      {kind: 'sent', count: 2}, {kind: 'focus-notification', activated: false, monotonicTime: t + 1}]})),
    received: [0, 2].flatMap(t => [{kind: 'focus-event', phase: 'after', subtype: 1, active: true, monotonicTime: t + 0.1},
      {kind: 'event', type: 1, flags: 0, clickCount: 1},
      {kind: 'button-received', flags: 0, clickCount: 1}, {kind: 'effect', effect: 'click', clickCount: 1},
      {kind: 'focus-event', phase: 'before', subtype: 2, monotonicTime: t + 0.9}]),
    guardEvents: mode === 'human' ? [...'pudding 1234567890'].map(() => ({kind: 'event', type: 10, monotonicTime: 0.5})) : []};
}

test('parallel assessment rejects shared activation overlap and swallowed clicks', () => {
  const r = sample();
  assert.equal(assessParallel(r).pass, true);
  r.senders[1].events.find(e => e.kind === 'focus-notification' && e.activated).monotonicTime = 0.5;
  assert.equal(assessParallel(r).overlap, true);
  assert.equal(assessParallel(r).pass, false);
  assert.equal(assessParallel({...sample(), after: {...r.after, count: 1}}).exactClicks, false);
  assert.equal(assessParallel({...sample(), senders: []}).completed, false);
  const noWait = sample();
  noWait.senders[1].events = noWait.senders[1].events.filter(e => e.kind !== 'lease-wait');
  assert.equal(assessParallel(noWait).contentionExercised, false);
});

test('another caller may proceed only after interrupted ownership restores its pending press', () => {
  for (const mode of ['contention-worker-killed', 'contention-sender-killed']) {
    const r = sample(mode); r.killedCaller = 1;
    r.senders[0].exit = mode === 'contention-worker-killed' ? [1, null] : [null, 'SIGKILL'];
    r.senders[0].events = r.senders[0].events.filter(e => e.kind !== 'sent' && !(e.kind === 'input-posted' && e.type === 2));
    r.senders[0].events.push({kind: 'cleanup', event: 'button-up'}, {kind: 'lease-worker-exit', waitStatus: 9});
    assert.equal(assessParallel(r).pass, true, mode);
    assert.equal(assessParallel({...r, killedCaller: 2}).completed, false);
    r.senders[0].events = r.senders[0].events.filter(e => e.kind !== 'cleanup');
    assert.equal(assessParallel(r).completed, false);
  }
});

test('human assessment requires actual overlapping input, movement, full text and duration', () => {
  const r = sample('human');
  assert.equal(assessParallel(r).pass, true);
  assert.equal(assessParallel({...r, guardEvents: []}).inputVerified, false);
  assert.equal(assessParallel({...r, received: r.received.filter(e => e.kind !== 'focus-event')}).humanOverlap, false);
  assert.equal(assessParallel({...r, guardEvents: r.guardEvents.map(e => ({...e, monotonicTime: 0.05}))}).humanOverlap, false);
  assert.equal(assessParallel({...r, guardEvents: r.guardEvents.map(e => ({...e, monotonicTime: 9}))}).inputVerified, false);
  assert.equal(assessParallel({...r, durationMs: 29_000}).inputVerified, false);
  assert.equal(assessParallel({...r, guardAfter: {...r.guardAfter, inputText: 'pudding'}}).inputVerified, false);
  assert.equal(assessParallel({...r, monitor: {...r.monitor, maxCursorDistance: 0}}).inputVerified, false);
  assert.equal(assessParallel({...r, monitor: {...r.monitor, activations: [1]}}).foregroundStable, false);
  assert.equal(assessParallel({...r, received: [...r.received, {kind: 'event', type: 10}]}).noCrossInput, false);
});
