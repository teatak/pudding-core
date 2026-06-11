import { useSyncExternalStore } from "react";

export type Locale = "zh-CN" | "zh-TW" | "en";

type Messages = Record<string, string>;

const STORAGE_KEY = "pudding.locale";

const zh: Messages = {
  "app.name": "Pudding",
  "app.core": "Pudding Core",
  "common.cancel": "取消",
  "common.delete": "删除",
  "common.loading": "加载中",
  "common.refresh": "刷新",
  "common.save": "保存",
  "composer.messagePlaceholder": "消息",
  "composer.send": "发送",
  "composer.stop": "停止",
  "composer.stopPending": "请稍候",
  "composer.turnRunning": "当前会话正在生成。",
  "composer.submitFailed": "发送失败",
  "deleteSession.description": "删除后会移除此会话及其消息。这个操作不能撤销。",
  "deleteSession.title": "删除这个会话？",
  "language.en": "English",
  "language.label": "语言",
  "language.toggle": "切换语言",
  "language.zhCN": "中文(简体)",
  "language.zhTW": "中文(繁體)",
  "session.create": "新建会话",
  "session.delete": "删除会话",
  "session.empty": "暂无会话",
  "session.model": "mock-model",
  "session.noSelected": "未选择会话",
  "session.selectOrCreate": "创建或选择一个会话",
  "session.start": "开始对话",
  "session.untitled": "未命名会话",
  "settings.description": "键值配置",
  "settings.entries": "配置项",
  "settings.placeholder":
    "provider.openai.base_url=https://api.openai.com/v1\nprovider.openai.api_key=sk-...\nmodel.default=gpt-5.5\nsystem_prompt=You are...",
  "settings.providerPresets": "供应商预设",
  "settings.title": "设置",
  "theme.dark": "深色",
  "theme.light": "浅色",
  "theme.system": "跟随系统",
  "theme.toggle": "切换主题",
  "token.continue": "继续",
  "token.description": "守护进程令牌",
  "token.label": "令牌",
  "token.placeholder": "粘贴 daemon token",
  "transcript.interrupted": "已中断",
  "transcript.jumpLatest": "跳到最新",
};

const zhTW: Messages = {
  "app.name": "Pudding",
  "app.core": "Pudding Core",
  "common.cancel": "取消",
  "common.delete": "刪除",
  "common.loading": "載入中",
  "common.refresh": "重新整理",
  "common.save": "儲存",
  "composer.messagePlaceholder": "訊息",
  "composer.send": "傳送",
  "composer.stop": "停止",
  "composer.stopPending": "請稍候",
  "composer.turnRunning": "目前會話正在生成。",
  "composer.submitFailed": "傳送失敗",
  "deleteSession.description": "刪除後會移除此會話及其訊息。這個操作無法復原。",
  "deleteSession.title": "刪除這個會話？",
  "language.en": "English",
  "language.label": "語言",
  "language.toggle": "切換語言",
  "language.zhCN": "中文(简体)",
  "language.zhTW": "中文(繁體)",
  "session.create": "新增會話",
  "session.delete": "刪除會話",
  "session.empty": "暫無會話",
  "session.model": "mock-model",
  "session.noSelected": "未選擇會話",
  "session.selectOrCreate": "建立或選擇一個會話",
  "session.start": "開始對話",
  "session.untitled": "未命名會話",
  "settings.description": "鍵值設定",
  "settings.entries": "設定項",
  "settings.placeholder":
    "provider.openai.base_url=https://api.openai.com/v1\nprovider.openai.api_key=sk-...\nmodel.default=gpt-5.5\nsystem_prompt=You are...",
  "settings.providerPresets": "供應商預設",
  "settings.title": "設定",
  "theme.dark": "深色",
  "theme.light": "淺色",
  "theme.system": "跟隨系統",
  "theme.toggle": "切換主題",
  "token.continue": "繼續",
  "token.description": "守護程序令牌",
  "token.label": "令牌",
  "token.placeholder": "貼上 daemon token",
  "transcript.interrupted": "已中斷",
  "transcript.jumpLatest": "跳到最新",
};

const en: Messages = {
  "app.name": "Pudding",
  "app.core": "Pudding Core",
  "common.cancel": "Cancel",
  "common.delete": "Delete",
  "common.loading": "Loading",
  "common.refresh": "Refresh",
  "common.save": "Save",
  "composer.messagePlaceholder": "Message",
  "composer.send": "Send",
  "composer.stop": "Stop",
  "composer.stopPending": "Please wait",
  "composer.turnRunning": "The session is already streaming.",
  "composer.submitFailed": "Submit failed",
  "deleteSession.description": "This removes the session and its messages. This action cannot be undone.",
  "deleteSession.title": "Delete this session?",
  "language.en": "English",
  "language.label": "Language",
  "language.toggle": "Change language",
  "language.zhCN": "中文(简体)",
  "language.zhTW": "中文(繁體)",
  "session.create": "Create session",
  "session.delete": "Delete session",
  "session.empty": "No sessions",
  "session.model": "mock-model",
  "session.noSelected": "No session selected",
  "session.selectOrCreate": "Create or select a session",
  "session.start": "Start a conversation",
  "session.untitled": "Untitled session",
  "settings.description": "Key-value settings",
  "settings.entries": "Entries",
  "settings.placeholder":
    "provider.openai.base_url=https://api.openai.com/v1\nprovider.openai.api_key=sk-...\nmodel.default=gpt-5.5\nsystem_prompt=You are...",
  "settings.providerPresets": "Provider presets",
  "settings.title": "Settings",
  "theme.dark": "Dark",
  "theme.light": "Light",
  "theme.system": "System",
  "theme.toggle": "Change theme",
  "token.continue": "Continue",
  "token.description": "Daemon token",
  "token.label": "Token",
  "token.placeholder": "Paste daemon token",
  "transcript.interrupted": "interrupted",
  "transcript.jumpLatest": "Jump to latest",
};

const dictionaries: Record<Locale, Messages> = { "zh-CN": zh, "zh-TW": zhTW, en };
const listeners = new Set<() => void>();
let current = detectLocale();

function detectLocale(): Locale {
  if (typeof window === "undefined") {
    return "en";
  }
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === "zh-CN" || stored === "zh-TW" || stored === "en") {
    return stored;
  }
  const language = navigator.language.toLowerCase();
  if (language.startsWith("zh")) {
    return language.includes("tw") || language.includes("hk") || language.includes("hant") ? "zh-TW" : "zh-CN";
  }
  return "en";
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function snapshot() {
  return current;
}

export function setLocale(locale: Locale) {
  current = locale;
  window.localStorage.setItem(STORAGE_KEY, locale);
  listeners.forEach((listener) => listener());
}

export function translate(key: string, locale = current) {
  return dictionaries[locale][key] ?? dictionaries.en[key] ?? key;
}

export function useI18n() {
  const locale = useSyncExternalStore(subscribe, snapshot, snapshot);
  return {
    locale,
    setLocale,
    t: (key: string) => translate(key, locale),
  };
}
