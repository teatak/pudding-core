const crypto = require("node:crypto");
const fs = require("node:fs/promises");
const path = require("node:path");

const vaultVersion = 1;
const maxCredentialCount = 10_000;
const maxImportBytes = 10 * 1024 * 1024;
const pendingCredentialTTLMS = 10 * 60 * 1000;

class BrowserCredentialVault {
  constructor({ filePath, safeStorage, fsModule = fs, now = () => new Date(), randomUUID = () => crypto.randomUUID() }) {
    this.filePath = String(filePath || "").trim();
    this.safeStorage = safeStorage;
    this.fs = fsModule;
    this.now = now;
    this.randomUUID = randomUUID;
    this.loaded = false;
    this.state = emptyVault();
    this.queue = Promise.resolve();
  }

  availability() {
    if (!this.safeStorage?.isEncryptionAvailable?.()) {
      return { available: false, reason: "encryption_unavailable" };
    }
    if (process.platform === "linux" && this.safeStorage.getSelectedStorageBackend?.() === "basic_text") {
      return { available: false, reason: "insecure_storage_backend" };
    }
    return { available: true, reason: "" };
  }

  list() {
    return this.run(async () => {
      await this.load();
      return {
        credentials: this.state.credentials.map(credentialMetadata),
        neverSaveOrigins: [...this.state.neverSaveOrigins],
      };
    });
  }

