const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow } = require("electron");

const webRoot = path.resolve(__dirname, "../../web");
const smokeHome = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-reasoning-smoke-"));
app.setPath("userData", path.join(smokeHome, "user-data"));
app.disableHardwareAcceleration();

let window;
let server;
let finishing = false;
let currentCheck = "startup";
const timeout = setTimeout(() => finish(new Error(`Timed out during ${currentCheck}`)), 90_000);
void app.whenReady().then(run).then(() => finish()).catch(finish);

async function run() {
  const { createServer } = await import(pathToFileURL(require.resolve("vite", { paths: [webRoot] })).href);
  server = await createServer({
    root: webRoot,
    configFile: path.join(webRoot, "vite.config.ts"),
    cacheDir: path.join(smokeHome, "vite-cache"),
    server: { host: "127.0.0.1", port: 0, watch: null, hmr: false, ws: false },
  });
  await server.listen();
  const address = server.httpServer.address();
  assert.ok(address && typeof address === "object");
  const baseURL = `http://127.0.0.1:${address.port}`;
  window = new BrowserWindow({
    width: 920,
    height: 700,
    show: true,
    webPreferences: { contextIsolation: true, nodeIntegration: false, backgroundThrottling: false },
  });
  window.webContents.on("render-process-gone", (_event, details) => {
    void finish(new Error(`Renderer exited: ${details.reason}`));
  });
  window.focus();

  const loadFixture = async (mode) => {
    await window.loadURL(`${baseURL}/test/fixtures/model-reasoning.html?mode=${mode}`);
    await waitFor(`new URL(location.href).searchParams.get("mode") === ${JSON.stringify(mode)} && Boolean(window.modelReasoningSmoke && document.querySelector(".pudding-composer-model-picker"))`);
    app.focus({ steal: true });
    window.focus();
    window.webContents.focus();
    await evaluate('document.querySelector("#composer-a").focus()');
    await waitFor('document.hasFocus() && document.activeElement?.id === "composer-a"');
    await frames();
  };

  currentCheck = "draft model preferences";
  await loadFixture("draft");
  await pickGoogleModel();
  await waitFor(`window.modelReasoningSmoke.snapshot().selection.model === "gemini-3.1-pro-preview"`);
  await frames();
  let snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.preferences["anthropic-smoke:claude-opus-4-6"], "max");
  assert.equal(snapshot.preferences["google-smoke:gemini-3.1-pro-preview"], "low");
  assert.deepEqual(snapshot.reasoningChanges, [
    { modelKey: "google-smoke:gemini-3.1-pro-preview", effort: "low" },
  ]);
  assert.equal(snapshot.patches.length, 0);
  console.log("PASS draft model switch preserves the previous model and uses the target preference");

  currentCheck = "session model transaction";
  await loadFixture("session");
  await pickGoogleModel();
  await waitFor(`window.modelReasoningSmoke.snapshot().selection.model === "gemini-3.1-pro-preview"`);
  await frames();
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.deepEqual(snapshot.patches, [
    { provider: "google-smoke", model: "gemini-3.1-pro-preview", reasoningEffort: "low" },
  ]);
  assert.deepEqual(snapshot.reasoningChanges, []);
  assert.equal(snapshot.preferences["anthropic-smoke:claude-opus-4-6"], "max");
  console.log("PASS session model switch sends one model-and-effort PATCH");

  currentCheck = "queued settings updates";
  await loadFixture("session");
  await evaluate(`(() => {
    const smoke = window.modelReasoningSmoke;
    smoke.respond({ hold: true });
    void smoke.update({ provider: "anthropic-smoke", model: "claude-opus-4-6", reasoningEffort: "high" });
    void smoke.update({ provider: "google-smoke", model: "gemini-3.1-pro-preview", reasoningEffort: "low" });
    return true;
  })()`);
  await waitFor("window.modelReasoningSmoke.snapshot().heldResponses === 1 && window.modelReasoningSmoke.snapshot().pending");
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.patches.length, 1);
  assert.equal(snapshot.session.reasoningEffort, "max");
  assert.equal(snapshot.preferences["anthropic-smoke:claude-opus-4-6"], "max");
  await evaluate("window.modelReasoningSmoke.release()");
  await waitFor("window.modelReasoningSmoke.snapshot().patches.length === 2 && window.modelReasoningSmoke.snapshot().heldResponses === 1");
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.session.model, "claude-opus-4-6");
  assert.equal(snapshot.session.reasoningEffort, "high");
  await evaluate("window.modelReasoningSmoke.release()");
  await waitFor("!window.modelReasoningSmoke.snapshot().pending");
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.session.model, "gemini-3.1-pro-preview");
  assert.equal(snapshot.session.reasoningEffort, "low");
  console.log("PASS queued settings updates preserve canonical values until each request succeeds");

  currentCheck = "failed settings retry";
  await loadFixture("session");
  const retryPatch = { provider: "google-smoke", model: "gemini-3.1-pro-preview", reasoningEffort: "high" };
  await evaluate("window.modelReasoningSmoke.respond({ failNext: true })");
  await evaluate(`window.modelReasoningSmoke.update(${JSON.stringify(retryPatch)})`);
  await frames();
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.pending, false);
  assert.equal(snapshot.session.model, "claude-opus-4-6");
  assert.equal(snapshot.session.reasoningEffort, "max");
  assert.equal(snapshot.preferences["google-smoke:gemini-3.1-pro-preview"], "low");
  assert.ok(snapshot.lastError);
  await evaluate(`window.modelReasoningSmoke.update(${JSON.stringify(retryPatch)})`);
  await frames();
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.equal(snapshot.pending, false);
  assert.equal(snapshot.session.model, "gemini-3.1-pro-preview");
  assert.equal(snapshot.session.reasoningEffort, "high");
  assert.equal(snapshot.preferences["google-smoke:gemini-3.1-pro-preview"], "high");
  assert.deepEqual(snapshot.patches, [retryPatch, retryPatch]);
  console.log("PASS a failed update preserves model and effort and allows an explicit retry");

  currentCheck = "Escape focus";
  await loadFixture("draft");
  await clickSelector("#composer-a");
  await clickSelector(".pudding-composer-model-picker");
  await waitFor(`document.querySelector(".pudding-composer-model-picker")?.getAttribute("aria-expanded") === "true"`);
  window.webContents.sendInputEvent({ type: "keyDown", keyCode: "Escape" });
  window.webContents.sendInputEvent({ type: "keyUp", keyCode: "Escape" });
  await waitFor(`document.activeElement?.id === "composer-a" && document.querySelector(".pudding-composer-model-picker")?.getAttribute("aria-expanded") === "false"`);
  console.log("PASS Escape returns focus to the originating composer");

  currentCheck = "outside focus";
  await clickSelector(".pudding-composer-model-picker");
  await waitFor(`document.querySelector(".pudding-composer-model-picker")?.getAttribute("aria-expanded") === "true"`);
  await clickSelector("#composer-b");
  await waitFor(`document.querySelector(".pudding-composer-model-picker")?.getAttribute("aria-expanded") === "false"`);
  await frames();
  assert.equal(await evaluate("document.activeElement?.id"), "composer-b");
  await window.webContents.insertText("outside click");
  assert.equal(await evaluate('document.querySelector("#composer-b").value'), "outside click");
  assert.equal(await evaluate('document.querySelector("#composer-a").value'), "");
  console.log("PASS clicking another composer keeps focus and text in that composer");

  currentCheck = "unsupported reasoning model";
  await loadFixture("haiku");
  await clickSelector(".pudding-composer-model-picker");
  await waitFor(`Boolean(document.querySelector("[data-app-floating-content]"))`);
  const unsupportedSnapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.deepEqual(unsupportedSnapshot.reasoningChanges, []);
  assert.deepEqual(unsupportedSnapshot.patches, []);
  assert.equal(await evaluate('Boolean(document.querySelector("[data-app-floating-content] button[aria-label=Medium]"))'), false);
  assert.equal(await evaluate('Boolean(document.querySelector(".pudding-composer-reasoning-detail"))'), false);
  console.log("PASS Haiku has no reasoning slider and opening it does not persist an effort");

  currentCheck = "stop active audio while model settings are pending";
  await loadFixture("audio-active");
  assert.equal(await evaluate('document.querySelector("#audio-controls button").disabled'), false);
  await clickSelector("#audio-controls button");
  await waitFor("window.modelReasoningSmoke.snapshot().audioRequests.length === 1 && window.modelReasoningSmoke.snapshot().audioBindings.inputOwner === ''");
  await frames();
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.deepEqual(snapshot.audioRequests, [{ enabled: false, mode: "transcribe" }]);
  assert.equal(snapshot.audioBindings.inputMode, "");
  assert.equal(await evaluate('document.querySelector("#audio-controls button").disabled'), true);
  console.log("PASS an active microphone can stop while model settings block new input");

  currentCheck = "block idle audio while model settings are pending";
  await loadFixture("audio-idle");
  assert.equal(await evaluate('document.querySelector("#audio-controls button").disabled'), true);
  await clickSelector("#audio-controls button", { allowDisabled: true });
  snapshot = await evaluate("window.modelReasoningSmoke.snapshot()");
  assert.deepEqual(snapshot.audioRequests, []);
  assert.equal(snapshot.audioBindings.inputOwner, "");
  console.log("PASS an idle microphone cannot start until model settings are ready");
}

