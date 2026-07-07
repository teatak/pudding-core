import { z } from "zod";

import type { Locale } from "@/i18n";

const localizedText = z.record(z.string(), z.string());

const starterPromptItem = z.object({
  id: z.string().min(1),
  capabilities: z.array(z.string().min(1)).default([]),
  labels: localizedText,
  prompts: localizedText,
});

const starterPromptCatalog = z.object({
  version: z.number().int().positive(),
  items: z.array(starterPromptItem),
});

const starterPromptCache = z.object({
  expiresAt: z.number(),
  data: starterPromptCatalog,
});

export type StarterPromptCatalogItem = z.infer<typeof starterPromptItem>;
export type StarterPromptCatalog = z.infer<typeof starterPromptCatalog>;
export type LocalizedStarterPrompt = {
  id: string;
  capabilities: string[];
  label: string;
  prompt: string;
};

export const OFFICIAL_STARTER_PROMPTS_URL =
  import.meta.env.VITE_PUDDING_STARTER_PROMPTS_URL ||
  "https://cdn.jsdelivr.net/gh/teatak/pudding@main/catalog/starter-prompts.json";

export const STARTER_PROMPTS_CACHE_TTL_MS = 5 * 60 * 1000;

export async function fetchStarterPromptCatalog(url = OFFICIAL_STARTER_PROMPTS_URL): Promise<StarterPromptCatalog> {
  const cached = readStarterPromptCache(url);
  if (cached) {
    return cached;
  }
  const response = await fetch(url, { cache: "reload" });
  if (!response.ok) {
    throw new Error(`starter prompts request failed: ${response.status}`);
  }
  const data = starterPromptCatalog.parse(await response.json());
  writeStarterPromptCache(url, data);
  return data;
}

export function localizeStarterPrompts(items: StarterPromptCatalogItem[], locale: Locale): LocalizedStarterPrompt[] {
  return items
    .map((item) => ({
      id: item.id,
      capabilities: item.capabilities,
      label: localizeText(item.labels, locale),
      prompt: localizeText(item.prompts, locale),
    }))
    .filter((item) => item.label && item.prompt);
}

function localizeText(value: Record<string, string>, locale: Locale) {
  return (
    value[locale]?.trim() ||
    value["zh-CN"]?.trim() ||
    value.en?.trim() ||
    Object.values(value).find((text) => text.trim())?.trim() ||
    ""
  );
}

function readStarterPromptCache(url: string): StarterPromptCatalog | null {
  try {
    const raw = window.localStorage.getItem(starterPromptCacheKey(url));
    if (!raw) {
      return null;
    }
    const cached = starterPromptCache.parse(JSON.parse(raw));
    if (cached.expiresAt <= Date.now()) {
      window.localStorage.removeItem(starterPromptCacheKey(url));
      return null;
    }
    return cached.data;
  } catch {
    return null;
  }
}

function writeStarterPromptCache(url: string, data: StarterPromptCatalog) {
  try {
    window.localStorage.setItem(
      starterPromptCacheKey(url),
      JSON.stringify({
        expiresAt: Date.now() + STARTER_PROMPTS_CACHE_TTL_MS,
        data,
      }),
    );
  } catch {
    // 缓存失败不影响远程加载结果。
  }
}

function starterPromptCacheKey(url: string) {
  return `pudding.starterPrompts:${url}`;
}