  listForOrigin(rawOrigin) {
    const origin = normalizeCredentialOrigin(rawOrigin);
    return this.run(async () => {
      await this.load();
      return this.state.credentials
        .filter((credential) => credential.origin === origin)
        .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))
        .map(credentialMetadata);
    });
  }

  isNeverSaveOrigin(rawOrigin) {
    const origin = normalizeCredentialOrigin(rawOrigin);
    return this.run(async () => {
      await this.load();
      return this.state.neverSaveOrigins.includes(origin);
    });
  }

  inspectCandidate(candidate) {
    const normalized = normalizeCredential(candidate);
    return this.run(async () => {
      await this.load();
      if (this.state.neverSaveOrigins.includes(normalized.origin)) {
        return { ignored: true, kind: "" };
      }
      const existing = this.state.credentials.find(
        (credential) => credential.origin === normalized.origin && credential.username === normalized.username,
      );
      if (!existing) {
        return { ignored: false, kind: "save" };
      }
      if (existing.password === normalized.password) {
        return { ignored: true, kind: "" };
      }
      return { ignored: false, kind: "update", credentialID: existing.id };
    });
  }

  save(candidate) {
    const normalized = normalizeCredential(candidate);
    return this.run(async () => {
      await this.load();
      const now = this.now().toISOString();
      const existing = this.state.credentials.find(
        (credential) => credential.origin === normalized.origin && credential.username === normalized.username,
      );
      if (existing) {
        existing.password = normalized.password;
        existing.updatedAt = now;
        this.state.neverSaveOrigins = this.state.neverSaveOrigins.filter((origin) => origin !== normalized.origin);
        await this.persist();
        return credentialMetadata(existing);
      }
      if (this.state.credentials.length >= maxCredentialCount) {
        throw new Error("browser credential limit reached");
      }
      const credential = {
        id: `credential_${this.randomUUID().replaceAll("-", "")}`,
        origin: normalized.origin,
        username: normalized.username,
        password: normalized.password,
        createdAt: now,
        updatedAt: now,
      };
      this.state.credentials.push(credential);
      this.state.neverSaveOrigins = this.state.neverSaveOrigins.filter((origin) => origin !== normalized.origin);
      await this.persist();
      return credentialMetadata(credential);
    });
  }

  secret(credentialID) {
    const id = normalizeCredentialID(credentialID);
    return this.run(async () => {
      await this.load();
      const credential = this.state.credentials.find((item) => item.id === id);
      if (!credential) {
        throw new Error("browser credential not found");
      }
      return { ...credential };
    });
  }

  delete(credentialID) {
    const id = normalizeCredentialID(credentialID);
    return this.run(async () => {
      await this.load();
      const next = this.state.credentials.filter((credential) => credential.id !== id);
      if (next.length === this.state.credentials.length) {
        throw new Error("browser credential not found");
      }
      this.state.credentials = next;
      await this.persist();
    });
  }

  clear() {
    return this.run(async () => {
      await this.load();
      this.state.credentials = [];
      await this.persist();
    });
  }

  setNeverSave(rawOrigin, neverSave) {
    const origin = normalizeCredentialOrigin(rawOrigin);
    return this.run(async () => {
      await this.load();
      const origins = new Set(this.state.neverSaveOrigins);
      if (neverSave) {
        origins.add(origin);
      } else {
        origins.delete(origin);
      }
      this.state.neverSaveOrigins = [...origins].sort();
      await this.persist();
    });
  }

  importRecords(records) {
    const normalizedRecords = records.map(normalizeCredential);
    return this.run(async () => {
      await this.load();
      let imported = 0;
      let updated = 0;
      let unchanged = 0;
      const now = this.now().toISOString();
      for (const record of normalizedRecords) {
        const existing = this.state.credentials.find(
          (credential) => credential.origin === record.origin && credential.username === record.username,
        );
        if (existing) {
          if (existing.password === record.password) {
            unchanged += 1;
            continue;
          }
          existing.password = record.password;
          existing.updatedAt = now;
          updated += 1;
          continue;
        }
        if (this.state.credentials.length >= maxCredentialCount) {
          throw new Error("browser credential limit reached");
        }
        this.state.credentials.push({
          id: `credential_${this.randomUUID().replaceAll("-", "")}`,
          origin: record.origin,
          username: record.username,
          password: record.password,
          createdAt: now,
          updatedAt: now,
        });
        imported += 1;
      }
      if (imported || updated) {
        const importedOrigins = new Set(normalizedRecords.map((record) => record.origin));
        this.state.neverSaveOrigins = this.state.neverSaveOrigins.filter((origin) => !importedOrigins.has(origin));
        await this.persist();
      }
      return { imported, updated, unchanged };
    });
  }

  run(operation) {
    const result = this.queue.then(operation, operation);
    this.queue = result.catch(() => undefined);
    return result;
  }

  async load() {
    if (this.loaded) {
      return;
    }
    assertEncryptionAvailable(this.availability());
    if (!this.filePath) {
      throw new Error("browser credential vault path is empty");
    }
    let encrypted;
    try {
      encrypted = await this.fs.readFile(this.filePath);
    } catch (error) {
      if (error?.code === "ENOENT") {
        this.state = emptyVault();
        this.loaded = true;
        return;
      }
      throw error;
    }
    const decrypted = await this.safeStorage.decryptStringAsync(encrypted);
    this.state = parseVault(decrypted?.result);
    this.loaded = true;
    if (decrypted?.shouldReEncrypt) {
      await this.persist();
    }
  }

  async persist() {
    assertEncryptionAvailable(this.availability());
    const directory = path.dirname(this.filePath);
    const temporaryPath = `${this.filePath}.${process.pid}.${this.randomUUID()}.tmp`;
    const encrypted = await this.safeStorage.encryptStringAsync(JSON.stringify(this.state));
    await this.fs.mkdir(directory, { recursive: true, mode: 0o700 });
    await this.fs.chmod(directory, 0o700);
    try {
      await this.fs.writeFile(temporaryPath, encrypted, { mode: 0o600 });
      await this.fs.rename(temporaryPath, this.filePath);
    } finally {
      await this.fs.rm(temporaryPath, { force: true }).catch(() => undefined);
    }
  }
}

class BrowserCredentialController {
  constructor({ vault, now = () => new Date(), randomUUID = () => crypto.randomUUID() }) {
    this.vault = vault;
    this.now = now;
    this.randomUUID = randomUUID;
    this.detectedForms = new Map();
    this.pendingByTab = new Map();
  }

  async noteForm(context, detected) {
    const normalized = normalizeBrowserContext(context);
    if (detected && credentialOriginAllowed(normalized.origin)) {
      this.detectedForms.set(tabKey(normalized), normalized.origin);
    } else {
      this.detectedForms.delete(tabKey(normalized));
    }
    return this.state(normalized);
  }

  async noteCandidate(context, payload) {
    const normalized = normalizeBrowserContext(context);
    const candidate = normalizeCredential(payload);
    if (candidate.origin !== normalized.origin || !credentialOriginAllowed(candidate.origin)) {
      throw new Error("browser credential origin mismatch");
    }
    const inspected = await this.vault.inspectCandidate(candidate);
    if (inspected.ignored) {
      this.pendingByTab.delete(tabKey(normalized));
      return null;
    }
    const createdAt = this.now();
    const pending = {
      id: `pending_${this.randomUUID().replaceAll("-", "")}`,
      sessionID: normalized.sessionID,
      tabID: normalized.tabID,
      origin: candidate.origin,
      username: candidate.username,
      password: candidate.password,
      kind: inspected.kind,
      createdAt: createdAt.toISOString(),
      expiresAt: createdAt.getTime() + pendingCredentialTTLMS,
    };
    this.pendingByTab.set(tabKey(normalized), pending);
    return pendingMetadata(pending);
  }

