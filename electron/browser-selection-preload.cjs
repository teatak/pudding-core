const { ipcRenderer } = require("electron");

const selectionChangedChannel = "pudding:browser:selection-changed";
const selectionMaxCharacters = 16 * 1024;
const editableSelector =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"]';

let lastSelectionText = "";
let interactionTimer = 0;

function selectionElement(node) {
  if (!node) return null;
  return node.nodeType === Node.ELEMENT_NODE ? node : node.parentElement;
}

function readSelectionText() {
  const selection = typeof window.getSelection === "function" ? window.getSelection() : null;
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
    return "";
  }
  const anchorElement = selectionElement(selection.anchorNode);
  const focusElement = selectionElement(selection.focusNode);
  if (anchorElement?.closest?.(editableSelector) || focusElement?.closest?.(editableSelector)) {
    return "";
  }
  return String(selection.toString() || "").trim().slice(0, selectionMaxCharacters);
}

function noteLocalInteraction() {
  window.clearTimeout(interactionTimer);
  interactionTimer = window.setTimeout(() => {
    interactionTimer = 0;
    publishSelection(true);
  }, 0);
}

function publishSelection(allowEmpty = false) {
  const selectionText = readSelectionText();
  if (!selectionText && !allowEmpty) {
    return;
  }
  if (selectionText === lastSelectionText) {
    return;
  }
  lastSelectionText = selectionText;
  ipcRenderer.send(selectionChangedChannel, { selectionText });
}

document.addEventListener("pointerdown", noteLocalInteraction, true);
document.addEventListener("keydown", noteLocalInteraction, true);
document.addEventListener("click", () => publishSelection(), false);
document.addEventListener("selectionchange", () => publishSelection(), true);
window.addEventListener("focus", () => publishSelection());
window.addEventListener("pagehide", () => {
  window.clearTimeout(interactionTimer);
  lastSelectionText = "";
  ipcRenderer.send(selectionChangedChannel, { selectionText: "" });
});
