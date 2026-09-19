// Real address input -> native local-file authorization -> same Chromium guest.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { webContents } = require('electron');

module.exports = async ({ api, js, waitFor, click, input, check, screenshot, home, projectRoot, sessionID, browserFileWarnings, browserFileChoices }) => {
  const q = JSON.stringify;
  const inside = path.join(projectRoot, 'preview.html');
  const special = path.join(projectRoot, '中文 #?% 页面.html');
  const outside = path.join(home, 'outside-preview.html');
  for (const [file, text] of [[inside, 'PROJECT_HTML'], [special, 'SPECIAL_FILENAME'], [outside, 'OUTSIDE_HTML']]) {
    fs.writeFileSync(file, `<!doctype html><meta charset="utf-8"><title>${text}</title><h1>${text}</h1><p id="intro">Local address preview</p>`);
  }
  const route = `/sessions/${sessionID}/browser/tabs`;
  const current = await api(route, 'POST', {});
  const other = await api(route, 'POST', {});
  await js(`(async () => {
    const { router } = await import('/src/main.tsx');
    const { queryKeys } = await import('/src/api/queryKeys.ts');
    await router.options.context.queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(${q(sessionID)}) });
    const workspace = await import('/src/state/workspaceStore.ts');
    workspace.openWorkspaceTab(${q(sessionID)}, workspace.browserWorkspaceTabKey(${q(current.id)}));
  })()`);
  const address = 'input[aria-label="搜索或输入网址"]';
  await waitFor(() => js(`Boolean(document.querySelector(${q(address)}))`), 'address bar mounted');
  const tabs = async () => (await api(route)).tabs;
  const originalIDs = (await tabs()).map(tab => tab.id).sort();
  const guest = url => webContents.getAllWebContents().find(contents => contents.getType() === 'webview' && contents.getURL() === url);
  const submit = async value => {
    await input(address, value);
    await waitFor(() => js(`document.querySelector(${q(address)})?.value === ${q(value)}`), 'address draft updated');
    await click('button[aria-label="打开 URL"]');
  };
  const settled = () => waitFor(() => js(`(() => { const button = document.querySelector('button[aria-label="打开 URL"]'); return button && !button.disabled; })()`), 'address navigation settled');
  const expectPage = async (url, text) => {
    await waitFor(async () => (await tabs()).find(tab => tab.id === current.id)?.url === url, 'same tab canonical URL');
    await waitFor(async () => guest(url) && (await guest(url).executeJavaScript('document.querySelector("h1")?.textContent')) === text, 'real HTML rendered');
    await settled();
    assert.deepEqual((await tabs()).map(tab => tab.id).sort(), originalIDs, 'navigation never creates extra tabs');
    assert.equal((await tabs()).find(tab => tab.id === other.id).url, 'about:blank', 'other tab unchanged');
    assert.equal(await js(`document.querySelector('[data-workspace-tab-key=${q(`browser:${current.id}`)}]')?.dataset.selected`), 'true');
  };

  await submit(inside);
  await expectPage(pathToFileURL(inside).href, 'PROJECT_HTML');
  assert.equal(browserFileWarnings.length, 0);
  check('raw project HTML path renders in the current tab without approval');
  const runtime = guest(pathToFileURL(inside).href).id;

  await submit(special);
  await expectPage(pathToFileURL(special).href, 'SPECIAL_FILENAME');
  assert.equal(guest(pathToFileURL(special).href).id, runtime, 'same guest retained');
  const explicit = pathToFileURL(special).href + '?q=1#intro';
  await submit(explicit);
  await expectPage(explicit, 'SPECIAL_FILENAME');
  check('Chinese, spaces and literal filename punctuation work; file URL keeps query and fragment');

  await submit(path.join(projectRoot, 'missing.html'));
  await waitFor(() => js(`document.body.innerText.includes('目标文件或目录不存在')`), 'missing file message');
  await expectPage(explicit, 'SPECIAL_FILENAME');
  await waitFor(() => js(`document.querySelector(${q(address)})?.value === ${q(explicit)}`), 'failed navigation restores address');
  check('missing file gives feedback without navigating, creating or closing tabs');

  browserFileChoices.push(1);
  await submit(outside);
  await waitFor(() => browserFileWarnings.length === 1, 'outside file warning');
  await expectPage(explicit, 'SPECIAL_FILENAME');
  await waitFor(() => js(`document.querySelector(${q(address)})?.value === ${q(explicit)}`), 'cancel restores address');
  assert.ok(browserFileWarnings[0].detail.includes(outside));
  browserFileChoices.push(0);
  await submit(pathToFileURL(outside).href);
  await expectPage(pathToFileURL(outside).href, 'OUTSIDE_HTML');
  assert.equal(browserFileWarnings.length, 2);
  await submit(pathToFileURL(outside).href);
  await expectPage(pathToFileURL(outside).href, 'OUTSIDE_HTML');
  assert.equal(browserFileWarnings.length, 2);
  check('outside path and file URL share confirmation; cancel preserves page, approval navigates in place');

  await click('button[aria-label="后退"]');
  await expectPage(explicit, 'SPECIAL_FILENAME');
  await click('button[aria-label="前进"]');
  await expectPage(pathToFileURL(outside).href, 'OUTSIDE_HTML');
  assert.equal(browserFileWarnings.length, 2);
  check('back/forward keep native page history and existing file authorization');
  assert.equal(browserFileChoices.length, 0);
  await screenshot('browser-local-navigation');
};
