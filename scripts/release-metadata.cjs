const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

function releaseReportPath(projectRoot, tag) {
  const normalizedTag = String(tag || "").trim();
  if (!normalizedTag.startsWith("v") || normalizedTag.length === 1) {
    throw new Error(`invalid release tag: ${normalizedTag || "<empty>"}`);
  }
  return path.join(projectRoot, "docs", `release-report-${normalizedTag.slice(1)}.md`);
}

function extractReleaseNotes(markdown) {
  const lines = String(markdown || "").split(/\r?\n/);
  const start = lines.findIndex((line) => /^##\s+Release Notes(?:\s+草案)?\s*$/.test(line));
  if (start === -1) {
    throw new Error("release report is missing a '## Release Notes 草案' section");
  }

  let end = lines.length;
  for (let index = start + 1; index < lines.length; index += 1) {
    if (/^##\s+/.test(lines[index])) {
      end = index;
      break;
    }
  }

  const notes = lines.slice(start + 1, end).join("\n").trim();
  if (!notes || !/^###\s+\S+/m.test(notes)) {
    throw new Error("release notes must contain at least one non-empty feature group");
  }
  return notes;
}

function readReleaseNotes(projectRoot, tag) {
  const reportPath = releaseReportPath(projectRoot, tag);
  if (!fs.existsSync(reportPath)) {
    throw new Error(`release report is missing: ${reportPath}`);
  }
  const markdown = fs.readFileSync(reportPath, "utf8");
  return extractReleaseNotes(markdown);
}

function buildReleaseBody(releaseNotes) {
  return `## 功能清单\n\n${String(releaseNotes || "").trim()}\n`;
}

function describeReleaseAssets(assets) {
  return assets.map(({ name, filePath, size }) => ({
    name,
    size: Number(size),
    sha256: sha256File(filePath),
  }));
}

function buildVersionManifest({ tag, channel, sourceCommit, releaseNotes, assets }) {
  return {
    formatVersion: 1,
    version: tag.slice(1),
    tag,
    channel,
    source: {
      repository: "teatak/pudding-core",
      tag,
      commit: sourceCommit,
    },
    releaseNotes,
    assets: assets.map(({ name, size, sha256 }) => ({ name, size, sha256 })),
  };
}

function serializeVersionManifest(manifest) {
  return `${JSON.stringify(manifest, null, 2)}\n`;
}

function versionManifestPath(tag) {
  return `releases/${tag}.json`;
}

function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  const descriptor = fs.openSync(filePath, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    let bytesRead = 0;
    do {
      bytesRead = fs.readSync(descriptor, buffer, 0, buffer.length, null);
      if (bytesRead > 0) {
        hash.update(buffer.subarray(0, bytesRead));
      }
    } while (bytesRead > 0);
  } finally {
    fs.closeSync(descriptor);
  }
  return hash.digest("hex");
}

module.exports = {
  buildReleaseBody,
  buildVersionManifest,
  describeReleaseAssets,
  extractReleaseNotes,
  readReleaseNotes,
  releaseReportPath,
  serializeVersionManifest,
  versionManifestPath,
};
