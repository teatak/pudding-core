// Exercise the real desktop shell, renderer and daemon with disposable data.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const http = require("node:http");
const { execFile, spawn } = require("node:child_process");
const { promisify } = require("node:util");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow, webContents } = require("electron");

const repo = path.resolve(__dirname, "../..");
const home = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-workspace-smoke-"));
const reportDir = process.env.PUDDING_SMOKE_OUTPUT || path.join(os.tmpdir(), "pudding-workspace-smoke-results");
fs.mkdirSync(reportDir, { recursive: true });
process.env.PUDDING_HOME = home;
process.env.PUDDING_ELECTRON_USER_DATA_DIR = path.join(home, "user-data");
process.env.PUDDING_DAEMON_BIN = path.join(repo, "bin/puddingd");
if (["conversation-restore", "native-ime", "computer-preview"].includes(process.env.PUDDING_SMOKE_SCENARIO)) {
  const wrapper = path.join(home, "mock-daemon");
  const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
  fs.writeFileSync(wrapper, `#!/bin/sh\nexec ${quote(process.env.PUDDING_DAEMON_BIN)} -mock "$@"\n`, { mode: 0o755 });
  process.env.PUDDING_DAEMON_BIN = wrapper;
}
process.env.PUDDING_LOCALE = "zh-CN";
delete process.env.PUDDING_API_BASE;
let vite, window, apiBase, token, phase = "startup", finished = false, exitCode = 0;
let computerBridgeIdentity, computerPreviewManager;
if (process.env.PUDDING_SMOKE_SCENARIO === "computer-preview") {
  // Observe the real native managers without exposing production testing IPC.
  const { ComputerUseBridgeServer } = require('../computer-use-bridge-server.cjs');
  const start = ComputerUseBridgeServer.prototype.start;
  ComputerUseBridgeServer.prototype.start = async function () {
    const identity = await start.call(this); computerBridgeIdentity = identity; return identity;
  };
  const { ComputerUsePreview } = require('../computer-use-preview.cjs');
  const subscribe = ComputerUsePreview.prototype.subscribe;
  ComputerUsePreview.prototype.subscribe = function (...args) { computerPreviewManager = this; return subscribe.apply(this, args); };
}
const checks = [], memory = [], rendererErrors = [];
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const timeout = setTimeout(() => void finish(new Error(`Timed out: ${phase}`)), 180_000);

app.once("will-quit", (event) => {
  fs.rmSync(home, { recursive: true, force: true });
  if (exitCode) { event.preventDefault(); app.exit(exitCode); }
});
void run().then(() => finish(), finish);

