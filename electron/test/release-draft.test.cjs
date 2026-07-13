const assert = require("node:assert/strict");
const test = require("node:test");

const { validateDraftRelease } = require("../../scripts/release-draft.cjs");

function draft(version = "0.1.2", updateInfo = "latest-mac.yml") {
  const names = [
    `Pudding-${version}-arm64.dmg`,
    `Pudding-${version}-arm64.dmg.blockmap`,
    `Pudding-${version}-arm64.zip`,
    `Pudding-${version}-arm64.zip.blockmap`,
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

test("rejects incomplete or already published releases", () => {
  const incomplete = draft();
  incomplete.assets.pop();
  assert.throws(() => validateDraftRelease(incomplete, "v0.1.2", "stable"), /missing a complete/);
  assert.throws(
    () => validateDraftRelease({ ...draft(), draft: false }, "v0.1.2", "stable"),
    /not a draft/,
  );
});
