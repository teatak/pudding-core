const assert = require("node:assert/strict");
const test = require("node:test");

const { resolveBrowserFavicon } = require("../browser-favicon.cjs");

function fakeNativeImage() {
  return {
    createFromBuffer: (body) => ({
      isEmpty: () => body.length === 0,
      resize: () => ({ toPNG: () => Buffer.from("small-png") }),
    }),
  };
}

test("converts a same-origin favicon to a bounded raster data URL", async () => {
  const calls = [];
  const result = await resolveBrowserFavicon({
    url: "https://discord.com/assets/favicon.ico",
    pageURL: "https://discord.com/login",
    fetch: async (url, options) => {
      calls.push({ url, options });
      return new Response(Buffer.from("ico"), {
        status: 200,
        headers: { "content-type": "image/vnd.microsoft.icon", "content-length": "3" },
      });
    },
    nativeImage: fakeNativeImage(),
  });

  assert.equal(result, `data:image/vnd.microsoft.icon;base64,${Buffer.from("ico").toString("base64")}`);
  assert.equal(calls[0].url, "https://discord.com/assets/favicon.ico");
  assert.equal(calls[0].options.referrer, "https://discord.com/login");
});

test("rejects cross-origin and oversized favicon fetches", async () => {
  let fetchCount = 0;
  const crossOrigin = await resolveBrowserFavicon({
    url: "http://127.0.0.1/private.png",
    pageURL: "https://example.com/",
    fetch: async () => { fetchCount += 1; },
    nativeImage: fakeNativeImage(),
  });
  assert.equal(crossOrigin, "");
  assert.equal(fetchCount, 0);

  const oversized = await resolveBrowserFavicon({
    url: "https://example.com/favicon.png",
    pageURL: "https://example.com/",
    fetch: async () => new Response(Buffer.from("ignored"), {
      status: 200,
      headers: { "content-type": "image/png", "content-length": String(256 * 1024 + 1) },
    }),
    nativeImage: fakeNativeImage(),
  });
  assert.equal(oversized, "");
});

test("compresses large valid favicon images before publishing", async () => {
  const result = await resolveBrowserFavicon({
    url: "https://example.com/favicon.png",
    pageURL: "https://example.com/",
    fetch: async () => new Response(Buffer.alloc(70 * 1024, 1), {
      status: 200,
      headers: { "content-type": "image/png" },
    }),
    nativeImage: fakeNativeImage(),
  });
  assert.equal(result, `data:image/png;base64,${Buffer.from("small-png").toString("base64")}`);
});
