// Source Electron/Vite fixture; no daemon, accounts or real app mutations.
// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/session-apps-smoke.cjs
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow } = require("electron");
const repo = path.resolve(__dirname, "../..");
const home = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-session-apps-"));
app.setPath("userData", path.join(home, "user-data"));
let vite, window;
const js = (source) => window.webContents.executeJavaScript(source, true);
const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms));

async function run() {
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  const { createServer } = await import(pathToFileURL(path.join(repo, "web/node_modules/vite/dist/node/index.js")));
  vite = await createServer({
    root: path.join(repo, "web"), cacheDir: path.join(home, "vite-cache"), logLevel: "error",
    server: { host: "127.0.0.1", port: 0 },
    plugins: [{
      name: "session-apps-fixture",
      resolveId(id) { if (id === "/__session_apps.js") return "\0session-apps-fixture"; },
      load(id) {
        if (id !== "\0session-apps-fixture") return;
        return `
          import React from 'react'; import {createRoot} from 'react-dom/client';
          import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
          import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
          import {SessionAppsControl} from '/src/components/SessionAppsControl.tsx';
          import '/src/styles.css';
          const root=createRoot(document.getElementById('root')), client=new QueryClient();
          window.renderApps=(count,width)=>{
            document.activeElement?.blur();
            document.getElementById('pane').style.width=width+'px';
            root.render(React.createElement(QueryClientProvider,{client},React.createElement(TooltipProvider,null,
              React.createElement(SessionAppsControl,{token:'',session:{id:'fixture',loadedAppIDs:Array.from({length:count},(_,i)=>'app-'+i)}}))));
          };
        `;
      },
      configureServer(server) {
        server.middlewares.use("/__session_apps.html", async (_req, res) => {
          res.setHeader("Content-Type", "text/html");
          res.end(await server.transformIndexHtml("/__session_apps.html", '<html class="dark"><body style="margin:40px"><div id="pane" style="container:chat-pane / inline-size"><div id="root"></div></div><script type="module" src="/__session_apps.js"></script></body></html>'));
        });
      },
    }],
  });
  await vite.listen();
  await app.whenReady();
  window = new BrowserWindow({width: 960, height: 480, show: false, webPreferences: {backgroundThrottling: false}});
  window.webContents.on("console-message", details => {
    if (details.level === "error" || details.level === 3) console.error(details.message);
  });
  const origin = `http://127.0.0.1:${vite.httpServer.address().port}`;
  await window.loadURL(origin + "/__session_apps.html");
  assert.equal(new URL(window.webContents.getURL()).origin, origin);
  for (let i = 0; i < 100 && !(await js("typeof window.renderApps === 'function'")); i++) await delay(50);
  assert.equal(await js("typeof window.renderApps"), "function", "production fixture loaded");
  for (const width of [400, 800]) {
    for (const count of [0, 1, 2, 3, 6, 7]) {
      await js(`window.renderApps(${count},${width})`);
      await delay(180);
      const state = await js(`(() => {
        const visible=el=>el.getBoundingClientRect().width>0;
        const icons=[...document.querySelectorAll('.pudding-session-apps-expanded')].filter(visible);
        const trigger=document.querySelector('.pudding-session-apps-trigger');
        const shown=trigger&&visible(trigger);
        const boxes=[...icons,...(shown?[trigger]:[])].map(el=>el.getBoundingClientRect());
        return {icons:icons.length, count:shown?[...trigger.children].filter(visible).map(el=>el.textContent).join(''):'',
          stacked:boxes.every((box,i)=>i===0||box.left<boxes[i-1].right)};
      })()`);
      const shown = width === 400 ? (count <= 2 ? count : 1) : (count <= 6 ? count : 5);
      assert.deepEqual(state, {icons: shown, count: count > shown ? '+' + (count-shown) : '', stacked: true}, `${width}px / ${count} apps`);
    }
  }
  await js("window.renderApps(3,400)");
  await delay(180);
  const point = await js(`(() => {const r=document.querySelector('.pudding-session-apps-trigger').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()`);
  for (const type of ["mouseDown", "mouseUp"]) window.webContents.sendInputEvent({type, ...point, button:"left", clickCount:1});
  await delay(180);
  assert.equal(await js("document.querySelectorAll('[role=dialog] button').length"), 3, "overflow popover exposes all loaded apps");
  console.log("PASS: narrow/wide 0, 1, 2, 3, 6, 7 apps; stacked icons/count; native overflow click");
}
void run().then(() => finish(0), error => {console.error(error); return finish(1);});
async function finish(code) {
  window?.destroy();
  await vite?.close();
  fs.rmSync(home, {recursive:true, force:true});
  app.exit(code);
}
