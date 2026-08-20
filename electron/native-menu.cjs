const maxMenuDepth = 4;
const maxMenuItems = 128;
const maxMenuIDLength = 128;
const maxMenuLabelLength = 512;
const validMenuID = /^[A-Za-z0-9._:-]+$/;
const validItemTypes = new Set(["normal", "checkbox", "radio"]);

function buildNativeMenuTemplate(rawItems, onSelect) {
  const state = { count: 0, ids: new Set() };
  return normalizeItems(rawItems, onSelect, state, 0);
}

function normalizeItems(rawItems, onSelect, state, depth) {
  if (!Array.isArray(rawItems) || depth > maxMenuDepth) {
    return [];
  }
  const template = [];
  for (const rawItem of rawItems) {
    state.count += 1;
    if (state.count > maxMenuItems) {
      throw new TypeError("native menu contains too many items");
    }
    if (!rawItem || typeof rawItem !== "object") {
      continue;
    }
    if (rawItem.type === "separator") {
      appendSeparator(template);
      continue;
    }
    const label = normalizeLabel(rawItem.label);
    if (!label) {
      continue;
    }
    if (rawItem.type === "label") {
      template.push({ label, enabled: false });
      continue;
    }
    const submenu = normalizeItems(rawItem.submenu, onSelect, state, depth + 1);
    if (submenu.length > 0) {
      template.push({ label, enabled: rawItem.enabled !== false, submenu });
      continue;
    }
    const id = normalizeID(rawItem.id);
    if (!id || state.ids.has(id)) {
      continue;
    }
    state.ids.add(id);
    const type = validItemTypes.has(rawItem.type) ? rawItem.type : "normal";
    template.push({
      id,
      label,
      type,
      enabled: rawItem.enabled !== false,
      ...(type === "checkbox" || type === "radio" ? { checked: Boolean(rawItem.checked) } : {}),
      click: () => onSelect?.(id),
    });
  }
  trimSeparators(template);
  return template;
}

function normalizeNativeMenuPosition(request, window) {
  const x = Number(request?.x);
  const y = Number(request?.y);
  if (!Number.isFinite(x) || !Number.isFinite(y)) {
    return {};
  }
  const bounds = window.getContentBounds();
  return {
    x: Math.max(0, Math.min(Math.round(x), bounds.width)),
    y: Math.max(0, Math.min(Math.round(y), bounds.height)),
  };
}

function normalizeID(value) {
  const id = typeof value === "string" ? value.trim() : "";
  return id.length > 0 && id.length <= maxMenuIDLength && validMenuID.test(id) ? id : "";
}

function normalizeLabel(value) {
  const label = typeof value === "string" ? value.trim() : "";
  return label.slice(0, maxMenuLabelLength);
}

function appendSeparator(template) {
  if (template.length > 0 && template.at(-1)?.type !== "separator") {
    template.push({ type: "separator" });
  }
}

function trimSeparators(template) {
  while (template.at(-1)?.type === "separator") {
    template.pop();
  }
}

module.exports = { buildNativeMenuTemplate, normalizeNativeMenuPosition };
