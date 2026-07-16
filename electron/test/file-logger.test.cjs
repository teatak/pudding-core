const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { DailyLogWriter, redactLogText } = require("../file-logger.cjs");

test("daily logger rotates and keeps ten calendar days", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-electron-log-"));
  let now = new Date(2026, 6, 16, 23, 59, 0);
  fs.writeFileSync(path.join(dir, "electron-2026-07-06.log"), "expired\n");
  fs.writeFileSync(path.join(dir, "electron-2026-07-07.log"), "boundary\n");
  fs.writeFileSync(path.join(dir, "notes.log"), "unrelated\n");

  const writer = new DailyLogWriter({ logsDir: dir, prefix: "electron", now: () => now });
  writer.write("INFO", ["started", { token: "secret" }]);

  assert.equal(fs.existsSync(path.join(dir, "electron-2026-07-06.log")), false);
  assert.equal(fs.existsSync(path.join(dir, "electron-2026-07-07.log")), true);
  assert.equal(fs.existsSync(path.join(dir, "notes.log")), true);

  now = new Date(2026, 6, 17, 0, 1, 0);
  writer.write("ERROR", [new Error("boom")]);
  const firstLog = fs.readFileSync(path.join(dir, "electron-2026-07-16.log"), "utf8");
  assert.match(firstLog, /started/);
  assert.match(firstLog, /\[REDACTED\]/);
  assert.doesNotMatch(firstLog, /secret/);
  assert.match(fs.readFileSync(path.join(dir, "electron-2026-07-17.log"), "utf8"), /boom/);
});

test("electron logger redacts credentials", () => {
  const redacted = redactLogText(
    "http://127.0.0.1/?token=abc&code=oauth authorization=Bearer xyz api_key=key-value",
  );
  assert.doesNotMatch(redacted, /abc|oauth|xyz|key-value/);
  assert.match(redacted, /token=\[REDACTED\]/);
});
