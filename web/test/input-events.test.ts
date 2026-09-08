import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

// Use the renderer's module resolver for contracts and store imports; no server
// is started and no user data or live session is accessed.
const server = await createServer({
  configFile: false,
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { sessionEvent } = await server.ssrLoadModule("/contracts/events.ts");
const { useOverlayStore } = await server.ssrLoadModule("/src/state/overlayStore.ts");

for (const kind of ["input.queued", "input.updated", "input.steered"]) {
  test(`${kind} accepts omitted text from Go's attachment-only input event`, () => {
    const payload = {
      kind, seq: 1, sessionID: "attachment-input", clientMessageID: "guide",
      ...(kind === "input.steered" ? { turnID: "turn", userMessageID: "user-guide" } : { status: "queued" }),
    };
    assert.equal(sessionEvent.parse(payload).text, "");
    assert.equal(sessionEvent.parse({ ...payload, text: "" }).text, "");
    assert.equal(sessionEvent.parse({ ...payload, text: "look here" }).text, "look here");
    assert.equal(sessionEvent.safeParse({ ...payload, text: 123 }).success, false);
    assert.equal(sessionEvent.safeParse({ ...payload, text: null }).success, false);
  });
}

test("attachment-only steer is applied, retains attachment parts and reconciles to canonical input", () => {
  const sessionID = "attachment-steer";
  const store = useOverlayStore.getState();
  const parts = [{ type: "attachment", id: "image", name: "image.png", mime: "image/png", size: 100,
    attachmentKey: "image", url: "" }];
  store.addPendingUser({ sessionID, turnID: "turn", clientMessageID: "guide", createdAt: "2026-09-08T12:00:00Z",
    status: "steering", text: "", parts });
  const message = { id: "user-guide", clientMessageID: "guide" };
  store.reconcileMessages(sessionID, [message]);
  assert.equal(useOverlayStore.getState().pendingUsers[sessionID][0].status, "steering");
  store.applyEvent(sessionEvent.parse({ kind: "input.steered", seq: 2, sessionID, turnID: "turn",
    userMessageID: "user-guide", clientMessageID: "guide" }));
  const pending = useOverlayStore.getState().pendingUsers[sessionID][0];
  assert.equal(pending.status, "steered");
  assert.deepEqual(pending.parts, parts);
  assert.equal(useOverlayStore.getState().lastEventSeqs[sessionID], 2);
  store.reconcileMessages(sessionID, [message]);
  assert.deepEqual(useOverlayStore.getState().pendingUsers[sessionID], []);
  store.clearSession(sessionID);
});

for (const accept of ["event", "response"]) {
  test(`a queued input accepted as a new turn leaves the dock (${accept})`, () => {
    const sessionID = `queue-start-${accept}`;
    const store = useOverlayStore.getState();
    const parts = [{ type: "text", text: "next" }];
    store.addPendingUser({ sessionID, clientMessageID: "next", status: "queued", parts, text: "next", createdAt: "2026-09-08T12:00:00Z" });
    if (accept === "event") store.applyEvent(sessionEvent.parse({ kind: "turn.started", sessionID, turnID: "next-turn", seq: 1, clientMessageID: "next", userMessageID: "next-message" }));
    else store.acceptSubmittingTurn(sessionID, "next", "next-turn");
    const input = useOverlayStore.getState().pendingUsers[sessionID][0];
    assert.equal(input.status, "submitting");
    assert.equal(input.turnID, "next-turn");
    assert.deepEqual(input.parts, parts);
    store.clearSession(sessionID);
  });
}
