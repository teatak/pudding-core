const { spawn } = require("node:child_process");

// A 640×480 RGBA PNG can exceed 1 MiB after base64 encoding (~1.6 MiB).
const maximumFrameBytes = 2 * 1024 * 1024;
const idleTimeoutMs = 30_000;

// Native capture is owned here. Renderers only subscribe to engine-authorized targets.
class ComputerUsePreview {
  constructor({ binaryPath, host, spawnProcess = spawn }) {
    this.binaryPath = binaryPath;
    this.host = host;
    this.spawnProcess = spawnProcess;
    this.clients = new Map();
    this.entries = new Map();
    this.owners = new WeakSet();
    this.version = 0;
  }

  subscribe(owner, { sessionID, turnID, visible } = {}) {
    if (!validID(sessionID) || (turnID && !validID(turnID))) throw new Error("Invalid preview session or turn");
    const key = `${owner.id}:${sessionID}`;
    const previous = this.clients.get(key);
    if (!this.owners.has(owner)) {
      this.owners.add(owner);
      owner.once("destroyed", () => this.releaseOwner(owner));
      owner.on("did-start-navigation", (_event, _url, _inPlace, mainFrame) => {
        if (mainFrame) this.releaseOwner(owner);
      });
    }
    if (!turnID) {
      this.clients.delete(key);
    } else {
      this.clients.set(key, { owner, sessionID, turnID, visible: visible === true, waiting: 0, sent: new Map() });
    }
    for (const [entryKey, entry] of this.entries) {
      if (entry.target.sessionID !== sessionID) continue;
      this.reconcile(entry);
      if (!turnID && previous?.turnID === entry.target.turnID
        && ![...this.clients.values()].some(client => client.sessionID === sessionID)) {
        this.remove(entryKey, entry);
      }
    }
    const client = this.clients.get(key);
    if (client) this.deliverNext(client);
  }

  noteActivity(target) {
    if (!validID(target.sessionID) || !validID(target.turnID)
      || ![...this.clients.values()].some(client => client.sessionID === target.sessionID && client.turnID === target.turnID)) return;
    for (const [key, old] of this.entries) {
      if (old.target.sessionID === target.sessionID && old.target.turnID !== target.turnID) this.remove(key, old);
    }
    const key = JSON.stringify([target.sessionID, target.appID]);
    let entry = this.entries.get(key);
    if (!entry || entry.target.windowID !== target.windowID || entry.expiresAt <= Date.now()) {
      if (entry) this.remove(key, entry);
      entry = { target: { ...target }, child: null, frame: null, error: false, timer: null };
      this.entries.set(key, entry);
    }
    entry.version = ++this.version;
    entry.activityVersion = entry.version;
    entry.expiresAt = Date.now() + idleTimeoutMs;
    clearTimeout(entry.timer);
    entry.timer = setTimeout(() => this.invalidate(entry), idleTimeoutMs);
    entry.timer.unref();
    this.reconcile(entry);
    this.publish();
  }

