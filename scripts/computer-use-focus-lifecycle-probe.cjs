#!/usr/bin/env node
// Lifecycle fault injection against owned fixtures only. Never kills a user app.
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const readline = require('node:readline');
const {once} = require('node:events');
const {Fixture, createProbe, pause} = require('./computer-use-background-probe.cjs');

const scenarios = ['cancel', 'sender-killed', 'sender-killed-after-down', 'window-moved',
  'target-foreground', 'output-closed', 'front-input', 'worker-killed', 'worker-killed-after-down', 'worker-killed-after-up'];

function assessLifecycle(r) {
  const {scenario, before, after, monitor, guardAfter, received, guardEvents, events} = r;
  const takeover = scenario === 'target-foreground';
  const foreground = takeover ? before.pid : monitor.pid;
  const restored = after.active === takeover && after.keyWindow === takeover
    && after.foregroundPID === foreground;
  const isolated = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
    && monitor.foregroundPIDs.every(pid => [monitor.pid, ...(takeover ? [before.pid] : [])].includes(pid))
    && monitor.activations.every(pid => takeover && pid === before.pid) && monitor.maxCursorDistance === 0;
  const inputEvents = guardEvents.filter(e => e.kind === 'event');
  const inputPreserved = scenario === 'front-input'
    ? guardAfter.inputText === 'abc' && inputEvents.length === 3 && inputEvents.every(e => e.type === 10)
    : guardAfter.inputText === '' && inputEvents.length === 0;
  const guardUntouched = !guardEvents.some(e => e.kind === 'effect') && inputPreserved;
  const effects = received.filter(e => e.kind === 'effect');
  const shouldClick = ['sender-killed-after-down', 'worker-killed-after-down', 'worker-killed-after-up', 'front-input', 'output-closed'].includes(scenario);
  const correctEffect = shouldClick ? after.count === 1 && effects.length === 1
    && effects[0].effect === 'click' && effects[0].clickCount === 1 && !effects[0].command
    : after.count === 0 && !received.some(e => ['event', 'button-received', 'effect'].includes(e.kind));
  const targetInput = received.filter(e => ['event', 'button-received'].includes(e.kind));
  const ordinary = targetInput.every(e => [1, 2].includes(e.type) && e.flags === 0 && e.clickCount === 1);
  const onePress = !shouldClick || (targetInput.filter(e => e.kind === 'event' && e.type === 1).length === 1
    && targetInput.filter(e => e.kind === 'event' && e.type === 2).length <= 1
    && targetInput.filter(e => e.kind === 'button-received').length === 1);
  const sameIdentity = before.pid === after.pid && before.windowID === after.windowID;
  const geometry = scenario === 'window-moved' ? after.frame.x === before.frame.x + 20
    : JSON.stringify(after.frame) === JSON.stringify(before.frame);
  const noReplay = !events.some(e => e.kind === 'input-posted' && e.count > 2);
  const crashInjected = !scenario.startsWith('worker-killed') || (r.workerKilled > 0 && r.exit?.[0] === 1
    && received.some(e => e.kind === 'focus-event' && e.phase === 'after' && e.subtype === 1 && e.active)
    && (!scenario.endsWith('-after-down') || received.some(e => e.kind === 'button-received'))
    && (!scenario.endsWith('-after-up') || received.some(e => e.kind === 'effect')));
  const cleanupCount = events.filter(e => e.kind === 'cleanup' && e.event === 'button-up').length;
  const pairedCleanup = scenario.startsWith('worker-killed')
    ? cleanupCount === (scenario.endsWith('-after-down') ? 1 : 0)
    : true;
  return {restored, isolated, guardUntouched, correctEffect, ordinary, onePress, sameIdentity, geometry, noReplay, crashInjected, pairedCleanup,
    pass: restored && isolated && guardUntouched && correctEffect && ordinary && onePress && sameIdentity && geometry && noReplay && crashInjected && pairedCleanup};
}

