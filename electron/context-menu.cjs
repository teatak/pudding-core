function buildEditContextMenuTemplate(contents, params = {}, text = (key) => key, options = {}) {
  const flags = params.editFlags || {};
  const platform = options.platform || process.platform;
  const selectionText = String(params.selectionText || "").trim();
  const item = (key, command, enabled) => ({
    label: text(key),
    enabled: Boolean(enabled),
    click: () => runEditCommand(contents, command),
  });
  const template = [];

  if (params.isEditable && params.misspelledWord) {
    for (const suggestion of params.dictionarySuggestions || []) {
      template.push({
        label: suggestion,
        click: () => contents.replaceMisspelling?.(suggestion),
      });
    }
    template.push({
      label: text("addToDictionary"),
      click: () => contents.session?.addWordToSpellCheckerDictionary?.(params.misspelledWord),
    });
    template.push({ type: "separator" });
  }

  if (!params.isEditable) {
    if (selectionText.length > 0) {
      template.push(item("copy", "copy", flags.canCopy !== false));
      if (flags.canSelectAll) {
        template.push(item("selectAll", "selectAll", true));
      }
    }
  } else {
    template.push(
      item("undo", "undo", flags.canUndo),
      item("redo", "redo", flags.canRedo),
      { type: "separator" },
      item("cut", "cut", flags.canCut),
      item("copy", "copy", flags.canCopy),
      item("paste", "paste", flags.canPaste),
      item("pasteAndMatchStyle", "pasteAndMatchStyle", flags.canPaste),
      item("delete", "delete", flags.canDelete),
      { type: "separator" },
      item("selectAll", "selectAll", flags.canSelectAll),
    );
  }

  if (platform === "darwin" && (selectionText || params.isEditable)) {
    appendSeparator(template);
    if (selectionText) {
      template.push({
        label: text("searchWithGoogle", { selection: summarizeSelection(selectionText) }),
        click: () => options.openExternal?.(`https://www.google.com/search?q=${encodeURIComponent(selectionText)}`),
      });
      template.push({
        label: text("share"),
        role: "shareMenu",
        sharingItem: { texts: [selectionText] },
      });
    }
    if (params.isEditable) {
      template.push(
        {
          label: text("spellingAndGrammar"),
          submenu: [{ label: text("checkSpellingWhileTyping"), role: "toggleSpellChecker" }],
        },
        {
          label: text("substitutions"),
          submenu: [
            { label: text("showSubstitutions"), role: "showSubstitutions" },
            { type: "separator" },
            { label: text("smartQuotes"), role: "toggleSmartQuotes" },
            { label: text("smartDashes"), role: "toggleSmartDashes" },
            { label: text("textReplacement"), role: "toggleTextReplacement" },
          ],
        },
      );
    }
    template.push({
      label: text("speech"),
      submenu: [
        { label: text("startSpeaking"), role: "startSpeaking" },
        { label: text("stopSpeaking"), role: "stopSpeaking" },
      ],
    });
  }

  return trimSeparators(template);
}

function appendSeparator(template) {
  if (template.length > 0 && template.at(-1)?.type !== "separator") {
    template.push({ type: "separator" });
  }
}

function trimSeparators(template) {
  while (template[0]?.type === "separator") {
    template.shift();
  }
  while (template.at(-1)?.type === "separator") {
    template.pop();
  }
  return template;
}

function summarizeSelection(value) {
  const normalized = value.replace(/\s+/g, " ");
  return normalized.length > 48 ? `${normalized.slice(0, 47)}…` : normalized;
}

function runEditCommand(contents, command) {
  if (!contents || contents.isDestroyed?.()) {
    return;
  }
  const action = contents[command];
  if (typeof action === "function") {
    action.call(contents);
  }
}

module.exports = { buildEditContextMenuTemplate };
