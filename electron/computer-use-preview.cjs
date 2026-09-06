const { spawn } = require("node:child_process");

const maximumFrameBytes = 1024 * 1024;

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
      this.clients.set(key, { owner, sessionID, turnID, visible: visible === true, waiting: false, sent: 0 });
    }
    const entry = this.entries.get(sessionID);
    if (!entry) return;
    this.reconcile(entry);
    if (!turnID && previous?.turnID === entry.target.turnID
      && ![...this.clients.values()].some(client => client.sessionID === sessionID)) {
      this.entries.delete(sessionID);
    }
    const client = this.clients.get(key);
    if (client) this.deliver(entry, client);
  }

  noteActivity(target) {
    if (!validID(target.sessionID) || !validID(target.turnID)
      || ![...this.clients.values()].some(client => client.sessionID === target.sessionID && client.turnID === target.turnID)) return;
    let entry = this.entries.get(target.sessionID);
    if (!entry || entry.target.turnID !== target.turnID || entry.target.appID !== target.appID
      || entry.target.windowID !== target.windowID) {
      if (entry) this.stopCapture(entry);
      entry = { target: { ...target }, version: ++this.version, child: null, frame: null, error: false };
      this.entries.set(target.sessionID, entry);
      for (const client of this.clients.values()) {
        if (client.sessionID === target.sessionID) client.waiting = false;
      }
    }
    this.reconcile(entry);
    this.publish(entry);
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
      entry.error = true;
      entry.frame = null;
      this.stopCapture(entry);
      entry.version = ++this.version;
      this.publish(entry);
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
          || typeof frame.jpeg !== "string" || !frame.jpeg || !/^[A-Za-z0-9+/=]+$/.test(frame.jpeg)
          || typeof frame.name !== "string" || typeof frame.title !== "string") { fail(); return; }
        entry.target.pid = frame.pid;
        entry.frame = { imageURL: `data:image/jpeg;base64,${frame.jpeg}`, name: frame.name, title: frame.title, width: frame.width, height: frame.height };
        entry.version = ++this.version;
        this.publish(entry);
      }
    });
    child.on("error", fail);
    child.on("exit", fail);
    child.stderr.resume();
    child.stdin.on("error", fail);
    child.stdin.write(JSON.stringify({ bundleID: entry.target.appID, windowID: entry.target.windowID, pid: entry.target.pid }) + "\n");
  }

  publish(entry) {
    for (const client of this.clients.values()) this.deliver(entry, client);
  }

  deliver(entry, client) {
    if (client.sessionID !== entry.target.sessionID || client.turnID !== entry.target.turnID
      || !client.visible || client.waiting || client.sent === entry.version || client.owner.isDestroyed()) return;
    client.waiting = true;
    client.sent = entry.version;
    client.owner.send("pudding:desktop:computer-preview", {
      ...entry.target, ...entry.frame, version: entry.version, status: entry.error ? "unavailable" : entry.frame ? "live" : "loading",
    });
  }

  acknowledge(owner, { sessionID, turnID, version } = {}) {
    const client = this.clients.get(`${owner.id}:${sessionID}`);
    if (!client || client.turnID !== turnID || client.sent !== version) return;
    client.waiting = false;
    const entry = this.entries.get(sessionID);
    if (entry) this.deliver(entry, client);
  }

  async reveal(owner, { sessionID, turnID, windowID } = {}) {
    const client = this.clients.get(`${owner.id}:${sessionID}`);
    const entry = this.entries.get(sessionID);
    if (!client?.visible || client.turnID !== turnID || entry?.target.turnID !== turnID
      || entry.target.windowID !== windowID || !entry.target.pid) return false;
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
    for (const [sessionID, entry] of this.entries) {
      this.reconcile(entry);
      if (![...this.clients.values()].some(client => client.sessionID === sessionID)) this.entries.delete(sessionID);
    }
  }

  stop() {
    for (const entry of this.entries.values()) this.stopCapture(entry);
    this.entries.clear();
    this.clients.clear();
  }
}

function validID(value) { return typeof value === "string" && value.length > 0 && value.length <= 128; }
module.exports = { ComputerUsePreview };
