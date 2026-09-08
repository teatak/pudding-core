// Test-only receiver for the native background-input probe. No Pudding daemon, network or clipboard.
const {app, BrowserWindow, screen} = require('electron');
const path = require('node:path');
const readline = require('node:readline');
const dir = process.argv[2];
app.setPath('userData', path.join(dir, 'electron-user-data'));
let win;
const emit = value => process.stdout.write(JSON.stringify(value) + '\n');
app.whenReady().then(async () => {
  win = new BrowserWindow({width: 400, height: 400, useContentSize: true,
    // Explicit positive control, not a change to the ordinary/default receiver.
    acceptFirstMouse: process.argv[3] === 'click-through',
    title: 'Pudding Background Probe — electron',
    webPreferences: {contextIsolation: true, nodeIntegration: false}});
  for (const type of ['focus', 'blur']) win.on(type, () => emit({kind: 'window-focus', type}));
  win.webContents.on('console-message', details => {
    if (details.message.startsWith('PROBE:')) emit(JSON.parse(details.message.slice(6)));
  });
  await win.loadURL('data:text/html;charset=utf-8,' + encodeURIComponent(`<!doctype html>
  <meta charset="utf-8"><title>Pudding Background Probe — electron</title>
  <style>body{margin:0}button,#canvas,#scroll{position:absolute;left:30px;width:330px;box-sizing:border-box}
  button{top:40px;height:40px}#canvas{top:110px;height:80px;background:#78a6ff;user-select:none;touch-action:none}
  #scroll{top:220px;height:160px;overflow:auto}</style>
  <button id="click">Increment</button><div id="canvas">Drag here</div><div id="scroll"></div>
  <script>
  const report = v => console.log('PROBE:'+JSON.stringify(v));
  let count=0,start;
  for(const type of ['mousedown','mouseup','mousemove','click','dblclick','contextmenu','wheel']) {
    document.addEventListener(type,e=>{
      if(type!=='mousemove'||e.buttons) report({kind:'event',type,command:e.metaKey,trusted:e.isTrusted,
        flags:(e.metaKey?0x100000:0)|(e.ctrlKey?0x40000:0)|(e.shiftKey?0x20000:0)|(e.altKey?0x80000:0),
        detail:e.detail,target:e.target.id,buttons:e.buttons,x:e.clientX,y:e.clientY});
    },true);
  }
  const button=document.querySelector('#click'), canvas=document.querySelector('#canvas'), scroll=document.querySelector('#scroll');
  button.onclick=e=>{button.textContent=String(++count);report({kind:'effect',effect:'click',count,detail:e.detail,command:e.metaKey});};
  button.ondblclick=e=>report({kind:'effect',effect:'button-double',detail:e.detail,command:e.metaKey});
  canvas.ondblclick=e=>report({kind:'effect',effect:'double',command:e.metaKey});
  canvas.oncontextmenu=e=>{e.preventDefault();report({kind:'effect',effect:'right',command:e.metaKey});};
  canvas.onmousedown=e=>{start=e.clientX;};
  canvas.onmouseup=e=>{if(start!==undefined&&e.clientX-start>30)report({kind:'effect',effect:'drag',command:e.metaKey});start=undefined;};
  scroll.innerHTML=Array.from({length:100},(_,i)=>'<p>Test row '+i+'</p>').join('');
  scroll.onwheel=e=>{const before=scroll.scrollTop;setTimeout(()=>report({kind:'effect',effect:'scroll',before,after:scroll.scrollTop,command:e.metaKey}),120);};
  window.probePoints=()=>Object.fromEntries(Object.entries({click:button,double:canvas,right:canvas,drag:canvas,scroll}).map(([k,v])=>{
    const r=v.getBoundingClientRect();return [k,{x:r.x+r.width/2,y:r.y+r.height/2}];
  }));
  </script>`));
  emit({kind: 'ready', pid: process.pid});
  let queue = Promise.resolve();
  const lines = readline.createInterface({input: process.stdin});
  lines.on('line', line => {
    queue = queue.then(async () => {
      const command = JSON.parse(line);
      if (command.op === 'position') {
        win.setPosition(80, screen.getPrimaryDisplay().bounds.height - 140 - win.getBounds().height);
        win.show(); win.focus(); // Experiment setup only.
      }
      const local = await win.webContents.executeJavaScript('window.probePoints()');
      const content = win.getContentBounds();
      const points = Object.fromEntries(Object.entries(local).map(([key,p]) => [key,{x:content.x+p.x,y:content.y+p.y}]));
      emit({kind: 'reply', id: command.id, pid: process.pid, executable: process.execPath,
        frame: win.getBounds(), points, keyWindow: win.isFocused(), acceptFirstMouse: process.argv[3] === 'click-through',
        buttonText: await win.webContents.executeJavaScript('document.querySelector("#click").textContent')});
    }).catch(error => emit({kind: 'error', error: error.message}));
  });
  lines.on('close', () => app.quit());
});
app.on('window-all-closed', () => app.quit());
