// Real provider protocol -> daemon policy -> approval UI -> sandbox runner.
// All fixtures and commands remain inside the harness's disposable project.
const assert = require('node:assert/strict');
const http = require('node:http');

module.exports = async ({ api, js, waitFor, click, clickText, check, screenshot, projectID, projectRoot }) => {
  const failures = [], executions = [];
  let toolCalls = 0;
  const provider = http.createServer(async (request, response) => {
    try {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const body = JSON.parse(raw);
      const send = (delta, finish = null) => response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`);
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      const lastUser = body.messages.findLastIndex(m => m.role === 'user');
      const results = body.messages.slice(lastUser + 1).filter(m => m.role === 'tool');
      if (!body.tools?.some(t => t.function.name === 'builtin_command_run')) {
        send({ content: 'Command approval fixture' }, 'stop');
      } else if (results.length) {
        executions.push(JSON.parse(results.at(-1).content));
        send({ content: 'Command completed.' }, 'stop');
      } else {
        toolCalls++;
        assert.ok(toolCalls <= 8, 'bounded smoke tool count');
        const changed = JSON.stringify(body.messages[lastUser]).includes('CHANGED');
        const args = { scope: 'project', command: `cat ${changed ? 'file-02.md' : 'file-01.md'}`, env: { PYTHONPATH: projectRoot } };
        send({ tool_calls: [{ index: 0, id: `command-${toolCalls}`, type: 'function', function: { name: 'builtin_command_run', arguments: JSON.stringify(args) } }] }, 'tool_calls');
      }
      response.end('data: [DONE]\n\n');
    } catch (error) { failures.push(error.message); response.destroy(error); }
  });
  await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
  try {
    await api('/providers', 'POST', { id: 'command-smoke', displayName: 'Command smoke', protocol: 'openai-compatible', baseURL: `http://127.0.0.1:${provider.address().port}/v1`, apiKey: 'fixture', models: [{ id: 'commands', contextWindow: 64000, capabilities: { tools: true }, limits: { maxOutputTokens: 2048 } }] });
    await api(`/projects/${projectID}`, 'PATCH', { approvalMode: 'auto' });
    const session = await api('/sessions', 'POST', { title: '命令复用验收', provider: 'command-smoke', model: 'commands', projectID });
    await api(`/sessions/${session.id}`, 'PATCH', { activeMode: 'code', modeLease: 'session' });
    await js(`(async () => {
      const {router} = await import('/src/main.tsx');
      await router.options.context.queryClient.invalidateQueries({queryKey:['sessions']});
      await router.navigate({to:'/', search:{session:${JSON.stringify(session.id)}}});
    })()`);
    await waitFor(() => js(`Boolean(document.querySelector('.pudding-conversation[data-session-id="${session.id}"] textarea'))`), 'command session composer');
    let sequence = 0;
    const submit = async (text = 'SAME') => api(`/sessions/${session.id}/submit`, 'POST', { clientMessageID: `command-smoke-${++sequence}`, text });
    const completed = async (turn) => waitFor(async () => {
      assert.deepEqual(failures, []);
      return (await api(`/sessions/${session.id}/turns/${turn.turnID}`)).status === 'completed';
    }, 'command turn completes');
    const approveSession = async () => {
      await waitFor(() => js(`Boolean(document.querySelector('[data-command-approval-reasons]'))`), 'command reasons visible');
      assert.ok(await js(`document.querySelector('[data-command-approval-reasons]').textContent.includes('自定义运行环境')`));
      await click('[data-approval-panel] [role="option"]:nth-child(2)');
    };
    const first = await submit();
    await approveSession();
    await completed(first);
    const second = await submit();
    await completed(second);
    assert.equal(executions.length, 2);
    assert.ok(executions.every(result => result.ok && result.execution === 'sandbox'), JSON.stringify(executions));
    assert.deepEqual(await api(`/sessions/${session.id}/command-approvals`), { grantCount: 1, reusedCount: 1, approvalReasons: { custom_environment: 1 } });
    check('Auto custom project environment: one confirmation, repeat stays sandboxed');

    const row = `[data-sidebar="menu-item"]:has([data-session-item-id="${session.id}"])`;
    await js(`document.querySelector(${JSON.stringify(row)}).scrollIntoView({block:'nearest'})`);
    await click(`${row} button[aria-label="操作"]`);
    await clickText('命令授权', '[role="menuitem"]');
    await waitFor(() => js(`document.querySelector('[data-command-approvals]')?.textContent.includes('已记住 1 项授权')`), 'session permission status');
    await waitFor(() => js(`(() => { const dialog = document.querySelector('[data-command-approvals]'); return dialog && getComputedStyle(dialog).opacity === '1' && dialog.getAnimations({subtree:true}).every(a => a.playState !== 'running'); })()`), 'dialog entrance animation settles');
    await screenshot('command-permissions');
    await clickText('撤销本会话命令授权');
    await waitFor(() => js(`document.querySelector('[data-command-approvals]')?.textContent.includes('已记住 0 项授权')`), 'revocation refreshed');
    await click('[data-command-approvals] button[data-slot="dialog-close"]');
    const third = await submit();
    await approveSession();
    await completed(third);
    check('real session menu revokes the grant; next execution asks again');

    await api(`/sessions/${session.id}`, 'PATCH', { projectID });
    assert.equal((await api(`/sessions/${session.id}/command-approvals`)).grantCount, 1, 'same project keeps grant');
    const otherProject = await api('/projects', 'POST', { name: 'Other command scope', rootDirs: [projectRoot] });
    await api(`/sessions/${session.id}`, 'PATCH', { projectID: otherProject.id });
    await api(`/sessions/${session.id}`, 'PATCH', { projectID });
    assert.equal((await api(`/sessions/${session.id}/command-approvals`)).grantCount, 0, 'moving away and back invalidates grant');
    const afterMove = await submit();
    await approveSession();
    await completed(afterMove);
    check('same project is stable; moving away and back invalidates the old grant');

    const fourth = await submit('CHANGED');
    await waitFor(async () => (await api(`/sessions/${session.id}/approvals`)).approvals.length === 1, 'changed args require approval');
    const pending = (await api(`/sessions/${session.id}/approvals`)).approvals[0];
    await api(`/sessions/${session.id}/command-approvals`, 'DELETE');
    await api(`/sessions/${session.id}/approvals/${pending.id}/approve`, 'POST', { scope: 'session' });
    await completed(fourth);
    assert.equal(executions.at(-1).reason, 'approval_context_changed');
    assert.equal((await api(`/sessions/${session.id}/command-approvals`)).grantCount, 0);
    check('changed arguments ask; approving a revoked pending request does not execute or recreate a grant');
    assert.deepEqual(failures, []);
  } finally {
    provider.closeAllConnections();
    await new Promise(resolve => provider.close(resolve));
  }
};
