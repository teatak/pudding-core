// Run with web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron.
// Production event contract -> overlay -> view model -> virtual transcript,
// with disposable inputs only; no daemon, provider or existing session is used.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { app, BrowserWindow } = require('electron');
const repo = path.resolve(__dirname, '../..');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-guides-smoke-'));
app.setPath('userData', path.join(output, 'user-data'));
let vite, window, exitCode = 0;
const errors = [];
const js = (source) => window.webContents.executeJavaScript(source, true);
const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(predicate, label) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    assert.deepEqual(errors, []);
    if (await predicate()) return;
    await delay(30);
  }
  throw new Error('Timed out: ' + label);
}
async function click(selector) {
  const point = await js(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});e.scrollIntoView({block:'nearest'});
    const r=e.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  window.webContents.sendInputEvent({type:'mouseDown',...point,button:'left',clickCount:1});
  window.webContents.sendInputEvent({type:'mouseUp',...point,button:'left',clickCount:1});
}
async function run() {
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const { createServer } = await import(pathToFileURL(path.join(repo, 'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({
    root:path.join(repo,'web'), cacheDir:path.join(output,'vite-cache'), logLevel:'error',
    server:{host:'127.0.0.1',port:0},
    plugins:[{
      name:'guides-fixture',
      resolveId(id) { if (id === '/__guides.js') return '\0guides-fixture'; },
      load(id) {
        if (id !== '\0guides-fixture') return;
        return `
          import React, {useEffect,useState} from 'react';
          import {createRoot} from 'react-dom/client';
          import '/src/styles.css';
          import {TranscriptList} from '/src/components/transcript/TranscriptList.tsx';
          import {useTranscriptViewModel} from '/src/components/transcript/useTranscriptViewModel.ts';
          import {useOverlayStore} from '/src/state/overlayStore.ts';
          import {sessionEvent} from '/contracts/events.ts';
          import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
          import {SidebarProvider} from '/src/components/ui/sidebar.tsx';
          import {SessionItem} from '/src/components/session-rail/SessionItem.tsx';
          import {isSessionTurnRunning} from '/src/components/session-rail/activity.ts';
          import {setLocale} from '/src/i18n/index.ts';
          setLocale('zh-CN');
          const h=React.createElement, sessionID='guide-fixture', turnID='turn-guide';
          const store=useOverlayStore.getState(), createdAt='2026-09-08T12:00:00Z';
          const fast=location.search.includes('fast');
          let now=Date.parse(createdAt)+(fast?500:59_000);
          Date.now=()=>now;
          const image=(id)=>({type:'attachment',id,name:id+'.png',mime:'image/png',size:68,attachmentKey:id,url:'/__guide-image.png'});
          const file={type:'attachment',id:'file',name:'notes.txt',mime:'text/plain',size:10,attachmentKey:'file',url:'/__guide-file.txt'};
          const text=(text)=>({type:'text',text});
          const message=(id,role,parts,clientMessageID)=>({id,role,parts,clientMessageID,sessionID,turnID,createdAt,
            text:parts.filter(p=>p.type==='text').map(p=>p.text).join('')});
          const original=message('original','user',[image('original'),text('初始请求')],'original');
          const before=message('before','assistant',[text('引导前输出')]);
          const guide=message('guide-image','user',[image('guide-image')],'guide-image');
          const after=message('after','assistant',[text('第一张图之后的输出')]);
          const mixed=message('guide-mixed','user',[image('guide-mixed'),file,text('补充说明')],'guide-mixed');
          const final=message('final','assistant',[text('第二次引导后的结果')]);
          const completed=location.search.includes('completed');
          const historical=Array.from({length:30},(_,i)=>({id:'history-'+i,sessionID,clientMessageID:'history-'+i,status:'completed',createdAt,updatedAt:createdAt,
            messages:[{...message('history-user-'+i,'user',[text('历史消息 '+i)],'history-'+i),turnID:'history-'+i},
              {...message('history-ai-'+i,'assistant',[text('历史输出。'.repeat(80))]),turnID:'history-'+i}]}));
          const baseTurn={id:turnID,sessionID,clientMessageID:'original',createdAt,updatedAt:createdAt};
          const event=(value)=>store.applyEvent(sessionEvent.parse({sessionID,turnID,...value}));
          if(!completed) {
            event({kind:'turn.started',seq:1,clientMessageID:'original',userMessageID:'original',text:'初始请求'});
            if(!fast) event({kind:'turn.delta',part:'text',delta:'引导前输出'});
          }
          function Fixture() {
            const [viewport,setViewport]=useState(null);
            const [sessionRunning,setSessionRunning]=useState(!completed);
            const [turns,setTurns]=useState([...historical,{...baseTurn,status:completed?'completed':'running',
              messages:completed?[original,before,guide,after,mixed,final]:[original]}]);
            const state=useOverlayStore();
            const transcript=useTranscriptViewModel({sessionID,sessionRunning:turns.at(-1).status==='running',turns,
              assistantOverlays:Object.values(state.assistants),pendingUsers:state.pendingUsers[sessionID]||[],turnPhase:state.turnPhases[sessionID]});
            useEffect(()=>store.reconcileMessages(sessionID,turns.flatMap(t=>t.messages)),[turns]);
            const canonical=(messages,status='running')=>setTurns([...historical,{...baseTurn,status,messages,
              updatedAt:status==='running'?createdAt:new Date(now).toISOString(),error:status==='failed'?'Test failure':undefined}]);
            window.fixture={
              vm:transcript,
              advance(ms) {now+=ms;},
              lateAck() {store.acceptSubmittingTurn(sessionID,'original',turnID);},
              queue(id) { const m=id==='guide-image'?guide:mixed;
                store.addPendingUser({sessionID,turnID,clientMessageID:id,createdAt,text:m.text,parts:m.parts,status:'steering'}); },
              apply(id) {event({kind:'input.steered',seq:id==='guide-image'?2:3,clientMessageID:id,userMessageID:id,
                ...(id==='guide-mixed'?{text:'补充说明'}:{})});},
              commit(id) {canonical(id==='guide-image'?[original,before,guide]:[original,before,guide,after,mixed]);},
              delta(value) {event({kind:'turn.delta',part:'text',delta:value});},
              terminal(status='completed') {event({kind:'turn.'+status,seq:4,assistantMessageID:fast?undefined:'final',...(status==='failed'?{error:'Test failure'}:{})});setSessionRunning(false);},
              finish(status='completed') {canonical(fast?[original]:[original,before,guide,after,mixed,final],status);},
            };
            const railSession={id:sessionID,title:'Failure regression',running:sessionRunning,createdAt,backgroundProcessCount:0};
            const noop=()=>{};
            return h(TooltipProvider,null,
              h(SidebarProvider,{style:{position:'fixed',top:0,right:0,width:260,minHeight:0,zIndex:50}},
                h('ul',{id:'test-rail',style:{width:'100%'}},h(SessionItem,{session:railSession,
                  running:isSessionTurnRunning(railSession,state.runningTurns,state.turnPhases),selected:true,completed:false,
                  archivePending:false,hasProjects:false,projectChangePending:false,suppressInteractiveState:false,dragging:false,
                  onSelect:noop,onOpenSplit:noop,onOpenProjectPicker:noop,onPinChange:noop,onRemoveProject:async()=>{},
                  onArchive:noop,onRename:async()=>{},onPointerDragStart:noop,onPointerDragMove:noop,onPointerDragEnd:noop,onPointerDragCancel:noop}))),
              h('div',{id:'viewport',ref:setViewport,style:{height:820,overflow:'auto',overflowAnchor:'none',contain:'strict',padding:'0 28px',
              '--pudding-composer-mask-height':'24px','--pudding-composer-overlay-height':'24px'}},
              h(TranscriptList,{turns:transcript.turnVMs,scrollElement:viewport,sessionID,searchSlot:'primary',searchState:{terms:[]},token:'',
                hasMoreHistory:false,isLoadingHistory:false,jumpLatestSignal:0,onLoadHistory:()=>{}})));
          }
          createRoot(document.getElementById('root')).render(h(Fixture));
        `;
      },
      configureServer(server) {
        server.middlewares.use('/__guide-image.png',(_req,res)=>{
          res.setHeader('Content-Type','image/png');
          res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jVh8AAAAASUVORK5CYII=','base64'));
        });
        server.middlewares.use('/__guide-fixture',async (_req,res,next)=>{
          try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__guide-fixture',
            '<!doctype html><html><head><title>Guide regression</title></head><body><div id="root"></div><script type="module" src="/__guides.js"></script></body></html>'));}
          catch(error){next(error);}
        });
      },
    }],
  });
  await vite.listen();
  window = new BrowserWindow({width:1000,height:880,show:true,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message',details=>{if(details.level==='error')errors.push(details.message);});
  const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__guide-fixture';
  await window.loadURL(url);
  await waitFor(()=>js('Boolean(window.fixture)'), 'fixture ready');
  const root='[data-transcript-turn-id="turn-guide"]';
  const header=root+' [data-turn-header]';
  const clock=root+' [data-turn-duration]';
  const clockText=()=>js(`document.querySelector('${clock}')?.textContent.trim()`);
  const settle=()=>js('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))');
  const scene=()=>js(`(()=>{const viewport=document.getElementById('viewport'),turn=document.querySelector('${root}');
    return {height:turn.getBoundingClientRect().height,scrollTop:viewport.scrollTop,waiting:turn.textContent.includes('等待模型'),
      spinner:!!document.querySelector('#test-rail [aria-label="处理中"]')};})()`);
  const checkLateAck=async()=>{
    await settle();
    const before=await scene();
    assert.equal(before.waiting,false);
    assert.equal(before.spinner,false);
    await js('window.fixture.lateAck()');
    await settle();
    assert.deepEqual(await scene(),before,'late HTTP ack cannot change content height, viewport or sidebar state');
  };
  const headerMetrics=()=>js(`(()=>{const row=document.querySelector('${header}'),r=row.getBoundingClientRect();
    const turn=document.querySelector('${root}').getBoundingClientRect();
    const content=document.querySelector('${root} [data-transcript-ai-anchor]')?.getBoundingClientRect();
    return {count:document.querySelectorAll('${header}').length,height:r.height,offset:r.y-turn.y,contentOffset:content?.y-turn.y};})()`);
  await waitFor(async()=>await clockText()==='已用 59秒','initial turn duration');
  await js(`window.originalTurnHeader=document.querySelector('${header}')`);
  const initialHeader=await headerMetrics();
  assert.equal(initialHeader.height,24);
  assert.equal(initialHeader.count,1);
  const initialScroll=await js('document.getElementById("viewport").scrollTop');
  await js('window.fixture.advance(2000)');
  await waitFor(async()=>await clockText()==='已用 1分1秒','live duration crosses a minute');
  assert.deepEqual(await headerMetrics(),initialHeader,'clock text does not move the content');
  assert.equal(await js('document.getElementById("viewport").scrollTop'),initialScroll,'clock tick does not move the viewport');
  const guideSelector=root+' [aria-label="已引导"]';
  const order=()=>js(`Array.from(Array.from(document.querySelector('${root}').children).find(e=>e.classList.contains('group/assistant-turn')).children)
    .filter(e=>e.matches('[aria-label="已引导"], [data-transcript-ai-anchor]'))
    .map(e=>e.getAttribute('aria-label')==='已引导' ? e.querySelector('img')?.alt : e.textContent.trim())`);
  await js(`window.fixture.queue('guide-image')`);
  await delay(80);
  assert.equal(await js(`document.querySelectorAll('${guideSelector}').length`),0,'waiting guide stays out of transcript');
  await js(`window.fixture.apply('guide-image')`);
  await waitFor(()=>js(`Boolean(document.querySelector('${guideSelector} img')?.naturalWidth)`),'applied image thumbnail');
  const sizes=await js(`Array.from(document.querySelectorAll('${root} img')).map(i=>({w:i.parentElement.offsetWidth,h:i.parentElement.offsetHeight,fit:getComputedStyle(i).objectFit}))`);
  assert.equal(sizes.length,2); assert.deepEqual(sizes[0],sizes[1]);
  assert.deepEqual(sizes[1],{w:96,h:80,fit:'contain'});
  await click(guideSelector+' button:has(img)');
  await waitFor(()=>js(`Boolean(document.querySelector('[role=dialog] img')?.naturalWidth)`),'guide image lightbox');
  await click('[aria-label="关闭预览"]');
  await waitFor(()=>js(`!document.querySelector('[role=dialog]')`),'lightbox closed');
  console.log('PASS guide thumbnail matches user input and opens image preview');

  await js(`window.fixture.commit('guide-image'); window.fixture.delta('第一张图之后的输出');`);
  await waitFor(async()=>JSON.stringify(await order())===JSON.stringify(['引导前输出','guide-image.png','第一张图之后的输出']),'first guide positioned before continuation');
  await js(`window.fixture.delta('，继续执行');`);
  await waitFor(async()=>(await order()).at(-1)==='第一张图之后的输出，继续执行','continued streaming after guide');
  assert.equal((await order())[1],'guide-image.png');
  console.log('PASS attachment-only input.steered moves guide out of waiting tail; streaming stays after guide');

  await js(`window.fixture.queue('guide-mixed')`);
  await delay(80);
  assert.equal(await js(`document.querySelectorAll('${guideSelector}').length`),1,'second waiting guide stays out of transcript');
  await js(`window.fixture.apply('guide-mixed')`);
  await waitFor(()=>js(`document.querySelectorAll('${guideSelector}').length===2`),'second applied guide rendered');
  assert.ok(await js(`document.querySelectorAll('${guideSelector}')[1].textContent.includes('notes.txt')`));
  assert.ok(await js(`document.querySelectorAll('${guideSelector}')[1].textContent.includes('补充说明')`));
  await js(`window.fixture.commit('guide-mixed'); window.fixture.delta('第二次引导后的结果');`);
  const expected=['引导前输出','guide-image.png','第一张图之后的输出','guide-mixed.png','第二次引导后的结果'];
  await waitFor(async()=>JSON.stringify(await order())===JSON.stringify(expected),'multiple guides ordered');
  assert.equal(await clockText(),'已用 1分1秒','steering and follow-up output do not reset the timer');
  const guidedHeader=await headerMetrics();
  await js('window.fixture.terminal()');
  await waitFor(async()=>await clockText()==='用时 1分1秒','completion freezes before final refetch');
  await checkLateAck();
  await js('window.fixture.advance(2000)');
  await delay(1100);
  assert.equal(await clockText(),'用时 1分1秒','terminal event stops the interval');
  // Persisted timestamps, not arrival time or a phase timer, own final duration.
  await js('window.fixture.advance(-2000); window.fixture.finish()');
  await waitFor(async()=>JSON.stringify(await order())===JSON.stringify(expected),'canonical reconciliation');
  assert.deepEqual(await headerMetrics(),guidedHeader,'header/content offsets stay fixed on reconciliation');
  assert.ok(await js(`window.originalTurnHeader===document.querySelector('${header}')`),'same header DOM survives all stages');
  console.log('PASS mixed attachments and multiple guides retain order through completion');
  for(const status of ['cancelled','failed']) {
    await window.loadURL(url);
    await waitFor(async()=>await clockText()==='已用 59秒','fresh running turn');
    const beforeStop=await headerMetrics();
    await js(`window.fixture.terminal('${status}')`);
    await waitFor(async()=>(await clockText())?.endsWith(status==='failed'?'后失败':'后中止'),'terminal status label');
    await checkLateAck();
    const frozen=await clockText();
    await js('window.fixture.advance(2000)');
    await delay(1100);
    assert.equal(await clockText(),frozen,'stopped/failed timer stays frozen');
    assert.deepEqual(await headerMetrics(),beforeStop,'terminal status does not change header/content geometry');
  }
  console.log('PASS fixed turn header, ticking/frozen duration, canonical timestamps and unchanged viewport');
  await window.loadURL(url+'?fast');
  await waitFor(async()=>await clockText()==='已用 不到1秒','fast failure fixture ready');
  assert.equal((await scene()).spinner,true,'sidebar shows a genuinely running turn');
  await js("window.fixture.terminal('failed')");
  await waitFor(()=>js(`document.querySelector('${root}').textContent.includes('请求失败')`),'failure before HTTP acknowledgement');
  await checkLateAck();
  const failedScene=await scene();
  await js("window.fixture.finish('failed')");
  await settle();
  assert.deepEqual(await scene(),failedScene,'final failed snapshot does not insert/remove a waiting row');
  console.log('PASS fast failure -> late HTTP acknowledgement -> final snapshot; no waiting row, sidebar spinner or scroll jump');
  await window.loadURL(url+'?completed');
  await waitFor(async()=>js('Boolean(window.fixture)').then(Boolean),'canonical reload');
  await waitFor(async()=>JSON.stringify(await order())===JSON.stringify(expected),'canonical history matches live order');
  fs.writeFileSync(path.join(output,'result.png'),(await window.webContents.capturePage()).toPNG());
  assert.deepEqual(errors,[]);
  console.log('PASS reload renders canonical guide images exactly once; REPORT '+output);

}
app.whenReady().then(run).catch(error=>{console.error(error);exitCode=1;}).finally(async()=>{
  await vite?.close(); app.exit(exitCode);
});
