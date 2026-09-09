#!/usr/bin/env node
// Product Helper; only owned fixture windows change. Never reset TCC or touch real apps.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {Fixture, createProbe, pause} = require('./computer-use-background-probe.cjs');
const {phase, safeResult} = require('./computer-use-background-lifecycle.cjs');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');

async function main() {
  const binaryPath = process.argv[2];
  assert.ok(process.platform === 'darwin' && path.isAbsolute(binaryPath || '') && fs.statSync(binaryPath).isFile());
  const probe = createProbe(), results = [];
  const host = new ComputerUseHost({binaryPath});
  let target, guard;
  try {
    const permissions = await host.permissions();
    assert.ok(permissions.accessibility && permissions.screenRecording, 'Existing permissions required; no requests or resets');
    const targetPath = probe.bundle('target'), guardPath = probe.bundle('guard');
    const bundleID = 'com.teatak.pudding.background-probe.target';
    for (const mutation of ['hide', 'minimize', 'move', 'close']) {
      target = new Fixture(targetPath, 'target'); await target.ready();
      guard = new Fixture(guardPath, 'guard'); await guard.ready();
      await host.useApp({bundleID, pid:target.child.pid, foreground:false});
      await target.request('position', {covered:true});
      await guard.request('position', {covered:true}); await pause(200);
      const before = await target.request('state'), point = before.points.drag, frame = before.frame;
      assert.equal((await guard.request('state')).foregroundPID, guard.child.pid, 'Guard must be foreground before test');
      await target.request('arm-window-mutation', {mutation});
      const start = target.events.length, guardStart = guard.events.length;
      await guard.request('monitor', {targetPID:target.child.pid});
      const outcome = await host.pointer({bundleID, windowID:before.windowID,
        action:'drag', delivery:'background', x:(point.x-frame.x)/frame.width, y:(point.y-frame.y)/frame.height,
        toX:(point.x+70-frame.x)/frame.width, toY:(point.y-frame.y)/frame.height}).catch(safeResult);
      await pause(350);
      const after = await target.request('state'), monitor = await guard.request('stop-monitor');
      const received = target.events.slice(start), events = received.filter(e=>e.kind==='event');
      const downs = events.filter(e=>e.type===1), ups = events.filter(e=>e.type===2), drags = events.filter(e=>e.type===6);
      const changed = received.filter(e=>e.kind==='window-mutation' && e.mutation===mutation);
      const isolated = monitor.samples>0 && monitor.foregroundPIDs.every(pid=>pid===guard.child.pid)
        && monitor.activations.length===0 && monitor.maxCursorDistance===0
        && !guard.events.slice(guardStart).some(e=>['event','effect'].includes(e.kind))
        && monitor.windowOrders.every(order=>order.split(',')[0]===String(guard.child.pid));
      // NSWindow.close can precede WindowServer destruction. A single cleanup
      // up to that same window is valid while it still exists; never another down.
      const released = mutation==='close' ? ups.length<=1 && after.windowID===0
        : ups.length===1 && !after.holdingPointer;
      const correctTarget = events.every(e=>e.windowID===before.windowID);
      const releasePosition = ups.every(e=>e.x===drags.at(-1)?.x && e.y===drags.at(-1)?.y);
      const stateChanged = mutation==='hide' ? !after.visible : mutation==='minimize' ? after.minimized
        : mutation==='move' ? after.frame.x!==before.frame.x : after.windowID===0;
      const pass = changed.length===1 && changed[0].afterDragStep===4 && downs.length===1
        && drags.length>=4 && drags.length<=8 && released && correctTarget && releasePosition && stateChanged && isolated
        && outcome.outcome==='unknown' && !outcome.completed && phase(target.child.pid)===0 && !after.active;
      const result = {mutation,pass,outcome,released,correctTarget,releasePosition,stateChanged,isolated,phaseAfter:phase(target.child.pid),before,after,received,monitor};
      results.push(result);
      fs.writeFileSync(path.join(probe.dir,'window-lifecycle.json'), JSON.stringify(results,null,2));
      console.log(JSON.stringify({mutation,pass,outcome,released,stateChanged,isolated,drags:drags.length,ups:ups.length}));
      assert.ok(pass, 'Lifecycle failure; stopping without replay');
      await guard.close(); guard=null; await target.close(); target=null;
    }
  } finally {
    await host.stop();
    if (guard) await guard.close();
    if (target) await target.close();
  }
  console.log(JSON.stringify({pass:results.length===4 && results.every(r=>r.pass), evidence:probe.dir}));
}
main().catch(error=>{console.error(error);process.exitCode=1;});
