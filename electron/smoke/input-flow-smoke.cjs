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
  window.focus();window.webContents.focus();
  window.setSize(width,700);
  await show([{id:'value',type,allowCustom:true,title:'输入测试值',options:Array.from({length:9},(_,i)=>({title:'选项 '+(i+1),value:'value-'+i}))}]);
  if(type==='single_select') {
    await js(`(()=>{const row=document.querySelector('[data-input-flow-panel] [role=option]');
      window.fixture.presetRow={height:row.getBoundingClientRect().height,badgeLeft:row.querySelector('span[aria-hidden]').getBoundingClientRect().left,
        labelLeft:row.querySelector('div').getBoundingClientRect().left,arrowRight:row.querySelector('svg').getBoundingClientRect().right};})()`);
    await key('End');
    await key('Enter');
  }
  await waitFor('document.activeElement?.tagName === "INPUT"','input before Tab');
  const entryStyle=()=>js(`(()=>{
    const input=document.querySelector('[data-input-flow-entry] input'),entry=input.closest('[role=option]') || input.closest('[data-input-flow-entry]');
    return {background:getComputedStyle(entry).backgroundColor,radius:parseFloat(getComputedStyle(entry).borderRadius),inputBackground:getComputedStyle(input).backgroundColor,
      border:getComputedStyle(input).borderWidth,pencil:!!entry.querySelector('.lucide-pencil'),focused:document.activeElement===input};
  })()`);
  const idle=await entryStyle();
  assert.notEqual(idle.background,'rgba(0, 0, 0, 0)','focused input row is highlighted without hover');
  assert.equal(idle.inputBackground,'rgba(0, 0, 0, 0)');
  assert.equal(idle.border,'0px','input has no separate border');
  assert.equal(idle.pencil,true,'edit badge replaces the custom row number');
  assert.equal(idle.focused,true);
  assert.ok(await js(`Array.from(document.querySelectorAll('[data-input-flow-body] span[aria-hidden]')).every(badge=>{
    const r=badge.getBoundingClientRect(),radius=parseFloat(getComputedStyle(badge).borderRadius);
    const row=badge.closest('[role=option]') || badge.closest('[data-input-flow-entry]'),rr=row.getBoundingClientRect();
    return r.width===28 && r.height===28 && radius===${idle.radius} && radius>0 && radius<r.height/2
      && r.left-rr.left===8 && r.top-rr.top===8 && rr.bottom-r.bottom===8 && parseFloat(getComputedStyle(row).paddingRight)===8;
  })`),'option numbers and edit badge use matching small-radius squares with 8px spacing on all sides');
  await js('document.querySelector("[data-input-flow-header] button").focus()');
  assert.equal((await entryStyle()).background,'rgba(0, 0, 0, 0)','row clears when focus and pointer are outside');
  window.webContents.sendInputEvent({type:'mouseMove',...await point('[data-input-flow-entry] input')});
  await delay(60);
  assert.equal((await entryStyle()).background,idle.background,'hover and focus use the same row background');
  assert.ok((await entryStyle()).radius>0,'hover background keeps rounded corners');
  window.webContents.sendInputEvent({type:'mouseMove',x:0,y:0});
  await delay(60);
  assert.equal((await entryStyle()).background,'rgba(0, 0, 0, 0)','leaving an unfocused row clears the background');
  await js('document.querySelector("[data-input-flow-entry] input").focus()');
  assert.equal((await entryStyle()).background,idle.background,'focus restores the background without pointer hover');
  await window.webContents.insertText('12');
  await key('Tab');
  await delay(150);
  assert.equal((await entryStyle()).background,idle.background,'row stays highlighted when focus moves to its send button');
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
  if(geometry.scrolling) assert.ok(geometry.clearance.right>=8,'row inset keeps the focus ring inside the scroll viewport');
  const layout=await js(`(()=>{
    const input=document.querySelector('[data-input-flow-panel] input'),button=document.activeElement;
    const ir=input.getBoundingClientRect(),br=button.getBoundingClientRect(),row=input.closest('[role=option]');
    const preset=window.fixture.presetRow;
    const title=document.querySelector('[data-input-flow-step-title]').firstElementChild.getBoundingClientRect();
    const headingIcon=document.querySelector('[data-input-flow-header] > svg').getBoundingClientRect(),close=document.querySelector('[data-input-flow-header] button').getBoundingClientRect();
    const entry=input.closest('[data-input-flow-entry]'),badgeRect=entry.querySelector('span[aria-hidden]').getBoundingClientRect();
    const panel=input.closest('[data-input-flow-panel]').getBoundingClientRect(),entryRow=(row || entry).getBoundingClientRect();
    const firstControl=document.querySelector('[data-input-flow-step-title]').nextElementSibling.getBoundingClientRect();
    const badge=row?.querySelector('span[aria-hidden]');
    return {inputHeight:ir.height,buttonHeight:br.height,centerOffset:ir.top+ir.height/2-br.top-br.height/2,
      outerSpacing:{left:entryRow.left-panel.left,right:panel.right-entryRow.right},
      bottomPadding:parseFloat(getComputedStyle(input.closest('[data-input-flow-panel]')).paddingBottom),questionGap:firstControl.top-title.bottom,
      headingIconLeft:headingIcon.left,headerRight:close.right,titleLeft:title.left,titleRight:title.right,entryLeft:badgeRect.left,entryRight:br.right,
      row:row ? {height:row.getBoundingClientRect().height,previousHeight:preset.height,
        badgeLeft:badge?.getBoundingClientRect().left,previousBadgeLeft:preset.badgeLeft,
        inputLeft:ir.left,labelLeft:preset.labelLeft,
        buttonRight:br.right,arrowRight:preset.arrowRight} : null};
  })()`);
  console.log('LAYOUT '+type+'/'+width+' '+JSON.stringify(layout));
  assert.equal(layout.inputHeight,32,'input uses the standard 32px control height');
  assert.equal(layout.buttonHeight,32,'confirmation matches input height');
  assert.equal(layout.centerOffset,0,'controls share a center line');
  assert.equal(layout.outerSpacing.left,layout.outerSpacing.right,'row background has equal left and right outer spacing');
  assert.equal(layout.bottomPadding,4,'bottom spacing matches the compact side spacing');
  assert.equal(layout.questionGap,8,'question and controls use an 8px gap');
  assert.equal(layout.titleLeft,layout.headingIconLeft,'question aligns with the top question icon');
  assert.equal(layout.titleRight,layout.headerRight,'question right edge aligns with the header action');
  assert.equal(layout.entryLeft,layout.titleLeft,'entry left edge aligns with the question');
  assert.equal(layout.entryRight,layout.titleRight,'entry right edge aligns with the question');
  if(layout.row) {
    assert.equal(layout.row.height,44,'custom option keeps the standard 44px row');
    assert.equal(layout.row.height,layout.row.previousHeight,'custom and preset rows match');
    assert.equal(layout.row.badgeLeft,layout.row.previousBadgeLeft,'edit badge aligns with option numbers');
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
          import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
          import {renderToStaticMarkup} from 'react-dom/server';
          import '/src/styles.css';
          import {InputFlowPanel} from '/src/components/transcript/InputFlowToolPart.tsx';
          import {FormResultCard} from '/src/components/transcript/FormResultCard.tsx';
          import {ChoiceMenu} from '/src/components/ChoiceMenu.tsx';
          import {ComposerApprovalBar} from '/src/components/ComposerApprovalBar.tsx';
          import {showInputFlow,useInputFlowStore} from '/src/state/inputFlowStore.ts';
          import {setLocale} from '/src/i18n/index.ts';
          import {initComposerTestState,composerTestPresentation} from '/src/dev/composerTestState.ts';
          import {createInputFlowTools} from '/src/mcp/inputFlowTools.ts';
          setLocale('zh-CN');
          const h=React.createElement;
          window.fixture={submissions:[],resultHTML:part=>renderToStaticMarkup(h(FormResultCard,{part})),show(args) {
            this.submissions=[];
            showInputFlow({args,sessionID:'input-flow-smoke',title:args.title});
          },showTool(args) {
            this.submissions=[];
            return createInputFlowTools()[0].handler({...args,_pudding_session_id:'input-flow-smoke'});
          },showComposerTest() {
            sessionStorage.setItem('pudding.composerTestState','interaction');
            initComposerTestState();
            this.show(composerTestPresentation('input-flow-smoke').inputFlow.args);
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
          const root=createRoot(document.getElementById('root'));
          root.render(h(Fixture));
          window.fixture.showApproval=()=>{
            sessionStorage.setItem('pudding.composerTestState','approval');
            initComposerTestState();
            root.render(h(QueryClientProvider,{client:new QueryClient()},
              h('main',{style:{position:'absolute',inset:24}},
                h('div',{style:{position:'absolute',bottom:0,left:0,right:0}},
                  h(ComposerApprovalBar,{approval:composerTestPresentation('input-flow-smoke').approval,preview:true,token:''}),
                  h('div',{style:{height:120,border:'1px solid var(--border)',borderRadius:'var(--radius)',padding:16,color:'var(--muted-foreground)'}},'输入消息')))));
          };
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

  await js('window.fixture.showComposerTest()');
  await waitFor('document.querySelectorAll("[data-input-flow-panel] [role=option]").length===2','simulation choices');
  await click(option(1));
  await waitFor('document.querySelectorAll("[data-input-flow-panel] [role=option]").length===4','presets and custom choice');
  await waitFor('!!document.querySelector("[data-input-flow-entry] input")','simulation custom input');
  assert.equal(await js('document.querySelectorAll("[data-input-flow-panel] [role=option]").length'),4,'presets remain visible while editing');
  await click('[data-input-flow-entry] input');
  fs.writeFileSync(path.join(output,'simulation-presets-custom.png'),(await window.webContents.capturePage()).toPNG());
  await window.webContents.insertText('只验证键盘操作');await key('Enter');
  await waitFor('document.querySelector("[data-input-flow-entry] input")?.type==="text"','simulation final text step');
  await click('[data-input-flow-entry] button:first-of-type');
  assert.deepEqual(await js('window.fixture.submissions[0].result'),{type:'user_input_result',title:'选择执行方式',mode:'continue',validation:'只验证键盘操作'});
  console.log('PASS interaction simulation includes presets with custom input');

  for (const value of ['001','',0,false,null,{mode:'focused',limit:2},['a','b']]) {
    await js(`window.fixture.showTool(${JSON.stringify({type:'form',title:'问题',steps:[{id:'answer',type:'single_select',title:'选择方案',allowCustom:true,options:[{title:'模型指定的方案',value}]}]})})`);
    await waitFor('!!document.querySelector("[data-input-flow-panel] [role=option]")','model-defined choice');
    await click(option(1));
    await submitted({answer:value});
    assert.ok(await js('window.fixture.submissions[0].text.includes("模型指定的方案")'),'submission preserves the label for any JSON choice value');
    assert.ok((await js('window.fixture.resultHTML(window.fixture.submissions[0].formResult)')).includes('模型指定的方案'),'result card preserves the choice label');
  }
  assert.ok((await js(`window.fixture.resultHTML(${JSON.stringify({type:'form_result',title:'旧记录',schema:{type:'form',title:'旧记录',steps:[{id:'quantity',type:'quick_number',title:'数量'}]},result:{quantity:2}})})`)).includes('数量'),'completed legacy records still render through the generic result card');
  console.log('PASS model-defined JSON values and immutable historical result rendering');

  // Exercise optional repeat fields through the actual tool handler, not just the panel.
  await js(`window.fixture.showTool(${JSON.stringify({type:'repeat',title:'问题',minItems:1,maxItems:1,
    repeatSteps:[{id:'room',type:'single_select',title:'房型',options:['标准间'],required:false},
      {id:'quantity',type:'single_select',allowCustom:true,title:'数量',options:[1,2],required:false}],
    nextSteps:[{id:'extra',type:'single_select',allowCustom:true,title:'额外要求',options:['无','其他'],required:false},
      {id:'count',type:'single_select',allowCustom:true,title:'执行方式',options:['自动','手动']},
      {id:'note',type:'text_input',title:'备注',required:false},
      {id:'contact',type:'text_input',title:'联系人'},
      {id:'confirmed',type:'confirm',title:'确认记录',required:false}]})})`);
  const repeatAction='[data-input-flow-body] button:not([role="option"])';
  await waitFor(`document.querySelector('${repeatAction}')?.textContent==='跳过'`,'optional repeated choice can skip');
  await click(repeatAction);
  await click('[data-input-flow-entry] input');await window.webContents.insertText('99');
  await click(repeatAction+':first-of-type');
  assert.equal(await js(`document.querySelector('${option(2)}').disabled`),true,'an entirely skipped record does not satisfy minItems');
  await click(option(1));
  await waitFor(`document.querySelector('[data-input-flow-step-title]')?.textContent==='房型'`,'continue resets an empty record');
  await click(option(1));
  assert.equal(await js('document.querySelector("[data-input-flow-entry] input").value'),'','discarded number does not leak into the next record');
  await click(repeatAction+':first-of-type');
  assert.equal(await js('document.querySelectorAll("[data-input-flow-panel] [role=option]").length'),2,'maxItems counts only the non-empty record');
  await click(option(1));
  await waitFor(`document.querySelector('[data-input-flow-step-title]')?.textContent==='额外要求'`,'nextSteps begin after record collection');
  await click('[data-input-flow-entry] input');await window.webContents.insertText('9');
  await click(repeatAction+':first-of-type');
  assert.equal(await js('document.querySelector("[data-input-flow-entry] input").value'),'','skipped nextStep numeric draft is cleared');
  assert.equal(await js(`document.querySelectorAll('${repeatAction}').length`),1,'required numeric field has no skip action');
  await click('[data-input-flow-entry] input');await window.webContents.insertText('3');await key('Enter');
  await window.webContents.insertText('不提交这段备注');
  await click(repeatAction+':first-of-type');
  assert.equal(await js('document.querySelector("[data-input-flow-entry] input").value'),'','skipped nextStep text draft is cleared');
  assert.equal(await js(`document.querySelectorAll('${repeatAction}').length`),1,'required text field has no skip action');
  await window.webContents.insertText('张三');await key('Enter');
  assert.equal(await js(`Array.from(document.querySelectorAll('[data-input-flow-panel] button')).some(b=>b.textContent==='跳过')`),false,'confirmation cannot be skipped even if required=false');
  assert.equal(await js('window.fixture.submissions.length'),0,'answers are accumulated until the flow is complete');
  await click(option(1));
  await submitted({items:[{room:'标准间',roomLabel:'标准间'}],count:'3',contact:'张三',confirmed:true});

  await js(`window.fixture.showTool(${JSON.stringify({type:'repeat',title:'问题',minItems:1,maxItems:2,
    repeatSteps:[{id:'quantity',type:'single_select',allowCustom:true,title:'数量',options:[1,2],required:false}]})})`);
  await waitFor('!!document.querySelector("[data-input-flow-panel] [role=listbox]")','second repeated flow');
  await click(option(2));
  await click(option(1));
  await click(repeatAction+':first-of-type');
  assert.equal(await js(`document.querySelector('${option(2)}').disabled`),false,'previous completed record still satisfies minItems');
  await click(option(2));
  await submitted({items:[{quantity:2,quantityLabel:'2'}]});
  console.log('PASS repeat optional steps, draft isolation, empty records and aggregate submission');

  for(const width of [760,420]) {
    for(const type of ['single_select','text_input']) await checkInputFocusRing(type,width);
  }
  window.setSize(760,700);
  console.log('PASS input confirmation focus rings');

  await show();
  assert.equal(await js('document.activeElement.getAttribute("role")'),'listbox');
  assert.equal(await js('document.querySelector("[data-input-flow-panel] [role=option] > span").textContent'),'1');
  // Establish the keyboard starting point; pointer hover may have selected another row.
  await key('Home');
  assert.equal(await js(`document.querySelector('${option(1)}').getAttribute('aria-selected')`),'true');
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
    for(const type of ['multi_select','text_input','phone_input','number_input','date_input','single_select']) {
      await show([{...optionalChoices,type,allowCustom:true,options:type==='single_select' ? [1,2] : optionalChoices.options}]);
      if(type==='multi_select') await key('2');
      if(type==='single_select') await key('3');
      if(type==='text_input' || type==='single_select') await window.webContents.insertText('12');
      const actions=await js(`(()=>{
        const body=document.querySelector('[data-input-flow-body]'),r=body.getBoundingClientRect();
        return [...body.querySelectorAll('button:not([role="option"])')].map(b=>{
          const br=b.getBoundingClientRect();
          return {label:b.textContent,top:br.top,height:br.height,left:br.left,right:br.right,inside:br.left>=r.left && br.right<=r.right};
        });
      })()`);
      assert.deepEqual(actions.map(a=>a.label),['跳过',type==='multi_select' ? '确定' : '发送']);
      assert.equal(actions[0].top,actions[1].top,`${type}/${width}: actions share a row`);
      assert.equal(actions[0].height,actions[1].height,`${type}/${width}: actions share a height`);
      assert.ok(actions[1].left>actions[0].right && actions.every(a=>a.inside),'actions fit without overlap');
      if(type==='multi_select') {
        await js('document.documentElement.classList.add("dark")');
        fs.writeFileSync(path.join(output,'question-actions-'+width+'.png'),(await window.webContents.capturePage()).toPNG());
        await js('document.documentElement.classList.remove("dark")');
      }
      await click(actionButton+':first-of-type');
      await submitted({});
    }
  }
  window.setSize(760,700);
  await show([optionalChoices]);
  await key('1');
  await click(actionButton+':last-of-type');
  await submitted({answer:['猪里脊']});
  await show([optionalChoices]);
  await click(actionButton+':last-of-type');
  await submitted({answer:[]});
  for(const allowCustom of [false,true]) {
    await show([{...optionalChoices,type:'single_select',allowCustom}]);
    assert.equal(await js(`document.querySelectorAll(${JSON.stringify(actionButton)}).length`),allowCustom ? 2 : 1);
    await click(actionButton+':first-of-type');
    await submitted({});
  }
  console.log('PASS optional field action rows, confirm and skip');

  await show([{id:'quantity',type:'number_input',title:'需要多少份？',min:0,max:20}]);
  await waitFor('document.activeElement?.tagName === "INPUT"','numeric field focus');
  assert.equal(await js('document.querySelector("[data-input-flow-entry] button").disabled'),true,'blank custom value cannot send even when zero is allowed');
  await window.webContents.insertText('12');
  await key('Enter');
  await submitted({quantity:12});
  await show([{id:'method',type:'single_select',allowCustom:true,title:'如何执行？',options:[{title:'自动',value:'auto'},{title:'手动',value:'manual'}]}]);
  assert.equal(await js('!!document.querySelector("[data-input-flow-entry] input")'),true,'custom input is visible without activating the row');
  await click('[data-input-flow-entry] input');
  await waitFor('document.activeElement?.tagName === "INPUT"','clicked custom number focus');
  await window.webContents.insertText('自定义执行方式');
  await waitFor('document.querySelector("[data-input-flow-panel] input").value === "自定义执行方式"','custom text typed');
  await click('[data-input-flow-panel] input');
  assert.equal(await js('document.activeElement.tagName'),'INPUT');
  await js('document.querySelector("[data-input-flow-panel] [role=option] button").focus()');
  assert.equal(await js('document.activeElement.tagName'),'BUTTON');
  await key('Enter');
  await submitted({method:'自定义执行方式'});
  await show([{id:'method',type:'single_select',allowCustom:true,title:'如何执行？',options:['自动','手动']}]);
  await key('3');
  await waitFor('document.activeElement?.tagName === "INPUT"','custom input before dismissal');
  await key('Escape');
  assert.equal(await js('!!document.querySelector("[data-input-flow-panel]")'),false);
  assert.equal(await js('window.fixture.submissions.length'),0);
  console.log('PASS custom input owns typing, caret and embedded button keyboard');

  await show([singleStep,{id:'note',title:'补充说明',type:'text_input',required:false}]);
  await key('1');
  await waitFor('!!document.querySelector("[data-input-flow-panel] input")','next text step');
  assert.equal(await js('document.querySelector("[data-input-flow-entry] button:last-of-type").disabled'),true,'empty optional text uses the existing skip action');
  await window.webContents.insertText('123 测试说明');
  await js('document.querySelector("[data-input-flow-entry] input").dispatchEvent(new KeyboardEvent("keydown",{key:"Enter",isComposing:true,bubbles:true}))');
  assert.equal(await js('window.fixture.submissions.length'),0,'IME confirmation does not send');
  await key('Enter');
  await submitted({answer:'yes',note:'123 测试说明'});
  console.log('PASS multi-step flow and text input');

  await js(`window.fixture.show(${JSON.stringify({type:'repeat',title:'问题',maxItems:1,
    repeatSteps:[{id:'quantity',type:'number_input',title:'需要多少份？',min:1}],
    afterItem:{title:'继续收集吗？'},nextSteps:[{id:'confirmed',type:'confirm',title:'确认这些信息？'}]})})`);
  await waitFor('document.activeElement?.tagName === "INPUT"','repeat numeric item');
  await delay(80);
  await window.webContents.insertText('2');await key('Enter');
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
  await js('window.fixture.showApproval()');
  await waitFor('!!document.querySelector("[data-approval-panel]")','approval preview');
  for(const width of [760,420]) {
    window.setSize(width,700);
    for(const dark of [false,true]) {
      await js(`document.documentElement.classList.toggle('dark',${dark})`);
      await delay(100);
      const layout=await js(`(()=>{
        const panel=document.querySelector('[data-approval-panel]'),header=panel.querySelector('[data-approval-header]'),menu=panel.querySelector('[role=listbox]');
        const rows=[...menu.children],p=panel.getBoundingClientRect(),h=header.getBoundingClientRect(),r=rows[0].getBoundingClientRect();
        const icon=rows[0].querySelector('span'),is=getComputedStyle(icon),command=panel.querySelector('pre').parentElement;
        return {bottomPadding:parseFloat(getComputedStyle(panel).paddingBottom),headingSize:parseFloat(getComputedStyle(header.querySelector('span')).fontSize),
          iconLeft:icon.getBoundingClientRect().left,headerLeft:h.left,commandLeft:command.getBoundingClientRect().left,
          outerLeft:r.left-p.left,outerRight:p.right-r.right,rows:rows.map(row=>row.getBoundingClientRect().height),
          iconSize:icon.getBoundingClientRect().width,iconBorder:is.borderWidth,iconBackground:is.backgroundColor,
          divider:getComputedStyle(menu).borderTopWidth,shadow:getComputedStyle(panel).boxShadow,
          fits:panel.scrollWidth<=panel.clientWidth && p.top>=0 && p.bottom<=innerHeight,command:command.textContent};
      })()`);
      assert.equal(layout.bottomPadding,4);
      assert.equal(layout.headingSize,14);
      assert.equal(layout.iconLeft,layout.headerLeft,'approval icons align with the header');
      assert.equal(layout.commandLeft,layout.headerLeft,'command aligns with the header');
      assert.equal(layout.outerLeft,layout.outerRight,'approval rows have balanced horizontal insets');
      assert.deepEqual(layout.rows,[56,56],'two-line approval options use consistent compact rows');
      assert.equal(layout.iconSize,16);
      assert.equal(layout.iconBorder,'0px','approval icons do not use numbered-choice boxes');
      assert.equal(layout.iconBackground,'rgba(0, 0, 0, 0)');
      assert.equal(layout.divider,'0px');
      assert.notEqual(layout.shadow,'none');
      assert.equal(layout.fits,true);
      assert.ok(layout.command.includes('npm --prefix web run build'));
      fs.writeFileSync(path.join(output,'approval-'+width+(dark?'-dark':'-light')+'.png'),(await window.webContents.capturePage()).toPNG());
    }
  }
  await js('document.querySelector("[data-approval-panel] [role=listbox]").focus()');
  await key('Home');await key('Down');
  assert.equal(await js('document.querySelector("[data-approval-panel] [role=option]:nth-child(2)").getAttribute("aria-selected")'),'true');
  console.log('PASS approval layout, original icons, light/dark themes and keyboard navigation');
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
