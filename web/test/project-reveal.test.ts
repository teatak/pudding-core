import assert from "node:assert/strict";
import { test } from "node:test";

import { resolveProjectFileReveal } from "../src/components/project/projectReveal.ts";

const roots = [
  {
    id: "root-core",
    name: "pudding-core",
    path: "/Users/yanggang/workspace/github.com/teatak/pudding-core",
  },
  {
    id: "root-mobile",
    name: "pudding-mobile",
    path: "/Users/yanggang/workspace/github.com/teatak/pudding-mobile",
  },
];

test("resolveProjectFileReveal resolves relative path when given absolute path", () => {
  const result = resolveProjectFileReveal(roots, {
    absolutePath: "/Users/yanggang/workspace/github.com/teatak/pudding-core/web/src/components/Composer.tsx",
    serial: 1,
    sessionID: "s1",
  });
  assert.deepEqual(result, {
    path: "web/src/components/Composer.tsx",
    rootID: "root-core",
  });
});

test("resolveProjectFileReveal corrects relativePath when mistakenly passed an absolute path", () => {
  const result = resolveProjectFileReveal(roots, {
    absolutePath: "/Users/yanggang/workspace/github.com/teatak/pudding-core/web/src/components/Composer.tsx",
    relativePath: "/Users/yanggang/workspace/github.com/teatak/pudding-core/web/src/components/Composer.tsx",
    rootPath: "/Users/yanggang/workspace/github.com/teatak/pudding-core",
    serial: 1,
    sessionID: "s1",
  });
  assert.deepEqual(result, {
    path: "web/src/components/Composer.tsx",
    rootID: "root-core",
  });
});

test("resolveProjectFileReveal resolves standard relative path", () => {
  const result = resolveProjectFileReveal(roots, {
    relativePath: "web/src/components/Composer.tsx",
    rootPath: "/Users/yanggang/workspace/github.com/teatak/pudding-core",
    serial: 1,
    sessionID: "s1",
  });
  assert.deepEqual(result, {
    path: "web/src/components/Composer.tsx",
    rootID: "root-core",
  });
});

test("resolveProjectFileReveal resolves path in secondary root", () => {
  const result = resolveProjectFileReveal(roots, {
    absolutePath: "/Users/yanggang/workspace/github.com/teatak/pudding-mobile/App.tsx",
    serial: 1,
    sessionID: "s1",
  });
  assert.deepEqual(result, {
    path: "App.tsx",
    rootID: "root-mobile",
  });
});

test("resolveProjectFileReveal returns undefined for path outside roots", () => {
  const result = resolveProjectFileReveal(roots, {
    absolutePath: "/etc/passwd",
    serial: 1,
    sessionID: "s1",
  });
  assert.equal(result, undefined);
});
