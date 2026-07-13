const fs = require("node:fs");
const path = require("node:path");

function readPreviewUpdatePreference(filePath) {
  try {
    const value = JSON.parse(fs.readFileSync(filePath, "utf8"))?.receivePreviewUpdates;
    return typeof value === "boolean" ? value : null;
  } catch {
    return null;
  }
}

function writePreviewUpdatePreference(filePath, enabled) {
  const directory = path.dirname(filePath);
  const temporaryPath = `${filePath}.${process.pid}.tmp`;
  const preferences = readPreferences(filePath);
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  try {
    fs.writeFileSync(
      temporaryPath,
      `${JSON.stringify({ ...preferences, receivePreviewUpdates: Boolean(enabled) }, null, 2)}\n`,
      { mode: 0o600 },
    );
    fs.renameSync(temporaryPath, filePath);
  } finally {
    try {
      fs.rmSync(temporaryPath, { force: true });
    } catch {
      // Best-effort cleanup after a failed atomic write.
    }
  }
}

function readPreferences(filePath) {
  try {
    const value = JSON.parse(fs.readFileSync(filePath, "utf8"));
    return value && typeof value === "object" && !Array.isArray(value) ? value : {};
  } catch {
    return {};
  }
}

module.exports = { readPreviewUpdatePreference, writePreviewUpdatePreference };
