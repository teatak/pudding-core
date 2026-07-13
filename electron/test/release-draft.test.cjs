const assert = require("node:assert/strict");
const test = require("node:test");

const {
  createDraftMetadata,
  expectedAssetNames,
  validateDraftRelease,
} = require("../../scripts/release-draft.cjs");

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
    draft: true,
    assets: names.map((name) => ({ name, state: "uploaded", size: 10 })),
  };
}

test("accepts complete stable and preview draft releases", () => {
  assert.doesNotThrow(() => validateDraftRelease(draft(), "v0.1.2", "stable"));
  assert.doesNotThrow(() =>
    validateDraftRelease(draft("0.1.3-beta.1", "beta-mac.yml"), "v0.1.3-beta.1", "preview"),
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
  assert.deepEqual(createDraftMetadata("v0.1.3", "stable"), {
    tag_name: "v0.1.3",
    name: "0.1.3",
    body: "",
    draft: true,
    prerelease: false,
  });
  assert.equal(createDraftMetadata("v0.1.4-beta.1", "preview").prerelease, true);
});

test("rejects incomplete or already published releases", () => {
  const incomplete = draft();
  incomplete.assets.pop();
  assert.throws(() => validateDraftRelease(incomplete, "v0.1.2", "stable"), /missing a complete/);
  assert.throws(
    () => validateDraftRelease({ ...draft(), draft: false }, "v0.1.2", "stable"),
    /not a draft/,
  );
});