async function waitFor(predicate, label, timeoutMs = 15_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await delay(30);
  }
  throw new Error(`Timed out: ${label}`);
}
function check(label) {
  checks.push(label);
  console.info(`[workspace-smoke] PASS ${label}`);
}
async function api(route, method = "GET", body) {
  const response = await fetch(`${apiBase}${route}`, {
    method,
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const raw = await response.text();
  assert.ok(response.ok, `${method} ${route}: ${response.status} ${raw}`);
  return raw ? JSON.parse(raw) : undefined;
}
const js = (source) => window.webContents.executeJavaScript(source, true);
const runFile = promisify(execFile);
async function focusSmokeWindow() {
  const helper = path.join(home, "macos-input");
  if (!fs.existsSync(helper)) await runFile("xcrun", ["clang", "-fobjc-arc", "-framework", "AppKit", "-framework", "ApplicationServices", "-framework", "Carbon", path.join(__dirname, "macos-input.m"), "-o", helper]);
  // Several source Electron instances may share one bundle ID; activate this PID.
  await runFile(helper, ["activate", String(process.pid)]);
  window.show();
  window.focus();
  await waitFor(() => window.isFocused(), "native input window focused");
  return helper;
}
async function nativeInput(action, ...args) {
  const helper = await focusSmokeWindow();
  return runFile(helper, [action, String(process.pid), ...args.map(String)]);
}

function screenPoint(point) {
  const bounds = window.getContentBounds(), zoom = window.webContents.getZoomFactor();
  return { x: Math.round(bounds.x + point.x * zoom), y: Math.round(bounds.y + point.y * zoom) };
}
async function verifyNativeIme(primaryID, secondaryID) {
  assert.equal(process.platform, 'darwin', 'native IME acceptance requires macOS');
  phase = 'native Chinese IME';
  window.setContentSize(1440, 920);
  const status = JSON.parse((await nativeInput('status')).stdout);
  assert.equal(status.inputSource, 'com.apple.inputmethod.SCIM.ITABC', 'select macOS Simplified Chinese Pinyin before this test');
  assert.ok(status.postEventsAllowed, 'native event posting allowed');
  await js(`window.__nativeIme = []; window.__nativeComposing = false;
    for (const type of ['compositionstart','compositionupdate','compositionend','keydown','input']) document.addEventListener(type, event => {
      if (!event.target.matches('textarea,input')) return;
      if(type === 'compositionstart') window.__nativeComposing = true;
      if(type === 'compositionend') window.__nativeComposing = false;
      window.__nativeIme.push({type, key:event.key, code:event.keyCode, data:event.data, trusted:event.isTrusted, composing:event.isComposing, target:event.target.tagName, value:event.target.value});
    }, true);`);
  const requests = [];
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/*/submit`, `${apiBase}/sessions/*/library/recent*`, `${apiBase}/sessions/*/browser/*`]}, (details, callback) => {
    requests.push({url:details.url, method:details.method});
    callback({});
  });
  const submitted = () => requests.filter(r => r.method === 'POST' && r.url.endsWith('/submit'));
  const selector = 'textarea';
  const value = () => js(`document.querySelector('${selector}').value`);
  const pinyin = async () => { for (const code of [45,34,4,0,31]) await nativeInput('key',code); };
  const navigate = async id => {
    await js(`(async()=>{ const {router}=await import('/src/main.tsx'); await router.navigate({to:'/',search:{session:${JSON.stringify(id)}}}); })()`);
    await waitFor(()=>js(`Boolean(document.querySelector('textarea'))`),'session composer');
    await delay(150);
  };
  try {
    await click(selector);
    await pinyin();
    await waitFor(()=>js('window.__nativeComposing'),'real macOS composition starts');
    await nativeInput('key',49); // Space commits the Chinese candidate.
    await waitFor(()=>js('!window.__nativeComposing'),'candidate committed');
    assert.match(await value(), /[\u3400-\u9fff]/, 'native Pinyin inserts Chinese text');
    assert.equal(submitted().length, 0);
    assert.ok(await js(`!document.querySelector('button[aria-label="发送"]').disabled`), 'send is enabled, so candidate Enter is a meaningful check');
    await pinyin();
    await nativeInput('key',36); // Return commits the current composition.
    await waitFor(()=>js('!window.__nativeComposing'),'Return ends composition');
    assert.equal(submitted().length, 0, 'candidate Return must not submit');
    await nativeInput('key',36);
    await waitFor(()=>submitted().length === 1,'deliberate Return sends once');
    assert.equal(submitted()[0].url, `${apiBase}/sessions/${primaryID}/submit`);
    await waitFor(async()=>!(await api(`/sessions/${primaryID}`)).running,'mock response ends');
    check('native Pinyin composition, Chinese candidate commit, candidate Return suppression and deliberate send');

    await click(selector);
    await pinyin();
    await click('button[aria-label="专注"]');
    await waitFor(()=>js('!window.__nativeComposing'),'focus change ends composition');
    const focusedDraft = await value();
    assert.ok(focusedDraft.length > 0, 'focus change preserves composed draft');
    await click('button[aria-label="退出专注"]');
    assert.equal(await value(), focusedDraft);
    assert.equal(submitted().length, 1);
    await click(selector);
    await pinyin();
    const pendingDraft = await value();
    await navigate(secondaryID);
    assert.equal(await value(), '', 'pending composition does not leak to next session');
    await navigate(primaryID);
    assert.equal(await value(), pendingDraft, 'original session retains the entire pending composition');
    check('native composition survives workspace focus changes and remains isolated across sessions');

    await selectSurface('资源库');
    const recentSearches = () => requests.filter(r => r.method==='GET' && r.url.includes('/library/recent') && new URL(r.url).searchParams.get('q'));
    const previous = recentSearches().length;
    await pinyin();
    await waitFor(()=>js('window.__nativeComposing'),'library real composition starts');
    await delay(250);
    assert.equal(recentSearches().length, previous, 'library waits for real candidate commit');
    await nativeInput('key',36);
    await waitFor(()=>js('!window.__nativeComposing'),'library candidate Return');
    assert.equal(await js(`document.querySelector('[data-workspace-add]').getAttribute('aria-pressed')`), 'true', 'candidate Return does not open a resource');
    await waitFor(()=>recentSearches().length > previous,'committed search requested');
    check('resource search waits for native Chinese composition; candidate Return does not open a result');

    await click('button[aria-label="新建浏览器标签页"]');
    await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key^="browser:"][data-selected="true"]'))`),'browser tab');
    const address = 'input[placeholder="搜索或输入网址"]';
    await click(address);
    await pinyin();
    await waitFor(()=>js('window.__nativeComposing'),'browser real composition starts');
    await js(`window.__addressSubmits=0; document.querySelector(${JSON.stringify(address)}).form.addEventListener('submit',()=>window.__addressSubmits++);`);
    const browserContents = () => webContents.getAllWebContents().filter(contents=>contents.getType()==='webview');
    const urls = browserContents().map(contents=>contents.getURL());
    await nativeInput('key',36);
    await waitFor(()=>js('!window.__nativeComposing'),'address candidate Return');
    await delay(200);
    assert.equal(await js('window.__addressSubmits'), 0, 'candidate Return does not submit the address form');
    assert.deepEqual(browserContents().map(contents=>contents.getURL()), urls, 'candidate Return leaves native page URLs unchanged');
    assert.ok(await js(`document.querySelector(${JSON.stringify(address)}).value.length > 0`));
    const localURL = `${process.env.PUDDING_DEV_URL}/__workspace_smoke`;
    await input(address,localURL);
    await nativeInput('key',36);
    await waitFor(()=>browserContents().some(contents=>contents.getURL()===localURL),'deliberate Return navigates to local fixture');
    assert.equal(await js('window.__addressSubmits'), 1);
    check('browser candidate Return leaves the page unchanged; deliberate Return navigates once');
    const trace = await js('window.__nativeIme');
    assert.ok(trace.some(e=>e.type==='compositionstart') && trace.filter(e=>['compositionstart','compositionupdate'].includes(e.type)).every(e=>e.trusted), 'OS-generated trusted composition start/update events');
    assert.ok(trace.some(e=>e.type==='input' && e.trusted && /[\u3400-\u9fff]/.test(e.data)), 'trusted native input inserts Chinese text');
    await screenshot('native-ime');
  } finally {
    fs.writeFileSync(path.join(reportDir,'native-ime-events.json'),JSON.stringify({status,requests,events:await js('window.__nativeIme')},null,2));
    window.webContents.session.webRequest.onBeforeRequest(null);
  }
}
async function verifyNativeWindowChrome(sessionID) {
  assert.equal(process.platform, "darwin", "native chrome acceptance requires macOS");
  phase = "native window chrome";
  window.setBounds({ x: 100, y: 100, width: 1440, height: 820 });
  await js(`(async () => {
    const workspace = await import('/src/state/workspaceStore.ts');
    workspace.closeWorkspaceTabs(${JSON.stringify(sessionID)}, workspace.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder.filter(key => !['project','canvas:smoke-01'].includes(key)));
  })()`);
  await waitFor(() => js(`document.querySelectorAll('[data-workspace-tab-key]').length === 2`), "two tabs leave a blank titlebar");
  const blank = async () => screenPoint(await js(`(() => {
    const bar = document.querySelector('.pudding-workspace-topbar').getBoundingClientRect();
    const add = document.querySelector('[data-workspace-add]')?.getBoundingClientRect();
    const controls = document.querySelector('.pudding-workspace-focus-control').getBoundingClientRect();
    const left = add ? add.right : bar.left;
    if (controls.left - left < 30) throw new Error('No blank titlebar space');
    const point = {x:(left + controls.left)/2, y:bar.y + bar.height/2};
    if (document.elementFromPoint(point.x, point.y)?.closest('button,[data-workspace-tab-key]')) throw new Error('Titlebar target is interactive');
    return point;
  })()`));
  for (const mode of ['standard', 'focused']) {
    if (mode === 'focused') await click('button[aria-label="专注"]');
    await delay(350);
    const before = window.getBounds(), point = await blank();
    await nativeInput('drag', point.x, point.y, point.x + 60, point.y + 35);
    await delay(150);
    const moved = window.getBounds();
    console.info('[workspace-smoke] native drag', {mode, before, moved, point});
    assert.ok(Math.abs(moved.x - before.x - 60) <= 2 && Math.abs(moved.y - before.y - 35) <= 2, `${mode}: blank area moves the native window`);
    assert.equal(moved.width, before.width);
    assert.equal(moved.height, before.height);
    const zoomPoint = await blank();
    await nativeInput('double-click', zoomPoint.x, zoomPoint.y);
    await waitFor(() => JSON.stringify(window.getBounds()) !== JSON.stringify(moved), `${mode}: native double-click zooms window`);
    await delay(250);
    const restorePoint = await blank();
    await nativeInput('double-click', restorePoint.x, restorePoint.y);
    await waitFor(() => JSON.stringify(window.getBounds()) === JSON.stringify(moved), `${mode}: second double-click restores window`);
    check(`${mode}: native blank-area window drag and double-click zoom/restore`);

    const order = () => js(`Array.from(document.querySelectorAll('[data-workspace-tab-key]')).map(el => el.dataset.workspaceTabKey)`);
    const originalOrder = await order();
    const points = (await js(`Array.from(document.querySelectorAll('[data-workspace-tab-key] .pudding-workspace-tab-select')).map(el => { const r=el.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })`)).map(screenPoint);
    await nativeInput('drag', points[0].x, points[0].y, points[1].x, points[1].y);
    await waitFor(async () => (await order())[0] === originalOrder[1], `${mode}: native tab drag reorders`);
    assert.deepEqual(window.getBounds(), moved, `${mode}: tab drag leaves window in place`);
    await selectTopTab('project');
    await selectTopTab('canvas:smoke-01');
    await click('[data-workspace-add]');
    await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden') !== 'true' && document.querySelector('[data-workspace-add]').getAttribute('aria-pressed') === 'true'`), 'add opens resources');
    check(`${mode}: native tab reordering, tab activation and add button remain interactive`);
  }
  await click('button[aria-label="退出专注"]');
  await click('button[aria-label="收起工作区"]');
  await click('button[aria-label="打开工作区"]');
  await waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), 'workspace toggle settles');
  await js(`(async () => { const workspace=await import('/src/state/workspaceStore.ts'); workspace.closeWorkspaceTabs(${JSON.stringify(sessionID)}, workspace.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder); })()`);
  await waitFor(() => js(`document.querySelectorAll('[data-workspace-tab-key]').length === 0`), 'empty workspace titlebar');
  const before = window.getBounds(), point = await blank();
  await nativeInput('drag', point.x, point.y, point.x - 40, point.y - 25);
  assert.ok(Math.abs(window.getBounds().x - before.x + 40) <= 2 && Math.abs(window.getBounds().y - before.y + 25) <= 2, 'empty workspace titlebar moves the window');
  check('focus/collapse controls remain clickable; empty workspace titlebar is draggable');
  await screenshot('native-window-chrome');
}
const visible = `(el) => el.getClientRects().length && getComputedStyle(el).visibility !== 'hidden'`;
async function reloadRenderer() {
  // Wait for the new document rather than accepting the old tree during reload.
  window.focus();
  await waitFor(() => window.isFocused(), 'reload window focused');
  await new Promise(resolve => {
    window.webContents.once('did-finish-load', resolve);
    window.webContents.reload();
  });
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
}
async function selectTopTab(key) {
  const source = `document.querySelector('[data-workspace-tab-key="${key}"] .pudding-workspace-tab-select')`;
  await waitFor(() => js(`Boolean(${source})`), `tab ${key}`);
  await js(`${source}.scrollIntoView({block:'nearest',inline:'nearest'})`);
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await clickElement(source);
}
async function clickElement(source) {
  await focusSmokeWindow();
  const target = await js(`(() => {
    const el = ${source};
    const r = el.getBoundingClientRect();
    const x = r.x + r.width / 2, y = r.y + r.height / 2;
    const hit = document.elementFromPoint(x, y);
    const strip = el.closest('.pudding-workspace-tabs-scroll');
    return { x, y, reachable: el.contains(hit), obstruction: hit?.outerHTML.slice(0, 500), scroll:strip?.scrollLeft, strip:strip?.getBoundingClientRect().toJSON(), focused:document.hasFocus() };
  })()`);
  assert.ok(target.reachable, `Click blocked: ${source}\n${JSON.stringify(target)}`);
  const zoom = window.webContents.getZoomFactor();
  const point = { x: Math.round(target.x * zoom), y: Math.round(target.y * zoom) };
  await js(`(() => {
    window.__workspaceSmokeClick = null;
    const el = ${source};
    // Radix opens modal dropdowns on pointerdown; their overlay consumes the
    // subsequent click. Verify the event that actually activates the control.
    const activation = el.getAttribute('aria-haspopup') === 'menu' ? 'pointerdown' : 'click';
    document.addEventListener(activation, event => {
      window.__workspaceSmokeClick = { matched: el.contains(event.target), target: event.target.outerHTML.slice(0, 500) };
    }, { once: true, capture: true });
  })()`);
  window.webContents.sendInputEvent({ type: "mouseMove", ...point });
  window.webContents.sendInputEvent({ type: "mouseDown", ...point, button: "left", clickCount: 1 });
  window.webContents.sendInputEvent({ type: "mouseUp", ...point, button: "left", clickCount: 1 });
  await waitFor(() => js(`window.__workspaceSmokeClick !== null`), `mouse click delivered: ${source}`);
  const delivered = await js(`window.__workspaceSmokeClick`);
  assert.ok(delivered.matched, `Mouse click missed ${source}: ${JSON.stringify({ zoom, point, delivered })}`);
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
}
async function click(selector) {
  await waitFor(() => js(`Array.from(document.querySelectorAll(${JSON.stringify(selector)})).some(${visible})`), selector);
  await clickElement(`Array.from(document.querySelectorAll(${JSON.stringify(selector)})).find(${visible})`);
}
async function input(selector, value) {
  await js(`(() => {
    const el = document.querySelector(${JSON.stringify(selector)});
    if (!el) throw new Error('Missing input');
    el.focus();
    Object.getOwnPropertyDescriptor(el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(value)});
    el.dispatchEvent(new Event('input', { bubbles: true }));
  })()`);
}
const tabNodes = `() => {
  if (document.querySelector('[data-workspace-tab-key="project"][data-selected="true"]')) return Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-content-tab')).filter(${visible});
  const selected = document.querySelector('[data-workspace-tab-key][data-selected="true"]');
  const prefix = selected?.dataset.workspaceTabKey.split(':')[0];
  return Array.from(document.querySelectorAll('[data-workspace-tab-key]')).filter(el => !prefix || el.dataset.workspaceTabKey.startsWith(prefix + ':'));
}`;
const tabs = () => js(`(${tabNodes})().map(el => ({title:el.querySelector('.pudding-workspace-tab-select').textContent, selected:el.dataset.selected === 'true'}))`);
async function openTabsMenu() {
  const root = await js(`document.querySelector('[data-workspace-tab-key="project"][data-selected="true"]') ? '[data-project-workspace]' : '.pudding-workspace-topbar'`);
  await click(`${root} button[aria-label^="已打开 "]`);
}
async function selectSurface(label) {
  if (label === "项目" || label === "资源库") {
    const source = label === "项目"
      ? `document.querySelector('[data-workspace-tab-key="project"] .pudding-workspace-tab-select')`
      : `document.querySelector('[data-workspace-add]')`;
    await js(`${source}.scrollIntoView({block:'nearest',inline:'nearest'})`);
    await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
    await clickElement(source);
    return;
  }
  const kind = label === "浏览器" ? "browser" : "canvas";
  const source = `document.querySelector('[data-workspace-tab-key^="${kind}:"] .pudding-workspace-tab-select')`;
  await waitFor(() => js(`Boolean(${source})`), `${kind} content tab`);
  await js(`${source}.scrollIntoView({block:'nearest',inline:'nearest'})`);
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await clickElement(source);
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key^="${kind}:"][data-selected="true"]'))`), `mouse selects ${kind}`);
}
async function clickText(text, selector = "button") {
  const source = `Array.from(document.querySelectorAll(${JSON.stringify(selector)})).find(el => (${visible})(el) && el.textContent.trim() === ${JSON.stringify(text)})`;
  await waitFor(() => js(`Boolean(${source})`), text);
  await clickElement(source);
  if (selector === '[role="menuitem"]') {
    await waitFor(() => js(`!document.querySelector('[role="menu"]')`), `menu closes after ${text}`);
  }
}
async function swapFirstTwoTabs() {
  await delay(250);
  await js(`(${tabNodes})()[0].scrollIntoView({block:'nearest',inline:'start'})`);
  await delay(100);
  const points = await js(`(${tabNodes})().map(el => el.querySelector('.pudding-workspace-tab-select')).slice(0, 2).map(el => {
    const r=el.getBoundingClientRect(), x=r.x+r.width/2, y=r.y+r.height/2;
    if (!el.contains(document.elementFromPoint(x,y))) throw new Error('Drag handle is obscured');
    return {x:Math.round(x),y:Math.round(y)};
  })`);
  assert.equal(points.length, 2);
  window.webContents.sendInputEvent({ type: "mouseMove", ...points[0] });
  window.webContents.sendInputEvent({ type: "mouseDown", ...points[0], button: "left", clickCount: 1 });
  window.webContents.sendInputEvent({ type: "mouseMove", x: points[0].x + 12, y: points[0].y, button: "left" });
  await delay(50);
  window.webContents.sendInputEvent({ type: "mouseMove", ...points[1], button: "left" });
  await delay(150);
  window.webContents.sendInputEvent({ type: "mouseUp", ...points[1], button: "left", clickCount: 1 });
}
async function verifyWorkspaceToggle() {
  const source = `document.querySelector('.pudding-workspace-focus-control button[aria-expanded]')`;
  const measure = `(() => { const el = ${source}, r = el.getBoundingClientRect(), svg = el.querySelector('svg').getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, iconX: svg.x, iconY: svg.y }; })()`;
  for (const size of [[1440, 920], [780, 760]]) {
    window.setContentSize(...size);
    await delay(300);
    const before = await js(measure);
    const header = await js(`(() => {
      const library=document.querySelector('[data-workspace-add]').getBoundingClientRect();
      const tab=document.querySelector('[data-workspace-tab-key]').getBoundingClientRect();
      return {centre:library.y+library.height/2, tabHeight:tab.height};
    })()`);
    assert.ok(Math.abs(header.centre - before.y - before.height / 2) < .1, 'add button aligns with workspace controls');
    assert.equal(header.tabHeight, 28, 'top-level tabs stay compact');
    await js(`window.__toggleSamples = []; window.__toggleRecording = true; (function frame() { if (!window.__toggleRecording) return; window.__toggleSamples.push(${measure}); requestAnimationFrame(frame); })()`);
    for (const label of ['打开工作区', '收起工作区']) {
      await clickElement(source);
      await waitFor(() => js(`${source}?.getAttribute('aria-label') === ${JSON.stringify(label)}`), `toggle becomes ${label}`);
      await delay(300);
      if (label === '打开工作区') {
        const gap = await js(`(() => {
          const header = document.querySelector('.pudding-chat-pane-header');
          const apps = header.lastElementChild.getBoundingClientRect();
          return ${source}.getBoundingClientRect().left - apps.right;
        })()`);
        assert.ok(Math.abs(gap - 8) < .1, 'session Apps should sit 8px left of the workspace toggle');
      } else {
        assert.equal(await js(`document.querySelector('.pudding-workspace-focus-control button[aria-pressed]').textContent.trim()`), '', 'focus control stays icon-only');
      }
    }
    const point = { x: Math.round(before.x + before.width / 2), y: Math.round(before.y + before.height / 2) };
    window.webContents.sendInputEvent({ type: 'mouseMove', ...point });
    window.webContents.sendInputEvent({ type: 'mouseDown', ...point, button: 'left', clickCount: 1 });
    await delay(100);
    window.webContents.sendInputEvent({ type: 'mouseUp', ...point, button: 'left', clickCount: 1 });
    await delay(300);
    await clickElement(source);
    await delay(300);
    const samples = await js(`window.__toggleRecording = false; window.__toggleSamples`);
    const drift = Object.fromEntries(Object.keys(before).map(key => [key, Math.max(...samples.map(row => Math.abs(row[key] - before[key])))]));
    console.info('[workspace-smoke] TOGGLE GEOMETRY', JSON.stringify({size, before, drift, frames:samples.length}));
    assert.ok(Object.values(drift).every(value => value < .1), 'workspace toggle moves while opening, closing or pressed');
  }
  window.setContentSize(1440, 920);
  await delay(300);
  check('workspace toggle stays fixed, focus stays icon-only and session Apps align right, wide and narrow');
}

async function verifyProjectTabShape(sessionID, browserKey, canvasKey) {
  const original = await js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}))`);
  await js(`import('/src/state/workspaceStore.ts').then(m => {
    m.closeWorkspaceTabs(${JSON.stringify(sessionID)}, ${JSON.stringify(original.tabOrder.filter(key => key.startsWith('canvas:') && key !== canvasKey))});
    m.setWorkspaceTabOrder(${JSON.stringify(sessionID)}, ['project',${JSON.stringify(browserKey)},${JSON.stringify(canvasKey)}]);
    m.openWorkspaceView(${JSON.stringify(sessionID)}, 'project');
  })`);
  await delay(250);
  const points = await js(`['project', ${JSON.stringify(browserKey)}].map(key => {
    const el=document.querySelector('[data-workspace-tab-key="'+key+'"]'), r=el.getBoundingClientRect();
    return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2),width:r.width,height:r.height};
  })`);
  assert.equal(points[0].width, 112, 'project uses the previous compact tab width');
  const {x,y}=points[0];
  window.webContents.sendInputEvent({type:'mouseMove',x,y});
  window.webContents.sendInputEvent({type:'mouseDown',x,y,button:'left',clickCount:1});
  window.webContents.sendInputEvent({type:'mouseMove',x:x+8,y,button:'left'});
  await delay(100);
  window.webContents.sendInputEvent({type:'mouseMove',x:points[1].x,y:points[1].y+18,button:'left'});
  await delay(150);
  const during = await js(`(() => {const el=document.querySelector('[data-workspace-tab-key="project"]'),r=el.getBoundingClientRect();return {width:r.width,height:r.height,y:r.y,transform:getComputedStyle(el).transform};})()`);
  console.info('[workspace-smoke] PROJECT DRAG', JSON.stringify({before:points[0],during}));
  assert.ok(Math.abs(during.width-points[0].width)<.1, 'project width unchanged while crossing browser');
  assert.ok(Math.abs(during.height-points[0].height)<.1, 'project height unchanged while dragging');
  assert.ok(Math.abs(during.y+points[0].height/2-points[0].y)<.6, 'project drag stays horizontal');
  window.webContents.sendInputEvent({type:'mouseUp',x:points[1].x,y:points[1].y+18,button:'left',clickCount:1});
  await waitFor(() => js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder[1] === 'project')`), 'project reorders with browser');
  await js(`import('/src/state/workspaceStore.ts').then(m => m.replaceWorkspaceSessionUI(${JSON.stringify(sessionID)}, ${JSON.stringify(original)}))`);
  await waitFor(() => js(`document.querySelectorAll('[data-workspace-tab-key]').length === ${original.tabOrder.length}`), 'restore mixed fixture');
  check('project tab retains the previous compact width without scaling and preserves horizontal position');
}

async function verifyMixedTabs(sessionID) {
  const original = await js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder)`);
  const browser = await api(`/sessions/${sessionID}/browser/tabs`, "POST");
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="browser:${browser.id}"]'))`), "browser joins canvas strip");
  const canvasKey = original.find(key => key.startsWith('canvas:')), browserKey = `browser:${browser.id}`;
  await js(`import('/src/state/workspaceStore.ts').then(m => { m.setWorkspaceTabOrder(${JSON.stringify(sessionID)}, [${JSON.stringify(canvasKey)}, ${JSON.stringify(browserKey)}, ...${JSON.stringify(original.filter(key => key !== canvasKey))}]); m.setWorkspaceActiveTab(${JSON.stringify(sessionID)}, ${JSON.stringify(canvasKey)}); })`);
  await delay(250);
  const points = await js(`Array.from(document.querySelectorAll('[data-workspace-tab-key] .pudding-workspace-tab-select')).slice(0,2).map(el => {const r=el.getBoundingClientRect(); return {x:Math.round(r.x+r.width/2), y:Math.round(r.y+r.height/2)};})`);
  window.webContents.sendInputEvent({type:"mouseMove",...points[0]});
  window.webContents.sendInputEvent({type:"mouseDown",...points[0],button:"left",clickCount:1});
  window.webContents.sendInputEvent({type:"mouseMove",x:points[0].x+12,y:points[0].y,button:"left"});
  await delay(50);
  window.webContents.sendInputEvent({type:"mouseMove",...points[1],button:"left"});
  await delay(150);
  window.webContents.sendInputEvent({type:"mouseUp",...points[1],button:"left",clickCount:1});
  await waitFor(() => js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder[0] === ${JSON.stringify(browserKey)})`), "canvas drags across browser");
  await verifyProjectTabShape(sessionID, browserKey, canvasKey);
  assert.equal(await js(`document.querySelectorAll('.pudding-workspace-topbar').length`), 1);
  assert.equal(await js(`Boolean(document.querySelector('[data-workspace-tab-key="project"] .pudding-workspace-tab-close'))`), true);
  await reloadRenderer();
  await waitFor(() => js(`document.querySelectorAll('[data-workspace-tab-key]').length === ${original.length + 1}`), "mixed order restores after reload");
  assert.deepEqual(await js(`Array.from(document.querySelectorAll('[data-workspace-tab-key]')).slice(0,2).map(el => el.dataset.workspaceTabKey)`), [browserKey, canvasKey]);
  assert.equal(await js(`document.querySelectorAll('.pudding-workspace-topbar button[aria-haspopup="dialog"]').length`), 0, "no top-level overflow popup");
  for (const theme of ["light", "dark"]) {
    await js(`window.puddingElectronTheme.setTheme(${JSON.stringify(theme)})`);
    await delay(250);
    await screenshot(`mixed-tabs-${theme}`);
  }
  const source = `document.querySelector('[data-workspace-tab-key="${browserKey}"] .pudding-workspace-tab-select')`;
  await js(`${source}.scrollIntoView({block:'nearest',inline:'nearest'})`);
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await clickElement(source);
  await click(`[data-workspace-tab-key="${browserKey}"] .pudding-workspace-tab-close`);
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="${canvasKey}"][data-selected="true"]'))`), "closing browser selects its neighbouring canvas");
  assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.length, 20);
  check("single strip: mixed mouse drag, persisted order and cross-type close selection");
}

async function verifyTabContextMenu() {
  const point = await js(`(() => {
    const el=(${tabNodes})()[0].querySelector('.pudding-workspace-tab-select');
    const r=el.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};
  })()`);
  window.webContents.sendInputEvent({ type: "mouseDown", ...point, button: "right", clickCount: 1 });
  window.webContents.sendInputEvent({ type: "mouseUp", ...point, button: "right", clickCount: 1 });
  await waitFor(() => js(`Array.from(document.querySelectorAll('[role="menu"]')).some(el => (${visible})(el) && el.textContent.includes('关闭右侧标签'))`), "sortable tab context menu remains available");
  window.webContents.sendInputEvent({ type: "keyDown", keyCode: "Escape" });
  window.webContents.sendInputEvent({ type: "keyUp", keyCode: "Escape" });
  await waitFor(() => js(`!document.querySelector('[role="menu"]')`), "context menu exit animation finishes");
}
async function verifyTabDrag(label, single = false) {
  const initial = await js(`(() => {
    const tab = (${tabNodes})().find(el => el.dataset.selected === 'true');
    window.__workspaceSmokeDraggedTab = tab;
    const r = tab.getBoundingClientRect(), button = tab.querySelector('.pudding-workspace-tab-select').getBoundingClientRect();
    const strip = tab.parentElement.getBoundingClientRect();
    return { left: r.left, top: r.top, x: button.x + button.width / 2, y: button.y + button.height / 2, stripLeft: strip.left, stripRight: strip.right };
  })()`);
  const point = { x: Math.round(initial.x), y: Math.round(initial.y) };
  window.webContents.sendInputEvent({ type: "mouseMove", ...point });
  window.webContents.sendInputEvent({ type: "mouseDown", ...point, button: "left", clickCount: 1 });
  window.webContents.sendInputEvent({ type: "mouseMove", x: point.x + 12, y: point.y, button: "left" });
  await delay(50);
  for (const destination of [{ x: initial.stripRight + 400, y: initial.y + 150 }, { x: initial.stripLeft - 200, y: initial.y - 80 }]) {
    window.webContents.sendInputEvent({ type: "mouseMove", x: Math.round(destination.x), y: Math.round(destination.y), button: "left" });
    await delay(250);
    const actual = await js(`(() => {
      const tab = window.__workspaceSmokeDraggedTab, r = tab.getBoundingClientRect(), strip = tab.parentElement.getBoundingClientRect();
      return { left: r.left, right: r.right, top: r.top, stripLeft: strip.left, stripRight: strip.right };
    })()`);
    assert.ok(Math.abs(actual.top - initial.top) < 1, `${label}: dragged tab stays on its horizontal row: ${JSON.stringify(actual)}`);
    assert.ok(actual.left >= actual.stripLeft - 1 && actual.right <= actual.stripRight + 1, `${label}: dragged tab stays inside its strip: ${JSON.stringify(actual)}`);
    if (single) assert.ok(Math.abs(actual.left - initial.left) < 1, `${label}: a lone tab does not drag`);
  }
  window.webContents.sendInputEvent({ type: "keyDown", keyCode: "Escape" });
  window.webContents.sendInputEvent({ type: "keyUp", keyCode: "Escape" });
  window.webContents.sendInputEvent({ type: "mouseUp", ...point, button: "left", clickCount: 1 });
  await delay(250);
  check(`${label}: tab drag is horizontal and bounded${single ? "; single tab stays fixed" : ""}`);
}
async function measureMemory(label) {
  // A renderer may share an OS process with other same-site guests. Count PIDs
  // once via app metrics, and record guests separately from process memory.
  await delay(1500);
  const metrics = app.getAppMetrics();
  const guests = webContents.getAllWebContents().filter(c => c.getType() === "webview");
  const guestPIDs = new Set(guests.map(c => c.getOSProcessId()));
  const sample = {
    label,
    guestCount: guests.length,
    rendererProcesses: metrics.filter(m => m.type === "Tab").length,
    workingSetMiB: Math.round(metrics.reduce((sum, m) => sum + m.memory.workingSetSize, 0) / 1024),
    guestWorkingSetMiB: Math.round(metrics.filter(m => guestPIDs.has(m.pid)).reduce((sum, m) => sum + m.memory.workingSetSize, 0) / 1024),
  };
  memory.push(sample);
  console.info(`[workspace-smoke] MEMORY ${JSON.stringify(sample)}`);
  return sample;
}
async function screenshot(name) {
  fs.writeFileSync(path.join(reportDir, `${name}.png`), (await window.webContents.capturePage()).toPNG());
}

async function verifyEmptyWorkspace(projectID) {
  phase = "empty workspace";
  const emptySession = await api("/sessions", "POST", {title:"Empty workspace",provider:"mock",model:"mock",projectID});
  // The fixture is created outside React's mutation path; refresh its list
  // before using the real sidebar navigation.
  await js(`import('/src/main.tsx').then(({router}) => router.options.context.queryClient.invalidateQueries({queryKey:['sessions']}))`);
  await click(`[data-session-item-id="${emptySession.id}"]`);
  await waitFor(() => js(`new URLSearchParams(location.search).get('session') === ${JSON.stringify(emptySession.id)}`), "empty session selected");
  await click('button[aria-label="打开工作区"]');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="project"]'))`), "empty project tab");
  await delay(300);
  await click('[data-workspace-tab-key="project"] .pudding-workspace-tab-close');
  await waitFor(() => js(`document.querySelector('[data-workspace-library]')?.getAttribute('aria-hidden') === 'false' && !document.querySelector('[data-workspace-tab-key]')`), "last tab shows resource landing");
  assert.equal(await js(`Boolean(document.querySelector('[data-workspace-add]'))`), false, "no add button without tabs");
  assert.equal(await js(`getComputedStyle(document.querySelector('.pudding-workspace-topbar')).boxShadow`), 'none', "no landing header divider");
  assert.equal(await js(`getComputedStyle(document.querySelector('[data-workspace-library]')).backgroundColor === getComputedStyle(document.querySelector('.pudding-workspace-pane')).backgroundColor`), true, "landing uses workspace background");
  await screenshot("workspace-empty");
  await click('[data-workspace-library] button[aria-label="新建浏览器标签页"]');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key^="browser:"][data-selected="true"]'))`), "landing creates a browser tab");
  await click('[data-workspace-add]');
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden') === 'false'`), "add button opens resource landing");
  assert.equal((await api(`/sessions/${emptySession.id}/browser/tabs`)).tabs.length, 1, "plus does not create another browser");
  await click('[data-workspace-library] button[aria-label="打开项目"]');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="project"][data-selected="true"]'))`), "landing reopens project");
  check("zero-tab landing: matching background, no divider/add/popup; explicit project/browser actions; plus opens resources");
}

async function verifyProjectTreeReveal(sessionID, projectRoot) {
  phase = "tree reveal";
  window.setContentSize(1440,920);
  for(const file of ['reveal/nested/target.md','file-01.md']) {
    await js(`(async()=>{const {requestProjectFileReveal}=await import('/src/state/projectRevealStore.ts');requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:${JSON.stringify(file)}});})()`);
    await waitFor(async () => (await tabs()).some(tab=>tab.title===path.basename(file)&&tab.selected), 'open '+file);
    await js(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent==='${path.basename(file)}').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);
  }
  const treeSource=`document.querySelector('[data-project-workspace] nav').nextElementSibling.firstElementChild`;
  const targetSource=`Array.from((${treeSource}).querySelectorAll('button')).find(el=>el.textContent.trim()==='target.md')`;
  await waitFor(()=>js(`Boolean(${targetSource})`),'nested target loaded');
  const measure=`(() => {const tree=${treeSource},file=${targetSource}; const t=tree?.getBoundingClientRect(),r=file?.getBoundingClientRect();return {scroll:tree?.scrollTop,visible:Boolean(r&&t&&r.top>=t.top&&r.bottom<=t.bottom),focused:document.activeElement===file,active:Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.getAttribute('aria-pressed')==='true')?.textContent,filesTab:document.querySelector('[data-project-workspace] nav button[aria-pressed="true"]')?.textContent};})()`;
  const revealMenu=async()=>{
    app.focus({steal:true});window.show();window.focus();
    const p=await js(`(() => {const el=Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent==='target.md');el.scrollIntoView({block:'nearest',inline:'nearest'});const r=el.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()`);
    const zoom=window.webContents.getZoomFactor(); p.x=Math.round(p.x*zoom);p.y=Math.round(p.y*zoom);
    window.webContents.sendInputEvent({type:'mouseDown',...p,button:'right',clickCount:1});
    window.webContents.sendInputEvent({type:'mouseUp',...p,button:'right',clickCount:1});
    await clickText('在文件树中定位','[role="menuitem"]');
    await delay(250);
  };
  const assertRevealed=async(label)=>{
    await waitFor(()=>js(`Boolean(${targetSource})`),label+' target mounted');
    const result=await js(measure);console.log('[tree-reveal]',label,JSON.stringify(result));
    await screenshot('tree-'+label);
    assert.equal(result.visible,true,label+' scrolls target into view');
    assert.equal(result.focused,true,label+' focuses target');
  };
  await js(`(${treeSource}).scrollTop=0`);
  const before = await js(measure);
  assert.equal(before.visible, false, 'fixture starts with target outside the tree viewport');
  console.log('[tree-reveal] before', JSON.stringify(before));
  await revealMenu();
  await assertRevealed('offscreen');
  assert.equal((await js(measure)).active,'file-01.md','reveal background tab keeps current editor');
  await js(`(${treeSource}).scrollTop=0`);
  await revealMenu(); await assertRevealed('repeat');
  await js(`(${treeSource}).scrollTop=0`);
  await clickElement(`(${treeSource}).querySelector('button')`);
  await waitFor(()=>js(`!(${targetSource})`),'root collapsed');
  await js(`import('/src/main.tsx').then(({router})=>router.options.context.queryClient.removeQueries({queryKey:['session',${JSON.stringify(sessionID)},'project','tree'],type:'inactive'}))`);
  await revealMenu(); await assertRevealed('collapsed');
  await click('[data-project-workspace] nav button[aria-label="搜索项目内容"]');
  await revealMenu(); await assertRevealed('from-search');
  await click('[data-project-workspace] nav button[aria-label="源代码管理"]');
  await revealMenu(); await assertRevealed('from-git');
  window.setContentSize(560,760); window.webContents.setZoomFactor(1.5); await delay(400);
  assert.ok(await js(`document.querySelector('[data-project-workspace]').clientWidth<420`),'single-pane project layout');
  await revealMenu(); await assertRevealed('narrow');
  check('tree reveal: repeated/background/collapsed/search/git/narrow; visible and focused');

}

async function verifyProjectDraftLayout(sessionID, projectRoot) {
  phase = "project draft layout";
  window.setContentSize(1440, 920);
  await js(`import('/src/state/projectRevealStore.ts').then(m => m.requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:'draft.txt'}))`);
  await waitFor(() => js(`Boolean(document.querySelector('[data-project-workspace] .monaco-editor textarea'))`), "source editor ready");
  await clickElement(`document.querySelector('[data-project-workspace] .monaco-editor .view-lines')`);
  await window.webContents.insertText("UNSAVED_LAYOUT_DRAFT ");
  const hasDraft = () => js(`document.querySelector('[data-project-workspace] .view-lines')?.textContent.includes('UNSAVED_LAYOUT_DRAFT')`);
  await waitFor(hasDraft, "native input modifies the file");
  await js(`window.__draftEditor = document.querySelector('[data-project-workspace] .monaco-editor')`);
  assert.equal(fs.readFileSync(path.join(projectRoot, "draft.txt"), "utf8"), "Original file\n", "fixture remains unsaved");
  window.setContentSize(560, 760);
  window.webContents.setZoomFactor(1.5);
  await delay(400);
  assert.ok(await js(`document.querySelector('[data-project-workspace]').clientWidth < 420`), "single-pane layout");
  await screenshot("draft-narrow");
  assert.ok(await hasDraft(), "unsaved file survives switching from wide to narrow layout");
  await click('[data-project-workspace] button[aria-label="项目文件"]');
  await clickElement(`Array.from(document.querySelectorAll('[data-project-workspace] nav + div button')).find(el => el.textContent.trim() === 'draft.txt')`);
  await waitFor(hasDraft, "unsaved file survives tree and viewer switching");
  window.webContents.setZoomFactor(1);
  window.setContentSize(1440, 920);
  await delay(400);
  assert.ok(await hasDraft(), "unsaved file survives returning to wide layout");
  assert.ok(await js(`window.__draftEditor === document.querySelector('[data-project-workspace] .monaco-editor')`), "layout changes preserve the editor instance and undo stack");
  await click('[data-project-workspace] button[aria-label="保存文件"]');
  await waitFor(() => fs.readFileSync(path.join(projectRoot, "draft.txt"), "utf8").includes("UNSAVED_LAYOUT_DRAFT"), "preserved draft saves to the original file");
  check("project draft survives wide/narrow/tree/viewer transitions and saves to the original file");
}

async function verifyLibrarySearch(sessionID, projectRoot, otherSessionID) {
  phase = 'library search';
  window.setContentSize(1440,920);
  for (const itemID of ['smoke-01','smoke-02']) await api(`/sessions/${sessionID}/library/recent`, 'POST', {kind:'canvas',itemID});
  const roots=await api(`/sessions/${sessionID}/project/tree`);
  await api(`/sessions/${sessionID}/library/recent`, 'POST', {kind:'file',rootID:roots.roots[0].id,path:'file-01.md'});
  fs.unlinkSync(path.join(projectRoot, 'file-01.md'));
  await selectSurface('资源库');
  const search='[data-workspace-library] input';
  const settled=()=>js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy')==='false'`);
  const rows=()=>js(`Array.from(document.querySelectorAll('[data-library-results] [data-library-open]')).map(el=>el.textContent)`);
  const key=async(keyCode)=>{
    app.focus({steal:true});window.focus();await waitFor(()=>window.isFocused(),'keyboard focus');
    window.webContents.sendInputEvent({type:'keyDown',keyCode});
    if (keyCode === 'Return') window.webContents.sendInputEvent({type:'char',keyCode:'\r'});
    window.webContents.sendInputEvent({type:'keyUp',keyCode});
    await delay(50);
  };
  await waitFor(settled,'initial results');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')`),'plus focuses search');
  assert.ok(await js(`Boolean(document.querySelector('[data-library-results] [data-library-open]:disabled'))`),'unavailable source fixture');
  await key('Down');
  assert.ok(await js(`document.activeElement===document.querySelector('[data-library-results] [data-library-open]:not(:disabled)')`),'down skips unavailable result');
  await key('Down');
  assert.ok(await js(`document.activeElement===document.querySelectorAll('[data-library-results] [data-library-open]:not(:disabled)')[1]`),'down selects next result');
  await key('Up');
  await key('Escape');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')`),'escape restores search focus');
  await clickText('打开项目','[data-workspace-library] button');
  await selectSurface('资源库');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')`),'returning plus focuses search');
  await click('[data-workspace-add]');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')`),'plus refocuses already open library');
  check('resource entry focuses search; arrows skip disabled results; Escape returns to search');

  await waitFor(settled,'results settled before delayed search');
  const oldRows=await rows(), requests=[];
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/*/library/recent*`]}, (details, callback)=>{
    if(details.method!=='GET') {callback({});return;}
    const query=new URL(details.url).searchParams.get('q');requests.push(query);
    setTimeout(()=>callback({}),600);
  });
  for(const value of ['C','Ca','Can','Canvas 01']) {await input(search,value);await delay(25);}
  await waitFor(()=>requests.includes('Canvas 01'),'debounced request');
  assert.deepEqual(requests,['Canvas 01'],'rapid typing issues only one request');
  assert.deepEqual(await rows(),oldRows,'previous rows remain visible while loading');
  await key('Return');
  assert.ok(await js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden')==='false'`),'pending Enter does not open stale result');
  await waitFor(settled,'search completed');
  assert.ok((await rows()).every(row=>row.includes('Canvas 01')),'completed search replaces old rows');
  check('search debounces rapid typing, preserves previous rows and blocks stale Enter');

  const count=requests.length;
  await js(`document.querySelector('${search}').dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true,data:'hua'}))`);
  await input(search,'画');await delay(300);
  await js(`document.querySelector('${search}').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,isComposing:true}))`);
  assert.equal(requests.length,count,'composition does not request incomplete text');
  assert.ok(await js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden')==='false'`),'IME Enter does not open a result');
  await js(`document.querySelector('${search}').dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'画'}))`);
  await waitFor(()=>requests.includes('画'),'committed composition queried');
  await input(search,'Canvas 02');
  await waitFor(settled,'replacement query settled');
  assert.equal((await rows()).length,1,'one matching canvas result');
  assert.ok((await rows()).every(row=>row.includes('Canvas 02')),'outdated response never replaces newest search');
  window.webContents.session.webRequest.onBeforeRequest(null);
  check('composition events wait for commit; latest search wins after an in-flight request');
  await key('Down');
  assert.ok(await js(`document.activeElement.matches('[data-library-open]')`),'result focused before Enter');
  await key('Return');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:smoke-02"][data-selected="true"]'))`),'Enter opens selected canvas');
  await selectSurface('资源库');
  await click('button[aria-label="清除搜索"]');
  await waitFor(settled,'clear search restores recents');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')&&document.querySelector('${search}').value===''`),'clear keeps focus');
  assert.ok((await rows()).some(row=>row.includes('Canvas 01')),'clear restores full recent list');
  await screenshot('library-keyboard-search');
  check('keyboard opens the selected canvas; clearing restores recents and input focus');

  for (const [id, title] of [[sessionID, 'Shared reference current'], [otherSessionID, 'Shared reference other']]) {
    await api(`/sessions/${id}/canvas/items`, 'POST', {id:'shared-library-id',kind:'markdown',title,item:{markdown:`# ${title}`}});
  }
  await js(`import('/src/main.tsx').then(({router}) => router.options.context.queryClient.invalidateQueries({queryKey:['session',${JSON.stringify(sessionID)},'canvas','items']}))`);
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:shared-library-id"]'))`), 'new canvas available');
  await js(`import('/src/state/workspaceStore.ts').then(m => m.closeWorkspaceTabs(${JSON.stringify(sessionID)}, ['canvas:shared-library-id']))`);
  await api(`/sessions/${otherSessionID}/library/recent`, 'POST', {kind:'canvas',itemID:'shared-library-id'});
  await input(search, 'Shared reference');
  await waitFor(settled, 'cross-session search completed');
  const sharedRows = await rows();
  assert.ok(sharedRows.some(row => row.includes('Shared reference other')), 'other session recent canvas is searchable');
  assert.ok(sharedRows.some(row => row.includes('Shared reference current')), 'same ID in another session must not hide the current closed canvas');
  check('search deduplicates canvases by session and item identity');
}

async function seedConversation(sessionID) {
  for (let i = 1; i <= 24; i++) {
    const response = await api(`/sessions/${sessionID}/submit`, "POST", {
      clientMessageID: `reading-${i}`,
      parts: [{ type: "text", text: `第${i}条阅读位置验收。` + "这是一段长消息，工作区变化后应该继续阅读同一位置。".repeat(24) }],
    });
    await waitFor(async () => (await api(`/sessions/${sessionID}/turns?limit=1`)).turns.some(turn => turn.id === response.turnID && turn.status === 'completed'), 'mock turn completed');
  }
}
async function verifyConversationRestore(primaryID, secondaryID) {
  phase = 'conversation restoration';
  window.setContentSize(1440, 920);
  const root = (id) => `.pudding-conversation[data-session-id="${id}"]`;
  const viewport = (id) => `document.querySelector('${root(id)} [data-transcript-viewport]')`;
  const draft = (id) => js(`import('/src/state/sessionDraftStore.ts').then(m => m.useSessionDraftStore.getState().drafts[${JSON.stringify(id)}])`);
  const navigate = async (id) => {
    await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(id)}}}))`);
    await waitFor(() => js(`Boolean(${viewport(id)}?.querySelector('[data-transcript-turn-id]'))`), 'conversation ready');
    await delay(350);
  };
  const readingPosition = (id) => js(`(() => {
    const v=${viewport(id)}, top=v.getBoundingClientRect().top;
    const el=Array.from(v.querySelectorAll('[data-transcript-turn-id]')).find(el => el.getBoundingClientRect().bottom > top + 1);
    return {turn:el?.dataset.transcriptTurnId, offset:el?.getBoundingClientRect().top-top, scrollTop:v.scrollTop, end:v.scrollHeight-v.clientHeight-v.scrollTop};
  })()`);
  const readHistory = async (id, distance) => {
    const point = await js(`(() => { const r=${viewport(id)}.getBoundingClientRect(); return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+100)}; })()`);
    app.focus({steal:true}); window.focus();
    await waitFor(() => window.isFocused(), 'reading window focused');
    window.webContents.sendInputEvent({type:'mouseMove',...point});
    for (let step = 0; step < 4; step++) {
      window.webContents.sendInputEvent({type:'mouseWheel',...point,deltaX:0,deltaY:distance/4});
      await delay(100);
    }
    await delay(600);
    const position = await readingPosition(id);
    assert.ok(position.end > 500, 'fixture actually reads history above the latest turn');
    return position;
  };
  const assertPosition = async (id, expected, label) => {
    const actual = await readingPosition(id);
    console.info('[workspace-smoke] READING', JSON.stringify({label, expected, actual}));
    assert.equal(actual.turn, expected.turn, `${label}: same reading turn`);
    assert.ok(Math.abs(actual.offset - expected.offset) < 4, `${label}: reading offset preserved`);
  };
  await navigate(primaryID);
  await input(`${root(primaryID)} textarea`, '会话甲的草稿');
  let releaseUpload;
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/${primaryID}/attachments`]}, (_details, callback) => { releaseUpload = () => callback({}); });
  await js(`(() => {
    const transfer = new DataTransfer();
    transfer.items.add(new File(['conversation attachment'], 'reading-state.txt', {type:'text/plain'}));
    const input=document.querySelector('${root(primaryID)} input[type="file"]');
    input.files=transfer.files; input.dispatchEvent(new Event('change',{bubbles:true}));
  })()`);
  await waitFor(() => Boolean(releaseUpload), 'attachment upload held in flight');
  await navigate(secondaryID);
  await input(`${root(secondaryID)} textarea`, '会话乙的草稿');
  releaseUpload();
  window.webContents.session.webRequest.onBeforeRequest(null);
  await waitFor(async () => (await draft(primaryID)).attachments[0]?.status === 'uploaded', 'real attachment uploaded');
  assert.equal((await draft(secondaryID)).attachments.length, 0, 'delayed upload belongs only to original session');
  await navigate(primaryID);
  assert.ok(await js(`document.querySelector('${root(primaryID)}').textContent.includes('reading-state.txt')`), 'uploaded attachment is visible after switching back');
  check('attachment upload completing after a session switch remains in its source draft');
  const originalDraft = await draft(primaryID);
  let before = await readHistory(primaryID, 1800);
  for (const label of ['专注', '退出专注', '收起工作区', '打开工作区']) {
    await click(`button[aria-label="${label}"]`);
    await delay(400);
  }
  assert.deepEqual(await draft(primaryID), originalDraft, 'layout toggles preserve complete draft and attachment');
  await assertPosition(primaryID, before, 'focus and workspace round trip');
  check('focus/workspace toggles preserve conversation draft, attachment and reading position');
  await click(`${root(primaryID)} button[aria-label="跳到最新"]`);
  await waitFor(async () => (await readingPosition(primaryID)).end < 8, 'prepare near-bottom reading');
  before = await readHistory(primaryID, 650);
  for (const label of ['专注', '退出专注', '收起工作区', '打开工作区']) {
    await click(`button[aria-label="${label}"]`);
    await delay(400);
  }
  await assertPosition(primaryID, before, 'near-bottom layout round trip');
  check('layout changes near the bottom do not resume latest following');
  await navigate(secondaryID);
  await input(`${root(secondaryID)} textarea`, '会话乙的草稿');
  const secondPosition = await readHistory(secondaryID, 2600);
  await navigate(primaryID);
  assert.equal(await js(`document.querySelector('${root(primaryID)} textarea').value`), originalDraft.text);
  assert.deepEqual(await draft(primaryID), originalDraft);
  check('session switching preserves separate composer text and attachment');
  await assertPosition(primaryID, before, 'primary session restored');
  await navigate(secondaryID);
  await assertPosition(secondaryID, secondPosition, 'secondary session restored');
  check('independent reading positions restored across sessions');
  // A past jump-to-latest command must not replay on the next session mount.
  await click(`${root(secondaryID)} button[aria-label="跳到最新"]`);
  await waitFor(async () => (await readingPosition(secondaryID)).end < 8, 'jump reaches latest');
  const afterJump = await readHistory(secondaryID, 2100);
  await navigate(primaryID);
  await navigate(secondaryID);
  await assertPosition(secondaryID, afterJump, 'history after previous jump');
  check('past jump-to-latest command does not override restored history');

  const append = async (id, key) => {
    const response = await api(`/sessions/${id}/submit`, 'POST', {clientMessageID:key,parts:[{type:'text',text:'新的消息不应打断历史阅读。'}]});
    await waitFor(async () => (await api(`/sessions/${id}/turns?limit=1`)).turns.some(t => t.id === response.turnID && t.status === 'completed'), 'appended turn completed');
    await delay(350);
  };
  await append(secondaryID, 'read-while-appending');
  await assertPosition(secondaryID, afterJump, 'new message while reading history');
  await click(`${root(secondaryID)} .pudding-conversation-bottom-dock button`);
  await append(secondaryID, 'follow-while-appending');
  await waitFor(async () => (await readingPosition(secondaryID)).end < 8, 'latest continues following new messages');
  await navigate(primaryID);
  await navigate(secondaryID);
  assert.ok((await readingPosition(secondaryID)).end < 8, 'latest position survives session switch');
  check('new messages preserve history reading; explicit latest remains pinned');

  // An explicit reveal of an older, unloaded turn takes precedence over the saved position.
  const firstTurn = (await api(`/sessions/${primaryID}/turns?limit=100`)).turns[0];
  await js(`import('/src/state/transcriptRevealStore.ts').then(m=>m.requestTranscriptTurnReveal(${JSON.stringify(primaryID)},${JSON.stringify(firstTurn.id)},'assistant'))`);
  await navigate(primaryID);
  await waitFor(() => js(`Boolean(${viewport(primaryID)}.querySelector('[data-transcript-turn-id="${firstTurn.id}"] [data-transcript-turn-reveal]'))`), 'explicit reveal loads and highlights older turn');
  const oldestPosition = await readingPosition(primaryID);
  await navigate(secondaryID);
  await navigate(primaryID);
  await assertPosition(primaryID, oldestPosition, 'paginated history restored');
  check('explicit turn reveal overrides saved position; older loaded history then restores');

  await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(primaryID)},split:${JSON.stringify(secondaryID)}}}))`);
  await waitFor(() => js(`Boolean(${viewport(secondaryID)})`), 'split conversation mounted');
  await delay(400);
  const splitPosition = await readHistory(secondaryID, 1800);
  await navigate(primaryID);
  await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(primaryID)},split:${JSON.stringify(secondaryID)}}}))`);
  await waitFor(() => js(`Boolean(${viewport(secondaryID)})`), 'split conversation reopened');
  await delay(400);
  await assertPosition(secondaryID, splitPosition, 'split pane reopened');
  await navigate(secondaryID);
  assert.ok((await readingPosition(secondaryID)).end < 8, 'split reading does not overwrite primary reading position');
  check('split pane unmount/reopen restores its own reading position');
  await click(`${root(secondaryID)} textarea`);
  window.webContents.sendInputEvent({type:'keyDown',keyCode:'F',modifiers:['meta']});
  window.webContents.sendInputEvent({type:'keyUp',keyCode:'F',modifiers:['meta']});
  await waitFor(() => js(`Boolean(document.querySelector('${root(secondaryID)} input[aria-label="搜索当前会话"]'))`), 'native find shortcut opens conversation search');
  await input(`${root(secondaryID)} input[aria-label="搜索当前会话"]`, '第1条阅读位置验收');
  await waitFor(() => js(`Boolean(${viewport(secondaryID)}.querySelector('[data-transcript-search-active]'))`), 'search loads and navigates to uncached old message');
  assert.ok(await js(`${viewport(secondaryID)}.querySelector('[data-transcript-search-active]').textContent.includes('第1条阅读位置验收')`));
  await click(`${root(secondaryID)} button[aria-label="关闭会话搜索"]`);
  check('native conversation search navigates from latest to an unloaded older message');
  await screenshot('conversation-restored');
}

