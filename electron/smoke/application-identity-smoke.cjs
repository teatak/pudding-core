// Source Electron + real preload/IPC/Helper + production approval card.
// Only reads installed app metadata. Disposable daemon/userData; no TCC prompts or input to other apps.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const http = require("node:http");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow, ipcMain } = require("electron");

const repo = path.resolve(__dirname, "../..");
const output = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-identity-smoke-"));
process.env.PUDDING_HOME = path.join(output, "home");
process.env.PUDDING_ELECTRON_USER_DATA_DIR = path.join(output, "user-data");
process.env.PUDDING_DAEMON_BIN = path.join(repo, "bin/puddingd");
process.env.PUDDING_COMPUTER_USE_HELPER_BIN = path.join(repo, "bin/computer-use-helper-build/debug/PuddingComputerUseHelper");
process.env.PUDDING_LOCALE = "zh-CN";
delete process.env.PUDDING_API_BASE;
let vite, window, exitCode = 0, holdEnglish = false, releaseEnglish;
const calls = [], errors = [], checks = [];
const js = (source) => window.webContents.executeJavaScript(source, true);
const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms));
const handle = ipcMain.handle.bind(ipcMain);
ipcMain.handle = (channel, listener) => handle(channel, channel !== "pudding:desktop:application-identity" ? listener : async (...args) => {
  calls.push({ appID: args[1], locale: args[2] });
  const identity = await listener(...args);
  // Hold only the reply, after real native resolution, to exercise a late old-language response.
  if (holdEnglish && args[2] === "en") await new Promise(resolve => { releaseEnglish = resolve; });
  return identity;
});
async function waitFor(predicate, label) {
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    assert.deepEqual(errors, []);
    if (await predicate()) return;
    await delay(30);
  }
  throw new Error("Timed out: " + label);
}
async function clickLocale(locale) {
  const point = await js(`(()=>{const r=document.querySelector('[data-locale="${locale}"]').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  for (const type of ["mouseDown", "mouseUp"]) window.webContents.sendInputEvent({type, ...point, button:"left", clickCount:1});
}
async function expectName(name) {
  await waitFor(() => js(`document.querySelector('[title="com.apple.calculator"]')?.textContent === ${JSON.stringify(name)}`), name);
  assert.ok(await js(`Boolean(document.querySelector('.pudding-composer-floating-panel img')?.naturalWidth)`));
}
function check(text) { checks.push(text); console.log("PASS " + text); }

async function run() {
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  assert.ok(fs.existsSync(process.env.PUDDING_COMPUTER_USE_HELPER_BIN), "build the current source Helper first");
  const reservation = http.createServer();
  await new Promise(resolve => reservation.listen(0, "127.0.0.1", resolve));
  process.env.PUDDING_DAEMON_ADDR = `127.0.0.1:${reservation.address().port}`;
  await new Promise(resolve => reservation.close(resolve));
  const { createServer } = await import(pathToFileURL(path.join(repo, "web/node_modules/vite/dist/node/index.js")));
  vite = await createServer({
    root: path.join(repo, "web"), cacheDir: path.join(output, "vite-cache"), logLevel: "error",
    server: {host: "127.0.0.1", port: 0},
    plugins: [{
      name: "application-identity-fixture",
      resolveId(id) { if (id === "/__identity.js") return "\0identity-fixture"; },
      load(id) {
        if (id !== "\0identity-fixture") return;
        return `
          import React from 'react';
          import {createRoot} from 'react-dom/client';
          import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
          import {ComposerApprovalBar} from '/src/components/ComposerApprovalBar.tsx';
          import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
          import {setLocale} from '/src/i18n/index.ts';
          import '/src/styles.css';
          const client=new QueryClient(), h=React.createElement;
          const approval={type:'approval',approvalID:'unchanged-approval',approvalKind:'tool_call',
            sessionID:'identity-fixture',payload:{scope:'computer',appID:'com.apple.calculator'}};
          setLocale('zh-CN');
          window.fixture={clearEnglish:()=>client.removeQueries({queryKey:['desktop','application-identity','com.apple.calculator','en']}),
            keys:()=>client.getQueryCache().getAll().map(q=>q.queryKey),approval};
          createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,
            h('div',{style:{padding:32}},['zh-CN','zh-TW','en'].map(locale=>h('button',{
              key:locale,'data-locale':locale,onClick:()=>setLocale(locale),style:{marginRight:20,padding:10,border:'1px solid #ccc'}},locale))),
            h('div',{style:{position:'relative',marginTop:60}},h(ComposerApprovalBar,{approval,preview:true,token:''})))));
        `;
      },
      configureServer(server) {
        server.middlewares.use(async (req, res, next) => {
          if (new URL(req.url, "http://localhost").pathname !== "/") return next();
          try {
            res.setHeader("Content-Type", "text/html");
            res.end(await server.transformIndexHtml("/",
              '<!doctype html><html><head><title>Application name regression</title></head><body><div id="root"></div><script type="module" src="/__identity.js"></script></body></html>'));
          } catch (error) { next(error); }
        });
      },
    }],
  });
  await vite.listen();
  process.env.PUDDING_DEV_URL = `http://127.0.0.1:${vite.httpServer.address().port}`;
  require("../main.cjs");
  await waitFor(() => {
    window = BrowserWindow.getAllWindows().find(w => !w.isDestroyed() && !w.webContents.isLoadingMainFrame()
      && w.webContents.getURL().startsWith(process.env.PUDDING_DEV_URL));
    return Boolean(window);
  }, "isolated source window");
  window.webContents.setBackgroundThrottling(false);
  window.webContents.on("console-message", details => { if (details.level === "error") errors.push(details.message); });
  assert.equal(new URL(window.webContents.getURL()).origin, new URL(process.env.PUDDING_DEV_URL).origin);
  await expectName("计算器");
  check("initial Chinese card reads the installed Calculator name through real preload/IPC/Helper");
  await clickLocale("en"); await expectName("Calculator");
  await clickLocale("zh-TW"); await expectName("計算機");
  await clickLocale("zh-CN"); await expectName("计算器");
  assert.deepEqual(calls.map(c => c.locale), ["zh-CN", "en", "zh-TW"]);
  assert.ok(calls.every(c => c.appID === "com.apple.calculator"));
  assert.equal(await js("window.fixture.approval.approvalID"), "unchanged-approval");
  check("Chinese/English/Traditional switching reuses language-scoped cache and preserves approval identity");

  await js("window.fixture.clearEnglish()");
  holdEnglish = true;
  await clickLocale("en");
  await waitFor(() => Boolean(releaseEnglish), "held English reply");
  await clickLocale("zh-CN"); await expectName("计算器");
  holdEnglish = false; releaseEnglish();
  await delay(200);
  await expectName("计算器");
  await clickLocale("en"); await expectName("Calculator");
  assert.equal(calls.length, 4);
  check("late English reply cannot overwrite Chinese; resolved English remains reusable");
  await clickLocale("zh-CN"); await expectName("计算器");
  fs.writeFileSync(path.join(output, "chinese.png"), (await window.webContents.capturePage()).toPNG());
  fs.writeFileSync(path.join(output, "result.json"), JSON.stringify({checks,calls,errors,sourceURL:process.env.PUDDING_DEV_URL}, null, 2));
  console.log("REPORT " + output);
}
let timeout;
Promise.race([run(), new Promise((_, reject) => {
  timeout = setTimeout(() => reject(new Error("identity smoke timed out")), 90_000);
})]).catch(error => { console.error(error); exitCode = 1; }).finally(async () => {
  releaseEnglish?.();
  clearTimeout(timeout);
  await vite?.close();
  // main.cjs owns and stops only this disposable daemon and Helper on quit.
  app.on("will-quit", () => { if (exitCode) app.exit(exitCode); });
  app.quit();
});
