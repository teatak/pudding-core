#!/usr/bin/env node
// Isolated native experiment, not a Pudding tool or a production fallback.
const {spawn, spawnSync} = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const readline = require('node:readline');
const {once} = require('node:events');

const root = path.resolve(__dirname, '..');
const variants = ['quartz', 'appkit', 'appkit-local-factory', 'appkit-window-local', 'appkit-command'];
const actions = ['click', 'double', 'right', 'drag', 'scroll'];
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

function assess(action, events, monitor, guardEvents, sendOK, guardPID) {
  const effects = events.filter(e => e.kind === 'effect' && e.effect === action);
  const effect = effects.length === 1 && effects.every(e => action !== 'scroll' || e.after !== e.before);
  const rawEvents = events.filter(e => e.kind === 'event');
  const trusted = rawEvents.every(e => e.trusted !== false && (typeof e.type !== 'string' || e.trusted === true));
  const modified = [...rawEvents, ...effects].some(e => e.command || (e.flags & 0x1e0000));
  const foregroundStable = monitor.samples > 0 && monitor.foregroundPIDs.length > 0
    && monitor.foregroundPIDs.every(pid => pid === guardPID)
    && monitor.activations.every(pid => pid === guardPID);
  const cursorStable = monitor.samples > 0 && monitor.maxCursorDistance < 0.5;
  const guardUntouched = !guardEvents.some(e => e.kind === 'effect' || e.kind === 'event');
  return {pass: sendOK && effect && rawEvents.length > 0 && trusted && !modified && foregroundStable && cursorStable && guardUntouched,
    effect, rawEventCount: rawEvents.length, trusted, modified, foregroundStable, cursorStable, guardUntouched};
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {encoding: 'utf8', timeout: 60_000, ...options});
  if (result.error || result.status !== 0) throw new Error(`${command}: ${result.error || result.stderr || result.stdout}`);
  return result.stdout.trim();
}

class Fixture {
  constructor(executable, role, args = [role]) {
    this.events = []; this.id = 0; this.pending = new Map();
    this.child = spawn(executable, args, {stdio: ['pipe', 'pipe', 'pipe'], env: {...process.env, ELECTRON_RUN_AS_NODE: ''}});
    this.child.stderr.on('data', data => process.stderr.write(`[${role}] ${data}`));
    readline.createInterface({input: this.child.stdout}).on('line', line => {
      let event;
      try { event = JSON.parse(line); } catch { return; }
      this.events.push(event);
      if (event.kind === 'reply') this.pending.get(event.id)?.(event);
    });
  }
  async ready() {
    const end = Date.now() + 5000;
    while (!this.events.some(e => e.kind === 'ready')) {
      if (Date.now() > end || this.child.exitCode !== null) throw new Error('fixture did not start');
      await pause(30);
    }
  }
  request(op, fields = {}) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`fixture timeout: ${op}`)); }, 3000);
      this.pending.set(id, result => { clearTimeout(timer); this.pending.delete(id); resolve(result); });
      this.child.stdin.write(JSON.stringify({id, op, ...fields}) + '\n');
    });
  }
  async close() {
    if (this.child.exitCode !== null) return;
    this.child.stdin.end(); // Native fixture exits on EOF; only our child is affected.
    const timer = setTimeout(() => this.child.kill('SIGTERM'), 1500);
    await once(this.child, 'exit');
    clearTimeout(timer);
  }
}

function createProbe() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-background-probe-'));
  console.log(`Evidence directory: ${dir}`);
  const binary = path.join(dir, 'probe');
  run('swiftc', ['-module-cache-path', path.join(dir, 'modules'),
    path.join(root, 'native/macos/background-input-probe/main.swift'),
    path.join(root, 'native/macos/background-input-probe/FocusLease.swift'),
    path.join(root, 'native/macos/background-input-probe/CalculatorProbe.swift'), '-o', binary]);
  console.log(run(binary, ['self-test']));
  const status = JSON.parse(run(binary, ['status']));
  fs.writeFileSync(path.join(dir, 'status.json'), JSON.stringify(status, null, 2));
  function bundle(role) {
    const contents = path.join(dir, `Probe-${role}.app`, 'Contents');
    const executable = path.join(contents, 'MacOS', 'probe');
    fs.mkdirSync(path.dirname(executable), {recursive: true});
    fs.copyFileSync(binary, executable);
    fs.writeFileSync(path.join(contents, 'Info.plist'), `<?xml version="1.0" encoding="UTF-8"?>
      <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
      <plist version="1.0"><dict><key>CFBundleExecutable</key><string>probe</string>
      <key>CFBundleIdentifier</key><string>com.teatak.pudding.background-probe.${role}</string>
      <key>CFBundleName</key><string>Pudding Background Probe ${role}</string>
      <key>CFBundlePackageType</key><string>APPL</string><key>NSHighResolutionCapable</key><true/></dict></plist>`);
    run('codesign', ['--sign', '-', path.dirname(contents)]);
    return executable;
  }
  return {dir, binary, status, bundle};
}

