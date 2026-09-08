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
          import {setLocale} from '/src/i18n/index.ts';
          setLocale('zh-CN');
          const h=React.createElement, sessionID='guide-fixture', turnID='turn-guide';
          const store=useOverlayStore.getState(), createdAt='2026-09-08T12:00:00Z';
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
            event({kind:'turn.delta',part:'text',delta:'引导前输出'});
          }
          function Fixture() {
            const [viewport,setViewport]=useState(null);
            const [turns,setTurns]=useState([...historical,{...baseTurn,status:completed?'completed':'running',
              messages:completed?[original,before,guide,after,mixed,final]:[original]}]);
            const state=useOverlayStore();
            const transcript=useTranscriptViewModel({sessionID,sessionRunning:turns.at(-1).status==='running',turns,turnDurationByID:new Map(),
              assistantOverlays:Object.values(state.assistants),pendingUsers:state.pendingUsers[sessionID]||[],turnPhase:state.turnPhases[sessionID]});
            useEffect(()=>store.reconcileMessages(sessionID,turns.flatMap(t=>t.messages)),[turns]);
            const canonical=(messages,status='running')=>setTurns([...historical,{...baseTurn,status,messages}]);
            window.fixture={
              vm:transcript,
              queue(id) { const m=id==='guide-image'?guide:mixed;
                store.addPendingUser({sessionID,turnID,clientMessageID:id,createdAt,text:m.text,parts:m.parts,status:'steering'}); },
              apply(id) {event({kind:'input.steered',seq:id==='guide-image'?2:3,clientMessageID:id,userMessageID:id,
                ...(id==='guide-mixed'?{text:'补充说明'}:{})});},
              commit(id) {canonical(id==='guide-image'?[original,before,guide]:[original,before,guide,after,mixed]);},
              delta(value) {event({kind:'turn.delta',part:'text',delta:value});},
              finish() {event({kind:'turn.completed',seq:4,assistantMessageID:'final'});canonical([original,before,guide,after,mixed,final],'completed');},
            };
            return h(TooltipProvider,null,h('div',{id:'viewport',ref:setViewport,style:{height:820,overflow:'auto',overflowAnchor:'none',contain:'strict',padding:'0 28px',
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
  await js('window.fixture.finish()');
  await waitFor(async()=>JSON.stringify(await order())===JSON.stringify(expected),'canonical reconciliation');
  console.log('PASS mixed attachments and multiple guides retain order through completion');
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
