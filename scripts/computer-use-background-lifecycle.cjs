#!/usr/bin/env node
// Faults affect only Helpers/workers spawned here. Calendar navigation only.
// No production test hooks, policy bypass, permission reset, or input replay.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {execFileSync} = require('node:child_process');
const {Fixture, createProbe, pause} = require('./computer-use-background-probe.cjs');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');
const {ComputerUseBridgeServer} = require('../electron/computer-use-bridge-server.cjs');
const {ComputerUsePermissionCoordinator} = require('../electron/computer-use-permissions.cjs');

const scenarios = ['normal', 'worker-1', 'worker-2', 'worker-3',
  'helper-1', 'helper-2', 'helper-3', 'cancel', 'concurrent'];
const appID = 'com.apple.iCal';

function calendarMonth(observation) {
  const months = observation.elements.filter(e => e.role === 'AXStaticText' && /^\d{4}年\d{1,2}月$/.test(e.value ?? ''));
  assert.equal(months.length, 1, 'unique Chinese Calendar month heading required');
  const [, year, month] = months[0].value.match(/^(\d{4})年(\d{1,2})月$/);
  return Number(year) * 12 + Number(month) - 1;
}

function assess(record) {
  const {scenario, before, after, finalMonth, outcome, monitor, guardEvents, checkpoint, phaseAfter} = record;
  const expectedDelta = ['worker-1', 'helper-1'].includes(scenario) ? 0 : 1;
  const correctEffect = after - before === expectedDelta && finalMonth === before;
  const fault = /^(worker|helper)-/.test(scenario);
  const correctOutcome = fault ? outcome?.outcome === 'unknown' && !outcome?.completed && checkpoint?.signal === 'SIGKILL'
    : scenario === 'cancel' ? outcome?.code === 'computer_action_cancelled' && outcome?.outcome === 'unknown'
      : outcome?.completed === true && outcome?.delivery === 'background';
  const concurrent = scenario !== 'concurrent' || (record.contender?.code === 'computer_input_busy'
    && record.contender?.outcome === 'not_started' && !record.contender?.retryable);
  const isolated = monitor?.samples > 0 && monitor.foregroundPIDs?.length > 0
    && monitor.foregroundPIDs.every(pid => pid === monitor.pid)
    && monitor.activations.length === 0 && monitor.maxCursorDistance === 0
    && !guardEvents.some(e => ['event', 'effect'].includes(e.kind));
  const checkpointValid = scenario === 'normal' || checkpoint?.phase === (Number(scenario.split('-')[1]) || (scenario === 'cancel' ? 2 : 1));
  const recovered = phaseAfter === 0;
  return {correctEffect, correctOutcome, concurrent, isolated, checkpointValid, recovered,
    pass: correctEffect && correctOutcome && concurrent && isolated && checkpointValid && recovered};
}

