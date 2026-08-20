const assert = require("node:assert/strict");
const http = require("node:http");
const test = require("node:test");

const { MobileAccessBridge, preferredLANHost } = require("../mobile-access-bridge.cjs");

test("preferredLANHost prioritizes the primary ethernet interface", () => {
  assert.equal(preferredLANHost({
    utun3: [{ address: "10.0.0.8", family: "IPv4", internal: false }],
    en0: [{ address: "192.168.1.23", family: "IPv4", internal: false }],
  }), "192.168.1.23");
});

test("createPairing asks the loopback daemon for a LAN pairing URL", async (t) => {
  const upstream = http.createServer((request, response) => {
    assert.equal(request.url, "/mobile/pairings");
    assert.equal(request.headers.authorization, "Bearer desktop-token");
    response.writeHead(201, { "Content-Type": "application/json" });
    response.end(JSON.stringify({
      code: "pair-code",
      url: `http://${request.headers.host}/?pairing=pair-code`,
      urls: [`http://${request.headers.host}/?pairing=pair-code`],
      expiresAt: "2026-08-20T12:00:00Z",
    }));
  });
  await listen(upstream);
  t.after(() => close(upstream));
  const address = upstream.address();
  const upstreamURL = `http://127.0.0.1:${address.port}`;
  const bridge = new MobileAccessBridge({
    apiBase: upstreamURL,
    webBase: upstreamURL,
    networkInterfaces: () => ({
      en0: [{ address: "192.168.1.23", family: "IPv4", internal: false }],
    }),
  });
  t.after(() => bridge.stop());

  const pairing = await bridge.createPairing("desktop-token");

  assert.match(pairing.url, /^http:\/\/192\.168\.1\.23:\d+\/\?pairing=pair-code$/);
});

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
}

function close(server) {
  return new Promise((resolve) => server.close(resolve));
}
