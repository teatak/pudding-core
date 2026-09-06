#!/usr/bin/env node
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawn,execFileSync} = require('node:child_process');
const assert = require('node:assert/strict');
const {ComputerUseHost} = require('../electron/computer-use-host.cjs');
const {ComputerUseBridgeServer} = require('../electron/computer-use-bridge-server.cjs');
const root=path.resolve(__dirname,'..');
const appPath=path.join(root,'web/node_modules/electron/dist/Electron.app');
const host=new ComputerUseHost({binaryPath:path.join(root,'bin/Pudding Computer Use.app/Contents/MacOS/PuddingComputerUseHelper')});
const bridge=new ComputerUseBridgeServer(host);
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'pudding-cu-electron-'));
const children=[];
const ime=process.argv.includes('--ime');
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function fixture(label) {
  const child=spawn(path.join(appPath,'Contents/MacOS/Electron'),[path.join(root,'electron/smoke/computer-use-fixture.cjs'),dir,label],{stdio:'ignore',env:{...process.env,ELECTRON_RUN_AS_NODE:''}});
  children.push(child);
  for(let i=0;i<100;i++) {
    if(fs.existsSync(path.join(dir,label+'.json')))return JSON.parse(fs.readFileSync(path.join(dir,label+'.json'),'utf8'));
    assert(child.exitCode==null,'fixture exited');await pause(100);
  }
  throw new Error('fixture startup timed out');
}
async function main(){
  try {
    const p=await host.permissions();assert(p.accessibility&&p.screenRecording,'development helper needs Accessibility and Screen Recording');
    const first=await fixture('first');const second=await fixture('second');
    const inventory=await host.listApps();const electron=inventory.apps.find(a=>a.bundleID==='com.github.Electron');
    for(const instance of [first,second])assert(electron.instances.some(i=>i.pid===instance.pid&&i.appPath===appPath),'missing explicit instance identity');
    await assert.rejects(host.useApp({bundleID:'com.github.Electron',appPath}),e=>e.code==='computer_app_ambiguous');
    await assert.rejects(host.useApp({bundleID:'com.github.Electron',appPath:'/System/Applications/Calculator.app',pid:first.pid}),e=>e.code==='computer_invalid_request');
    const use=await host.useApp({bundleID:'com.github.Electron',appPath,pid:first.pid,foreground:true});
    assert.equal(use.pid,first.pid);assert.equal(use.newlyLaunched,false);
    const target=use.windows.find(w=>w.title==='CU first');assert(target,'exact PID window missing');
    if(ime){
      const statusTool=path.join(dir,'input-status');
      execFileSync('clang',[path.join(root,'electron/smoke/macos-input.m'),'-framework','AppKit','-framework','ApplicationServices','-framework','Carbon','-o',statusTool]);
      const status=JSON.parse(execFileSync(statusTool,['status'],{encoding:'utf8'}));
      assert.equal(status.inputSource,'com.apple.inputmethod.SCIM.ITABC','--ime requires macOS Simplified Pinyin already selected; the smoke does not change input sources');
    }
    const other=await host.useApp({bundleID:'com.github.Electron',appPath,pid:second.pid});
    await assert.rejects(host.keyboard({bundleID:'com.github.Electron',windowID:other.windows[0].windowID,action:'press_key',key:'a'}),e=>e.code==='computer_app_not_foreground'&&e.outcome==='not_started');
    const image=await host.observeCapture({bundleID:'com.github.Electron',windowID:target.windowID,output:path.join(dir,'image.png'),includeAccessibility:false});
    assert(image.capture);assert(!image.observation&&!image.observationError);
    const identity=await bridge.start();
    await new Promise((resolve,reject)=>{
      const child=spawn('go',['test','./internal/engine','-run','^TestComputerUseElectronSmoke$','-count=1','-timeout=90s','-v'],{cwd:root,stdio:'inherit',env:{...process.env,PUDDING_COMPUTER_USE_ELECTRON_SMOKE:'1',PUDDING_CU_IME_SMOKE:ime?'1':'0',PUDDING_CU_FIXTURE_PID:String(first.pid),PUDDING_CU_FIXTURE_PATH:appPath,PUDDING_ELECTRON_COMPUTER_BRIDGE_URL:identity.url,PUDDING_ELECTRON_COMPUTER_BRIDGE_TOKEN:identity.token}});
      child.on('error',reject);child.on('exit',code=>code===0?resolve():reject(new Error('Electron product smoke failed')));
    });
    if(ime){
      const events=fs.readFileSync(path.join(dir,'first.events.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
      assert(events.some(e=>e.type==='compositionstart'&&e.trusted),'missing trusted IME composition');
      assert(events.some(e=>e.type==='compositionend'&&e.data==='你好'),'missing Chinese candidate commit');
    }
    const finalAX=await host.observe({bundleID:'com.github.Electron',windowID:target.windowID,maxElements:200});
    const secure=finalAX.elements.find(e=>e.secure);assert(secure&&!secure.value,'secure value leaked');
    await assert.rejects(host.act({bundleID:'com.github.Electron',windowID:target.windowID,elementID:secure.elementID,action:'focus'}),e=>e.code==='computer_element_not_actionable');
    await host.pointer({bundleID:'com.github.Electron',windowID:target.windowID,action:'click',x:(secure.frame.x-target.frame.x+secure.frame.width/2)/target.frame.width,y:(secure.frame.y-target.frame.y+secure.frame.height/2)/target.frame.height});
    await assert.rejects(host.keyboard({bundleID:'com.github.Electron',windowID:target.windowID,action:'type_text',value:'blocked'}),e=>e.code==='computer_element_not_actionable'&&e.outcome==='not_started');
    const untouched=await host.useApp({bundleID:'com.github.Electron',appPath,pid:second.pid});
    const state=await host.observe({bundleID:'com.github.Electron',windowID:untouched.windows[0].windowID,maxElements:200});
    assert(state.elements.some(e=>e.role==='AXTextField'&&e.value==='initial'),'other instance was modified');
    console.log(`PASS: PID/path selection, ambiguity, image-only, Electron AX, real model-to-native input, foreground/secure-field guards, untouched second instance${ime ? ', native Pinyin composition' : ''}`);
  } finally {
    for(const child of children){await host.quitApp({bundleID:'com.github.Electron',pid:child.pid}).catch(()=>{});}
    await bridge.stop();await host.stop();
    fs.rmSync(dir,{recursive:true,force:true});
  }
}
main().catch(e=>{console.error(e);process.exitCode=1;});
