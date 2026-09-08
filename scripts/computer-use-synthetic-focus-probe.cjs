#!/usr/bin/env node
// One causal change: an AppKit activation notification around the existing single click.
// Own fixtures only. No user-app activation, event interception, extra click or fallback.
const fs = require('node:fs');
const path = require('node:path');
const {Fixture, createProbe, run, pause, assess} = require('./computer-use-background-probe.cjs');
const {copyElectron, summarize} = require('./computer-use-electron-compare.cjs');

function evaluate(before, after, received, monitor, guardEvents, sender, guardPID, receiver) {
  const trace = (sender.output ?? '').split('\n').filter(Boolean).map(line => JSON.parse(line));
  const sent = trace.filter(e => e.kind === 'sent');
  const singlePairSent = sender.ok && sent.length === 1 && sent[0].count === 2
    && sent[0].variant === 'appkit-window-local' && sent[0].command === false
    && !trace.some(e => e.kind === 'cleanup');
  const result = receiver === 'electron' ? summarize(before, after, received, monitor, guardEvents, guardPID)
    : assess('click', received, monitor, guardEvents, sender.ok, guardPID);
  const sameWindow = before.pid === after.pid && before.windowID === after.windowID
    && JSON.stringify(before.frame) === JSON.stringify(after.frame);
  const focusRestored = after.active === before.active && after.keyWindow === before.keyWindow;
  const raw = received.filter(e => e.kind === 'event');
  const button = received.filter(e => e.kind === 'button-received');
  const effects = received.filter(e => e.kind === 'effect');
  // NSButton's tracking loop can consume mouseUp below NSApplication.sendEvent.
  // Check the sender's complete pair AND the receiver's one ordinary callback.
  const ordinarySingleClick = receiver === 'electron' ? result.ordinarySingleClick
    : raw.filter(e => e.type === 1).length === 1 && raw.filter(e => e.type === 2).length <= 1
      && raw.every(e => [1, 2].includes(e.type) && e.clickCount === 1)
      && button.length === 1 && button[0].clickCount === 1 && button[0].flags === 0
      && effects.length === 1 && effects[0].effect === 'click' && effects[0].clickCount === 1
      && after.count - before.count === 1;
  return {...result, sameWindow, focusRestored, ordinarySingleClick, singlePairSent,
    isolated: result.foregroundStable && result.cursorStable && result.guardUntouched && sameWindow,
    pass: singlePairSent && result.pass && sameWindow && focusRestored && ordinarySingleClick};
}

async function main() {
  const receiver = process.argv[2];
  if (process.platform !== 'darwin' || process.argv.length !== 3 || !['appkit', 'electron'].includes(receiver)) {
    throw new Error('usage: node scripts/computer-use-synthetic-focus-probe.cjs appkit|electron; owned fixtures only');
  }
  const probe = createProbe(), {dir, binary, status} = probe;
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const electron = receiver === 'electron' ? copyElectron(dir) : undefined;
  const targetPath = electron?.executable ?? probe.bundle('target');
  const guard = new Fixture(probe.bundle('guard'), 'guard'), records = [];
  let target;
  async function state() {
    const value = await target.request('state');
    if (receiver === 'appkit') return value;
    return {...value, ...JSON.parse(run(binary, ['window'], {input: JSON.stringify(value)}))};
  }
  try {
    await guard.ready();
    for (const syntheticActivation of [false, true]) {
      const cell = path.join(dir, syntheticActivation ? 'synthetic' : 'baseline'); fs.mkdirSync(cell);
      target = new Fixture(targetPath, receiver, receiver === 'electron'
        ? [path.resolve(__dirname, '../electron/smoke/background-input-fixture.cjs'), cell, 'default']
        : ['target', 'first-mouse-off']);
      await target.ready(); await target.request('position');
      const geometry = await state();
      await guard.request('place', {frame: geometry.frame});
      await pause(250);
      const before = await state(), g = await guard.request('state');
      if (before.active || before.keyWindow || g.foregroundPID !== guard.child.pid) throw new Error('receiver not inactive behind guard; no input sent');
      const targetStart = target.events.length, guardStart = guard.events.length;
      await guard.request('monitor');
      let sender;
      try {
        sender = {ok: true, output: run(binary, [syntheticActivation ? 'focus-probe-send' : 'send'], {
          input: JSON.stringify({target: before, guardPID: guard.child.pid, variant: 'appkit-window-local', action: 'click'})})};
      } catch (error) { sender = {ok: false, error: error.message}; }
      await pause(250);
      const monitor = await guard.request('stop-monitor'), after = await state();
      const received = target.events.slice(targetStart), guardEvents = guard.events.slice(guardStart);
      const assessment = evaluate(before, after, received, monitor, guardEvents, sender, guard.child.pid, receiver);
      const trace = (sender.output ?? '').split('\n').filter(Boolean).map(line => JSON.parse(line));
      assessment.coverageVerified = trace.find(e => e.kind === 'scene')?.topmostNormalWindowPID === guard.child.pid;
      assessment.pass &&= assessment.coverageVerified;
      records.push({receiver, syntheticActivation, before, after, received, monitor, guardEvents, sender, assessment});
      console.log(JSON.stringify({receiver, syntheticActivation, targetPID: before.pid, ...assessment}));
      await target.close(); target = undefined;
      if (!assessment.isolated || !assessment.focusRestored || !assessment.coverageVerified) throw new Error('isolation/restoration changed; stopped without retry');
    }
  } finally {
    if (target) await target.close();
    await guard.close();
    fs.writeFileSync(path.join(dir, 'synthetic-focus.json'), JSON.stringify({status, receiver, records}, null, 2));
  }
  const mechanismVerified = records.length === 2 && !records[0].assessment.effect && records[1].assessment.pass;
  console.log(JSON.stringify({evidence: dir, mechanismVerified, productionReady: false}));
  if (!mechanismVerified) process.exitCode = 1;
}

module.exports = {evaluate};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
