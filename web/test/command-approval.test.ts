import assert from "node:assert/strict";
import test from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer();
const { commandSessionGrantFromPayload } = await server.ssrLoadModule("/src/components/ComposerApprovalBar.tsx");
const payload = {
  toolName: "builtin_command_run", execution: "host",
  sessionGrant: { kind: "chrome_headless_screenshot", execution: "host", executable: "/Applications/Chrome", cwd: "/project", inputPath: "/project/page.html", outputDirectory: "/project", profilePath: "/project/.preview" },
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

test("sandbox reuse requires an explicitly matching execution boundary", () => {
  const sandbox = { toolName: "builtin_command_run", execution: "sandbox", sessionGrant: { kind: "sandbox_command", execution: "sandbox", executable: "/usr/bin/python3", cwd: "/project" } };
  assert.deepEqual(commandSessionGrantFromPayload(sandbox), sandbox.sessionGrant);
  for (const invalid of [{ ...sandbox, execution: "host" }, { ...sandbox, sessionGrant: { ...sandbox.sessionGrant, execution: "host" } }, { ...sandbox, sessionGrant: { ...sandbox.sessionGrant, cwd: "" } }]) {
    assert.equal(commandSessionGrantFromPayload(invalid), null);
  }
});

test("approval reasons only render known server codes, once each", async () => {
  const { commandApprovalReasonLabels } = await server.ssrLoadModule("/src/lib/commandApprovalReasons.ts");
  assert.deepEqual(commandApprovalReasonLabels({ toolName: "builtin_command_run", approvalReasons: ["host_execution", "custom_environment", "host_execution", "constructor", "unknown", null] }, (key) => key), ["commandApproval.reason.host", "commandApproval.reason.environment"]);
  assert.deepEqual(commandApprovalReasonLabels({ approvalReasons: ["host_execution"] }, (key) => key), []);
});
