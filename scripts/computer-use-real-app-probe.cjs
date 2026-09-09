#!/usr/bin/env node
// Manual, allowlisted native app smoke using the same shared focus lease as fixtures.
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {Fixture, createProbe, run, pause} = require('./computer-use-background-probe.cjs');
const {point} = require('./computer-use-mirror-probe.cjs');

function selectWindow(state, windowID) {
  const windows = state.windows.filter(w => windowID === undefined || w.windowID === windowID);
  if (windows.length !== 1) throw new Error('select one of the current window IDs explicitly');
  return windows[0];
}

function assess(sender, monitor, guardEvents, guardPID, covered, targetPID, notificationOnly = false) {
  const trace = sender.output.trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  const sent = trace.filter(e => e.kind === 'sent');
  const posted = trace.filter(e => e.kind === 'input-posted');
  const notifications = trace.filter(e => e.kind === 'focus-notification');
  const ordinaryClick = posted.length === 2 && posted[0].type === 1 && posted[1].type === 2;
  const paired = sender.code === 0 && sent.length === 1 && sent[0].count === (notificationOnly ? 0 : 2) && sent[0].command === false
    && (notificationOnly ? posted.length === 0 : ordinaryClick)
    && notifications.length === 2 && notifications[0].activated === true && notifications[1].activated === false
    && !trace.some(e => ['error', 'cleanup', 'recovery-error'].includes(e.kind))
    && trace.some(e => e.kind === 'lease-restored');
  const foregroundStable = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
    && monitor.foregroundPIDs.every(pid => pid === guardPID) && monitor.activations.every(pid => pid === guardPID);
  const cursorStable = monitor.samples > 0 && monitor.maxCursorDistance < 0.5;
  const guardUntouched = !guardEvents.some(e => ['event', 'effect'].includes(e.kind));
  const coverageVerified = trace.find(e => e.kind === 'scene')?.topmostNormalWindowPID === (covered ? guardPID : targetPID);
  const passed = paired && foregroundStable && cursorStable && guardUntouched && coverageVerified;
  return {paired, foregroundStable, cursorStable, guardUntouched, coverageVerified,
    deliveryPass: !notificationOnly && passed, notificationControlPass: notificationOnly && passed,
    uiEffect: 'unverified: inspect before/after images; routing is not receiver success'};
}

async function main() {
  const realApp = process.argv[2];
  if (process.platform !== 'darwin' || process.argv.length !== 3 || !['mail', 'feishu', 'calendar'].includes(realApp)) {
    throw new Error('usage: node scripts/computer-use-real-app-probe.cjs mail|feishu|calendar');
  }
  const {dir, binary, status, bundle} = createProbe();
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const windows = () => JSON.parse(run(binary, ['real-app-windows'], {input: JSON.stringify({realApp})}));
  const initial = windows();
  const guard = new Fixture(bundle('guard'), 'guard');
  const records = [];
  let target, covered, count = 0;
  async function invoke(mode, input) {
    const child = spawn(binary, [mode], {stdio: ['pipe', 'pipe', 'pipe']});
    let output = '', error = '';
    child.stdout.on('data', chunk => output += chunk);
    child.stderr.on('data', chunk => error += chunk);
    child.stdin.end(JSON.stringify(input));
    const timer = setTimeout(() => child.kill('SIGTERM'), 10_000);
    // On caller death, the surviving worker still owns stdout until paired recovery.
    const [code, signal] = await once(child, 'close'); clearTimeout(timer);
    return {code, signal, output, error};
  }
  async function capture(label) {
    if (!target) throw new Error('prepare an explicit target window first');
    const current = selectWindow(windows(), target.windowID);
    if (current.pid !== target.pid || JSON.stringify(current.frame) !== JSON.stringify(target.frame)) {
      throw new Error('target changed; no coordinate reuse');
    }
    const output = path.join(dir, `${++count}-${label}.png`);
    const result = await invoke('real-app-capture', {realApp, target, output});
    if (result.code !== 0) throw new Error(result.output || result.error);
    return {output, windowID: target.windowID};
  }
  const lines = readline.createInterface({input: process.stdin});
  const deadline = setTimeout(() => lines.close(), 10 * 60_000);
  try {
    await guard.ready();
    console.log(JSON.stringify({ready: true, evidence: dir, ...initial}));
    for await (const line of lines) {
      try {
        const command = JSON.parse(line);
        if (command.op === 'quit') break;
        if (command.op === 'windows') { console.log(JSON.stringify(windows())); continue; }
        if (command.op === 'prepare') {
          target = selectWindow(windows(), command.windowID);
          covered = command.covered === true;
          // Only our guard is placed/activated. Never move or raise the user's target.
          await guard.request('place', {frame: covered ? target.frame : {
            x: target.frame.x + target.frame.width - 220, y: target.frame.y, width: 220, height: 180,
          }});
          await pause(250);
          console.log(JSON.stringify({prepared: true, covered, target, capture: await capture('before')}));
        } else if (command.op === 'capture') {
          console.log(JSON.stringify({capture: await capture('observed')}));
        } else if (command.op === 'click' || command.op === 'notify-only') {
          if (!target) throw new Error('prepare and inspect the window first');
          const location = point(target.frame, command.x, command.y);
          const guardStart = guard.events.length;
          await guard.request('monitor');
          let sender;
          try {
            sender = await invoke(command.op === 'click' ? 'focus-probe-send' : 'real-app-notify', {realApp, target: {...target, points: {click: location}},
              guardPID: guard.child.pid, variant: 'appkit-window-local', action: 'click'});
            await pause(350);
          } finally { await guard.request('stop-monitor'); }
          const monitor = await guard.request('state');
          const assessment = assess(sender, monitor, guard.events.slice(guardStart), guard.child.pid, covered, target.pid,
            command.op === 'notify-only');
          let after;
          try { after = await capture('after'); } catch (error) { after = {error: error.message}; }
          const record = {realApp, covered, command, sender, monitor, assessment,
            guardEvents: guard.events.slice(guardStart), capture: after};
          records.push(record);
          fs.appendFileSync(path.join(dir, 'real-app-events.jsonl'), JSON.stringify(record) + '\n');
          console.log(JSON.stringify(record));
          if (!assessment.deliveryPass && !assessment.notificationControlPass) {
            console.log(JSON.stringify({stopped: 'delivery/isolation failed; no replay'})); break;
          }
        } else throw new Error('unknown command; use prepare/capture/windows/click/notify-only/quit');
      } catch (error) { console.log(JSON.stringify({error: error.message})); }
    }
  } finally {
    clearTimeout(deadline); lines.close();
    await guard.close(); // Leave the user's applications running.
    fs.writeFileSync(path.join(dir, 'real-app-results.json'), JSON.stringify({realApp, status, records}, null, 2));
  }
}

module.exports = {selectWindow, assess};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
