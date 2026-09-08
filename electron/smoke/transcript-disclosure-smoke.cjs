// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/transcript-disclosure-smoke.cjs
// Isolated production TranscriptList/AssistantError regression; no daemon or real session data.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-disclosure-smoke-'));
app.setPath('userData', path.join(home, 'user-data'));
let vite, window, exitCode = 0;
const js = source => window.webContents.executeJavaScript(source, true);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const errors = [];
async function waitFor(predicate, label) {
  const until = Date.now() + 15_000;
  while (Date.now() < until) {
    assert.deepEqual(errors, []);
    if (await predicate()) return;
    await delay(30);
  }
  throw new Error('Timed out: ' + label);
}
async function run() {
  assert.equal(process.execPath, path.join(repo, 'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {createServer} = await import(pathToFileURL(path.join(repo, 'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({
    root:path.join(repo, 'web'), cacheDir:path.join(home, 'vite-cache'), logLevel:'error', server:{host:'127.0.0.1',port:0},
    plugins:[{
      name:'disclosure-fixture', resolveId(id) {if (id === '/__disclosure.js') return '\0disclosure-fixture';},
      load(id) {
        if (id !== '\0disclosure-fixture') return;
        return `
          import React, {useState} from 'react';
          import {createRoot} from 'react-dom/client';
          import '/src/styles.css';
          import {TranscriptList} from '/src/components/transcript/TranscriptList.tsx';
          import {TooltipProvider} from '/src/components/ui/tooltip.tsx';
          import {setLocale} from '/src/i18n/index.ts';
          setLocale('zh-CN');
          const h=React.createElement;
          const params=new URLSearchParams(location.search);
          const error='Post "https://example.invalid/v1/chat/completions": http2: timeout awaiting response headers';
          const errorText=params.has('long') ? Array(30).fill(error).join('\\n') : error;
          const turns=Array.from({length:40}, (_,i)=>({key:'turn-'+i,turnID:'turn-'+i,kind:'canonical',
            user:{text:'测试消息 '+i,createdAt:'2026-09-08T03:14:00Z'},
            assistant:{kind:'canonical',error:i>=38 ? errorText : undefined,
              messages:[{id:'message-'+i,role:'assistant',createdAt:'2026-09-08T03:14:00Z',parts:[{type:'text',text:i===39 ? '测试请求失败展开和收起。' : '历史内容需要保持阅读位置。'.repeat(30)}]}]}}));
          if(params.has('live')) {
            turns[39].kind='live';
            turns[39].assistant={kind:'live',canonicalReady:false,overlay:{sessionID:'disclosure-fixture',turnID:'turn-39',
              status:'failed',text:'测试请求失败展开和收起。',parts:[],error:errorText}};
          }
          if(params.has('thought')) turns[39].assistant.messages[0].parts.unshift({type:'thought',text:'测试受控思考折叠。'.repeat(20)});
          function Fixture() {
            const [viewport,setViewport]=useState(null);
            return h(TooltipProvider,null,h('div',{id:'viewport',ref:setViewport,style:{height:540,overflow:'auto',overflowAnchor:'none',contain:'strict',padding:'0 24px',
              '--pudding-composer-mask-height':'24px','--pudding-composer-overlay-height':'100px'}},
              h(TranscriptList,{turns,scrollElement:viewport,sessionID:'disclosure-fixture',searchSlot:'primary',searchState:{terms:[]},
                token:'',hasMoreHistory:false,isLoadingHistory:false,jumpLatestSignal:0,onLoadHistory:()=>{},
                footer:h('div',{style:{height:24}},'正在压缩上下文')})));
          }
          createRoot(document.getElementById('root')).render(h(Fixture));
          window.sample=()=>{
            const v=document.getElementById('viewport'), list=v.querySelector('[role=list]'), summary=v.querySelector(window.targetSelector);
            return {y:summary?.getBoundingClientRect().top-v.getBoundingClientRect().top,open:summary?.parentElement.open,
              scroll:v.scrollTop,height:v.scrollHeight,listHeight:list?.getBoundingClientRect().height,
              tail:list?.firstElementChild?.lastElementChild.getBoundingClientRect().bottom-v.getBoundingClientRect().bottom};
          };
        `;
      },
      configureServer(server) {server.middlewares.use('/__fixture', async (_req,res,next)=>{
        try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture',
          '<!doctype html><html><head><title>Disclosure regression</title></head><body><div id="root"></div><script type="module" src="/__disclosure.js"></script></body></html>'));}
        catch(error){next(error);}
      });},
    }],
  });
  await vite.listen();
  window = new BrowserWindow({width:740,height:600,show:true,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message', details=>{if(details.level==='error'){errors.push(details.message);console.error(details.message);}});
  for (const scenario of ['canonical','long','live','history','thought']) {
    await window.loadURL('http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture?'+scenario);
    const turnID=scenario==='history' ? 'turn-38' : 'turn-39';
    const selector='[data-transcript-turn-id="'+turnID+'"] '+(scenario==='thought' ? 'summary' : '[role=alert] summary');
    await js('window.targetSelector='+JSON.stringify(selector));
    await waitFor(()=>js(`Boolean(document.querySelector(window.targetSelector))`),'target row');
    await delay(300);
    if(scenario==='history') {
      await js(`(()=>{const v=document.getElementById('viewport'), s=v.querySelector(window.targetSelector);
        v.dispatchEvent(new WheelEvent('wheel',{deltaY:-100,bubbles:true}));
        v.scrollTop+=s.getBoundingClientRect().top-v.getBoundingClientRect().top-240;})()`);
      await delay(200);
      assert.ok((await js('window.sample()')).tail>50,'history scenario is away from the latest edge');
    }
    for (const [action,key] of [['expand'],['collapse'],['enter-expand','Enter'],['space-collapse',' '],['expand-again'],['collapse-again']]) {
      await js(`(()=>{window.frames=[]; const record=()=>{window.frames.push(window.sample()); window.recordFrame=requestAnimationFrame(record);}; record();})()`);
      if(key) {
        await js(`document.querySelector(window.targetSelector).focus({preventScroll:true})`);
        window.webContents.sendInputEvent({type:'keyDown',keyCode:key});
        window.webContents.sendInputEvent({type:'keyUp',keyCode:key});
      } else {
        const point = await js(`(()=>{const r=document.querySelector(window.targetSelector).getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
        assert.ok(point.y>0 && point.y<540,'summary is visible for real mouse input');
        window.webContents.sendInputEvent({type:'mouseMove',...point});
        window.webContents.sendInputEvent({type:'mouseDown',...point,button:'left',clickCount:1});
        window.webContents.sendInputEvent({type:'mouseUp',...point,button:'left',clickCount:1});
      }
      await delay(400);
      const samples=await js('cancelAnimationFrame(window.recordFrame); window.frames');
      fs.writeFileSync(path.join(home,scenario+'-'+action+'.json'),JSON.stringify(samples,null,2));
      const before=samples[0], after=samples.at(-1), label=scenario+'/'+action;
      assert.equal(before.open,action.includes('collapse'),label+': initial open state');
      assert.notEqual(after.open,before.open,label+': toggled exactly once');
      assert.ok(Math.abs(after.listHeight-before.listHeight)>20,label+': actual content resize');
      // The first frame with the new open state must already have its final row
      // measurement and scroll compensation, not paint once with the old offset.
      for(const frame of samples) {
        const expected=frame.open===before.open ? before : after;
        for(const field of ['y','scroll','listHeight','tail']) {
          assert.ok(Number.isFinite(frame[field]) && Math.abs(frame[field]-expected[field])<1,
            label+': stale '+field+' in painted frame '+JSON.stringify({frame,expected}));
        }
      }
      if(scenario==='history') assert.ok(Math.abs(after.y-before.y)<1,label+': reading position retained');
      else assert.ok(Math.abs(after.tail-before.tail)<1,label+': latest anchor retained');
      console.log('PASS '+label+' ('+samples.length+' frames)');
    }
  }
  fs.writeFileSync(path.join(home,'result.png'),(await window.webContents.capturePage()).toPNG());
  assert.deepEqual(errors,[]);
  console.log('REPORT '+home);
}
app.whenReady().then(run).catch(error=>{console.error(error);exitCode=1;}).finally(async()=>{
  await vite?.close();app.exit(exitCode);
});
