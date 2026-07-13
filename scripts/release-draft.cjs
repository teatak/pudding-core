#!/usr/bin/env node

const { spawnSync } = require("node:child_process");

const packageMetadata = require("../package.json");
const { inferReleaseChannelFromTag } = require("./release-gate.cjs");

const repository = "teatak/pudding";

if (require.main === module) {
  main(process.argv.slice(2), process.env).catch((error) => {
    console.error(`Release draft command failed: ${error.message}`);
    process.exitCode = 1;
  });
}

async function main(argv, env) {
  const [command, requestedTag] = argv;
  if (command !== "status" && command !== "publish") {
    throw new Error("usage: release-draft.cjs <status|publish> [vX.Y.Z[-beta.N]]");
  }

  const tag = String(requestedTag || `v${packageMetadata.version}`).trim();
  const channel = inferReleaseChannelFromTag(tag);
  const token = resolveGitHubToken(env);
  const releases = await githubRequest("GET", `/repos/${repository}/releases?per_page=100`, token);
  const release = releases.find((candidate) => candidate.tag_name === tag);
  if (!release) {
    throw new Error(`draft ${tag} is not ready; signing or notarization may still be running`);
  }
  if (!release.draft) {
    if (command === "status") {
      console.log(`Release is already published: ${tag} ${release.html_url}`);
      return;
    }
    throw new Error(`release ${tag} is already published`);
  }

  validateDraftRelease(release, tag, channel);
  if (command === "status") {
    console.log(`Draft release is ready: ${tag} assets=${release.assets.length} ${release.html_url}`);
    return;
  }

  const published = await githubRequest(
    "PATCH",
    `/repos/${repository}/releases/${release.id}`,
    token,
    {
      draft: false,
      prerelease: channel === "preview",
      make_latest: channel === "stable" ? "true" : "false",
    },
  );
  console.log(`Release published: ${tag} ${published.html_url}`);
}

function validateDraftRelease(release, tag, channel) {
  if (release.tag_name !== tag || release.draft !== true) {
    throw new Error(`release ${tag} is not a draft`);
  }

  const version = tag.slice(1);
  const updateInfo = channel === "preview" ? "beta-mac.yml" : "latest-mac.yml";
  const expectedAssets = [
    `Pudding-${version}-arm64.dmg`,
    `Pudding-${version}-arm64.dmg.blockmap`,
    `Pudding-${version}-arm64.zip`,
    `Pudding-${version}-arm64.zip.blockmap`,
    updateInfo,
  ];
  const assets = new Map((release.assets || []).map((asset) => [asset.name, asset]));
  for (const name of expectedAssets) {
    const asset = assets.get(name);
    if (!asset || asset.state !== "uploaded" || Number(asset.size) <= 0) {
      throw new Error(`draft ${tag} is missing a complete ${name} asset`);
    }
  }
}

function resolveGitHubToken(env) {
  const fromEnvironment = String(env.GH_TOKEN || "").trim();
  if (fromEnvironment) {
    return fromEnvironment;
  }
  const result = spawnSync("gh", ["auth", "token"], { encoding: "utf8" });
  const token = String(result.stdout || "").trim();
  if (result.status !== 0 || !token) {
    throw new Error("GH_TOKEN or an authenticated gh CLI is required");
  }
  return token;
}

async function githubRequest(method, endpoint, token, body) {
  const response = await fetch(`https://api.github.com${endpoint}`, {
    method,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "User-Agent": "pudding-release-draft",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(`GitHub API ${method} ${endpoint} failed (${response.status}): ${payload.message || "unknown error"}`);
  }
  return payload;
}

module.exports = { validateDraftRelease };
