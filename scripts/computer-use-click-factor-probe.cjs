#!/usr/bin/env node
// Orthogonal click diagnostics against owned fixtures only. Never a product fallback.
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {Fixture, createProbe, run, pause, assess} = require('./computer-use-background-probe.cjs');

function cases() {
  return [1, 2].flatMap(pairs => [1, 2].flatMap(lastCount => [1, 2].map(lastNumber => ({
    id: `pairs${pairs}-count${lastCount}-number${lastNumber}`,
    clicks: Array.from({length: pairs}, (_, i) => ({
      clickCount: i === pairs - 1 ? lastCount : 1,
      eventNumber: i === pairs - 1 ? lastNumber : 1,
    })),
  }))));
}

function summarize(received) {
  return {
    buttonEffects: received.filter(e => e.kind === 'effect' && e.effect === 'click').length,
    buttonDoubles: received.filter(e => e.kind === 'effect' && e.effect === 'button-double').length,
    events: received.filter(e => e.kind === 'event').map(e => ({type: e.type,
      clickCount: e.clickCount ?? e.detail, eventNumber: e.eventNumber, target: e.target})),
    buttonReceived: received.filter(e => e.kind === 'button-received'),
    firstMouseChecks: received.filter(e => e.kind === 'first-mouse'),
  };
}

async function main() {
  const receiver = process.argv[2];
  if (process.platform !== 'darwin' || process.argv.length !== 3 || !['appkit', 'electron'].includes(receiver)) {
    throw new Error('usage: node scripts/computer-use-click-factor-probe.cjs appkit|electron; owned fixtures only');
  }
  const {dir, binary, status, bundle} = createProbe();
  if (!status.accessibility || !status.postEvents || !status.screenCapture) throw new Error('existing permissions required; no request issued');
  const targetPath = bundle('target'), guardPath = bundle('guard');
  const records = [];
  const guard = new Fixture(guardPath, 'guard');
  try {
    await guard.ready();
    for (const firstMouse of [false, true]) {
      for (const covered of [false, true]) {
        for (const cell of cases()) {
          // A fresh receiving process for every cell prevents discarded click
          // history / renderer state from leaking into the next factor sample.
          const cellDir = path.join(dir, `${firstMouse}-${covered}-${cell.id}`);
          fs.mkdirSync(cellDir);
          const target = receiver === 'appkit'
            ? new Fixture(targetPath, 'target', ['target', firstMouse ? 'first-mouse-on' : 'first-mouse-off'])
            : new Fixture(path.resolve(__dirname, '../web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'),
              'electron', [path.resolve(__dirname, '../electron/smoke/background-input-fixture.cjs'), cellDir,
                firstMouse ? 'click-through' : 'default']);
          try {
            await target.ready();
            // Controlled setup activates owned windows, not the sender and not a retry.
            await target.request('position', {covered});
            await guard.request('position', {covered});
            await pause(200);
            const before = await guard.request('state');
            if (before.foregroundPID !== guard.child.pid) throw new Error('foreground changed during setup; stopping');
            const state = await target.request('state');
            if (receiver === 'electron') Object.assign(state, JSON.parse(run(binary, ['window'], {input: JSON.stringify(state)})));
            if (state.active) throw new Error('target not background');
            const targetStart = target.events.length, guardStart = guard.events.length;
            await guard.request('monitor');
            const child = spawn(binary, ['click-probe-send'], {stdio: ['pipe', 'pipe', 'pipe']});
            let output = '', error = '';
            child.stdout.on('data', v => output += v);
            child.stderr.on('data', v => error += v);
            child.stdin.end(JSON.stringify({request: {target: state, guardPID: guard.child.pid,
              action: 'click', variant: 'appkit-window-local'}, clicks: cell.clicks}));
            const timer = setTimeout(() => child.kill('SIGTERM'), 5000);
            const [code] = await once(child, 'exit'); clearTimeout(timer);
            await pause(150);
            const monitor = await guard.request('stop-monitor');
            const received = target.events.slice(targetStart), guardEvents = guard.events.slice(guardStart);
            const assessment = assess('click', received, monitor, guardEvents, code === 0, guard.child.pid);
            const scene = output.trim().split('\n').filter(Boolean).map(s => JSON.parse(s)).find(e => e.kind === 'scene');
            const coverageVerified = scene?.topmostNormalWindowPID === (covered ? guard.child.pid : target.child.pid);
            const validExperiment = code === 0 && assessment.foregroundStable && assessment.cursorStable
              && assessment.guardUntouched && !assessment.modified && assessment.trusted && coverageVerified;
            const result = {receiver, firstMouse, covered, ...cell, validExperiment, targetPID: target.child.pid,
              ...summarize(received), monitor, received, guardEvents, sender: {code, output, error}, coverageVerified};
            records.push(result);
            fs.appendFileSync(path.join(dir, 'click-factors.jsonl'), JSON.stringify(result) + '\n');
            console.log(JSON.stringify({receiver, firstMouse, covered, id: cell.id, validExperiment,
              buttonEffects: result.buttonEffects, buttonDoubles: result.buttonDoubles,
              firstMouseChecks: result.firstMouseChecks.map(e => ({accepted: e.accepted, clickCount: e.clickCount})),
              eventCounts: result.events.map(e => e.clickCount)}));
            if (!validExperiment) throw new Error('isolation/delivery changed; stopping without retry');
          } finally { await target.close(); }
        }
      }
    }
  } finally {
    await guard.close();
    fs.writeFileSync(path.join(dir, 'click-factors.json'), JSON.stringify({status, receiver, records}, null, 2));
  }
  console.log(JSON.stringify({complete: true, cases: records.length, evidence: dir, productionReady: false}));
}

module.exports = {cases, summarize};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
