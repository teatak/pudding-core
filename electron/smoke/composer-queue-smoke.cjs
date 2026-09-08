// Source Electron + disposable Vite fixture; no daemon, provider or user data.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-queue-smoke-'));
app.setPath('userData',path.join(output,'user-data'));
let vite,window,exitCode=0;
const errors=[];
const js=source=>window.webContents.executeJavaScript(source,true);
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function waitFor(source,label){
 const deadline=Date.now()+15000;
 while(Date.now()<deadline){assert.deepEqual(errors,[]);if(await js(source))return;await delay(30);}
 console.error('STATE',await js('JSON.stringify({value:document.querySelector("textarea")?.value,editing:window.fixture?.store.getState().queueEdits})'));
 throw new Error('Timed out: '+label);
}
async function point(selector){
 return js('(()=>{const e=document.querySelector('+JSON.stringify(selector)+');e.scrollIntoView({block:"nearest"});const r=e.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()');
}
async function click(selector){
 const p=await point(selector);
 for(const type of ['mouseMove','mouseDown','mouseUp'])window.webContents.sendInputEvent({type,...p,button:'left',clickCount:1});
 await delay(50);
}
async function drag(from,to,expectedOrder){
 const a=await point(from),b=await point(to);
 window.webContents.sendInputEvent({type:'mouseMove',...a});
 window.webContents.sendInputEvent({type:'mouseDown',...a,button:'left',clickCount:1});
 for(let i=1;i<=12;i++){window.webContents.sendInputEvent({type:'mouseMove',x:Math.round(a.x+(b.x-a.x)*i/12),y:Math.round(a.y+(b.y-a.y)*i/12),button:'left'});await delay(20);}
 await delay(220);
 await js(`(()=>{
  window.fixture.dropStates=[];
  window.fixture.recordDrop=()=>{
   const rows=[...document.querySelectorAll('[data-queued-input]')].sort((a,b)=>a.getBoundingClientRect().top-b.getBoundingClientRect().top);
   window.fixture.dropStates.push(rows.map(row=>({id:row.dataset.queuedInput,top:row.getBoundingClientRect().top,opacity:getComputedStyle(row.querySelector('button[aria-label="编辑待发送消息"]')).opacity})));
  };
  window.fixture.dropObserver=new MutationObserver(window.fixture.recordDrop);
  window.fixture.dropObserver.observe(document.querySelector('[data-composer-queue]'),{subtree:true,childList:true,attributes:true});
 })()`);
 if(expectedOrder)await js('window.fixture.refreshPending()');
 window.webContents.sendInputEvent({type:'mouseUp',...b,button:'left',clickCount:1});
 const dropAnimations=await js(`(async()=>{
  const found=[];
  for(const start=performance.now();performance.now()-start<250;){
   await new Promise(requestAnimationFrame);
   window.fixture.recordDrop();
   for(const row of document.querySelectorAll('[data-queued-input]')){
    if(row.getAnimations().some(animation=>animation.transitionProperty==='transform')) found.push(row.dataset.queuedInput);
   }
  }
  return [...new Set(found)];
 })()`);
 assert.deepEqual(dropAnimations,[],'releasing the drag does not animate another row exchange');
 const dropStates=await js('(()=>{window.fixture.dropObserver.disconnect();return window.fixture.dropStates})()');
 if(expectedOrder){
  assert.ok(dropStates.length>0);
  for(const state of dropStates){
   assert.deepEqual(state.map(row=>row.id),expectedOrder,'drop never briefly restores the old visual order');
   assert.ok(state.every(row=>row.opacity==='1'),'saving the order does not flash disabled buttons');
  }
 }
}
async function checkDragBounds(dx,dy,expectScroll=false){
 const geometry=()=>js('(()=>{const row=document.querySelector("[data-queued-input=c]"),viewport=row.parentElement;const r=row.getBoundingClientRect(),v=viewport.getBoundingClientRect();return {scrollTop:viewport.scrollTop,scrollWidth:viewport.scrollWidth,scrollHeight:viewport.scrollHeight,clientWidth:viewport.clientWidth,clientHeight:viewport.clientHeight,left:r.left,right:r.right,top:r.top,bottom:r.bottom,viewportLeft:v.left,viewportRight:v.right,viewportTop:v.top,viewportBottom:v.bottom}})()');
 const before=await geometry(),p=await point(dragHandle('c'));
 window.webContents.sendInputEvent({type:'mouseMove',...p});
 window.webContents.sendInputEvent({type:'mouseDown',...p,button:'left',clickCount:1});
 for(let i=1;i<=8;i++){window.webContents.sendInputEvent({type:'mouseMove',x:p.x+Math.round(dx*i/8),y:p.y+Math.round(dy*i/8),button:'left'});await delay(20);}
 await delay(100);
 const during=await geometry();
 window.webContents.sendInputEvent({type:'keyDown',keyCode:'Escape'});
 window.webContents.sendInputEvent({type:'keyUp',keyCode:'Escape'});
 window.webContents.sendInputEvent({type:'mouseUp',x:p.x+dx,y:p.y+dy,button:'left',clickCount:1});
 await delay(100);
 console.log('DRAG BOUNDS',JSON.stringify({dx,dy,before,during}));
 assert.equal(during.scrollWidth,before.scrollWidth,'drag does not create horizontal overflow');
 assert.equal(during.scrollHeight,before.scrollHeight,'drag does not create vertical overflow');
 assert.ok(during.left>=during.viewportLeft && during.right<=during.viewportRight,'row stays within horizontal viewport');
 assert.ok(during.top>=during.viewportTop && during.bottom<=during.viewportBottom,'row stays within vertical viewport');
 if(expectScroll)assert.ok(during.scrollTop>before.scrollTop,'long queues still auto-scroll at the edge');
}
async function replaceText(value){
 await click('textarea');
 await js('document.querySelector("textarea").select()');
 await window.webContents.insertText(value);
}
const row=id=>'[data-queued-input="'+id+'"]';
const edit=id=>row(id)+' [aria-label="编辑待发送消息"]';
const dragHandle=id=>row(id)+' [aria-label^="拖拽"]';
async function run(){
 assert.equal(process.execPath,path.join(repo,'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
 const {createServer}=await import(pathToFileURL(path.join(repo,'web/node_modules/vite/dist/node/index.js')));
 vite=await createServer({root:path.join(repo,'web'),cacheDir:path.join(output,'vite-cache'),logLevel:'error',server:{host:'127.0.0.1',port:0},plugins:[{
 name:'queue-fixture',resolveId(id){if(id==='/__queue.js')return '\0queue-fixture'},
 load(id){if(id==='\0queue-fixture')return `
import React,{useState} from 'react';
import {createRoot} from 'react-dom/client';
import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {createRootRoute,createRoute,createRouter,createMemoryHistory,RouterProvider} from '@tanstack/react-router';
import '/src/styles.css';
import {Composer} from '/src/components/Composer.tsx';
import {TranscriptTurn} from '/src/components/transcript/TranscriptTurn.tsx';
import {useTranscriptData} from '/src/components/transcript/useTranscriptData.ts';
import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
import {useOverlayStore} from '/src/state/overlayStore.ts';
import {useSessionDraftStore} from '/src/state/sessionDraftStore.ts';
import {queryKeys} from '/src/api/queryKeys.ts';
import {sessionEvent} from '/contracts/events.ts';
import {setLocale} from '/src/i18n/index.ts';
setLocale('zh-CN');
const h=React.createElement,sessionID='queue-fixture',turnID='original-turn',createdAt='2026-09-08T10:00:00Z';
const client=new QueryClient({defaultOptions:{queries:{enabled:false,retry:false},mutations:{retry:false}}});
const store=useOverlayStore.getState();
let seq=0,failNext=false,steering;
const event=value=>store.applyEvent(sessionEvent.parse({sessionID,seq:++seq,...value}));
const text=value=>({type:'text',text:value});
const image={type:'attachment',id:'image',name:'fixture.png',mime:'image/png',size:68,attachmentKey:'image',url:'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=='};
let queue=['a','b','c'].map((id,i)=>({sessionID,clientMessageID:id,text:['第一条任务','带图片的第二条任务','第三条任务'][i],parts:i===1?[image,text('带图片的第二条任务')]:[text(['第一条任务','','第三条任务'][i])],status:'queued',createdAt,updatedAt:createdAt}));
const message=(id,role,parts,clientMessageID)=>({id,role,parts,clientMessageID,sessionID,turnID,createdAt});
const original=message('original','user',[text('当前任务')],'original');
let turns=[{id:turnID,sessionID,clientMessageID:'original',createdAt,updatedAt:createdAt,status:'running',messages:[original]}];
const canonical=()=>client.setQueryData(queryKeys.turns(sessionID),{pages:[{turns,hasMore:false}],pageParams:[undefined]});
const snapshot=()=>client.setQueryData(queryKeys.queuedInputs(sessionID),{queuedInputs:queue.map(q=>({...q}))});
canonical(); snapshot();
client.setQueryData(queryKeys.queuedInputs('other'),{queuedInputs:[]});
client.setQueryData(queryKeys.turns('other'),{pages:[{turns:[],hasMore:false}],pageParams:[undefined]});
useSessionDraftStore.getState().setText(sessionID,'未发送的原草稿');
useSessionDraftStore.getState().setAttachments(sessionID,[{id:'draft-image',name:'draft.png',size:68,status:'uploaded',attachment:{...image,id:'draft-image',name:'draft.png'}}]);
event({kind:'turn.started',turnID,clientMessageID:'original',userMessageID:'original'});
event({kind:'turn.delta',turnID,part:'text',delta:'正在执行当前任务。'});
const originalFetch=window.fetch;
window.fetch=async(url,options)=>{
 if(!String(url).startsWith('/sessions/')) return originalFetch(url,options);
 const body=JSON.parse(options.body),path=String(url);
 window.fixture.calls.push({path,body});
 await new Promise(r=>setTimeout(r,120));
 if(failNext){failNext=false;snapshot();return new Response(JSON.stringify({error:'queued_inputs_changed'}),{status:409});}
 let result;
 if(path.endsWith('/reorder')) {
  queue=body.clientMessageIDs.map(id=>queue.find(q=>q.clientMessageID===id));result={queuedInputs:queue};
 } else {
  const id=path.split('/queued-inputs/')[1].split('/')[0],input=queue.find(q=>q.clientMessageID===id);
  if(path.endsWith('/steer')) {steering=input;result={turnID,userMessageID:'guide-'+id};}
  else {
   Object.assign(input,body);
   event({kind:'input.updated',clientMessageID:id,status:input.status,text:input.text});
   result={...input};queue=queue.filter(q=>q.status!=='cancelled');
  }
 }
 snapshot();
 return new Response(JSON.stringify(result),{status:200,headers:{'Content-Type':'application/json'}});
};
window.fixture={calls:[],
 refreshPending(){
  const queuedInputs=queue.map(input=>({...input}));
  void client.fetchQuery({queryKey:queryKeys.queuedInputs(sessionID),queryFn:()=>new Promise(resolve=>setTimeout(()=>resolve({queuedInputs}),180))});
 },
 pendingNew(){store.addPendingUser({sessionID,clientMessageID:'unconfirmed',text:'刚添加的消息',parts:[text('刚添加的消息')],status:'queued',createdAt});},
 clearPending(){store.removePendingUser(sessionID,'unconfirmed');},
 longQueue(){queue.push(...Array.from({length:12},(_,i)=>({sessionID,clientMessageID:'long-'+i,text:'后续任务 '+i,parts:[text('后续任务 '+i)],status:'queued',createdAt,updatedAt:createdAt})));snapshot();},
 fail(){failNext=true},queue:()=>queue,store:useSessionDraftStore,
 apply(){
  const input=steering;
  event({kind:'input.steered',turnID,clientMessageID:input.clientMessageID,userMessageID:'guide-'+input.clientMessageID,text:input.text});
  queue=queue.filter(q=>q!==input); snapshot();
  turns=[{...turns[0],messages:[original,message('before','assistant',[text('正在执行当前任务。')]),message('guide-'+input.clientMessageID,'user',input.parts,input.clientMessageID)]}];canonical();
 },
};
function Fixture(){
 const [selected,setSelected]=useState(sessionID);window.fixture.select=setSelected;
 const {transcript}=useTranscriptData({token:'',sessionID:selected,sessionRunning:selected===sessionID});
 return h('main',{style:{height:'100vh',display:'flex',flexDirection:'column',padding:'24px 24px 0'}},
 h('div',{id:'transcript',style:{flex:1,overflow:'auto'}},transcript.turnVMs.map(turn=>h(TranscriptTurn,{key:turn.key,turn,sessionID:selected,token:''}))),
 h(Composer,{session:{id:selected,title:'队列测试',provider:'fixture',model:'fixture-model',activeMode:'chat',running:selected===sessionID},token:''}));
}
const rootRoute=createRootRoute(),indexRoute=createRoute({getParentRoute:()=>rootRoute,path:'/',component:Fixture});
const router=createRouter({routeTree:rootRoute.addChildren([indexRoute]),history:createMemoryHistory({initialEntries:['/']})});
createRoot(document.getElementById('root')).render(h(QueryClientProvider,{client},h(TooltipProvider,null,h(RouterProvider,{router}))));
`;},
 configureServer(server){server.middlewares.use('/__fixture',async(_req,res,next)=>{try{res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture','<!doctype html><html><head><title>Composer queue regression</title></head><body><div id="root"></div><script type="module" src="/__queue.js"></script></body></html>'));}catch(error){next(error);}});}
 }]});
 await vite.listen();
 window=new BrowserWindow({width:800,height:750,show:false,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
 window.webContents.on('console-message',details=>{if(details.level==='error')errors.push(details.message)});
 const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture';
 await window.loadURL(url);assert.equal(window.webContents.getURL(),url);
 await waitFor('document.querySelectorAll("[data-queued-input]").length===3','three queue rows');
 assert.equal(await js('document.getElementById("transcript").textContent.includes("第一条任务")'),false);
 assert.equal(await js('document.querySelector("textarea").value'),'未发送的原草稿');
 assert.equal(await js('document.querySelectorAll("[data-composer-queue] img").length'),1);
 await checkDragBounds(180,0);
 await checkDragBounds(0,100);
 const gripStyle=()=>js('(()=>{const s=getComputedStyle(document.querySelector("[data-queued-input=a] button"));return {color:s.color,opacity:s.opacity}})()');
 assert.deepEqual(await js('[...document.querySelectorAll("[data-queued-input]")].map(e=>e.getBoundingClientRect().height)'),[32,32,32]);
 const originalGrip=await gripStyle();
 await js('window.fixture.pendingNew()');
 await waitFor('document.querySelectorAll("[data-queued-input]").length===4','unconfirmed append');
 assert.equal(await js('document.querySelector("[data-queued-input=a] button").disabled'),true);
 assert.deepEqual(await gripStyle(),originalGrip,'append does not flash existing drag handles');
 await js('window.fixture.clearPending()');
 await waitFor('!document.querySelector("[data-queued-input=a] button").disabled','sorting unlocked');
 fs.writeFileSync(path.join(output,'queue-light.png'),(await window.webContents.capturePage()).toPNG());
 await js('document.documentElement.classList.add("dark")');await delay(150);
 fs.writeFileSync(path.join(output,'queue-dark.png'),(await window.webContents.capturePage()).toPNG());
 await drag(dragHandle('c'),dragHandle('a'),['c','a','b']);
 await waitFor('window.fixture.queue()[0].clientMessageID==="c"','durable reorder request');
 assert.deepEqual(await js('window.fixture.calls[0].body.clientMessageIDs'),['c','a','b']);
 assert.equal(await js('window.fixture.calls[0].path'),'/sessions/queue-fixture/queued-inputs/reorder');
 await click(edit('b'));
 await waitFor('!!document.querySelector("[data-queue-editor]")','composer edit mode');
 await replaceText('编辑后的第二条');
 await js('window.fixture.select("other")');
 await waitFor('!document.querySelector("[data-composer-queue]")','other session has no queue');
 await js('window.fixture.select("queue-fixture")');
 await waitFor('document.querySelector("textarea").value==="编辑后的第二条"','edit draft survives session switch');
 await click('[data-queue-editor] button[type=submit]');
 await waitFor('!document.querySelector("[data-queue-editor]")','save complete');
 assert.equal(await js('document.querySelector("textarea").value'),'未发送的原草稿');
 assert.equal(await js('window.fixture.store.getState().drafts["queue-fixture"].attachments[0].name'),'draft.png');
 assert.equal(await js('window.fixture.queue()[2].text'),'编辑后的第二条');
 assert.equal(await js('window.fixture.queue()[2].parts[0].id'),'image');
 await click(edit('a'));await waitFor('!!document.querySelector("[data-queue-editor]")','second edit');
 await replaceText('不保存的改动');
 await click('[data-queue-editor] button[type=button]');
 await waitFor('!document.querySelector("[data-queue-editor]")','cancel complete');
 assert.equal(await js('window.fixture.queue()[1].text'),'第一条任务');
 assert.equal(await js('document.querySelector("textarea").value'),'未发送的原草稿');
 await js('window.fixture.fail()');
 await drag(dragHandle('c'),dragHandle('b'));
 await waitFor('document.querySelector("[data-queued-input]").dataset.queuedInput==="c"','failed reorder restores server order');
 await click(row('b')+' [aria-label="立即引导"]');
 await waitFor('document.querySelector("[data-queued-input=b]").textContent.includes("正在引导")','steering stays in queue');
 assert.equal(await js('document.getElementById("transcript").textContent.includes("编辑后的第二条")'),false);
 await waitFor('window.fixture.calls.at(-1).path.endsWith("/b/steer")','steer route');
 await delay(250);await js('window.fixture.apply()');
 await waitFor('!document.querySelector("[data-queued-input=b]") && document.getElementById("transcript").textContent.includes("编辑后的第二条")','accepted steer appears once in transcript');
 await click(row('a')+' [aria-label="取消待发送消息"]');
 await waitFor('document.querySelectorAll("[data-queued-input]").length===1','delete queue item');
 window.setSize(460,680);await delay(200);
 await js('document.documentElement.classList.add("dark")');
 await delay(150);
 fs.writeFileSync(path.join(output,'queue-dark-narrow.png'),(await window.webContents.capturePage()).toPNG());
 assert.ok(await js('document.querySelector("[data-composer-queue]").scrollWidth<=document.querySelector("[data-composer-queue]").clientWidth'),'no horizontal overflow');
 await js('window.fixture.longQueue()');
 await waitFor('document.querySelectorAll("[data-queued-input]").length===13','long scrollable queue');
 await checkDragBounds(0,260,true);
 assert.deepEqual(errors,[]);
 console.log('PASS queue dock, pointer sorting, stable drop order/appearance with pending refresh, conflict recovery, composer save/cancel, attachment/draft preservation, session isolation, steer reconciliation, deletion; REPORT '+output);
}
app.whenReady().then(run).catch(error=>{console.error(error);exitCode=1}).finally(async()=>{await vite?.close();app.exit(exitCode)});
