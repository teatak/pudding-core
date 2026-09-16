import assert from "node:assert/strict";
import { test } from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer();
const { useTranscriptViewportStore } = await server.ssrLoadModule("/src/state/transcriptViewportStore.ts");

test("transcript viewport store retains independent atLatest state across sessions", () => {
  const store = useTranscriptViewportStore.getState();

  // Session A 离开底部 (未贴底)
  store.save("primary:session-a", {
    atLatest: false,
    measurements: [],
    scrollOffset: 450,
  });

  // Session B 处于最新内容 (贴底)
  store.save("primary:session-b", {
    atLatest: true,
    measurements: [],
    scrollOffset: 1200,
  });

  const latestState = useTranscriptViewportStore.getState().viewports;

  // 验证两会话状态独立，未贴底的会话保持 false，贴底会话保持 true
  assert.equal(latestState["primary:session-a"]?.atLatest, false);
  assert.equal(latestState["primary:session-a"]?.scrollOffset, 450);
  assert.equal(latestState["primary:session-b"]?.atLatest, true);
  assert.equal(latestState["primary:session-b"]?.scrollOffset, 1200);

  // 全新未访问会话默认回退为 true (即贴底，不展示跳到最新按钮)
  assert.equal(latestState["primary:session-c"]?.atLatest ?? true, true);
});