async function verifyEditorContinuity(sessionID, secondaryID, projectRoot) {
  phase = 'editor continuity';
  window.setContentSize(1440, 920);
  const open = async (name, targetSessionID = sessionID) => {
    await js(`import('/src/state/projectRevealStore.ts').then(m => m.requestProjectFileReveal({sessionID:${JSON.stringify(targetSessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:${JSON.stringify(name)}}))`);
    await waitFor(async () => (await tabs()).some(tab => tab.title === name && tab.selected), 'open ' + name);
    await js(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent===${JSON.stringify(name)}).dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);
  };
  const activate = async (name) => {
    await waitFor(() => js(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).some(el=>el.textContent===${JSON.stringify(name)})`), 'tab ready ' + name);
    await js(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent===${JSON.stringify(name)}).scrollIntoView({block:'nearest',inline:'nearest'})`);
    await js(`new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))`);
    await clickElement(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent===${JSON.stringify(name)})`);
    await waitFor(async () => (await tabs()).some(tab => tab.title === name && tab.selected), 'activate ' + name);
    await delay(150);
  };
  await open('continuity-a.ts');
  await waitFor(() => js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .monaco-editor textarea'))`), 'source editor ready');
  await js(`(async()=>{
    const {editor}=await import('/__workspace_editor.js');
    window.__monaco=editor;
    window.__editorA=editor.getEditors().find(e=>e.getDomNode()?.getClientRects().length);
    window.__modelA=window.__editorA.getModel();
    window.__editorA.setPosition({lineNumber:2,column:3});
    window.__editorA.focus();
  })()`);
  await window.webContents.insertText('PRESERVED_UNDO');
  await waitFor(() => js(`window.__modelA.getValue().includes('PRESERVED_UNDO')`), 'native editor edit');
  await js(`(async()=>{
    const e=window.__editorA;e.setPosition({lineNumber:1,column:1});
    await e.getAction('editor.fold').run();
    e.setPosition({lineNumber:70,column:4});e.setScrollTop(800);
  })()`);
  await delay(200);
  const before = await js(`window.__editorA.saveViewState()`);
  assert.ok(before.contributionsState['editor.contrib.folding'].collapsedRegions?.length, 'fixture has a collapsed code block');
  await open('continuity-b.ts');
  await waitFor(() => js(`window.__monaco.getEditors().length===2`), 'second editor created');
  await js(`window.__editorB=window.__monaco.getEditors().find(e=>e!==window.__editorA);window.__editorB.setPosition({lineNumber:2,column:1});window.__editorB.focus()`);
  await window.webContents.insertText('SECOND_FILE');
  await waitFor(() => js(`window.__editorB.getValue().includes('SECOND_FILE')`), 'second file edit');
  await activate('continuity-a.ts');
  const after = await js(`window.__editorA.saveViewState()`);
  console.info('[editor-continuity]', JSON.stringify({before,after}));
  assert.ok(await js(`window.__monaco.getEditors().includes(window.__editorA)`), 'original editor retained');
  assert.deepEqual(after.cursorState, before.cursorState, 'cursor retained');
  assert.deepEqual(after.viewState, before.viewState, 'scroll retained');
  assert.deepEqual(after.contributionsState, before.contributionsState, 'folding retained');
  app.focus({steal:true}); window.focus();
  await waitFor(() => window.isFocused(), 'undo window focused');
  await js(`window.__editorA.focus()`);
  assert.ok(await js(`window.__modelA.canUndo()`), 'undo stack retained');
  window.webContents.sendInputEvent({type:'keyDown',keyCode:'Z',modifiers:['meta']});
  window.webContents.sendInputEvent({type:'keyUp',keyCode:'Z',modifiers:['meta']});
  await waitFor(() => js(`!window.__modelA.getValue().includes('PRESERVED_UNDO')`), 'undo survives tab switching');
  assert.ok(await js(`window.__editorB.getValue().includes('SECOND_FILE')`), 'undo remains scoped to file A');
  check('file switches preserve cursor, scroll, folds and native undo; edits isolated per file');

  await open('file-01.md');
  await waitFor(() => js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .vditor-ir [contenteditable="true"]'))`), 'Markdown editor ready');
  await js(`window.__markdown=document.querySelector('[data-project-document]:not([hidden]) .vditor');window.__markdownScroll=document.querySelector('[data-project-document]:not([hidden]) .vditor-content')`);
  await activate('continuity-a.ts');
  await activate('file-01.md');
  assert.ok(await js(`window.__markdown===document.querySelector('[data-project-document]:not([hidden]) .vditor')`), 'Markdown editor retained');
  check('Markdown editor instance retained across file switches');

  await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(secondaryID)}}}))`);
  await open('continuity-a.ts', secondaryID);
  await waitFor(() => js(`window.__monaco.getEditors().length===3`), 'separate editor in second session');
  assert.ok(await js(`!window.__monaco.getEditors().find(e=>e.getDomNode()?.getClientRects().length).getValue().includes('SECOND_FILE')`), 'second session has independent state');
  await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(sessionID)}}}))`);
  await activate('continuity-b.ts');
  assert.ok(await js(`window.__monaco.getEditors().includes(window.__editorB)&&window.__editorB.getValue().includes('SECOND_FILE')`), 'returning to session retains its unsaved draft');
  await click('[data-project-document]:not([hidden]) button[aria-label="保存文件"]');
  await waitFor(() => fs.readFileSync(path.join(projectRoot, 'continuity-b.ts'),'utf8').includes('SECOND_FILE'), 'save correct file after session switch');
  await activate('continuity-a.ts');
  await clickElement(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-content-tab')).find(el=>el.querySelector('.pudding-workspace-tab-select').textContent==='continuity-a.ts').querySelector('button[aria-label^="关闭"]')`);
  await waitFor(() => js(`window.__modelA.isDisposed()`), 'closing tab disposes its model');
  assert.equal(await js(`window.__monaco.getEditors().length`), 2, 'other open editor models retained');
  check('session switching preserves file drafts; save routes correctly; close disposes only that file');
}