async function runCase(probe, paths, scenario) {
  const target = new Fixture(paths.target, 'target', ['target', 'first-mouse-off']);
  const guard = new Fixture(paths.guard, 'guard');
  let sender, senderExit, leasePID, checkpointReached = false;
  const events = [];
  const record = {scenario, events};
  try {
    await target.ready(); await guard.ready();
    const geometry = await target.request('state');
    await guard.request('place', {frame: geometry.frame}); await guard.request('input-focus'); await pause(200);
    const before = await target.request('state'); record.before = before;
    if (before.active || before.keyWindow || before.foregroundPID !== guard.child.pid) throw new Error('inactive owned receiver required');
    const start = target.events.length, guardStart = guard.events.length;
    await guard.request('monitor');
    sender = spawn(probe.binary, ['focus-probe-send'], {stdio: ['pipe', 'pipe', 'pipe']});
    senderExit = once(sender, 'exit');
    sender.stderr.on('data', data => { record.stderr = (record.stderr ?? '') + data; });
    readline.createInterface({input: sender.stdout}).on('line', line => {
      const event = JSON.parse(line); events.push(event);
      if (event.kind === 'lease-start' && event.ownerPID === sender.pid) leasePID = event.leasePID;
      const checkpoint = scenario.endsWith('-after-up') ? event.kind === 'input-posted' && event.type === 2 : scenario.endsWith('-after-down')
        ? event.kind === 'input-posted' && event.type === 1
        : event.kind === 'focus-notification' && event.activated;
      if (checkpoint) {
        checkpointReached = true;
        // Do not SIGSTOP the worker: suspension plus parent death changes job-control
        // behaviour. Kill the owner at a real delivery boundary while the worker runs.
        if (['cancel', 'sender-killed', 'sender-killed-after-down'].includes(scenario)) {
          sender.kill(scenario === 'cancel' ? 'SIGTERM' : 'SIGKILL');
        } else if (scenario === 'output-closed') sender.stdout.destroy();
      }
    });
    sender.stdin.end(JSON.stringify({target: before, guardPID: guard.child.pid,
      variant: 'appkit-window-local', action: 'click'}));
    const deadline = Date.now() + 3000;
    while (!checkpointReached) { if (Date.now() > deadline || sender.exitCode !== null || sender.signalCode !== null) throw new Error('activation checkpoint not reached'); await pause(5); }
    if (scenario.startsWith('worker-killed')) {
      // Posting is asynchronous. Killing on the sender log can discard an event
      // before receipt, falsely passing a recovery test that never activated.
      const delivered = () => target.events.slice(start).some(e => scenario.endsWith('-after-up') ? e.kind === 'effect' : scenario.endsWith('-after-down')
        ? e.kind === 'button-received' : e.kind === 'focus-event' && e.phase === 'after' && e.subtype === 1 && e.active);
      while (!delivered()) { if (Date.now() > deadline || sender.exitCode !== null) throw new Error('receiver checkpoint not reached'); await pause(2); }
      if (!Number.isInteger(leasePID) || leasePID <= 1 || leasePID === sender.pid) throw new Error('owned worker identity missing');
      process.kill(leasePID, 'SIGKILL'); record.workerKilled = leasePID;
    }
    if (scenario === 'window-moved') await target.request('move-window');
    else if (scenario === 'target-foreground') await guard.request('activate-test-target', {
      targetPID: before.pid, targetExecutable: before.executable});
    else if (scenario === 'front-input') await guard.request('type-probe');
    record.exit = await senderExit;
    await pause(250);
    record.after = await target.request('state'); record.monitor = await guard.request('stop-monitor');
    record.guardAfter = await guard.request('state');
    record.received = target.events.slice(start); record.guardEvents = guard.events.slice(guardStart);
    record.assessment = assessLifecycle(record);
    console.log(JSON.stringify({scenario, ...record.assessment}));
  } catch (error) { record.error = error.message; throw error; }
  finally {
    if (sender && sender.exitCode === null && sender.signalCode === null) sender.kill('SIGKILL');
    record.targetLog = target.events; record.guardLog = guard.events;
    await target.close(); await guard.close();
    fs.writeFileSync(path.join(probe.dir, `focus-lifecycle-${scenario}.json`), JSON.stringify(record, null, 2));
  }
  return record.assessment.pass;
}

async function main() {
  const selected = process.argv[2];
  if (process.platform !== 'darwin' || process.argv.length !== 3 || (selected !== 'all' && !scenarios.includes(selected))) {
    throw new Error('usage: node scripts/computer-use-focus-lifecycle-probe.cjs all|' + scenarios.join('|'));
  }
  const probe = createProbe();
  if (!probe.status.accessibility || !probe.status.postEvents || !probe.status.screenCapture) throw new Error('existing permissions required; no request issued');
  const paths = {target: probe.bundle('target'), guard: probe.bundle('guard')};
  for (const scenario of selected === 'all' ? scenarios : [selected]) {
    // Each case closes its receiver and starts a fresh one. Collect independent
    // failures in one run; never retry input against a failed receiver.
    if (!await runCase(probe, paths, scenario)) process.exitCode = 1;
  }
  console.log(JSON.stringify({evidence: probe.dir, productionReady: false}));
}
module.exports = {assessLifecycle, scenarios};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
