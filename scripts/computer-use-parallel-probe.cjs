#!/usr/bin/env node
// Test-only callers and human foreground input. No Pudding session/daemon access.
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const readline = require('node:readline');
const {Fixture, createProbe, pause} = require('./computer-use-background-probe.cjs');

const expectedText = 'pudding 1234567890';

function intervals(senders) {
  return senders.flatMap(s => {
    const a = s.events.find(e => e.kind === 'focus-notification' && e.activated);
    const b = s.events.find(e => e.kind === 'focus-notification' && !e.activated);
    return a && b && Number.isFinite(a.monotonicTime) && Number.isFinite(b.monotonicTime)
      && b.monotonicTime > a.monotonicTime ? [[a.monotonicTime, b.monotonicTime]] : [];
  });
}

function receiverActivationIntervals(events) {
  let start;
  const result = [];
  for (const e of events) {
    if (e.kind !== 'focus-event' || !Number.isFinite(e.monotonicTime)) continue;
    if (e.phase === 'after' && e.subtype === 1 && e.active) start = e.monotonicTime;
    if (e.phase === 'before' && e.subtype === 2 && start !== undefined) {
      if (e.monotonicTime > start) result.push([start, e.monotonicTime]);
      start = undefined;
    }
  }
  return result;
}

function assessParallel(r) {
  const {before, after, monitor, guardAfter, received, guardEvents, senders, mode} = r;
  const spans = intervals(senders);
  const interrupted = mode === 'contention-worker-killed' || mode === 'contention-sender-killed';
  const completed = senders.length > 0 && senders.every(s => {
    const posted = s.events.filter(e => e.kind === 'input-posted').map(e => e.type).join(',');
    if (interrupted && s.index === r.killedCaller) {
      const exited = mode === 'contention-worker-killed' ? s.exit?.[0] === 1
        && s.events.some(e => e.kind === 'lease-worker-exit' && e.waitStatus !== 0)
        : s.exit?.[1] === 'SIGKILL';
      return exited && posted === '1' && !s.events.some(e => e.kind === 'sent')
        && s.events.filter(e => e.kind === 'cleanup' && e.event === 'button-up').length === 1;
    }
    return s.exit?.[0] === 0 && s.events.filter(e => e.kind === 'sent' && e.count === 2).length === 1
      && posted === '1,2' && !s.events.some(e => e.kind === 'cleanup');
  }) && spans.length === senders.length;
  const contentionExercised = !mode.startsWith('contention') || (senders.length === 2
    && senders.some(s => s.events.some(e => e.kind === 'lease-wait')));
  const effects = received.filter(e => e.kind === 'effect');
  const buttons = received.filter(e => e.kind === 'button-received');
  const exactClicks = after.count - before.count === senders.length && effects.length === senders.length
    && buttons.length === senders.length && buttons.every(e => e.flags === 0 && e.clickCount === 1)
    && effects.every(e => e.effect === 'click' && e.clickCount === 1 && !e.command);
  const targetInput = received.filter(e => e.kind === 'event');
  const noCrossInput = targetInput.every(e => [1, 2].includes(e.type) && e.flags === 0 && e.clickCount === 1)
    && !guardEvents.some(e => e.kind === 'effect');
  const foregroundStable = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
    && monitor.foregroundPIDs.every(pid => pid === monitor.pid) && monitor.activations.length === 0;
  const restored = !after.active && !after.keyWindow && after.foregroundPID === monitor.pid
    && guardAfter.active && guardAfter.keyWindow;
  const sameWindow = before.pid === after.pid && before.windowID === after.windowID
    && JSON.stringify(before.frame) === JSON.stringify(after.frame);
  const overlap = spans.some((a, i) => spans.slice(i + 1).some(b => Math.max(a[0], b[0]) < Math.min(a[1], b[1])));
  const keyboard = guardEvents.filter(e => e.kind === 'event' && e.type === 10);
  const receiverSpans = receiverActivationIntervals(received);
  const humanOverlap = keyboard.some(e => receiverSpans.some(([a, b]) => a <= e.monotonicTime && e.monotonicTime <= b));
  const inputVerified = mode === 'human'
    ? guardAfter.inputText === expectedText && keyboard.length >= expectedText.length && humanOverlap
      && monitor.maxCursorDistance > 10 && r.durationMs >= 30_000
    : guardAfter.inputText === '' && !guardEvents.some(e => e.kind === 'event') && monitor.maxCursorDistance === 0;
  return {completed, contentionExercised, exactClicks, noCrossInput, foregroundStable, restored, sameWindow, overlap, humanOverlap, inputVerified,
    pass: completed && contentionExercised && exactClicks && noCrossInput && foregroundStable && restored && sameWindow && inputVerified && !overlap};
}

