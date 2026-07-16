const crypto = require("node:crypto");

const daemonProtocolVersion = 1;

async function probePuddingDaemon(apiBase, token, options = {}) {
  const cleanToken = String(token || "").trim();
  if (!cleanToken) {
    return false;
  }
  const fetchImpl = options.fetchImpl || globalThis.fetch;
  const timeoutMs = options.timeoutMs || 750;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const challenge = crypto.randomBytes(32).toString("hex");
    const url = new URL(`${String(apiBase || "").replace(/\/+$/, "")}/desktop/health`);
    url.searchParams.set("challenge", challenge);
    const response = await fetchImpl(url.toString(), {
      redirect: "error",
      signal: controller.signal,
    });
    if (!response.ok) {
      return false;
    }
    const payload = await response.json();
    if (payload?.service !== "puddingd" || payload?.protocolVersion !== daemonProtocolVersion) {
      return false;
    }
    const expected = crypto.createHmac("sha256", cleanToken).update(challenge).digest();
    const actual = Buffer.from(String(payload?.proof || ""), "hex");
    return actual.length === expected.length && crypto.timingSafeEqual(actual, expected);
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

module.exports = { daemonProtocolVersion, probePuddingDaemon };
