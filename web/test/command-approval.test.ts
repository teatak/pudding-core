import assert from "node:assert/strict";
import test from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer();
const { commandSessionGrantFromPayload } = await server.ssrLoadModule("/src/components/ComposerApprovalBar.tsx");
const payload = {
  toolName: "builtin_command_run", execution: "host",
  sessionGrant: { kind: "chrome_headless_screenshot", executable: "/Applications/Chrome", cwd: "/project", inputPath: "/project/page.html", outputDirectory: "/project", profilePath: "/project/.preview" },
};

test("only bounded host screenshot approvals offer session reuse", () => {
  assert.deepEqual(commandSessionGrantFromPayload(payload), payload.sessionGrant);
  for (const invalid of [null, {}, { ...payload, execution: "sandbox" }, { ...payload, toolName: "builtin_file_patch" }, { ...payload, sessionGrant: {} }, { ...payload, sessionGrant: { ...payload.sessionGrant, kind: "any_command" } }]) {
    assert.equal(commandSessionGrantFromPayload(invalid), null);
  }
  for (const field of ["executable", "cwd", "inputPath", "outputDirectory", "profilePath"]) {
    assert.equal(commandSessionGrantFromPayload({ ...payload, sessionGrant: { ...payload.sessionGrant, [field]: "" } }), null);
  }
});
