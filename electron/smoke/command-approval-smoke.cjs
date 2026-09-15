// Source Electron + real approval card/input. Disposable Vite/userData, no daemon or host commands.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const {pathToFileURL} = require("node:url");
const {app, BrowserWindow} = require("electron");
const repo = path.resolve(__dirname, "../..");
const output = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-command-approval-smoke-"));
app.setPath("userData", path.join(output, "user-data"));
let vite, window, exitCode = 0;
const errors = [];
const js = source => window.webContents.executeJavaScript(source, true);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(source) {
  const until = Date.now() + 15000;
  while (Date.now() < until) {
    assert.deepEqual(errors, []);
    if (await js(source)) return;
    await delay(30);
  }
  throw new Error("Timed out: " + source);
}
async function clickOption(index) {
  const point = await js(`(()=>{const e=document.querySelectorAll('[role=option]')[${index}];e.scrollIntoView({block:'nearest'});const r=e.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  for (const type of ["mouseMove", "mouseDown", "mouseUp"]) window.webContents.sendInputEvent({type, ...point, button:"left",clickCount:1});
}
async function run() {
  assert.equal(process.execPath, path.join(repo, "web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"));
  const {createServer} = await import(pathToFileURL(path.join(repo, "web/node_modules/vite/dist/node/index.js")));
  vite = await createServer({
    root:path.join(repo,"web"),cacheDir:path.join(output,"vite-cache"),logLevel:"error",
    server:{host:"127.0.0.1",port:0},
    plugins:[{name:"command-approval-fixture",
      resolveId(id){if(id==="/__approval.js")return "\0approval-fixture";},
      load(id){if(id!=="\0approval-fixture")return;return `
        import React from 'react';import {createRoot} from 'react-dom/client';
        import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
        import {ComposerApprovalBar} from '/src/components/ComposerApprovalBar.tsx';
        import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
        import {setLocale} from '/src/i18n/index.ts';import '/src/styles.css';
        const h=React.createElement,root=createRoot(document.getElementById('root')),client=new QueryClient();
        const session={id:'s',title:'test',provider:'mock',model:'mock',activeMode:'code',modeLease:'session',pinned:false,pinnedOrder:0,createdAt:'',updatedAt:'',lastActivityAt:'',running:true,backgroundProcessCount:0};
        window.fixture={requests:[],show(bounded=true){
          this.requests=[];setLocale('zh-CN');
          const approval={type:'approval',approvalID:bounded?'bounded':'ordinary',approvalKind:'tool_call',sessionID:'s',payload:{toolName:'builtin_command_run',operation:'shell',execution:'host',hostAccessReason:'渲染项目内预览页面',command:'Chrome --headless=new --user-data-dir=.preview-profile --screenshot=preview.png page.html',
            ...(bounded?{sessionGrant:{kind:'chrome_headless_screenshot',executable:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',cwd:'/project',inputPath:'/project/page.html',outputDirectory:'/project',profilePath:'/project/.preview-profile'}}:{})}};
          root.render(h(QueryClientProvider,{client},h(TooltipProvider,null,h('div',{style:{position:'relative',height:0,margin:'620px 24px 0'}},h(ComposerApprovalBar,{key:approval.approvalID,approval,token:'fixture'})))));
        }};
        window.fetch=async(url,init)=>{window.fixture.requests.push({url:String(url),body:JSON.parse(init.body)});return Response.json({status:String(url).endsWith('/deny')?'denied':'approved',session});};
        window.addEventListener('unhandledrejection',e=>console.error(String(e.reason)));window.fixture.show();
      `;},
      configureServer(server){server.middlewares.use(async(req,res,next)=>{
        if(new URL(req.url,'http://localhost').pathname!=='/')return next();
        try{res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/', '<!doctype html><html><head><title>Command approval regression</title></head><body><div id="root"></div><script type="module" src="/__approval.js"></script></body></html>'));}catch(e){next(e);}
      });}
    }],
  });
  await vite.listen();
  const url = `http://127.0.0.1:${vite.httpServer.address().port}`;
  window=new BrowserWindow({width:760,height:720,webPreferences:{contextIsolation:true,nodeIntegration:false}});
  window.webContents.on('console-message',details=>{if(details.level==='error')errors.push(details.message);});
  await window.loadURL(url);
  assert.equal(new URL(window.webContents.getURL()).origin,url);
  await waitFor("document.querySelectorAll('[role=option]').length===3");
  assert.ok(await js("document.body.textContent.includes('重启后失效')"));
  await clickOption(1);
  await waitFor("window.fixture.requests.length===1 && !document.querySelector('[aria-busy=true]')");
  assert.deepEqual(await js("window.fixture.requests[0]"), {url:'/sessions/s/approvals/bounded/approve',body:{scope:'session',projectDirs:[]}});
  await clickOption(0);
  await waitFor("window.fixture.requests.length===2 && !document.querySelector('[aria-busy=true]')");
  assert.equal(await js("window.fixture.requests[1].body.scope"),'turn');
  fs.writeFileSync(path.join(output,'bounded.png'),(await window.webContents.capturePage()).toPNG());
  await js("window.fixture.show(false)");
  await waitFor("document.querySelectorAll('[role=option]').length===2");
  assert.equal(await js("document.body.textContent.includes('本会话允许同类截图')"),false);
  await clickOption(1);
  await waitFor("window.fixture.requests.length===1 && !document.querySelector('[aria-busy=true]')");
  assert.equal(await js("window.fixture.requests[0].url"),'/sessions/s/approvals/ordinary/deny');
  assert.deepEqual(errors,[]);
  console.log('PASS session / once / deny use session-scoped API; ordinary commands have no reusable option');
  console.log('REPORT '+output);
}
const deadline=setTimeout(()=>{console.error('Smoke timed out');app.exit(1);},60000);
app.whenReady().then(run).catch(async error=>{
  console.error(error);exitCode=1;
  if(window){
    console.error(await js("({requests:window.fixture?.requests,options:[...document.querySelectorAll('[role=option]')].map(e=>({text:e.textContent,rect:e.getBoundingClientRect().toJSON()})),busy:document.querySelector('[aria-busy=true]')?.outerHTML,focus:document.activeElement?.outerHTML})"));
    fs.writeFileSync(path.join(output,'failure.png'),(await window.webContents.capturePage()).toPNG());
    console.error('REPORT '+output);
  }
}).finally(async()=>{clearTimeout(deadline);await vite?.close();app.exit(exitCode);});
