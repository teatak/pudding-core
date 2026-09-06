// Isolated local fixture: no network, no Pudding data, no forced AX enablement.
const {app, BrowserWindow, Menu, clipboard} = require('electron');
const fs = require('node:fs');
const path = require('node:path');
const dir = process.argv[2];
const label = process.argv[3];
app.setPath('userData', path.join(dir, label));
let win;
let clipboardBefore;
app.whenReady().then(async () => {
  win = new BrowserWindow({width:740,height:500,title:`CU ${label}`,webPreferences:{contextIsolation:true,nodeIntegration:false}});
  win.webContents.on('console-message', details=>{
    if(details.message.startsWith('CU_EVENT:')) fs.appendFileSync(path.join(dir,label+'.events.jsonl'),details.message.slice(9)+'\n');
  });
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    {label:'Fixture',submenu:[{role:'quit'}]},
    {label:'Edit',submenu:[{role:'undo'},{role:'redo'},{role:'cut'},{role:'copy'},{role:'paste'},{role:'selectAll'}]},
    {label:'Test',submenu:[{label:'Find',accelerator:'Command+F',click:()=>win.webContents.executeJavaScript("document.querySelector('dialog').showModal();document.querySelector('#find').focus()")}]},
  ]));
  const html = `<!doctype html><html><head><meta charset="utf-8"><title>CU ${label}</title></head>
  <body><h1>Computer Use isolated fixture</h1><label>Editor<input id="editor" aria-label="CU editor" value="initial"></label>
  <label>Next<input id="next" aria-label="CU next"></label><label>Password<input id="secret" type="password" aria-label="CU secure" value="fixture-secret"></label>
  <p id="status" aria-live="polite">ready</p><dialog><label>Find<input id="find" aria-label="CU find"></label></dialog>
  <script>window.events=[]; for(const type of ['input','keydown','compositionstart','compositionupdate','compositionend'])document.addEventListener(type,e=>{const item={type,key:e.key,data:e.data,composing:e.isComposing,trusted:e.isTrusted};events.push(item);console.log('CU_EVENT:'+JSON.stringify(item));});
  document.querySelector('dialog').addEventListener('close',()=>document.querySelector('#status').textContent='dialog closed');</script></body></html>`;
  await win.loadURL('data:text/html;charset=utf-8,'+encodeURIComponent(html));
  // Snapshot remains in this fixture process only, and is restored only if still our test value.
  clipboardBefore = clipboard.availableFormats().map(format=>[format,clipboard.readBuffer(format)]);
  fs.writeFileSync(path.join(dir,label+'.json'),JSON.stringify({pid:process.pid}));
});
app.on('before-quit',()=>{
  if (clipboard.readText()==='剪贴板🙂' && clipboardBefore) {
    clipboard.clear(); for(const [format,data] of clipboardBefore) clipboard.writeBuffer(format,data);
  }
});
app.on('window-all-closed',()=>app.quit());
