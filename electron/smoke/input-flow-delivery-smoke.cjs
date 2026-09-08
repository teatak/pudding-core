// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/input-flow-delivery-smoke.cjs
// Production Composer -> API payload -> SSE overlay -> canonical transcript.
// HTTP responses/events are fixtures; no daemon, provider, or real session data.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-input-delivery-'));
app.setPath('userData', path.join(output, 'user-data'));
let vite, window, exitCode = 0;
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
async function answer() {
  await waitFor('!!document.querySelector("[data-input-flow-panel] [role=option]")', 'question');
  const point = await js(`(()=>{const r=document.querySelector('[data-input-flow-panel] [role=option]').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  for(const type of ['mouseMove','mouseDown','mouseUp']) window.webContents.sendInputEvent({type,...point,button:'left',clickCount:1});
}
async function run() {
  assert.equal(process.execPath,path.join(repo,'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {createServer} = await import(pathToFileURL(path.join(repo,'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({root:path.join(repo,'web'),cacheDir:path.join(output,'vite-cache'),logLevel:'error',server:{host:'127.0.0.1',port:0},plugins:[{
    name:'input-delivery-fixture',
    resolveId(id) {if(id==='/__delivery.js')return '\0input-delivery-fixture';},
    load(id) {
      if(id!=='\0input-delivery-fixture')return;
      return `
        import React from 'react';
        import {createRoot} from 'react-dom/client';
        import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
        import {createRootRoute,createRoute,createRouter,createMemoryHistory,RouterProvider} from '@tanstack/react-router';
        import '/src/styles.css';
        import {Composer} from '/src/components/Composer.tsx';
        import {TranscriptTurn} from '/src/components/transcript/TranscriptTurn.tsx';
        import {useTranscriptData} from '/src/components/transcript/useTranscriptData.ts';
        import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
        import {useOverlayStore} from '/src/state/overlayStore.ts';
        import {useInputFlowStore} from '/src/state/inputFlowStore.ts';
        import {createInputFlowTools} from '/src/mcp/inputFlowTools.ts';
        import {queryKeys} from '/src/api/queryKeys.ts';
        import {sessionEvent} from '/contracts/events.ts';
        import {setLocale} from '/src/i18n/index.ts';
        setLocale('zh-CN');
        const h=React.createElement,sessionID='form-delivery-current',turnID='original-turn',createdAt='2026-09-08T10:00:00Z';
        const active=location.search!=='?idle';
        const client=new QueryClient({defaultOptions:{queries:{enabled:false,retry:false},mutations:{retry:false}}});
        const store=useOverlayStore.getState();
        let seq=0,resolveResponse;
        const event=value=>store.applyEvent(sessionEvent.parse({sessionID,seq:++seq,...value}));
        const message=(id,role,parts,clientMessageID)=>({id,role,parts,clientMessageID,sessionID,turnID,createdAt});
        const text=value=>({type:'text',text:value});
        const original=message('original','user',[text('请询问我的选择。')],'original');
        const before=message('before','assistant',[text('等待你的选择。')]);
        let turns=[{id:turnID,sessionID,clientMessageID:'original',createdAt,updatedAt:createdAt,status:active?'running':'completed',messages:[original,before]}];
        const canonical=()=>client.setQueryData(queryKeys.turns(sessionID),{pages:[{turns,hasMore:false}],pageParams:[undefined]});
        canonical();
        client.setQueryData(queryKeys.queuedInputs(sessionID),{queuedInputs:[]});
        if(active) {
          event({kind:'turn.started',turnID,clientMessageID:'original',userMessageID:'original'});
          event({kind:'turn.delta',turnID,part:'text',delta:'等待你的选择。'});
        }
        store.applyEvent(sessionEvent.parse({kind:'turn.started',sessionID:'other-session',turnID:'other-turn',seq:1,clientMessageID:'other-input',userMessageID:'other-message'}));
        const originalFetch=window.fetch;
        window.fetch=(url,options)=>{
          if(String(url).startsWith('/sessions/')) {
            if(options?.method!=='POST')throw new Error('Unexpected fixture request: '+url);
            window.fixture.calls.push({path:String(url),body:JSON.parse(options.body)});
            return new Promise(resolve=>{resolveResponse=resolve;});
          }
          return originalFetch(url,options);
        };
        window.fixture={calls:[],
          respond(error) {
            const call=this.calls.at(-1),newTurnID=call.path.endsWith('/steer')?turnID:'answer-turn';
            resolveResponse(new Response(JSON.stringify(error?{error}:{turnID:newTurnID,userMessageID:'answer'}),{status:error?409:200,headers:{'Content-Type':'application/json'}}));
          },
          apply() {
            const call=this.calls.at(-1),body=call.body;
            const answer=message('answer','user',body.parts,body.clientMessageID);
            if(call.path.endsWith('/steer')) {
              event({kind:'input.steered',turnID,clientMessageID:body.clientMessageID,userMessageID:'answer',text:body.text});
              turns=[{...turns[0],messages:[original,before,answer]}];
            } else {
              answer.turnID='answer-turn';
              turns=[turns[0],{...turns[0],id:'answer-turn',clientMessageID:body.clientMessageID,status:'running',messages:[answer]}];
              event({kind:'turn.started',turnID:'answer-turn',clientMessageID:body.clientMessageID,userMessageID:'answer',text:body.text});
            }
            canonical();
            event({kind:'turn.delta',turnID:turns.at(-1).id,part:'text',delta:'已按你的选择继续。'});
          },
          finish() {
            const current=turns.at(-1),final={...message('final','assistant',[text('已按你的选择继续。')]),turnID:current.id};
            event({kind:'turn.completed',turnID:current.id,assistantMessageID:'final'});
            turns=[...turns.slice(0,-1),{...current,status:'completed',messages:[...current.messages,final]}];
            canonical();
          },
          canonicalOnly() {store.clearSession(sessionID);},
          pending() {return useOverlayStore.getState().pendingUsers[sessionID]||[];},
        };
        function Fixture() {
          const running=useOverlayStore(state=>!!state.runningTurns[sessionID]);
          const {transcript}=useTranscriptData({token:'',sessionID,sessionRunning:running});
          window.fixture.vm=transcript.turnVMs;
          return h('main',{style:{padding:24}},
            h('div',{id:'transcript',style:{height:370,overflow:'auto'}},transcript.turnVMs.map(turn=>h(TranscriptTurn,{key:turn.key,turn,sessionID,token:''}))),
            h(Composer,{session:{id:sessionID,title:'答复投递测试',provider:'fixture',model:'fixture-model',mode:'chat',running},token:'',onSubmitError:error=>window.fixture.submitError=error}));
        }
        const rootRoute=createRootRoute();
        const indexRoute=createRoute({getParentRoute:()=>rootRoute,path:'/',component:Fixture});
        const router=createRouter({routeTree:rootRoute.addChildren([indexRoute]),history:createMemoryHistory({initialEntries:['/']})});
        createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,h(RouterProvider,{router}))));
        createInputFlowTools()[0].handler({_pudding_session_id:sessionID,type:'form',title:'选购确认',steps:[{id:'action',type:'single_select',title:'原有商品怎么处理？',options:[{title:'删掉，只买这次清单',value:'remove'},{title:'保留',value:'keep'}]}]});
      `;
    },
    configureServer(server) {server.middlewares.use('/__fixture',async(_req,res,next)=>{
      try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture','<!doctype html><html><head><title>Input delivery regression</title></head><body><div id="root"></div><script type="module" src="/__delivery.js"></script></body></html>'));}catch(error){next(error);}
    });},
  }]});
  await vite.listen();
  // Hidden source Electron window avoids interrupting the user's foreground app.
  window=new BrowserWindow({width:900,height:900,show:false,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message',details=>{if(details.level==='error')errors.push(details.message);});
  const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture';
  for(const scenario of ['active','idle','reject']) {
    await window.loadURL(url+'?'+scenario);
    assert.equal(window.webContents.getURL(),url+'?'+scenario);
    await answer();
    await waitFor('window.fixture.calls.length===1','one API request');
    const call=await js('window.fixture.calls[0]');
    assert.equal(call.path,scenario==='idle'?'/sessions/form-delivery-current/submit':'/sessions/form-delivery-current/turns/original-turn/steer',scenario+': answer uses correct delivery route');
    assert.equal(call.body.parts[0].type,'form_result');
    assert.equal(call.body.parts[0].result.action,'remove');
    assert.match(call.body.text,/删掉，只买这次清单/);
    assert.equal((await js('window.fixture.pending()'))[0].status,scenario==='idle'?'submitting':'steering');
    assert.equal(await js('document.getElementById("transcript").textContent.includes("待发送")'),false);
    if(scenario==='reject') {
      await js('window.fixture.respond("turn_not_active")');
      await waitFor('!!document.querySelector("[data-input-flow-panel]") && !!window.fixture.submitError','failed answer restored');
      assert.equal(await js('window.fixture.pending().length'),0);
      assert.equal(await js('window.fixture.calls.length'),1,'no implicit queued resend');
      await js('window.fixture.canonicalOnly()');
      await answer();
      await waitFor('window.fixture.calls.length===2','explicit retry');
      const retry=await js('window.fixture.calls[1]');
      assert.equal(retry.path,'/sessions/form-delivery-current/submit');
      assert.equal(retry.body.clientMessageID,call.body.clientMessageID,'retry retains idempotency key');
    }
    await js('window.fixture.respond()');
    await delay(100);
    await js('window.fixture.apply()');
    await waitFor('window.fixture.pending().length===0','canonical answer replaces overlay');
    assert.equal(await js('document.querySelectorAll("#transcript [data-transcript-message-role=user]").length'),2,'one original input and one form answer');
    assert.equal(await js('document.getElementById("transcript").textContent.includes("待发送")'),false);
    assert.equal(await js('window.fixture.vm.length'),scenario==='active'?1:2,'answer belongs to current turn while running');
    await js('window.fixture.finish()');
    await delay(200);
    await js('window.fixture.canonicalOnly()');
    await delay(100);
    assert.equal(await js('window.fixture.pending().length'),0);
    assert.equal(await js('document.querySelectorAll("#transcript [data-transcript-message-role=user]").length'),2);
    assert.equal(await js('window.fixture.calls.length'),scenario==='reject'?2:1,'turn completion does not send again');
    fs.writeFileSync(path.join(output,scenario+'.png'),(await window.webContents.capturePage()).toPNG());
    console.log('PASS '+scenario+' routing, canonical reconciliation and no resend');
  }
  assert.deepEqual(errors,[]);
  console.log('REPORT '+output);
}
app.whenReady().then(run).catch(error=>{console.error(error);exitCode=1;}).finally(async()=>{await vite?.close();app.exit(exitCode);});
