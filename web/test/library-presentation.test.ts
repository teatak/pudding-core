import assert from "node:assert/strict";
import { test } from "node:test";
import type { LibraryEntry } from "../contracts/api.ts";
import {
  libraryDescription,
  libraryMatches,
} from "../src/components/workspace/libraryPresentation.ts";

const web: LibraryEntry = {
  id: "bookmark",
  kind: "web",
  url: "https://example.com/docs?query=1",
  title: "Guide",
  favoriteID: "favorite",
  sourceSessionID: "source",
  sourceSessionTitle: "Old conversation",
  sourceProjectName: "Old project",
  sourceSessionAvailable: true,
  available: true,
  createdAt: "2026-09-05T10:00:00Z",
  updatedAt: "2026-09-05T10:00:00Z",
};
const t = (key: string) => key;
test("global bookmarks search title and URL without implying conversation ownership", () => {
  assert.equal(libraryMatches(web, "GUIDE", t), true);
  assert.equal(libraryMatches(web, "example.com/docs", t), true);
  assert.equal(libraryMatches(web, "Old conversation", t), false);
  assert.equal(libraryMatches(web, "Old project", t), false);
  const display = libraryDescription(web, t);
  assert.equal(display.description, "example.com");
  assert.equal(display.source, "workspace.library.global");
  assert.equal(display.location, web.url);
});

test("saved canvas remains searchable by source after unfavorite and source deletion", () => {
  const saved: LibraryEntry = {
    ...web,
    kind: "canvas",
    url: undefined,
    favoriteID: undefined,
    title: "Reusable table",
    savedItemID: "saved",
    canvasKind: "table",
    revision: 3,
    sourceSessionAvailable: false,
  };
  assert.equal(libraryMatches(saved, "Old conversation", t), true);
  assert.equal(libraryMatches(saved, "Reusable table", t), true);
  const display = libraryDescription(saved, (key) =>
    key === "workspace.savedVersion" ? "Saved version {revision}" : key,
  );
  assert.match(display.description, /Saved version 3/);
  assert.match(display.description, /Old conversation/);
});
