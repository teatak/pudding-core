const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  buildReleaseBody,
  buildVersionManifest,
  describeReleaseAssets,
  extractReleaseNotes,
  serializeVersionManifest,
  versionManifestPath,
} = require("../../scripts/release-metadata.cjs");

test("extracts the release feature groups from a release report", () => {
  const notes = extractReleaseNotes(`
# Pudding 0.1.6 发版报告

## 改动摘要

- Internal detail.

## Release Notes 草案

### 新增

- 新功能。

### 改进

- 更稳定。
`);
  assert.equal(notes, "### 新增\n\n- 新功能。\n\n### 改进\n\n- 更稳定。");
  assert.equal(
    buildReleaseBody(notes),
    "## 功能清单\n\n### 新增\n\n- 新功能。\n\n### 改进\n\n- 更稳定。\n",
  );
});

test("rejects release reports without a grouped feature list", () => {
  assert.throws(() => extractReleaseNotes("## 改动摘要\n\n- Change"), /missing/);
  assert.throws(() => extractReleaseNotes("## Release Notes 草案\n\n- Change"), /feature group/);
});

test("builds a deterministic public version manifest with asset hashes", (context) => {
  const tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-release-metadata-"));
  context.after(() => fs.rmSync(tempDirectory, { recursive: true, force: true }));
  const assetPath = path.join(tempDirectory, "Pudding-0.1.6-arm64.zip");
  fs.writeFileSync(assetPath, "pudding");
  const assets = describeReleaseAssets([
    { name: "Pudding-0.1.6-arm64.zip", filePath: assetPath, size: 7 },
  ]);
  const manifest = buildVersionManifest({
    tag: "v0.1.6",
    channel: "stable",
    sourceCommit: "abc123",
    releaseNotes: "### 新增\n\n- 新功能。",
    assets,
  });

  assert.equal(versionManifestPath("v0.1.6"), "releases/v0.1.6.json");
  assert.equal(manifest.source.commit, "abc123");
  assert.equal(manifest.assets[0].sha256.length, 64);
  assert.equal(serializeVersionManifest(manifest), serializeVersionManifest(manifest));
});
