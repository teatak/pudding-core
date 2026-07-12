import { z } from "zod";

import bundledCatalogJSON from "@/assets/user-messages.json";
import type { Locale } from "@/i18n";

const localizedText = z.record(z.string(), z.string());
const appVersion = z.string().regex(/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/);
const externalURL = z
  .string()
  .url()
  .refine((value) => value.startsWith("https://") || value.startsWith("http://"));

const userMessageItem = z.object({
  id: z.string().min(1),
  placement: z.string().min(1),
  enabled: z.boolean().default(true),
  priority: z.number().int().default(0),
  startsAt: z.string().datetime({ offset: true }).optional(),
  endsAt: z.string().datetime({ offset: true }).optional(),
  minAppVersion: appVersion.optional(),
  maxAppVersion: appVersion.optional(),
  titles: localizedText,
  subtitles: localizedText.optional(),
  link: z
    .object({
      url: externalURL,
      labels: localizedText,
    })
    .optional(),
});

const userMessageCatalog = z.object({
  version: z.number().int().positive(),
  refreshMinutes: z.number().int().min(5).max(24 * 60).default(30),
  items: z.array(userMessageItem),
});

const userMessageCache = z.object({
  expiresAt: z.number(),
  data: userMessageCatalog,
});

export type UserMessageCatalog = z.infer<typeof userMessageCatalog>;
export type LocalizedUserMessage = {
  id: string;
  title: string;
  subtitle?: string;
  link?: { label: string; url: string };
};

export const OFFICIAL_USER_MESSAGES_URL =
  import.meta.env.VITE_PUDDING_USER_MESSAGES_URL ||
  "https://cdn.jsdelivr.net/gh/teatak/pudding@main/catalog/user-messages.json";

export const USER_MESSAGES_STALE_TIME_MS = 15 * 60 * 1000;

const bundledCatalog = userMessageCatalog.parse(bundledCatalogJSON);

export async function fetchUserMessageCatalog(url = OFFICIAL_USER_MESSAGES_URL): Promise<UserMessageCatalog> {
  const cached = readUserMessageCache(url);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.data;
  }
  try {
    const response = await fetch(url, { cache: "reload", signal: AbortSignal.timeout(3_000) });
    if (!response.ok) {
      throw new Error(`user messages request failed: ${response.status}`);
    }
    const data = userMessageCatalog.parse(await response.json());
    writeUserMessageCache(url, data);
    return data;
  } catch {
    return cached?.data || bundledCatalog;
  }
}

export function localizeUserMessage(
  catalog: UserMessageCatalog,
  locale: Locale,
  placement: string,
  now = new Date(),
): LocalizedUserMessage | null {
  const selected = catalog.items
    .map((item, index) => ({ item, index }))
    .filter(
      ({ item }) =>
        item.enabled &&
        item.placement === placement &&
        isActiveUserMessage(item, now, __PUDDING_APP_VERSION__),
    )
    .sort((left, right) => right.item.priority - left.item.priority || left.index - right.index)[0]?.item;
  if (!selected) {
    return null;
  }
  const title = localizeText(selected.titles, locale);
  if (!title) {
    return null;
  }
  const subtitle = selected.subtitles ? localizeText(selected.subtitles, locale) : "";
  const linkLabel = selected.link ? localizeText(selected.link.labels, locale) : "";
  return {
    id: selected.id,
    title,
    ...(subtitle ? { subtitle } : {}),
    ...(selected.link && linkLabel ? { link: { label: linkLabel, url: selected.link.url } } : {}),
  };
}

function isActiveUserMessage(item: z.infer<typeof userMessageItem>, now: Date, currentVersion: string) {
  const timestamp = now.getTime();
  if (item.startsAt && Date.parse(item.startsAt) > timestamp) {
    return false;
  }
  if (item.endsAt && Date.parse(item.endsAt) <= timestamp) {
    return false;
  }
  if (item.minAppVersion && compareVersions(currentVersion, item.minAppVersion) < 0) {
    return false;
  }
  if (item.maxAppVersion && compareVersions(currentVersion, item.maxAppVersion) > 0) {
    return false;
  }
  return true;
}

function compareVersions(left: string, right: string) {
  const [leftCore, leftPrerelease = ""] = left.trim().split("-", 2);
  const [rightCore, rightPrerelease = ""] = right.trim().split("-", 2);
  const leftParts = leftCore.split(".").map((value) => Number.parseInt(value, 10) || 0);
  const rightParts = rightCore.split(".").map((value) => Number.parseInt(value, 10) || 0);
  for (let index = 0; index < Math.max(leftParts.length, rightParts.length, 3); index += 1) {
    const difference = (leftParts[index] || 0) - (rightParts[index] || 0);
    if (difference !== 0) {
      return difference;
    }
  }
  if (!leftPrerelease && rightPrerelease) {
    return 1;
  }
  if (leftPrerelease && !rightPrerelease) {
    return -1;
  }
  return leftPrerelease.localeCompare(rightPrerelease);
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

function readUserMessageCache(url: string): z.infer<typeof userMessageCache> | null {
  try {
    const raw = window.localStorage.getItem(userMessageCacheKey(url));
    return raw ? userMessageCache.parse(JSON.parse(raw)) : null;
  } catch {
    return null;
  }
}

function writeUserMessageCache(url: string, data: UserMessageCatalog) {
  try {
    window.localStorage.setItem(
      userMessageCacheKey(url),
      JSON.stringify({
        expiresAt: Date.now() + data.refreshMinutes * 60_000,
        data,
      }),
    );
  } catch {
    // A cache failure must not hide bundled or remotely loaded user messages.
  }
}

function userMessageCacheKey(url: string) {
  return `pudding.userMessages:${url}`;
}
