// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/transcript-history-smoke.cjs
// Source Electron + production Transcript/query pagination with isolated HTTP
// fixtures. Record every painted frame, not only the final restored anchor.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { app, BrowserWindow } = require('electron');
const repo = path.resolve(__dirname, '../..');
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-history-smoke-'));
const output = process.env.PUDDING_SMOKE_OUTPUT || path.join(home, 'results');
fs.mkdirSync(output, { recursive: true });
app.setPath('userData', path.join(home, 'user-data'));
let vite, window;
const errors = [];
const js = code => window.webContents.executeJavaScript(code, true);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(code, label) {
  const until = Date.now() + 15000;
  while (Date.now() < until) {
    assert.deepEqual(errors, []);
    if (await js(code)) return;
    await delay(20);
  }
  throw new Error('Timed out: ' + label + ' ' + JSON.stringify(await js(`({text:document.body.innerText.slice(0,1500), requests:window.fixture?.requests, url:location.href})`)));
}

async function run() {
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const { createServer } = await import(pathToFileURL(path.join(repo, 'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({
    root: path.join(repo, 'web'), cacheDir: path.join(home, 'vite-cache'), logLevel: 'error',
    server: { host: '127.0.0.1', port: 0, watch: null, hmr: false },
    plugins: [{
      name: 'history-fixture',
      resolveId(id) { if (id === '/__history.js') return '\0history-fixture'; },
      load(id) {
        if (id !== '\0history-fixture') return;
        return `
          import React from 'react';
          import {createRoot} from 'react-dom/client';
          import {QueryClient, QueryClientProvider} from '@tanstack/react-query';
          import {createRootRoute, createRoute, createRouter, createMemoryHistory, RouterProvider} from '@tanstack/react-router';
          import {Transcript} from '/src/components/Transcript.tsx';
          import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
          import {setLocale} from '/src/i18n/index.ts';
          import '/src/styles.css';
          setLocale('zh-CN');
          const h=React.createElement, sessionID='history-fixture';
          const turns=Array.from({length:60},(_,i)=>{
            const id='turn-'+i, createdAt=new Date(Date.UTC(2026,8,14,0,i)).toISOString();
            const text='第 '+i+' 轮的历史回复。\\n\\n'+Array.from({length:[1,12,3,24,2][i%5]},(_,j)=>
              '**段落 '+j+'**：'+ '每条历史消息的高度不同，加载更早的内容时应保持正在阅读的位置。'.repeat(4)).join('\\n\\n');
            return {id,sessionID,clientMessageID:'input-'+i,status:'completed',createdAt,updatedAt:createdAt,messages:[
              {id:'user-'+i,turnID:id,sessionID,role:'user',clientMessageID:'input-'+i,kind:'text',createdAt,turnIndex:0,text:'第 '+i+' 轮问题',parts:[{type:'text',text:'第 '+i+' 轮问题'}]},
              {id:'answer-'+i,turnID:id,sessionID,role:'assistant',kind:'text',createdAt,turnIndex:1,text,parts:[{type:'thought',text:'已完成分析。'}, {type:'text',text}]}
            ]};
          });
          const originalFetch=window.fetch;
          window.fixture={requests:0,release:null};
          window.fetch=(url,options)=>{
            const target=new URL(String(url),location.href);
            if(target.pathname==='/settings')return Promise.resolve(Response.json({settings:{}}));
            if(target.pathname==='/sessions/'+sessionID+'/turns'){
              const before=target.searchParams.get('before');
              const end=before ? turns.findIndex(turn=>turn.id===before) : turns.length;
              const page={turns:turns.slice(Math.max(0,end-20),end),hasMore:end>20};
              window.fixture.requests++;
              if(!before)return Promise.resolve(Response.json(page));
              return new Promise(resolve=>{window.fixture.release=()=>{window.fixture.release=null;resolve(Response.json(page));};});
            }
            return originalFetch(url,options);
          };
          const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});
          function Fixture(){return h('main',{style:{height:'100vh',display:'flex',flexDirection:'column',
            '--pudding-composer-mask-height':'24px','--pudding-composer-overlay-height':'100px'}},
            h(Transcript,{sessionID,token:'fixture-token',searchSlot:'primary',searchState:{terms:[]}}));}
          const root=createRootRoute();
          const route=createRoute({getParentRoute:()=>root,path:'/',component:Fixture});
          const router=createRouter({routeTree:root.addChildren([route]),history:createMemoryHistory({initialEntries:['/']})});
          createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,h(RouterProvider,{router}))));
          window.fixture.viewport=()=>document.querySelector('[data-transcript-viewport]');
          window.fixture.anchor=()=>{
            const v=window.fixture.viewport(),top=v.getBoundingClientRect().top;
            return Array.from(v.querySelectorAll('[data-transcript-turn-id]')).find(row=>row.getBoundingClientRect().bottom>top+1);
          };
          window.fixture.sample=()=>{
            const v=window.fixture.viewport(),top=v.getBoundingClientRect().top;
            const row=v.querySelector('[data-transcript-turn-id="'+window.fixture.anchorID+'"]');
            return {offset:row ? row.getBoundingClientRect().top-top : null,first:window.fixture.anchor()?.dataset.transcriptTurnId,
              scroll:v.scrollTop,height:v.scrollHeight,rows:v.querySelectorAll('[data-transcript-turn-id]').length,
              jump:Boolean(document.querySelector('.pudding-conversation-bottom-dock button'))};
          };
          window.fixture.watch=()=>{
            window.fixture.anchorID=window.fixture.anchor().dataset.transcriptTurnId;
            window.fixture.frames=[window.fixture.sample()];
            const frame=()=>{
              window.fixture.timer=setTimeout(()=>window.fixture.frames.push(window.fixture.sample()),0);
              window.fixture.raf=requestAnimationFrame(frame);
            };
            window.fixture.raf=requestAnimationFrame(frame);
          };
        `;
      },
      configureServer(server) {
        server.middlewares.use('/__fixture', async (_req, res, next) => {
          try {
            res.setHeader('Content-Type', 'text/html');
            res.end(await server.transformIndexHtml('/__fixture', '<!doctype html><html><head><title>History pagination regression</title></head><body><div id="root"></div><script type="module" src="/__history.js"></script></body></html>'));
          } catch (error) { next(error); }
        });
      },
    }],
  });
  await vite.listen();
  window = new BrowserWindow({ width: 1000, height: 800, show: true, webPreferences: { contextIsolation: true, nodeIntegration: false, backgroundThrottling: false } });
  window.webContents.on('console-message', details => { if (details.level === 'error') errors.push(details.message); });
  const url = 'http://127.0.0.1:' + vite.httpServer.address().port + '/__fixture';
  await window.loadURL(url);
  assert.equal(window.webContents.getURL(), url);
  await waitFor(`document.querySelectorAll('[data-transcript-turn-id]').length > 0`, 'initial transcript');
  await delay(400);
  const results = [];
  for (const mode of ['idle', 'scrolling']) {
    await js(`(() => { const v=window.fixture.viewport(); v.dispatchEvent(new WheelEvent('wheel',{deltaY:-100,bubbles:true}));v.scrollTop=60; })()`);
    await waitFor('Boolean(window.fixture.release)', 'older history request');
    await delay(300);
    // Reposition after initial above-fold rows have mounted and been measured.
    await js('window.fixture.viewport().scrollTop = 60');
    await delay(300);
    if (mode === 'scrolling') {
      await js('window.fixture.viewport().scrollTop -= 1');
      await delay(20); // Response arrives while the virtualizer is still scrolling.
    }
    await js('window.fixture.watch()');
    const before = await js('window.fixture.sample()');
    await js('window.fixture.release()');
    await delay(500);
    const frames = await js('cancelAnimationFrame(window.fixture.raf);clearTimeout(window.fixture.timer);window.fixture.frames');
    const drift = Math.max(...frames.map(frame => frame.offset === null ? Infinity : Math.abs(frame.offset - before.offset)));
    fs.writeFileSync(path.join(output, mode + '.json'), JSON.stringify({ before, drift, frames }, null, 2));
    fs.writeFileSync(path.join(output, mode + '.png'), (await window.webContents.capturePage()).toPNG());
    console.info('[history-scroll]', JSON.stringify({ mode, drift, before, after: frames.at(-1), frames: frames.length }));
    results.push({mode,drift,before,frames});
  }
  for (const {mode,drift,before,frames} of results) {
    assert.ok(frames.length > 10, 'captured the pagination transition');
    assert.ok(drift < 2, mode + ': reading anchor must remain in place in every painted frame');
    assert.ok(frames.every(frame => frame.first === before.first && frame.jump), 'history loading must preserve the reading turn and never resume following');
  }
  assert.deepEqual(errors, []);
  console.info('PASS paginated history stays anchored both while idle and while scrolling. Artifacts: ' + output);
}
app.whenReady().then(run).then(() => finish(0), error => { console.error(error); return finish(1); });
async function finish(code) {
  window?.destroy();
  await vite?.close();
  app.exit(code);
}
