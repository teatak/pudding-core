const assert = require("node:assert/strict");
const test = require("node:test");
const { EventEmitter } = require("node:events");
const { PassThrough } = require("node:stream");
const { ComputerUsePreview } = require("../computer-use-preview.cjs");

function fixture() {
  const children = [], revealed = [];
  const preview = new ComputerUsePreview({binaryPath: '/signed/helper',
    host: {async request(command, target) { revealed.push({command, target}); }},
    spawnProcess(binary, args) {
      assert.equal(binary, '/signed/helper'); assert.deepEqual(args, ['preview']);
      const child = new EventEmitter();
      child.stdout = new PassThrough(); child.stderr = new PassThrough(); child.stdin = new PassThrough();
      child.input = ''; child.stdin.on('data', data => child.input += data);
      child.stopped = false;
      child.stdin.on('finish', () => { child.stopped = true; queueMicrotask(() => child.emit('exit', 0)); });
      child.kill = () => child.emit('exit', null);
      children.push(child); return child;
    },
  });
  const owner = new EventEmitter(); owner.id = 1; owner.isDestroyed = () => false;
  owner.messages = []; owner.send = (_channel, packet) => owner.messages.push(packet);
  const target = {sessionID:'s1',turnID:'t1',appID:'com.example.App',windowID:42};
  const subscribe = (visible = true, turnID = 't1') => preview.subscribe(owner, {sessionID:'s1',turnID,visible});
  const ack = () => preview.acknowledge(owner, owner.messages.at(-1));
  const frame = (child, text, windowID = 42, pid = 123) => child.stdout.write(JSON.stringify({type:'frame',windowID,pid,width:520,height:480,name:'Example',title:'Window',png:Buffer.from(text).toString('base64')})+'\n');
  const entry = (appID = target.appID) => preview.entries.get(JSON.stringify([target.sessionID, appID]));
  return {preview, owner, children, target, subscribe, ack, frame, revealed, entry};
}

test('preview only captures the subscribed turn and an engine-authorized target', t => {
  const f = fixture(); t.after(() => f.preview.stop());
  f.subscribe(); assert.equal(f.children.length, 0);
  f.preview.noteActivity({...f.target,sessionID:'other'}); assert.equal(f.children.length, 0);
  f.preview.noteActivity({...f.target,turnID:'old'}); assert.equal(f.children.length, 0);
  f.preview.noteActivity(f.target); assert.equal(f.children.length, 1);
  assert.deepEqual(JSON.parse(f.children[0].input), {bundleID:'com.example.App',windowID:42});
  assert.equal(f.owner.messages[0].status, 'loading');
  f.preview.noteActivity({...f.target,turnID:'old',windowID:99});
  assert.equal(f.entry().target.windowID, 42, 'late activity from an old turn cannot replace the current window');
  assert.equal(f.children[0].stdin.writableEnded, false);
});

test('slow renderers retain only the latest frame and require a matching acknowledgement', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  f.frame(f.children[0], 'one'); const first = f.owner.messages.at(-1);
  f.frame(f.children[0], 'two'); f.frame(f.children[0], 'three');
  assert.equal(f.owner.messages.length, 2);
  f.preview.acknowledge(f.owner, {...first,turnID:'wrong'}); assert.equal(f.owner.messages.length, 2);
  f.preview.acknowledge(f.owner, first);
  assert.equal(f.owner.messages.length, 3);
  assert.equal(f.owner.messages.at(-1).imageURL, 'data:image/png;base64,dGhyZWU=');
});

test('preview transports large PNG frames without the former JPEG size limit', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  const payload = Buffer.alloc(950_000, 255);
  assert.ok(payload.toString('base64').length > 1024 * 1024);
  f.frame(f.children[0], payload);
  assert.equal(f.owner.messages.at(-1).status, 'live');
  assert.equal(f.owner.messages.at(-1).imageURL, `data:image/png;base64,${payload.toString('base64')}`);
  assert.equal(f.children[0].stdin.writableEnded, false);
});

test('preview rejects oversized frames and the removed JPEG protocol', t => {
  for (const data of [
    'A'.repeat(2 * 1024 * 1024 + 1),
    JSON.stringify({type:'frame',windowID:42,pid:123,width:520,height:480,name:'Example',title:'Window',jpeg:'b2xk'}) + '\n',
  ]) {
    const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
    f.children[0].stdout.write(data);
    assert.equal(f.owner.messages.at(-1).status, 'unavailable');
    assert.equal(f.owner.messages.at(-1).imageURL, undefined);
    assert.equal(f.children[0].stdin.writableEnded, true);
  }
});

test('hide stops capture; resume binds the same PID; stale frames cannot replace a new window', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  const first = f.children[0]; f.frame(first, 'one'); f.ack();
  f.subscribe(false); assert.equal(first.stdin.writableEnded, true); assert.equal(f.entry().frame, null);
  f.subscribe(); assert.equal(f.children.length, 2); assert.equal(JSON.parse(f.children[1].input).pid, 123);
  f.preview.noteActivity({...f.target,windowID:43}); const current = f.children[2]; f.ack();
  f.frame(first, 'late'); assert.equal(f.owner.messages.at(-1).windowID, 43);
  f.frame(current, 'new', 43, 456);
  assert.equal(f.owner.messages.at(-1).status, 'loading', 'replacement still respects outstanding packet backpressure');
  f.ack(); assert.equal(f.owner.messages.at(-1).pid, 456);
});

