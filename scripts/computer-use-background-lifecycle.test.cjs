const test = require('node:test');
const assert = require('node:assert/strict');
const {assess, calendarMonth, scenarios} = require('./computer-use-background-lifecycle.cjs');

function sample(scenario = 'normal') {
  const fault = /^(worker|helper)-/.test(scenario);
  return {scenario, before:100, after:['worker-1', 'helper-1'].includes(scenario) ? 100 : 101,
    finalMonth:100, phaseAfter:0,
    checkpoint:{phase:Number(scenario.split('-')[1]) || (scenario === 'cancel' ? 2 : 1), signal:fault ? 'SIGKILL' : undefined},
    outcome:fault ? {outcome:'unknown'} : scenario === 'cancel'
      ? {code:'computer_action_cancelled', outcome:'unknown'} : {completed:true, delivery:'background'},
    contender:{code:'computer_input_busy', outcome:'not_started', retryable:false},
    monitor:{pid:99, samples:50, foregroundPIDs:[99], activations:[], maxCursorDistance:0}, guardEvents:[]};
}

test('each native lifecycle case has explicit effect, outcome and recovery requirements', () => {
  for (const scenario of scenarios) assert.equal(assess(sample(scenario)).pass, true, scenario);
});
test('a response alone cannot certify lifecycle or no-interference success', () => {
  const mutations = [r => r.after++, r => r.finalMonth++, r => r.phaseAfter = 2,
    r => r.monitor.samples = 0, r => r.monitor.foregroundPIDs = [],
    r => r.monitor.foregroundPIDs.push(88), r => r.monitor.activations.push(88),
    r => r.monitor.maxCursorDistance = 1, r => r.guardEvents.push({kind:'event'})];
  for (const mutate of mutations) { const r = sample(); mutate(r); assert.equal(assess(r).pass, false); }
});
test('crash must actually be injected at the requested journal phase and remain uncertain', () => {
  for (const mutate of [r => r.checkpoint.signal = undefined, r => r.checkpoint.phase = 1,
    r => r.outcome = {completed:true, delivery:'background'}, r => r.outcome.outcome = 'not_started']) {
    const r = sample('worker-2'); mutate(r); assert.equal(assess(r).pass, false);
  }
});
test('concurrent input must be rejected without a second effect or implicit replay', () => {
  for (const contender of [{completed:true}, {code:'computer_input_busy', outcome:'unknown'},
    {code:'computer_input_busy', outcome:'not_started', retryable:true}]) {
    const r = sample('concurrent'); r.contender = contender; assert.equal(assess(r).pass, false);
  }
});
test('calendar effect reads only an unambiguous month heading', () => {
  const e = value => ({role:'AXStaticText', value});
  assert.equal(calendarMonth({elements:[e('2026年9月')]}), 2026*12+8);
  for (const elements of [[], [e('2026年9月'), e('2026年10月')], [e('an event')]]) {
    assert.throws(() => calendarMonth({elements}));
  }
});
