const {test} = require('node:test');
const assert = require('node:assert/strict');
const {selectWindow, assess} = require('./computer-use-real-app-probe.cjs');

test('real-app smoke never silently chooses among multiple windows', () => {
  const a = {windowID: 1}, b = {windowID: 2};
  assert.equal(selectWindow({windows: [a]}, undefined), a);
  assert.equal(selectWindow({windows: [a, b]}, 2), b);
  for (const state of [{windows: []}, {windows: [a,b]}]) assert.throws(() => selectWindow(state), /explicitly/);
  assert.throws(() => selectWindow({windows: [a]}, 99), /explicitly/);
});

test('real-app delivery assessment requires full pairing, isolation and actual coverage', () => {
  const trace = [
    {kind: 'scene', topmostNormalWindowPID: 20},
    {kind: 'focus-notification', activated: true},
    {kind: 'input-posted', type: 1}, {kind: 'input-posted', type: 2},
    {kind: 'sent', count: 2, command: false},
    {kind: 'focus-notification', activated: false}, {kind: 'lease-restored'},
  ];
  const sender = {code: 0, output: trace.map(e => JSON.stringify(e)).join('\n')};
  const monitor = {samples: 100, foregroundPIDs: [20], activations: [], maxCursorDistance: 0};
  const check = (s = sender, m = monitor, events = [], covered = true) => assess(s, m, events, 20, covered, 10);
  assert.equal(check().deliveryPass, true);
  assert.match(check().uiEffect, /unverified/);
  assert.equal(check({...sender, code: 1}).deliveryPass, false);
  assert.equal(check(sender, {...monitor, foregroundPIDs: [10,20]}).deliveryPass, false);
  assert.equal(check(sender, {...monitor, maxCursorDistance: 2}).deliveryPass, false);
  assert.equal(check(sender, monitor, [{kind: 'event'}]).deliveryPass, false);
  assert.equal(check(sender, monitor, [], false).deliveryPass, false);
  for (const kind of ['lease-restored', 'input-posted', 'focus-notification']) {
    assert.equal(check({...sender, output: trace.filter(e => e.kind !== kind).map(e => JSON.stringify(e)).join('\n')}).deliveryPass, false);
  }
  const control = trace.filter(e => e.kind !== 'input-posted').map(e => e.kind === 'sent' ? {...e, count: 0} : e);
  const controlSender = {...sender, output: control.map(e => JSON.stringify(e)).join('\n')};
  const result = assess(controlSender, monitor, [], 20, true, 10, true);
  assert.equal(result.notificationControlPass, true);
  assert.equal(result.deliveryPass, false); // A zero-input control is never a successful click.
  assert.equal(assess(sender, monitor, [], 20, true, 10, true).notificationControlPass, false);
  assert.equal(assess({...controlSender, output: controlSender.output + '\n' + JSON.stringify({kind:'input-posted', type:1})},
    monitor, [], 20, true, 10, true).notificationControlPass, false);
});
