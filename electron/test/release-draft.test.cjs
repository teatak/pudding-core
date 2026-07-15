const assert = require("node:assert/strict");
const test = require("node:test");

const {
  assertPublicTagTarget,
  createDraftMetadata,
  ensureVersionManifest,
  expectedAssetNames,
  validateDraftRelease,
} = require("../../scripts/release-draft.cjs");

const releaseBody = "## What's New\n\n### Added\n\n- New feature.\n";

function draft(version = "0.1.2", updateInfo = "latest-mac.yml") {
  const names = [
    `Pudding-${version}-arm64.dmg`,
    `Pudding-${version}-arm64.dmg.blockmap`,
    `Pudding-${version}-arm64.zip`,
    `Pudding-${version}-arm64.zip.blockmap`,
    `Pudding-${version}-x64.dmg`,
    `Pudding-${version}-x64.dmg.blockmap`,
    `Pudding-${version}-x64.zip`,
    `Pudding-${version}-x64.zip.blockmap`,
    updateInfo,
  ];
  return {
    tag_name: `v${version}`,
    name: `v${version}`,
    body: releaseBody,
    draft: true,
    assets: names.map((name) => ({ name, state: "uploaded", size: 10 })),
  };
}

test("accepts complete stable and preview draft releases", () => {
  assert.doesNotThrow(() => validateDraftRelease(draft(), "v0.1.2", "stable", releaseBody));
  assert.doesNotThrow(() =>
    validateDraftRelease(
      draft("0.1.3-beta.1", "beta-mac.yml"),
      "v0.1.3-beta.1",
      "preview",
      releaseBody,
    ),
  );
});

test("lists the complete channel-specific release assets", () => {
  assert.deepEqual(expectedAssetNames("v0.1.2", "stable"), [
    "Pudding-0.1.2-arm64.dmg",
    "Pudding-0.1.2-arm64.dmg.blockmap",
    "Pudding-0.1.2-arm64.zip",
    "Pudding-0.1.2-arm64.zip.blockmap",
    "Pudding-0.1.2-x64.dmg",
    "Pudding-0.1.2-x64.dmg.blockmap",
    "Pudding-0.1.2-x64.zip",
    "Pudding-0.1.2-x64.zip.blockmap",
    "latest-mac.yml",
  ]);
  assert.equal(expectedAssetNames("v0.1.3-beta.1", "preview").at(-1), "beta-mac.yml");
});

test("creates drafts through non-interactive GitHub API metadata", () => {
  assert.deepEqual(createDraftMetadata("v0.1.3", "stable", "public-commit", releaseBody), {
    tag_name: "v0.1.3",
    target_commitish: "public-commit",
    name: "v0.1.3",
    body: releaseBody,
    draft: true,
    prerelease: false,
  });
  assert.equal(
    createDraftMetadata("v0.1.4-beta.1", "preview", "public-commit", releaseBody).prerelease,
    true,
  );
});

test("rejects incomplete or already published releases", () => {
  const incomplete = draft();
  incomplete.assets.pop();
  assert.throws(
    () => validateDraftRelease(incomplete, "v0.1.2", "stable", releaseBody),
    /missing a complete/,
  );
  assert.throws(
    () => validateDraftRelease({ ...draft(), draft: false }, "v0.1.2", "stable", releaseBody),
    /not a draft/,
  );
});

test("rejects drafts with inconsistent titles or feature lists", () => {
  assert.throws(
    () => validateDraftRelease({ ...draft(), name: "0.1.2" }, "v0.1.2", "stable", releaseBody),
    /must use v0\.1\.2 as its title/,
  );
  assert.throws(
    () => validateDraftRelease({ ...draft(), body: "" }, "v0.1.2", "stable", releaseBody),
    /expected feature list/,
  );
});

test("commits a version manifest to the public repository", async (context) => {
  const originalFetch = global.fetch;
  const requests = [];
  context.after(() => {
    global.fetch = originalFetch;
  });
  global.fetch = async (url, options) => {
    requests.push({ url, options });
    if (options.method === "GET") {
      return mockResponse(404, { message: "Not Found" });
    }
    return mockResponse(201, { commit: { sha: "manifest-commit" } });
  };

  const commit = await ensureVersionManifest("v0.1.6", "{\n  \"version\": \"0.1.6\"\n}\n", "token");
  assert.equal(commit, "manifest-commit");
  assert.equal(requests.length, 2);
  assert.match(requests[0].url, /contents\/releases\/v0\.1\.6\.json\?ref=main$/);
  const body = JSON.parse(requests[1].options.body);
  assert.equal(body.message, "release: add v0.1.6 manifest");
  assert.equal(
    Buffer.from(body.content, "base64").toString("utf8"),
    "{\n  \"version\": \"0.1.6\"\n}\n",
  );
});

test("verifies the published tag points to the version manifest commit", async (context) => {
  const originalFetch = global.fetch;
  const requests = [];
  context.after(() => {
    global.fetch = originalFetch;
  });
  global.fetch = async (url, options) => {
    requests.push({ url, options });
    return mockResponse(200, {
      ref: "refs/tags/v0.1.6",
      object: { type: "commit", sha: "manifest-commit" },
    });
  };

  await assertPublicTagTarget("v0.1.6", "manifest-commit", "token");
  assert.equal(requests.length, 1);
  assert.equal(requests[0].options.method, "GET");
});

function mockResponse(status, payload) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => payload,
  };
}
