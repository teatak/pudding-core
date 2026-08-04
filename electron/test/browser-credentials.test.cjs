const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  BrowserCredentialController,
  BrowserCredentialVault,
  parseChromePasswordCSV,
} = require("../browser-credentials.cjs");

function fakeSafeStorage() {
  return {
    isEncryptionAvailable: () => true,
    encryptStringAsync: async (value) => Buffer.from(`encrypted:${Buffer.from(value).toString("base64")}`),
    decryptStringAsync: async (value) => ({
      result: Buffer.from(String(value).slice("encrypted:".length), "base64").toString(),
      shouldReEncrypt: false,
    }),
  };
}

async function testVault(t) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "pudding-browser-credentials-"));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  let id = 0;
  return new BrowserCredentialVault({
    filePath: path.join(directory, "credentials.vault"),
    safeStorage: fakeSafeStorage(),
    randomUUID: () => `00000000-0000-0000-0000-${String(++id).padStart(12, "0")}`,
  });
}

test("stores the complete browser credential vault encrypted and updates duplicates", async (t) => {
  const vault = await testVault(t);
  const first = await vault.save({ origin: "https://example.com/login", username: "user@example.com", password: "first-secret" });
  const updated = await vault.save({ origin: "https://example.com/other", username: "user@example.com", password: "second-secret" });

  assert.equal(updated.id, first.id);
  assert.equal((await vault.list()).credentials.length, 1);
  assert.equal((await vault.secret(first.id)).password, "second-secret");

  const bytes = await fs.readFile(vault.filePath, "utf8");
  assert.doesNotMatch(bytes, /example\.com|user@example\.com|second-secret/);
  assert.equal((await fs.stat(path.dirname(vault.filePath))).mode & 0o777, 0o700);
  assert.equal((await fs.stat(vault.filePath)).mode & 0o777, 0o600);
});

test("controller scopes fill to the exact origin and suppresses never-save sites", async (t) => {
  const vault = await testVault(t);
  let now = new Date("2026-08-04T00:00:00.000Z");
  let id = 0;
  const controller = new BrowserCredentialController({
    vault,
    now: () => now,
    randomUUID: () => `10000000-0000-0000-0000-${String(++id).padStart(12, "0")}`,
  });
  const context = { sessionID: "sess_a", tabID: "tab_a", url: "https://example.com/login" };
  const prompt = await controller.noteCandidate(context, {
    origin: "https://example.com",
    username: "alice",
    password: "secret",
  });
  assert.equal(prompt.kind, "save");
  const saved = await controller.commit(context, prompt.id);
  assert.equal((await controller.fill(context, saved.id)).password, "secret");
  await assert.rejects(
    controller.fill({ ...context, url: "https://other.example/" }, saved.id),
    /origin mismatch/,
  );

  const secondPrompt = await controller.noteCandidate(context, {
    origin: "https://example.com",
    username: "bob",
    password: "other",
  });
  await controller.dismiss(context, secondPrompt.id, true);
  assert.equal(await controller.noteCandidate(context, {
    origin: "https://example.com",
    username: "charlie",
    password: "ignored",
  }), null);

  now = new Date("2026-08-04T00:20:00.000Z");
  assert.equal((await controller.state(context)).prompt, null);
});

test("controller discards password candidates as soon as their tab is released", async (t) => {
  const vault = await testVault(t);
  const controller = new BrowserCredentialController({ vault });
  const context = { sessionID: "sess_a", tabID: "tab_a", url: "https://example.com/login" };
  await controller.noteForm(context, true);
  await controller.noteCandidate(context, { origin: "https://example.com", username: "alice", password: "secret" });

  controller.release(context);

  const state = await controller.state(context);
  assert.equal(state.formDetected, false);
  assert.equal(state.prompt, null);
});

test("parses quoted Chrome password CSV records without returning row secrets in errors", () => {
  const parsed = parseChromePasswordCSV([
    "name,url,username,password,note",
    'Example,https://example.com/login,"user,one","p""ass",',
    "Invalid,not-a-url,user,secret,",
  ].join("\n"));

  assert.deepEqual(parsed, {
    records: [{ origin: "https://example.com", username: "user,one", password: 'p"ass' }],
    skipped: 1,
  });
});

test("an explicit password import re-enables saving for imported sites", async (t) => {
  const vault = await testVault(t);
  await vault.setNeverSave("https://example.com", true);
  await vault.importRecords([{ origin: "https://example.com/login", username: "alice", password: "secret" }]);

  assert.equal(await vault.isNeverSaveOrigin("https://example.com"), false);
  assert.equal((await vault.listForOrigin("https://example.com")).length, 1);
});
