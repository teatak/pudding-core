// Source Electron + production Composer/Transcript; isolated API responses and user data.
// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/compact-delivery-smoke.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-compact-delivery-'));
app.setPath('userData', path.join(output, 'user-data'));
let vite, window;
const errors = [];
const js = source => window.webContents.executeJavaScript(source, true);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(source, label) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    assert.deepEqual(errors, []);
    if (await js(source)) return;
    await delay(30);
  }
  throw new Error('Timed out: ' + label);
}

async function run() {
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {createServer} = await import(pathToFileURL(path.join(repo, 'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({root:path.join(repo, 'web'), cacheDir:path.join(output, 'vite-cache'), logLevel:'error', server:{host:'127.0.0.1', port:0, watch:null, hmr:false}, plugins:[{
    name:'compact-delivery-fixture',
    resolveId(id) { if (id === '/__compact.js') return '\0compact-delivery-fixture'; },
    load(id) {
      if (id !== '\0compact-delivery-fixture') return;
      return `
        import React from 'react';
        import {createRoot} from 'react-dom/client';
        import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
        import {createRootRoute,createRoute,createRouter,createMemoryHistory,RouterProvider} from '@tanstack/react-router';
        import '/src/styles.css';
        import {Composer} from '/src/components/Composer.tsx';
        import {Transcript} from '/src/components/Transcript.tsx';
        import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
        import {useSessionEvents} from '/src/hooks/useSessionEvents.ts';
        import {useOverlayStore} from '/src/state/overlayStore.ts';
        import {setLocale} from '/src/i18n/index.ts';
        setLocale('zh-CN');
        const h=React.createElement, sessionID='compact-delivery', turnID='compact-turn';
        const session={id:sessionID,title:'压缩回归',provider:'fixture',model:'fixture-model',activeMode:'chat',running:false};
        const createdAt='2026-09-12T00:00:00Z';
        const message=(id,role,text,tid)=>({id,sessionID,turnID:tid,role,kind:role==='summary'?'summary':'text',text,parts:[{type:'text',text}],turnIndex:role==='user'?0:1,createdAt});
        const history=Array.from({length:12},(_,i)=>{
          const time=new Date(Date.parse(createdAt)+i*1000).toISOString();
          return {id:'turn-'+i,sessionID,clientMessageID:'client-'+i,status:'completed',createdAt:time,updatedAt:time,messages:[
            {...message('u-'+i,'user','历史消息 '+i,'turn-'+i),clientMessageID:'client-'+i},
            message('a-'+i,'assistant','已有回复，保持滚动位置。','turn-'+i),
          ]};
        });
        const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});
        let resolvePost,body,snapshot,resolveGets=[],row,observer,removed=false;
        window.EventSource=class extends EventTarget {constructor(){super();window.fixture.source=this;}close(){}};
        const originalFetch=window.fetch;
        window.fetch=(url,options)=>{
          const route=String(url);
          if(route==='/settings')return Promise.resolve(Response.json({settings:{}}));
          if(!route.startsWith('/sessions/'))return originalFetch(url,options);
          if(route==='/sessions/'+sessionID+'/approvals')return Promise.resolve(Response.json({approvals:[]}));
          if(route.startsWith('/sessions/'+sessionID+'/turns?'))return Promise.resolve(Response.json({turns:history,hasMore:false}));
          if(route==='/sessions/'+sessionID+'/compact'){
            body=JSON.parse(options.body);
            snapshot={id:turnID,sessionID,clientMessageID:body.clientMessageID,status:'completed',createdAt:'2026-09-12T00:01:00Z',updatedAt:'2026-09-12T00:01:00Z',messages:[
              {...message('summary','summary','正式压缩摘要',turnID),metadata:{compact:{source_message_ids:['u-0','a-0'],tail_message_ids:['u-11','a-11']}}},
            ]};
            window.fixture.calls++;
            return new Promise(resolve=>{resolvePost=resolve;});
          }
          if(route==='/sessions/'+sessionID+'/turns/'+turnID){
            window.fixture.snapshotReads++;
            return new Promise(resolve=>resolveGets.push(resolve));
          }
          throw new Error('Unexpected API route: '+route);
        };
        window.fixture={calls:0,snapshotReads:0,
          pending:()=>Boolean(useOverlayStore.getState().compactRuns[sessionID]),
          complete(error){resolvePost(Response.json(error?{error}:{turnID,summaryMessageID:'summary',status:'completed',sourceMessages:2,tailMessages:2,summaryChars:6},{status:error?409:202}));},
          snapshots(error){for(const resolve of resolveGets.splice(0))resolve(Response.json(error?{error}:snapshot,{status:error?500:200}));},
          event(){this.source.dispatchEvent(new MessageEvent('turn.completed',{data:JSON.stringify({kind:'turn.completed',seq:1,sessionID,turnID,assistantMessageID:'summary'})}));},
          watch(){
            row=document.querySelector('[data-transcript-turn-id="turn:client:'+body.clientMessageID+'"]');
            if(!row)throw new Error('Missing pending compact row');
            observer=new MutationObserver(()=>{if(!row.isConnected)removed=true;});
            observer.observe(document.getElementById('transcript'),{childList:true,subtree:true});
          },
          continuity(){return {connected:row.isConnected,removed,same:document.querySelector('[data-transcript-turn-id="'+turnID+'"]')===row};},
        };
        function Fixture(){
          useSessionEvents(sessionID,'fixture-token');
          return h('main',{style:{height:'100vh',display:'flex',flexDirection:'column',padding:24}},
            h('div',{id:'transcript',style:{flex:1,minHeight:0,display:'flex',flexDirection:'column'}},h(Transcript,{token:'fixture-token',sessionID,searchSlot:'primary',searchState:{terms:[]}})),
            h(Composer,{session,token:'',onSubmitError:error=>window.fixture.submitError=error}));
        }
        const rootRoute=createRootRoute();
        const indexRoute=createRoute({getParentRoute:()=>rootRoute,path:'/',component:Fixture});
        const router=createRouter({routeTree:rootRoute.addChildren([indexRoute]),history:createMemoryHistory({initialEntries:['/']})});
        createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,h(RouterProvider,{router}))));
      `;
    },
    configureServer(server) { server.middlewares.use('/__fixture', async(_req,res,next)=>{
      try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture','<!doctype html><html><head><title>Compact delivery regression</title></head><body><div id="root"></div><script type="module" src="/__compact.js"></script></body></html>'));}catch(error){next(error);}
    }); },
  }]});
  await vite.listen();
  window = new BrowserWindow({width:900,height:850,show:false,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message', details=>{if(details.level==='error')errors.push(details.message);});
  const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture';
  for (const scenario of ['http-first','sse-first','reject','snapshot-error']) {
    await window.loadURL(url+'?'+scenario);
    assert.equal(window.webContents.getURL(),url+'?'+scenario);
    await waitFor('document.querySelectorAll(".pudding-transcript-turn").length>0 && !!document.querySelector("textarea")','fixture ready');
    await js('document.querySelector("textarea").focus()');
    await window.webContents.insertText('/compact');
    await js('document.querySelector("form").requestSubmit()');
    await waitFor('window.fixture.calls===1 && window.fixture.pending()','pending compact');
    await delay(100);
    await js('window.fixture.watch()');
    if (scenario === 'reject') {
      await js('window.fixture.complete("compact_empty")');
      await waitFor('!window.fixture.pending() && !!window.fixture.submitError','failed compact cleanup');
      assert.equal(await js('window.fixture.snapshotReads'),0);
      console.log('PASS rejected compact clears its pending row and reports the error');
      continue;
    }
    if (scenario === 'sse-first') {
      await js('window.fixture.event()');
      await waitFor('window.fixture.snapshotReads===1','SSE snapshot request');
      await js('window.fixture.snapshots()');
      await waitFor('document.querySelector("[data-transcript-turn-id=compact-turn]")?.textContent.includes("上下文已压缩")','early SSE snapshot');
    }
    await js('window.fixture.complete()');
    await waitFor('window.fixture.snapshotReads>'+ (scenario==='sse-first'?1:0)+' || !window.fixture.pending()','HTTP completion');
    await delay(200);
    assert.equal(await js('window.fixture.pending()'),true,'HTTP completion must retain the pending state while the snapshot is delayed');
    assert.deepEqual(await js('window.fixture.continuity()'),{connected:true,removed:false,same:scenario==='sse-first'});
    await js('window.fixture.snapshots('+ (scenario==='snapshot-error'?'"snapshot_failed"':'') +')');
    if (scenario === 'snapshot-error') {
      await waitFor('!window.fixture.pending() && !!window.fixture.submitError','snapshot failure cleanup');
      console.log('PASS snapshot failure reports the error without leaving a stuck pending row');
      continue;
    }
    await waitFor('!window.fixture.pending() && document.querySelector("[data-transcript-turn-id=compact-turn]")?.textContent.includes("上下文已压缩")','canonical compact');
    await delay(250); // Include the compositor frame and deferred virtualizer measurements.
    assert.deepEqual(await js('window.fixture.continuity()'),{connected:true,removed:false,same:true},'pending and canonical must reuse the same mounted row');
    assert.equal(await js('document.querySelectorAll("[data-transcript-turn-id=compact-turn]").length'),1);
    fs.writeFileSync(path.join(output,scenario+'.png'),(await window.webContents.capturePage()).toPNG());
    console.log('PASS '+scenario+' keeps the mounted compact row through canonical reconciliation');
  }
  assert.deepEqual(errors, []);
  console.log('Artifacts: '+output);
}
app.whenReady().then(run).then(()=>finish(0),error=>{console.error(error);return finish(1);});
async function finish(code) {
  window?.destroy();
  await vite?.close();
  app.exit(code);
}
