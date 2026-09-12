// Run after npm --prefix web run build, using the repository's Electron binary.
// Uses only temporary data and a local Vite fixture.
const assert = require('node:assert/strict');
const os = require('node:os');
const fs = require('node:fs');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const out = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-stream-smoke-'));
app.setPath('userData', path.join(out, 'user-data'));
let vite, win, exitCode = 0;
const errors = [];
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const js = source => win.webContents.executeJavaScript(source, true);
const fixture = `
import React, {Profiler,useState} from 'react';
import {createRoot} from 'react-dom/client';
import {useShallow} from 'zustand/react/shallow';
import '/src/styles.css';
import {TranscriptList} from '/src/components/transcript/TranscriptList.tsx';
import {useTranscriptViewModel} from '/src/components/transcript/useTranscriptViewModel.ts';
import {useOverlayStore} from '/src/state/overlayStore.ts';
import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
import {requestCodeHighlight} from '/src/lib/shiki.ts';
import {createSessionEventBatcher} from '/src/lib/sessionEventBatcher.ts';
import {setLocale} from '/src/i18n/index.ts';
setLocale('zh-CN');
const h=React.createElement, id='profile-session',turnID='profile-turn';
const searchState={terms:[]},empty=[],durations=new Map(),noop=()=>{};
const spec=JSON.parse(new URLSearchParams(location.search).get('spec'));

const disclosureState=new Map();
const disclosure={isOpen:key=>disclosureState.get(key)??!!spec.open,hasState:key=>disclosureState.has(key),setOpen:(key,open)=>disclosureState.set(key,open)};
window.__stats={commits:[],ticks:[],events:[],inputs:[],longTasks:[]};
const stats=window.__stats;
new PerformanceObserver(list=>stats.longTasks.push(...list.getEntries().map(e=>e.duration))).observe({type:'longtask'});
new PerformanceObserver(list=>stats.inputs.push(...list.getEntries().map(e=>({name:e.name,delay:e.processingStart-e.startTime,duration:e.duration})))).observe({type:'event',durationThreshold:16});
const code=Array.from({length:spec.lines||0},(_,i)=>'const item'+i+' = {name: "sample", value: calculate('+i+', [1, 2, 3])};').join('\\n');
const prefix=spec.kind==='code' ? '\u0060\u0060\u0060typescript\\n'+code+(spec.growing?'':'\\n\u0060\u0060\u0060\\n\\n') : spec.kind==='markdown' ? ('### A sample heading\\n\\nA paragraph with **bold** and a [link](https://example.invalid).\\n\\n- One item\\n- Another item\\n\\n').repeat(spec.lines||0) : '思考下一步如何实现并验证逻辑。'.repeat(spec.lines||0);
const phase=spec.kind==='thought'?'thinking':'streaming_text';
const initialParts=spec.priorCode ? [{type:'text',text:'\u0060\u0060\u0060typescript\\n'+code+(spec.growing?'':'\\n\u0060\u0060\u0060\\n\\n')},{type:'thought',text:prefix}] : [{type:spec.kind==='thought'?'thought':'text',text:prefix}];
if(spec.tools) initialParts.unshift(...Array.from({length:spec.tools},(_,i)=>({type:'tool',callID:'call-'+i,name:'builtin_file_read',argsText:JSON.stringify({path:'src/example'+i+'.ts'}),phase:'ok',resultOk:true,resultContent:JSON.stringify({path:'src/example'+i+'.ts',content:'const value = 1;'.repeat(1300),lineCount:1300})})));
const canonical=Array.from({length:40},(_,i)=>({id:'old-'+i,sessionID:id,status:'completed',clientMessageID:'old-input-'+i,messages:[{id:'user-'+i,role:'user',parts:[{type:'text',text:'历史消息 '+i}],createdAt:'2026-09-12T00:00:00Z'},{id:'answer-'+i,role:'assistant',parts:[{type:'text',text:'历史回复。'.repeat(12)}],createdAt:'2026-09-12T00:00:00Z'}]}));
useOverlayStore.setState({assistants:{[turnID]:{sessionID:id,turnID,status:'streaming',text:spec.kind==='thought'?'':prefix,parts:initialParts}},turnPhases:{[id]:{sessionID:id,turnID,phase,updatedAt:new Date().toISOString()}},runningTurns:{[id]:turnID}});
function Fixture(){
 const [viewport,setViewport]=useState(null);
 const assistantOverlays=useOverlayStore(useShallow(s=>Object.values(s.assistants)));
 const turnPhase=useOverlayStore(s=>s.turnPhases[id]);
 const transcript=useTranscriptViewModel({assistantOverlays,pendingUsers:empty,sessionID:id,sessionRunning:true,turnDurationByID:durations,turnPhase,turns:canonical});
 return h(TooltipProvider,null,h(Profiler,{id:'transcript',onRender:(_id,_phase,duration)=>stats.commits.push(duration)},h('div',{id:'viewport',ref:setViewport,style:{height:510,overflow:'auto',overflowAnchor:'none',contain:'strict',padding:'0 24px','--pudding-composer-mask-height':'24px','--pudding-composer-overlay-height':'80px'}},h(TranscriptList,{turns:transcript.turnVMs,scrollElement:viewport,sessionID:id,searchSlot:'primary',searchState,token:'',hasMoreHistory:false,isLoadingHistory:false,jumpLatestSignal:0,onLoadHistory:noop,disclosure}))),h('textarea',{id:'input',style:{width:'90%',margin:20},placeholder:'Isolated input latency measurement'}));
}
await new Promise(resolve=>requestCodeHighlight('const warm = 1;', 'ts', resolve));
createRoot(document.getElementById('root')).render(h(Fixture));
window.__deltaCount=0;
window.__batch=createSessionEventBatcher(event=>useOverlayStore.getState().applyEvent(event));
window.__start=()=>{
 for(const v of Object.values(stats)) v.length=0;
 let previous=performance.now();
 window.__tick=setInterval(()=>{const now=performance.now();stats.ticks.push(now-previous);previous=now;},16);
 window.__stream=setInterval(()=>{const start=performance.now();window.__batch.push({kind:'turn.delta',sessionID:id,turnID,part:spec.kind==='thought'?'thought':'text',delta:spec.growing?'\\nconst streamed'+(++window.__deltaCount)+' = 123;':' 新的内容',seq:1});stats.events.push(performance.now()-start);},20);
 document.getElementById('input').focus();
};
window.__stop=()=>{clearInterval(window.__tick);clearInterval(window.__stream);window.__batch.flush();return {...stats,nodes:document.querySelectorAll('*').length,chars:prefix.length,inputLength:document.getElementById('input').value.length};};
window.__latestCode=()=>document.querySelector('.code-block-scroll code')?.textContent;
window.__expectedCode=()=>useOverlayStore.getState().assistants[turnID].parts[0].text.replace(/^\u0060\u0060\u0060typescript\\n/,'');
window.__ready=true;
`;
function summary(values){const sorted=[...values].sort((a,b)=>a-b);return {n:values.length,mean:values.reduce((a,b)=>a+b,0)/(values.length||1),p95:sorted[Math.floor(sorted.length*.95)]||0,max:sorted.at(-1)||0};}
async function waitFor(predicate,label){
  for(let i=0;i<200;i++){if(await predicate())return;await delay(50);}
  throw Error('Timed out: '+label);
}
async function click(selector){
  // Enter history-reading mode before navigating an expanded group; otherwise
  // the virtual list's follow-latest frame can move the target before mouseDown.
  await js(`document.getElementById('viewport').dispatchEvent(new WheelEvent('wheel',{deltaY:-100,bubbles:true}));document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block:'center'})`);
  await js('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))');
  const point=await js(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
  win.webContents.sendInputEvent({type:'mouseMove',...point});
  win.webContents.sendInputEvent({type:'mouseDown',...point,button:'left',clickCount:1});
  win.webContents.sendInputEvent({type:'mouseUp',...point,button:'left',clickCount:1});
}
async function main(){
 if(process.execPath!==path.join(repo,'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'))throw Error('Wrong Electron');
 const assets=path.join(repo,'web/dist/assets');
 const builtWorker=path.join(assets,fs.readdirSync(assets).find(name=>/^shiki\.worker-.*\.js$/.test(name)));
 const {createServer}=await import(pathToFileURL(path.join(repo,'web/node_modules/vite/dist/node/index.js')));
 vite=await createServer({root:path.join(repo,'web'),cacheDir:path.join(out,'vite-cache'),logLevel:'error',server:{host:'127.0.0.1',port:0},plugins:[{
  name:'stream-profile',enforce:'pre',
  resolveId(id){if(id==='/__stream-entry.js')return '\0stream-profile';},
  load(id){if(id==='\0stream-profile')return fixture;},
  configureServer(server){
    server.middlewares.use('/__built-worker.js',(_req,res)=>{res.setHeader('Content-Type','text/javascript');res.end(fs.readFileSync(builtWorker));});
    server.middlewares.use('/__profile',async(_req,res,next)=>{try{res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__profile','<!doctype html><html><head><title>Pudding isolated stream profile</title></head><body><div id="root"></div><script type="module" src="/__stream-entry.js"></script></body></html>'));}catch(e){next(e);}});
  },
 }]});
 await vite.listen();
 win=new BrowserWindow({width:850,height:650,show:true,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
 win.webContents.on('console-message',details=>{if(details.level==='error'){errors.push(details.message);console.error(details.message);}});
 const specs=[{name:'thought-30k-closed',kind:'thought',lines:2000},{name:'markdown-12k',kind:'markdown',lines:100},{name:'code-500-lines',kind:'code',lines:500},{name:'code-500-growing',kind:'code',lines:500,growing:true},{name:'thought-after-code-500',kind:'thought',lines:500,priorCode:true},{name:'thought-after-200-tools',kind:'thought',lines:20,tools:200}];
 const results=[];
 for(const spec of specs){
  await win.loadURL('http://127.0.0.1:'+vite.httpServer.address().port+'/__profile?spec='+encodeURIComponent(JSON.stringify(spec)));
  for(let i=0;i<100&&!await js('!!window.__ready');i++)await delay(100);
  if(!await js('!!window.__ready'))throw Error('Fixture not ready');
  if(spec.kind==='code'||spec.priorCode) await waitFor(()=>js('!!document.querySelector(".shiki")'),'initial worker highlight');
  await delay(700);
  win.webContents.debugger.attach('1.3');
  await win.webContents.debugger.sendCommand('Profiler.enable');
  await win.webContents.debugger.sendCommand('Profiler.start');
  await js('window.__preservedCode=document.querySelector(".shiki");window.__start()');
  let inputSent=0;
  const timer=setInterval(()=>{inputSent++;win.webContents.sendInputEvent({type:'keyDown',keyCode:'A'});win.webContents.sendInputEvent({type:'char',keyCode:'a'});win.webContents.sendInputEvent({type:'keyUp',keyCode:'A'});},120);
  await delay(3000);clearInterval(timer);
  const raw=await js('window.__stop()');
  assert.equal(raw.inputLength,inputSent,'every injected character reaches the input');
  assert.ok(raw.events.length>80,'stream remains responsive');
  assert.ok(summary(raw.ticks).p95<100,'stream does not monopolize the UI thread');
  if(spec.growing) {
    await waitFor(()=>js('!!document.querySelector(".shiki") && window.__latestCode()===window.__expectedCode()'),'latest growing code is highlighted');
  } else if(spec.kind==='code'||spec.priorCode) {
    assert.ok(await js('document.querySelector(".shiki")===window.__preservedCode'),'unchanged code DOM is retained');
  }
  if(spec.tools) {
    assert.ok(raw.nodes<800,'closed tool details stay unmounted');
    await click('[data-transcript-turn-id="profile-turn"] summary');
    await waitFor(()=>js('document.querySelectorAll("[data-transcript-turn-id=profile-turn] details").length>100'),'tool group expands');
    await click('[data-transcript-turn-id="profile-turn"] details details summary');
    await waitFor(()=>js('document.querySelector("[data-transcript-turn-id=profile-turn] details details").open'),'nested tool expands');
    await click('[data-transcript-turn-id="profile-turn"] summary');
    await waitFor(()=>js('document.querySelectorAll("*").length<800'),'tool details unmount on collapse');
    await click('[data-transcript-turn-id="profile-turn"] summary');
    await waitFor(()=>js('document.querySelector("[data-transcript-turn-id=profile-turn] details details")?.open'),'nested disclosure state survives unmount');
    await click('[data-transcript-turn-id="profile-turn"] summary');
    await waitFor(()=>js('document.querySelectorAll("*").length<800'),'restored group finishes collapsing before input');
  }
  await js('(()=>{const input=document.getElementById("input");input.focus();input.setSelectionRange(input.value.length,input.value.length);})()');
  await win.webContents.debugger.sendCommand('Input.imeSetComposition',{text:'中文',selectionStart:2,selectionEnd:2});
  await win.webContents.debugger.sendCommand('Input.insertText',{text:'中文'});
  assert.ok(await js('document.getElementById("input").value.endsWith("中文")'),'composed Unicode text is retained');
  assert.deepEqual(errors,[]);
  if(spec.growing) fs.writeFileSync(path.join(out,'growing-code.png'),(await win.webContents.capturePage()).toPNG());
  const {profile}=await win.webContents.debugger.sendCommand('Profiler.stop');
  win.webContents.debugger.detach();
  fs.writeFileSync(path.join(out,spec.name+'.cpuprofile'),JSON.stringify(profile));
  const result={...spec,chars:raw.chars,nodes:raw.nodes,commit:summary(raw.commits),enqueue:summary(raw.events),loop:summary(raw.ticks),longTasks:summary(raw.longTasks),inputDelay:summary(raw.inputs.map(e=>e.delay)),inputSent,inputReceived:raw.inputLength};
  results.push(result);console.log(JSON.stringify(result));
  fs.writeFileSync(path.join(out,'results.json'),JSON.stringify(results,null,2));
 }
 const builtResult=await js(`new Promise((resolve,reject)=>{
   const worker=new Worker('/__built-worker.js',{type:'module'});
   const timer=setTimeout(()=>{worker.terminate();reject(Error('built worker timed out'));},10000);
   worker.onmessage=({data})=>{clearTimeout(timer);worker.terminate();resolve(data);};
   worker.onerror=event=>{clearTimeout(timer);worker.terminate();reject(Error(event.message));};
   worker.postMessage({id:1,code:'const builtWorkerValue = 42;',lang:'ts'});
 })`);
 assert.ok(builtResult.html.includes('builtWorkerValue')&&builtResult.html.includes('shiki'),'production worker bundle highlights code');
 console.log('PASS production Worker, input, latest code, stable code DOM and nested disclosures');
}
app.whenReady().then(main).catch(async e=>{console.error(e);exitCode=1;if(win)fs.writeFileSync(path.join(out,'failure.png'),(await win.webContents.capturePage()).toPNG());}).finally(async()=>{win?.destroy();await vite?.close();console.log('Artifacts: '+out);app.exit(exitCode);});