  reconcile(entry) {
    const needed = [...this.clients.values()].some(client => client.visible
      && client.sessionID === entry.target.sessionID && client.turnID === entry.target.turnID);
    if (!needed) {
      this.stopCapture(entry);
      entry.frame = null;
      return;
    }
    if (entry.child || entry.error) return;
    const child = this.spawnProcess(this.binaryPath, ["preview"], { stdio: ["pipe", "pipe", "pipe"], windowsHide: true });
    entry.child = child;
    let buffer = "";
    const fail = () => {
      if (entry.child !== child) return;
      this.invalidate(entry);
    };
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", chunk => {
      if (entry.child !== child) return;
      buffer += chunk;
      if (Buffer.byteLength(buffer) > maximumFrameBytes) { fail(); return; }
      let index;
      while ((index = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, index);
        buffer = buffer.slice(index + 1);
        let frame;
        try { frame = JSON.parse(line); } catch { fail(); return; }
        if (frame.type !== "frame" || !Number.isInteger(frame.pid) || frame.pid <= 0
          || frame.windowID !== entry.target.windowID || (entry.target.pid && frame.pid !== entry.target.pid)
          || !Number.isInteger(frame.width) || frame.width < 1 || frame.width > 640
          || !Number.isInteger(frame.height) || frame.height < 1 || frame.height > 480
          || typeof frame.png !== "string" || !frame.png || !/^[A-Za-z0-9+/=]+$/.test(frame.png)
          || typeof frame.name !== "string" || typeof frame.title !== "string") { fail(); return; }
        entry.target.pid = frame.pid;
        entry.frame = { imageURL: `data:image/png;base64,${frame.png}`, name: frame.name, title: frame.title, width: frame.width, height: frame.height };
        entry.version = ++this.version;
        this.publish();
      }
    });
    child.on("error", fail);
    child.on("exit", fail);
    child.stderr.resume();
    child.stdin.on("error", fail);
    child.stdin.write(JSON.stringify({ bundleID: entry.target.appID, windowID: entry.target.windowID, pid: entry.target.pid }) + "\n");
  }

  publish() {
    for (const client of this.clients.values()) this.deliverNext(client);
  }

  invalidate(entry) {
    // Retain a frameless tombstone until release so a backpressured renderer
    // still receives the removal after acknowledging its outstanding frame.
    clearTimeout(entry.timer);
    entry.timer = null;
    entry.error = true;
    entry.frame = null;
    this.stopCapture(entry);
    entry.version = ++this.version;
    this.publish();
  }

  deliverNext(client) {
    // One outstanding packet per renderer, including when several apps produce
    // frames. Send the oldest pending version first so no app starves another.
    for (const entry of [...this.entries.values()].sort((a, b) => a.version - b.version)) this.deliver(entry, client);
  }

  deliver(entry, client) {
    if (client.sessionID !== entry.target.sessionID || client.turnID !== entry.target.turnID
      || !client.visible || client.waiting || client.sent.get(entry.target.appID) === entry.version || client.owner.isDestroyed()) return;
    client.waiting = entry.version;
    client.sent.set(entry.target.appID, entry.version);
    client.owner.send("pudding:desktop:computer-preview", {
      ...entry.target, ...entry.frame, version: entry.version, status: entry.error ? "unavailable" : entry.frame ? "live" : "loading",
      activityVersion: entry.activityVersion, expiresAt: entry.expiresAt,
    });
  }

  acknowledge(owner, { sessionID, turnID, version } = {}) {
    const client = this.clients.get(`${owner.id}:${sessionID}`);
    if (!client || client.turnID !== turnID || client.waiting !== version) return;
    client.waiting = 0;
    this.deliverNext(client);
  }

  async reveal(owner, { sessionID, turnID, windowID } = {}) {
    const client = this.clients.get(`${owner.id}:${sessionID}`);
    const entry = [...this.entries.values()].find(entry => entry.target.sessionID === sessionID
      && entry.target.turnID === turnID && entry.target.windowID === windowID);
    if (!client?.visible || client.turnID !== turnID || entry?.target.turnID !== turnID
      || entry.error || !entry.frame || !entry.target.pid || entry.expiresAt <= Date.now()) return false;
    await this.host.request("reveal_window", {
      bundleID: entry.target.appID, windowID, pid: entry.target.pid,
    });
    return true;
  }

  stopCapture(entry) {
    const child = entry.child;
    entry.child = null;
    if (!child) return;
    child.stdin.end();
    const timeout = setTimeout(() => child.kill(), 750);
    timeout.unref();
    child.once("exit", () => clearTimeout(timeout));
  }

  releaseOwner(owner) {
    for (const [key, client] of this.clients) {
      if (client.owner === owner) this.clients.delete(key);
    }
    for (const [key, entry] of this.entries) {
      this.reconcile(entry);
      if (![...this.clients.values()].some(client => client.sessionID === entry.target.sessionID)) this.remove(key, entry);
    }
  }

  remove(key, entry) {
    clearTimeout(entry.timer);
    this.stopCapture(entry);
    this.entries.delete(key);
  }

  stop() {
    for (const [key, entry] of this.entries) this.remove(key, entry);
    this.clients.clear();
  }
}

function validID(value) { return typeof value === "string" && value.length > 0 && value.length <= 128; }
module.exports = { ComputerUsePreview };
