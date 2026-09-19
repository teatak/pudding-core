const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { createLocalFileBrowserOpener } = require("../local-file-browser.cjs");

function fixture(t) {
  const home = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "pudding-file-preview-")));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const project = path.join(home, "project");
  fs.mkdirSync(project);
  const inside = path.join(project, "中文 #?%.html"), outside = path.join(home, "outside.html");
  for (const file of [inside, outside]) fs.writeFileSync(file, "<h1>fixture</h1>");
  const state = { confirmations: [], creates: 0, grants: [], tabs: [], requests: [], allowed: true, hasProject: true };
  const owner = { isDestroyed: () => false };
  const requestAPI = async (route, method) => {
    state.requests.push([route, method]);
    if (method === "POST") { state.creates++; return { id: `tab-${state.creates}` }; }
    if (route.startsWith("/sessions/")) return { projectID: state.hasProject ? "project-1" : "" };
    return { rootDirs: [project] };
  };
  const host = {
    listTabs: ({ sessionID }) => ({ tabs: state.tabs.filter(tab => tab.sessionID === sessionID) }),
    ensure: async request => {
      state.grants.push(request);
      const tab = { ...request }; state.tabs.push(tab); return tab;
    },
  };
  const confirm = async (_owner, filename) => {
    state.confirmations.push(filename);
    if (state.onConfirm) return state.onConfirm();
    return state.allowed;
  };
  const open = createLocalFileBrowserOpener({ host, requestAPI, confirm });
  const request = (file, sessionID = "session-1") => ({ sessionID, url: pathToFileURL(file).href + "?q=1#intro" });
  return { home, project, inside, outside, state, owner, open, request, host };
}

test("project file opens without warning, keeps URL encoding/query/hash and reuses the live tab", async t => {
  const f = fixture(t), req = f.request(f.inside);
  assert.equal((await f.open(f.owner, req)).ok, true);
  assert.deepEqual(f.state.confirmations, []);
  assert.equal(f.state.grants[0].fileRoot, f.project);
  assert.equal(f.state.grants[0].url, req.url);
  assert.equal((await f.open(f.owner, req)).ok, true);
  assert.equal(f.state.creates, 1);
});

test("cancel never creates a tab; renderer-supplied approval cannot bypass the native dialog", async t => {
  const f = fixture(t); f.state.allowed = false;
  const result = await f.open(f.owner, { ...f.request(f.outside), confirmed: true, fileRoot: f.home, _fileAuthorized: true });
  assert.deepEqual(result, { ok: false, cancelled: true });
  assert.deepEqual(f.state.confirmations, [f.outside]);
  assert.equal(f.state.creates, 0);
  assert.deepEqual(f.state.grants, []);
});

test("outside confirmation grants exactly the file; close, new session and projectless preview prompt again", async t => {
  const f = fixture(t), req = f.request(f.outside);
  await f.open(f.owner, req);
  assert.deepEqual(f.state.grants[0].fileRoot, { file: f.outside });
  await f.open(f.owner, req);
  assert.equal(f.state.confirmations.length, 1);
  f.state.tabs = [];
  await f.open(f.owner, req);
  assert.equal(f.state.confirmations.length, 2);
  f.state.hasProject = false;
  await f.open(f.owner, f.request(f.inside, "other-session"));
  assert.equal(f.state.confirmations.length, 3);
  assert.equal(f.state.grants.at(-1).sessionID, "other-session");
  assert.ok(f.state.requests.every(([, method]) => !method || method === "POST"));
  assert.ok(f.state.requests.every(([route, method]) => !method || route.endsWith("/browser/tabs")));
});

test("symlink escaping project warns for the canonical file, and changing its target during confirmation aborts", async t => {
  const f = fixture(t), link = path.join(f.project, "link.html");
  fs.symlinkSync(f.outside, link);
  f.state.onConfirm = () => { fs.unlinkSync(link); fs.symlinkSync(f.inside, link); return true; };
  assert.deepEqual(await f.open(f.owner, f.request(link)), { ok: false, reason: "changed" });
  assert.deepEqual(f.state.confirmations, [f.outside]);
  assert.equal(f.state.creates, 0);
});

test("concurrent clicks share one pending confirmation, but do not retain permission afterward", async t => {
  const f = fixture(t); let complete;
  f.state.onConfirm = () => new Promise(resolve => { complete = resolve; });
  const first = f.open(f.owner, f.request(f.outside));
  const second = f.open(f.owner, f.request(f.outside));
  while (!complete) await new Promise(resolve => setImmediate(resolve));
  complete(true);
  assert.deepEqual(await first, await second);
  assert.equal(f.state.confirmations.length, 1);
  assert.equal(f.state.creates, 1);
});

test("invalid, remote, missing and non-file destinations never prompt or create a tab", async t => {
  const f = fixture(t);
  for (const url of ["https://example.com", "file:relative", "file://remote/tmp/a", "file:////remote/a", "file:///tmp/a%2Fb", "file:///tmp/a%5Cb", "file:///tmp/%00", "file:///tmp/a#%00", "file:///tmp/%XX", pathToFileURL(f.home).href, pathToFileURL(path.join(f.home, "missing")).href]) {
    assert.equal((await f.open(f.owner, { sessionID: "session-1", url })).ok, false, url);
  }
  assert.equal(f.state.creates, 0);
  assert.deepEqual(f.state.confirmations, []);
});

test("a destroyed owner does not open the file after confirmation", async t => {
  const f = fixture(t);
  f.state.onConfirm = () => { f.owner.isDestroyed = () => true; return true; };
  assert.deepEqual(await f.open(f.owner, f.request(f.outside)), { ok: false, cancelled: true });
  assert.equal(f.state.creates, 0);
});