async function pickGoogleModel() {
  await clickSelector(".pudding-composer-model-picker");
  await clickButton("Opus smoke");
  await clickButton("Google smoke");
  await clickButton("Gemini smoke");
}

async function clickSelector(selector, options) {
  return clickElement(`document.querySelector(${JSON.stringify(selector)})`, options);
}

async function clickButton(text) {
  return clickElement(`Array.from(document.querySelectorAll("[data-app-floating-content] button")).find((button) => button.textContent.trim() === ${JSON.stringify(text)} || button.getAttribute("aria-label") === ${JSON.stringify(text)} || button.getAttribute("title") === ${JSON.stringify(text)})`);
}

async function clickElement(expression, { allowDisabled = false } = {}) {
  let point = await actionablePoint(expression, allowDisabled);
  window.webContents.sendInputEvent({ type: "mouseMove", ...point });
  // Hover may change styles. Recheck actionability without repeating a click.
  point = await actionablePoint(expression, allowDisabled);
  window.webContents.sendInputEvent({ type: "mouseDown", button: "left", clickCount: 1, ...point });
  window.webContents.sendInputEvent({ type: "mouseUp", button: "left", clickCount: 1, ...point });
  await frames();
}

function actionablePoint(expression, allowDisabled) {
  // Radix mounts content before positioning it, and the popover then animates
  // for 100 ms. DOM presence or two elapsed frames cannot establish a hit target.
  return evaluate(`(async () => {
    const deadline = Date.now() + 10_000;
    let previousElement;
    let previousBounds;
    let stableFrames = 0;
    while (Date.now() < deadline) {
      const element = ${expression};
      let reason = "missing element";
      let bounds;
      let point;
      if (element?.isConnected) {
        const rect = element.getBoundingClientRect();
        bounds = [rect.x, rect.y, rect.width, rect.height];
        point = { x: Math.round(rect.x + rect.width / 2), y: Math.round(rect.y + rect.height / 2) };
        const visible = element.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) && rect.width > 0 && rect.height > 0;
        const disabled = element.matches(":disabled") || element.getAttribute("aria-disabled") === "true" || Boolean(element.closest("[inert]"));
        const hit = document.elementFromPoint(point.x, point.y);
        const receivesPointer = hit && (element.contains(hit) || (${allowDisabled} && disabled && hit.contains(element)));
        const animatedElements = new Set(document.querySelectorAll("[data-app-floating-content]"));
        for (let ancestor = element; ancestor; ancestor = ancestor.parentElement) animatedElements.add(ancestor);
        const moving = Array.from(animatedElements).some((target) => target.getAnimations().some((animation) =>
          (animation.pending || animation.playState === "running") && animation.effect?.getComputedTiming().endTime !== Infinity));
        reason = !visible ? "not visible"
          : disabled && !${allowDisabled} ? "disabled"
          : point.x < 0 || point.y < 0 || point.x >= innerWidth || point.y >= innerHeight ? "outside viewport"
          : !receivesPointer ? "center is covered"
          : moving ? "animation running"
          : "waiting for stable bounds";
        const unchanged = previousElement === element && previousBounds && bounds.every((value, index) => Math.abs(value - previousBounds[index]) < 0.1);
        stableFrames = visible && (!disabled || ${allowDisabled}) && receivesPointer && !moving && unchanged ? stableFrames + 1 : 0;
        if (stableFrames >= 2) {
          window.modelReasoningSmokeLastAction = { expression: ${JSON.stringify(expression)}, bounds, point, reason: "ready" };
          return point;
        }
      } else {
        stableFrames = 0;
      }
      window.modelReasoningSmokeLastAction = { expression: ${JSON.stringify(expression)}, bounds, point, reason };
      previousElement = element;
      previousBounds = bounds;
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
    throw new Error("Actionability timeout: " + JSON.stringify(window.modelReasoningSmokeLastAction));
  })()`);
}

