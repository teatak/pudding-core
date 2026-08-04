const { ipcRenderer } = require("electron");

const selectionChangedChannel = "pudding:browser:selection-changed";
const credentialFormChannel = "pudding:browser:credential-form";
const credentialCandidateChannel = "pudding:browser:credential-candidate";
const credentialFillChannel = "pudding:browser:credential-fill";
const credentialFillResultChannel = "pudding:browser:credential-fill-result";
const credentialFocusChannel = "pudding:browser:credential-focus";
const credentialSuggestionsChannel = "pudding:browser:credential-suggestions";
const credentialFillRequestChannel = "pudding:browser:credential-fill-request";
const credentialManageRequestChannel = "pudding:browser:credential-manage-request";
const selectionMaxCharacters = 16 * 1024;
const topLevelDocument = window.top === window;
const editableSelector =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"]';

let lastSelectionText = "";
let lastCredentialFormState = "";
let interactionTimer = 0;
let credentialDetectionTimer = 0;
let credentialSuggestionHost = null;
let credentialSuggestionRows = [];
let credentialPositionSheet = null;
let credentialSuggestionPayload = null;
let focusedCredentialUsername = null;
let selectedCredentialIndex = -1;
let credentialPositionFrame = 0;

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

function credentialOrigin() {
  try {
    const url = new URL(location.href);
    if (url.protocol === "https:") return url.origin;
    if (url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) return url.origin;
  } catch {
  }
  return "";
}

function autocompleteTokens(input) {
  return String(input?.autocomplete || input?.getAttribute?.("autocomplete") || "")
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
}

function usableInput(input) {
  return input instanceof HTMLInputElement && !input.disabled && !input.readOnly && input.getAttribute("aria-disabled") !== "true";
}

function currentPasswordInput(form) {
  const inputs = form
    ? Array.from(form.elements || []).filter((element) => element instanceof HTMLInputElement)
    : Array.from(document.querySelectorAll('input[type="password"]'));
  return inputs.find((input) =>
    usableInput(input) &&
    String(input.type || "").toLowerCase() === "password" &&
    !autocompleteTokens(input).includes("new-password"),
  ) || null;
}

function usernameInput(form, passwordInput) {
  const inputs = form
    ? Array.from(form.elements || []).filter((element) => element instanceof HTMLInputElement)
    : Array.from(document.querySelectorAll("input"));
  const candidates = inputs.filter((input) => {
    if (!usableInput(input) || input === passwordInput) return false;
    const type = String(input.type || "text").toLowerCase();
    return type === "text" || type === "email" || type === "tel" || type === "url" || type === "search";
  });
  return candidates.find((input) => autocompleteTokens(input).includes("username")) ||
    candidates.find((input) => String(input.type || "").toLowerCase() === "email") ||
    [...candidates].reverse().find((input) => Boolean(input.compareDocumentPosition(passwordInput) & Node.DOCUMENT_POSITION_FOLLOWING)) ||
    candidates[0] ||
    null;
}

function credentialFields(form) {
  const password = currentPasswordInput(form);
  if (!password) return null;
  return { password, username: usernameInput(form, password) };
}

function credentialFieldsForUsername(input) {
  if (!usableInput(input)) return null;
  const form = input.form || null;
  const passwords = form
    ? Array.from(form.elements || []).filter((element) =>
      element instanceof HTMLInputElement &&
      usableInput(element) &&
      String(element.type || "").toLowerCase() === "password" &&
      !autocompleteTokens(element).includes("new-password"),
    )
    : Array.from(document.querySelectorAll('input[type="password"]')).filter((element) =>
      usableInput(element) && !autocompleteTokens(element).includes("new-password"),
    );
  for (const password of passwords) {
    const username = usernameInput(form, password);
    if (username === input) return { username, password };
  }
  return null;
}

function detectCredentialForm() {
  credentialDetectionTimer = 0;
  const origin = credentialOrigin();
  const detected = Boolean(origin && currentPasswordInput(null));
  const state = `${origin}\u0000${detected}`;
  if (state === lastCredentialFormState) return;
  lastCredentialFormState = state;
  ipcRenderer.send(credentialFormChannel, { origin, detected });
}

