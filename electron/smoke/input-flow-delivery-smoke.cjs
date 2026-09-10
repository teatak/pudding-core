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
async function typeKeys(text) {
  // insertText alone bypasses the native number-key path that lost user input.
  for(const keyCode of text) {
    for(const type of ['keyDown','char','keyUp']) window.webContents.sendInputEvent({type,keyCode});
    await delay(60);
  }
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
        import React,{useState} from 'react';
        import {createRoot} from 'react-dom/client';
        import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
        import {createRootRoute,createRoute,createRouter,createMemoryHistory,RouterProvider} from '@tanstack/react-router';
        import '/src/styles.css';
        import {Composer} from '/src/components/Composer.tsx';
        import {TurnParts} from '/src/components/transcript/TurnParts.tsx';
        import {TranscriptTurn} from '/src/components/transcript/TranscriptTurn.tsx';
        import {useTranscriptViewModel} from '/src/components/transcript/useTranscriptViewModel.ts';
        import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
        import {useOverlayStore} from '/src/state/overlayStore.ts';
        import {useInputFlowStore} from '/src/state/inputFlowStore.ts';
        import {createInputFlowTools} from '/src/mcp/inputFlowTools.ts';
        import {queryKeys} from '/src/api/queryKeys.ts';
        import {useSessionEvents} from '/src/hooks/useSessionEvents.ts';
        import {conversationTurn} from '/contracts/api.ts';
        import {setLocale} from '/src/i18n/index.ts';
        setLocale('zh-CN');
        const h=React.createElement,sessionID='form-delivery',turnID='original-turn',requestID=turnID+':question';
        const scenario=location.search.slice(1),queueScenario=scenario.startsWith('queue-'),waitSeconds=scenario==='async'||queueScenario?0:scenario==='long'?300:scenario==='number-repeat'?180:10;
        const hours={id:'hours',placeholder:'每周几小时？',title:'这项技能每周打算投入几小时？',type:'number_input'};
        const args=scenario==='number-repeat'?{
          type:'repeat',title:'技能学习计划 📚',waitSeconds,maxItems:3,
          repeatSteps:[{id:'skill',options:['Python','吉他','游泳','摄影','做饭'],title:'想学的技能',type:'single_select'},hours],
          afterItem:{title:'还想继续添加技能吗？',actions:[{id:'continue',label:'再加一个'},{id:'done',label:'就这些'}]},
        }:{type:'form',title:'反馈确认',waitSeconds,steps:[scenario==='number-form'?hours:{id:'reply',type:'text_input',title:'你有什么建议？'}]};
        const client=new QueryClient({defaultOptions:{queries:{enabled:false,retry:false},mutations:{retry:false}}});
        const realNow=Date.now;let offset=0,resolveResponse,refresh,history=[],queuedInputs=[];
        Date.now=()=>realNow()+offset;
        let request={id:requestID,sessionID,turnID,title:args.title,args,status:waitSeconds?'waiting':'awaiting_user',...(waitSeconds?{deadline:new Date(Date.now()+waitSeconds*1000).toISOString()}:{})};
        function snapshot(){if(request.status==='waiting'&&Date.parse(request.deadline)<=Date.now())request={...request,status:'timeout',deadline:undefined};return request;}
        client.setQueryData(queryKeys.queuedInputs(sessionID),{queuedInputs:[]});
        const originalFetch=window.fetch;
        if(queueScenario) window.EventSource=class extends EventTarget {
          constructor(){super();window.fixture.source=this;}
          close(){}
        };
        window.fetch=(url,options)=>{
          if(!String(url).startsWith('/sessions/'))return originalFetch(url,options);
          if(queueScenario && String(url)==='/sessions/'+sessionID+'/approvals')return Promise.resolve(Response.json({approvals:[]}));
          if(queueScenario && String(url)==='/sessions/'+sessionID+'/queued-inputs')return Promise.resolve(Response.json({queuedInputs}));
          if(queueScenario && options?.method==='PATCH' && String(url)==='/sessions/'+sessionID+'/queued-inputs/'+encodeURIComponent('input-flow-'+requestID)){
            const patch=JSON.parse(options.body);if(patch.status!=='cancelled')throw new Error('Unexpected queue mutation');
            return Promise.resolve(Response.json(window.fixture.withdraw(false)));
          }
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
            const toolStatus=snapshot().status==='waiting'?'answered':snapshot().status;
            if(!error){
              request={...request,status:'answered',deadline:undefined};
              const answer=this.calls.at(-1).body,createdAt=new Date().toISOString();
              const message=(id,role,parts,index,text='')=>({id,sessionID,turnID,role,kind:'text',parts,turnIndex:index,text,createdAt});
              history=[
                {...message('initial','user',[{type:'text',text:'请收集我的建议。'}],-1,'请收集我的建议。'),clientMessageID:'initial'},
                message('question-call','assistant',[{type:'tool_use',id:'question',name:'builtin_request_user_input',args}],0),
                message('question-result','tool',[{type:'tool_result',id:'question',name:'builtin_request_user_input',ok:true,content:JSON.stringify({requestID,status:toolStatus})}],1),
                {...message('answer','user',answer.parts,2,answer.text),clientMessageID:'input-flow-'+requestID},
                message('continued','assistant',[{type:'text',text:'已收到你的回答。'}],3,'已收到你的回答。')
              ];
              if(queueScenario){
                queuedInputs=[{sessionID,clientMessageID:'input-flow-'+requestID,text:answer.text,parts:answer.parts,status:'queued',createdAt,updatedAt:createdAt}];
                history=history.slice(0,3); // Queued answers are not canonical messages yet.
                history[2].parts[0].content=JSON.stringify({requestID,status:'awaiting_user'});
              }
              refresh();
            }
            resolveResponse(Response.json(error?{error}:queueScenario?{request,queued:true,status:'queued',clientMessageID:'input-flow-'+requestID}:{request,turnID,userMessageID:'answer'},{status:error?409:200}));
          },
          withdraw(remote){
            const input={...queuedInputs[0],status:'cancelled'};queuedInputs=[];request={...request,status:'awaiting_user'};
            if(remote)this.source.dispatchEvent(new MessageEvent('input.updated',{data:JSON.stringify({kind:'input.updated',seq:1,sessionID,clientMessageID:input.clientMessageID,status:'cancelled'})}));
            return input;
          },
          shift(ms){offset+=ms;client.invalidateQueries({queryKey:queryKeys.inputRequest(sessionID,requestID)});},
          restartUI(){useInputFlowStore.setState({requests:[],drafts:{}});client.removeQueries({queryKey:queryKeys.inputRequest(sessionID,requestID)});},
          pending(){return useOverlayStore.getState().pendingUsers[sessionID]||[];},
          status(){return snapshot().status;},
          deadline(){return snapshot().deadline;},
          reloadTranscript(){history=JSON.parse(JSON.stringify(history));refresh();},
        };
        function History(){
          const createdAt=new Date().toISOString();
          const turn=conversationTurn.parse({id:turnID,sessionID,clientMessageID:'initial',status:'completed',createdAt,updatedAt:createdAt,messages:history});
          const {turnVMs}=useTranscriptViewModel({sessionID,sessionRunning:false,assistantOverlays:[],pendingUsers:[],turnDurationByID:new Map(),turns:[turn]});
          return turnVMs.map(turn=>h(TranscriptTurn,{key:turn.key,turn,token:'',sessionID}));
        }
        function Fixture(){
          const [,update]=useState(0);refresh=()=>update(n=>n+1);
          useSessionEvents(scenario==='queue-sse'?sessionID:undefined,'fixture-token');
          return h('main',{style:{padding:24}},
            h('div',{id:'transcript',style:{height:370,overflow:'auto'}},history.length?h(History):h(TurnParts,{token:'',sessionID,disclosureRootKey:'fixture',parts:[{type:'tool_use',id:'question',turnID,name:'builtin_request_user_input',args,phase:'running'}]})),
            h(Composer,{session:{id:sessionID,title:'答复投递测试',provider:'fixture',model:'fixture-model',activeMode:'chat',running:true},token:queueScenario?'fixture-token':'',onSubmitError:error=>window.fixture.submitError=error}));
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
  for(const scenario of ['queue-withdraw','queue-sse']){
    await window.loadURL(url+'?'+scenario);
    assert.equal(window.webContents.getURL(),url+'?'+scenario);
    await answer('旧答复');
    await waitFor('window.fixture.calls.length===1','first late answer');
    await js('window.fixture.respond()');
    await waitFor('!!document.querySelector("[data-queued-input]") && document.getElementById("transcript").textContent.includes("已完成")','accepted queued answer');
    assert.equal(await js('document.querySelectorAll("#transcript .pudding-user-message").length'),1,'queued answer is not a canonical bubble');
    if(scenario==='queue-sse')await js('window.fixture.withdraw(true)');
    else await click('[data-queued-input] button[aria-label="取消待发送消息"]');
    await waitFor('!document.querySelector("[data-queued-input]")','withdrawal removes queue entry');
    assert.equal(await js('window.fixture.status()'),'awaiting_user','authority permits re-answer after withdrawal');
    await waitFor('document.getElementById("transcript").textContent.includes("回答问题")','withdrawal restores reopen action without navigation');
    fs.writeFileSync(path.join(output,scenario+'-reopen.png'),(await window.webContents.capturePage()).toPNG());
    await js(`Array.from(document.querySelectorAll('#transcript button')).find(b=>b.textContent.includes('回答问题')).click()`);
    await answer('新答复');
    await waitFor('window.fixture.calls.length===2','new answer');
    const calls=await js('window.fixture.calls');
    assert.equal(calls[1].path,calls[0].path,'re-answer retains question identity');
    assert.equal(calls[1].body.parts[0].result.reply,'新答复');
    await js('window.fixture.respond()');
    await waitFor('document.querySelector("[data-composer-queue]")?.textContent.includes("新答复")','new answer enters queue');
    assert.equal(await js('document.querySelectorAll("[data-queued-input]").length'),1,'one replacement queue item');
    console.log('PASS '+scenario+' authoritative refresh, immediate reopen and replacement answer');
  }
  for(const scenario of ['number-repeat','number-form']) {
    await window.loadURL(url+'?'+scenario);
    assert.equal(window.webContents.getURL(),url+'?'+scenario);
    const repeat=scenario==='number-repeat',input='[data-input-flow-panel] input';
    if(repeat) await click('[data-input-flow-panel] [role=option]');
    await waitFor('document.activeElement?.type==="number"','numeric input focused');
    assert.equal(await js('!!document.querySelector("[data-input-wait-countdown], [data-input-panel-countdown]")'),false,'numeric panel has no upfront countdown');
    await js('window.fixture.shift('+(repeat?51000:5500)+')');
    const countdown=repeat?'[data-input-panel-countdown]':'[data-input-wait-countdown]';
    await waitFor('!!document.querySelector('+JSON.stringify(countdown)+')','countdown before numeric typing');
    const deadline=await js('window.fixture.deadline()');
    await typeKeys(repeat?'5':'12');
    await delay(600); // Include timer ticks and the touch response, not just the key event.
    assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),repeat?'5':'12','native digit keys must survive lifecycle refresh');
    await waitFor('!document.querySelector('+JSON.stringify(countdown)+')','typing clears countdown');
    assert.ok(Date.parse(await js('window.fixture.deadline()'))>Date.parse(deadline),'typing renews the active model wait');
    assert.equal(await js('document.querySelector("[data-input-flow-entry] button").disabled'),false);
    fs.writeFileSync(path.join(output,scenario+'-entry.png'),(await window.webContents.capturePage()).toPNG());
    if(repeat) {
      // Crossing the old panel deadline must not close an actively edited form.
      await js('window.fixture.shift(11000)');
      await delay(300);
      assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),'5');
      await click('[data-input-flow-entry] button');
      await click('[data-input-flow-panel] [role=option]'); // Continue collecting.
      await waitFor('document.querySelector("[data-input-flow-step-title]")?.textContent==="想学的技能"','next record');
      await click('[data-input-flow-panel] [role=option]:nth-child(2)'); // Guitar.
      await waitFor('document.activeElement?.type==="number"','next numeric field');
      assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),'','no previous-record draft');
      await typeKeys('2.5');
      await delay(600);
      assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),'2.5','decimal typing survives timer ticks');
      await click('[data-input-flow-entry] button');
      await click('[data-input-flow-panel] [role=option]:nth-child(2)'); // Done.
    } else {
      for(const type of ['keyDown','keyUp']) window.webContents.sendInputEvent({type,keyCode:'Backspace'});
      await delay(100);
      assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),'1','backspace edits the numeric draft');
      await window.webContents.insertText('2');
      await delay(300);
      assert.equal(await js('document.querySelector('+JSON.stringify(input)+').value'),'12','text insertion remains supported');
      for(const type of ['keyDown','keyUp']) window.webContents.sendInputEvent({type,keyCode:'Return'});
    }
    await waitFor('window.fixture.calls.length===1','one numeric answer');
    assert.deepEqual(await js('window.fixture.calls[0].body.parts[0].result'),repeat?{
      type:'user_input_result',title:'技能学习计划 📚',items:[{skill:'Python',skillLabel:'Python',hours:5},{skill:'吉他',skillLabel:'吉他',hours:2.5}],
    }:{type:'user_input_result',title:'反馈确认',hours:12});
    await js('window.fixture.respond()');
    await waitFor('document.querySelectorAll("#transcript .pudding-user-message").length===2','numeric result user bubble');
    assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),false,'submitted panel closes');
    fs.writeFileSync(path.join(output,scenario+'-answer.png'),(await window.webContents.capturePage()).toPNG());
    console.log('PASS '+scenario+' native typing, timer renewal and numeric answer bubble');
  }
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
    assert.equal(await js('window.fixture.calls.length'),scenario==='reject'?2:1,'no second delivery');
    await waitFor('document.querySelectorAll("#transcript .pudding-user-message").length===2','initial user message and one canonical answer bubble');
    assert.equal(await js('Array.from(document.querySelectorAll("#transcript .pudding-user-message")).at(-1).textContent.includes("'+(scenario==='timeout'?'保留草稿':'测试答复')+'")'),true,'form answer is visible');
    assert.equal(await js(`(()=>{const bubble=Array.from(document.querySelectorAll('#transcript .pudding-user-message')).at(-1),tool=document.querySelector('#transcript details'),continued=Array.from(document.querySelectorAll('#transcript p')).find(p=>p.textContent==='已收到你的回答。');return !!(tool.compareDocumentPosition(bubble)&Node.DOCUMENT_POSITION_FOLLOWING)&&!!(bubble.compareDocumentPosition(continued)&Node.DOCUMENT_POSITION_FOLLOWING);})()`),true,'tool result → user answer → assistant');
    await js('window.fixture.reloadTranscript()');
    await waitFor('document.querySelectorAll("#transcript .pudding-user-message").length===2','history reload preserves one answer bubble');
    if(scenario==='sync'){
      await click('#transcript details > summary');
      await waitFor('document.querySelector("#transcript details").open','expanded tool');
      await click('#transcript details details > summary');
      await waitFor('!!document.querySelector("#transcript pre")','expanded raw data');
      const layout=await js(`(()=>{const summary=document.querySelector('#transcript details>summary'),action=summary.querySelector('[data-transcript-header-action]'),s=summary.getBoundingClientRect(),a=action.getBoundingClientRect();return {inside:summary.contains(action),offset:a.x-s.x,dy:Math.abs(a.y-s.y),text:action.textContent};})()`);
      assert.equal(layout.inside,true);
      assert.ok(layout.offset<250 && layout.dy<10,JSON.stringify(layout));
      assert.equal(layout.text,'已完成');
      fs.writeFileSync(path.join(output,'sync-expanded.png'),(await window.webContents.capturePage()).toPNG());
      await click('#transcript details > summary');
      await waitFor('!document.querySelector("#transcript details").open','collapsed tool');
    }
    fs.writeFileSync(path.join(output,scenario+'.png'),(await window.webContents.capturePage()).toPNG());
    console.log('PASS '+scenario+' lifecycle, request routing and no duplicate delivery');
  }
  assert.deepEqual(errors,[]);
  console.log('REPORT '+output);
}
app.whenReady().then(run).catch(async error=>{
  console.error(error);exitCode=1;
  if(window && !window.isDestroyed()){
    fs.writeFileSync(path.join(output,'failure.png'),(await window.webContents.capturePage()).toPNG());
    fs.writeFileSync(path.join(output,'failure.html'),await js('document.body.innerHTML'));
  }
  console.log('REPORT '+output);
}).finally(async()=>{await vite?.close();app.exit(exitCode);});
