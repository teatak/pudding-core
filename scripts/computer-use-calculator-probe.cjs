#!/usr/bin/env node
// Explicit side-by-side smoke: Codex clicks are made by CUA, never by this runner.
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const {Fixture, createProbe, run, pause} = require('./computer-use-background-probe.cjs');
const {guardPlacement} = require('./computer-use-mirror-probe.cjs');

function sevenPoint(target) {
  const matches = target.buttons.filter(b => b.id === 'Seven');
  if (matches.length !== 1) throw new Error('unique Seven button required');
  const {x, y} = matches[0], f = target.frame;
  if (![x, y].every(Number.isFinite) || x < 0 || y < 0 || x >= f.width || y >= f.height) throw new Error('Seven outside window');
  return {x: f.x + x, y: f.y + y};
}

function summarize(before, after, monitor, guardEvents, routed, guardPID) {
  const foregroundStable = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
    && monitor.foregroundPIDs.every(pid => pid === guardPID) && monitor.activations.every(pid => pid === guardPID);
  const cursorStable = monitor.samples > 0 && monitor.maxCursorDistance < 0.5;
  const guardUntouched = !guardEvents.some(e => e.kind === 'event' || e.kind === 'effect');
  const sameWindow = before.pid === after.pid && before.windowID === after.windowID
    && JSON.stringify(before.frame) === JSON.stringify(after.frame);
  const clicks = routed.filter(e => e.kind === 'routed-click');
  return {foregroundStable, cursorStable, guardUntouched, sameWindow,
    isolated: foregroundStable && cursorStable && guardUntouched && sameWindow,
    before: before.text, after: after.text, routedClicks: clicks,
    tapHealthy: !routed.some(e => ['tap-disabled', 'listener-deadline'].includes(e.kind))
      && routed.some(e => e.kind === 'listener-stopped'),
    // No events is not evidence of an unmodified click (AX actions need not route CGEvents).
    clickMetadataObserved: clicks.length > 0,
    uiEffect: 'compare AX readout and CUA screenshot; delivery alone is not success'};
}

async function main() {
  if (process.platform !== 'darwin' || process.argv.length !== 2) throw new Error('macOS Calculator only; no arbitrary targets');
  const {dir, binary, status, bundle} = createProbe();
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const state = () => JSON.parse(run(binary, ['calculator-window']));
  const initial = state();
  const guard = new Fixture(bundle('guard'), 'guard');
  const records = [];
  let pending, listener, prepared;
  const lines = readline.createInterface({input: process.stdin});
  const idle = setTimeout(() => lines.close(), 10 * 60_000);
  async function begin(arm) {
    if (pending || !prepared) throw new Error('prepare first; finish pending sample before starting another');
    const before = state(), g = await guard.request('state');
    if (!g.foregroundPID || g.foregroundPID === before.pid || before.active) throw new Error('Calculator must be background; no automatic activation');
    if (JSON.stringify(before.frame) !== JSON.stringify(prepared.frame)) throw new Error('window moved since prepare');
    sevenPoint(before);
    listener = new Fixture(binary, 'calculator-listen', ['calculator-listen']);
    await listener.ready();
    const guardStart = guard.events.length;
    await guard.request('monitor');
    pending = {arm, covered: prepared.covered, before, guardStart, foregroundPID: g.foregroundPID};
    return {armed: arm, before, foregroundPID: g.foregroundPID};
  }
  async function finish(sender) {
    if (!pending) throw new Error('no pending sample');
    await pause(200);
    const monitor = await guard.request('stop-monitor');
    const after = state();
    await listener.close();
    const record = {...pending, sender, after, monitor, routed: listener.events,
      guardEvents: guard.events.slice(pending.guardStart)};
    record.assessment = summarize(record.before, after, monitor, record.guardEvents, listener.events, pending.foregroundPID);
    delete record.guardStart;
    records.push(record);
    fs.appendFileSync(path.join(dir, 'calculator-events.jsonl'), JSON.stringify(record) + '\n');
    pending = undefined; listener = undefined;
    return record;
  }
  try {
    await guard.ready();
    console.log(JSON.stringify({ready: true, evidence: dir, initial, commands: ['prepare', 'begin-codex', 'finish-codex', 'prototype', 'quit']}));
    for await (const line of lines) {
      try {
        const c = JSON.parse(line);
        if (c.op === 'quit') break;
        if (c.op === 'prepare') {
          if (pending) throw new Error('finish sample first');
          const target = state();
          await guard.request('place', {frame: guardPlacement(target, c.covered === true)});
          await pause(200);
          prepared = {frame: target.frame, covered: c.covered === true};
          console.log(JSON.stringify({prepared, guard: await guard.request('state'), target}));
        } else if (c.op === 'begin-codex') {
          console.log(JSON.stringify(await begin('codex')));
        } else if (c.op === 'finish-codex') {
          if (pending?.arm !== 'codex') throw new Error('Codex sample not armed');
          console.log(JSON.stringify(await finish({executor: 'external CUA; verify its tool log'})));
        } else if (c.op === 'prototype') {
          await begin('pudding-prototype');
          const target = {...pending.before, points: {click: sevenPoint(pending.before)}};
          let sender;
          try { sender = {ok: true, output: run(binary, ['calculator-send'], {input: JSON.stringify({target,
            guardPID: pending.foregroundPID, variant: 'appkit-window-local', action: 'click'})})}; }
          catch (error) { sender = {ok: false, error: error.message}; }
          console.log(JSON.stringify(await finish(sender)));
        } else throw new Error('unknown command');
      } catch (error) {
        console.log(JSON.stringify({error: error.message}));
        if (listener) { await listener.close(); listener = undefined; }
        if (pending) { await guard.request('stop-monitor'); pending = undefined; }
      }
    }
  } finally {
    clearTimeout(idle); lines.close();
    if (listener) await listener.close();
    await guard.close(); // Never close, move or activate Calculator.
    fs.writeFileSync(path.join(dir, 'calculator-results.json'), JSON.stringify({status, initial, records}, null, 2));
  }
}

module.exports = {sevenPoint, summarize};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