function scheduleCredentialDetection() {
  window.clearTimeout(credentialDetectionTimer);
  credentialDetectionTimer = window.setTimeout(detectCredentialForm, 50);
}

function submitCredentialCandidate(event) {
  const origin = credentialOrigin();
  if (!origin || !(event.target instanceof HTMLFormElement)) return;
  const fields = credentialFields(event.target);
  const password = String(fields?.password?.value || "");
  if (!fields || !password) return;
  ipcRenderer.send(credentialCandidateChannel, {
    origin,
    username: String(fields.username?.value || "").trim().slice(0, 512),
    password,
  });
}

function setNativeInputValue(input, value) {
  let prototype = Object.getPrototypeOf(input);
  let setter = null;
  while (prototype && !setter) {
    setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set || null;
    prototype = Object.getPrototypeOf(prototype);
  }
  if (!setter) throw new Error("native input setter unavailable");
  setter.call(input, value);
  input.dispatchEvent(new InputEvent("input", { bubbles: true, composed: true, inputType: "insertReplacementText", data: value }));
  input.dispatchEvent(new Event("change", { bubbles: true, composed: true }));
}

function hideCredentialSuggestions() {
  window.cancelAnimationFrame(credentialPositionFrame);
  credentialPositionFrame = 0;
  credentialSuggestionHost?.remove();
  credentialSuggestionHost = null;
  credentialSuggestionRows = [];
  credentialPositionSheet = null;
  selectedCredentialIndex = -1;
}

function visibleCredentialSuggestions() {
  const credentials = Array.isArray(credentialSuggestionPayload?.credentials)
    ? credentialSuggestionPayload.credentials
    : [];
  const query = String(focusedCredentialUsername?.value || "").trim().toLowerCase();
  if (!query) return credentials;
  return credentials.filter((credential) => String(credential.username || "").toLowerCase().includes(query));
}

function keyIcon() {
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("viewBox", "0 0 24 24");
  icon.setAttribute("aria-hidden", "true");
  icon.innerHTML = '<circle cx="8" cy="15" r="4"></circle><path d="m11 12 8-8m-2 2 2 2m-5 1 2 2"></path>';
  return icon;
}

function positionCredentialSuggestions() {
  credentialPositionFrame = 0;
  const host = credentialSuggestionHost;
  const input = focusedCredentialUsername;
  if (!host || !input?.isConnected || document.activeElement !== input) {
    hideCredentialSuggestions();
    return;
  }
  const rect = input.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0 || rect.bottom <= 0 || rect.top >= window.innerHeight) {
    hideCredentialSuggestions();
    return;
  }
  const width = Math.min(Math.max(rect.width, 280), Math.max(280, window.innerWidth - 16));
  const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
  let top = rect.bottom + 6;
  credentialPositionSheet?.replaceSync(`:host { width: ${width}px; left: ${left}px; top: ${top}px; }`);
  const height = host.getBoundingClientRect().height;
  if (rect.bottom + 6 + height > window.innerHeight && rect.top - height - 6 >= 8) {
    top = rect.top - height - 6;
    credentialPositionSheet?.replaceSync(`:host { width: ${width}px; left: ${left}px; top: ${top}px; }`);
  }
}

function scheduleCredentialPosition() {
  window.cancelAnimationFrame(credentialPositionFrame);
  credentialPositionFrame = window.requestAnimationFrame(positionCredentialSuggestions);
}

function selectCredentialSuggestion(index) {
  selectedCredentialIndex = index;
  credentialSuggestionRows.forEach((row, rowIndex) => row.toggleAttribute("data-selected", rowIndex === index));
}

function requestCredentialFill(credential) {
  const origin = credentialOrigin();
  if (!origin || !credential?.id || !focusedCredentialUsername?.isConnected) return;
  ipcRenderer.send(credentialFillRequestChannel, { origin, credentialID: credential.id });
  hideCredentialSuggestions();
}