async function verifyBatchSaveFailure(sessionID) {
  phase = "batch save failure";
  const ids = ["smoke-01", "smoke-02"];
  for (const id of ids) {
    await api(`/sessions/${sessionID}/canvas/items/${id}/save`, "POST");
    await api(`/sessions/${sessionID}/canvas/items/${id}`, "PUT", {kind:"markdown",title:id,item:{markdown:`Unsaved ${id}`}});
  }
  await js(`import('/src/main.tsx').then(({router}) => router.options.context.queryClient.invalidateQueries({queryKey:['session',${JSON.stringify(sessionID)},'canvas','items']}))`);
  await selectTopTab("canvas:smoke-01");
  const point = await js(`(() => {const r=document.querySelector('[data-workspace-tab-key="canvas:smoke-01"]').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()`);
  window.webContents.sendInputEvent({type:"mouseDown",...point,button:"right",clickCount:1});
  window.webContents.sendInputEvent({type:"mouseUp",...point,button:"right",clickCount:1});
  await clickText("关闭全部标签", '[role="menuitem"]');
  const requests = [];
  let failSecond = true;
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/${sessionID}/canvas/items/*/save`]}, (details, callback) => {
    if (details.method !== "POST") { callback({}); return; }
    const id = new URL(details.url).pathname.split("/").at(-2);
    requests.push(id);
    callback({cancel: failSecond && id === "smoke-02"});
  });
  try {
    await clickText("保存并关闭", '[role="alertdialog"] button');
    await waitFor(() => js(`Boolean(Array.from(document.querySelectorAll('[role="alertdialog"] button')).find(el => el.textContent === '保存并关闭' && !el.disabled))`), "failed batch becomes retryable");
    assert.deepEqual(requests, ids, "batch stops at the failing save");
    assert.equal(await js(`document.querySelectorAll('[data-workspace-tab-key]').length`), 21, "partially saved batch keeps all tabs open");
    const items = (await api(`/sessions/${sessionID}/canvas/items`)).items;
    assert.equal(Boolean(items.find(item => item.id === ids[0]).savedDirty), false, "first save committed");
    assert.equal(items.find(item => item.id === ids[1]).savedDirty, true, "failed item remains dirty");
    assert.equal(items.find(item => item.id === ids[1]).item.markdown, `Unsaved ${ids[1]}`, "failed save preserves work");
    failSecond = false;
    await clickText("保存并关闭", '[role="alertdialog"] button');
    await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key]')`), "successful retry closes the batch");
    assert.deepEqual(requests, [...ids, ids[1]], "retry only saves the remaining dirty item");
    assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.length, 20, "batch close preserves canonical items");
  } finally {
    window.webContents.session.webRequest.onBeforeRequest(null);
  }
  check("partial batch save failure keeps all tabs and drafts; retry saves only pending work then closes");
}

async function verifyPreviewNarrow(sessionID) {
  phase = "preview narrow";
  window.setContentSize(1440, 920);
  await js(`(async () => {
    const {router} = await import('/src/main.tsx');
    await router.navigate({to:'/',search:{session:${JSON.stringify(sessionID)}}});
    const {openFilePreview} = await import('/src/state/filePreviewStore.ts');
    openFilePreview({sessionID:${JSON.stringify(sessionID)},path:'preview.txt',content:'NARROW_PREVIEW_CONTENT',source:'read',lineStart:1,lineStep:1,truncated:false});
  })()`);
  await waitFor(() => js(`document.body.innerText.includes('NARROW_PREVIEW_CONTENT')`), "preview opens in project");
  window.setContentSize(560, 760);
  window.webContents.setZoomFactor(1.5);
  await delay(400);
  await screenshot("preview-narrow");
  assert.ok(await js(`document.body.innerText.includes('NARROW_PREVIEW_CONTENT')`), "preview stays visible when no project file is selected");
  check("standalone file preview remains visible in a narrow project");
}

async function verifyComputerPreview(sessionID, otherSessionID) {
  phase = 'native computer preview';
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {ComputerUseHost} = require('../computer-use-host.cjs');
  const native = new ComputerUseHost({binaryPath:path.join(repo, 'bin/Pudding Computer Use.app/Contents/MacOS/PuddingComputerUseHelper')});
  const children = [];
  const fixtureBundleID = 'com.teatak.pudding.computer-use-fixture';
  const fixtureApp = path.join(repo, 'bin/Pudding Computer Use Fixture.app');
  const fixture = async () => {
    const child = spawn(path.join(fixtureApp, 'Contents/MacOS/PuddingComputerUseFixture'), [], {stdio:'ignore'});
    children.push(child);
    await waitFor(async () => (await native.listApps()).apps.some(a => a.bundleID === fixtureBundleID && a.instances.some(i => i.pid === child.pid)), 'native fixture starts');
    const used = await native.useApp({bundleID:fixtureBundleID,appPath:fixtureApp,pid:child.pid,foreground:false});
    const target = used.windows.find(w => w.title === 'Computer Use Fixture'); assert.ok(target); return target;
  };
  const entry = () => computerPreviewManager.entries.get(sessionID);
  const image = () => js(`document.querySelector('[data-computer-preview] img[draggable="false"]')?.src`);
  const startTurn = async (id, key, words = 2000) => {
    const result = await api(`/sessions/${id}/submit`, 'POST', {clientMessageID:key,parts:[{type:'text',text:'preview '.repeat(words)}]});
    await waitFor(() => js(`import('/src/state/overlayStore.ts').then(m => m.useOverlayStore.getState().runningTurns[${JSON.stringify(id)}] === ${JSON.stringify(result.turnID)})`), 'real SSE turn begins');
    await waitFor(() => [...computerPreviewManager.clients.values()].some(c => c.sessionID === id && c.turnID === result.turnID), 'preview turn subscription');
    return result.turnID;
  };
  const operation = async (target, turnID, id = sessionID) => {
    const response = await fetch(`${computerBridgeIdentity.url}/computer/observe`, {
      method:'POST',headers:{authorization:`Bearer ${computerBridgeIdentity.token}`,'content-type':'application/json','x-pudding-turn-id':turnID},
      body:JSON.stringify({sessionID:id,appID:fixtureBundleID,windowID:target.windowID,maxElements:30}),
    });
    assert.ok(response.ok, await response.text());
  };
  const live = target => waitFor(async () => Boolean(await image()) && entry()?.target.pid === target.pid, 'real live preview frame');
  const settle = () => waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), 'workspace transition settles');
  try {
    const permissions = await native.permissions(); assert.ok(permissions.accessibility && permissions.screenRecording);
    const first = await fixture(), second = await fixture();
    window.setContentSize(1440, 920);
    await click('button[aria-label="收起工作区"]'); await settle();
    const turnID = await startTurn(sessionID, 'preview-running');
    await operation(first, turnID); await live(first);
    const captured = entry().child;
    assert.equal(await js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).presentation)`), 'hidden');
    const geometry = await js(`(() => { const card=document.querySelector('[data-computer-preview]'), artifacts=document.querySelector('[aria-label="临时工作区内容"]'); const c=card.getBoundingClientRect(),a=artifacts.getBoundingClientRect();return {top:c.top,bottom:c.bottom,artifactBottom:a.bottom,height:innerHeight}})()`);
    assert.ok(geometry.top >= geometry.artifactBottom && geometry.bottom <= geometry.height, JSON.stringify(geometry));
    const before = await image();
    const observed = await native.observe({bundleID:fixtureBundleID,windowID:first.windowID,maxElements:100});
    const editor = observed.elements.find(e => e.role === 'AXTextField' && !e.secure); assert.ok(editor);
    await native.act({bundleID:fixtureBundleID,windowID:first.windowID,elementID:editor.elementID,action:'set_value',value:'Live preview changed'});
    await waitFor(async () => (await image()) !== before, 'preview updates as native window changes without another observe');
    assert.equal(entry().child, captured, 'continuous capture reused between operations');
    await screenshot('computer-preview-live');
    await js(`window.puddingElectronTheme.setTheme('dark')`);
    await waitFor(() => js(`document.documentElement.classList.contains('dark')`), 'dark preview theme');
    await js(`Promise.all(document.querySelector('[data-computer-preview]').getAnimations({subtree:true}).filter(a => a instanceof CSSTransition).map(a=>a.finished))`);
    await screenshot('computer-preview-dark');
    await js(`window.puddingElectronTheme.setTheme('light')`);
    check('real ScreenCaptureKit frames below artifacts, live window changes, no workspace expansion or repeated model screenshots');
    const assertAspect = async ratio => {
      await waitFor(() => entry()?.frame && Math.abs(entry().frame.width / entry().frame.height - ratio) < 0.01, 'native frame follows current window aspect');
      await waitFor(() => js(`(() => {const card=document.querySelector('[data-computer-preview]'), img=card?.querySelector('img[draggable="false"]');if(!img?.complete)return false; const r=img.getBoundingClientRect(),box=img.parentElement.getBoundingClientRect();return Math.abs(r.width/r.height-${ratio})<0.01 && Math.abs(box.width-r.width)<1 && Math.abs(box.height-r.height)<=2 && card.firstElementChild.getBoundingClientRect().height===32;})()`), 'single-line title and image fit without letterboxing');
    };
    await assertAspect(first.frame.width / first.frame.height);
    for (const [label, ratio] of [['wide',960/532],['portrait',360/672],['restored',520/532]]) {
      children.find(child => child.pid === first.pid).kill('SIGUSR1');
      await assertAspect(ratio);
      assert.equal(entry().child, captured, 'resizing reuses the same stream');
      await screenshot(`computer-preview-${label}`);
    }
    check('native window aspect follows wide and portrait resizing, without borders or restarting capture; title stays one line');


    await click('button[aria-label="打开工作区"]'); await settle();
    await waitFor(() => !entry()?.child && captured.exitCode !== null, 'workspace expansion releases capture process');
    assert.equal(await js(`document.querySelectorAll('[data-computer-preview]').length`), 0);
    assert.ok((await api(`/sessions/${sessionID}`)).running, 'hiding preview does not cancel the turn');
    await click('button[aria-label="收起工作区"]'); await settle(); await live(first);
    assert.notEqual(entry().child, captured);
    const resumed = entry().child;
    await click('[data-computer-preview]');
    await waitFor(async () => (await native.listApps()).apps.find(a => a.bundleID === fixtureBundleID)?.instances.some(i => i.pid === first.pid && i.active), 'click raises exact native instance');
    const nativeState = await native.observe({bundleID:fixtureBundleID,windowID:first.windowID,maxElements:100});
    assert.equal(nativeState.elements.find(e => e.role === 'AXTextField' && !e.secure)?.value, 'Live preview changed', 'preview click never types into the app');
    check('workspace open pauses capture without stopping operations; collapse resumes; preview click raises the exact application');

    await operation(second, turnID); await live(second);
    await waitFor(() => resumed.exitCode !== null, 'window switch releases old stream');
    const expected = entry().target.windowID;
    await operation(first, turnID, otherSessionID);
    assert.equal(entry().target.windowID, expected, 'another session cannot replace this preview');
    window.setContentSize(720, 920);
    await waitFor(() => js(`(() => {const r=document.querySelector('[data-computer-preview]')?.getBoundingClientRect();return r && r.width>100 && r.x>=0 && r.right<=innerWidth && r.bottom<=innerHeight})()`), 'narrow preview stays visible');
    await screenshot('computer-preview-narrow');
    await js(`import('/src/state/workspaceStore.ts').then(m => m.closeWorkspaceTabs(${JSON.stringify(sessionID)}, m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder))`);
    await waitFor(() => js(`!document.querySelector('[aria-label="临时工作区内容"]') && Boolean(document.querySelector('[data-computer-preview]'))`), 'preview remains independent when all artifacts close');
    check('window switching drops old frames; session isolation; narrow layout and no-artifact preview');

    const cancelled = entry().child;
    await api(`/sessions/${sessionID}/cancel`, 'POST');
    await waitFor(() => cancelled.exitCode !== null && !entry(), 'turn cancellation stops and releases capture');
    await waitFor(() => js(`!document.querySelector('[data-computer-preview]')`), 'cancelled preview removed');
    const completedTurnID = await startTurn(sessionID, 'preview-complete', 100);
    await operation(first, completedTurnID); await live(first);
    const completed = entry().child;
    await waitFor(() => js(`document.querySelector('[data-computer-preview]')?.dataset.status === 'complete'`), 'real turn completion shows finished preview');
    await waitFor(() => completed.exitCode !== null, 'completion stops capture');
    const finishedAt = Date.now();
    await delay(5_000);
    assert.equal(await js(`document.querySelector('[data-computer-preview]')?.dataset.status`), 'complete', 'last frame remains after the old three-second expiry');
    await waitFor(() => js(`!document.querySelector('[data-computer-preview]')`), 'completed preview disappears after thirty seconds', 35_000);
    assert.ok(Date.now() - finishedAt >= 29_000, 'completed preview retains the final result for thirty seconds');
    check('real SSE cancel stops immediately; completion stops capture but retains the last frame for thirty seconds');

    const closeTurn = await startTurn(sessionID, 'preview-window-close');
    await operation(second, closeTurn); await live(second);
    await native.quitApp({bundleID:fixtureBundleID,pid:second.pid});
    await waitFor(() => !entry()?.child, 'closing target application stops capture');
    assert.equal(entry().frame, null, 'closed window cannot display stale image');
    await api(`/sessions/${sessionID}/cancel`, 'POST');
    check('native application exit stops capture and clears stale content');
  } finally {
    for (const child of children) if (child.exitCode === null) await native.quitApp({bundleID:fixtureBundleID,pid:child.pid}).catch(() => child.kill());
    await native.stop();
  }
}

async function verifyArtifactVisibility(sessionID, otherSessionID) {
  phase = "open workspace artifacts";
  window.setContentSize(1440, 920);
  const activitySelector = `[data-session-id="${sessionID}"] [role="group"][aria-label="临时工作区内容"]`;
  const labels = () => js(`Array.from(document.querySelectorAll(${JSON.stringify(activitySelector + ' button[aria-label^="打开工作区内容:"]')})).map(el => el.getAttribute('aria-label'))`);
  const settle = () => waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), "workspace transition settles");
  const toggle = async (open) => { await click(`button[aria-label="${open ? '打开' : '收起'}工作区"]`); await settle(); };
  const tab = await api(`/sessions/${sessionID}/browser/tabs`, "POST");
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="browser:${tab.id}"]'))`), "browser fixture tab");
  await toggle(false);
  await waitFor(async () => (await labels()).length === 4, "three open canvases and browser outside workspace");

  await toggle(true);
  await selectTopTab('canvas:smoke-01');
  await click('[data-workspace-tab-key="canvas:smoke-01"] .pudding-workspace-tab-close');
  await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key="canvas:smoke-01"]')`), "canvas tab closed");
  await toggle(false);
  assert.equal((await labels()).some(label => label.endsWith('Canvas 01')), false, "closed canvas must disappear from external artifacts");
  assert.equal((await labels()).length, 3, "remaining canvases and browser stay visible");
  assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.length, 3, "close preserves canonical canvas content");
  window.setContentSize(720, 920);
  await waitFor(() => js(`document.querySelector('[data-session-id="${sessionID}"]').dataset.activityRail !== 'true'`), "narrow activity dock");
  assert.equal((await labels()).some(label => label.endsWith('Canvas 01')), false, "dock also excludes closed canvas");
  window.setContentSize(1440, 920);
  window.webContents.reload();
  await waitFor(async () => (await labels()).length === 3, "closed canvas remains absent after renderer reload");
  assert.equal((await labels()).some(label => label.endsWith('Canvas 01')), false);
  check("closing a canvas removes it from rail and dock, persists after reload, and preserves saved content");

  await toggle(true);
  await selectSurface('资源库');
  await input('[data-workspace-library] input', 'Canvas 01');
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy') === 'false' && Array.from(document.querySelectorAll('[data-library-row]')).some(el => el.textContent.includes('Canvas 01'))`), "closed canvas searchable in library");
  await clickElement(`Array.from(document.querySelectorAll('[data-library-row]')).find(el => el.textContent.includes('Canvas 01'))`);
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:smoke-01"][data-selected="true"]'))`), "library reopens canvas");
  await toggle(false);
  await waitFor(async () => (await labels()).length === 4, "reopened canvas returns outside workspace");
  check("reopening from resource library restores the existing canvas to external artifacts");

  await toggle(true);
  await selectTopTab(`browser:${tab.id}`);
  await click(`[data-workspace-tab-key="browser:${tab.id}"] .pudding-workspace-tab-close`);
  await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key="browser:${tab.id}"]')`), "browser tab released");
  await toggle(false);
  await waitFor(async () => (await labels()).length === 3, "browser removed from external artifacts");
  await toggle(true);
  await selectTopTab('canvas:smoke-01');
  const point = await js(`(() => {const r=document.querySelector('[data-workspace-tab-key="canvas:smoke-01"]').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()`);
  window.webContents.sendInputEvent({type:'mouseDown',...point,button:'right',clickCount:1});
  window.webContents.sendInputEvent({type:'mouseUp',...point,button:'right',clickCount:1});
  await clickText('关闭全部标签', '[role="menuitem"]');
  await waitFor(() => js(`document.querySelectorAll('[data-workspace-tab-key]').length === 0`), "all tabs closed");
  await toggle(false);
  await waitFor(() => js(`!document.querySelector(${JSON.stringify(activitySelector)})`), "empty external artifact card removed");
  assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.length, 3);
  check("closing browser updates external list; closing all tabs removes the artifact card without deleting canvases");

  await api(`/sessions/${otherSessionID}/canvas/items`, 'POST', {id:'smoke-01',kind:'markdown',title:'Other session canvas',item:{markdown:'# Other session'}});
  await js(`import('/src/main.tsx').then(({router}) => router.navigate({to:'/',search:{session:${JSON.stringify(otherSessionID)}}}))`);
  await waitFor(() => js(`Boolean(document.querySelector('[data-session-id="${otherSessionID}"] button[aria-label="打开工作区内容: Other session canvas"]'))`), "same canvas ID in another session remains open");
  check("canvas closure stays scoped to its session");
}

async function verifyAutomationPresentation(sessionID, otherSessionID) {
  phase = "automation workspace presentation";
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  window.setContentSize(1440, 920);
  const state = (id = sessionID) => js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(id)}))`);
  const settle = () => waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), "workspace transition settles");
  const hide = async () => {
    await click('button[aria-label="收起工作区"]');
    await settle();
    assert.equal((await state()).presentation, "hidden");
  };
  const revealBrowser = async (tabID, explicit = false) => {
    await js(`import('/src/state/browserRevealStore.ts').then(m => m.${explicit ? 'openBrowserReveal' : 'requestBrowserReveal'}(${JSON.stringify(sessionID)}, ${JSON.stringify(tabID)}))`);
    await waitFor(() => js(`import('/src/state/browserRevealStore.ts').then(m => !m.useBrowserRevealStore.getState().latest)`), "browser reveal consumed");
  };
  // Same save, query invalidation and reveal sequence used by canvas MCP tools.
  const generateCanvas = async (id, targetSessionID = sessionID) => {
    await js(`(async () => {
      const {createCanvasItem} = await import('/src/api/client.ts');
      const {queryKeys} = await import('/src/api/queryKeys.ts');
      const {router} = await import('/src/main.tsx');
      const {requestCanvasReveal} = await import('/src/state/canvasRevealStore.ts');
      const item = await createCanvasItem(${JSON.stringify(token)}, ${JSON.stringify(targetSessionID)}, {id:${JSON.stringify(id)},kind:'markdown',title:'Generated canvas',item:{kind:'markdown',content:'# Generated quietly'}});
      await router.options.context.queryClient.invalidateQueries({queryKey:queryKeys.canvasItems(item.sessionID)});
      requestCanvasReveal(item.sessionID, item.id);
    })()`);
    if (targetSessionID === sessionID) await waitFor(async () => (await state()).activeTab === `canvas:${id}`, "generated canvas selected");
  };

  await hide();
  const tab = await api(`/sessions/${sessionID}/browser/tabs`, "POST");
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="browser:${tab.id}"]'))`), "new browser tab reconciled");
  assert.equal((await state()).presentation, "hidden", "new browser snapshot must not expand workspace");
  const route = `/sessions/${sessionID}/browser/tabs/${tab.id}`;
  await api(`${route}/open`, "POST", { url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?automation=1` });
  assert.equal((await state()).presentation, "hidden", "automation start must not expand workspace");
  await waitFor(() => js(`(() => {
    const host = document.querySelector('.pudding-browser-runtime-host[data-pip="true"][data-anchored="true"]');
    const rect = host?.getBoundingClientRect();
    return Boolean(rect && rect.width > 0 && rect.height > 0 && rect.x >= 0 && rect.y >= 0 && rect.right <= innerWidth && rect.bottom <= innerHeight);
  })()`), "real browser picture in picture visible within window");
  await revealBrowser(tab.id);
  assert.equal((await state()).presentation, "hidden", "tool completion must not expand workspace");
  await api(`${route}/type`, "POST", { selector: 'input', text: 'Quiet automation', clear: true });
  const guest = webContents.getAllWebContents().find(contents => contents.getType() === "webview" && contents.getURL().includes("automation=1"));
  assert.ok(guest, "real browser webview exists");
  assert.equal(await guest.executeJavaScript('document.querySelector("input").value'), "Quiet automation");
  assert.equal((await state()).presentation, "hidden");
  await screenshot("automation-pip");
  check("browser creation, navigation, typing and completion keep workspace hidden while real PiP is visible");

  await generateCanvas("automation-hidden");
  assert.equal((await state()).presentation, "hidden", "canvas generation must not expand workspace");
  assert.equal((await state(otherSessionID)).presentation, "hidden");
  check("canvas generation selects its result without expanding either session");

  await js(`import('/src/state/canvasRevealStore.ts').then(m => m.openCanvasReveal(${JSON.stringify(sessionID)}, 'automation-hidden'))`);
  await settle();
  assert.equal((await state()).presentation, "standard", "explicit canvas reveal opens workspace");
  await waitFor(() => js(`document.querySelector('.pudding-workspace-stage').innerText.includes('Generated quietly')`), "explicit canvas content visible");
  await click('button[aria-label="专注"]');
  await settle();
  await generateCanvas("automation-focused");
  assert.equal((await state()).presentation, "focused", "canvas must preserve focus mode");
  await api(`${route}/open`, "POST", { url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?automation=2` });
  assert.equal((await state()).presentation, "focused", "browser must preserve focus mode");
  assert.equal(await js(`document.querySelectorAll('.pudding-browser-pip').length`), 0, "expanded workspace has no duplicate PiP");
  check("explicit canvas reveal opens workspace; subsequent canvas and browser automation preserve focus mode");

  await hide();
  await api(`${route}/open`, "POST", { url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?automation=3` });
  await generateCanvas("automation-after-collapse");
  assert.equal((await state()).presentation, "hidden", "later operations respect user collapse");
  const before = await state();
  await generateCanvas("automation-other-session", otherSessionID);
  assert.deepEqual(await state(), before, "background session cannot change current workspace");
  assert.equal((await state(otherSessionID)).presentation, "hidden");
  await revealBrowser(tab.id, true);
  await settle();
  assert.equal((await state()).presentation, "standard", "explicit browser reveal opens workspace");
  check("user collapse persists through later operations; background canvas stays isolated; explicit browser reveal still opens");

  await selectSurface("资源库");
  await click('button[aria-label="新建浏览器标签页"]');
  await waitFor(async () => (await api(`/sessions/${sessionID}/browser/tabs`)).tabs.length === 2, "user-created browser tab");
  assert.equal((await state()).presentation, "standard");
  assert.notEqual((await state()).activeTab, `browser:${tab.id}`, "user-created tab selected");
  check("user new-tab action still creates and displays the browser");
}

async function run() {
  assert.ok(fs.existsSync(process.env.PUDDING_DAEMON_BIN), "Build bin/puddingd before running the smoke");
  const reservation = http.createServer();
  await new Promise((resolve) => reservation.listen(0, "127.0.0.1", resolve));
  const port = reservation.address().port;
  await new Promise((resolve) => reservation.close(resolve));
  process.env.PUDDING_DAEMON_ADDR = `127.0.0.1:${port}`;
  apiBase = `http://${process.env.PUDDING_DAEMON_ADDR}`;
  const { createServer } = await import(pathToFileURL(path.join(repo, "web/node_modules/vite/dist/node/index.js")));
  vite = await createServer({
    root: path.join(repo, "web"), cacheDir: path.join(home, "vite-cache"),
    server: { host: "127.0.0.1", port: 0 }, logLevel: "error",
    plugins: [{ name: "workspace-smoke-fixture",
      resolveId(id) { if (id === '/__workspace_editor.js') return '\0workspace-editor-smoke'; },
      load(id) { if (id === '\0workspace-editor-smoke') return 'export { editor } from "monaco-editor/editor";'; },
      configureServer(server) {
      server.middlewares.use("/__workspace_smoke", (_req, res) => {
        res.setHeader("Content-Type", "text/html");
        res.end(`<!doctype html><title>Workspace fixture</title><style>html{background:white;color:#222}body{font:16px sans-serif;margin:24px}article{padding:8px;border-bottom:1px solid #ddd}</style><input aria-label="Draft" value="unchanged">${"<article>Browser workspace fixture content</article>".repeat(300)}`);
      });
    } }],
  });
  await vite.listen();
  process.env.PUDDING_DEV_URL = `http://127.0.0.1:${vite.httpServer.address().port}`;
  require("../main.cjs");
  await waitFor(() => fs.existsSync(path.join(home, "daemon.token")), "isolated daemon token");
  token = fs.readFileSync(path.join(home, "daemon.token"), "utf8").trim();
  await waitFor(async () => {
    try { return (await fetch(`${apiBase}/health`, { headers: { Authorization: `Bearer ${token}` } })).ok; }
    catch { return false; }
  }, "isolated daemon health");

  phase = "fixtures";
  const projectRoot = path.join(home, "project");
  fs.mkdirSync(projectRoot);
  if (process.env.PUDDING_SMOKE_SCENARIO === "editor-continuity") {
    for (const name of ['continuity-a.ts', 'continuity-b.ts']) fs.writeFileSync(path.join(projectRoot, name), 'function sample() {\n  return 42;\n}\n' + 'const example = 1;\n'.repeat(200));
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "project-draft") fs.writeFileSync(path.join(projectRoot, "draft.txt"), "Original file\n");
  if (process.env.PUDDING_SMOKE_SCENARIO === "tree-reveal") {
    const folder = path.join(projectRoot, "reveal", "nested");
    fs.mkdirSync(folder, { recursive: true });
    for (let i = 0; i < 80; i++) fs.writeFileSync(path.join(folder, `file-${String(i).padStart(2, "0")}.md`), "# Tree fixture");
    fs.writeFileSync(path.join(folder, "target.md"), "# Reveal target");
  }
  for (let i = 1; i <= 20; i++) fs.writeFileSync(path.join(projectRoot, `file-${String(i).padStart(2, "0")}.md`), `# File ${i}\n\n${"Long project document.\n".repeat(150)}`);
  const project = await api("/projects", "POST", { name: "Workspace smoke", rootDirs: [projectRoot] });
  const primary = await api("/sessions", "POST", { title: "Workspace acceptance", provider: "mock", model: "mock", projectID: project.id });
  const secondary = await api("/sessions", "POST", { title: "Conflict source", provider: "mock", model: "mock", projectID: project.id });
  if (process.env.PUDDING_SMOKE_SCENARIO === "conversation-restore") {
    await Promise.all([seedConversation(primary.id), seedConversation(secondary.id)]);
  }
  const canvasCount = process.env.PUDDING_SMOKE_SCENARIO === "automation-presentation" ? 1 : process.env.PUDDING_SMOKE_SCENARIO === "artifact-visibility" ? 3 : 20;
  for (let i = 1; i <= canvasCount; i++) await api(`/sessions/${primary.id}/canvas/items`, "POST", {
    id: `smoke-${String(i).padStart(2, "0")}`, kind: "markdown", title: `Canvas ${String(i).padStart(2, "0")}`, item: { markdown: `# Canvas ${i}\n\n${"Persistent content.\n".repeat(100)}` },
  });
  await waitFor(() => {
    window = BrowserWindow.getAllWindows().find(w => !w.isDestroyed() && w.webContents.getURL().startsWith(process.env.PUDDING_DEV_URL));
    return Boolean(window);
  }, "source renderer");
  await focusSmokeWindow();
  // Keep frame-by-frame input checks deterministic while the user works in another window.
  window.webContents.setBackgroundThrottling(false);
  await waitFor(() => js(`Boolean(document.querySelector('#root')?.childElementCount)`), "React mount");
  window.webContents.on("console-message", (details) => {
    if (/Encountered two children|Maximum update depth|Minified React error/.test(details.message)) rendererErrors.push(details.message);
  });
  assert.equal(new URL(window.webContents.getURL()).origin, process.env.PUDDING_DEV_URL);
  await js(`(async () => {
    const workspace = await import('/src/state/workspaceStore.ts');
    workspace.setWorkspaceActiveTab(${JSON.stringify(primary.id)}, 'canvas:smoke-01');
    const { router } = await import('/src/main.tsx');
    await router.navigate({ to: '/', search: { session: ${JSON.stringify(primary.id)} } });
  })()`);
  await waitFor(() => js(`Boolean(document.querySelector('button[aria-label="打开工作区"]'))`), "closed workspace ready");
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await click('button[aria-label="打开工作区"]');
  await waitFor(async () => (await tabs()).length === canvasCount, `${canvasCount} canonical canvas tabs`);
  await waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), "workspace entrance animation settles");
  for (const label of ["项目", "资源库", "画布"]) {
    await selectSurface(label);
  }
  check(`isolated source Electron/Vite/daemon; ${canvasCount} canonical canvas tabs`);
  if (process.env.PUDDING_SMOKE_SCENARIO === "computer-preview") {
    await verifyComputerPreview(primary.id, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "artifact-visibility") {
    await verifyArtifactVisibility(primary.id, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "automation-presentation") {
    await verifyAutomationPresentation(primary.id, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "native-ime") {
    await verifyNativeIme(primary.id, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "native-chrome") {
    await verifyNativeWindowChrome(primary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "conversation-restore") {
    await verifyConversationRestore(primary.id, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "library-search") {
    await verifyLibrarySearch(primary.id, projectRoot, secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "editor-continuity") {
    await verifyEditorContinuity(primary.id, secondary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "preview-narrow") {
    await verifyPreviewNarrow(secondary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "batch-save") {
    await verifyBatchSaveFailure(primary.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "project-draft") {
    await verifyProjectDraftLayout(primary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === 'entry') {
    await verifyEmptyWorkspace(project.id);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "tree-reveal") {
    await verifyProjectTreeReveal(primary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  await verifyWorkspaceToggle();
  await swapFirstTwoTabs();
  await waitFor(async () => (await tabs())[0]?.title === "Canvas 02", "canvas horizontal drop updates order");
  await verifyTabContextMenu();
  check("canvas horizontal drop reorders existing tabs");
  await screenshot("canvas-20");
  await verifyTabDrag("canvas");

  phase = "canvas overflow";
  await selectTopTab("canvas:smoke-20");
  await waitFor(async () => (await tabs()).some(t => t.title === "Canvas 20" && t.selected), "selected overflow tab");
  assert.equal(await js(`(() => {
    const tab = (${tabNodes})().find(el => el.dataset.selected === 'true');
    const a = tab.getBoundingClientRect(), b = tab.closest('.pudding-workspace-tabs-scroll').getBoundingClientRect();
    return a.left >= b.left - 1 && a.right <= b.right + 1;
  })()`), true, "selected tab is scrolled into view");
  await click('button[aria-label="关闭标签 Canvas 20"]');
  await waitFor(async () => (await tabs()).length === 19, "close canvas tab");
  assert.equal((await api(`/sessions/${primary.id}/canvas/items`)).items.length, 20);
  check("canvas overflow scroll, reveal and close preserve canonical content");

  phase = "mixed tabs";
  await verifyMixedTabs(primary.id);

  phase = "files and focus";
  for (let i = 1; i <= 20; i++) {
    const file = `file-${String(i).padStart(2, "0")}.md`;
    await js(`(async () => {
      const { requestProjectFileReveal } = await import('/src/state/projectRevealStore.ts');
      requestProjectFileReveal({ sessionID: ${JSON.stringify(primary.id)}, rootPath: ${JSON.stringify(projectRoot)}, relativePath: ${JSON.stringify(file)} });
    })()`);
    await waitFor(async () => (await tabs()).some(t => t.title === file && t.selected), file);
    await js(`(${tabNodes})().find(el => el.dataset.selected === 'true').querySelector('.pudding-workspace-tab-select').dispatchEvent(new MouseEvent('dblclick', {bubbles:true}))`);
    if (i === 2) {
      await swapFirstTwoTabs();
      await waitFor(async () => (await tabs())[0]?.title === "file-02.md", "project horizontal drop updates order");
      assert.equal((await tabs()).find(t => t.selected)?.title, "file-02.md");
      await verifyTabContextMenu();
      check("project horizontal drop reorders files without changing selection");
    }
  }
  assert.equal((await tabs()).length, 20);
  await openTabsMenu();
  await input('input[aria-label="搜索标题、路径或网址"]', "file-03");
  await waitFor(() => js(`document.querySelectorAll('[role="listitem"]').length === 1`), "filtered file overflow");
  await click('[role="listitem"] button');
  await waitFor(async () => (await tabs()).some(t => t.title === "file-03.md" && t.selected), "file overflow selection");
  check("20 pinned files; searchable overflow selects the original file");
  await verifyTabDrag("project files");
  await screenshot("files-20");
  await js(`document.querySelector('[data-workspace-tab-key="project"]').scrollIntoView({block:'nearest',inline:'nearest'})`);
  await click('[data-workspace-tab-key="project"] .pudding-workspace-tab-close');
  await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key="project"]')`), "project tab closes");
  await selectSurface("资源库");
  await click('[data-workspace-library] button[aria-label="打开项目"]');
  await waitFor(async () => (await tabs()).length === 20, "reopening project restores its file tabs");
  assert.equal((await tabs()).find(tab => tab.selected)?.title, "file-03.md");
  check("project closes and reopens from library with all files and selection preserved");
  await waitFor(() => js(`Boolean(document.querySelector('textarea'))`), "composer");
  await input("textarea", "中文草稿 · keep this draft");
  const widthBefore = await js(`document.querySelector('.pudding-workspace-stage').getBoundingClientRect().width`);
  await click('button[aria-label="专注"]');
  await waitFor(() => js(`Boolean(document.querySelector('button[aria-label="退出专注"]'))`), "focus enabled");
  assert.equal(await js(`document.querySelector('textarea').value`), "中文草稿 · keep this draft");
  assert.ok(await js(`document.querySelector('.pudding-workspace-stage').getBoundingClientRect().width`) > widthBefore);
  await click('button[aria-label="退出专注"]');
  await waitFor(() => js(`Boolean(document.querySelector('button[aria-label="专注"]'))`), "focus restored");
  assert.equal(await js(`document.querySelector('textarea').value`), "中文草稿 · keep this draft");
  await waitFor(async () => Math.abs(await js(`document.querySelector('.pudding-workspace-stage').getBoundingClientRect().width`) - widthBefore) < 2, "restored workspace width after transition");
  check("focus expands workspace and restores width, draft and session");

  phase = "saved canvas conflict";
  const base = await api(`/sessions/${primary.id}/canvas/items/smoke-01/save`, "POST");
  const other = await api(`/sessions/${secondary.id}/canvas/saved/${base.savedItem.id}/open`, "POST");
  await api(`/sessions/${secondary.id}/canvas/items/${other.id}`, "PUT", { kind: "markdown", title: "Conflict from other session", item: { markdown: "New saved revision" } });
  const revision = await api(`/sessions/${secondary.id}/canvas/items/${other.id}/save`, "POST");
  await api(`/sessions/${primary.id}/canvas/items/smoke-01`, "PUT", { kind: "markdown", title: "Canvas 01", item: { markdown: "Unsaved local revision" } });
  // Reload fetches the real canonical fixture and also verifies that closed
  // canvas references survive a renderer restart.
  await reloadRenderer();
  await waitFor(() => js(`Boolean(document.querySelector('.pudding-workspace-topbar'))`), "renderer reload");
  await selectSurface("项目");
  await waitFor(async () => (await tabs()).length === 20, "project tabs restored after reload");
  assert.deepEqual((await tabs()).slice(0, 2).map(t => t.title), ["file-02.md", "file-01.md"]);
  check("project tab order survives app switches and renderer reload");
  await selectSurface("画布");
  await waitFor(async () => (await tabs()).length === 19, "closed canvas remains closed after reload");
  assert.deepEqual((await tabs()).slice(0, 2).map(t => t.title), ["Canvas 02", "Canvas 01"]);
  const cancelBrowser = await api(`/sessions/${primary.id}/browser/tabs`, "POST");
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="browser:${cancelBrowser.id}"]'))`), "mixed close fixture browser");
  // Wait for the new guest's title/selection updates to finish centering the strip.
  await delay(250);
  await selectSurface("画布");
  const dirtyPoint = await js(`(() => {const r=document.querySelector('[data-workspace-tab-key^="canvas:"][data-selected="true"]').getBoundingClientRect(); return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  window.webContents.sendInputEvent({type:"mouseDown",...dirtyPoint,button:"right",clickCount:1});
  window.webContents.sendInputEvent({type:"mouseUp",...dirtyPoint,button:"right",clickCount:1});
  await clickText("关闭全部标签", '[role="menuitem"]');
  await waitFor(() => js(`Boolean(document.querySelector('[role="alertdialog"]'))`), "mixed close asks before releasing browser");
  await clickText("取消", '[role="alertdialog"] button');
  await waitFor(() => js(`!document.querySelector('[data-slot="alert-dialog-overlay"]')`), "mixed close cancellation settles");
  assert.equal((await api(`/sessions/${primary.id}/browser/tabs`)).tabs.some(tab => tab.id === cancelBrowser.id), true);
  assert.equal(await js(`document.querySelectorAll('[data-workspace-tab-key]').length`), 21);
  assert.ok(webContents.getAllWebContents().some(c => c.getType() === "webview"));
  await api(`/sessions/${primary.id}/browser/tabs/${cancelBrowser.id}/release`, "POST");
  await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key="browser:${cancelBrowser.id}"]')`), "mixed close fixture cleaned up");
  check("cancel mixed close-all keeps every canvas and native browser open");
  await selectTopTab("canvas:smoke-01");
  await waitFor(async () => (await tabs()).some(t => t.title === "Canvas 01" && t.selected), "reveal dirty canvas before closing");
  await click('button[aria-label="关闭标签 Canvas 01"]');
  await waitFor(() => js(`Boolean(document.querySelector('[role="alertdialog"]'))`), "dirty close dialog");
  await clickText("保存并关闭", '[role="alertdialog"] button');
  await waitFor(() => js(`document.body.textContent.includes('保存版本已在其他对话中更新')`), "visible save conflict");
  assert.equal((await tabs()).length, 19, "failed save keeps all tabs open");
  assert.equal((await api(`/sessions/${primary.id}/canvas/saved`)).items.find(i => i.id === base.savedItem.id).revision, revision.savedItem.revision);
  assert.equal((await api(`/sessions/${primary.id}/canvas/items`)).items.find(i => i.id === "smoke-01").item.markdown, "Unsaved local revision");
  await screenshot("canvas-conflict");
  await clickText("保留修改并关闭", '[role="alertdialog"] button');
  await waitFor(async () => (await tabs()).length === 18, "close without overwriting saved revision");
  await waitFor(() => js(`!document.querySelector('[data-slot="alert-dialog-overlay"]')`), "close dialog finishes its exit animation");
  check("cross-session save conflict is visible, keeps local content and does not overwrite saved revision");

  phase = "20 native browser tabs";
  const baseline = await measureMemory("before-browser");
  const browserTabs = [];
  // Loaded local documents with DOM content, not uninitialised blank slots.
  for (let i = 0; i < 20; i++) {
    const tab = await api(`/sessions/${primary.id}/browser/tabs`, "POST");
    browserTabs.push(tab);
    await api(`/sessions/${primary.id}/browser/tabs/${tab.id}/open`, "POST", { url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?tab=${i}` });
    if ([0, 7, 19].includes(i)) await measureMemory(`loaded-${i + 1}`);
    if (i === 0) await verifyTabDrag("browser alongside canvas");
    if (i === 1) {
      await waitFor(async () => (await tabs()).length === 2, "two browser tabs for sorting");
      await swapFirstTwoTabs();
      await waitFor(() => js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(primary.id)}).tabOrder.filter(id => id.startsWith('browser:'))[0] === ${JSON.stringify(`browser:${tab.id}`)})`), "horizontal drop reorders browser tabs");
      check("horizontal drop reorders existing browser tabs");
    }
  }
  await waitFor(async () => (await tabs()).length === 20, "20 browser tabs in renderer");
  const denied = await fetch(`${apiBase}/sessions/${primary.id}/browser/tabs`, { method: "POST", headers: { Authorization: `Bearer ${token}` } });
  assert.equal(denied.status, 429);
  const idsBefore = webContents.getAllWebContents().filter(c => c.getType() === "webview").map(c => c.id).sort();
  assert.equal(idsBefore.length, 20);
  await selectTopTab(`browser:${browserTabs[19].id}`);
  await screenshot("browser-20");
  await verifyTabDrag("browser overflow");
  for (const label of ["项目", "画布", "浏览器"]) await selectSurface(label);
  assert.deepEqual(webContents.getAllWebContents().filter(c => c.getType() === "webview").map(c => c.id).sort(), idsBefore);
  check("20 loaded native browser tabs; 21st rejected; horizontal overflow and content switches preserve WebContents");

  phase = "hidden browser geometry during resize";
  window.setContentSize(1440, 920);
  await selectSurface("画布");
  await delay(300);
  const guests = webContents.getAllWebContents().filter(c => c.getType() === "webview");
  const keepAliveSize = await js(`(() => {const style=getComputedStyle(document.documentElement);return {width:parseInt(style.getPropertyValue('--browser-runtime-width')),height:parseInt(style.getPropertyValue('--browser-runtime-height'))}})()`);
  for (const guest of guests) {
    const geometry = await guest.executeJavaScript(`({width:innerWidth,height:innerHeight})`);
    assert.deepEqual(geometry, keepAliveSize);
    await guest.executeJavaScript(`window.__smokeResizeCount = 0; window.addEventListener('resize', () => window.__smokeResizeCount++)`);
  }
  const divider = await js(`(() => {const r=document.querySelector('.pudding-shell-divider').getBoundingClientRect();return {x:Math.round(r.x),y:Math.round(r.y+r.height/2)}})()`);
  window.webContents.sendInputEvent({ type: "mouseDown", ...divider, button: "left", clickCount: 1 });
  const started = Date.now();
  while (Date.now() - started < 5000) {
    window.webContents.sendInputEvent({ type: "mouseMove", x: divider.x + Math.round(Math.sin((Date.now() - started) / 400) * 120), y: divider.y, button: "left" });
    await delay(16);
  }
  window.webContents.sendInputEvent({ type: "mouseUp", ...divider, button: "left", clickCount: 1 });
  await waitFor(() => js(`!document.querySelector('.pudding-agent-console-resize-shield')`), "divider releases pointer capture");
  // Committing the drag finishes the workspace width transition; its resize
  // observer still reveals the selected tab until that transition completes.
  await delay(300);
  for (const guest of guests) assert.equal(await guest.executeJavaScript("window.__smokeResizeCount"), 0, "hidden guest resized during divider drag");
  check(`five-second divider drag keeps 20 hidden webviews at ${keepAliveSize.width}×${keepAliveSize.height} without resize events`);

  phase = "themes, locales and narrow windows";
  await selectSurface("浏览器");
  for (const locale of ["zh-CN", "zh-TW", "en"]) {
    await js(`import('/src/i18n/index.ts').then(m => m.setLocale(${JSON.stringify(locale)}))`);
    for (const theme of ["light", "dark"]) {
      await js(`window.puddingElectronTheme.setTheme(${JSON.stringify(theme)})`);
      window.setContentSize(560, 680);
      window.webContents.setZoomFactor(1.25);
      await delay(300);
      assert.equal(await js(`(() => {
        const button=document.querySelector('[data-workspace-add]');
        const pane=document.querySelector('.pudding-workspace-pane').getBoundingClientRect(), r=button.getBoundingClientRect();
        return r.width>0 && r.left>=pane.left-1 && r.right<=pane.right+1;
      })()`), true, `${locale}/${theme}: library remains reachable in narrow workspace`);
      await selectSurface("项目");
      await selectSurface("画布");
      await selectSurface("浏览器");
      await waitFor(() => js(`(() => {
        const host=document.querySelector('.pudding-browser-runtime-host[data-presentation="visible"]');
        if (!host) return false;
        const r=host.getBoundingClientRect();
        return r.width>0 && r.height>0 && host.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2));
      })()`), `${locale}/${theme}: loaded webpage is above the workspace drawer and receives pointer input`);
      assert.equal(await js(`(() => {
        const tab=(${tabNodes})().find(el => el.dataset.selected === 'true');
        const r=tab.getBoundingClientRect(), strip=tab.closest('.pudding-workspace-tabs-scroll').getBoundingClientRect();
        return r.left>=strip.left-1 && r.right<=strip.right+1;
      })()`), true, `${locale}/${theme}: selected tab stays visible after resize`);
      if (locale === "en") await screenshot(`narrow-${theme}`);
    }
  }
  window.webContents.setZoomFactor(1);
  window.setContentSize(1440, 920);
  await js(`import('/src/i18n/index.ts').then(m => m.setLocale('zh-CN'))`);
  check("three locales × two themes at 560px/125% zoom: navigation and native webpage stay reachable");

  phase = "browser release and memory";
  for (const tab of browserTabs) await api(`/sessions/${primary.id}/browser/tabs/${tab.id}/release`, "POST");
  await waitFor(() => webContents.getAllWebContents().every(c => c.getType() !== "webview"), "destroy closed webviews");
  await measureMemory("after-close-all");
  const replacement = await api(`/sessions/${primary.id}/browser/tabs`, "POST");
  await api(`/sessions/${primary.id}/browser/tabs/${replacement.id}/release`, "POST");
  assert.equal(baseline.guestCount, 0);
  check("all 20 guests destroyed on release; capacity reusable");
  phase = "resource library";
  await api(`/sessions/${primary.id}/library/favorites`, "POST", { kind: "canvas", savedItemID: base.savedItem.id });
  await api(`/sessions/${primary.id}/library/favorites`, "POST", { kind: "web", url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?library=favorite-only`, title: "Library favorite only" });
  for (let i = 1; i <= 2; i++) await api(`/sessions/${primary.id}/library/favorites`, "POST", { kind: "web", url: `${process.env.PUDDING_DEV_URL}/__workspace_smoke?extra=${i}`, title: `Extra favorite ${i}` });
  await selectSurface("资源库");
  await waitFor(() => js(`document.querySelectorAll('[data-library-shortcuts] [data-library-row]').length === 3`), "three real favorite shortcuts");
  assert.equal(await js(`Boolean(document.querySelector('button[aria-label="资源库分类"]'))`), false, "no hidden classification menu");
  assert.equal((await api(`/sessions/${primary.id}/library`)).entries.some(e => e.kind === "file"), false, "no file favorites");
  assert.equal(await js(`Array.from(document.querySelectorAll('[data-workspace-library] h2')).at(-1)?.textContent`), "最近打开");
  assert.equal(await js(`document.querySelector('[data-workspace-library]').innerText.includes(${JSON.stringify(projectRoot)})`), false, "absolute file paths stay out of compact rows");
  assert.equal(await js(`Array.from(document.querySelectorAll('[data-library-results] [data-library-row]')).every(el => el.getBoundingClientRect().height < 75)`), true, "two-line resource rows");
  for (const theme of ["light", "dark"]) {
    await js(`window.puddingElectronTheme.setTheme(${JSON.stringify(theme)})`);
    await delay(150);
    await screenshot(`library-${theme}`);
  }
  await clickText("网页", '[data-workspace-library] button');
  await waitFor(() => js(`Array.from(document.querySelectorAll('[data-library-results] [data-library-row]')).every(el => !/file-\d+\.md|Canvas/.test(el.textContent))`), "web filter excludes files and canvas");
  await input('[data-workspace-library] input', "Library favorite only");
  await waitFor(() => js(`document.querySelectorAll('[data-library-row]').length === 1 && document.querySelector('[data-library-row]').textContent.includes('Library favorite only')`), "global search includes unvisited favorites");
  await clickText("全部", '[data-workspace-library] button');
  await input('[data-workspace-library] input', "file-01.md");
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy') === 'false' && document.querySelectorAll('[data-library-row]').length === 1 && document.querySelector('[data-library-row]').textContent.includes('file-01.md')`), "file remains searchable through recent opens");
  await click('button[aria-label="更多操作 file-01.md"]');
  await clickText("查看详情", '[role="menuitem"]');
  await waitFor(() => js(`document.querySelector('[role="dialog"]')?.textContent.includes(${JSON.stringify(projectRoot)})`), "full path appears in resource details");
  await screenshot("library-details");
  window.webContents.sendInputEvent({ type: "keyDown", keyCode: "Escape" });
  window.webContents.sendInputEvent({ type: "keyUp", keyCode: "Escape" });
  await waitFor(() => js(`!document.querySelector('[data-slot="dialog-overlay"]')`), "resource details close");
  await click('[data-library-row] > button');
  await waitFor(async () => (await tabs()).some(t => t.title === "file-01.md" && t.selected), "library opens original project file");
  assert.equal(await js(`Array.from(document.querySelectorAll('button[aria-label="收藏"], button[aria-label="取消收藏"]')).some(el => el.getBoundingClientRect().width > 0)`), false, "project file has no favorite button");
  await selectSurface("资源库");
  assert.equal(await js(`document.querySelector('[data-workspace-library] input').value`), "file-01.md", "search survives opening and returning");
  await input('[data-workspace-library] input', "");
  await clickText("展开全部", '[data-workspace-library] button');
  await waitFor(() => js(`document.querySelectorAll('[data-library-shortcuts] [data-library-row]').length === 4`), "favorites expand in place");
  assert.equal(await js(`document.querySelector('[data-library-results] h2').textContent`), "最近打开", "expanding keeps recent opens visible");
  const savedEntry = (await api(`/sessions/${primary.id}/library`)).entries.find(e => e.savedItemID === base.savedItem.id);
  await clickElement(`Array.from(document.querySelectorAll('[data-library-shortcuts] button[aria-label]')).find(el => el.getAttribute('aria-label') === ${JSON.stringify(`更多操作 ${savedEntry.title}`)})`);
  await clickText("取消收藏", '[role="menuitem"]');
  await waitFor(() => js(`document.querySelectorAll('[data-library-shortcuts] [data-library-row]').length === 3`), "unfavorite refreshes permanent collection");
  const preserved = (await api(`/sessions/${primary.id}/library`)).entries.find(e => e.savedItemID === base.savedItem.id);
  assert.ok(preserved && !preserved.favoriteID && preserved.revision === savedEntry.revision, "unfavorite retains saved version");
  await input('[data-workspace-library] input', savedEntry.title);
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy') === 'false' && document.querySelector('[data-library-results]').textContent.includes('保存版本')`), "unstarred canvas remains searchable");
  await clickElement(`Array.from(document.querySelectorAll('[data-library-results] [data-library-row]')).find(el => el.textContent.includes('保存版本')).querySelector('button[aria-label]')`);
  await clickText("收藏", '[role="menuitem"]');
  await waitFor(async () => Boolean((await api(`/sessions/${primary.id}/library`)).entries.find(e => e.savedItemID === base.savedItem.id)?.favoriteID), "saved content can be favorited again");
  // History is not the inventory: closed canvas remains searchable even after
  // its visit record is removed, without a separate recently-closed view.
  await api(`/sessions/${primary.id}/library/recent?kind=canvas`, "DELETE");
  await input('[data-workspace-library] input', "Canvas 20");
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy') === 'false' && document.querySelectorAll('[data-library-row]').length === 1 && document.querySelector('[data-library-row]').textContent.includes('Canvas 20')`), "closed canvas searchable without history");
  await clickElement(`Array.from(document.querySelectorAll('[data-library-row] > button')).find(el => el.textContent.includes('Canvas 20'))`);
  await waitFor(async () => (await tabs()).some(t => t.title === "Canvas 20" && t.selected), "closed canvas restores its original item");
  assert.equal((await api(`/sessions/${primary.id}/canvas/items`)).items.filter(item => item.id === "smoke-20").length, 1);
  await selectSurface("资源库");
  await input('[data-workspace-library] input', "");
  await click('button[aria-label="清除记录"]');
  await waitFor(() => js(`Boolean(document.querySelector('[role="alertdialog"]'))`), "history clear retains confirmation");
  await clickText("取消", '[role="alertdialog"] button');
  await waitFor(() => js(`!document.querySelector('[data-slot="alert-dialog-overlay"]')`), "clear confirmation closes");
  window.setContentSize(780, 760);
  await delay(150);
  assert.equal(await js(`(() => { const el = document.querySelector('[data-workspace-library]'); return el.scrollWidth <= el.clientWidth + 1; })()`), true, "resource library fits a narrow desktop pane");
  await screenshot("library-narrow");
  window.setContentSize(1440, 920);
  check("resource library: permanent favorites, inline expansion, no file favorites, unstarred/closed search, source routing and clear confirmation");
  await verifyEmptyWorkspace(project.id);
  assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
  check("unified content and library retain distinct component identities");
}

async function finish(error) {
  if (finished) return;
  finished = true;
  clearTimeout(timeout);
  if (error) {
    console.error(`[workspace-smoke] FAIL ${phase}`, error);
    if (window && !window.isDestroyed()) {
      await screenshot("failure").catch(() => {});
      const snapshot = await js(`document.body.innerText`).catch(() => "renderer unavailable");
      fs.writeFileSync(path.join(reportDir, "failure.txt"), snapshot);
    }
  }
  fs.writeFileSync(path.join(reportDir, "result.json"), JSON.stringify({
    passed: !error, phase, checks, memory, rendererErrors, error: error?.stack,
    environment: { platform: process.platform, arch: process.arch, electron: process.versions.electron, ramGiB: Math.round(os.totalmem() / 2 ** 30) },
    fixture: process.env.PUDDING_SMOKE_SCENARIO
      ? `Workspace ${process.env.PUDDING_SMOKE_SCENARIO} regression; source Electron/Vite/daemon; disposable home`
      : "20 local HTML pages, each with 300 text rows and one input; source Vite build; 20 project files and 18 canvas views retained",
    memoryNote: "Electron app.getAppMetrics working set KiB, summed once per PID. Shared pages may be counted in multiple processes; this is not net physical memory. Total includes the embedded Vite server, guest-only excludes it.",
  }, null, 2));
  await vite?.close();
  exitCode = error ? 1 : 0;
  app.quit();
}
