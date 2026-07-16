const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const test = require("node:test");

const { daemonProtocolVersion, probePuddingDaemon } = require("../daemon-health.cjs");

test("daemon probe requires authenticated pudding identity", async () => {
  let request;
  const fetchImpl = async (url, options) => {
    request = { url, options };
    const challenge = new URL(url).searchParams.get("challenge");
    return {
      ok: true,
      async json() {
        return {
          service: "puddingd",
          protocolVersion: daemonProtocolVersion,
          proof: crypto.createHmac("sha256", "token").update(challenge).digest("hex"),
        };
      },
    };
  };

  assert.equal(await probePuddingDaemon("http://127.0.0.1:9669/", " token ", { fetchImpl }), true);
  assert.match(request.url, /^http:\/\/127\.0\.0\.1:9669\/desktop\/health\?challenge=[a-f0-9]{64}$/);
  assert.equal(request.options.headers, undefined);
  assert.equal(request.options.redirect, "error");
});

test("daemon probe rejects arbitrary listeners and protocol mismatches", async () => {
  const response = (payload, ok = true) => async () => ({ ok, json: async () => payload });

  assert.equal(
    await probePuddingDaemon("http://127.0.0.1:9669", "token", {
      fetchImpl: response({ service: "other", protocolVersion: daemonProtocolVersion, proof: "00" }),
    }),
    false,
  );
  assert.equal(
    await probePuddingDaemon("http://127.0.0.1:9669", "token", {
      fetchImpl: response({ service: "puddingd", protocolVersion: daemonProtocolVersion + 1, proof: "00" }),
    }),
    false,
  );
  assert.equal(
    await probePuddingDaemon("http://127.0.0.1:9669", "token", {
      fetchImpl: response({ service: "puddingd", protocolVersion: daemonProtocolVersion, proof: "00" }),
    }),
    false,
  );
  assert.equal(
    await probePuddingDaemon("http://127.0.0.1:9669", "token", { fetchImpl: response({}, false) }),
    false,
  );
});

test("daemon probe treats timeout as unavailable", async () => {
  const fetchImpl = (_url, options) =>
    new Promise((_resolve, reject) => {
      options.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    });
  assert.equal(
    await probePuddingDaemon("http://127.0.0.1:9669", "token", { fetchImpl, timeoutMs: 5 }),
    false,
  );
});