  async state(context) {
    const normalized = normalizeBrowserContext(context);
    this.discardExpired();
    const availability = this.vault.availability();
    if (!availability.available || !credentialOriginAllowed(normalized.origin)) {
      return {
        ...availability,
        origin: normalized.origin,
        formDetected: false,
        credentials: [],
        prompt: pendingMetadata(this.pendingByTab.get(tabKey(normalized))),
      };
    }
    return {
      ...availability,
      origin: normalized.origin,
      formDetected: this.detectedForms.get(tabKey(normalized)) === normalized.origin,
      credentials: await this.vault.listForOrigin(normalized.origin),
      prompt: pendingMetadata(this.pendingByTab.get(tabKey(normalized))),
    };
  }

  async commit(context, pendingID) {
    const normalized = normalizeBrowserContext(context);
    const pending = this.pendingCandidate(normalized, pendingID);
    const saved = await this.vault.save(pending);
    this.pendingByTab.delete(tabKey(normalized));
    return saved;
  }

  async dismiss(context, pendingID, neverSave) {
    const normalized = normalizeBrowserContext(context);
    const pending = this.pendingCandidate(normalized, pendingID);
    this.pendingByTab.delete(tabKey(normalized));
    if (neverSave) {
      await this.vault.setNeverSave(pending.origin, true);
    }
  }

  async fill(context, credentialID) {
    const normalized = normalizeBrowserContext(context);
    const credential = await this.vault.secret(credentialID);
    if (credential.origin !== normalized.origin || !credentialOriginAllowed(normalized.origin)) {
      throw new Error("browser credential origin mismatch");
    }
    return credential;
  }

  release(context) {
    const sessionID = String(context?.sessionID || "").trim();
    const tabID = String(context?.tabID || "").trim();
    if (!sessionID || !tabID) return;
    const key = `${sessionID}\u0000${tabID}`;
    this.detectedForms.delete(key);
    this.pendingByTab.delete(key);
  }

  pendingCandidate(context, pendingID) {
    this.discardExpired();
    const pending = this.pendingByTab.get(tabKey(context));
    if (!pending || pending.id !== String(pendingID || "").trim()) {
      throw new Error("browser credential prompt expired");
    }
    return pending;
  }

  discardExpired() {
    const now = this.now().getTime();
    for (const [key, pending] of this.pendingByTab) {
      if (pending.expiresAt <= now) {
        this.pendingByTab.delete(key);
      }
    }
  }
}

function parseChromePasswordCSV(contents) {
  const text = String(contents || "");
  if (Buffer.byteLength(text, "utf8") > maxImportBytes) {
    throw new Error("Chrome password CSV is too large");
  }
  const rows = parseCSV(text.replace(/^\uFEFF/, ""));
  if (rows.length < 2) {
    throw new Error("Chrome password CSV is empty");
  }
  const headers = rows[0].map((header) => header.trim().toLowerCase());
  const urlIndex = headers.indexOf("url");
  const usernameIndex = headers.indexOf("username");
  const passwordIndex = headers.indexOf("password");
  if (urlIndex < 0 || usernameIndex < 0 || passwordIndex < 0) {
    throw new Error("Chrome password CSV headers are invalid");
  }
  const records = [];
  let skipped = 0;
  for (const row of rows.slice(1)) {
    const password = String(row[passwordIndex] || "");
    if (!password) {
      skipped += 1;
      continue;
    }
    try {
      records.push(normalizeCredential({
        origin: row[urlIndex],
        username: row[usernameIndex] || "",
        password,
      }));
    } catch {
      skipped += 1;
    }
  }
  if (records.length > maxCredentialCount) {
    throw new Error("Chrome password CSV contains too many entries");
  }
  return { records, skipped };
}

