const nativeMessages = {
  "zh-CN": {
    about: "关于 {app}",
    services: "服务",
    hideApp: "隐藏 {app}",
    hideOthers: "隐藏其他",
    showAll: "全部显示",
    quitApp: "退出 {app}",
    showApp: "显示 {app}",
    file: "文件",
    closeWindow: "关闭窗口",
    edit: "编辑",
    undo: "撤销",
    redo: "重做",
    cut: "剪切",
    copy: "复制",
    paste: "粘贴",
    pasteAndMatchStyle: "粘贴并匹配样式",
    delete: "删除",
    selectAll: "全选",
    view: "视图",
    reload: "重新加载",
    forceReload: "强制重新加载",
    toggleDevTools: "切换开发者工具",
    actualSize: "实际大小",
    zoomIn: "放大",
    zoomOut: "缩小",
    fullScreen: "全屏",
    window: "窗口",
    minimize: "最小化",
    zoomWindow: "缩放窗口",
    bringAllToFront: "前置全部窗口",
  },
  "zh-TW": {
    about: "關於 {app}",
    services: "服務",
    hideApp: "隱藏 {app}",
    hideOthers: "隱藏其他",
    showAll: "全部顯示",
    quitApp: "結束 {app}",
    showApp: "顯示 {app}",
    file: "檔案",
    closeWindow: "關閉視窗",
    edit: "編輯",
    undo: "復原",
    redo: "重做",
    cut: "剪下",
    copy: "複製",
    paste: "貼上",
    pasteAndMatchStyle: "貼上並符合樣式",
    delete: "刪除",
    selectAll: "全選",
    view: "顯示",
    reload: "重新載入",
    forceReload: "強制重新載入",
    toggleDevTools: "切換開發者工具",
    actualSize: "實際大小",
    zoomIn: "放大",
    zoomOut: "縮小",
    fullScreen: "全螢幕",
    window: "視窗",
    minimize: "縮到最小",
    zoomWindow: "縮放視窗",
    bringAllToFront: "將所有視窗移到最前面",
  },
  en: {
    about: "About {app}",
    services: "Services",
    hideApp: "Hide {app}",
    hideOthers: "Hide Others",
    showAll: "Show All",
    quitApp: "Quit {app}",
    showApp: "Show {app}",
    file: "File",
    closeWindow: "Close Window",
    edit: "Edit",
    undo: "Undo",
    redo: "Redo",
    cut: "Cut",
    copy: "Copy",
    paste: "Paste",
    pasteAndMatchStyle: "Paste and Match Style",
    delete: "Delete",
    selectAll: "Select All",
    view: "View",
    reload: "Reload",
    forceReload: "Force Reload",
    toggleDevTools: "Toggle Developer Tools",
    actualSize: "Actual Size",
    zoomIn: "Zoom In",
    zoomOut: "Zoom Out",
    fullScreen: "Full Screen",
    window: "Window",
    minimize: "Minimize",
    zoomWindow: "Zoom",
    bringAllToFront: "Bring All to Front",
  },
};

function normalizeNativeLocale(value) {
  const locale = String(value || "").trim().replaceAll("_", "-").toLowerCase();
  if (locale.startsWith("zh")) {
    return /(?:^|-)tw(?:-|$)|(?:^|-)hk(?:-|$)|(?:^|-)mo(?:-|$)|hant/.test(locale) ? "zh-TW" : "zh-CN";
  }
  return "en";
}

function nativeText(locale, key, values = {}) {
  const normalized = normalizeNativeLocale(locale);
  let text = nativeMessages[normalized]?.[key] || nativeMessages.en[key] || key;
  for (const [name, value] of Object.entries(values)) {
    text = text.replaceAll(`{${name}}`, String(value));
  }
  return text;
}

module.exports = { nativeText, normalizeNativeLocale };
