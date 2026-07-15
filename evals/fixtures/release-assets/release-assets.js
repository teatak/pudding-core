const required = [
  "Pudding-arm64.dmg",
  "Pudding-arm64.zip",
  "Pudding-x64.dmg",
  "Pudding-x64.zip",
  "latest-mac.yml",
];

export function validateReleaseAssets(files) {
  const missing = required.filter((name) => !files.includes(name));
  return { ok: files.length >= required.length, missing, duplicates: [] };
}