function renderCredentialSuggestions() {
  hideCredentialSuggestions();
  const input = focusedCredentialUsername;
  if (!topLevelDocument || !input?.isConnected || document.activeElement !== input) return;
  if (String(credentialSuggestionPayload?.origin || "") !== credentialOrigin()) return;
  const credentials = visibleCredentialSuggestions();
  if (credentials.length === 0 || !document.documentElement) return;

  const host = document.createElement("div");
  host.setAttribute("data-pudding-credential-suggestions", "");
  host.toggleAttribute("data-dark", Boolean(credentialSuggestionPayload.dark));
  const shadow = host.attachShadow({ mode: "closed" });
  const style = new CSSStyleSheet();
  style.replaceSync(`
    :host { all: initial; position: fixed; z-index: 2147483647; display: block; box-sizing: border-box; color-scheme: light dark; }
    * { box-sizing: border-box; }
    .panel { overflow: hidden; padding: 6px; border: 1px solid rgba(0,0,0,.12); border-radius: 12px; background: #fff; color: #202124; box-shadow: 0 12px 30px rgba(0,0,0,.18); font: 14px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    .title { padding: 7px 8px 6px; color: #6b7280; font-size: 12px; font-weight: 600; }
    button { appearance: none; width: 100%; border: 0; background: transparent; color: inherit; font: inherit; }
    .row { display: flex; min-height: 42px; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 8px; cursor: default; text-align: left; }
    .row:hover, .row[data-selected] { background: rgba(15,23,42,.06); }
    .row svg { width: 18px; height: 18px; flex: none; fill: none; stroke: currentColor; stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.8; color: #5f6368; }
    .username { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
    .separator { height: 1px; margin: 5px 4px; background: rgba(0,0,0,.10); }
    .manage { color: #3c4043; font-weight: 500; }
    :host([data-dark]) .panel { border-color: rgba(255,255,255,.14); background: #252525; color: #f1f3f4; box-shadow: 0 12px 30px rgba(0,0,0,.45); }
    :host([data-dark]) .title { color: #aeb4bc; }
    :host([data-dark]) .row:hover, :host([data-dark]) .row[data-selected] { background: rgba(255,255,255,.09); }
    :host([data-dark]) .row svg, :host([data-dark]) .manage { color: #d7d9dc; }
    :host([data-dark]) .separator { background: rgba(255,255,255,.12); }
  `);
  credentialPositionSheet = new CSSStyleSheet();
  shadow.adoptedStyleSheets = [style, credentialPositionSheet];
  const panel = document.createElement("div");
  panel.className = "panel";
  panel.addEventListener("pointerleave", () => selectCredentialSuggestion(-1));
  const title = document.createElement("div");
  title.className = "title";
  title.textContent = String(credentialSuggestionPayload.title || "");
  panel.append(title);
  credentials.forEach((credential, index) => {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "row";
    row.append(keyIcon());
    const username = document.createElement("span");
    username.className = "username";
    username.textContent = String(credential.username || credentialSuggestionPayload.origin || "");
    row.append(username);
    row.addEventListener("pointerenter", () => selectCredentialSuggestion(index));
    row.addEventListener("pointerleave", () => {
      if (selectedCredentialIndex === index) selectCredentialSuggestion(-1);
    });
    row.addEventListener("pointerdown", (event) => {
      if (!event.isTrusted) return;
      event.preventDefault();
      event.stopPropagation();
      requestCredentialFill(credential);
    });
    credentialSuggestionRows.push(row);
    panel.append(row);
  });
  const separator = document.createElement("div");
  separator.className = "separator";
  panel.append(separator);
  const manage = document.createElement("button");
  manage.type = "button";
  manage.className = "row manage";
  manage.append(keyIcon());
  const manageLabel = document.createElement("span");
  manageLabel.textContent = String(credentialSuggestionPayload.manageLabel || "");
  manage.append(manageLabel);
  manage.addEventListener("pointerenter", () => selectCredentialSuggestion(-1));
  manage.addEventListener("pointerdown", (event) => {
    if (!event.isTrusted) return;
    event.preventDefault();
    event.stopPropagation();
    ipcRenderer.send(credentialManageRequestChannel);
    hideCredentialSuggestions();
  });
  panel.append(manage);
  shadow.append(panel);
  credentialSuggestionHost = host;
  document.documentElement.append(host);
  scheduleCredentialPosition();
}

function noteCredentialFocus(event) {
  const input = event.target;
  if (!(input instanceof HTMLInputElement) || !credentialFieldsForUsername(input)) {
    focusedCredentialUsername = null;
    hideCredentialSuggestions();
    return;
  }
  focusedCredentialUsername = input;
  credentialSuggestionPayload = null;
  hideCredentialSuggestions();
  const origin = credentialOrigin();
  if (origin) ipcRenderer.send(credentialFocusChannel, { origin });
}

