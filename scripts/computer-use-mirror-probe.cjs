#!/usr/bin/env node
// Interactive manual smoke for the user's running system iPhone Mirroring app.
// Only explicit commands act; fixture matrix's "send" remains fixture-only.
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {Fixture, createProbe, run, pause} = require('./computer-use-background-probe.cjs');

function point(frame, x, y) {
  if (![x,y].every(n => Number.isFinite(n) && n >= 0 && n < 1)) throw new Error('normalized coordinates required');
  return {x: frame.x + frame.width * x, y: frame.y + frame.height * y};
}

function guardPlacement({frame, display}, covered) {
  if (covered) return frame;
  const right = frame.x + frame.width + 20;
  const x = right + 220 <= display.x + display.width ? right : frame.x - 240;
  if (x < display.x) throw new Error('no room for non-overlapping guard on mirror display');
  return {x, y: Math.max(display.y + 30, frame.y), width: 220, height: 180};
}

async function main() {
  if (process.platform !== 'darwin' || process.argv.length !== 2) throw new Error('macOS only; no arbitrary application targets');
  const {dir, binary, status, bundle} = createProbe();
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const mirrorState = () => JSON.parse(run(binary, ['mirror-window']));
  mirrorState(); // Fail before showing a guard if the system mirror is not running.
  const guard = new Fixture(bundle('guard'), 'guard');
  const records = [];
  let count = 0, capturedTarget;
  async function invoke(mode, input) {
    const child = spawn(binary, [mode], {stdio: ['pipe', 'pipe', 'pipe']});
    let output = '', error = '';
    child.stdout.on('data', chunk => output += chunk);
    child.stderr.on('data', chunk => error += chunk);
    child.stdin.end(JSON.stringify(input));
    const timer = setTimeout(() => child.kill('SIGTERM'), 10000);
    const [code] = await once(child, 'exit'); clearTimeout(timer);
    return {code, output, error};
  }
  async function capture(label) {
    capturedTarget = undefined;
    const target = mirrorState();
    const output = path.join(dir, `${++count}-${label}.png`);
    const result = await invoke('mirror-capture', {target, output});
    if (result.code !== 0) throw new Error(result.output || result.error);
    capturedTarget = target;
    return {output, target};
  }
  try {
    await guard.ready();
    const lines = readline.createInterface({input: process.stdin});
    console.log(JSON.stringify({ready: true, evidence: dir, commands: ['prepare', 'capture', 'send', 'quit'], mirror: mirrorState()}));
    for await (const line of lines) {
      try {
        const command = JSON.parse(line);
        if (command.op === 'quit') break;
        if (command.op === 'prepare') {
          const target = mirrorState();
          const placement = guardPlacement(target, command.covered === true);
          await guard.request('place', {frame: placement});
          await pause(200);
          console.log(JSON.stringify({prepared: true, covered: command.covered === true,
            guard: await guard.request('state'), capture: await capture('prepared')}));
        } else if (command.op === 'capture') {
          console.log(JSON.stringify({capture: await capture('observed')}));
        } else if (command.op === 'send') {
          if (!capturedTarget) throw new Error('capture a reference image first (manual smoke only)');
          const state = await guard.request('state');
          if (state.foregroundPID !== guard.child.pid) throw new Error(`foreground changed to ${state.foregroundPID}; no automatic reactivation`);
          const target = {...capturedTarget, points: {[command.action]: point(capturedTarget.frame, command.x, command.y)}};
          const request = {target, guardPID: guard.child.pid, variant: 'appkit-window-local', action: command.action};
          if (command.end) request.end = point(target.frame, command.end.x, command.end.y);
          if (command.scrollY !== undefined) request.scrollY = command.scrollY;
          const start = guard.events.length;
          await guard.request('monitor');
          const sender = await invoke('mirror-send', request);
          await pause(300);
          const monitor = await guard.request('stop-monitor');
          const untouched = !guard.events.slice(start).some(e => e.kind === 'event' || e.kind === 'effect');
          const isolated = monitor.samples > 0 && monitor.foregroundPIDs.length > 0 && monitor.foregroundPIDs.every(pid => pid === guard.child.pid)
            && monitor.activations.every(pid => pid === guard.child.pid) && monitor.maxCursorDistance < 0.5 && untouched;
          let after;
          try { after = await capture('after'); } catch (error) { after = {error: error.message}; }
          const result = {action: command.action, input: command, sender, isolated, monitor, guardUntouched: untouched,
            capture: after, uiEffect: 'requires visual verification; event delivery is not success'};
          records.push(result);
          fs.appendFileSync(path.join(dir, 'mirror-events.jsonl'), JSON.stringify(result) + '\n');
          console.log(JSON.stringify(result));
          if (!isolated) { console.log(JSON.stringify({stopped: 'isolation changed; no retry'})); break; }
        } else throw new Error('unknown manual probe command');
      } catch (error) { console.log(JSON.stringify({error: error.message})); }
    }
    lines.close();
  } finally {
    await guard.close(); // Never quit, move or activate the user's mirror app.
    fs.writeFileSync(path.join(dir, 'mirror-results.json'), JSON.stringify({status, records}, null, 2));
  }
}

module.exports = {point, guardPlacement};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
