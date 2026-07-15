export function normalizeUpdatePreferences(value) {
  return {
    channel: value?.channel || "stable",
    enabled: Boolean(value?.enabled),
  };
}
