const assert = require("node:assert/strict");

module.exports = async ({ api, js, waitFor, click, clickText, check, screenshot, projectID, sessionID, reload }) => {
  const rows = `Array.from(document.querySelectorAll('[data-project-group-key]')).filter(el => el.getBoundingClientRect().width > 0).map(el => el.dataset.projectGroupKey)`;
  const refresh = () => js(`import('/src/main.tsx').then(({router}) => Promise.all([
    router.options.context.queryClient.invalidateQueries({queryKey:['sessions']}),
    router.options.context.queryClient.invalidateQueries({queryKey:['projects']})
  ]))`);
  const select = id => js(`import('/src/main.tsx').then(({router}) => router.navigate({to:'/',search:{session:${JSON.stringify(id)}}}))`);
  const expectOrder = async expected => {
    await waitFor(async () => JSON.stringify(await js(rows)) === JSON.stringify(expected), `project order ${expected}`);
    assert.deepEqual(await js(rows), expected);
  };
  const activity = async id => (await api("/projects")).projects.find(project => project.id === id).lastActivityAt;

  await js(`import('/src/state/railStore.ts').then(m => m.setRailCollapsed(false))`);
  const other = await api("/projects", "POST", { name: "Newer project", rootDirs: [] });
  await refresh();
  await expectOrder([other.id, projectID]);
  const newest = await api("/sessions", "POST", { title: "Latest activity", provider: "mock", model: "mock", projectID });
  await refresh();
  await select(newest.id);
  await expectOrder([projectID, other.id]);
  const before = await activity(projectID);
  await js(`window.__projectOrders = [];
    window.__projectOrderObserver = new MutationObserver(() => window.__projectOrders.push(${rows}));
    window.__projectOrderObserver.observe(document.querySelector('[data-project-group-key]').parentElement, {childList:true,subtree:true});`);
  await click('.pudding-chat-pane-header button[aria-label="操作"]');
  await clickText("归档", '[role="menuitem"]');
  await waitFor(() => js(`import('/src/main.tsx').then(({router}) => router.state.location.search.draft === '1')`), "archive finishes");
  await refresh();
  await expectOrder([projectID, other.id]);
  assert.equal(await activity(projectID), before);
  const snapshots = await js(`window.__projectOrderObserver.disconnect(); window.__projectOrders`);
  for (const snapshot of snapshots.filter(order => order.length === 2)) {
    assert.deepEqual(snapshot, [projectID, other.id], "archive never transiently reorders projects");
  }
  check("archiving the latest session preserves stored activity and sidebar order without transient jumps");

  await api(`/sessions/${newest.id}`, "DELETE");
  await api(`/projects/${other.id}`, "PATCH", { name: "Renamed project" });
  await refresh();
  await expectOrder([projectID, other.id]);
  assert.equal(await activity(projectID), before);
  reload();
  await expectOrder([projectID, other.id]);
  check("permanent deletion, project rename and renderer reload preserve recent project order");

  await api("/sessions", "POST", { title: "Other activity", provider: "mock", model: "mock", projectID: other.id });
  await refresh();
  await expectOrder([other.id, projectID]);
  await select(sessionID);
  await waitFor(() => js(`Boolean(document.querySelector('textarea'))`), "existing conversation ready");
  await api(`/sessions/${sessionID}/submit`, "POST", { clientMessageID: "project-activity-smoke", parts: [{ type: "text", text: "Activity sorting regression" }] });
  // No manual cache refresh: real SSE must fetch the authoritative project clock.
  await expectOrder([projectID, other.id]);
  await waitFor(async () => !(await api(`/sessions/${sessionID}`)).running, "mock turn completes");
  assert.ok(Date.parse(await activity(projectID)) > Date.parse(before));
  await screenshot("project-activity");
  check("new sessions and real turn activity still advance project order through REST and SSE");
};
