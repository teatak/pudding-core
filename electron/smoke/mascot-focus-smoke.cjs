// Run with web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron.
// Isolated source component, temporary data, and native window focus changes; no daemon.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const out = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-mascot-focus-'));
app.setPath('userData', path.join(out, 'user-data'));
let vite, win, other, exitCode = 0;
const errors = [], results = [];
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const js = source => win.webContents.executeJavaScript(source);
const fixture = `
import React, {useState} from 'react';
import {createRoot} from 'react-dom/client';
import '/src/styles.css';
import {MascotSceneV1Adapter} from '/src/components/mascot-scene/MascotSceneV1Adapter.tsx';
const h = React.createElement;
const request = window.requestAnimationFrame.bind(window), cancel = window.cancelAnimationFrame.bind(window);
let frames = 0;
const pending = new Set();
window.requestAnimationFrame = callback => {
  const id = request(time => {pending.delete(id); frames++; callback(time);});
  pending.add(id);
  return id;
};
window.cancelAnimationFrame = id => {pending.delete(id); cancel(id);};
function Fixture() {
  const [props, setProps] = useState({mood:'thinking'});
  window.setMascotProps = props => setProps(current => ({...current, ...props}));
  return h(MascotSceneV1Adapter, {...props, className:'size-16'});
}
const root = createRoot(document.getElementById('root'));
window.mountMascot = () => root.render(h(Fixture));
window.unmountMascot = () => root.render(null);
window.mountMascot();
window.sample = () => {
  const root = document.querySelector('[data-slot=mascot-scene-v1]');
  return root ? {
    focused:document.hasFocus(), hidden:document.hidden,
    ambient:root.dataset.ambientMotion, pointer:root.dataset.pointerTracking,
    running:root.getAnimations({subtree:true}).filter(a => a.playState === 'running').length,
    gesture:root.querySelector('.mascot-scene-gesture-motion').getAnimations().length,
    head:root.querySelector('.mascot-scene-head').style.transform,
    frames, pending:pending.size,
  } : null;
};
`;
async function waitFor(predicate, label) {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    assert.deepEqual(errors, []);
    if (await predicate()) return;
    await delay(20);
  }
  throw Error('Timed out: ' + label + ' ' + JSON.stringify(await js('window.sample?.()')));
}
async function record(label) {
  const state = await js('window.sample()');
  results.push({label, ...state});
  console.log(label, JSON.stringify(state));
  return state;
}
async function expectStopped(label) {
  await waitFor(() => js('window.sample()?.ambient === "false"'), label + ': disabled');
  const before = await record(label);
  assert.equal(before.pointer, 'false', label + ': pointer listener disabled');
  assert.equal(before.running, 0, label + ': CSS/gesture animations stopped');
  assert.equal(before.pending, 0, label + ': no remaining gaze frame');
  await delay(250);
  const after = await js('window.sample()');
  for (const field of ['ambient','pointer','running','pending']) {
    assert.equal(after[field], before[field], label + ': remains stopped (' + field + ')');
  }
  assert.equal(after.frames, before.frames, label + ': no gaze frames executed');
  assert.equal(after.head, before.head, label + ': pose remains still');
}
async function focusMascot() {
  win.show();
  app.focus({steal:true});
  win.focus();
  await waitFor(() => js('window.sample()?.focused && window.sample().ambient === "true"'), 'foreground resumes');
  assert.ok((await record('foreground')).running > 0);
}
async function blurMascot() {
  other.show();
  other.focus();
  await waitFor(() => js('window.sample()?.focused === false'), 'native blur');
  assert.equal(await js('document.hidden'), false, 'blur fixture stays visible');
}
async function run() {
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {createServer} = await import(pathToFileURL(path.join(repo, 'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({
    root:path.join(repo,'web'), cacheDir:path.join(out,'vite-cache'), logLevel:'error', server:{host:'127.0.0.1',port:0},
    plugins:[{
      name:'mascot-focus-fixture',
      resolveId(id) {if (id === '/__mascot.js') return '\0mascot-focus-fixture';},
      load(id) {if (id === '\0mascot-focus-fixture') return fixture;},
      configureServer(server) {server.middlewares.use('/__fixture', async (_req,res,next) => {
        try {
          res.setHeader('Content-Type','text/html');
          res.end(await server.transformIndexHtml('/__fixture', '<!doctype html><html><head><title>Mascot focus regression</title></head><body><div id="root" style="margin:100px"></div><script type="module" src="/__mascot.js"></script></body></html>'));
        } catch (error) {next(error);}
      });},
    }],
  });
  await vite.listen();
  // Match the main window's background throttling and sandbox configuration.
  win = new BrowserWindow({x:60,y:80,width:600,height:420,show:true,webPreferences:{contextIsolation:true,nodeIntegration:false,sandbox:true}});
  other = new BrowserWindow({x:720,y:80,width:240,height:180,show:false});
  await other.loadURL('data:text/html,<title>Focus target</title>Focus target');
  win.webContents.on('console-message', details => {if (details.level === 'error') errors.push(details.message);});
  const url = 'http://127.0.0.1:' + vite.httpServer.address().port + '/__fixture';
  console.log('SOURCE', process.execPath, url);
  await win.loadURL(url);
  await focusMascot();
  const center = await js(`(() => {const r = document.querySelector('[data-slot=mascot-scene-v1]').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  win.webContents.sendInputEvent({type:'mouseDown', ...center, button:'left', clickCount:1});
  win.webContents.sendInputEvent({type:'mouseUp', ...center, button:'left', clickCount:1});
  await waitFor(() => js('window.sample().gesture > 0'), 'click feedback starts');
  win.webContents.sendInputEvent({type:'mouseMove',x:520,y:300});
  await waitFor(() => js('window.sample().pending > 0'), 'pointer smoothing starts');
  await blurMascot();
  await expectStopped('blur during click and gaze');
  for (const mood of ['idle','thinking','error']) {
    await js(`window.setMascotProps({mood:${JSON.stringify(mood)},headShakeSignal:1,gaze:{type:'input',target:{clientX:500,clientY:300}}});window.dispatchEvent(new PointerEvent('pointermove',{clientX:20,clientY:20}))`);
    await delay(50);
    await expectStopped('background mood ' + mood);
  }
  await js("window.setMascotProps({mood:'thinking',headShakeSignal:0,gaze:{type:'pointer'}})");
  await focusMascot();
  const resumed = await js('window.sample()');
  win.webContents.sendInputEvent({type:'mouseMove',x:10,y:10});
  await waitFor(() => js('window.sample().frames > ' + resumed.frames), 'pointer resumes');
  await blurMascot();
  await js('window.unmountMascot()');
  await waitFor(() => js('window.sample() === null'), 'unmounted');
  await js('window.mountMascot()');
  await waitFor(() => js('window.sample()?.focused === false'), 'mount without window focus');
  await expectStopped('mount in background');
  await focusMascot();
  win.minimize();
  await waitFor(() => win.isMinimized(), 'minimized');
  await expectStopped('minimized');
  win.restore();
  await focusMascot();
  win.hide();
  await expectStopped('hidden');
  await focusMascot();
  await js('document.getElementById("root").style.transform="translateY(1000px)"');
  await expectStopped('outside viewport');
  await js('document.getElementById("root").style.transform="none"');
  await waitFor(() => js('window.sample().ambient === "true"'), 'viewport restored');
  fs.writeFileSync(path.join(out,'resumed.png'), (await win.webContents.capturePage()).toPNG());
  assert.deepEqual(errors, []);
  console.log('PASS mascot focus, background changes/mount, gesture/gaze cancellation, minimize, hide, viewport, resume');
}
app.whenReady().then(run).catch(error => {console.error(error);exitCode = 1;}).finally(async () => {
  fs.writeFileSync(path.join(out,'results.json'), JSON.stringify(results,null,2));
  console.log('REPORT', out);
  win?.destroy();
  other?.destroy();
  await vite?.close();
  app.exit(exitCode);
});
