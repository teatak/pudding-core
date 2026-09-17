const assert = require("node:assert/strict");
const fs = require("node:fs");
const { app, BrowserWindow } = require("electron");

const [fixtureURL, userData, expectedBinary] = process.argv.slice(2);
assert.equal(fs.realpathSync(process.execPath), fs.realpathSync(expectedBinary));
const origin = new URL(fixtureURL).origin;
assert.equal(new URL(fixtureURL).hostname, "127.0.0.1");
fs.mkdirSync(userData, { recursive: true });
app.setPath("userData", userData);
app.setName("Pudding selector regression");
app.disableHardwareAcceleration();

let window;
const errors = [];
const popover = '[data-app-floating-content]';
const slider = '[data-slot="slider"]';
const thumb = '[role="slider"]';
const trigger = 'button[aria-haspopup="dialog"]';
const js = (source) => window.webContents.executeJavaScript(source, true);
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const state = () => js("window.pickerFixture.state()");
const element = (selector) => `document.querySelector(${JSON.stringify(selector)})`;
const button = (label) => `[...document.querySelectorAll('${popover} button')].find((item) => item.textContent.trim() === ${JSON.stringify(label)})`;

async function waitFor(source, label) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    assert.deepEqual(errors, [], "renderer/network errors");
    if (await js(source)) return;
    await delay(25);
  }
  throw new Error("Timed out: " + label);
}

