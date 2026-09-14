// Real daemon + SSE + renderer regression. The local provider grows one tool
// loop, then holds its first post-compaction response so Stop can be exercised.
const assert = require('node:assert/strict');
const http = require('node:http');

module.exports = async function verifyContextCompaction({ api, js, waitFor, click, input, check, screenshot, reload, pressEnter, scrollUp }) {
  let calls = 0, summaries = 0, heldResponse;
  let manualSummary;
  const failures = [];
  const provider = http.createServer(async (request, response) => {
    try {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      const body = JSON.parse(raw);
      const isSummary = body.messages.some(m => m.role === 'system' && m.content.includes('You compact conversation history'));
      const send = (delta, finish) => response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta, finish_reason: finish ?? null }] })}\n\n`);
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      if (isSummary) {
        summaries++;
        if (manualSummary?.hold) await new Promise(resolve => { manualSummary.release = resolve; });
        send({ content: manualSummary?.text ?? '## TODO / Open Questions\nContinue COMPACT_DESKTOP_TASK and preserve the latest time result.' });
        send({}, manualSummary?.finish ?? 'stop');
        response.end('data: [DONE]\n\n');
        return;
      }
      if (JSON.stringify(body.messages).includes('MANUAL_COMPACT_SOURCE')) {
        send({ content: 'Recorded the facts.' });
        send({}, 'stop');
        response.end('data: [DONE]\n\n');
        return;
      }
      calls++;
      assert.ok(calls <= 24, 'one running turn should compact before 24 tool exchanges');
      assert.ok(JSON.stringify(body.messages).includes('COMPACT_DESKTOP_TASK'), 'current input retained');
      if (summaries > 0) {
        const results = body.messages.filter(m => m.role === 'tool');
        assert.ok(results.some(m => m.tool_call_id === `smoke-call-${calls - 1}`), 'latest tool result retained');
        assert.ok(results.length < calls - 1, 'older tool exchanges removed from the request');
        heldResponse = response;
        response.flushHeaders();
        return;
      }
      assert.ok(body.tools.some(t => t.function.name === 'builtin_time_get_current'), 'real built-in tool is available');
      send({ content: `Step ${calls}: ${'progress '.repeat(1500)}` });
      send({ tool_calls: [{ index: 0, id: `smoke-call-${calls}`, type: 'function', function: { name: 'builtin_time_get_current', arguments: '{"timezone":"UTC"}' } }] });
      send({}, 'tool_calls');
      response.end('data: [DONE]\n\n');
    } catch (error) {
      failures.push(error.message);
      response.destroy(error);
    }
  });
  await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
  try {
    await api('/providers', 'POST', {
      id: 'compact-smoke', displayName: 'Compaction smoke', protocol: 'openai-compatible',
      baseURL: `http://127.0.0.1:${provider.address().port}/v1`, apiKey: 'local-fixture',
      models: [{ id: 'compact-model', contextWindow: 32000, capabilities: { tools: true }, limits: { maxOutputTokens: 4096 } }],
    });
    const session = await api('/sessions', 'POST', { title: 'Compaction acceptance', provider: 'compact-smoke', model: 'compact-model' });
    await js(`(async () => {
      const { router } = await import('/src/main.tsx');
      await router.options.context.queryClient.invalidateQueries({ queryKey: ['sessions'] });
      await router.options.context.queryClient.invalidateQueries({ queryKey: ['providers'] });
      await router.navigate({ to: '/', search: { session: ${JSON.stringify(session.id)} } });
    })()`);
    await waitFor(() => js(`new URL(location.href).searchParams.get('session') === ${JSON.stringify(session.id)} && Boolean(document.querySelector('textarea'))`), 'compaction composer');
    const submitted = await api(`/sessions/${session.id}/submit`, 'POST', { clientMessageID: 'compact-desktop', text: 'COMPACT_DESKTOP_TASK: continue checking time until stopped.' });
    await waitFor(() => {
      assert.deepEqual(failures, []);
      return Boolean(heldResponse);
    }, 'running turn compacts and resumes', 45000);
    await waitFor(() => js(`(async () => {
      const { useOverlayStore } = await import('/src/state/overlayStore.ts');
      return useOverlayStore.getState().runningTurns[${JSON.stringify(session.id)}] === ${JSON.stringify(submitted.turnID)}
        && Boolean(document.querySelector('button[aria-label="停止"]:not(:disabled)'));
    })()`), 'summary completion keeps the active turn stoppable');
    const messages = await api(`/sessions/${session.id}/messages`);
    const canonical = Array.isArray(messages) ? messages : messages.messages;
    assert.ok(canonical.some(m => m.role === 'summary'), 'summary was committed');
    assert.ok(canonical.filter(m => m.role === 'summary').every(m => m.turnID === submitted.turnID), 'running summaries belong to the executing turn');
    assert.equal((await api(`/sessions/${session.id}/turns`)).turns.length, 1, 'compaction must not create another turn');
    assert.ok(canonical.some(m => m.parts?.some(p => p.id === 'smoke-call-1')), 'old canonical tool messages remain available');
    await waitFor(() => js(`document.body.innerText.includes('上下文已压缩') && !document.body.innerText.includes('整理 0 轮历史')`), 'within-turn compaction shows message counts');
    const measureCards = `(() => {
      const card = document.querySelector('[data-compact-message-id]');
      const turn = card?.closest('[data-transcript-turn-id]');
      const label = Array.from(turn?.querySelectorAll('span') || []).find(el => el.textContent === '查看当前时间');
      const tool = label?.closest('[class~="h-6"]');
      return { turnID: turn?.getAttribute('data-transcript-turn-id'), cardHeight: card?.getBoundingClientRect().height, toolHeight: tool?.getBoundingClientRect().height,
        toolCount: Array.from(turn?.querySelectorAll('span') || []).filter(el => el.textContent === '查看当前时间').length,
        cardCount: turn?.querySelectorAll('[data-compact-message-id]').length };
    })()`;
    const layout = await js(measureCards);
    assert.equal(layout.turnID, submitted.turnID, 'card is inside the current transcript turn');
    assert.equal(layout.cardHeight, layout.toolHeight, 'compact and tool cards have equal collapsed height');
    assert.equal(layout.cardHeight, 24, 'shared 24px activity row');
    assert.equal(layout.toolCount, calls - 1, 'canonical prefix must not duplicate streamed tools');
    assert.equal(layout.cardCount, 1);
    await screenshot('context-compaction-running');
    check(`running turn compacted after ${calls - 1} tool exchanges; current input, latest result and Stop retained`);
    await click('button[aria-label="停止"]');
    await waitFor(async () => (await api(`/sessions/${session.id}/turns/${submitted.turnID}`)).status === 'cancelled', 'Stop commits cancelled turn');
    await waitFor(() => js(`(async () => {
      const { useOverlayStore } = await import('/src/state/overlayStore.ts');
      const state = useOverlayStore.getState();
      return !state.runningTurns[${JSON.stringify(session.id)}]
        && !document.querySelector('button[aria-label="停止"]');
    })()`), 'real Stop cancels the resumed turn');
    assert.deepEqual(failures, []);
    check('real Stop cancels the post-compaction provider request and reconciles SSE state');
    reload();
    await waitFor(async () => {
      try { return Boolean(await js(`document.querySelector('[data-compact-message-id]')`)); }
      catch { return false; }
    }, 'canonical card restored after renderer reload');
    assert.deepEqual(await js(measureCards), layout, 'reload preserves one turn, one compact card and tool heights');
    check('compaction stays inside the executing turn after cancellation and reload; tool and compact rows are 24px');

    // Hold the real manual compact request briefly to inspect its pending UI.
    await js(`(() => { const originalFetch = window.fetch; window.__compactFetch = originalFetch;
      window.fetch = async (...args) => {
        if (String(args[0]).endsWith('/compact')) {
          await new Promise(resolve => { window.__releaseCompact = resolve; });
        }
        return originalFetch(...args);
      }; })();`);
    await input('textarea', '/compact');
    await click('button[aria-label="发送"]');
    await waitFor(() => js(`Boolean(window.__releaseCompact)`), 'manual compact pending request');
    const pending = await js(`(() => {
      const title = Array.from(document.querySelectorAll('.pudding-transcript-turn span')).find(el => el.textContent === '正在压缩上下文');
      const row = title?.closest('[class~="h-6"]');
      const spinner = row?.querySelector('[data-slot="spinner"]');
      return { height: row?.getBoundingClientRect().height, first: Boolean(spinner && row.firstElementChild.contains(spinner)), icons: row?.querySelectorAll('svg').length, spinners: row?.querySelectorAll('[data-slot="spinner"]').length };
    })()`);
    assert.deepEqual(pending, { height: 24, first: true, icons: 0, spinners: 1 }, 'pending compact uses only a leading spinner in the tool icon slot');
    await js(`window.__pendingCompactCard = document.querySelector('[data-compact-run-id]');
      window.__pendingCompactRect = window.__pendingCompactCard.getBoundingClientRect().toJSON(); void 0;`);
    await screenshot('context-compaction-pending');
    await js(`window.fetch = window.__compactFetch; window.__releaseCompact(); delete window.__releaseCompact; delete window.__compactFetch;`);
    await waitFor(() => js(`!Array.from(document.querySelectorAll('.pudding-transcript-turn span')).some(el => el.textContent === '正在压缩上下文')`), 'manual compact request settles');
    check('manual compact pending row is 24px, with the spinner before its title');
    const failed = await js(`(() => {
      const card = document.querySelector('[data-compact-run-id]');
      const rect = card?.getBoundingClientRect();
      return { same: card === window.__pendingCompactCard, height: rect?.height, dy: rect ? rect.y - window.__pendingCompactRect.y : null,
        text: card?.innerText, expanded: Boolean(card?.querySelector('details[open]')), errorsOutside: Array.from(document.querySelectorAll('[role="alert"]')).some(el => !card?.contains(el)) };
    })()`);
    assert.equal(failed.same, true, 'failed compact keeps the original card DOM node');
    assert.equal(failed.height, 24, 'error remains a collapsed tool-height row');
    assert.ok(Math.abs(failed.dy) <= 1, `failed card moved ${failed.dy}px`);
    assert.equal(failed.text.trim(), '压缩失败', 'collapsed failure shows only its title');
    assert.equal(failed.expanded, false, 'error details do not expand automatically');
    assert.equal(failed.errorsOutside, false, 'no separate composer error banner');
    await screenshot('context-compaction-failed');
    await click('[data-compact-run-id] summary');
    await waitFor(() => js(`Boolean(document.querySelector('[data-compact-run-id] [role="alert"]'))`), 'expand compact failure details');
    assert.ok(await js(`document.querySelector('[data-compact-run-id] [role="alert"]').textContent.includes('可压缩的历史还不够')`), 'expanded card shows the detailed cause');
    check('failed compact retains its card, height and position; details expand inside the card');

    await api('/settings', 'PUT', { compact_auto_threshold_percent: '0' });
    const manual = await api('/sessions', 'POST', { title: 'Manual compaction results', provider: 'compact-smoke', model: 'compact-model' });
    for (let index = 0; index < 3; index++) {
      const turn = await api(`/sessions/${manual.id}/submit`, 'POST', { clientMessageID: `manual-source-${index}`, text: index === 0 ? `MANUAL_COMPACT_SOURCE ${'facts '.repeat(500)}` : `Recent question ${index}` });
      await waitFor(async () => (await api(`/sessions/${manual.id}/turns/${turn.turnID}`)).status === 'completed', 'manual history seeded');
    }
    await js(`(async () => {
      const { router } = await import('/src/main.tsx');
      await router.options.context.queryClient.invalidateQueries({ queryKey: ['sessions'] });
      await router.navigate({ to: '/', search: { session: ${JSON.stringify(manual.id)} } });
    })()`);
    await waitFor(() => js(`new URL(location.href).searchParams.get('session') === ${JSON.stringify(manual.id)} && Boolean(document.querySelector('textarea'))`), 'manual session selected');
    const compactFromUI = async () => {
      await input('textarea', '/compact');
      await pressEnter();
    };
    await api('/settings', 'PUT', { compact_auto_threshold_percent: '5' });
    manualSummary = { text: 'facts '.repeat(350), hold: true };
    await js(`(() => {
      const original = window.fetch;
      window.__compactReads = [];
      window.__restoreCompactFetch = () => { window.fetch = original; };
      window.fetch = async (...args) => {
        if (String(args[0]).includes('/sessions/${manual.id}/turns/')) {
          await new Promise(resolve => window.__compactReads.push(resolve));
        }
        return original(...args);
      };
      window.__compactFrames = [];
      window.__compactPhase = 'before';
      window.__sampleCompact = () => {
        const viewport = document.querySelector('[data-transcript-viewport]');
        const rows = Array.from(viewport.querySelectorAll('.pudding-transcript-turn'));
        const rects = rows.map(row => ({id:row.dataset.transcriptTurnId, y:row.getBoundingClientRect().y, height:row.getBoundingClientRect().height, text:row.innerText}));
        const last = rects.at(-1), prior = rects.at(-2);
        return {phase:window.__compactPhase, bottom:viewport.scrollHeight-viewport.clientHeight-viewport.scrollTop,
          jump:Boolean(document.querySelector('.pudding-conversation-bottom-dock button')), rows:rects.length,
          gap:last && prior ? last.y-prior.y-prior.height : null, last};
      };
      const frame = () => { window.__compactFrames.push(window.__sampleCompact()); window.__compactFrame = requestAnimationFrame(frame); };
      window.__compactFrame = requestAnimationFrame(frame);
    })()`);
    await waitFor(() => js(`window.__sampleCompact().bottom <= 8 && !window.__sampleCompact().jump`), 'manual starts pinned to bottom');
    // A tiny upward wheel turns off follow mode but stays within the 8px
    // geometric bottom threshold, so no jump button is visible to the user.
    await scrollUp(1);
    const beforePosition = await js(`window.__sampleCompact()`);
    assert.ok(beforePosition.bottom > 0 && beforePosition.bottom <= 8 && !beforePosition.jump, 'near-bottom upward scroll remains visually pinned');
    await js(`window.__compactPhase = 'pending';`);
    await compactFromUI();
    await waitFor(() => Boolean(manualSummary.release), 'manual summary held for scroll inspection');
    await js(`new Promise(resolve => { let n=10; const next=()=> --n ? requestAnimationFrame(next) : resolve(); requestAnimationFrame(next); })`);
    const pendingPosition = await js(`window.__sampleCompact()`);
    console.info('[compact-scroll] pending', JSON.stringify(pendingPosition));
    // A completion event arrives before both its HTTP response and snapshot.
    await js(`window.__compactPhase = 'event-before-snapshot';`);
    manualSummary.release();
    await waitFor(() => js(`window.__compactReads.length > 0`), 'canonical snapshots held after completion');
    await js(`new Promise(resolve => { let n=10; const next=()=> --n ? requestAnimationFrame(next) : resolve(); requestAnimationFrame(next); })`);
    const eventPosition = await js(`window.__sampleCompact()`);
    console.info('[compact-scroll] terminal before snapshot', JSON.stringify(eventPosition));
    await js(`window.__compactPhase = 'canonical'; window.__restoreCompactFetch(); window.__compactReads.splice(0).forEach(resolve=>resolve());`);
    await waitFor(() => js(`Boolean(document.querySelector('[data-compact-message-id]')) && !document.querySelector('[data-compact-run-id]')`), 'useful above-target manual summary accepted');
    await js(`new Promise(resolve => { let n=10; const next=()=> --n ? requestAnimationFrame(next) : resolve(); requestAnimationFrame(next); })`);
    const finalPosition = await js(`cancelAnimationFrame(window.__compactFrame); window.__sampleCompact()`);
    console.info('[compact-scroll] canonical', JSON.stringify(finalPosition));
    assert.equal(pendingPosition.jump, false, 'manual Enter must retain following latest');
    assert.ok(pendingPosition.bottom <= 8, 'pending manual card remains at the bottom');
    assert.equal(eventPosition.rows, pendingPosition.rows, 'terminal event must not create an empty extra row');
    assert.ok(Math.abs(eventPosition.gap-pendingPosition.gap) <= 1, 'card gap remains stable before snapshot');
    assert.ok(finalPosition.bottom <= 8 && !finalPosition.jump, 'canonical replacement retains the bottom');
    assert.equal(finalPosition.last.height, pendingPosition.last.height, 'pending and canonical rows have exactly the same height');
    const frames = await js(`window.__compactFrames`);
    assert.ok(frames.filter(frame=>frame.phase==='event-before-snapshot').every(frame=>frame.rows===pendingPosition.rows && Math.abs(frame.gap-pendingPosition.gap)<=1), 'every completion frame keeps the same row count and gap');
    check('manual Enter restores follow after a near-bottom scroll; completion before the snapshot introduces no empty row, height or gap change');
    const accepted = await api(`/sessions/${manual.id}/messages`);
    const acceptedMessages = Array.isArray(accepted) ? accepted : accepted.messages;
    const compactMessage = acceptedMessages.find(m => m.role === 'summary');
    assert.ok(compactMessage.metadata.compact.before_input_estimate > compactMessage.metadata.compact.after_input_estimate, 'canonical message records actual reduction');
    assert.equal(compactMessage.metadata.compact.tail_message_ids.length, 4, 'recent two turns retained');
    assert.ok(await js(`document.querySelector('[data-compact-message-id]').innerText.includes('估算减少')`), 'success card shows estimated savings');
    await screenshot('manual-compaction-saved');
    check('manual compact accepts a useful summary above its length and auto targets, displaying canonical estimated savings');
    await api('/settings', 'PUT', { compact_auto_threshold_percent: '0' });
    for (const outcome of [
      { text: 'verbose '.repeat(900), hold: true, title: '未获得压缩收益', detail: '没有减少上下文', role: 'status' },
      { text: '', title: '压缩失败', detail: '空摘要', role: 'alert' },
      { text: 'partial summary', finish: 'length', title: '压缩失败', detail: '未完整生成', role: 'alert' },
    ]) {
      manualSummary = outcome;
      await compactFromUI();
      let readingPosition;
      if (outcome.hold) {
        await waitFor(() => Boolean(manualSummary.release), 'hold no-gain result while reading');
        await scrollUp(320);
        await waitFor(() => js(`window.__sampleCompact().bottom > 8 && window.__sampleCompact().jump`), 'reading history while compaction is held');
        readingPosition = await js(`window.__sampleCompact()`);
        manualSummary.release();
      }
      await waitFor(() => js(`document.querySelector('[data-compact-run-id]')?.innerText.trim() === ${JSON.stringify(outcome.title)}`), 'manual outcome remains in card');
      if (readingPosition) {
        const after = await js(`window.__sampleCompact()`);
        console.info('[compact-scroll] reading on completion', JSON.stringify({before:readingPosition, after}));
        assert.ok(Math.abs(after.bottom-readingPosition.bottom)<=1 && after.jump, 'completion must not force a reader back to the bottom');
        await click('.pudding-conversation-bottom-dock button');
        await waitFor(() => js(`window.__sampleCompact().bottom <= 8 && !window.__sampleCompact().jump`), 'return to the result card before expanding it');
      }
      assert.equal(await js(`document.querySelector('[data-compact-run-id]').getBoundingClientRect().height`), 24);
      await click('[data-compact-run-id] summary');
      assert.ok(await js(`document.querySelector('[data-compact-run-id] [role="${outcome.role}"]').textContent.includes(${JSON.stringify(outcome.detail)})`), 'card explains the specific outcome');
      const current = await api(`/sessions/${manual.id}/messages`);
      assert.deepEqual(current, accepted, 'no-gain, empty and truncated results leave canonical history unchanged');
    }
    check('no-gain, empty and truncated manual summaries show distinct outcomes and preserve original history');
    await api('/providers/compact-smoke', 'PATCH', { models: [{ id: 'compact-model', contextWindow: 32000, capabilities: { tools: true }, limits: { maxOutputTokens: 30000 } }] });
    manualSummary = { text: 'Previous facts recorded. Continue with the latest question.', hold: true };
    await compactFromUI();
    await waitFor(() => Boolean(manualSummary.release), 'hold useful result before clicking jump latest');
    await scrollUp(160);
    await click('.pudding-conversation-bottom-dock button');
    await waitFor(() => js(`window.__sampleCompact().bottom <= 8 && !window.__sampleCompact().jump`), 'jump latest during compaction');
    manualSummary.release();
    await waitFor(() => js(`document.body.innerText.includes('已压缩，但仍超过上下文上限') && !document.querySelector('[data-compact-run-id]')`), 'useful over-capacity compact retained');
    assert.ok(await js(`window.__sampleCompact().bottom <= 8 && !window.__sampleCompact().jump`), 'result after jump latest stays pinned');
    await screenshot('manual-compaction-over-budget');
    reload();
    await waitFor(async () => {
      try { return await js(`document.body.innerText.includes('已压缩，但仍超过上下文上限') && document.body.innerText.includes('估算减少')`); }
      catch { return false; }
    }, 'canonical estimates restored after reload');
    check('useful manual compaction survives excessive fixed overhead; savings and capacity notice survive reload');
    assert.deepEqual(failures, []);
  } finally {
    heldResponse?.destroy();
    provider.closeAllConnections();
    await new Promise(resolve => provider.close(resolve));
  }
};