async function main() {
  if (process.platform !== 'darwin') throw new Error('macOS only');
  const receiver = process.argv[2] ?? 'appkit';
  const selectedVariants = process.argv[3] ? [process.argv[3]] : variants;
  if (process.argv.length > 4 || !['appkit', 'electron', 'electron-click-through'].includes(receiver)
    || selectedVariants.some(v => !variants.includes(v))) {
    throw new Error('usage: node scripts/computer-use-background-probe.cjs [appkit|electron|electron-click-through] [variant]; no external targets');
  }
  const {dir, binary, status, bundle} = createProbe();
  if (!status.accessibility || !status.postEvents || !status.screenCapture) {
    console.log(JSON.stringify({blocked: 'Existing AX, event-post and window-list permissions required. No prompt or reset issued.', ...status}));
    process.exitCode = 2;
    return;
  }
  const targetPath = bundle('target');
  const guardPath = bundle('guard');
  const fixtures = [];
  const results = [];
  try {
    const electron = path.join(root, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron');
    const target = receiver === 'appkit' ? new Fixture(targetPath, 'target')
      : new Fixture(electron, 'electron', [path.join(root, 'electron/smoke/background-input-fixture.cjs'), dir,
        receiver === 'electron-click-through' ? 'click-through' : 'default']);
    fixtures.push(target);
    await target.ready();
    const guard = new Fixture(guardPath, 'guard'); fixtures.push(guard);
    await guard.ready();
    for (const covered of [false, true]) {
      for (const variant of selectedVariants) {
        for (const action of actions) {
          // Explicit experiment setup, never a delivery retry or automatic foreground fallback.
          await target.request('position', {covered});
          await guard.request('position', {covered});
          let state = await guard.request('state');
          const deadline = Date.now() + 3000;
          while (state.foregroundPID !== guard.child.pid && Date.now() < deadline) {
            await pause(30); state = await guard.request('state');
          }
          if (state.foregroundPID !== guard.child.pid) throw new Error('foreground guard not active; stopping');
          await pause(150);
          const targetState = await target.request('state');
          if (receiver !== 'appkit') Object.assign(targetState, JSON.parse(run(binary, ['window'], {input: JSON.stringify(targetState)})));
          if (targetState.active) throw new Error('target not background');
          const targetStart = target.events.length, guardStart = guard.events.length;
          await guard.request('monitor');
          const request = {target: targetState, guardPID: guard.child.pid, variant, action};
          const sender = spawn(binary, ['send'], {stdio: ['pipe', 'pipe', 'pipe']});
          let output = '', error = '';
          sender.stdout.on('data', chunk => output += chunk);
          sender.stderr.on('data', chunk => error += chunk);
          sender.stdin.end(JSON.stringify(request));
          const timer = setTimeout(() => sender.kill('SIGTERM'), 5000);
          const [code] = await once(sender, 'exit'); clearTimeout(timer);
          await pause(200);
          const monitor = await guard.request('stop-monitor');
          const received = target.events.slice(targetStart), guardReceived = guard.events.slice(guardStart);
          const assessment = assess(action, received, monitor, guardReceived, code === 0, guard.child.pid);
          const trace = output.trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
          const scene = trace.find(item => item.kind === 'scene');
          const coverageVerified = scene?.topmostNormalWindowPID === (covered ? guard.child.pid : target.child.pid);
          assessment.pass &&= coverageVerified;
          const result = {receiver, covered, variant, action, ...assessment, coverageVerified,
            sender: {code, output, error}, monitor, received, guardReceived};
          results.push(result);
          fs.appendFileSync(path.join(dir, 'events.jsonl'), JSON.stringify(result) + '\n');
          console.log(JSON.stringify({receiver, covered, variant, action, ...assessment, coverageVerified}));
          if (scene && !coverageVerified) throw new Error('test window coverage changed; stopping without retry');
          if (!assessment.foregroundStable || !assessment.cursorStable || !assessment.guardUntouched) {
            throw new Error('isolation changed (or user intervened); stopping without retry');
          }
        }
      }
    }
  } finally {
    await Promise.all(fixtures.map(f => f.close()));
    fs.writeFileSync(path.join(dir, 'results.json'), JSON.stringify({status, receiver, results}, null, 2));
  }
  // A completed experiment is not proof of compatibility. Failing cells stay visible.
  const compatibleVariants = selectedVariants.filter(variant => {
    const cells = results.filter(r => r.variant === variant);
    return cells.length === 10 && cells.every(r => r.pass);
  });
  console.log(JSON.stringify({compatibleVariants, cases: results.length, evidence: dir,
    productionReady: false, reason: 'Fixture experiment only; mirror, cancellation and release signing are separate gates.'}));
}

module.exports = {assess, Fixture, createProbe, run, pause};
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