function handleCredentialSuggestionKey(event) {
  if (!event.isTrusted || event.target !== focusedCredentialUsername || !credentialSuggestionHost) return;
  const credentials = visibleCredentialSuggestions();
  if (event.key === "Escape") {
    event.preventDefault();
    hideCredentialSuggestions();
  } else if (event.key === "ArrowDown") {
    event.preventDefault();
    selectCredentialSuggestion(Math.min(selectedCredentialIndex + 1, credentials.length - 1));
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    selectCredentialSuggestion(selectedCredentialIndex <= 0 ? credentials.length - 1 : selectedCredentialIndex - 1);
  } else if (event.key === "Enter" && selectedCredentialIndex >= 0) {
    event.preventDefault();
    event.stopPropagation();
    requestCredentialFill(credentials[selectedCredentialIndex]);
  }
}

ipcRenderer.on(credentialSuggestionsChannel, (_event, payload) => {
  credentialSuggestionPayload = payload && typeof payload === "object" ? payload : null;
  renderCredentialSuggestions();
});

ipcRenderer.on(credentialFillChannel, (_event, payload) => {
  const requestID = String(payload?.requestID || "").trim();
  let ok = false;
  let reason = "credential_form_not_found";
  try {
    if (!topLevelDocument || !requestID || String(payload?.origin || "") !== credentialOrigin()) {
      reason = "credential_origin_mismatch";
    } else {
      const fields = credentialFieldsForUsername(focusedCredentialUsername) || credentialFields(null);
      if (fields) {
        if (fields.username && typeof payload?.username === "string") {
          setNativeInputValue(fields.username, payload.username);
        }
        setNativeInputValue(fields.password, String(payload?.password || ""));
        ok = true;
        reason = "";
        hideCredentialSuggestions();
      }
    }
  } catch {
    reason = "credential_fill_failed";
  }
  ipcRenderer.send(credentialFillResultChannel, { requestID, ok, reason });
});

document.addEventListener("pointerdown", noteLocalInteraction, true);
document.addEventListener("keydown", noteLocalInteraction, true);
document.addEventListener("click", () => publishSelection(), false);
document.addEventListener("selectionchange", () => publishSelection(), true);
if (topLevelDocument) {
  document.addEventListener("submit", submitCredentialCandidate, true);
  document.addEventListener("focusin", noteCredentialFocus, true);
  document.addEventListener("keydown", handleCredentialSuggestionKey, true);
  document.addEventListener("input", (event) => {
    if (event.target === focusedCredentialUsername && credentialSuggestionPayload) renderCredentialSuggestions();
  }, true);
  document.addEventListener("pointerdown", (event) => {
    if (event.target !== credentialSuggestionHost) hideCredentialSuggestions();
  }, true);
  document.addEventListener("scroll", scheduleCredentialPosition, true);
  window.addEventListener("resize", scheduleCredentialPosition);
}
window.addEventListener("focus", () => publishSelection());
const credentialObserver = new MutationObserver(scheduleCredentialDetection);

function observeCredentialForms() {
  if (!topLevelDocument || !document.documentElement) return;
  credentialObserver.disconnect();
  credentialObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["autocomplete", "disabled", "readonly", "type"],
    childList: true,
    subtree: true,
  });
  scheduleCredentialDetection();
}

window.addEventListener("DOMContentLoaded", observeCredentialForms, { once: true });
window.addEventListener("pageshow", observeCredentialForms);
observeCredentialForms();

window.addEventListener("pagehide", () => {
  window.clearTimeout(interactionTimer);
  window.clearTimeout(credentialDetectionTimer);
  hideCredentialSuggestions();
  credentialSuggestionPayload = null;
  focusedCredentialUsername = null;
  if (topLevelDocument) credentialObserver.disconnect();
  lastSelectionText = "";
  ipcRenderer.send(selectionChangedChannel, { selectionText: "" });
  if (topLevelDocument) ipcRenderer.send(credentialFormChannel, { origin: credentialOrigin(), detected: false });
});
