#!/usr/bin/env node
// Current-source Manager (or full Engine) -> Electron bridge -> real Helper.
// Calendar navigation only; no real daemon, release app replacement, or TCC reset.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {Fixture, createProbe, run, pause} = require('./computer-use-background-probe.cjs');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');
const {ComputerUseBridgeServer} = require('../electron/computer-use-bridge-server.cjs');
const {ComputerUsePermissionCoordinator} = require('../electron/computer-use-permissions.cjs');

async function main() {
  const binaryPath = process.argv[2];
  const mode = process.argv[3] || 'manager';
  const modes = {
    manager: {pkg:'computer', test:'TestBackgroundCalendarIntegration', evidence:'production-chain.json', timeout:20_000},
    engine: {pkg:'engine', test:'TestBackgroundCalendarEngineIntegration', evidence:'production-engine-chain.json', timeout:20_000},
    'engine-sessions': {pkg:'engine', test:'TestBackgroundCalendarEngineSessions', evidence:'production-engine-sessions.json', timeout:60_000, race:true},
  };
  const selected = Object.hasOwn(modes, mode) ? modes[mode] : null;
  if (process.platform !== 'darwin' || process.argv.length < 3 || process.argv.length > 4 || !selected || !path.isAbsolute(binaryPath) || !fs.statSync(binaryPath).isFile()) {
    throw new Error('usage: node scripts/computer-use-background-integration.cjs /absolute/path/to/current-source/PuddingComputerUseHelper [manager|engine|engine-sessions]');
  }
  const probe = createProbe();
  const host = new ComputerUseHost({binaryPath});
  let guard, bridge;
  try {
    const permissions = {refresh: async () => ({supported:true, ...await host.permissions()})};
    const state = await permissions.refresh();
    assert.ok(state.accessibility && state.screenRecording, 'existing permissions required; no prompts issued');
    const coordinator = new ComputerUsePermissionCoordinator({permissions,
      onGuideChange: guide => { if (guide) coordinator.deny(guide.requestID); }});
    bridge = new ComputerUseBridgeServer(host, {permissionCoordinator:coordinator});
    const identity = await bridge.start();
    const app = await host.useApp({bundleID:'com.apple.iCal', foreground:false});
    assert.equal(app.windowStatus, 'ready');
    assert.equal(app.windows.length, 1, 'one explicit Calendar window required');
    const window = app.windows[0];
    const testBinary = path.join(probe.dir, 'background-integration.test');
    run('go', ['test', ...(selected.race ? ['-race'] : []), '-tags', 'sqlite_fts5 webrtcaec', '-c', '-o', testBinary, `./internal/${selected.pkg}`],
      {cwd:path.resolve(__dirname, '..')});
    guard = new Fixture(probe.bundle('guard'), 'guard');
    await guard.ready();
    await guard.request('place', {frame:window.frame});
    await pause(250);
    assert.equal((await guard.request('state')).foregroundPID, guard.child.pid);
    const start = guard.events.length;
    await guard.request('monitor');
    const child = spawn(testBinary, ['-test.run', `^${selected.test}$`, '-test.v'], {
      stdio:['ignore','pipe','pipe'],
      env:{...process.env, PUDDING_CU_BACKGROUND_URL:identity.url, PUDDING_CU_BACKGROUND_TOKEN:identity.token,
        PUDDING_CU_BACKGROUND_WINDOW:String(window.windowID)},
    });
    let output = '';
    child.stdout.on('data', data => output += data);
    child.stderr.on('data', data => output += data);
    const timer = setTimeout(() => child.kill('SIGTERM'), selected.timeout);
    const [code] = await once(child, 'close');
    clearTimeout(timer);
    const monitor = await guard.request('stop-monitor');
    const received = guard.events.slice(start);
    const foregroundStable = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
      && monitor.foregroundPIDs.every(pid => pid === guard.child.pid) && monitor.activations.every(pid => pid === guard.child.pid);
    const cursorStable = monitor.samples > 0 && monitor.maxCursorDistance < 0.5;
    const guardUntouched = !received.some(e => e.kind === 'event' || e.kind === 'effect');
    const result = {chain:mode, race:!!selected.race, pass:code === 0 && foregroundStable && cursorStable && guardUntouched,
      code, foregroundStable, cursorStable, guardUntouched, monitor, output};
    fs.writeFileSync(path.join(probe.dir, selected.evidence), JSON.stringify(result, null, 2));
    console.log(JSON.stringify(result));
    assert.ok(result.pass, 'native integration failed; do not replay automatically');
  } finally {
    if (bridge) await bridge.stop();
    await host.stop();
    if (guard) await guard.close();
  }
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
