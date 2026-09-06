import assert from "node:assert/strict";
import { test } from "node:test";
import type { LibraryEntry, LibraryRecentEntry } from "../contracts/api.ts";
import { libraryDescription, libraryMatches, libraryReferenceKey } from "../src/components/workspace/libraryPresentation.ts";

const file: LibraryRecentEntry = { id: "bookmark", kind: "file", sourceSessionID: "source", sourceSessionAvailable: true, sourceSessionTitle: "Payment", sourceProjectName: "Project", available: true, rootPath: "/work/project", path: "docs/readme.md", title: "readme.md", openedAt: "2026-09-05T10:00:00Z" };
const recent: LibraryRecentEntry = { ...file, id: "recent" };
const t = (key: string) => ({ "workspace.kind.file": "File", "workspace.kind.table": "Table", "workspace.savedVersion": "Saved version {revision}" }[key] || key);

test("search identifies the same file across visits, but keeps different roots", () => {
  assert.equal(libraryReferenceKey(file), libraryReferenceKey(recent));
  assert.notEqual(libraryReferenceKey(file), libraryReferenceKey({ ...recent, rootPath: "/other/project" }));
});

test("web identity keeps different URLs, and saved canvases stay separate from working copies", () => {
  const web: LibraryEntry = { ...file, createdAt: file.openedAt, updatedAt: file.openedAt, kind: "web", url: "https://example.com/a" };
  assert.equal(libraryReferenceKey(web), libraryReferenceKey({ ...web, id: "visit", sourceSessionID: "other" }));
  assert.notEqual(libraryReferenceKey(web), libraryReferenceKey({ ...web, url: "https://example.com/b" }));
  const saved: LibraryEntry = { ...file, createdAt: file.openedAt, updatedAt: file.openedAt, kind: "canvas", id: "one", savedItemID: "one" };
  const working: LibraryRecentEntry = { id: "one", kind: "canvas", itemID: "one", sourceSessionID: "source", sourceSessionAvailable: true, available: true, openedAt: file.openedAt };
  assert.notEqual(libraryReferenceKey(saved), libraryReferenceKey(working));
});

test("compact file rows retain folder context while full paths are reserved for details", () => {
  const display = libraryDescription(file, t);
  assert.equal(display.description, "Project / docs · Payment");
  assert.equal(display.location, "/work/project/docs/readme.md");
  assert.equal(libraryMatches(file, "/WORK/PROJECT", t), true);
  assert.equal(libraryMatches(file, "payment", t), true);
});

test("web rows show the host and saved canvases visibly identify their version", () => {
  const display = libraryDescription({ ...file, kind: "web", url: "https://example.com/docs?query=1" }, t);
  assert.equal(display.description, "example.com");
  assert.equal(display.location, "https://example.com/docs?query=1");
  assert.match(libraryDescription({ ...file, createdAt: file.openedAt, updatedAt: file.openedAt, kind: "canvas", canvasKind: "table", savedItemID: "saved", revision: 3 }, t).description, /Saved version 3/);
});
