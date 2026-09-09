import assert from "node:assert/strict";
import { test } from "node:test";

import { routeAfterSessionArchive } from "../src/lib/route.ts";

test("archiving the current project session opens a draft in that project", () => {
  assert.deepEqual(
    routeAfterSessionArchive(
      { session: "current", split: "secondary", view: "apps", project: "stale" },
      { id: "current", projectID: "project-a" },
    ),
    { draft: "1", project: "project-a" },
  );
});

test("archiving the current projectless session opens a global draft", () => {
  assert.deepEqual(
    routeAfterSessionArchive(
      { session: "current", project: "stale" },
      { id: "current" },
    ),
    { draft: "1" },
  );
});

test("archiving a split or unrelated session does not replace the primary session", () => {
  assert.deepEqual(
    routeAfterSessionArchive(
      { session: "primary", split: "secondary" },
      { id: "secondary", projectID: "project-a" },
    ),
    { session: "primary" },
  );
  assert.deepEqual(
    routeAfterSessionArchive(
      { session: "primary" },
      { id: "other", projectID: "project-a" },
    ),
    { session: "primary" },
  );
});