function send(probe, request, record, index) {
  const child = spawn(probe.binary, ['focus-probe-send'], {stdio: ['pipe', 'pipe', 'pipe']});
  const entry = {index, pid: child.pid, events: []};
  record.senders.push(entry);
  const done = once(child, 'close').then(exit => { entry.exit = exit; return entry; });
  child.stderr.on('data', data => { entry.stderr = (entry.stderr ?? '') + data; });
  readline.createInterface({input: child.stdout}).on('line', line => entry.events.push(JSON.parse(line)));
  child.stdin.end(JSON.stringify(request));
  // Bound owned process lifetime, never replay on failure.
  const timer = setTimeout(() => child.kill('SIGTERM'), 5000);
  return done.finally(() => clearTimeout(timer));
}

async function main() {
  const mode = process.argv[2];
  if (process.platform !== 'darwin' || process.argv.length !== 3
    || !['contention', 'contention-worker-killed', 'contention-sender-killed', 'human'].includes(mode)) {
    throw new Error('usage: node scripts/computer-use-parallel-probe.cjs contention|contention-worker-killed|contention-sender-killed|human; owned fixtures only');
  }
  const probe = createProbe();
  if (!probe.status.accessibility || !probe.status.postEvents || !probe.status.screenCapture) throw new Error('existing permissions required; no request issued');
  const target = new Fixture(probe.bundle('target'), 'target', ['target', 'first-mouse-off']);
  const guard = new Fixture(probe.bundle('guard'), 'guard');
  const record = {mode, senders: []};
  let activeSends = [];
  try {
    await target.ready(); await guard.ready();
    await guard.request('place', {frame: (await target.request('state')).frame});
    if (mode === 'human') {
      await guard.request('human-prepare');
      console.log(JSON.stringify({ready: true, instructions: 'Click 开始 30 秒测试 in the owned window; type pudding 1234567890 once and move the mouse inside this window.'}));
      const deadline = Date.now() + 180_000;
      while (!guard.events.some(e => e.kind === 'human-start')) {
        if (Date.now() > deadline || guard.child.exitCode !== null) throw new Error('human test not started');
        await pause(100);
      }
    } else { await guard.request('input-focus'); await pause(200); }
    record.before = await target.request('state');
    if (record.before.active || record.before.keyWindow || record.before.foregroundPID !== guard.child.pid) throw new Error('inactive receiver and foreground guard required');
    const start = target.events.length, guardStart = guard.events.length;
    await guard.request('monitor');
    const request = {target: record.before, guardPID: guard.child.pid, variant: 'appkit-window-local', action: 'click'};
    if (mode.startsWith('contention')) {
      activeSends = [send(probe, request, record, 1), send(probe, request, record, 2)];
      if (mode !== 'contention') {
        const deadline = Date.now() + 3000;
        let owner;
        while (!owner) {
          owner = record.senders.find(s => s.events.some(e => e.kind === 'input-posted' && e.type === 1));
          if (!target.events.slice(start).some(e => e.kind === 'button-received')) owner = undefined;
          if (Date.now() > deadline) throw new Error('receiver did not reach concurrent crash checkpoint');
          if (!owner) await pause(2);
        }
        const lease = owner.events.find(e => e.kind === 'lease-start' && e.ownerPID === owner.pid);
        if (!lease || !Number.isInteger(lease.leasePID) || lease.leasePID <= 1) throw new Error('owned worker identity missing');
        const victim = mode === 'contention-worker-killed' ? lease.leasePID : owner.pid;
        process.kill(victim, 'SIGKILL');
        record.killedCaller = owner.index; record.killedPID = victim;
      }
      await Promise.all(activeSends);
    } else {
      const began = Date.now();
      while (Date.now() - began < 30_000) {
        const seconds = Math.max(1, Math.ceil((30_000 - (Date.now() - began)) / 1000));
        await guard.request('human-status', {text: `剩余 ${seconds} 秒\n请输入一次：${expectedText}\n在本窗口内移动鼠标，不要切换应用。`});
        const result = await send(probe, request, record, record.senders.length + 1);
        if (result.exit[0] !== 0) throw new Error('input failed; human test stopped without replay');
        await pause(120);
      }
      record.durationMs = Date.now() - began;
    }
    await pause(200);
    record.after = await target.request('state'); record.monitor = await guard.request('stop-monitor');
    record.guardAfter = await guard.request('state');
    record.received = target.events.slice(start); record.guardEvents = guard.events.slice(guardStart);
    record.assessment = assessParallel(record);
    console.log(JSON.stringify({mode, clicks: record.after.count - record.before.count, callers: record.senders.length, ...record.assessment}));
    if (!record.assessment.pass) process.exitCode = 1;
    if (mode === 'human') {
      await guard.request('human-status', {text: `测试结束，可停止输入。\n后台点击 ${record.after.count} 次。\n详细核对结果会在聊天中说明。`});
      await pause(2000);
    }
  } catch (error) { record.error = error.message; throw error; }
  finally {
    await Promise.allSettled(activeSends);
    record.targetLog = target.events; record.guardLog = guard.events;
    await target.close(); await guard.close();
    fs.writeFileSync(path.join(probe.dir, `parallel-${mode}.json`), JSON.stringify(record, null, 2));
    console.log(JSON.stringify({evidence: probe.dir, productionReady: false}));
  }
}

module.exports = {assessParallel, intervals};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