function parseCSV(text) {
  const rows = [];
  let row = [];
  let field = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (quoted) {
      if (character === '"' && text[index + 1] === '"') {
        field += '"';
        index += 1;
      } else if (character === '"') {
        quoted = false;
      } else {
        field += character;
      }
      continue;
    }
    if (character === '"') {
      quoted = true;
    } else if (character === ",") {
      row.push(field);
      field = "";
    } else if (character === "\n") {
      row.push(field.replace(/\r$/, ""));
      rows.push(row);
      row = [];
      field = "";
    } else {
      field += character;
    }
  }
  if (quoted) {
    throw new Error("Chrome password CSV has an unterminated field");
  }
  if (field || row.length) {
    row.push(field.replace(/\r$/, ""));
    rows.push(row);
  }
  return rows.filter((values) => values.some((value) => value !== ""));
}

function emptyVault() {
  return { version: vaultVersion, credentials: [], neverSaveOrigins: [] };
}

function parseVault(raw) {
  let parsed;
  try {
    parsed = JSON.parse(String(raw || ""));
  } catch {
    throw new Error("browser credential vault is corrupted");
  }
  if (parsed?.version !== vaultVersion || !Array.isArray(parsed.credentials) || !Array.isArray(parsed.neverSaveOrigins)) {
    throw new Error("browser credential vault format is unsupported");
  }
  const credentials = parsed.credentials.map((credential) => {
    const normalized = normalizeCredential(credential);
    return {
      id: normalizeCredentialID(credential.id),
      ...normalized,
      createdAt: normalizeTimestamp(credential.createdAt),
      updatedAt: normalizeTimestamp(credential.updatedAt),
    };
  });
  if (credentials.length > maxCredentialCount) {
    throw new Error("browser credential vault contains too many entries");
  }
  return {
    version: vaultVersion,
    credentials,
    neverSaveOrigins: [...new Set(parsed.neverSaveOrigins.map(normalizeCredentialOrigin))].sort(),
  };
}

function normalizeCredential(candidate) {
  const origin = normalizeCredentialOrigin(candidate?.origin || candidate?.url);
  const username = String(candidate?.username || "").trim().slice(0, 512);
  const password = String(candidate?.password || "");
  if (!credentialOriginAllowed(origin)) {
    throw new Error("browser credential origin is not allowed");
  }
  if (!password || password.length > 4096) {
    throw new Error("browser credential password is invalid");
  }
  return { origin, username, password };
}

function normalizeCredentialOrigin(rawOrigin) {
  let url;
  try {
    url = new URL(String(rawOrigin || "").trim());
  } catch {
    throw new Error("browser credential origin is invalid");
  }
  if (url.username || url.password) {
    throw new Error("browser credential origin is invalid");
  }
  return url.origin;
}

function credentialOriginAllowed(origin) {
  try {
    const url = new URL(origin);
    return url.protocol === "https:" || (url.protocol === "http:" && (url.hostname === "localhost" || url.hostname === "127.0.0.1" || url.hostname === "[::1]"));
  } catch {
    return false;
  }
}

function normalizeBrowserContext(context) {
  const sessionID = String(context?.sessionID || "").trim();
  const tabID = String(context?.tabID || "").trim();
  if (!sessionID || !tabID) {
    throw new Error("browser credential tab context is invalid");
  }
  return {
    sessionID,
    tabID,
    origin: normalizeCredentialOrigin(context?.url || context?.origin),
  };
}

function normalizeCredentialID(value) {
  const id = String(value || "").trim();
  if (!/^credential_[a-zA-Z0-9]+$/.test(id)) {
    throw new Error("browser credential id is invalid");
  }
  return id;
}

function normalizeTimestamp(value) {
  const date = new Date(String(value || ""));
  if (!Number.isFinite(date.getTime())) {
    throw new Error("browser credential timestamp is invalid");
  }
  return date.toISOString();
}

function credentialMetadata(credential) {
  return {
    id: credential.id,
    origin: credential.origin,
    username: credential.username,
    createdAt: credential.createdAt,
    updatedAt: credential.updatedAt,
  };
}

function pendingMetadata(pending) {
  if (!pending) {
    return null;
  }
  return {
    id: pending.id,
    origin: pending.origin,
    username: pending.username,
    kind: pending.kind,
    createdAt: pending.createdAt,
  };
}

function tabKey(context) {
  return `${context.sessionID}\u0000${context.tabID}`;
}

function assertEncryptionAvailable(availability) {
  if (!availability.available) {
    throw new Error(`browser credential storage unavailable: ${availability.reason}`);
  }
}

module.exports = {
  BrowserCredentialController,
  BrowserCredentialVault,
  credentialOriginAllowed,
  normalizeCredentialOrigin,
  parseChromePasswordCSV,
};