function evaluate(code) {
  return window.webContents.executeJavaScript(code);
}

function frames() {
  return evaluate("new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true))))");
}

async function waitFor(expression) {
  const deadline = Date.now() + 10_000;
  do {
    if (await evaluate(expression)) return;
    await new Promise((resolve) => setTimeout(resolve, 30));
  } while (Date.now() < deadline);
  throw new Error(`Timed out during ${currentCheck}: ${expression}`);
}

async function finish(error) {
  if (finishing) return;
  finishing = true;
  clearTimeout(timeout);
  if (error) {
    console.error(error);
    if (window && !window.webContents.isDestroyed()) {
      let diagnosticTimeout;
      try {
        const diagnosticsRequest = evaluate(`(() => {
          const describe = (element) => {
            if (!element) return null;
            const rect = element.getBoundingClientRect();
            const style = getComputedStyle(element);
            return { tag: element.tagName, id: element.id, text: element.textContent?.trim().slice(0, 160), label: element.getAttribute("aria-label"), state: element.getAttribute("data-state"), expanded: element.getAttribute("aria-expanded"), disabled: element.matches(":disabled"), bounds: [rect.x, rect.y, rect.width, rect.height], opacity: style.opacity, transform: style.transform };
          };
          return {
            url: location.href,
            focused: document.hasFocus(),
            activeElement: describe(document.activeElement),
            lastAction: window.modelReasoningSmokeLastAction,
            state: window.modelReasoningSmoke?.snapshot(),
            buttons: Array.from(document.querySelectorAll("button")).map(describe),
            popovers: Array.from(document.querySelectorAll("[data-app-floating-content]")).map(describe),
          };
        })()`);
        const diagnostics = await Promise.race([
          diagnosticsRequest,
          new Promise((_resolve, reject) => {
            diagnosticTimeout = setTimeout(() => reject(new Error("Smoke diagnostics timed out")), 2_000);
          }),
        ]);
        console.error("SMOKE_DIAGNOSTICS", JSON.stringify({ currentCheck, windowFocused: window.isFocused(), ...diagnostics }));
      } catch (diagnosticError) {
        console.error("Could not capture smoke diagnostics", diagnosticError);
      } finally {
        clearTimeout(diagnosticTimeout);
      }
    }
  }
  window?.destroy();
  await server?.close();
  fs.rmSync(smokeHome, { recursive: true, force: true });
  app.exit(error ? 1 : 0);
}
