const fs = require("node:fs");
const path = require("node:path");

const projectFileChangedChannel = "pudding:project-file:changed";
const maxSubscriptionsPerRenderer = 32;
const ignoredProjectDirectoryNames = new Set([
  ".cache", ".git", ".hg", ".next", ".pytest_cache", ".svn", ".turbo",
  "__pycache__", "build", "dist", "node_modules",
]);

class ProjectFileWatcher {
  constructor(options = {}) {
    this.debounceMs = options.debounceMs ?? 150;
    this.watchDirectory = options.watchDirectory || fs.watch;
    this.subscriptions = new Map();
    this.boundSenders = new Map();
  }

  subscribe(sender, rawRequest) {
    const request = normalizeWatchRequest(rawRequest);
    this.remove(sender.id, request.id);
    let entries = this.subscriptions.get(sender.id);
    if (entries && entries.size >= maxSubscriptionsPerRenderer) {
      throw new Error("too many project file subscriptions");
    }

    const directory = request.kind === "directory" ? request.path : path.dirname(request.path);
    const filename = request.kind === "file" ? path.basename(request.path) : "";
    const options = {
      encoding: "utf8",
      persistent: false,
      ...(request.kind === "directory" ? { recursive: true } : {}),
    };
    let entry;
    const watcher = this.watchDirectory(directory, options, (eventType, changedName) => {
      if (!entry || (request.kind === "file" && !matchesFilename(changedName, filename))) {
        return;
      }
      if (request.kind === "directory" && ignoredProjectChange(changedName)) {
        return;
      }
      this.schedule(entry, eventType, changedName);
    });
    entry = { id: request.id, kind: request.kind, path: request.path, sender, timer: null, watcher };
    if (!entries) {
      entries = new Map();
      this.subscriptions.set(sender.id, entries);
    }
    entries.set(request.id, entry);
    watcher.on?.("error", () => this.remove(sender.id, request.id));
    this.bindSender(sender);
    return { id: request.id, path: request.path };
  }

  unsubscribe(sender, rawRequest) {
    const id = normalizeSubscriptionID(rawRequest?.id);
    return this.remove(sender.id, id);
  }

  schedule(entry, eventType, changedName) {
    if (entry.timer) {
      clearTimeout(entry.timer);
    }
    entry.eventType = eventType === "rename" ? "rename" : "change";
    entry.changedPath = entry.kind === "directory" && changedName
      ? path.join(entry.path, String(changedName))
      : undefined;
    entry.timer = setTimeout(() => {
      entry.timer = null;
      const current = this.subscriptions.get(entry.sender.id)?.get(entry.id);
      if (current !== entry) {
        return;
      }
      if (entry.sender.isDestroyed()) {
        this.closeSender(entry.sender.id);
        return;
      }
      try {
        entry.sender.send(projectFileChangedChannel, {
          eventType: entry.eventType,
          id: entry.id,
          path: entry.path,
          ...(entry.changedPath ? { changedPath: entry.changedPath } : {}),
        });
      } catch {
        this.closeSender(entry.sender.id);
      }
    }, this.debounceMs);
  }

  bindSender(sender) {
    if (this.boundSenders.has(sender.id)) {
      return;
    }
    const onDestroyed = () => this.closeSender(sender.id);
    const onNavigation = (_event, _url, isInPlace, isMainFrame) => {
      if (isMainFrame && !isInPlace) {
        this.closeSender(sender.id);
      }
    };
    this.boundSenders.set(sender.id, { onDestroyed, onNavigation, sender });
    sender.once("destroyed", onDestroyed);
    sender.on("did-start-navigation", onNavigation);
  }

  unbindSender(senderID) {
    const binding = this.boundSenders.get(senderID);
    if (!binding) {
      return;
    }
    binding.sender.removeListener("destroyed", binding.onDestroyed);
    binding.sender.removeListener("did-start-navigation", binding.onNavigation);
    this.boundSenders.delete(senderID);
  }

  remove(senderID, id) {
    const entries = this.subscriptions.get(senderID);
    const entry = entries?.get(id);
    if (!entry) {
      return false;
    }
    if (entry.timer) {
      clearTimeout(entry.timer);
    }
    entry.watcher.close();
    entries.delete(id);
    if (entries.size === 0) {
      this.subscriptions.delete(senderID);
      this.unbindSender(senderID);
    }
    return true;
  }

  closeSender(senderID) {
    const entries = this.subscriptions.get(senderID);
    if (entries) {
      for (const id of Array.from(entries.keys())) {
        this.remove(senderID, id);
      }
    }
    this.unbindSender(senderID);
  }

  closeAll() {
    for (const senderID of Array.from(this.subscriptions.keys())) {
      this.closeSender(senderID);
    }
  }
}

function normalizeWatchRequest(rawRequest) {
  const id = normalizeSubscriptionID(rawRequest?.id);
  const filePath = String(rawRequest?.path || "").trim();
  const kind = rawRequest?.kind === "directory" ? "directory" : "file";
  if (!filePath || filePath.length > 4096 || filePath.includes("\0") || !path.isAbsolute(filePath)) {
    throw new Error("invalid project watch path");
  }
  return { id, kind, path: path.normalize(filePath) };
}

function normalizeSubscriptionID(rawID) {
  const id = String(rawID || "").trim();
  if (!id || id.length > 200) {
    throw new Error("invalid project file subscription id");
  }
  return id;
}

function matchesFilename(rawName, expected) {
  if (rawName == null || String(rawName) === "") {
    return true;
  }
  return path.basename(String(rawName)) === expected;
}

function ignoredProjectChange(rawName) {
  if (rawName == null || String(rawName) === "") {
    return false;
  }
  return String(rawName)
    .split(/[\\/]+/)
    .filter(Boolean)
    .some((segment) => segment.startsWith(".") || ignoredProjectDirectoryNames.has(segment));
}

module.exports = { ProjectFileWatcher, projectFileChangedChannel };
