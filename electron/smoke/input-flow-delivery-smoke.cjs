// Source Electron + production Composer/TurnParts, isolated HTTP fixtures.
// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/input-flow-delivery-smoke.cjs
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
async function click(selector) {
  await waitFor('!!document.querySelector('+JSON.stringify(selector)+')', selector);
  const point = await js(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  for(const type of ['mouseMove','mouseDown','mouseUp']) window.webContents.sendInputEvent({type,...point,button:'left',clickCount:1});
}
async function answer(value) {
  await click('[data-input-flow-panel] input');
  await window.webContents.insertText(value);
  for(const type of ['keyDown','keyUp']) window.webContents.sendInputEvent({type,keyCode:'Return'});
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
        import {TurnParts} from '/src/components/transcript/TurnParts.tsx';
        import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
        import {useOverlayStore} from '/src/state/overlayStore.ts';
        import {useInputFlowStore} from '/src/state/inputFlowStore.ts';
        import {createInputFlowTools} from '/src/mcp/inputFlowTools.ts';
        import {queryKeys} from '/src/api/queryKeys.ts';
        import {setLocale} from '/src/i18n/index.ts';
        setLocale('zh-CN');
        const h=React.createElement,sessionID='form-delivery',turnID='original-turn',requestID=turnID+':question';
        const scenario=location.search.slice(1),waitSeconds=scenario==='async'?0:scenario==='long'?300:10;
        const args={type:'form',title:'反馈确认',waitSeconds,steps:[{id:'reply',type:'text_input',title:'你有什么建议？'}]};
        const client=new QueryClient({defaultOptions:{queries:{enabled:false,retry:false},mutations:{retry:false}}});
        const realNow=Date.now;let offset=0,resolveResponse;
        Date.now=()=>realNow()+offset;
        let request={id:requestID,sessionID,turnID,title:args.title,args,status:waitSeconds?'waiting':'awaiting_user',...(waitSeconds?{deadline:new Date(Date.now()+waitSeconds*1000).toISOString()}:{})};
        function snapshot(){if(request.status==='waiting'&&Date.parse(request.deadline)<=Date.now())request={...request,status:'timeout',deadline:undefined};return request;}
        client.setQueryData(queryKeys.queuedInputs(sessionID),{queuedInputs:[]});
        const originalFetch=window.fetch;
        window.fetch=(url,options)=>{
          if(!String(url).startsWith('/sessions/'))return originalFetch(url,options);
          if(String(url)!=='/sessions/'+sessionID+'/input-requests/'+encodeURIComponent(requestID))throw new Error('Unexpected route: '+url);
          snapshot();
          if(options?.method==='POST'){
            const body=JSON.parse(options.body);
            if(body.action==='answer'){
              window.fixture.calls.push({path:String(url),body});
              return new Promise(resolve=>{resolveResponse=resolve;});
            }
            if(request.status==='waiting'){
              if(body.action==='touch'){window.fixture.touches++;request={...request,deadline:new Date(Date.now()+waitSeconds*1000).toISOString()};}
              if(body.action==='dismiss')request={...request,status:'dismissed',deadline:undefined};
            }
            return Promise.resolve(Response.json({request}));
          }
          return Promise.resolve(Response.json(request));
        };
        window.fixture={calls:[],touches:0,
          respond(error){
            this.delivery=snapshot().status==='waiting'?'tool':'message';
            if(!error)request={...request,status:'answered',deadline:undefined};
            resolveResponse(Response.json(error?{error}:{delivery:this.delivery,request},{status:error?409:200}));
          },
          shift(ms){offset+=ms;client.invalidateQueries({queryKey:queryKeys.inputRequest(sessionID,requestID)});},
          restartUI(){useInputFlowStore.setState({requests:[],drafts:{}});client.removeQueries({queryKey:queryKeys.inputRequest(sessionID,requestID)});},
          pending(){return useOverlayStore.getState().pendingUsers[sessionID]||[];},
          status(){return snapshot().status;},
        };
        function Fixture(){
          return h('main',{style:{padding:24}},
            h('div',{id:'transcript',style:{height:370,overflow:'auto'}},h(TurnParts,{token:'',sessionID,disclosureRootKey:'fixture',parts:[{type:'tool_use',id:'question',turnID,name:'builtin_request_user_input',args,phase:'running'}]})),
            h(Composer,{session:{id:sessionID,title:'答复投递测试',provider:'fixture',model:'fixture-model',activeMode:'chat',running:true},token:'',onSubmitError:error=>window.fixture.submitError=error}));
        }
        const rootRoute=createRootRoute();
        const indexRoute=createRoute({getParentRoute:()=>rootRoute,path:'/',component:Fixture});
        const router=createRouter({routeTree:rootRoute.addChildren([indexRoute]),history:createMemoryHistory({initialEntries:['/']})});
        createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,h(RouterProvider,{router}))));
        createInputFlowTools()[0].handler({...args,_pudding_session_id:sessionID,_pudding_request_id:requestID});
      `;
    },
    configureServer(server) {server.middlewares.use('/__fixture',async(_req,res,next)=>{
      try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture','<!doctype html><html><head><title>Input delivery regression</title></head><body><div id="root"></div><script type="module" src="/__delivery.js"></script></body></html>'));}catch(error){next(error);}
    });},
  }]});
  await vite.listen();
  window=new BrowserWindow({width:900,height:850,show:false,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message',details=>{if(details.level==='error')errors.push(details.message);});
  const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture';
  for(const scenario of ['sync','timeout','async','long','reject','restart','dismiss']) {
    await window.loadURL(url+'?'+scenario);
    assert.equal(window.webContents.getURL(),url+'?'+scenario);
    await waitFor('!!document.querySelector("[data-input-flow-panel] input")','panel');
    assert.equal(await js('!!document.querySelector("[data-input-wait-countdown], [data-input-panel-countdown]")'),false,'no upfront countdown');
    if(scenario==='timeout'){
      await js('window.fixture.shift(5500)');
      await waitFor('!!document.querySelector("[data-input-wait-countdown]")','delayed model countdown');
      await click('[data-input-flow-panel] input');
      await window.webContents.insertText('保留草稿');
      await waitFor('window.fixture.touches>0 && !document.querySelector("[data-input-wait-countdown]")','interaction renews model wait');
      await js('window.fixture.shift(11000)');
      await waitFor('window.fixture.status()==="timeout" && !document.querySelector("[data-input-wait-countdown]")','model timeout');
      assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),true,'model timeout does not close panel');
      await js('window.fixture.shift(41000)');
      await waitFor('!!document.querySelector("[data-input-panel-countdown]")','independent panel countdown');
      fs.writeFileSync(path.join(output,'countdown.png'),(await window.webContents.capturePage()).toPNG());
      await js('window.fixture.shift(10000)');
    }else if(scenario==='long'){
      await js('window.fixture.shift(61000)');
    }else if(scenario==='restart'){
      await js('window.fixture.restartUI()');
    }else if(scenario==='dismiss'){
      await click('[data-input-flow-header] button');
      await waitFor('window.fixture.status()==="dismissed"','explicit dismiss ends model wait');
    }
    if(['timeout','long','restart','dismiss'].includes(scenario)){
      await waitFor('!document.querySelector("[data-input-flow-panel]")','panel collapsed');
      await waitFor('document.getElementById("transcript").textContent.includes("回答问题")','reopen in original tool row');
      await js(`Array.from(document.querySelectorAll('#transcript button')).find(b=>b.textContent.includes('回答问题')).click()`);
      await waitFor('!!document.querySelector("[data-input-flow-panel] input")','reopened');
      if(scenario==='timeout')assert.equal(await js('document.querySelector("[data-input-flow-panel] input").value'),'保留草稿','draft retained');
      if(scenario==='long')assert.equal(await js('window.fixture.status()'),'waiting','panel does not end long model wait');
    }
    await answer(scenario==='timeout'?'，补充':'测试答复');
    await waitFor('window.fixture.calls.length===1','one answer request');
    const call=await js('window.fixture.calls[0]');
    assert.equal(call.path,'/sessions/form-delivery/input-requests/original-turn%3Aquestion');
    assert.equal(call.body.action,'answer');
    assert.equal(call.body.parts[0].type,'form_result');
    assert.equal(await js('window.fixture.pending().length'),0,'no duplicate user overlay for synchronous answer');
    if(scenario==='reject'){
      await js('window.fixture.respond("temporary_failure")');
      await waitFor('!!document.querySelector("[data-input-flow-panel]") && !!window.fixture.submitError','failure restores form');
      assert.equal(await js('window.fixture.calls.length'),1,'no implicit resend');
      await answer('重试答复');
      await waitFor('window.fixture.calls.length===2','explicit retry');
      assert.equal(await js('window.fixture.calls[1].path'),call.path,'same idempotent question');
    }
    await js('window.fixture.respond()');
    await waitFor('!document.querySelector("[data-input-flow-panel]") && document.getElementById("transcript").textContent.includes("已完成")','answered status');
    assert.equal(await js('window.fixture.delivery'),['timeout','async','dismiss'].includes(scenario)?'message':'tool');
    assert.equal(await js('window.fixture.calls.length'),scenario==='reject'?2:1,'no second delivery');
    fs.writeFileSync(path.join(output,scenario+'.png'),(await window.webContents.capturePage()).toPNG());
    console.log('PASS '+scenario+' lifecycle, request routing and no duplicate delivery');
  }
  assert.deepEqual(errors,[]);
  console.log('REPORT '+output);
}
app.whenReady().then(run).catch(error=>{console.error(error);exitCode=1;}).finally(async()=>{await vite?.close();app.exit(exitCode);});
