const fs = require("node:fs/promises");
const path = require("node:path");
const { fileURLToPath, pathToFileURL } = require("node:url");

function createLocalFileBrowserOpener({ host, requestAPI, confirm }) {
  // Coalesce concurrent clicks, not permissions. The live tab owns its grant.
  const pending = new Map();
  return async (owner, request) => {
    const sessionID = String(request?.sessionID || "").trim();
    const raw = request?.url;
    if (!sessionID || typeof raw !== "string") return { ok: false, reason: "invalid" };
    const key = JSON.stringify([sessionID, raw]);
    if (pending.has(key)) return pending.get(key);
    const work = open(owner, sessionID, raw).catch(error => ({
      ok: false,
      reason: error.code === "ENOENT" ? "not_found" : ["EACCES", "EPERM"].includes(error.code) ? "denied" : "failed",
    }));
    pending.set(key, work);
    try { return await work; } finally { pending.delete(key); }
  };

  async function resolve(sessionID, raw) {
    if (/[\u0000-\u001f\u007f\\]/.test(raw) || !/^file:\/\/(?:localhost)?\//i.test(raw)) throw new Error("invalid file URL");
    const url = new URL(raw);
    if (url.pathname.startsWith("//") || /%2f|%5c/i.test(url.pathname) || (url.hostname && url.hostname !== "localhost")) throw new Error("invalid file URL");
    const filename = fileURLToPath(url);
    if (/[\u0000-\u001f\u007f]/.test(filename + decodeURIComponent(url.hash))) throw new Error("invalid file URL");
    const target = await fs.realpath(filename);
    if (!(await fs.stat(target)).isFile()) throw new Error("not a regular file");
    const session = await requestAPI(`/sessions/${encodeURIComponent(sessionID)}`);
    let projectRoot = "";
    if (session.projectID) {
      const project = await requestAPI(`/projects/${encodeURIComponent(session.projectID)}`);
      for (const root of project.rootDirs) {
        const realRoot = await fs.realpath(root);
        const relative = path.relative(realRoot, target);
        if (relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative)) {
          projectRoot = realRoot;
          break;
        }
      }
    }
    const canonical = pathToFileURL(target);
    canonical.search = url.search;
    canonical.hash = url.hash;
    return { url: canonical.href, path: target, projectRoot };
  }

  async function open(owner, sessionID, raw) {
    const target = await resolve(sessionID, raw);
    const existing = host.listTabs({ sessionID }).tabs.find(tab => tab.url === target.url);
    if (existing) return { ok: true, tab: existing };
    if (!target.projectRoot && !(await confirm(owner, target.path))) return { ok: false, cancelled: true };
    if (owner.isDestroyed()) return { ok: false, cancelled: true };
    // A symlink, session or project may have changed while the dialog was open.
    const current = await resolve(sessionID, raw);
    if (current.path !== target.path || current.projectRoot !== target.projectRoot) return { ok: false, reason: "changed" };
    const route = `/sessions/${encodeURIComponent(sessionID)}/browser/tabs`;
    const tab = await requestAPI(route, "POST");
    try {
      const snapshot = await host.ensure({
        sessionID, tabID: tab.id, url: target.url,
        _fileAuthorized: true,
        // A regular-file scope grants only this file, never its parent directory.
        fileRoot: target.projectRoot || { file: target.path },
      });
      return { ok: true, tab: snapshot };
    } catch (error) {
      await requestAPI(`${route}/${encodeURIComponent(tab.id)}/release`, "POST");
      throw error;
    }
  }
}

module.exports = { createLocalFileBrowserOpener };