async function settle() {
  await js("new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
  await waitFor(`!document.querySelector('${popover}')?.getAnimations().some((animation) => animation.playState === "running")`, "popover animation");
}

async function pointFor(expression) {
  const point = await js(`(() => {
    const node = ${expression};
    if (!node) throw new Error("Missing interaction target");
    const rect = node.getBoundingClientRect();
    return { x: Math.round(rect.left + rect.width / 2), y: Math.round(rect.top + rect.height / 2),
      width: rect.width, height: rect.height, viewportWidth: innerWidth, viewportHeight: innerHeight };
  })()`);
  assert.ok(point.width > 0 && point.height > 0, "interaction target has a layout box");
  assert.ok(point.x > 0 && point.x < point.viewportWidth && point.y > 0 && point.y < point.viewportHeight,
    "interaction target is in the window");
  return { x: point.x, y: point.y };
}

function mouse(type, point, extra = {}) {
  window.webContents.sendInputEvent({ type, ...point, ...extra });
}

async function click(expression) {
  const point = await pointFor(expression);
  mouse("mouseMove", point);
  mouse("mouseDown", point, { button: "left", clickCount: 1 });
  mouse("mouseUp", point, { button: "left", clickCount: 1 });
  await settle();
}

async function key(keyCode) {
  window.webContents.sendInputEvent({ type: "keyDown", keyCode });
  window.webContents.sendInputEvent({ type: "keyUp", keyCode });
  await settle();
}

async function reset(reasoning = "") {
  await js(`window.pickerFixture.reset(${JSON.stringify(reasoning)})`);
  await settle();
  await waitFor(`!document.querySelector('${popover}') &&
    window.pickerFixture.state().reasoning === ${JSON.stringify(reasoning)} &&
    !window.pickerFixture.state().disabled`, "reset fixture");
}

async function openPicker() {
  await click(element(trigger));
  await waitFor(`Boolean(document.querySelector('${thumb}'))`, "reasoning slider");
  await settle();
}

async function showCatalog() {
  const current = await state();
  await click(button(current.modelLabel));
  await waitFor(`!document.querySelector('${thumb}') && Boolean(${button("Fixture OpenAI")})`, "model catalog");
  await settle();
}

async function beginDragToLast() {
  const start = await pointFor(element(thumb));
  const end = await js(`(() => {
    const rect = document.querySelector('${slider}').getBoundingClientRect();
    return { x: Math.round(rect.right - 2), y: Math.round(rect.top + rect.height / 2) };
  })()`);
  mouse("mouseMove", start);
  mouse("mouseDown", start, { button: "left", clickCount: 1 });
  mouse("mouseMove", end, { modifiers: ["leftButtonDown"] });
  await waitFor(`document.querySelector('${thumb}').getAttribute("aria-valuenow") ===
    document.querySelector('${thumb}').getAttribute("aria-valuemax")`, "drag preview");
  return end;
}

async function expectReasoning(value, commits) {
  await waitFor(`window.pickerFixture.state().reasoning === ${JSON.stringify(value)} &&
    window.pickerFixture.state().reasoningCommits.length === ${commits.length}`, "reasoning commit");
  assert.deepEqual((await state()).reasoningCommits, commits);
}

async function run() {
  window = new BrowserWindow({
    width: 840,
    height: 680,
    useContentSize: true,
    title: "Isolated model picker regression",
    show: true,
    webPreferences: { contextIsolation: true, nodeIntegration: false, backgroundThrottling: false },
  });
  window.webContents.on("console-message", (details) => {
    if (details.level === "error") errors.push(details.message);
  });
  window.webContents.on("render-process-gone", (_event, details) => {
    errors.push("renderer exited: " + details.reason);
  });
  window.webContents.session.webRequest.onBeforeRequest((details, callback) => {
    const target = new URL(details.url);
    const apiRequest = /^\/(providers|sessions|settings|projects)(\/|$)/.test(target.pathname);
    const blocked = !["data:", "blob:"].includes(target.protocol) &&
      (target.origin !== origin || apiRequest);
    if (blocked) errors.push("unexpected request: " + details.url);
    callback({ cancel: blocked });
  });
  await window.loadURL(fixtureURL);
  window.focus();
  window.webContents.focus();
  await waitFor("Boolean(window.pickerFixture) && document.hasFocus()", "focused fixture");
  assert.equal(await js("innerHeight"), 680, "the constrained-height test uses a known viewport");

  const scenarios = [
    ["inherited value, drag commit, and label commit", async () => {
      await reset();
      const label = await js(`${element(trigger)}.textContent`);
      assert.match(label, /Default \(Off\)/, "display the configured inherited effort");
      await openPicker();
      assert.equal(await js(`${element(thumb)}.getAttribute("aria-valuetext")`), "Default");
      const end = await beginDragToLast();
      assert.deepEqual((await state()).reasoningCommits, [], "dragging only previews");
      assert.equal((await state()).reasoning, "", "preview does not replace the authoritative value");
      mouse("mouseUp", end, { button: "left", clickCount: 1 });
      await expectReasoning("max", ["max"]);
      await click(button("Low"));
      await expectReasoning("low", ["max", "low"]);
      await click(button("Low"));
      assert.deepEqual((await state()).reasoningCommits, ["max", "low"], "active label does not save again");
    }],
    ["keyboard commits and subsequent external value", async () => {
      await reset("medium");
      await openPicker();
      await js(`${element(thumb)}.focus()`);
      await key("Right");
      await expectReasoning("high", ["high"]);
      await key("Home");
      await expectReasoning("", ["high", ""]);
      await key("End");
      await expectReasoning("max", ["high", "", "max"]);
      await key("Left");
      await expectReasoning("xhigh", ["high", "", "max", "xhigh"]);
      await js('window.pickerFixture.setReasoning("low")');
      await waitFor(`${element(thumb)}.getAttribute("aria-valuetext") === "Low"`, "external value replaces any keyboard preview");
      assert.deepEqual((await state()).reasoningCommits, ["high", "", "max", "xhigh"]);
    }],
    ["provider browsing, model selection, and Escape focus", async () => {
      await reset();
      await openPicker();
      await showCatalog();
      await click(button("Fixture Anthropic"));
      assert.deepEqual((await state()).modelCommits, [], "browsing a provider is not a model selection");
      assert.equal((await state()).selection.provider, "fixture-openai");
      await click(button("Anthropic Fixture 01"));
      await waitFor(`Boolean(document.querySelector('${thumb}'))`, "model selection returns to reasoning");
      assert.deepEqual((await state()).modelCommits, [{ provider: "fixture-anthropic", model: "anthropic-fixture-01" }]);
      assert.equal(await js(`${element(thumb)}.getAttribute("aria-valuetext")`), "Default", "model switch clears slider preview");
      await key("Escape");
      await waitFor(`!document.querySelector('${popover}') && document.activeElement?.id === "fixture-composer"`, "Escape restores composer focus");
    }],
    ["constrained catalog height and reachable last model", async () => {
      await reset();
      await openPicker();
      await showCatalog();
      const sample = await js(`(() => {
        const last = ${button("Openai Fixture 14")};
        let viewport = last.parentElement;
        while (viewport && !["auto", "scroll"].includes(getComputedStyle(viewport).overflowY)) viewport = viewport.parentElement;
        if (!viewport) throw new Error("Model catalog has no scroll container");
        window.catalogViewport = viewport;
        const rect = viewport.getBoundingClientRect();
        const panel = document.querySelector('${popover}').getBoundingClientRect();
        return { height: viewport.clientHeight, scrollHeight: viewport.scrollHeight,
          top: rect.top, bottom: rect.bottom, panelTop: panel.top, panelBottom: panel.bottom };
      })()`);
      assert.ok(sample.height > 0 && sample.height < 360, "the scrollport shrinks instead of being clipped");
      assert.ok(sample.scrollHeight > sample.height, "the fixture requires real scrolling");
      assert.ok(sample.top >= sample.panelTop - 1 && sample.bottom <= sample.panelBottom + 1, "scrollport fits inside the popover");
      await js("window.catalogViewport.scrollTop = window.catalogViewport.scrollHeight");
      await settle();
      const visible = await js(`(() => {
        const last = ${button("Openai Fixture 14")};
        const rect = last.getBoundingClientRect();
        const viewport = window.catalogViewport.getBoundingClientRect();
        const panel = document.querySelector('${popover}').getBoundingClientRect();
        return rect.top >= Math.max(viewport.top, panel.top) - 1 &&
          rect.bottom <= Math.min(viewport.bottom, panel.bottom) + 1;
      })()`);
      assert.equal(visible, true, "the last model is fully visible after scrolling");
      await click(button("Openai Fixture 14"));
      assert.equal((await state()).selection.model, "openai-fixture-14", "the last model is reachable by real mouse input");
    }],
    ["cancelled preview and disabled controls do not commit", async () => {
      await reset("low");
      await openPicker();
      let end = await beginDragToLast();
      await js(`${element(slider)}.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true }))`);
      mouse("mouseUp", end, { button: "left", clickCount: 1 });
      await waitFor(`${element(thumb)}.getAttribute("aria-valuetext") === "Low"`, "cancelled drag restores confirmed value");
      assert.deepEqual((await state()).reasoningCommits, []);

      end = await beginDragToLast();
      await js("window.pickerFixture.setDisabled(true)");
      await waitFor(`${element(slider)}.hasAttribute("data-disabled") &&
        ${element(thumb)}.getAttribute("aria-valuetext") === "Low"`, "disabling cancels a drag");
      mouse("mouseUp", end, { button: "left", clickCount: 1 });
      assert.equal(await js(`[...document.querySelectorAll('${popover} button[aria-pressed]')].every((item) => item.disabled)`), true);
      await click(button("Max"));
      await key("End");
      assert.deepEqual((await state()).reasoningCommits, [], "disabled pointer and keyboard input do not save");
      assert.equal((await state()).reasoning, "low");
    }],
  ];

  for (const [name, execute] of scenarios) {
    try {
      await execute();
      assert.deepEqual(errors, []);
      console.log("PASS " + name);
    } catch (error) {
      throw new Error(name, { cause: error });
    }
  }
  console.log("PASS 5 model picker scenarios");
}

app.whenReady().then(run).then(() => app.exit(0), (error) => {
  console.error(error.stack);
  if (error.cause) console.error(error.cause.stack || error.cause);
  app.exit(1);
});
