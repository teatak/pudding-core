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
  "https://raw.githubusercontent.com/teatak/pudding/main/catalog/starter-prompts.json";

export async function fetchStarterPromptCatalog(url = OFFICIAL_STARTER_PROMPTS_URL): Promise<StarterPromptCatalog> {
  const response = await fetch(url, { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`starter prompts request failed: ${response.status}`);
  }
  return starterPromptCatalog.parse(await response.json());
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
