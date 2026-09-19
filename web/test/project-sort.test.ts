import assert from "node:assert/strict";
import test from "node:test";
import type { Project, Session } from "../src/api/client.ts";
import { groupProjectSessions, sortProjectGroups } from "../src/components/session-rail/model.ts";

const at = (day: number) => `2026-09-${String(day).padStart(2, "0")}T00:00:00Z`;
const projects: Project[] = [
  { id: "older", name: "Older", rootDirs: [], approvalMode: "auto", createdAt: at(1), updatedAt: at(1), lastActivityAt: at(9) },
  { id: "newer", name: "Newer", rootDirs: [], approvalMode: "auto", createdAt: at(2), updatedAt: at(2), lastActivityAt: at(8) },
];
const session = { id: "s", projectID: "older", createdAt: at(3), lastActivityAt: at(9) } as Session;
const order = (items: Project[], sessions: Session[]) =>
  sortProjectGroups(groupProjectSessions(items, sessions), "activity", []).map((group) => group.key);

test("project activity order survives removing or hiding the latest session", () => {
  assert.deepEqual(order(projects, [session]), ["older", "newer"]);
  // Archived, deleted, pinned and filtered sessions are absent from this list.
  assert.deepEqual(order(projects, []), ["older", "newer"]);
});

test("project activity is authoritative, independent of metadata and session overlays", () => {
  const renamed = projects.map((project) => project.id === "newer" ? { ...project, updatedAt: at(19) } : project);
  assert.deepEqual(order(renamed, [{ ...session, projectID: "newer", lastActivityAt: at(19) }]), ["older", "newer"]);
  assert.deepEqual(order(projects.map((project) => project.id === "newer" ? { ...project, lastActivityAt: at(10) } : project), []), ["newer", "older"]);
});

test("custom sorting still keeps saved projects first and appends new projects", () => {
  const groups = groupProjectSessions(projects, []);
  assert.deepEqual(sortProjectGroups(groups, "custom", ["newer"]).map((group) => group.key), ["newer", "older"]);
});
