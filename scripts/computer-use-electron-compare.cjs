#!/usr/bin/env node
// Codex uses the actual CUA tool; this runner only sets up, measures and runs our arm.
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const {Fixture, createProbe, run, pause, assess} = require('./computer-use-background-probe.cjs');

function summarize(before, after, received, monitor, guardEvents, guardPID) {
  const isolation = assess('click', received, monitor, guardEvents, true, guardPID);
  const sameWindow = before.pid === after.pid && before.windowID === after.windowID
    && JSON.stringify(before.frame) === JSON.stringify(after.frame);
  const types = ['mousedown', 'mouseup', 'click'];
  const events = received.filter(e => e.kind === 'event');
  const ordinarySingleClick = types.every(type => {
    const matches = events.filter(e => e.type === type);
    return matches.length === 1 && matches[0].detail === 1 && matches[0].target === 'click';
  }) && !events.some(e => ['dblclick', 'contextmenu', 'wheel'].includes(e.type))
    && !received.some(e => e.kind === 'effect' && e.effect === 'button-double');
  const buttonEffects = received.filter(e => e.kind === 'effect' && e.effect === 'click').length;
  const isolated = isolation.foregroundStable && isolation.cursorStable && isolation.guardUntouched && sameWindow;
  return {...isolation, sameWindow, isolated, ordinarySingleClick, buttonEffects,
    pass: isolation.pass && sameWindow && ordinarySingleClick && before.buttonText === 'Increment' && after.buttonText === '1'};
}

function copyElectron(dir) {
  const source = path.resolve(__dirname, '../web/node_modules/electron/dist/Electron.app');
  const appPath = path.join(dir, 'Electron Probe.app');
  // Clone a build artifact, never modify the installed/source app's identity.
  run('/bin/cp', ['-cR', source, appPath]);
  const info = path.join(appPath, 'Contents/Info.plist');
  const plist = fs.readFileSync(info, 'utf8');
  if (!plist.includes('<string>com.github.Electron</string>')) throw new Error('unexpected source Electron identity');
  fs.writeFileSync(info, plist.replace('<string>com.github.Electron</string>', '<string>com.teatak.pudding.background-probe.electron</string>')
    .replace(/(<key>CFBundle(?:DisplayName|Name)<\/key>\s*)<string>Electron<\/string>/g, '$1<string>Pudding Electron Probe</string>'));
  run('codesign', ['--force', '--sign', '-', '--preserve-metadata=entitlements', appPath]);
  return {source, appPath, executable: path.join(appPath, 'Contents/MacOS/Electron')};
}

async function main() {
  if (process.platform !== 'darwin' || process.argv.length !== 2) throw new Error('macOS owned Electron comparison only');
  const probe = createProbe(), {dir, binary, status} = probe;
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const electron = copyElectron(dir);
  const guard = new Fixture(probe.bundle('guard'), 'guard');
  const records = [], lines = readline.createInterface({input: process.stdin});
  const deadline = setTimeout(() => lines.close(), 10 * 60_000);
  let target, prepared, pending, sequence = 0;
  const state = async () => {
    const value = await target.request('state');
    return {...value, ...JSON.parse(run(binary, ['window'], {input: JSON.stringify(value)}))};
  };
  async function begin() {
    if (!prepared || pending) throw new Error('prepare a fresh target, then finish each sample once');
    const before = await state();
    await guard.request('place', {frame: prepared.covered ? before.frame : {...before.frame, x: before.frame.x + before.frame.width + 60}});
    await pause(200); // Setup settles before measurement, not an input retry.
    const g = await guard.request('state'), current = await state();
    if (g.foregroundPID !== guard.child.pid || current.active) throw new Error('guard must be foreground; no input sent');
    if (current.buttonText !== 'Increment' || current.acceptFirstMouse) throw new Error('fresh default receiver required');
    pending = {arm: prepared.arm, covered: prepared.covered, before: current,
      targetStart: target.events.length, guardStart: guard.events.length};
    await guard.request('monitor');
    return {armed: pending.arm, target: current, guardPID: guard.child.pid};
  }
  async function finish(sender) {
    if (!pending) throw new Error('no sample armed');
    await pause(200);
    const monitor = await guard.request('stop-monitor'), after = await state();
    const {targetStart, guardStart, ...sample} = pending;
    const received = target.events.slice(targetStart), guardEvents = guard.events.slice(guardStart);
    const record = {...sample, after, received, monitor, guardEvents, sender,
      assessment: summarize(sample.before, after, received, monitor, guardEvents, guard.child.pid)};
    record.assessment.pass &&= sender.ok !== false;
    records.push(record);
    fs.appendFileSync(path.join(dir, 'electron-compare.jsonl'), JSON.stringify(record) + '\n');
    pending = prepared = undefined;
    return record;
  }
  try {
    await guard.ready();
    console.log(JSON.stringify({ready: true, evidence: dir, electron,
      commands: ['prepare {arm:codex|prototype,covered:boolean}', 'begin-codex', 'finish-codex', 'prototype', 'quit']}));
    for await (const line of lines) {
      try {
        const c = JSON.parse(line);
        if (c.op === 'quit') break;
        if (c.op === 'prepare') {
          if (pending || !['codex', 'prototype'].includes(c.arm)) throw new Error('finish sample first; explicit arm required');
          if (target) await target.close();
          const cell = path.join(dir, `target-${++sequence}`); fs.mkdirSync(cell);
          target = new Fixture(electron.executable, 'electron', [path.resolve(__dirname, '../electron/smoke/background-input-fixture.cjs'), cell, 'default']);
          await target.ready(); await target.request('position');
          prepared = {arm: c.arm, covered: c.covered === true};
          console.log(JSON.stringify({prepared, appPath: electron.appPath, target: await state()}));
        } else if (c.op === 'begin-codex') {
          if (prepared?.arm !== 'codex') throw new Error('prepare Codex target first');
          console.log(JSON.stringify(await begin()));
        } else if (c.op === 'finish-codex') {
          if (pending?.arm !== 'codex') throw new Error('Codex not armed');
          console.log(JSON.stringify(await finish({executor: 'external CUA; verify tool log'})));
        } else if (c.op === 'prototype') {
          if (prepared?.arm !== 'prototype') throw new Error('prepare prototype target first');
          await begin();
          let sender;
          try { sender = {ok: true, output: run(binary, ['send'], {input: JSON.stringify({target: pending.before,
            guardPID: guard.child.pid, variant: 'appkit-window-local', action: 'click'})})}; }
          catch (error) { sender = {ok: false, error: error.message}; }
          console.log(JSON.stringify(await finish(sender)));
        } else throw new Error('unknown command');
      } catch (error) {
        console.log(JSON.stringify({error: error.message}));
        if (pending) { await guard.request('stop-monitor'); pending = prepared = undefined; }
      }
    }
  } finally {
    clearTimeout(deadline); lines.close();
    if (target) await target.close();
    await guard.close();
    fs.writeFileSync(path.join(dir, 'electron-compare.json'), JSON.stringify({status, electron, records}, null, 2));
  }
}

module.exports = {summarize, copyElectron};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
