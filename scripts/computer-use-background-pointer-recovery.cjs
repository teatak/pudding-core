#!/usr/bin/env node
// Fault injection against owned Helper/worker children and an isolated AppKit receiver.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {Fixture, createProbe, pause} = require('./computer-use-background-probe.cjs');
const {journalFor, phase, until, ownedWorker, safeResult} = require('./computer-use-background-lifecycle.cjs');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');

async function main() {
  const binaryPath = process.argv[2], selectedAction = process.argv[3];
  assert.ok(process.platform === 'darwin' && path.isAbsolute(binaryPath || '') && fs.statSync(binaryPath).isFile());
  assert.ok(!selectedAction || ['right','double','drag'].includes(selectedAction));
  const probe = createProbe(), fixtures = [], results = [];
  let diagnostics = '';
  const host = new ComputerUseHost({binaryPath, spawnProcess:(...args)=>{
    const child=spawn(...args); child.stderr.on('data',data=>diagnostics+=data); return child;
  }});
  let operation;
  try {
    const permissions = await host.permissions();
    assert.ok(permissions.accessibility && permissions.screenRecording, 'Existing permissions required; no requests or resets');
    const target = new Fixture(probe.bundle('target'), 'target'); fixtures.push(target); await target.ready();
    const guard = new Fixture(probe.bundle('guard'), 'guard'); fixtures.push(guard); await guard.ready();
    const bundleID = 'com.teatak.pudding.background-probe.target';
    for (const action of selectedAction ? [selectedAction] : ['right', 'double', 'drag']) for (const fault of ['worker', 'helper', 'cancel']) {
      await host.useApp({bundleID, pid:target.child.pid, foreground:false});
      await target.request('position', {covered:true}); await guard.request('position', {covered:true}); await pause(200);
      const state = await target.request('state'), point = state.points[action], frame = state.frame;
      const params = {bundleID,windowID:state.windowID,delivery:'background',action:action==='drag'?'drag':'click',
        x:(point.x-frame.x)/frame.width,y:(point.y-frame.y)/frame.height};
      if(action==='right') params.button='right';
      if(action==='double') params.clickCount=2;
      if(action==='drag') Object.assign(params,{toX:params.x+70/frame.width,toY:params.y});
      assert.equal(phase(target.child.pid),0);
      assert.equal((await guard.request('state')).foregroundPID,guard.child.pid);
      const start=target.events.length,guardStart=guard.events.length;
      diagnostics = '';
      await guard.request('monitor',{targetPID:target.child.pid});
      const controller=new AbortController(), helper=host.child;
      let settled=false;
      operation=host.pointer(params,{signal:controller.signal}).catch(safeResult).then(result=>{settled=true;return result;});
      const worker=await until(()=>{
        assert.ok(!settled,'operation ended before worker identity check');
        return phase(target.child.pid)!==0 && ownedWorker(helper.pid,binaryPath);
      },'owned worker');
      const desiredStep=action==='right'?0:action==='double'?2:4;
      const checkpoint=await until(()=>{
        assert.ok(!settled,'requested interruption step missed; no replay');
        const byte=fs.readFileSync(journalFor(target.child.pid))[0];
        const events=target.events.slice(start).filter(e=>e.kind==='event');
        // Journal records intent before postToPid. This test deliberately kills
        // after the receiver saw the press/move; intent-only crashes are uncertain
        // and may correctly produce just a defensive up (no preceding down).
        const observed=action==='drag'?events.filter(e=>e.type===6).length===desiredStep
          :events.filter(e=>e.type===1||e.type===3).length===(action==='double'?2:1);
        return observed && (byte&3)===2 && (byte>>2)===desiredStep && {phase:byte&3,step:byte>>2,observed:true};
      },'pressed checkpoint');
      if(fault==='worker') process.kill(worker,'SIGKILL');
      if(fault==='helper') assert.ok(helper.kill('SIGKILL'));
      if(fault==='cancel') controller.abort();
      const outcome=await operation; operation=null;
      await until(()=>phase(target.child.pid)===0,'surviving owner recovery');
      const recoveryStart=Date.now();
      while(Date.now()-recoveryStart<2000 && !target.events.slice(start).some(e=>e.kind==='focus-event'&&e.phase==='after'&&e.subtype===2)) await pause(10);
      const settledState=await target.request('state');
      const monitor=await guard.request('stop-monitor'), received=target.events.slice(start);
      const events=received.filter(e=>e.kind==='event');
      const downs=events.filter(e=>e.type===1||e.type===3), ups=events.filter(e=>e.type===2||e.type===4);
      const drags=events.filter(e=>e.type===6), lastUp=ups.at(-1), lastHeld=drags.at(-1)||downs.at(-1);
      const paired=downs.length===(action==='double'?2:1) && ups.length===downs.length;
      // SIGKILL/EOF must stop exactly at the journaled step; cancellation may drain
      // the in-flight gesture to completion, but never sends an additional click.
      const rightButton=action!=='right'||events.every(e=>e.type===3||e.type===4);
      const counts=action!=='double'||JSON.stringify(events.map(e=>e.clickCount))==='[1,1,2,2]';
      const releasePosition=lastUp && lastHeld && lastUp.x===lastHeld.x && lastUp.y===lastHeld.y;
      const boundedDrag=action!=='drag'||(fault==='cancel'?drags.length<=8:drags.length===desiredStep);
      const inactive=received.some(e=>e.kind==='focus-event'&&e.phase==='after'&&e.subtype===2&&e.active===false);
      const isolated=monitor.samples>0&&monitor.foregroundPIDs.every(pid=>pid===guard.child.pid)&&monitor.activations.length===0
        && monitor.maxCursorDistance===0 && !guard.events.slice(guardStart).some(e=>['event','effect'].includes(e.kind))
        && monitor.windowOrders.length>0 && monitor.windowOrders.every(order=>order.split(',')[0]===String(guard.child.pid));
      const cleanExit=!diagnostics.includes('uncaught exception')&&!diagnostics.includes('Broken pipe');
      const pass=outcome.outcome==='unknown'&&!outcome.completed&&paired&&rightButton&&counts&&releasePosition&&boundedDrag&&inactive&&isolated&&cleanExit;
      const result={action,fault,checkpoint,outcome,paired,rightButton,counts,releasePosition,boundedDrag,inactive,isolated,pass,settledState,monitor,received,diagnostics};
      results.push(result);fs.writeFileSync(path.join(probe.dir,'pointer-recovery.json'),JSON.stringify(results,null,2));
      console.log(JSON.stringify({action,fault,checkpoint,outcome,paired,releasePosition,boundedDrag,inactive,isolated,pass}));
      assert.ok(pass,'Recovery test failed; stop without replay');
    }
  } finally {
    if(operation) await operation;
    await host.stop();
    for(const fixture of fixtures.reverse()) await fixture.close();
  }
  console.log(JSON.stringify({pass:results.length===(selectedAction?3:9)&&results.every(r=>r.pass),evidence:probe.dir}));
}
main().catch(error=>{console.error(error);process.exitCode=1;});
