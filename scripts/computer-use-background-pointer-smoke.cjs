#!/usr/bin/env node
// Real product Helper + existing Electron bridge; isolated AppKit/Electron receivers only.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {Fixture, createProbe, pause, run, assess} = require('./computer-use-background-probe.cjs');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');
const {ComputerUseBridgeServer} = require('../electron/computer-use-bridge-server.cjs');
const {ComputerUsePermissionCoordinator} = require('../electron/computer-use-permissions.cjs');

async function main() {
  const binaryPath = process.argv[2], receiver = process.argv[3] || 'appkit';
  assert.ok(process.platform === 'darwin' && path.isAbsolute(binaryPath || '') && fs.statSync(binaryPath).isFile());
  assert.ok(['appkit', 'electron'].includes(receiver));
  const probe = createProbe(), fixtures = [], results = [];
  const host = new ComputerUseHost({binaryPath});
  let bridge;
  try {
    const permissions = {refresh:async () => ({supported:true, ...await host.permissions()})};
    const status = await permissions.refresh();
    assert.ok(status.accessibility && status.screenRecording, 'Existing permissions required; no prompt/reset issued');
    const coordinator = new ComputerUsePermissionCoordinator({permissions,
      onGuideChange:guide => { if (guide) coordinator.deny(guide.requestID); }});
    bridge = new ComputerUseBridgeServer(host, {permissionCoordinator:coordinator});
    const identity = await bridge.start();
    const root = path.resolve(__dirname, '..');
    const target = receiver === 'appkit' ? new Fixture(probe.bundle('target'), 'target')
      : new Fixture(path.join(root,'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'), 'electron',
        [path.join(root,'electron/smoke/background-input-fixture.cjs'), probe.dir, 'default']);
    fixtures.push(target); await target.ready();
    const guard = new Fixture(probe.bundle('guard'), 'guard');
    fixtures.push(guard); await guard.ready();
    const appID = receiver === 'appkit' ? 'com.teatak.pudding.background-probe.target' : 'com.github.Electron';
    await host.useApp({bundleID:appID, pid:target.child.pid, foreground:false});
    for (const action of ['click', 'double', 'right', 'drag', 'scroll']) {
      await target.request('position', {covered:true});
      await guard.request('position', {covered:true});
      await pause(250);
      const state = await target.request('state');
      if (receiver === 'electron') Object.assign(state, JSON.parse(run(probe.binary,['window'],{input:JSON.stringify(state)})));
      assert.equal((await guard.request('state')).foregroundPID, guard.child.pid, 'Guard must already be foreground');
      const point = state.points[action], frame = state.frame;
      const params = {action:action === 'double' || action === 'right' ? 'click' : action,
        x:(point.x-frame.x)/frame.width, y:(point.y-frame.y)/frame.height, delivery:'background'};
      if (action === 'double') params.clickCount = 2;
      if (action === 'right') params.button = 'right';
      if (action === 'drag') Object.assign(params, {toX:params.x+70/frame.width, toY:params.y});
      if (action === 'scroll') params.deltaY = 120;
      const start = target.events.length, guardStart = guard.events.length;
      await guard.request('monitor', {targetPID:target.child.pid});
      const response = await fetch(`${identity.url}/computer/pointer`, {method:'POST',
        headers:{Authorization:`Bearer ${identity.token}`, 'Content-Type':'application/json'},
        body:JSON.stringify({sessionID:'pointer-smoke',appID,windowID:state.windowID,...params})});
      const body = await response.json();
      await pause(250);
      const monitor = await guard.request('stop-monitor');
      const received = target.events.slice(start), guardReceived = guard.events.slice(guardStart);
      const assessment = assess(action,received,monitor,guardReceived,response.ok,guard.child.pid);
      // AppKit may create extra target-owned tooltip windows below the guard.
      // Check actual occlusion ordering, not the number of windows in the app.
      const orderStable = monitor.windowOrders.length > 0 && monitor.windowOrders.every(order => {
        const pids = order.split(',');
        return pids[0] === String(guard.child.pid) && pids.length > 1 && pids.slice(1).every(pid => pid === String(target.child.pid));
      });
      const result = {receiver,action,params,body,...assessment,orderStable,pass:assessment.pass&&orderStable,
        monitor,received,guardReceived};
      results.push(result);
      fs.writeFileSync(path.join(probe.dir,'production-pointer.json'),JSON.stringify({receiver,results},null,2));
      console.log(JSON.stringify({action,body,...assessment,orderStable,pass:result.pass}));
      assert.ok(result.pass, 'Gesture failed; stopping without retry or foreground fallback');
    }
  } finally {
    if (bridge) await bridge.stop();
    await host.stop();
    for (const fixture of fixtures.reverse()) await fixture.close();
  }
  console.log(JSON.stringify({pass:results.length===5&&results.every(r=>r.pass),evidence:probe.dir}));
}
main().catch(error=>{console.error(error);process.exitCode=1;});
