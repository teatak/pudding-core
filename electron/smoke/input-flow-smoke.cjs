// Run: web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/input-flow-smoke.cjs
// Real renderer input against source components; temporary data, no daemon or model calls.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {app, BrowserWindow} = require('electron');
const repo = path.resolve(__dirname, '../..');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-input-flow-smoke-'));
app.setPath('userData', path.join(output, 'user-data'));
let vite, window, exitCode = 0;
const errors = [];
const js = source => window.webContents.executeJavaScript(source, true);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(source, label) {
  const until = Date.now() + 15_000;
  while (Date.now() < until) {
    assert.deepEqual(errors, []);
    if (await js(source)) return;
    await delay(30);
  }
  throw new Error('Timed out: ' + label);
}
async function key(keyCode) {
  window.webContents.sendInputEvent({type:'keyDown',keyCode});
  // Native button activation uses the character event; keyDown alone only reaches React handlers.
  if (keyCode === 'Enter') window.webContents.sendInputEvent({type:'char',keyCode:'\r'});
  window.webContents.sendInputEvent({type:'keyUp',keyCode});
  await delay(60);
}
async function point(selector) {
  return js(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});el.scrollIntoView({block:'nearest'});
    const r=el.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};})()`);
}
async function click(selector) {
  const p = await point(selector);
  window.webContents.sendInputEvent({type:'mouseMove',...p});
  window.webContents.sendInputEvent({type:'mouseDown',...p,button:'left',clickCount:1});
  window.webContents.sendInputEvent({type:'mouseUp',...p,button:'left',clickCount:1});
  await delay(60);
}
const option = n => `[data-input-flow-panel] [role="option"]:nth-child(${n})`;
const singleStep = {id:'answer',type:'single_select',title:'测试已准备好。方便接下来约 20 秒不移动鼠标、不切换应用吗？我只重测计算器的一个数字按钮。',options:[
  {title:'可以，现在测试',value:'yes'},
  {title:'稍后再测试',description:'保留当前工作，等我确认后再开始。',value:'later'},
  {title:'不需要了',value:'no'},
]};
async function show(steps = [singleStep]) {
  // Keep pointer hover from changing the keyboard selection when the card is replaced or resized.
  window.webContents.sendInputEvent({type:'mouseMove',x:0,y:0});
  await js(`window.fixture.show(${JSON.stringify({type:'form',title:'问题',steps})})`);
  await waitFor('!!document.querySelector("[data-input-flow-panel]")','question card');
  await delay(80);
}
async function submitted(expected) {
  await waitFor('window.fixture.submissions.length === 1','one submission');
  assert.deepEqual(await js('window.fixture.submissions[0].result'), {type:'user_input_result',title:'问题',...expected});
  assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'), false);
}
async function checkInputFocusRing(type, width) {
  window.setSize(width,700);
  await show([{id:'value',type,title:'输入测试值',options:Array.from({length:9},(_,i)=>i+1)}]);
  if(type==='quick_number') {
    await js(`(()=>{const row=document.querySelector('[data-input-flow-panel] [role=option]');
      window.fixture.presetRow={height:row.getBoundingClientRect().height,badgeLeft:row.querySelector('span[aria-hidden]').getBoundingClientRect().left,
        labelLeft:row.querySelector('div').getBoundingClientRect().left,arrowRight:row.querySelector('svg').getBoundingClientRect().right};})()`);
    await key('End');
    await key('Enter');
  }
  await waitFor('document.activeElement?.tagName === "INPUT"','input before Tab');
  await window.webContents.insertText('12');
  await key('Tab');
  await delay(150);
  const geometry=await js(`(()=>{
    const b=document.activeElement,r=b.getBoundingClientRect(),v=b.closest('[data-input-flow-body]'),vr=v.getBoundingClientRect();
    return {tag:b.tagName,focusVisible:b.matches(':focus-visible'),shadow:getComputedStyle(b).boxShadow,
      ring:getComputedStyle(b).getPropertyValue('--tw-ring-shadow'),scrolling:v.scrollHeight>v.clientHeight,
      clearance:{left:r.left-vr.left,top:r.top-vr.top,right:vr.left+v.clientWidth-r.right,bottom:vr.top+v.clientHeight-r.bottom}};
  })()`);
  console.log('FOCUS '+type+'/'+width+' '+JSON.stringify(geometry));
  fs.writeFileSync(path.join(output,'focus-'+type+'-'+width+'.png'),(await window.webContents.capturePage()).toPNG());
  assert.equal(geometry.tag,'BUTTON');
  assert.equal(geometry.focusVisible,true,'keyboard focus ring is active');
  assert.match(geometry.ring,/3px/,'the shared 3px focus ring is retained');
  for(const [edge,clearance] of Object.entries(geometry.clearance)) {
    assert.ok(clearance>=3,`${type}/${width}: focus ring clipped at ${edge} (${clearance}px)`);
  }
  if(geometry.scrolling) assert.ok(geometry.clearance.right>=12,'focus ring stays clear of the scrollbar');
  const layout=await js(`(()=>{
    const input=document.querySelector('[data-input-flow-panel] input'),button=document.activeElement;
    const ir=input.getBoundingClientRect(),br=button.getBoundingClientRect(),row=input.closest('[role=option]');
    const preset=window.fixture.presetRow;
    const badge=row?.querySelector('span[aria-hidden]');
    return {inputHeight:ir.height,buttonHeight:br.height,centerOffset:ir.top+ir.height/2-br.top-br.height/2,
      row:row ? {height:row.getBoundingClientRect().height,previousHeight:preset.height,
        badgeLeft:badge?.getBoundingClientRect().left,previousBadgeLeft:preset.badgeLeft,
        inputLeft:ir.left,labelLeft:preset.labelLeft,
        buttonRight:br.right,arrowRight:preset.arrowRight} : null};
  })()`);
  console.log('LAYOUT '+type+'/'+width+' '+JSON.stringify(layout));
  assert.equal(layout.inputHeight,32,'input uses the standard 32px control height');
  assert.equal(layout.buttonHeight,32,'confirmation matches input height');
  assert.equal(layout.centerOffset,0,'controls share a center line');
  if(layout.row) {
    assert.equal(layout.row.height,44,'custom option keeps the standard 44px row');
    assert.equal(layout.row.height,layout.row.previousHeight,'custom and preset rows match');
    assert.equal(layout.row.badgeLeft,layout.row.previousBadgeLeft,'custom option retains its aligned number');
    assert.equal(layout.row.inputLeft,layout.row.labelLeft,'input aligns with option content');
    assert.equal(layout.row.buttonRight,layout.row.arrowRight,'right action edges align');
  }
}
async function run() {
  assert.equal(process.execPath,path.join(repo,'web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'));
  const {createServer} = await import(pathToFileURL(path.join(repo,'web/node_modules/vite/dist/node/index.js')));
  vite = await createServer({root:path.join(repo,'web'),cacheDir:path.join(output,'vite-cache'),logLevel:'error',
    server:{host:'127.0.0.1',port:0},plugins:[{
      name:'input-flow-fixture',
      resolveId(id) {if (id === '/__input-flow.js') return '\0input-flow-fixture';},
      load(id) {
        if (id !== '\0input-flow-fixture') return;
        return `
          import React from 'react';
          import {createRoot} from 'react-dom/client';
          import '/src/styles.css';
          import {InputFlowPanel} from '/src/components/transcript/InputFlowToolPart.tsx';
          import {ChoiceMenu} from '/src/components/ChoiceMenu.tsx';
          import {showInputFlow,useInputFlowStore} from '/src/state/inputFlowStore.ts';
          import {setLocale} from '/src/i18n/index.ts';
          setLocale('zh-CN');
          const h=React.createElement;
          window.fixture={submissions:[],show(args) {
            this.submissions=[];
            showInputFlow({args,sessionID:'input-flow-smoke',title:args.title});
          }};
          function Fixture() {
            const request=useInputFlowStore(state=>state.requests[0]);
            return h('main',{style:{position:'absolute',inset:24}},
              h('div',{style:{color:'var(--muted-foreground)',fontSize:14}},'交互卡片 · 独立开发环境'),
              h('div',{id:'default-menu',style:{width:180,marginTop:16}},h(ChoiceMenu,{focusMode:'none',
                items:[{id:'allow',label:'允许',value:true},{id:'deny',label:'拒绝',value:false}],
                onSelect:value=>window.fixture.defaultResult=value})),
              h('div',{style:{position:'absolute',bottom:0,left:0,right:0}},
                request ? h(InputFlowPanel,{key:request.id,request,onSubmit:value=>window.fixture.submissions.push(value)}) : null,
                h('div',{style:{height:120,border:'1px solid var(--border)',borderRadius:'var(--radius)',padding:16,color:'var(--muted-foreground)'}},'输入消息')));
          }
          createRoot(document.getElementById('root')).render(h(Fixture));
        `;
      },
      configureServer(server) {server.middlewares.use('/__fixture',async (_req,res,next)=>{
        try {res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/__fixture',
          '<!doctype html><html><head><title>Input flow regression</title></head><body><div id="root"></div><script type="module" src="/__input-flow.js"></script></body></html>'));}
        catch(error){next(error);}
      });},
    }]});
  await vite.listen();
  window = new BrowserWindow({width:760,height:700,show:true,webPreferences:{contextIsolation:true,nodeIntegration:false,backgroundThrottling:false}});
  window.webContents.on('console-message',details=>{if(details.level==='error') errors.push(details.message);});
  const url='http://127.0.0.1:'+vite.httpServer.address().port+'/__fixture';
  await window.loadURL(url);
  assert.equal(window.webContents.getURL(),url);
  await waitFor('!!window.fixture','source fixture');
  // Default approval menus keep their existing behavior; question shortcuts are opt-in.
  await js('document.querySelector("#default-menu [role=listbox]").focus()');
  await key('2');
  assert.equal(await js('window.fixture.defaultResult'),undefined);
  await key('Down');
  await key('Enter');
  assert.equal(await js('window.fixture.defaultResult'),false);
  await js('document.getElementById("default-menu").style.display="none"');
  console.log('PASS default menu unchanged');

  for(const width of [760,420]) {
    for(const type of ['quick_number','text_input']) await checkInputFocusRing(type,width);
  }
  window.setSize(760,700);
  console.log('PASS input confirmation focus rings');

  await show();
  assert.equal(await js('document.activeElement.getAttribute("role")'),'listbox');
  assert.equal(await js('document.querySelector("[data-input-flow-panel] [role=option] > span").textContent'),'1');
  fs.writeFileSync(path.join(output,'question-light.png'),(await window.webContents.capturePage()).toPNG());
  await key('Down');
  await key('Enter');
  await submitted({answer:'later'});
  await show();
  await key('3');
  await submitted({answer:'no'});
  await show();
  const p=await point(option(2));
  window.webContents.sendInputEvent({type:'mouseMove',...p});
  await waitFor(`document.querySelector('${option(2)}').getAttribute('aria-selected')==='true'`,'hover active option');
  window.webContents.sendInputEvent({type:'mouseDown',...p,button:'left',clickCount:1});
  await delay(50);
  assert.equal(await js('window.fixture.submissions.length'),0,'mouse press alone does not submit');
  window.webContents.sendInputEvent({type:'mouseUp',...p,button:'left',clickCount:1});
  await submitted({answer:'later'});
  console.log('PASS numbered choices, hover, click, arrows and Enter');

  await show();
  await key('Escape');
  assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),false);
  assert.equal(await js('window.fixture.submissions.length'),0);
  await show();
  await click('button[aria-label="取消"]');
  assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),false);
  console.log('PASS Escape and dismiss button');

  await show([{id:'choices',type:'multi_select',title:'选择需要测试的功能（最多两项）',min:1,max:2,options:['浏览器','项目','会话']}]);
  assert.equal(await js('document.querySelector("[data-input-flow-panel] button:not([aria-label])").disabled'),true);
  await key('1');
  await key('2');
  assert.equal(await js('document.querySelector("[aria-multiselectable] [role=option]:nth-child(3)").getAttribute("aria-disabled")'),'true');
  await key('3');
  assert.equal(await js('document.querySelectorAll("[aria-multiselectable] [aria-selected=true]").length'),2);
  await key('1');
  await key('3');
  fs.writeFileSync(path.join(output,'question-multiple.png'),(await window.webContents.capturePage()).toPNG());
  await click('[data-input-flow-panel] button:not([aria-label])');
  await submitted({choices:['项目','会话']});
  console.log('PASS multi-select toggles, bounds and explicit confirm');

  // Optional fields keep confirm and skip together; skipping discards the current selection.
  const optionalChoices={id:'answer',type:'multi_select',title:'选择需要的食材（可跳过）',required:false,options:['猪里脊','甜面酱','大葱','豆腐皮']};
  const actionButton='[data-input-flow-body] button:not([role="option"])';
  for(const width of [760,420]) {
    window.setSize(width,700);
    for(const type of ['multi_select','text_input','phone_input','number_input','date_input','quick_number']) {
      await show([{...optionalChoices,type,options:type==='quick_number' ? [1,2] : optionalChoices.options}]);
      if(type==='multi_select') await key('2');
      if(type==='quick_number') await key('3');
      if(type==='text_input' || type==='quick_number') await window.webContents.insertText('12');
      const actions=await js(`(()=>{
        const body=document.querySelector('[data-input-flow-body]'),r=body.getBoundingClientRect();
        return [...body.querySelectorAll('button:not([role="option"])')].map(b=>{
          const br=b.getBoundingClientRect();
          return {label:b.textContent,top:br.top,height:br.height,left:br.left,right:br.right,inside:br.left>=r.left && br.right<=r.right};
        });
      })()`);
      assert.deepEqual(actions.map(a=>a.label),['确定','跳过']);
      assert.equal(actions[0].top,actions[1].top,`${type}/${width}: actions share a row`);
      assert.equal(actions[0].height,actions[1].height,`${type}/${width}: actions share a height`);
      assert.ok(actions[1].left>actions[0].right && actions.every(a=>a.inside),'actions fit without overlap');
      if(type==='multi_select') {
        await js('document.documentElement.classList.add("dark")');
        fs.writeFileSync(path.join(output,'question-actions-'+width+'.png'),(await window.webContents.capturePage()).toPNG());
        await js('document.documentElement.classList.remove("dark")');
      }
      await click(actionButton+':nth-of-type(2)');
      await submitted({});
    }
  }
  window.setSize(760,700);
  await show([optionalChoices]);
  await key('1');
  await click(actionButton+':first-of-type');
  await submitted({answer:['猪里脊']});
  await show([optionalChoices]);
  await click(actionButton+':first-of-type');
  await submitted({answer:[]});
  for(const type of ['single_select','quick_number']) {
    await show([{...optionalChoices,type,options:type==='quick_number' ? [1,2] : optionalChoices.options}]);
    assert.equal(await js(`document.querySelectorAll(${JSON.stringify(actionButton)}).length`),1);
    await click(actionButton);
    await submitted({});
  }
  console.log('PASS optional field action rows, confirm and skip');

  await show([{id:'quantity',type:'quick_number',title:'需要多少份？',options:[1,2],min:1,max:20}]);
  await key('3');
  await waitFor('document.activeElement?.tagName === "INPUT"','custom number focus');
  await window.webContents.insertText('12');
  await key('Enter');
  await submitted({quantity:12});
  await show([{id:'quantity',type:'quick_number',title:'需要多少份？',options:[1,2],min:1,max:20}]);
  await click(option(3));
  await waitFor('document.activeElement?.tagName === "INPUT"','clicked custom number focus');
  await window.webContents.insertText('15');
  await waitFor('document.querySelector("[data-input-flow-panel] input").value === "15"','custom number typed');
  await click('[data-input-flow-panel] input');
  assert.equal(await js('document.activeElement.tagName'),'INPUT');
  await js('document.querySelector("[data-input-flow-panel] [role=option] button").focus()');
  assert.equal(await js('document.activeElement.tagName'),'BUTTON');
  await key('Enter');
  await submitted({quantity:15});
  await show([{id:'quantity',type:'quick_number',title:'需要多少份？',options:[1,2],min:1,max:20}]);
  await key('3');
  await waitFor('document.activeElement?.tagName === "INPUT"','custom input before dismissal');
  await key('Escape');
  assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),false);
  assert.equal(await js('window.fixture.submissions.length'),0);
  console.log('PASS custom input owns typing, caret and embedded button keyboard');

  await show([singleStep,{id:'note',title:'补充说明',type:'text_input',required:false}]);
  await key('1');
  await waitFor('!!document.querySelector("[data-input-flow-panel] input")','next text step');
  await window.webContents.insertText('123 测试说明');
  await key('Enter');
  await submitted({answer:'yes',note:'123 测试说明'});
  console.log('PASS multi-step flow and text input');

  await js(`window.fixture.show(${JSON.stringify({type:'repeat',title:'问题',maxItems:1,
    repeatSteps:[{id:'quantity',type:'quick_number',title:'需要多少份？',options:[1,2]}],
    afterItem:{title:'继续收集吗？'},nextSteps:[{id:'confirmed',type:'confirm',title:'确认这些信息？'}]})})`);
  await waitFor('!!document.querySelector("[data-input-flow-panel] [role=listbox]")','repeat item');
  await delay(80);
  await key('2');
  await waitFor('document.querySelector("[data-input-flow-panel]").textContent.includes("继续收集吗？")','repeat actions');
  await key('1');
  await waitFor('document.querySelector("[data-input-flow-panel]").textContent.includes("确认这些信息？")','repeat confirmation');
  await key('1');
  await submitted({items:[{quantity:2}],confirmed:true});
  console.log('PASS repeat actions and final confirmation');

  const compactQuestion={type:'form',title:'继续加购确认',description:'状态汇报不应占据表单空间。'.repeat(12),
    steps:[{id:'ready',type:'single_select',title:'「iPhone 镜像」窗口已点回最前台了吗？',description:'问题的重复说明。',
      options:[{title:'已置前，继续加购',description:'选项的重复说明。',value:'continue'},
        {title:'还没弄好，等我',description:'再等你操作。',value:'wait'}]}]};
  for(const [width,height] of [[760,700],[420,500]]) {
    window.setSize(width,height);
    await js(`window.fixture.show(${JSON.stringify(compactQuestion)})`);
    await waitFor('!!document.querySelector("[data-input-flow-panel] [role=listbox]")','compact question');
    await delay(100);
    assert.ok(await js(`(()=>{const panel=document.querySelector('[data-input-flow-panel]'),r=panel.getBoundingClientRect(),body=panel.querySelector('[data-input-flow-body]');
      const options=Array.from(panel.querySelectorAll('[role=option]'));
      return !panel.textContent.includes('重复说明') && !panel.textContent.includes('状态汇报') && !panel.textContent.includes('再等你操作')
        && r.top>=0 && r.bottom<=innerHeight && body.scrollHeight===body.clientHeight
        && options.length===2 && options.every(o=>{const b=o.getBoundingClientRect();return b.top>=r.top && b.bottom<=r.bottom;});})()`),'question and both options are fully visible without scrolling');
    fs.writeFileSync(path.join(output,`compact-${width}.png`),(await window.webContents.capturePage()).toPNG());
  }
  console.log('PASS compact question without descriptions or scrolling');

  window.setSize(760,900);
  await show([{id:'answer',type:'single_select',title:'选择一项',options:Array.from({length:8},(_,i)=>'选项 '+(i+1))}]);
  assert.ok(await js(`(()=>{const p=document.querySelector('[data-input-flow-panel]'),b=p.querySelector('[data-input-flow-body]'),options=p.querySelectorAll('[role=option]');
    return options.length===8 && b.scrollHeight===b.clientHeight && p.getBoundingClientRect().height>320
      && options[7].getBoundingClientRect().bottom<=b.getBoundingClientRect().bottom;})()`),'tall windows show all eight options without scrolling');
  fs.writeFileSync(path.join(output,'question-expanded.png'),(await window.webContents.capturePage()).toPNG());

  window.setSize(420,700);
  const longOptions=Array.from({length:10},(_,i)=>({title:`选项 ${i+1}：${'较长的选项内容应自然换行。'.repeat(3)}`,value:i}));
  await show([{...singleStep,options:longOptions}]);
  const panelFits=`(()=>{const p=document.querySelector('[data-input-flow-panel]'),l=p.querySelector('[role=listbox]'),b=p.querySelector('[data-input-flow-body]');
    const r=p.getBoundingClientRect();return p.scrollWidth<=p.clientWidth && b.scrollHeight>b.clientHeight && l.scrollHeight===l.clientHeight
      && r.top>=0 && r.bottom<=innerHeight && l.children.length===10 && !p.querySelector('[data-choice-menu-pages]');})()`;
  assert.ok(await js(panelFits),'all options remain in one list with only the body scrolling at max height');
  await key('End');
  assert.ok(await js(`(()=>{const p=document.querySelector('[data-input-flow-panel]'),l=p.querySelector('[role=listbox]'),b=p.querySelector('[data-input-flow-body]');
    return b.scrollTop>0 && l.scrollTop===0 && l.lastElementChild.getBoundingClientRect().bottom<=b.getBoundingClientRect().bottom;})()`),'End exposes the final option');
  await key('Home');
  assert.equal(await js('document.querySelector("[data-input-flow-body]").scrollTop'),0,'Home exposes the question again');
  fs.writeFileSync(path.join(output,'question-narrow.png'),(await window.webContents.capturePage()).toPNG());
  await js('document.documentElement.classList.add("dark")');
  await delay(100);
  fs.writeFileSync(path.join(output,'question-dark.png'),(await window.webContents.capturePage()).toPNG());
  await key('5');
  await submitted({answer:4});
  console.log('PASS expanded content, responsive max height and single scroll navigation');

  window.setSize(760,700);
  await show([{id:'choices',type:'multi_select',title:'选择多项',min:1,max:2,options:['一','二','三','四','五']}]);
  assert.equal(await js('document.querySelectorAll("[aria-multiselectable] [role=option]").length'),5);
  await key('1');
  await key('4');
  assert.equal(await js('document.querySelectorAll("[aria-multiselectable] [aria-selected=true]").length'),2);
  await js(`Array.from(document.querySelectorAll('[data-input-flow-panel] button')).find(b=>b.textContent==='确定').click()`);
  await submitted({choices:['一','四']});
  console.log('PASS multi-select shows all choices together');
  assert.deepEqual(errors,[]);
  console.log('REPORT '+output);
}
app.whenReady().then(run).catch(async error=>{
  console.error(error);exitCode=1;
  if(window) {
    console.error(await js('({focus:document.activeElement?.outerHTML,card:document.querySelector("[data-input-flow-panel]")?.outerHTML,submissions:window.fixture?.submissions})'));
    fs.writeFileSync(path.join(output,'failure.png'),(await window.webContents.capturePage()).toPNG());
  }
  console.error('REPORT '+output);
}).finally(async()=>{
  await vite?.close();app.exit(exitCode);
});