test('reveal only raises the exact captured window of the visible owning session', async t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  assert.equal(await f.preview.reveal(f.owner, f.target), false);
  f.frame(f.children[0], 'one');
  assert.equal(await f.preview.reveal(f.owner, {...f.target,windowID:99}), false);
  assert.equal(await f.preview.reveal(f.owner, f.target), true);
  assert.deepEqual(f.revealed, [{command:'reveal_window',target:{bundleID:'com.example.App',windowID:42,pid:123}}]);
  f.subscribe(false); assert.equal(await f.preview.reveal(f.owner, f.target), false);
});

test('cancel, renderer reload and destruction release their capture without affecting other sessions', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target);
  f.preview.subscribe(f.owner, {sessionID:'s2',turnID:'t2',visible:true});
  f.preview.noteActivity({...f.target,sessionID:'s2',turnID:'t2'});
  f.subscribe(false, ''); assert.equal(f.children[0].stdin.writableEnded, true); assert.equal(f.children[1].stdin.writableEnded, false);
  f.owner.emit('did-start-navigation', {}, 'http://source', false, true);
  assert.equal(f.children[1].stdin.writableEnded, true); assert.equal(f.preview.entries.size, 0);
  f.subscribe(); f.preview.noteActivity(f.target); f.owner.emit('destroyed');
  assert.equal(f.children[2].stdin.writableEnded, true); assert.equal(f.preview.clients.size, 0);
});

test('capture failure clears stale content, does not retry, and rejects a mismatched window', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  f.frame(f.children[0], 'wrong', 99);
  assert.equal(f.owner.messages.at(-1).status, 'unavailable');
  assert.equal(f.owner.messages.at(-1).imageURL, undefined);
  f.preview.noteActivity(f.target); assert.equal(f.children.length, 1);
});

test('apps retain separate captures; only actual activity changes stacking order and expiry', t => {
  t.mock.timers.enable({apis:['Date','setTimeout'], now:1_000});
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe();
  f.preview.noteActivity(f.target); f.ack(); f.frame(f.children[0], 'A'); f.ack();
  const firstActivity = f.entry().activityVersion;
  assert.equal(f.entry().expiresAt, 31_000);
  t.mock.timers.tick(10_000);
  const second = {...f.target,appID:'com.example.Second',windowID:43};
  f.preview.noteActivity(second); f.ack(); f.frame(f.children[1], 'B', 43, 456); f.ack();
  assert.equal(f.children[0].stdin.writableEnded, false, 'another app does not replace the first capture');
  assert.equal(f.entry().expiresAt, 31_000, 'another app does not extend the deadline');
  assert.ok(f.entry(second.appID).activityVersion > firstActivity);
  f.frame(f.children[0], 'new A'); f.ack();
  assert.equal(f.entry().activityVersion, firstActivity, 'frames do not reorder cards');
  assert.equal(f.entry().expiresAt, 31_000, 'frames do not renew activity');
  t.mock.timers.tick(20_000); f.ack();
  assert.equal(f.entry().frame, null); assert.equal(f.entry().child, null);
  assert.ok(f.entry(second.appID).frame, 'only the idle app expires');
  assert.equal(f.owner.messages.at(-1).appID, f.target.appID);
  assert.equal(f.owner.messages.at(-1).status, 'unavailable');
  f.preview.noteActivity(f.target); f.ack();
  assert.equal(f.children.length, 3, 'explicit activity can restart an expired preview');
  assert.ok(f.entry().activityVersion > f.entry(second.appID).activityVersion);
  assert.equal(f.entry(second.appID).expiresAt, 41_000);
});

test('multi-app backpressure delivers latest pending frames and invalidation without starving apps', t => {
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target);
  const second = {...f.target,appID:'com.example.Second',windowID:43};
  f.preview.noteActivity(second); f.frame(f.children[1], 'B', 43, 456);
  f.frame(f.children[0], 'A'); f.children[0].emit('exit', 1);
  assert.equal(f.owner.messages.length, 1, 'one outstanding packet total');
  f.ack(); assert.equal(f.owner.messages.at(-1).appID, second.appID);
  f.ack(); assert.equal(f.owner.messages.at(-1).appID, f.target.appID);
  assert.equal(f.owner.messages.at(-1).status, 'unavailable');
  f.ack(); assert.equal(f.owner.messages.length, 3);
});

test('failed capture cannot reveal its stale target; new turn drops every old app and timer', async t => {
  t.mock.timers.enable({apis:['Date','setTimeout'], now:1_000});
  const f = fixture(); t.after(() => f.preview.stop()); f.subscribe(); f.preview.noteActivity(f.target); f.ack();
  f.frame(f.children[0], 'A'); f.ack(); f.children[0].emit('exit', 1);
  assert.equal(await f.preview.reveal(f.owner, f.target), false);
  const second = {...f.target,appID:'com.example.Second',windowID:43};
  f.preview.noteActivity(second);
  f.subscribe(true, 't2'); f.preview.noteActivity({...f.target,turnID:'t2'});
  assert.equal(f.preview.entries.size, 1);
  assert.equal(f.children[1].stdin.writableEnded, true);
  f.subscribe(false, ''); assert.equal(f.preview.entries.size, 0);
  const messages = f.owner.messages.length;
  t.mock.timers.tick(31_000);
  assert.equal(f.owner.messages.length, messages, 'removed targets have no surviving expiry timers');
});
