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
process.env.PUDDING_DAEMON_BIN ||= path.join(repo, "bin/puddingd");
if (["conversation-restore", "conversation-resize", "conversation-markdown-resize", "native-ime", "computer-preview"].includes(process.env.PUDDING_SMOKE_SCENARIO)) {
  const wrapper = path.join(home, "mock-daemon");
  const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
  fs.writeFileSync(wrapper, `#!/bin/sh\nexec ${quote(process.env.PUDDING_DAEMON_BIN)} -mock "$@"\n`, { mode: 0o755 });
  process.env.PUDDING_DAEMON_BIN = wrapper;
}
process.env.PUDDING_LOCALE = "zh-CN";
delete process.env.PUDDING_API_BASE;
let vite, window, apiBase, token, phase = "startup", finished = false, exitCode = 0;
let computerBridgeIdentity, computerPreviewManager;
const previewReveals = [];
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
  const reveal = ComputerUsePreview.prototype.reveal;
  ComputerUsePreview.prototype.reveal = async function (owner, request) {
    const record = { phase, request, startedAt: Date.now() };
    previewReveals.push(record);
    try { record.result = await reveal.call(this, owner, request); return record.result; }
    catch (error) { record.error = {code:error.code,message:error.message}; throw error; }
    finally { record.finishedAt = Date.now(); console.info('[workspace-smoke] preview reveal', JSON.stringify(record)); }
  };
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
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/*/submit`, `${apiBase}/sessions/*/browser/*`]}, (details, callback) => {
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
    const recentSearches = () => requests.filter(r => r.method==='GET' && r.url.includes('/browser/history') && new URL(r.url).searchParams.get('q'));
    const previous = recentSearches().length;
    await pinyin();
    await waitFor(()=>js('window.__nativeComposing'),'library real composition starts');
    await delay(250);
    assert.equal(recentSearches().length, previous, 'library waits for real candidate commit');
    await nativeInput('key',36);
    await waitFor(()=>js('!window.__nativeComposing'),'library candidate Return');
    assert.ok(await js(`document.querySelector('[data-workspace-library]')?.getAttribute('aria-hidden') === 'false'`), 'candidate Return does not open a resource');
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
    await waitFor(() => js(`document.querySelector('[data-workspace-library]')?.getAttribute('aria-hidden') === 'false'`), 'add opens resource page');
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

async function verifyRailDivider() {
  phase = "rail divider stability";
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  const measure = `(() => {
    const divider = document.querySelector('.pudding-shell-divider');
    const stage = document.querySelector('.pudding-session-stage');
    const chat = document.querySelector('.pudding-agent-console');
    return { x: divider.getBoundingClientRect().x, stageX: stage.getBoundingClientRect().x, chatWidth: chat.getBoundingClientRect().width };
  })()`;
  for (const width of [1440, 1800]) {
    window.setContentSize(width, 920);
    await delay(300);
    await waitFor(() => js(`Boolean(document.querySelector('button[aria-label="收起边栏"]'))`), "expanded sidebar");
    const ratio = await js(`localStorage.getItem('pudding.agentConsoleDockSplitRatio')`);
    for (const label of ["收起边栏", "展开边栏"]) {
      const before = await js(measure);
      await js(`window.__railFrames = []; (function frame() { window.__railFrames.push(${measure}); window.__railFrameID = requestAnimationFrame(frame); })()`);
      await click(`button[aria-label="${label}"]`);
      await delay(400);
      const samples = await js(`cancelAnimationFrame(window.__railFrameID); window.__railFrames`);
      const after = await js(measure);
      const drift = Math.max(...samples.map(frame => Math.abs(frame.x - before.x)));
      fs.writeFileSync(path.join(reportDir, `rail-${width}-${label === "收起边栏" ? "collapse" : "expand"}.json`), JSON.stringify({before, after, drift, samples}, null, 2));
      console.info('[rail-divider]', JSON.stringify({width, label, before, after, drift, frames:samples.length}));
      assert.ok(samples.length > 5, "recorded sidebar transition frames");
      assert.ok(Math.abs(before.x - after.x) < 1, "sidebar toggle retains final divider position");
      assert.ok(drift < 1, "sidebar toggle must not move the divider in intermediate frames");
      assert.equal(await js(`localStorage.getItem('pudding.agentConsoleDockSplitRatio')`), ratio, "sidebar toggle preserves split preference");
    }
    check(`sidebar collapse/expand keeps the divider fixed in every frame at ${width}px`);
  }
  await verifyWorkspaceToggle();
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
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-busy') === 'false'`), "empty landing loaded");
  assert.ok(await js(`(() => { const page=document.querySelector('[data-workspace-library]'), header=page.querySelector('header').getBoundingClientRect(), area=page.getBoundingClientRect();return Math.abs(header.top - area.top + page.scrollTop - 64) < 1 && !page.querySelector('h1'); })()`), "empty start actions retain fixed top spacing without a page title");
  await screenshot("workspace-empty");
  await click('[data-workspace-library] button[aria-label="新建浏览器标签页"]');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key^="browser:"][data-selected="true"]'))`), "landing creates a browser tab");
  await click('[data-workspace-add]');
  await waitFor(() => js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden') === 'false'`), "add button opens resource landing");
  await click('[data-workspace-add]');
  assert.equal(await js(`document.querySelectorAll('[data-workspace-tab-key]').length`), 1, "repeated plus does not add a tab");
  assert.equal(await js(`Boolean(document.querySelector('[data-workspace-tab-key="library"]'))`), false, "resource page is not a tab");
  assert.equal(await js(`document.querySelector('[data-workspace-add]').getAttribute('aria-pressed')`), null, "plus is an action, not the selected page");
  window.webContents.sendInputEvent({ type: "mouseMove", x: 1, y: 1 });
  await waitFor(() => js(`getComputedStyle(document.querySelector('[data-workspace-add]')).backgroundColor === 'rgba(0, 0, 0, 0)'`), "plus has no active background after the pointer leaves");
  await selectSurface('浏览器');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key^="browser:"][data-selected="true"]'))`), "browser selection leaves resource page");
  await click('[data-workspace-add]');
  assert.equal((await api(`/sessions/${emptySession.id}/browser/tabs`)).tabs.length, 1, "plus does not create another browser");
  await click('[data-workspace-library] button[aria-label="新建浏览器标签页"]');
  await waitFor(() => js(`!document.querySelector('[data-workspace-tab-key="library"]') && document.querySelectorAll('[data-workspace-tab-key^="browser:"]').length === 2`), "resource page creates another browser");
  await click('[data-workspace-add]');
  await click('[data-workspace-library] button[aria-label="打开项目"]');
  await waitFor(() => js(`Boolean(document.querySelector('[data-workspace-tab-key="project"][data-selected="true"]'))`), "landing reopens project");
  assert.equal(await js(`Boolean(document.querySelector('[data-workspace-tab-key="library"]'))`), false, "opening project does not add a resource page tab");
  await click('[data-workspace-add]');
  await click('[data-workspace-library] button[aria-label="打开项目"]');
  assert.equal(await js(`document.querySelectorAll('[data-workspace-tab-key="project"]').length`), 1, "existing project is reused");
  check("zero-tab landing: matching background, no divider/add/popup; explicit project/browser actions; plus opens resources");
}

async function verifyProjectGitScroll() {
  phase = "project git scroll";
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  await selectSurface("项目");
  await click('[data-project-workspace] nav button[aria-label="源代码管理"]');
  const list = `document.querySelector('[data-project-git-list]')`;
  await waitFor(() => js(`${list}?.querySelectorAll('[class~="group/git-file"]').length === 100`), "100 git changes loaded");
  for (const [width, height] of [[1440, 920], [900, 640], [620, 520]]) {
    window.setContentSize(width, height);
    await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
    const before = await js(`(() => {
      const list = ${list}, rect = list.getBoundingClientRect();
      list.scrollTop = 0;
      return {height: list.clientHeight, content: list.scrollHeight, width: list.clientWidth,
        contentWidth: list.scrollWidth, overflow: getComputedStyle(list).overflowY,
        scrollbar: getComputedStyle(list).scrollbarWidth,
        navTop: document.querySelector('[data-project-workspace] nav').getBoundingClientRect().top,
        x: rect.x + rect.width / 2, y: rect.y + rect.height / 2};
    })()`);
    assert.ok(before.height > 0 && before.content > before.height, "git list has a bounded scroll viewport");
    assert.equal(before.overflow, "auto");
    assert.notEqual(before.scrollbar, "none");
    assert.equal(before.width, before.contentWidth, "long paths must not overflow horizontally");
    const zoom = window.webContents.getZoomFactor();
    const point = {x: Math.round(before.x * zoom), y: Math.round(before.y * zoom)};
    const atBottom = `${list}.scrollTop > 0 && ${list}.scrollHeight - ${list}.clientHeight - ${list}.scrollTop < 2`;
    window.webContents.sendInputEvent({type: "mouseMove", ...point});
    for (let attempt = 0; attempt < 30; attempt++) {
      window.webContents.sendInputEvent({type: "mouseWheel", ...point, deltaX: 0, deltaY: -600});
      await delay(100);
      if (await js(atBottom)) break;
    }
    assert.ok(await js(atBottom), "native wheel reaches last git change");
    const after = await js(`(() => {
      const list = ${list}, rect = list.getBoundingClientRect(), last = list.querySelectorAll('[class~="group/git-file"]');
      const row = last[last.length - 1].getBoundingClientRect();
      return {lastVisible: row.top >= rect.top && row.bottom <= rect.bottom + 1,
        navTop: document.querySelector('[data-project-workspace] nav').getBoundingClientRect().top};
    })()`);
    assert.ok(after.lastVisible, "last change is reachable");
    assert.equal(after.navTop, before.navTop, "view switch stays fixed while git scrolls");
    await screenshot(`git-scroll-${width}`);
    check(`git changes scroll to the last row at ${window.getContentSize().join("x")}, with a fixed view switch`);
  }
}

async function verifyProjectEmpty(sessionID, projectRoot) {
  phase = "project empty viewer";
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  window.setContentSize(1440, 920);
  await selectSurface("项目");
  await js(`import('/src/state/projectRevealStore.ts').then(({requestProjectFileReveal}) => requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:'file-01.md'}))`);
  await waitFor(async () => (await tabs()).some(tab => tab.title === "file-01.md" && tab.selected), "open last file fixture");
  const fileSource = `Array.from(document.querySelector('[data-project-workspace] nav').nextElementSibling.querySelectorAll('button')).find(el => el.textContent.trim() === 'file-01.md')`;
  const verifyTree = async (label) => {
    await waitFor(() => js(`Boolean(${fileSource}) && Boolean(document.querySelector('[data-project-workspace] section[aria-label="从文件开始"]'))`), "empty viewer with mounted tree");
    await screenshot(`project-empty-${label}`);
    const bounds = await js(`(() => {
      const empty = document.querySelector('[data-project-workspace] section[aria-label="从文件开始"]');
      const viewer = empty.closest('[data-panel]');
      const file = ${fileSource};
      const e = empty.getBoundingClientRect(), v = viewer.getBoundingClientRect(), f = file.getBoundingClientRect();
      return {
        empty: e.toJSON(), viewer: v.toJSON(),
        contained: e.left >= v.left - 1 && e.right <= v.right + 1 && e.top >= v.top - 1 && e.bottom <= v.bottom + 1,
        treeClickable: file.contains(document.elementFromPoint(f.x + f.width / 2, f.y + f.height / 2)),
      };
    })()`);
    console.info('[project-empty]', label, JSON.stringify(bounds));
    assert.ok(bounds.contained, `${label}: empty state must stay inside the file viewer`);
    assert.ok(bounds.treeClickable, `${label}: tree remains visible and clickable`);
    assert.equal(await js(`document.querySelector('[data-workspace-tab-key="project"]')?.dataset.selected`), "true");
  };
  for (const label of ["standard", "focused"]) {
    if (label === "focused") {
      await click('button[aria-label="专注"]');
      await waitFor(() => js(`Boolean(document.querySelector('button[aria-label="退出专注"]'))`), "focus mode");
      await waitFor(() => js(`!document.querySelector('.pudding-workspace-stage[data-transition="opening"]')`), "focus transition settled");
    }
    await click('[data-project-workspace] button[aria-label="关闭文件标签 file-01.md"]');
    await waitFor(async () => (await tabs()).length === 0, "last file closed");
    await verifyTree(label);
    await clickElement(fileSource);
    await waitFor(async () => (await tabs()).some(tab => tab.title === "file-01.md" && tab.selected), "reopen from tree");
    check(`${label}: closing the last file preserves the project tree and reopening from it works`);
  }
  await click('[data-project-workspace] button[aria-label="关闭文件标签 file-01.md"]');
  await waitFor(async () => (await tabs()).length === 0, "close before reload");
  await reloadRenderer();
  await selectSurface("项目");
  await verifyTree("reloaded");
  await clickElement(fileSource);
  await waitFor(async () => (await tabs()).some(tab => tab.title === "file-01.md" && tab.selected), "reopen after reload");
  check("zero-file project keeps its tree after reload");
}

async function verifyProjectTreeReveal(sessionID, projectRoot) {
  phase = "tree reveal";
  window.setContentSize(1440,920);
  for(const file of ['reveal/nested/target.md','file-01.md']) {
    await js(`(async()=>{const {requestProjectFileReveal}=await import('/src/state/projectRevealStore.ts');requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:${JSON.stringify(file)}});})()`);
    await waitFor(async () => (await tabs()).some(tab=>tab.title===path.basename(file)&&tab.selected), 'open '+file);
    await js(`Array.from(document.querySelectorAll('[data-project-workspace] .pudding-workspace-tab-select')).find(el=>el.textContent==='${path.basename(file)}').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);
  }
  const treeSource=`document.querySelector('[data-project-workspace] [data-project-tree]')`;
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
  await input('[data-project-search] input', "Long project document");
  await waitFor(()=>js(`Boolean(document.querySelector('[data-project-search-results]'))`),"search replaces tree");
  await revealMenu(); await assertRevealed('from-search');
  await click('[data-project-workspace] nav button[aria-label="源代码管理"]');
  await revealMenu(); await assertRevealed('from-git');
  window.setContentSize(620,760); window.webContents.setZoomFactor(1.5); await delay(400);
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
  window.setContentSize(620, 760);
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
  const click = async selector => {
    const source = `document.querySelector(${JSON.stringify(selector)})`;
    await js(`${source}.scrollIntoView({block:'center',inline:'nearest'})`);
    await clickElement(source);
  };
  const clickText = async (text, selector) => {
    const source = `Array.from(document.querySelectorAll(${JSON.stringify(selector)})).find(el=>el.getClientRects().length&&el.textContent.trim()===${JSON.stringify(text)})`;
    await js(`${source}.scrollIntoView({block:'center',inline:'nearest'})`);
    await clickElement(source);
  };
  phase = 'resource library';
  window.setContentSize(1440,920);
  // Canonical fixtures are isolated from the user's database. Seed dates to
  // exercise real history grouping without visiting external websites.
  const seedHistory = async (count=25) => runFile('python3',['-c',`import sqlite3,sys,time
db=sqlite3.connect(sys.argv[1]);now=int(time.time()*1000)
for i in range(int(sys.argv[3])):
 db.execute("INSERT OR REPLACE INTO browser_history(id,url,title,favicon_url,visited_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",('library-'+str(i),sys.argv[2]+'/__workspace_smoke?history='+str(i),'Visit '+str(i),'',now-i*86400000,now,now))
db.commit();db.close()`,path.join(home,'data/pudding.db'),process.env.PUDDING_DEV_URL,String(count)]);
  await seedHistory();
  const bookmarkURL = `${process.env.PUDDING_DEV_URL}/__workspace_smoke?bookmark=global`;
  await api(`/sessions/${otherSessionID}/library/favorites`,'POST',{kind:'web',url:bookmarkURL,title:'Global bookmark'});
  const savedCanvas = await api(`/sessions/${sessionID}/canvas/items/smoke-01/save`,'POST');
  const requests=[];
  window.webContents.session.webRequest.onBeforeRequest({urls:[`${apiBase}/sessions/*/library/recent*`,`${apiBase}/sessions/*/browser/history*`]},(details,callback)=>{requests.push(details.url);callback({});});
  const refresh=()=>js(`import('/src/main.tsx').then(({router})=>Promise.all([router.options.context.queryClient.invalidateQueries({queryKey:['library']}),router.options.context.queryClient.invalidateQueries({predicate:q=>q.queryKey[0]==='session'&&q.queryKey[2]==='canvas'})]))`);
  const navigate=async id=>{
    await js(`import('/src/main.tsx').then(({router})=>router.navigate({to:'/',search:{session:${JSON.stringify(id)}}}))`);
    await waitFor(()=>js(`new URLSearchParams(location.search).get('session')===${JSON.stringify(id)}`),'session navigation');
    await waitFor(()=>js(`Boolean(document.querySelector('.pudding-conversation[data-session-id="${id}"]'))`),'selected conversation mounted');
    if(await js(`Boolean(document.querySelector('button[aria-label="打开工作区"]'))`))await click('button[aria-label="打开工作区"]');
    await waitFor(()=>js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el=>['opening','closing'].includes(el.dataset.transition))`),'workspace transition settled');
    await selectSurface('资源库');
  };
  await selectSurface('资源库');
  await refresh();
  const search='[data-workspace-library] input';
  const settled=()=>js(`document.querySelector('[data-workspace-library]')?.getAttribute('aria-busy')==='false'`);
  const rows=region=>js(`Array.from(document.querySelectorAll('${region} [data-library-open]')).map(el=>el.textContent)`);
  const key=async keyCode=>{
    await focusSmokeWindow();
    window.webContents.sendInputEvent({type:'keyDown',keyCode});
    if(keyCode==='Return')window.webContents.sendInputEvent({type:'char',keyCode:'\r'});
    window.webContents.sendInputEvent({type:'keyUp',keyCode});await delay(50);
  };
  const searchFor=async value=>{await input(search,value);await waitFor(settled,'settled search');};
  await waitFor(settled,'library loaded');
  assert.equal((await rows('[data-library-canvases]')).length,6,'six canvas cards initially');
  await waitFor(()=>js(`Array.from(document.querySelectorAll('[data-library-shortcuts] [data-library-row]')).filter(el=>el.textContent.includes('Global bookmark')).every(el=>{const img=el.querySelector('img');return img?.complete&&img.naturalWidth>0&&img.src.startsWith('data:image/');})`),'URL-only bookmark favicon resolves through native browser cache');
  assert.equal(await js(`document.querySelector('[data-library-browser]').innerText.includes('全局共享')`),false,'no global badge in recent visits');
  assert.ok(await js(`(() => {const header=document.querySelector('[data-workspace-library] header');return !header.querySelector('h1') && Array.from(header.querySelectorAll('button[aria-label]')).slice(0,2).every(el=>el.getBoundingClientRect().height===42) && header.querySelector('input').getBoundingClientRect().height===44;})()`),'compact start actions above search without a page title');
  assert.ok(await js(`(()=>{const rects=Array.from(document.querySelectorAll('[data-library-shortcuts] [data-library-row]')).map(el=>el.getBoundingClientRect());return Math.abs(rects[0].width-rects[1].width)<1})()`),'favorite cards share a column width');
  check('bookmark favicon loads from page URL; compact aligned header and favorites; no global badge');
  for(const section of ['[data-library-shortcuts]','[data-library-canvases]','[data-library-browser]']){
    const row=`${section} [data-library-row]`;
    const trigger=`${row} [aria-haspopup="menu"]`;
    const readState=()=>js(`(()=>{const row=document.querySelector(${JSON.stringify(row)}), trigger=row.querySelector('[aria-haspopup="menu"]');return {hover:row.matches(':hover'),focusWithin:row.matches(':focus-within'),focusVisible:Boolean(row.querySelector(':focus-visible')),background:getComputedStyle(row).backgroundColor,opacity:getComputedStyle(trigger).opacity,time:row.querySelector('time')?getComputedStyle(row.querySelector('time')).visibility:null};})()`);
    await js(`document.querySelector(${JSON.stringify(row)}).scrollIntoView({block:'center'});document.querySelector('${search}').focus({preventScroll:true})`);
    window.webContents.sendInputEvent({type:'mouseMove',x:1,y:1});
    await js(`new Promise(resolve=>requestAnimationFrame(resolve))`);
    const idle=await readState();
    await click(trigger);
    await waitFor(()=>js(`Boolean(document.querySelector('[role="menu"]'))`),'resource menu opens');
    window.webContents.sendInputEvent({type:'mouseMove',x:1,y:1});
    await js(`new Promise(resolve=>requestAnimationFrame(resolve))`);
    assert.equal((await readState()).opacity,'1','open menu keeps resource actions visible');
    const outside=await js(`(()=>{const r=document.querySelector('[data-workspace-library]').getBoundingClientRect();return {x:r.left+12,y:r.top+12};})()`);
    const zoom=window.webContents.getZoomFactor();
    const point={x:Math.round(outside.x*zoom),y:Math.round(outside.y*zoom)};
    window.webContents.sendInputEvent({type:'mouseMove',...point});
    window.webContents.sendInputEvent({type:'mouseDown',...point,button:'left',clickCount:1});
    window.webContents.sendInputEvent({type:'mouseUp',...point,button:'left',clickCount:1});
    await waitFor(()=>js(`!document.querySelector('[role="menu"]') && document.activeElement===document.querySelector(${JSON.stringify(trigger)})`),'pointer dismissal restores trigger focus');
    const dismissed=await readState();
    assert.equal(dismissed.hover,false,'pointer is outside the resource row');
    assert.equal(dismissed.focusVisible,false,'pointer-restored focus is not keyboard focus');
    assert.equal(dismissed.opacity,'0',`${section}: mouse dismissal must hide actions; ${JSON.stringify(dismissed)}`);
    assert.equal(dismissed.background,idle.background,'pointer dismissal restores the idle background');
    if(dismissed.time)assert.equal(dismissed.time,'visible','history time returns after pointer dismissal');
    await key('Return');await waitFor(()=>js(`Boolean(document.querySelector('[role="menu"]'))`),'keyboard opens resource menu');
    await key('Escape');await waitFor(()=>js(`!document.querySelector('[role="menu"]')`),'keyboard closes resource menu');
    const keyboard=await readState();
    assert.equal(keyboard.focusVisible,true,'keyboard dismissal retains visible focus');
    assert.equal(keyboard.opacity,'1','keyboard focus keeps resource actions discoverable');
  }
  check('resource menus clear pointer highlight on dismissal while preserving keyboard focus and open-menu visibility');
  assert.ok((await rows('[data-library-canvases]'))[0].includes('Canvas 01'),'favorite first');
  assert.equal((await rows('[data-library-browser]')).filter(x=>x.includes('Visit')).length,5,'five recent global visits');
  assert.ok((await rows('[data-library-shortcuts]')).includes('Global bookmark'),'bookmark from another conversation');
  assert.equal(await js(`document.querySelector('${search}').placeholder`),'搜索画布和网页');
  await clickText('查看更多画布 · 20','[data-library-canvases] button');
  assert.equal((await rows('[data-library-canvases]')).length,12,'inline canvas expansion');
  await clickText('历史记录','[data-library-browser] button');
  await waitFor(settled,'full history loaded');
  assert.equal((await rows('[data-library-browser]')).filter(x=>x.includes('Visit')).length,20);
  assert.ok(await js(`document.querySelectorAll('[data-library-browser] h3').length>=20`),'history grouped by date');
  await clickText('查看更多记录','[data-library-browser] button');
  assert.equal((await rows('[data-library-browser]')).filter(x=>x.includes('Visit')).length,25);
  await clickText('返回最近访问','[data-library-browser] button');
  await waitFor(settled,'recent view');
  assert.deepEqual(await js(`Array.from(document.querySelectorAll('[data-workspace-library] h2')).map(el=>el.textContent)`),['收藏','画布','最近访问']);
  assert.ok((await rows('[data-library-shortcuts]')).some(x=>x.includes('Canvas 01')),'global collection includes saved canvases');
  check('global canvas and web favorites; six current canvases; five recent visits; dated history expands 20 to 25');

  await searchFor('Canvas 20');
  assert.equal((await rows('[data-library-canvases]')).length,1,'all inventory is searchable beyond initial six');
  await key('Down');await key('Return');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:smoke-20"][data-selected="true"]'))`),'keyboard opens canvas');
  await click('button[aria-label="关闭标签 Canvas 20"]');
  await selectSurface('资源库');await waitFor(settled,'closed item discoverable');
  assert.equal((await rows('[data-library-canvases]')).length,1);
  await click('[data-library-canvases] [data-library-open]');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:smoke-20"][data-selected="true"]'))`),'closed canvas reopens');
  assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.filter(x=>x.id==='smoke-20').length,1,'no copy created');
  await selectSurface('资源库');
  await searchFor('file-01.md');
  assert.equal((await rows('[data-library-results]')).length,0,'no recent file results');
  await js(`import('/src/state/projectRevealStore.ts').then(m=>m.requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:'file-01.md'}))`);
  await waitFor(async()=>(await tabs()).some(t=>t.title==='file-01.md'&&t.selected),'project file opens normally');
  assert.ok(!await js(`document.querySelector('[data-project-workspace]').innerText.includes('最近打开')`),'project has no recent files feature');
  await selectSurface('资源库');await waitFor(settled,'return from project');
  assert.equal((await rows('[data-library-results]')).length,0);
  assert.equal(requests.filter(url=>url.includes('/library/recent')).length,0,'opening files writes no recent records');
  check('closed canvases reopen with original identity; files remain in project without recent-file reads or writes');

  await searchFor('Canvas 01');
  await js(`document.querySelector('${search}').dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}))`);
  const before=requests.length;
  await input(search,'画');await delay(300);
  await js(`document.querySelector('${search}').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,isComposing:true}))`);
  assert.equal(requests.length,before,'composition has no incomplete search request');
  assert.equal(await js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden')`),'false');
  await js(`document.querySelector('${search}').dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'画'}))`);
  await waitFor(settled,'composition committed');
  await searchFor('Canvas 02');
  await input(search,'no-result');
  await key('Return');
  assert.equal(await js(`document.querySelector('[data-workspace-library]').getAttribute('aria-hidden')`),'false','pending Enter cannot open stale result');
  await waitFor(settled,'pending query settled');
  await click('button[aria-label="清除搜索"]');await waitFor(settled,'clear');
  assert.ok(await js(`document.activeElement===document.querySelector('${search}')`));
  check('search handles IME composition, keyboard open and stale Enter protection');

  for(const [id,title] of [[sessionID,'Shared current'],[otherSessionID,'Shared other']])await api(`/sessions/${id}/canvas/items`,'POST',{id:'shared-library-id',kind:'markdown',title,item:{markdown:'# Shared'}});
  await refresh();await searchFor('Shared');
  assert.deepEqual((await rows('[data-library-canvases]')).map(x=>x.includes('Shared current')),[true],'default current scope');
  assert.equal(await js(`document.querySelector('[data-library-canvases]').innerText.includes('全部对话')||document.querySelector('[data-library-canvases]').innerText.includes('当前对话')`),false,'no redundant conversation scope labels');
  await navigate(otherSessionID);await searchFor('Shared');
  assert.equal((await rows('[data-library-canvases]')).length,1,'switching conversation changes canvas inventory');
  assert.ok((await rows('[data-library-canvases]'))[0].includes('Shared other'));
  await click('[data-library-canvases] [data-library-open]');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:shared-library-id"][data-selected="true"]'))`),'canvas opens in its own conversation');
  await selectSurface('资源库');await searchFor('');
  assert.ok((await rows('[data-library-shortcuts]')).some(x=>x.includes('Global bookmark')),'global bookmark survives session switch');
  await searchFor('Canvas 01');
  await click('[data-library-shortcuts] [data-library-open]');
  await waitFor(async()=>(await api(`/sessions/${otherSessionID}/canvas/items`)).items.some(x=>x.sourceSavedItemID===savedCanvas.savedItem.id),'global favorite opens a working copy in current conversation');
  assert.equal(await js(`new URLSearchParams(location.search).get('session')`),otherSessionID,'favorite does not jump to source conversation');
  const openedCopy=(await api(`/sessions/${otherSessionID}/canvas/items`)).items.find(x=>x.sourceSavedItemID===savedCanvas.savedItem.id);
  await selectSurface('资源库');await searchFor('Canvas 01');await click('[data-library-shortcuts] [data-library-open]');
  assert.equal((await api(`/sessions/${otherSessionID}/canvas/items`)).items.filter(x=>x.sourceSavedItemID===savedCanvas.savedItem.id).length,1,'reopening favorite reuses current copy');
  await api(`/sessions/${otherSessionID}/canvas/items`,'POST',{id:openedCopy.id,kind:openedCopy.kind,title:'支付状态码表',item:openedCopy.item,window:openedCopy.window});
  await refresh();await selectSurface('资源库');await searchFor('支付');
  assert.equal((await rows('[data-library-canvases]')).length,1,'renamed working copy remains searchable while its saved version is favorited');
  assert.equal((await rows('[data-library-shortcuts]')).length,0,'unmatched saved title is absent from favorites search');
  await click('[data-library-canvases] [data-library-open]');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-workspace-tab-key="canvas:${openedCopy.id}"][data-selected="true"]'))`),'search opens the renamed working copy');
  await selectSurface('资源库');await searchFor('Canvas 01');
  assert.equal((await rows('[data-library-results]')).length,1,'search by saved title keeps one result for the same opening target');
  await click('[data-library-shortcuts] button[aria-label="更多操作 Canvas 01"]');await clickText('取消收藏','[role="menuitem"]');
  await waitFor(async()=>!(await api(`/sessions/${otherSessionID}/library`)).entries.find(x=>x.savedItemID===savedCanvas.savedItem.id)?.favoriteID,'unfavorite updates global collection');
  await waitFor(settled,'unfavorite rendered');
  assert.ok((await api(`/sessions/${otherSessionID}/canvas/items`)).items.some(x=>x.id===openedCopy.id),'unfavorite retains current working copy');
  assert.ok((await api(`/sessions/${sessionID}/library`)).entries.some(x=>x.savedItemID===savedCanvas.savedItem.id),'unfavorite retains global saved content');
  assert.equal((await rows('[data-library-canvases]')).length,1,'saved title remains searchable after unfavorite even when its working copy has a different title');
  await click('[data-library-canvases] button[aria-label="更多操作 Canvas 01"]');await clickText('收藏','[role="menuitem"]');await waitFor(settled,'refavorite rendered');
  await searchFor('支付');
  assert.equal((await rows('[data-library-canvases]')).length,1,'refavoriting does not hide the renamed working copy');
  const renamedCopies=(await api(`/sessions/${otherSessionID}/canvas/items`)).items.filter(x=>x.sourceSavedItemID===savedCanvas.savedItem.id);
  assert.equal(renamedCopies.length,1,'search never creates another working copy');
  assert.equal(renamedCopies[0].title,'支付状态码表');
  assert.equal(renamedCopies[0].savedDirty,true,'search preserves unsaved changes');
  assert.equal((await api(`/sessions/${otherSessionID}/library`)).entries.find(x=>x.savedItemID===savedCanvas.savedItem.id).title,'Canvas 01','search does not overwrite the saved title');
  check('renamed working copies and unfavorited saved titles remain searchable without losing edits or creating copies');
  check('canvas favorites work globally, open in current conversation, reuse existing copy; unfavorite preserves both versions');
  await navigate(sessionID);await searchFor('Shared current');
  await click('button[aria-label="更多操作 Shared current"]');await clickText('删除画布项','[role="menuitem"]');
  await clickText('删除','[role="alertdialog"] button');
  await waitFor(()=>js(`!document.querySelector('[role="alertdialog"]')`),'canvas deletion confirmed');
  await waitFor(settled,'inventory refreshed');
  assert.equal((await rows('[data-library-canvases]')).length,0,'deleted working item disappears immediately');
  assert.ok((await api(`/sessions/${otherSessionID}/canvas/items`)).items.some(x=>x.id==='shared-library-id'),'same-ID other canvas intact');
  check('canvas inventory follows conversation; no scope switcher; identical IDs and deletion remain isolated');

  const origin=await api('/sessions','POST',{title:'Temporary saved source',provider:'mock',model:'mock'});
  await api(`/sessions/${origin.id}/canvas/items`,'POST',{id:'orphan',kind:'markdown',title:'Preserved saved canvas',item:{markdown:'# Keep this content'}});
  const orphan=await api(`/sessions/${origin.id}/canvas/items/orphan/save`,'POST');
  await api(`/sessions/${origin.id}`,'DELETE');
  await refresh();await searchFor('Preserved saved canvas');
  assert.equal((await rows('[data-library-shortcuts]')).length,1,'favorite survives source conversation deletion');
  await click('[data-library-shortcuts] button[aria-label="更多操作 Preserved saved canvas"]');await clickText('取消收藏','[role="menuitem"]');
  await waitFor(async()=>!(await api(`/sessions/${sessionID}/library`)).entries.find(x=>x.savedItemID===orphan.savedItem.id)?.favoriteID,'orphan unfavorited');
  await waitFor(()=>js(`document.querySelector('[data-library-canvases]').innerText.includes('保存版本')`),'unfavorited saved version remains searchable');
  await click('[data-library-canvases] [data-library-open]');
  await waitFor(async()=>(await api(`/sessions/${sessionID}/canvas/items`)).items.some(x=>x.sourceSavedItemID===orphan.savedItem.id),'saved version opens after source deletion');
  await selectSurface('资源库');
  check('saved content survives source deletion and remains searchable after unfavoriting');
  await searchFor('Global bookmark');await click('[data-library-shortcuts] [data-library-open]');
  await waitFor(async()=>(await api(`/sessions/${sessionID}/browser/tabs`)).tabs.some(t=>t.url===bookmarkURL),'global bookmark opens in current workspace');
  await selectSurface('资源库');await searchFor('');
  if(await js(`Boolean(Array.from(document.querySelectorAll('[data-library-browser] button')).find(el=>el.textContent==='历史记录'))`))await clickText('历史记录','[data-library-browser] button');
  await waitFor(settled,'history view');
  const canvasBefore=(await api(`/sessions/${sessionID}/canvas/items`)).items.length;
  const bookmarksBefore=(await api(`/sessions/${sessionID}/library`)).entries.length;
  await click('button[aria-label="清空历史记录"]');await clickText('清空历史记录','[role="alertdialog"] button');
  await waitFor(()=>js(`!document.querySelector('[role="alertdialog"]')`),'clear history confirmed');
  await waitFor(settled,'cleared history');
  assert.equal((await api(`/sessions/${otherSessionID}/browser/history`)).history.length,0,'history clears globally');
  assert.equal((await api(`/sessions/${sessionID}/canvas/items`)).items.length,canvasBefore);
  assert.equal((await api(`/sessions/${sessionID}/library`)).entries.length,bookmarksBefore);
  check('bookmarks open in current workspace; clearing global history preserves all canvases and favorites');
  await clickText('返回最近访问','[data-library-browser] button');await waitFor(settled,'back to recent');
  for(const theme of ['light','dark']){await js(`window.puddingElectronTheme.setTheme(${JSON.stringify(theme)})`);await delay(120);await screenshot(`library-${theme}`);}
  window.setContentSize(780,760);await delay(200);
  assert.equal(await js(`(()=>{const el=document.querySelector('[data-workspace-library]');return el.scrollWidth<=el.clientWidth+1})()`),true,'narrow library no horizontal overflow');
  await screenshot('library-narrow');
  window.setContentSize(1440,920);
  window.webContents.session.webRequest.onBeforeRequest(null);
  check('library light/dark and narrow-window layout captured');
  const empty=await api('/sessions','POST',{title:'Library without canvases',provider:'mock',model:'mock',projectID:(await api(`/sessions/${sessionID}`)).projectID});
  await js(`import('/src/main.tsx').then(({router})=>router.options.context.queryClient.invalidateQueries({queryKey:['sessions']}))`);
  await navigate(empty.id);await searchFor('');
  await waitFor(settled,'empty canvas library');
  assert.equal(await js(`Boolean(document.querySelector('[data-library-canvases]'))`),false,'empty canvas section is absent');
  assert.deepEqual(await js(`Array.from(document.querySelectorAll('[data-workspace-library] h2')).map(el=>el.textContent)`),['收藏','最近访问']);
  await waitFor(()=>js(`Array.from(document.querySelectorAll('[data-library-shortcuts] img')).some(img=>img.complete&&img.naturalWidth>0&&img.src.startsWith('data:image/'))`),'bookmark favicon survives clearing history and switching session');
  await seedHistory(5);
  await js(`import('/src/main.tsx').then(({router})=>router.options.context.queryClient.invalidateQueries({queryKey:['browser','history']}))`);
  await waitFor(settled,'history fixture refreshed');
  for(const theme of ['light','dark']){await js(`window.puddingElectronTheme.setTheme(${JSON.stringify(theme)})`);await delay(120);await screenshot(`library-empty-${theme}`);}
  window.setContentSize(780,760);await delay(200);
  assert.ok(await js(`(()=>{const el=document.querySelector('[data-workspace-library]');return el.scrollWidth<=el.clientWidth+1})()`),'empty layout fits narrow window');
  await screenshot('library-empty-narrow');
  check('empty canvas section hidden; favicon survives history clear; empty layout verified in light/dark and narrow window');
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
async function verifyConversationResize(sessionID) {
  phase = "conversation resize anchors";
  window.setContentSize(1440, 920);
  await focusSmokeWindow();
  await js(`window.__resizeErrors = []; window.addEventListener('error', event => window.__resizeErrors.push(event.message));`);
  const viewport = `document.querySelector('.pudding-conversation[data-session-id="${sessionID}"] [data-transcript-viewport]')`;
  const measure = `(() => {
    const v = ${viewport}, list = v.querySelector('[role="list"]');
    const grid = list?.firstElementChild, tail = grid?.lastElementChild;
    return {width:v.clientWidth, top:v.scrollTop, gap:v.scrollHeight-v.clientHeight-v.scrollTop,
      tailGap:tail ? v.getBoundingClientRect().bottom-tail.getBoundingClientRect().bottom : null,
      readingOffset: window.__readingTurnID ? v.querySelector('[data-transcript-turn-id="'+window.__readingTurnID+'"]')?.getBoundingClientRect().top-v.getBoundingClientRect().top : null,
      listHeight:list?.getBoundingClientRect().height, gridHeight:grid?.getBoundingClientRect().height};
  })()`;
  await waitFor(() => js(`Boolean(${viewport}?.querySelector('[data-transcript-turn-id]'))`), "long conversation rendered");
  if (process.env.PUDDING_SMOKE_SCENARIO === "conversation-markdown-resize") {
    assert.ok(await js(`${viewport}.querySelectorAll('.pudding-markdown table').length > 0`), "real Markdown tables rendered");
    assert.ok(await js(`${viewport}.querySelectorAll('.pudding-markdown ul ul').length > 0`), "nested Markdown lists rendered");
  }
  await delay(500);
  const bottomButton = `.pudding-conversation[data-session-id="${sessionID}"] .pudding-conversation-bottom-dock button`;
  if (await js(`Boolean(document.querySelector('${bottomButton}'))`)) await click(bottomButton);
  await waitFor(() => js(`${measure}.gap < 2`), "conversation initially pinned");
  const record = async (name, action) => {
    const before = await js(measure);
    await js(`window.__resizeFrames = []; (function frame() {
      window.__resizeTimer = setTimeout(() => window.__resizeFrames.push(${measure}), 0);
      window.__resizeFrame = requestAnimationFrame(frame);
    })()`);
    await action();
    await delay(400);
    const samples = await js(`cancelAnimationFrame(window.__resizeFrame); clearTimeout(window.__resizeTimer); window.__resizeFrames`);
    const gap = Math.max(...samples.map(row => Math.abs(row.gap)));
    const tailDrift = Math.max(...samples.map(row => Math.abs(row.tailGap)));
    fs.writeFileSync(path.join(reportDir, `${name}.json`), JSON.stringify({before, gap, tailDrift, samples}, null, 2));
    console.info("[conversation-resize]", JSON.stringify({name, gap, tailDrift, frames:samples.length}));
    assert.ok(samples.length > 10, "captured resize frames");
    assert.ok(Math.max(...samples.map(row => row.width))-Math.min(...samples.map(row => row.width)) > 20, `${name}: transcript actually changes width`);
    if (before.readingOffset !== null) {
      const drift = Math.max(...samples.map(row => Math.abs(row.readingOffset-before.readingOffset)));
      console.info("[conversation-resize] READING", JSON.stringify({name, drift}));
      assert.ok(drift < 2, `${name}: reading turn stays anchored in every painted frame`);
      assert.ok(samples.every(row => row.gap > 100), "history resize never pulls the reader to latest");
    } else {
      assert.ok(gap < 2 && tailDrift < 2, `${name}: latest content stays pinned in every painted frame`);
    }
    check(name);
  };
  const dragDivider = async (distance, steps, interval) => {
    const point = await js(`(() => {const r=document.querySelector('.pudding-shell-divider').getBoundingClientRect();return {x:Math.round(r.x),y:Math.round(r.y+r.height/2)}})()`);
    window.webContents.sendInputEvent({type:"mouseMove", ...point});
    window.webContents.sendInputEvent({type:"mouseDown", ...point, button:"left", clickCount:1});
    for (let step = 1; step <= steps; step++) {
      window.webContents.sendInputEvent({type:"mouseMove", x:point.x+Math.round(Math.sin(step / steps * Math.PI * 2)*distance), y:point.y, button:"left"});
      await delay(interval);
    }
    window.webContents.sendInputEvent({type:"mouseUp", ...point, button:"left", clickCount:1});
  };
  await record("divider-resize", () => dragDivider(140, 80, 20));
  const point = await js(`(() => { const r=${viewport}.getBoundingClientRect(); return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+100)}; })()`);
  window.webContents.sendInputEvent({type:"mouseMove", ...point});
  window.webContents.sendInputEvent({type:"mouseWheel", ...point, deltaX:0, deltaY:1600});
  await delay(600);
  await js(`(() => { const v=${viewport}, top=v.getBoundingClientRect().top;
    window.__readingTurnID=Array.from(v.querySelectorAll('[data-transcript-turn-id]')).find(el => el.getBoundingClientRect().bottom > top+1).dataset.transcriptTurnId;
  })()`);
  await record("history-divider-resize", () => dragDivider(140, 80, 20));
  await js(`delete window.__readingTurnID`);
  await click(bottomButton);
  await delay(400);
  if (process.env.PUDDING_SMOKE_SCENARIO === "conversation-markdown-resize") {
    for (const width of [1100, 1800, 2300]) {
      window.setContentSize(width, 920);
      await delay(400);
      await record(`markdown-divider-${width}`, () => dragDivider(420, 320, 4));
    }
    window.setContentSize(1440, 920);
    await delay(400);
  }
  for (const label of ["收起边栏", "展开边栏", "专注", "退出专注", "收起工作区", "打开工作区"]) {
    await record(`resize-${label}`, () => click(`button[aria-label="${label}"]`));
  }
  await input(`.pudding-conversation[data-session-id="${sessionID}"] textarea`, "改变宽度时，输入框换行也不能让消息跳动。".repeat(6));
  await delay(400);
  await record("window-resize-with-draft", async () => {
    for (const width of [1560, 1680, 1560, 1440, 1320, 1440]) {
      window.setContentSize(width, 920);
      await delay(120);
    }
  });
  assert.deepEqual(await js(`window.__resizeErrors`), [], "no resize observer loops or window errors");
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
  window.setContentSize(620, 760);
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
  let stacked;
  const fixtureBundleID = 'com.teatak.pudding.computer-use-fixture';
  const fixtureApp = path.join(repo, 'bin/Pudding Computer Use Fixture.app');
  const fixture = async () => {
    const child = spawn(path.join(fixtureApp, 'Contents/MacOS/PuddingComputerUseFixture'), [], {stdio:'ignore'});
    children.push(child);
    await waitFor(async () => {
      assert.ok(!exited(child), `fixture exited: ${child.exitCode}/${child.signalCode}`);
      const apps = (await native.listApps()).apps;
      const own = apps.find(a => a.instances.some(i => i.pid === child.pid));
      if (own) assert.equal(own.bundleID, fixtureBundleID, 'fixture reports its own bundle ID');
      return own?.bundleID === fixtureBundleID;
    }, 'native fixture starts');
    const used = await native.useApp({bundleID:fixtureBundleID,appPath:fixtureApp,pid:child.pid,foreground:false});
    const target = used.windows.find(w => w.title === 'Computer Use Fixture'); assert.ok(target); return {...target,appID:fixtureBundleID};
  };
  const exited = child => child.exitCode !== null || child.signalCode !== null;
  const entry = (appID = fixtureBundleID) => computerPreviewManager.entries.get(JSON.stringify([sessionID, appID]));
  const image = () => js(`document.querySelector('[data-computer-preview] img[draggable="false"]')?.src`);
  const startTurn = async (id, key, words = 2000) => {
    const result = await api(`/sessions/${id}/submit`, 'POST', {clientMessageID:key,parts:[{type:'text',text:'preview '.repeat(words)}]});
    await waitFor(() => js(`import('/src/state/overlayStore.ts').then(m => m.useOverlayStore.getState().runningTurns[${JSON.stringify(id)}] === ${JSON.stringify(result.turnID)})`), 'real SSE turn begins');
    await waitFor(() => [...computerPreviewManager.clients.values()].some(c => c.sessionID === id && c.turnID === result.turnID), 'preview turn subscription');
    return result.turnID;
  };
  const operation = async (target, turnID, id = sessionID, expectedError) => {
    const response = await fetch(`${computerBridgeIdentity.url}/computer/observe`, {
      method:'POST',headers:{authorization:`Bearer ${computerBridgeIdentity.token}`,'content-type':'application/json','x-pudding-turn-id':turnID},
      body:JSON.stringify({sessionID:id,appID:target.appID,windowID:target.windowID,maxElements:30}),
    });
    const result = await response.json();
    if (expectedError) {
      assert.equal(response.ok, false);
      assert.equal(result.code, expectedError);
    } else assert.ok(response.ok, JSON.stringify(result));
  };
  const live = target => waitFor(async () => entry(target.appID)?.target.pid === target.pid
    && Boolean(await js(`document.querySelector('[data-computer-preview][data-app-id="${target.appID}"] img')?.src`)), 'real live preview frame');
  const settle = () => waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), 'workspace transition settles');
  const chatGeometry = () => js(`Array.from(document.querySelectorAll('.pudding-conversation .pudding-chat-column')).map(el => {
    const r = el.getBoundingClientRect(); return {left:r.left,width:r.width};
  })`);
  let expectedReveals = 0;
  const clickPreview = async target => {
    assert.equal(previewReveals.length, expectedReveals, 'no unsolicited preview reveal');
    phase = `preview reveal ${target.appID} PID ${target.pid}`;
    await click(`[data-computer-preview][data-app-id="${target.appID}"]`);
    const index = expectedReveals++;
    // Activation happens before AXRaise/readiness completes. Checking active PID
    // alone allowed a failed reveal to pass and race the next test's focus change.
    await waitFor(() => previewReveals[index]?.finishedAt, 'native reveal settles');
    const record = previewReveals[index];
    assert.equal(record.request.windowID, target.windowID);
    assert.equal(record.error, undefined, JSON.stringify(record));
    assert.equal(record.result, true, 'native window readiness confirmed');
    const apps = (await native.listApps()).apps;
    assert.ok(apps.find(a => a.bundleID === target.appID)?.instances.some(i => i.pid === target.pid && i.active), 'exact preview instance is foreground');
    phase = 'native computer preview';
  };
  try {
    const permissions = await native.permissions(); assert.ok(permissions.accessibility && permissions.screenRecording);
    // A separate, disposable bundle exercises real two-App capture and stacking.
    const stackedApp = path.join(home, 'Stack Fixture.app');
    const stackedID = 'com.teatak.pudding.preview-stack-fixture';
    fs.cpSync(fixtureApp, stackedApp, {recursive:true});
    await runFile('/usr/libexec/PlistBuddy', ['-c', `Set :CFBundleIdentifier ${stackedID}`, path.join(stackedApp, 'Contents/Info.plist')]);
    await runFile('xcrun', ['clang', '-fobjc-arc', '-framework', 'AppKit', path.join(__dirname, 'macos-preview-fixture.m'), '-o', path.join(stackedApp, 'Contents/MacOS/PuddingComputerUseFixture')]);
    await runFile('/usr/bin/codesign', ['--force', '--sign', '-', stackedApp]);
    // Launch this new bundle through LaunchServices, as a normal new App.
    const stackedUsed = await native.useApp({bundleID:stackedID,appPath:stackedApp,foreground:false});
    stacked = {...stackedUsed.windows.find(w => w.title === 'Computer Use Fixture'),appID:stackedID};
    assert.ok(stacked.windowID && stacked.pid, 'second App exposes its native window');
    const first = await fixture(), second = await fixture();
    window.setContentSize(1440, 920);
    await click('button[aria-label="收起工作区"]'); await settle();
    const turnID = await startTurn(sessionID, 'preview-running');
    const artifactChatGeometry = await chatGeometry();
    // A stale window must never mount a loading/stopped card, even briefly.
    const expired = await fixture();
    await native.quitApp({bundleID:fixtureBundleID,pid:expired.pid});
    await js(`window.__invalidPreviewMounts = 0; window.__invalidPreviewObserver = new MutationObserver(records => {
      for (const record of records) for (const node of record.addedNodes)
        if (node.nodeType === 1 && (node.matches('[data-computer-preview]') || node.querySelector('[data-computer-preview]'))) window.__invalidPreviewMounts++;
    }); window.__invalidPreviewObserver.observe(document.body, {childList:true,subtree:true});`);
    await operation(expired, turnID, sessionID, 'computer_window_not_found');
    await waitFor(() => entry()?.error === true && !entry()?.child, 'invalid window capture stops');
    await waitFor(() => [...computerPreviewManager.clients.values()].some(c => c.sessionID === sessionID
      && c.sent.get(fixtureBundleID) === entry()?.version && !c.waiting), 'renderer acknowledges invalid preview');
    await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
    assert.equal(await js(`window.__invalidPreviewMounts`), 0, 'invalid target never mounts picture-in-picture');
    assert.equal(await js(`Boolean(document.querySelector('[data-computer-preview]'))`), false);
    await js(`window.__invalidPreviewObserver.disconnect()`);
    check('invalid window preserves the tool error but never mounts a loading or stopped preview');
    await operation(first, turnID); await live(first);
    await waitFor(() => js(`(() => {
      const img = document.querySelector('[data-computer-preview] img[draggable="false"]');
      return img?.complete && img.naturalWidth > 0 && img.src.startsWith('data:image/png;base64,');
    })()`), 'native PNG frame decodes in the renderer');
    const frameAlpha = await js(`(() => {
      const img = document.querySelector('[data-computer-preview] img[draggable="false"]');
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth; canvas.height = img.naturalHeight;
      const context = canvas.getContext('2d'); context.drawImage(img, 0, 0);
      const alpha = (x, y) => context.getImageData(x, y, 1, 1).data[3];
      return {
        corners: [[0, 0], [canvas.width - 1, 0], [0, canvas.height - 1], [canvas.width - 1, canvas.height - 1]].map(([x, y]) => alpha(x, y)),
        center: alpha(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2)),
      };
    })()`);
    // Downscaling can leave a few alpha levels at the antialiased bottom corners.
    assert.ok(frameAlpha.corners.every(alpha => alpha < 16), `all four native corners stay nearly transparent, not opaque black: ${frameAlpha.corners}`);
    assert.equal(frameAlpha.center, 255, 'window content remains opaque');
    check('BGRA capture and PNG IPC preserve native window corner transparency in the renderer');
    const captured = entry().child;
    assert.equal(await js(`import('/src/state/workspaceStore.ts').then(m => m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).presentation)`), 'hidden');
    const geometry = await js(`(() => { const card=document.querySelector('[data-computer-preview]'), artifacts=document.querySelector('[aria-label="临时工作区内容"]'); const c=card.getBoundingClientRect(),a=artifacts.getBoundingClientRect();return {top:c.top,bottom:c.bottom,artifactBottom:a.bottom,height:innerHeight}})()`);
    assert.ok(geometry.top >= geometry.artifactBottom && geometry.bottom <= geometry.height, JSON.stringify(geometry));
    assert.equal(await js(`Boolean(document.querySelector('[aria-label="临时工作区内容"] [data-computer-preview]'))`), false, 'preview is not artifact content');
    assert.deepEqual(await chatGeometry(), artifactChatGeometry, 'preview does not change the artifact rail chat layout');
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
      await waitFor(() => js(`(() => {const card=document.querySelector('[data-computer-preview]'), img=card?.querySelector('img[draggable="false"]');if(!img?.complete)return false; const r=img.getBoundingClientRect(),box=img.parentElement.getBoundingClientRect(),c=card.getBoundingClientRect();return Math.abs(r.width/r.height-${ratio})<0.01 && Math.abs(box.width-r.width)<1 && Math.abs(box.height-r.height)<1 && Math.abs(c.height-r.height-2)<1 && Math.abs(r.top-c.top-1)<1 && card.querySelectorAll('img').length===1 && Boolean(card.getAttribute('aria-label'));})()`), 'titleless preview fits its image without header space or letterboxing');
    };
    await assertAspect(first.frame.width / first.frame.height);
    for (const [label, ratio] of [['wide',960/532],['portrait',360/672],['restored',520/532]]) {
      children.find(child => child.pid === first.pid).kill('SIGUSR1');
      await assertAspect(ratio);
      assert.equal(entry().child, captured, 'resizing reuses the same stream');
      await screenshot(`computer-preview-${label}`);
    }
    check('titleless preview follows wide and portrait resizing without header space, letterboxing or restarting capture');


    const workspaceDeadline = entry().expiresAt;
    await js(`window.__previewNode = document.querySelector('[data-computer-preview]');
      window.__previewImage = window.__previewNode.querySelector('img');`);
    const assertContinuousPreview = async () => {
      assert.equal(entry().child, captured, 'workspace layout reuses the original capture process');
      assert.ok(!exited(captured), 'workspace layout never stops the stream');
      assert.equal(entry().expiresAt, workspaceDeadline, 'workspace changes do not renew activity');
      assert.ok(await js(`window.__previewNode === document.querySelector('[data-computer-preview]')
        && window.__previewImage === document.querySelector('[data-computer-preview] img')`), 'workspace changes reuse both card and image DOM nodes');
    };
    const assertExpandedPosition = async () => {
      const position = await js(`(() => {
        const conversation=document.querySelector('.pudding-conversation'), card=document.querySelector('[data-computer-preview]');
        const c=conversation.getBoundingClientRect(), p=card.getBoundingClientRect();
        return {rail:conversation.dataset.activityRail,artifacts:!!conversation.querySelector('[aria-label="临时工作区内容"]'),
          right:c.right-p.right,top:p.top-c.top,within:p.left>=c.left && p.bottom<=c.bottom,
          visible:document.elementFromPoint(p.left+p.width/2,p.top+p.height/2)?.closest('[data-computer-preview]')===card};
      })()`);
      assert.deepEqual(position, {rail:undefined,artifacts:false,right:16,top:16,within:true,visible:true}, 'expanded workspace keeps preview above the chat, not in the workspace or an artifact rail');
    };
    await click('button[aria-label="打开工作区"]'); await settle(); await live(first);
    await assertContinuousPreview(); await assertExpandedPosition();
    const expandedFrame = await image();
    await native.act({bundleID:fixtureBundleID,windowID:first.windowID,elementID:editor.elementID,action:'set_value',value:'Workspace preview still live'});
    await waitFor(async () => (await image()) !== expandedFrame, 'expanded workspace still receives updated native frames');
    await screenshot('computer-preview-workspace-open');
    await click('button[aria-label="专注"]'); await settle();
    await assertContinuousPreview(); await assertExpandedPosition();
    await screenshot('computer-preview-workspace-focused');
    await click('button[aria-label="退出专注"]'); await settle();
    await assertContinuousPreview(); await assertExpandedPosition();
    await click('button[aria-label="收起工作区"]'); await settle();
    await assertContinuousPreview();
    assert.ok((await api(`/sessions/${sessionID}`)).running, 'workspace changes do not cancel the turn');
    check('workspace expansion/focus/collapse reuse image DOM and capture process, preserve expiry, and receive live frames at chat top-right');

    // The smoke disables throttling globally for deterministic frame checks.
    // Electron also keeps visibilityState="visible" in that mode, even when
    // the native window is hidden. Restore normal visibility for this check.
    window.webContents.setBackgroundThrottling(true);
    window.hide();
    await waitFor(() => js(`document.visibilityState === 'hidden' && !document.querySelector('[data-computer-preview]')`), 'hidden page removes preview');
    await waitFor(() => !entry()?.child && exited(captured), 'hidden page still releases capture');
    window.show();
    await waitFor(() => js(`document.visibilityState === 'visible'`), 'page becomes visible again');
    window.webContents.setBackgroundThrottling(false);
    await live(first);
    assert.notEqual(entry().child, captured, 'page visibility resumes capture after the previous process exited');
    assert.equal(entry().expiresAt, workspaceDeadline, 'page visibility never renews activity');
    const resumed = entry().child;
    await clickPreview(first);
    const nativeState = await native.observe({bundleID:fixtureBundleID,windowID:first.windowID,maxElements:100});
    assert.equal(nativeState.elements.find(e => e.role === 'AXTextField' && !e.secure)?.value, 'Workspace preview still live', 'preview click never types into the app');
    check('page hide still pauses capture; visibility resumes the same target; preview click raises the exact application');

    await operation(second, turnID); await live(second);
    await waitFor(() => exited(resumed), 'window switch releases old stream');
    await clickPreview(second);
    const expected = entry().target.windowID;
    await operation(first, turnID, otherSessionID);
    assert.equal(entry().target.windowID, expected, 'another session cannot replace this preview');
    window.setContentSize(720, 920);
    await waitFor(() => js(`(() => {const r=document.querySelector('[data-computer-preview]')?.getBoundingClientRect();return r && r.width>100 && r.x>=0 && r.right<=innerWidth && r.bottom<=innerHeight})()`), 'narrow preview stays visible');
    await screenshot('computer-preview-narrow');
    await js(`import('/src/state/workspaceStore.ts').then(m => m.closeWorkspaceTabs(${JSON.stringify(sessionID)}, m.getWorkspaceSessionUI(${JSON.stringify(sessionID)}).tabOrder))`);
    await waitFor(() => js(`!document.querySelector('[aria-label="临时工作区内容"]') && Boolean(document.querySelector('[data-computer-preview]'))`), 'preview remains independent when all artifacts close');
    const standaloneGeometry = new Map();
    for (const width of [720, 1440]) {
      window.setContentSize(width, 920);
      await waitFor(() => js(`innerWidth === ${width}`), 'standalone preview viewport resize');
      await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
      const position = await js(`(() => {
        const conversation=document.querySelector('.pudding-conversation'), card=document.querySelector('[data-computer-preview]');
        const c=conversation.getBoundingClientRect(), p=card.getBoundingClientRect();
        return {rail:conversation.dataset.activityRail,left:p.left-c.left,top:p.top-c.top,right:c.right-p.right};
      })()`);
      assert.equal(position.rail, undefined, 'preview alone never reserves an artifact rail');
      assert.equal(position.right, 16, 'standalone preview floats at the conversation right edge');
      assert.equal(position.top, 16, 'standalone preview floats at the conversation top edge');
      assert.ok(position.left >= 0, JSON.stringify(position));
      standaloneGeometry.set(width, await chatGeometry());
      await screenshot(`computer-preview-standalone-${width}`);
    }
    check('window switching drops old frames; session isolation; wide/narrow standalone preview floats at top-right without an artifact rail');

    const cancelled = entry().child;
    await api(`/sessions/${sessionID}/cancel`, 'POST');
    await waitFor(() => exited(cancelled) && !entry(), 'turn cancellation stops and releases capture');
    await waitFor(() => js(`!document.querySelector('[data-computer-preview]')`), 'cancelled preview removed');
    for (const width of [720, 1440]) {
      window.setContentSize(width, 920);
      await waitFor(() => js(`innerWidth === ${width}`), 'no-preview viewport resize');
      await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
      assert.deepEqual(await chatGeometry(), standaloneGeometry.get(width), 'preview removal does not move or resize transcript/composer columns');
    }
    check('standalone preview appearance/removal leaves transcript and composer geometry unchanged');
    const completedTurnID = await startTurn(sessionID, 'preview-complete', 100);
    await operation(first, completedTurnID); await live(first);
    const completed = entry().child;
    const completedExpiry = entry().expiresAt;
    await waitFor(() => js(`document.querySelector('[data-computer-preview]')?.dataset.status === 'complete'`), 'real turn completion shows finished preview');
    await waitFor(() => exited(completed), 'completion stops capture');
    await js(`window.__completedPreviewImage = document.querySelector('[data-computer-preview] img')`);
    await click('button[aria-label="打开工作区"]'); await settle();
    await assertExpandedPosition();
    assert.ok(await js(`window.__completedPreviewImage === document.querySelector('[data-computer-preview] img')`), 'expansion preserves the completed turn last frame');
    assert.equal(entry(), undefined, 'showing a completed preview never restarts capture');
    await screenshot('computer-preview-completed-workspace-open');
    await click('button[aria-label="收起工作区"]'); await settle();
    assert.ok(await js(`window.__completedPreviewImage === document.querySelector('[data-computer-preview] img')`), 'collapse preserves the completed turn last frame');
    await delay(5_000);
    assert.equal(await js(`document.querySelector('[data-computer-preview]')?.dataset.status`), 'complete', 'last frame remains after the old three-second expiry');
    await waitFor(() => js(`!document.querySelector('[data-computer-preview]')`), 'completed preview disappears after thirty seconds', 35_000);
    assert.ok(Date.now() >= completedExpiry - 100, 'completion keeps the original activity deadline');
    check('real SSE cancel stops immediately; completion stops capture and keeps the per-app activity deadline');

    for (const status of ['completed', 'cancelled']) {
      const previewTurn = await startTurn(sessionID, `preview-before-${status}`, 100);
      await operation(first, previewTurn); await live(first);
      const previousCapture = entry().child;
      await waitFor(() => js(`document.querySelector('[data-computer-preview]')?.dataset.status === 'complete'`), 'previous turn retains its final frame');
      await waitFor(() => exited(previousCapture) && !entry(), 'previous capture released');

      // Start inside the 30-second retention window. This turn has no native
      // activity: subscribing and receiving ordinary SSE must not revive PiP.
      const chatTurn = await startTurn(sessionID, `chat-without-computer-${status}`, status === 'completed' ? 100 : 2000);
      assert.equal(await image(), undefined, 'new turn hides the previous final frame');
      assert.equal(entry(), undefined, 'ordinary chat never starts native capture');
      if (status === 'cancelled') await api(`/sessions/${sessionID}/cancel`, 'POST');
      await waitFor(() => js(`import('/src/state/overlayStore.ts').then(m => !m.useOverlayStore.getState().runningTurns[${JSON.stringify(sessionID)}])`), `ordinary chat ${status}`);
      assert.equal((await api(`/sessions/${sessionID}/turns/${chatTurn}`)).status, status, 'canonical turn confirms the outcome after the overlay is reconciled');
      await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
      assert.equal(await js(`Boolean(document.querySelector('[data-computer-preview]'))`), false, `${status} ordinary chat must not resurrect the previous turn's preview`);
      assert.equal(entry(), undefined, 'no native activity was needed to exercise retained renderer state');
      await screenshot(`computer-preview-no-resurrection-${status}`);
      check(`ordinary chat ${status} inside the retention window cannot resurrect the previous Computer Use preview`);
    }

    const closeTurn = await startTurn(sessionID, 'preview-window-close');
    await operation(second, closeTurn); await live(second);
    await native.quitApp({bundleID:fixtureBundleID,pid:second.pid});
    await waitFor(() => !entry()?.child, 'closing target application stops capture');
    assert.equal(entry().frame, null, 'closed window cannot display stale image');
    await waitFor(() => js(`!document.querySelector('[data-computer-preview]')`), 'closed window unmounts picture-in-picture');
    assert.equal(await js(`document.body.innerText.includes('窗口预览已停止')`), false);
    await screenshot('computer-preview-invalid-window');
    await operation(first, closeTurn); await live(first);
    check('invalid preview is removed; another valid target in the same turn can show a new preview');
    await api(`/sessions/${sessionID}/cancel`, 'POST');
    check('native application exit stops capture and clears stale content');

    const stackTurn = await startTurn(sessionID, 'preview-stack');
    const cards = () => js(`Array.from(document.querySelectorAll('[data-computer-preview]')).map(el=>el.dataset.appId)`);
    const assertStack = async appID => {
      await waitFor(async () => (await cards())[0] === appID, 'latest operated app leads the stack');
      const geometry = await js(`(() => {
        const cards=Array.from(document.querySelectorAll('[data-computer-preview]'));
        const [a,b]=cards.map(el=>el.getBoundingClientRect());
        const x=(Math.max(a.left,b.left)+Math.min(a.right,b.right))/2;
        const y=(Math.max(a.top,b.top)+Math.min(a.bottom,b.bottom))/2;
        return {overlap:Math.min(a.right,b.right)>Math.max(a.left,b.left)&&Math.min(a.bottom,b.bottom)>Math.max(a.top,b.top),
          front:document.elementFromPoint(x,y)?.closest('[data-computer-preview]')?.dataset.appId,
          within:cards.every(el=>{const r=el.getBoundingClientRect();return r.x>=0&&r.right<=innerWidth&&r.bottom<=innerHeight})};
      })()`);
      assert.ok(geometry.overlap && geometry.within, JSON.stringify(geometry));
      assert.equal(geometry.front, appID, 'cards really occlude one another in activity order');
    };
    await operation(first, stackTurn); await live(first);
    await operation(stacked, stackTurn); await live(stacked);
    await assertStack(stackedID);
    await clickPreview(stacked);
    const secondDeadline = entry(stackedID).expiresAt;
    await delay(1_000);
    await operation(first, stackTurn); await assertStack(fixtureBundleID);
    await clickPreview(first);
    assert.equal(entry(stackedID).expiresAt, secondDeadline, 'operating A does not renew B');
    await screenshot('computer-preview-stack');
    await delay(5_000);
    await operation(first, stackTurn); await assertStack(fixtureBundleID);
    const firstStream = entry().child, secondStream = entry(stackedID).child;
    await waitFor(() => exited(secondStream), 'idle App stops its capture independently', 31_000);
    await waitFor(async () => JSON.stringify(await cards()) === JSON.stringify([fixtureBundleID]), 'only idle App leaves the stack');
    assert.equal(entry().child, firstStream); assert.ok(!exited(firstStream));
    await screenshot('computer-preview-stack-expired');
    await operation(stacked, stackTurn); await live(stacked); await assertStack(stackedID);
    await native.quitApp({bundleID:stackedID,pid:stacked.pid});
    stacked = undefined;
    await waitFor(async () => JSON.stringify(await cards()) === JSON.stringify([fixtureBundleID]), 'failed App leaves the other preview intact');
    check('two real App previews overlap; activity alone reorders; each expires after its own 30 seconds; failure removes only its card');
    await api(`/sessions/${sessionID}/cancel`, 'POST');
    assert.equal(expectedReveals, 4, 'cover both instances and both Apps');
    assert.equal(previewReveals.length, expectedReveals, 'only the four explicit clicks can request reveal');
    assert.ok(previewReveals.every(record => record.result === true && !record.error && record.finishedAt), 'no hidden reveal failure or in-flight request');
    check('four explicit preview clicks await native success across two same-bundle processes and two Apps; no unsolicited reveals');
  } finally {
    if (stacked?.pid) await native.quitApp({bundleID:stacked.appID,pid:stacked.pid});
    for (const child of children) if (!exited(child)) await native.quitApp({bundleID:fixtureBundleID,pid:child.pid}).catch(() => child.kill());
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

async function verifyMarkdownLinks(sessionID, secondaryID, projectRoot) {
  phase = 'project Markdown links';
  const q = JSON.stringify;
  const webURL = process.env.PUDDING_DEV_URL + '/__workspace_smoke?from=markdown#intro';
  const source = '# Link fixture\n\n[中文文档](docs/%E4%B8%AD%E6%96%87%20Guide.md#%E5%AE%89%E8%A3%85)\n\n[Web](' + webURL + ')\n\n[Missing](gone.md)\n\n[Code](main.ts#L12)\n\n[Missing heading](#unknown-heading)\n\n[Outside](../outside.md)\n\n[Local](#local-section)\n\n[Reference][guide]\n\n[guide]: docs/%E4%B8%AD%E6%96%87%20Guide.md\n\n' + 'Spacing paragraph.\n\n'.repeat(70) + '## Local section\n\nEnd\n';
  const target = '# 中文文档\n\n[Back](../README.md)\n\n' + 'Spacing.\n\n'.repeat(65) + '## 安装\n\nInstall here.\n\n' + 'Tail.\n\n'.repeat(30);
  fs.mkdirSync(path.join(projectRoot,'docs'));
  fs.writeFileSync(path.join(projectRoot,'README.md'),source);
  fs.writeFileSync(path.join(projectRoot,'docs','中文 Guide.md'),target);
  fs.writeFileSync(path.join(projectRoot,'main.ts'), Array.from({length:40},(_,i)=>`export const value${i}= ${i};`).join('\n'));
  const existingURLs = [process.env.PUDDING_DEV_URL + '/__workspace_smoke?existing=1', process.env.PUDDING_DEV_URL + '/__workspace_smoke?existing=2'];
  for (const url of existingURLs) {
    const tab = await api(`/sessions/${sessionID}/browser/tabs`, 'POST', {});
    await api(`/sessions/${sessionID}/browser/tabs/${tab.id}/open`, 'POST', {url});
  }
  const initialURL = window.webContents.getURL();
  const doc = () => js(`Array.from(document.querySelectorAll('[data-project-document]')).filter(el=>!el.hidden).map(el=>el.dataset.projectDocument)`);
  const open = async name => {
    await js(`import('/src/state/projectRevealStore.ts').then(m=>m.requestProjectFileReveal({sessionID:${q(sessionID)},rootPath:${q(projectRoot)},relativePath:${q(name)}}))`);
    await waitFor(()=>js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .vditor-ir [data-type="a"]'))`),'Markdown links mounted');
    await delay(150);
  };
  const link = raw => `Array.from(document.querySelectorAll('[data-project-document]:not([hidden]) [data-type="a"]')).find(el=>el.querySelector(':scope > .vditor-ir__marker--link')?.textContent===${q(raw)})`;
  const activate = async (raw, modifier=false) => {
    const expression=link(raw);
    await waitFor(()=>js(`Boolean(${expression})`),'link target mounted');
    await js(`(${expression}).scrollIntoView({block:'center'})`);
    await delay(150);
    if (!modifier) { await clickElement(expression); return; }
    await focusSmokeWindow();
    const rect=await js(`(${expression}).getBoundingClientRect().toJSON()`);
    const zoom=window.webContents.getZoomFactor(), point={x:Math.round((rect.x+rect.width/2)*zoom),y:Math.round((rect.y+rect.height/2)*zoom)};
    window.webContents.sendInputEvent({type:'mouseMove',...point});
    window.webContents.sendInputEvent({type:'mouseDown',...point,button:'left',clickCount:1,modifiers:['meta']});
    window.webContents.sendInputEvent({type:'mouseUp',...point,button:'left',clickCount:1,modifiers:['meta']});
    await delay(150);
  };
  await open('README.md');
  await activate('docs/%E4%B8%AD%E6%96%87%20Guide.md#%E5%AE%89%E8%A3%85');
  await waitFor(async()=> (await doc()).some(x=>x.endsWith(':docs/中文 Guide.md')),'relative Chinese link opens project document');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .vditor-ir h2'))`),'destination heading');
  await waitFor(()=>js(`(()=>{const d=document.querySelector('[data-project-document]:not([hidden])'),h=d.querySelector('h2'),r=d.querySelector('.pudding-vditor-editor').getBoundingClientRect();return h.getBoundingClientRect().top>=r.top-2&&h.getBoundingClientRect().top<r.bottom})()`),'cross-file Chinese anchor scrolled');
  check('single-click relative Chinese path resolves from README and opens its heading');
  await activate('../README.md');
  await waitFor(async()=>(await doc()).some(x=>x.endsWith(':README.md')),'parent relative link returns');
  await activate('#local-section');
  await waitFor(()=>js(`document.querySelector('[data-project-document]:not([hidden]) .pudding-vditor-editor').scrollTop>500`),'same document heading scrolled');
  assert.equal(window.webContents.getURL(),initialURL);
  check('parent relative links and same-document fragments stay inside project without navigating shell');

  await open('README.md');
  const refLink = `document.querySelector('[data-project-document]:not([hidden]) [data-type="link-ref"]')`;
  await js(`(${refLink}).scrollIntoView({block:'center'})`);
  await clickElement(refLink);
  await waitFor(async()=>(await doc()).some(x=>x.endsWith(':docs/中文 Guide.md')),'reference-style link resolved');
  check('reference-style Markdown link uses its definition destination');
  await open('README.md');
  await activate('../outside.md',true);
  await waitFor(()=>js(`document.body.innerText.includes('路径不在项目内')`),'out-of-root link feedback');
  assert.ok((await doc()).some(x=>x.endsWith(':README.md')));
  await activate('#unknown-heading',true);
  await waitFor(()=>js(`document.body.innerText.includes('文档中未找到此锚点')`),'missing anchor feedback');
  await activate('main.ts#L12',true);
  await waitFor(()=>js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .monaco-editor'))`),'linked source editor');
  await waitFor(()=>js(`import('/__workspace_editor.js').then(({editor})=>editor.getEditors().some(e=>e.getDomNode()?.getBoundingClientRect().width>0&&e.getSelection()?.startLineNumber===12))`),'source line fragment revealed');
  check('code links reveal line numbers and missing Markdown anchors report failure');
  await open('README.md');
  await activate('gone.md',true);
  await waitFor(()=>js(`document.body.innerText.includes('文件不存在，或不在可访问')`),'missing file feedback');
  check('invalid and missing local links show feedback instead of opening a web page');

  await open('README.md');
  await click('[data-project-document]:not([hidden]) button[aria-label="查看源码"]');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .monaco-editor'))`),'source editor');
  await js(`(async()=>{const {editor}=await import('/__workspace_editor.js');const e=editor.getEditors().find(e=>e.getDomNode()?.getBoundingClientRect().width>0),m=e.getModel();e.executeEdits('markdown-link-test',[{range:{startLineNumber:1,startColumn:1,endLineNumber:1,endColumn:1},text:${q('DRAFT_UNSAVED\n\n')}}]);})()`);
  await click('[data-project-document]:not([hidden]) button[aria-label="实时编辑 Markdown"]');
  await waitFor(()=>js(`Boolean(document.querySelector('[data-project-document]:not([hidden]) .vditor-ir [data-type="a"]'))`),'Markdown returns');
  await js(`(()=>{const link=${link(webURL)},range=document.createRange();link.closest('[contenteditable]').focus();range.selectNodeContents(link.querySelector('.vditor-ir__link'));range.collapse(false);const selection=window.getSelection();selection.removeAllRanges();selection.addRange(range);})()`);
  window.webContents.sendInputEvent({type:'keyDown',keyCode:'Left'});
  window.webContents.sendInputEvent({type:'keyUp',keyCode:'Left'});
  await waitFor(()=>js(`(${link(webURL)}).classList.contains('vditor-ir__node--expand')`),'link expanded for text editing');
  await activate(webURL,true);
  await waitFor(async()=> (await api(`/sessions/${sessionID}/browser/tabs`)).tabs.some(t=>t.url===webURL),'web URL opened in correct session');
  const tabsResult=await api(`/sessions/${sessionID}/browser/tabs`);
  const tab=tabsResult.tabs.find(t=>t.url===webURL);
  assert.equal(tabsResult.tabs.length,3);
  assert.ok(existingURLs.every(url=>tabsResult.tabs.some(tab=>tab.url===url)), 'existing browser pages preserved');
  await waitFor(()=>js(`import('/src/state/workspaceStore.ts').then(m=>m.getWorkspaceSessionUI(${q(sessionID)}).activeTab===${q('browser:')}+${q(tab.id)})`),'browser workspace selected');
  assert.equal((await api(`/sessions/${secondaryID}/browser/tabs`)).tabs.length,0);
  assert.equal(window.webContents.getURL(),initialURL);
  await open('README.md');
  assert.ok(await js(`document.querySelector('[data-project-document]:not([hidden]) .vditor-ir').textContent.includes('DRAFT_UNSAVED')`));
  assert.equal(fs.readFileSync(path.join(projectRoot,'README.md'),'utf8'),source);
  await activate(webURL,true);
  await waitFor(()=>js(`import('/src/state/workspaceStore.ts').then(m=>m.getWorkspaceSessionUI(${q(sessionID)}).activeTab===${q('browser:')}+${q(tab.id)})`),'existing web tab selected');
  assert.equal((await api(`/sessions/${sessionID}/browser/tabs`)).tabs.length,3);
  await open('README.md');
  check('web link opens session-scoped workspace browser, preserves existing pages, reuses its tab and retains unsaved draft');
  await screenshot('markdown-links');
}

async function verifyIntegratedProjectSearch(sessionID, secondaryID, projectRoot) {
  phase = 'integrated project content search';
  const field = '[data-project-search] input';
  const tree = '[data-project-tree]';
  const results = '[data-project-search-results]';
  const needle = 'PuddingSearchNeedle';
  await js(`import('/src/state/projectRevealStore.ts').then(m=>m.requestProjectFileReveal({sessionID:${JSON.stringify(sessionID)},rootPath:${JSON.stringify(projectRoot)},relativePath:'nested/match.ts'}))`);
  await waitFor(()=>js(`Boolean(document.querySelector('${field}')&&document.querySelector('${tree}'))`),'file search and tree mounted');
  assert.deepEqual(await js(`Array.from(document.querySelectorAll('[data-project-workspace] nav button')).map(el=>el.getAttribute('aria-label'))`), ['项目文件','源代码管理']);
  assert.equal(await js(`document.activeElement===document.querySelector('${field}')`),false,'opening project does not steal editor focus');
  await waitFor(()=>js(`document.querySelector('${tree}').innerText.includes('match.ts')`),'nested directory expanded');
  const searchY=await js(`document.querySelector('${field}').getBoundingClientRect().y`);
  await screenshot('files-with-search');
  await input(field,needle);
  await waitFor(()=>js(`Boolean(document.querySelector('${results} mark'))`),'file content matches');
  assert.equal(await js(`Boolean(document.querySelector('${tree}'))`),false);
  assert.ok(await js(`document.querySelector('${results}').innerText.includes('1 个文件中有 1 个结果')`));
  assert.equal(await js(`document.querySelector('${field}').getBoundingClientRect().y`),searchY);
  await screenshot('file-content-results');
  await clickElement(`document.querySelector('${results} mark').closest('button')`);
  await waitFor(()=>js(`import('/__workspace_editor.js').then(({editor})=>editor.getEditors().some(e=>e.getDomNode()?.getBoundingClientRect().width>0&&e.getSelection()?.startLineNumber===3))`),'match reveals source line');
  assert.equal(await js(`document.querySelector('${field}').value`),needle);
  check('one file search input replaces the tree with grouped content matches and opens the matching source line');

  await click('[data-project-workspace] nav button[aria-label="源代码管理"]');
  await click('[data-project-workspace] nav button[aria-label="项目文件"]');
  assert.equal(await js(`document.querySelector('${field}').value`),needle);
  await waitFor(()=>js(`Boolean(document.querySelector('${results} mark'))`),'query survives version switch');
  await click('[data-project-search] button[aria-label="清除"]');
  assert.equal(await js(`document.querySelector('${field}').value`),'');
  await waitFor(()=>js(`document.querySelector('${tree}')?.innerText.includes('match.ts')`),'clear restores expanded tree');
  assert.equal(await js(`document.activeElement===document.querySelector('${field}')`),true);
  await input(field,'definitely-no-match');
  await waitFor(()=>js(`document.querySelector('${results}')?.textContent.includes('未找到匹配内容')`),'empty result');
  await js(`document.querySelector('${field}').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',isComposing:true,bubbles:true}))`);
  assert.equal(await js(`document.querySelector('${field}').value`),'definitely-no-match','composition Escape does not clear');
  await click(field);
  window.webContents.sendInputEvent({type:'keyDown',keyCode:'Escape'});
  window.webContents.sendInputEvent({type:'keyUp',keyCode:'Escape'});
  await waitFor(()=>js(`Boolean(document.querySelector('${tree}'))`),'Escape restores tree');
  check('version switching retains search; clear and Escape restore directory expansion and respect composition');

  await input(field,needle);
  await waitFor(()=>js(`Boolean(document.querySelector('${results} mark'))`),'results return');
  window.setContentSize(780,760);
  await delay(350);
  await click('[data-project-workspace] button[aria-label="项目文件"]');
  await waitFor(()=>js(`document.querySelector('${field}').getBoundingClientRect().width>0`),'narrow files visible');
  assert.equal(await js(`document.querySelector('${field}').value`),needle);
  await js(`document.documentElement.classList.add('dark')`);
  await screenshot('file-search-narrow-dark');
  assert.ok(await js(`(()=>{const p=document.querySelector('[data-project-search]');return p.scrollWidth<=p.clientWidth+1})()`),'narrow search has no horizontal overflow');
  await clickElement(`document.querySelector('${results} mark').closest('button')`);
  await waitFor(()=>js(`document.querySelector('[data-project-document]:not([hidden]) .monaco-editor')?.getBoundingClientRect().width>0`),'narrow match opens viewer');
  await click('[data-project-workspace] button[aria-label="项目文件"]');
  assert.equal(await js(`document.querySelector('${field}').value`),needle);
  check('narrow files/results/viewer navigation preserves the query and layout in dark mode');
  window.setContentSize(1440,920);
  await js(`document.documentElement.classList.remove('dark');`);
  await js(`(async()=>{const {router}=await import('/src/main.tsx');await router.navigate({to:'/',search:{session:${JSON.stringify(secondaryID)}}});const m=await import('/src/state/workspaceStore.ts');m.openWorkspaceTab(${JSON.stringify(secondaryID)},'project');})()`);
  await waitFor(()=>js(`document.querySelector('${field}')?.value===''&&Boolean(document.querySelector('${tree}'))`),'another session starts with tree');
  assert.equal(await js(`Boolean(document.querySelector('${results}'))`),false);
  check('changing session resets the query and cannot display the previous session results');
}

async function verifyArchiveNavigation(projectSessionID, projectID) {
  phase = "archive navigation";
  await click('.pudding-chat-pane-header button[aria-label="操作"]');
  await clickText("归档", '[role="menuitem"]');
  await waitFor(() => js(`import('/src/main.tsx').then(({router})=>{
    const search=router.state.location.search;
    return search.draft==='1'&&search.project===${JSON.stringify(projectID)}&&!search.session&&!search.split;
  })`), "project session archives to its project draft");
  assert.equal((await api("/sessions")).sessions.some(session => session.id === projectSessionID), false);
  check("archiving the current project session opens a new draft in that project");

  const globalSession = await api("/sessions", "POST", { title: "Global archive", provider: "mock", model: "mock" });
  await js(`import('/src/main.tsx').then(async({router})=>{
    await router.options.context.queryClient.invalidateQueries({queryKey:['sessions']});
    await router.navigate({to:'/',search:{session:${JSON.stringify(globalSession.id)}}});
  })`);
  await waitFor(() => js(`document.body.textContent.includes('Global archive')`), "global session selected");
  await click('.pudding-chat-pane-header button[aria-label="操作"]');
  await clickText("归档", '[role="menuitem"]');
  await waitFor(() => js(`import('/src/main.tsx').then(({router})=>{
    const search=router.state.location.search;
    return search.draft==='1'&&!search.project&&!search.session&&!search.split;
  })`), "projectless session archives to global draft");
  assert.equal((await api("/sessions")).sessions.some(session => session.id === globalSession.id), false);
  check("archiving the current projectless session opens a global new draft");
  await verifyArchiveRefreshRace(projectID);
}

async function verifyArchiveRefreshRace(projectID) {
  const retained = await api("/sessions", "POST", { title: "Retained session", provider: "mock", model: "mock" });
  const readSearch = () => js(`import('/src/main.tsx').then(({router}) => router.state.location.search)`);
  const settleRenderer = () => js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  const cases = [
    { entry: "header", projectID },
    { entry: "rail", projectID },
    { entry: "header" },
    { entry: "rail", projectID, switchAway: true },
    { entry: "header", projectID, split: true },
    { entry: "header", projectID, reject: true },
  ];
  for (const testCase of cases) {
    phase = `archive refresh race ${JSON.stringify(testCase)}`;
    const session = await api("/sessions", "POST", {
      title: "Archive race", provider: "mock", model: "mock", projectID: testCase.projectID,
    });
    const initialSearch = testCase.split ? { session: retained.id, split: session.id } : { session: session.id };
    await js(`import('/src/main.tsx').then(async ({router}) => {
      await router.options.context.queryClient.invalidateQueries({queryKey:['sessions']});
      const rail = await import('/src/state/railStore.ts');
      rail.setRailCollapsed(false);
      await router.navigate({to:'/', search:${JSON.stringify(initialSearch)}});
    })`);
    const pane = `[data-chat-pane-role="${testCase.split ? "split" : "primary"}"]`;
    await waitFor(() => js(`document.querySelector(${JSON.stringify(pane)})?.textContent.includes('Archive race')`), "race session selected");
    // Hold only the archive response. The real sessions refetch can observe the
    // committed archive first, as it can during backend resource cleanup.
    await js(`(() => {
      const originalFetch = window.fetch;
      const gate = new Promise(resolve => {
        window.__archiveRace = { ready:false, release:resolve, restore:() => { window.fetch=originalFetch; } };
      });
      window.fetch = async (...args) => {
        if (!String(args[0]).endsWith(${JSON.stringify(`/sessions/${session.id}/archive`)})) return originalFetch(...args);
        const response = ${Boolean(testCase.reject)}
          ? new Response(JSON.stringify({error:'archive rejected'}), {status:503,headers:{'Content-Type':'application/json'}})
          : await originalFetch(...args);
        window.__archiveRace.ready = true;
        await gate;
        return response;
      };
    })()`);
    try {
      if (testCase.entry === "rail") {
        const selector = `[data-sidebar="menu-item"]:has([data-session-item-id="${session.id}"]) button[aria-label="归档"]`;
        await waitFor(() => js(`Boolean(document.querySelector(${JSON.stringify(selector)}))`), "rail archive button");
        await js(`document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block:'nearest'})`);
        await click(selector);
      } else {
        await click(`${pane} .pudding-chat-pane-header button[aria-label="操作"]`);
        await clickText("归档", '[role="menuitem"]');
      }
      await waitFor(() => js(`window.__archiveRace.ready`), "archive response held");
      await js(`import('/src/main.tsx').then(({router}) => router.options.context.queryClient.invalidateQueries({queryKey:['sessions']}))`);
      await settleRenderer();
      assert.deepEqual(await readSearch(), initialSearch, "sessions refetch must not navigate while this session is archiving");
      if (!testCase.reject) {
        assert.equal((await api("/sessions")).sessions.some(item => item.id === session.id), false);
        assert.equal(await js(`Boolean(document.querySelector(${JSON.stringify(`${pane} textarea`)}))`), false, "missing pending session must not expose a global draft composer");
      }
      if (testCase.switchAway) {
        await js(`import('/src/main.tsx').then(({router}) => router.navigate({to:'/',search:{view:'projects'}}))`);
      }
      await js(`window.__archiveRace.release()`);
      await waitFor(() => js(`import('/src/main.tsx').then(({router}) => router.options.context.queryClient.getMutationCache().getAll().every(m => m.state.status !== 'pending'))`), "archive mutation settled");
      await settleRenderer();
      const expected = testCase.reject ? initialSearch
        : testCase.switchAway ? { view: "projects" }
        : testCase.split ? { session: retained.id }
        : testCase.projectID ? { draft: "1", project: testCase.projectID }
        : { draft: "1" };
      assert.deepEqual(await readSearch(), expected, "archive completion preserves the correct route");
      if (testCase.reject) {
        assert.equal((await api("/sessions")).sessions.some(item => item.id === session.id), true);
        assert.equal(await js(`Boolean(document.querySelector(${JSON.stringify(`${pane} textarea`)}))`), true, "failed archive restores the original session composer");
      }
      check(`${testCase.entry} archive survives sessions refresh (${testCase.reject ? "failure" : testCase.switchAway ? "user navigated away" : testCase.split ? "split" : testCase.projectID ? "project draft" : "global draft"})`);
    } finally {
      await js(`window.__archiveRace.release(); window.__archiveRace.restore(); delete window.__archiveRace;`);
    }
  }
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
      server.middlewares.use("/favicon.ico", (_req, res) => {
        res.setHeader("Content-Type", "image/png");
        res.end(fs.readFileSync(path.join(repo, "assets/macos/TrayTemplate.png")));
      });
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
  if (process.env.PUDDING_SMOKE_SCENARIO === "git-scroll") {
    await runFile("git", ["init", "--quiet", projectRoot]);
    for (let i = 0; i < 80; i++) fs.writeFileSync(path.join(projectRoot, `scroll-${String(i).padStart(2, "0")}.md`), "# Git scroll fixture\n");
    await runFile("git", ["-C", projectRoot, "add", "scroll-00.md"]);
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "project-search") {
    fs.mkdirSync(path.join(projectRoot, "nested"));
    fs.writeFileSync(path.join(projectRoot, "nested", "match.ts"), "export const first = 1;\n// lead\nexport const PuddingSearchNeedle = 42;\n");
  }
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
  if (["conversation-resize", "conversation-markdown-resize"].includes(process.env.PUDDING_SMOKE_SCENARIO)) await seedConversation(primary.id);
  if (process.env.PUDDING_SMOKE_SCENARIO === "conversation-markdown-resize") {
    // The echo provider flattens newlines. Seed canonical Markdown in this
    // disposable database before the renderer fetches its messages.
    const markdown = "下面是项目状态，拖动宽度时表格和正文都应该保持贴底。".repeat(12) + "\n\n" +
      "| 项目 | 状态 | 备注 |\n| --- | --- | --- |\n" +
      Array.from({length:6}, (_, i) => `| 项目 ${i + 1} | 进行中 | 需要继续跟进验证 |`).join("\n") + "\n\n" +
      "这里是表格后的长段落，宽度改变后会重新换行。".repeat(12) + "\n\n" +
      "| 商品 | 价格 | 详细说明 | 配送区域 |\n| --- | --- | --- | --- |\n" +
      Array.from({length:6}, (_, i) => `| 新鲜蔬菜 ${i + 1} | ¥9.99/份 | 每日新鲜配送，具体库存以门店页面为准 | 中国上海市浦东新区 |`).join("\n") + "\n\n" +
      Array.from({length:5}, (_, i) => `- **项目 ${i + 1}**：列表内容需要随着宽度变化重新换行，阅读位置应保持稳定。\n  - 子项：继续检查段落、列表与表格混合排列的情况。`).join("\n");
    await runFile("python3", ["-c", `import sqlite3,json,sys
db=sqlite3.connect(sys.argv[1])
db.execute("UPDATE messages SET text=?,parts=? WHERE session_id=? AND role='assistant'",(sys.argv[3],json.dumps([{"type":"text","text":sys.argv[3]}]),sys.argv[2]))
db.commit()
db.close()`, path.join(home, "data/pudding.db"), primary.id, markdown]);
  }
  const canvasCount = ["archive-navigation", "automation-presentation"].includes(process.env.PUDDING_SMOKE_SCENARIO) ? 1 : process.env.PUDDING_SMOKE_SCENARIO === "artifact-visibility" ? 3 : 20;
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
  if (process.env.PUDDING_SMOKE_SCENARIO === "archive-navigation") {
    await verifyArchiveNavigation(primary.id, project.id);
    assert.deepEqual(rendererErrors, [], "archive navigation renderer errors");
    return;
  }
  await js(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await click('button[aria-label="打开工作区"]');
  await waitFor(async () => (await tabs()).length === canvasCount, `${canvasCount} canonical canvas tabs`);
  await waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-workspace-stage')).some(el => ['opening','closing'].includes(el.dataset.transition))`), "workspace entrance animation settles");
  for (const label of ["项目", "资源库", "画布"]) {
    await selectSurface(label);
  }
  check(`isolated source Electron/Vite/daemon; ${canvasCount} canonical canvas tabs`);
  if (process.env.PUDDING_SMOKE_SCENARIO === "git-scroll") {
    await verifyProjectGitScroll();
    assert.deepEqual(rendererErrors, [], "git scroll renderer errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "project-search") {
    await verifyIntegratedProjectSearch(primary.id, secondary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "integrated project search renderer errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "markdown-links") {
    await verifyMarkdownLinks(primary.id, secondary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "Markdown link renderer errors");
    return;
  }
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
  if (["conversation-resize", "conversation-markdown-resize"].includes(process.env.PUDDING_SMOKE_SCENARIO)) {
    await verifyConversationResize(primary.id);
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
  if (process.env.PUDDING_SMOKE_SCENARIO === "project-empty") {
    await verifyProjectEmpty(primary.id, projectRoot);
    assert.deepEqual(rendererErrors, [], "renderer identity/layout errors");
    return;
  }
  if (process.env.PUDDING_SMOKE_SCENARIO === "rail-divider") {
    await verifyRailDivider();
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
      window.setContentSize(620, 680);
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
  check("three locales × two themes at 620px/125% zoom: navigation and native webpage stay reachable");

  phase = "browser release and memory";
  for (const tab of browserTabs) await api(`/sessions/${primary.id}/browser/tabs/${tab.id}/release`, "POST");
  await waitFor(() => webContents.getAllWebContents().every(c => c.getType() !== "webview"), "destroy closed webviews");
  await measureMemory("after-close-all");
  const replacement = await api(`/sessions/${primary.id}/browser/tabs`, "POST");
  await api(`/sessions/${primary.id}/browser/tabs/${replacement.id}/release`, "POST");
  assert.equal(baseline.guestCount, 0);
  check("all 20 guests destroyed on release; capacity reusable");
  await verifyLibrarySearch(primary.id, projectRoot, secondary.id);
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
    passed: !error, phase, checks, memory, rendererErrors, previewReveals, error: error?.stack,
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