function journalFor(pid) {
  const dir = path.join(os.tmpdir(), 'pudding-computer-input');
  const files = fs.existsSync(dir) ? fs.readdirSync(dir).filter(f => new RegExp(`^${pid}-[0-9]+-[0-9]+\\.state$`).test(f)) : [];
  assert.ok(files.length <= 1, 'ambiguous process-start journal; stop without changing it');
  return files.length ? path.join(dir, files[0]) : null;
}
function phase(pid) {
  const file = journalFor(pid);
  if (!file) return 0;
  const data = fs.readFileSync(file);
  assert.equal(data.length, 1, 'invalid journal');
  assert.ok((data[0] & 3) === 2 || data[0] <= 3, 'invalid recovery checkpoint');
  return data[0] & 3;
}
async function until(check, description, timeout = 3500) {
  const deadline = Date.now() + timeout;
  while (true) {
    const value = check();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timeout: ${description}; no replay`);
    await pause(1);
  }
}
function ownedWorker(helperPID, binaryPath) {
  let pids;
  try { pids = execFileSync('/usr/bin/pgrep', ['-P', String(helperPID)], {encoding:'utf8'}).trim().split(/\s+/).map(Number); }
  catch (error) { if (error.status === 1) return null; throw error; }
  assert.equal(pids.length, 1, 'one child worker required');
  const pid = pids[0];
  const line = execFileSync('/bin/ps', ['-p', String(pid), '-o', 'ppid=,command='], {encoding:'utf8'}).trim();
  assert.equal(line, `${helperPID} ${binaryPath} background-pointer-worker`, 'only exact child worker can receive a signal');
  return pid;
}
function safeResult(error) {
  return {code:error.code, outcome:error.outcome, retryable:error.retryable, message:error.message};
}

async function endpoint(binaryPath) {
  const host = new ComputerUseHost({binaryPath});
  let bridge;
  try {
    const permissions = {refresh:async () => ({supported:true, ...await host.permissions()})};
    const state = await permissions.refresh();
    assert.ok(state.accessibility && state.screenRecording, 'existing permissions required; no request issued');
    const coordinator = new ComputerUsePermissionCoordinator({permissions,
      onGuideChange:guide => { if (guide) coordinator.deny(guide.requestID); }});
    bridge = new ComputerUseBridgeServer(host, {permissionCoordinator:coordinator});
    const identity = await bridge.start();
    return {host, bridge, async call(route, body) {
      const response = await fetch(identity.url + route, {method:'POST',
        headers:{authorization:`Bearer ${identity.token}`, 'content-type':'application/json'}, body:JSON.stringify(body)});
      return response.json();
    }, async close() { await bridge.stop(); await host.stop(); }};
  } catch (error) {
    if (bridge) await bridge.stop();
    await host.stop();
    throw error;
  }
}

async function runCase(probe, binaryPath, guard, scenario) {
  const record = {scenario};
  let first, second, operation, suspendedWorker, targetPID, monitoring = false, guardStart = 0;
  try {
    first = await endpoint(binaryPath);
    if (scenario === 'concurrent') second = await endpoint(binaryPath);
    const app = await first.host.useApp({bundleID:appID, foreground:false});
    assert.equal(app.windowStatus, 'ready'); assert.equal(app.windows.length, 1);
    targetPID = app.pid;
    assert.equal(phase(targetPID), 0, 'prior input must be resolved; journal is read-only to the runner');
    const windowID = app.windows[0].windowID;
    const observe = () => first.host.observe({bundleID:appID, windowID, maxElements:60});
    const observation = await observe();
    record.before = calendarMonth(observation);
    const frame = app.windows[0].frame;
    function pointer(description) {
      const found = observation.elements.filter(e => e.description === description && e.frame && !e.secure);
      assert.equal(found.length, 1, `unique ${description} toolbar control required`);
      const e = found[0].frame;
      return {appID, windowID, action:'click', delivery:'background',
        x:(e.x + e.width / 2 - frame.x) / frame.width, y:(e.y + e.height / 2 - frame.y) / frame.height};
    }
    const next = pointer('下一月'), previous = pointer('上一月');
    await guard.request('place', {frame});
    await pause(150);
    assert.equal((await guard.request('state')).foregroundPID, guard.child.pid);
    guardStart = guard.events.length;
    await guard.request('monitor'); monitoring = true;
    const controller = new AbortController();
    const helper = first.host.child;
    let settled = false;
    // Cancellation uses the real Host signal so its uncertain outcome is observable.
    // Other writes exercise the existing authenticated, session-scoped bridge.
    operation = (scenario === 'cancel'
      ? first.host.pointer({...next, bundleID:appID}, {signal:controller.signal})
      : first.call('/computer/pointer', {...next, sessionID:'background-lifecycle-first'}))
      .catch(safeResult)
      .then(result => { settled = true; record.outcome = result; return result; });
    if (scenario !== 'normal') {
      const desired = Number(scenario.split('-')[1]) || (scenario === 'cancel' ? 2 : 1);
      // Identity lookup is done during the longer activation stage, not after down.
      const worker = await until(() => {
        if (settled) throw new Error('operation ended before checkpoint');
        return phase(targetPID) !== 0 && ownedWorker(helper.pid, binaryPath);
      }, 'owned worker');
      await until(() => {
        if (settled) throw new Error('checkpoint missed');
        return phase(targetPID) === desired;
      }, `journal phase ${desired}`);
      record.checkpoint = {phase:phase(targetPID), helperPID:helper.pid, workerPID:worker};
      if (scenario.startsWith('worker-')) {
        process.kill(worker, 'SIGKILL'); record.checkpoint.signal = 'SIGKILL';
      } else if (scenario.startsWith('helper-')) {
        // Do not stop the worker before killing its owner; it must process EOF.
        assert.ok(helper.kill('SIGKILL')); record.checkpoint.signal = 'SIGKILL';
      } else if (scenario === 'cancel') controller.abort();
      else {
        process.kill(worker, 'SIGSTOP'); suspendedWorker = worker;
        assert.equal(phase(targetPID), desired, 'worker advanced before suspension');
        record.contender = await second.call('/computer/pointer', {...next, sessionID:'background-lifecycle-second'});
        process.kill(worker, 'SIGCONT'); suspendedWorker = null;
      }
    }
    await operation;
    await until(() => phase(targetPID) === 0, 'recovered journal');
    record.phaseAfter = phase(targetPID);
    record.after = calendarMonth(await observe());
    assert.ok(record.after === record.before || record.after === record.before + 1, 'unexpected month; stop, no compensating click');
    // This is a new, observed reverse navigation, never replay of the failed click.
    if (record.after === record.before + 1) {
      assert.equal((await guard.request('state')).foregroundPID, guard.child.pid, 'do not restore against changed foreground');
      record.restore = await first.call('/computer/pointer', {...previous, sessionID:'background-lifecycle-first'});
      assert.equal(record.restore.completed, true, 'restoration delivery failed; do not repeat');
    }
    record.finalMonth = calendarMonth(await observe());
    record.monitor = await guard.request('stop-monitor'); monitoring = false;
    record.guardEvents = guard.events.slice(guardStart).filter(e => ['event', 'effect', 'workspace-activation'].includes(e.kind));
    record.assessment = assess(record);
    console.log(JSON.stringify({scenario, ...record.assessment, outcome:record.outcome,
      delta:record.after-record.before, samples:record.monitor.samples, cursor:record.monitor.maxCursorDistance}));
    assert.ok(record.assessment.pass, `${scenario} failed; stopping without replay`);
  } catch (error) { record.error = error.message; throw error; }
  finally {
    if (suspendedWorker) { try { process.kill(suspendedWorker, 'SIGCONT'); } catch {} }
    if (operation) { try { await operation; } catch {} }
    if (monitoring) { try { record.monitor = await guard.request('stop-monitor'); } catch {} }
    if (targetPID) { try { await until(() => phase(targetPID) === 0, 'cleanup', 6000); } catch (error) { record.cleanupError = error.message; } }
    if (second) await second.close();
    if (first) await first.close();
    fs.writeFileSync(path.join(probe.dir, `production-lifecycle-${scenario}.json`), JSON.stringify(record, null, 2));
  }
}

async function main() {
  const [binaryPath, selected] = process.argv.slice(2);
  assert.ok(process.platform === 'darwin' && process.argv.length === 4 && path.isAbsolute(binaryPath)
    && fs.statSync(binaryPath).isFile() && (selected === 'all' || scenarios.includes(selected)),
  'usage: node scripts/computer-use-background-lifecycle.cjs /absolute/current/Helper all|scenario');
  const probe = createProbe();
  assert.ok(probe.status.accessibility && probe.status.postEvents && probe.status.screenCapture, 'existing permissions required');
  const guard = new Fixture(probe.bundle('guard'), 'guard');
  try {
    await guard.ready();
    for (const scenario of selected === 'all' ? scenarios : [selected]) await runCase(probe, binaryPath, guard, scenario);
  } finally { await guard.close(); }
  console.log(JSON.stringify({evidence:probe.dir, productionReady:false}));
}
module.exports = {assess, calendarMonth, scenarios, journalFor, phase, until, ownedWorker, safeResult};
if (require.main === module) main().catch(error => { console.error(error.message); process.exitCode = 1; });
