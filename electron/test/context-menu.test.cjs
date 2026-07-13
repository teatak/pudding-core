const assert = require("node:assert/strict");
const test = require("node:test");

const { buildEditContextMenuTemplate } = require("../context-menu.cjs");

test("selected read-only text offers copy and select all", () => {
  const calls = [];
  const contents = fakeContents(calls);
  const template = buildEditContextMenuTemplate(
    contents,
    {
      editFlags: { canCopy: true, canSelectAll: true },
      isEditable: false,
      selectionText: "selected",
    },
    (key) => `text:${key}`,
    { platform: "linux" },
  );

  assert.deepEqual(
    template.map(({ label, enabled }) => ({ label, enabled })),
    [
      { label: "text:copy", enabled: true },
      { label: "text:selectAll", enabled: true },
    ],
  );
  template[0].click();
  template[1].click();
  assert.deepEqual(calls, ["copy", "selectAll"]);
});

test("editable text offers the complete edit menu with native enablement", () => {
  const calls = [];
  const contents = fakeContents(calls);
  const template = buildEditContextMenuTemplate(
    contents,
    {
      editFlags: {
        canCopy: true,
        canCut: true,
        canDelete: true,
        canPaste: false,
        canRedo: false,
        canSelectAll: true,
        canUndo: true,
      },
      isEditable: true,
    },
    undefined,
    { platform: "linux" },
  );

  assert.deepEqual(
    template.map(({ label, type, enabled }) => ({ label, type, enabled })),
    [
      { label: "undo", type: undefined, enabled: true },
      { label: "redo", type: undefined, enabled: false },
      { label: undefined, type: "separator", enabled: undefined },
      { label: "cut", type: undefined, enabled: true },
      { label: "copy", type: undefined, enabled: true },
      { label: "paste", type: undefined, enabled: false },
      { label: "pasteAndMatchStyle", type: undefined, enabled: false },
      { label: "delete", type: undefined, enabled: true },
      { label: undefined, type: "separator", enabled: undefined },
      { label: "selectAll", type: undefined, enabled: true },
    ],
  );
  template[3].click();
  assert.deepEqual(calls, ["cut"]);
});

test("blank read-only content does not open an empty menu", () => {
  assert.deepEqual(
    buildEditContextMenuTemplate(fakeContents([]), { editFlags: { canSelectAll: true }, isEditable: false }, undefined, {
      platform: "darwin",
    }),
    [],
  );
});

test("macOS selected text offers search, sharing, and speech", () => {
  const calls = [];
  const opened = [];
  const template = buildEditContextMenuTemplate(
    fakeContents(calls),
    {
      editFlags: { canCopy: true },
      isEditable: false,
      selectionText: "selected text",
    },
    (key, values) => (values?.selection ? `${key}:${values.selection}` : key),
    { platform: "darwin", openExternal: (url) => opened.push(url) },
  );

  assert.deepEqual(template.map(({ label, role, type }) => ({ label, role, type })), [
    { label: "copy", role: undefined, type: undefined },
    { label: undefined, role: undefined, type: "separator" },
    { label: "searchWithGoogle:selected text", role: undefined, type: undefined },
    { label: "share", role: "shareMenu", type: undefined },
    { label: "speech", role: undefined, type: undefined },
  ]);
  assert.deepEqual(template[3].sharingItem, { texts: ["selected text"] });
  assert.deepEqual(template[4].submenu.map(({ role }) => role), ["startSpeaking", "stopSpeaking"]);
  template[2].click();
  assert.deepEqual(opened, ["https://www.google.com/search?q=selected%20text"]);
});

test("macOS editable text offers spelling suggestions and native text submenus", () => {
  const calls = [];
  const contents = fakeContents(calls);
  contents.replaceMisspelling = (word) => calls.push(`replace:${word}`);
  contents.session = { addWordToSpellCheckerDictionary: (word) => calls.push(`dictionary:${word}`) };
  const template = buildEditContextMenuTemplate(
    contents,
    {
      dictionarySuggestions: ["pudding"],
      editFlags: {},
      isEditable: true,
      misspelledWord: "puding",
    },
    undefined,
    { platform: "darwin" },
  );

  assert.equal(template[0].label, "pudding");
  assert.equal(template[1].label, "addToDictionary");
  assert.deepEqual(template.slice(-3).map(({ label }) => label), ["spellingAndGrammar", "substitutions", "speech"]);
  assert.deepEqual(template.at(-3).submenu.map(({ role }) => role), ["toggleSpellChecker"]);
  assert.deepEqual(template.at(-2).submenu.filter(({ role }) => role).map(({ role }) => role), [
    "showSubstitutions",
    "toggleSmartQuotes",
    "toggleSmartDashes",
    "toggleTextReplacement",
  ]);
  template[0].click();
  template[1].click();
  assert.deepEqual(calls, ["replace:pudding", "dictionary:puding"]);
});

function fakeContents(calls) {
  return Object.fromEntries(
    ["undo", "redo", "cut", "copy", "paste", "pasteAndMatchStyle", "delete", "selectAll"].map((command) => [
      command,
      () => calls.push(command),
    ]),
  );
}
