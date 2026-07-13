#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { inferReleaseChannelFromTag } = require("./release-gate.cjs");

const repository = "teatak/pudding";
const root = path.resolve(__dirname, "..");

if (require.main === module) {
  main(process.argv.slice(2), process.env).catch((error) => {
    console.error(`Release draft command failed: ${error.message}`);
    process.exitCode = 1;
  });
}

async function main(argv, env) {
  const [command, requestedTag] = argv;
  if (command !== "create" && command !== "status" && command !== "publish") {
    throw new Error("usage: release-draft.cjs <create|status|publish> [vX.Y.Z[-beta.N]]");
  }

  const tag = String(requestedTag || `v${packageMetadata.version}`).trim();
  const channel = inferReleaseChannelFromTag(tag);
  const token = resolveGitHubToken(env);
  const releases = await githubRequest("GET", `/repos/${repository}/releases?per_page=100`, token);
  const matching = releases.filter((candidate) => candidate.tag_name === tag);
  if (matching.length > 1) {
    throw new Error(`multiple drafts exist for ${tag}; consolidate them before continuing`);
  }
  if (command === "create") {
    await createDraftRelease(matching[0], tag, channel, token, env);
    return;
  }

  const release = matching[0];
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

async function createDraftRelease(existingRelease, tag, channel, token, env) {
  const assets = expectedAssetNames(tag, channel).map((name) => {
    const filePath = path.join(root, "dist", "release", name);
    const stat = fs.statSync(filePath, { throwIfNoEntry: false });
    if (!stat?.isFile() || stat.size <= 0) {
      throw new Error(`local release asset is missing or empty: ${filePath}`);
    }
    return { name, filePath, size: stat.size };
  });

  let release = existingRelease;
  if (!release) {
    release = await githubRequest(
      "POST",
      `/repos/${repository}/releases`,
      token,
      createDraftMetadata(tag, channel),
    );
  }
  if (!release?.draft) {
    throw new Error(`draft ${tag} could not be created`);
  }

  const remoteAssets = new Map((release.assets || []).map((asset) => [asset.name, asset]));
  for (const asset of assets) {
    const remote = remoteAssets.get(asset.name);
    if (remote?.state === "uploaded" && Number(remote.size) === asset.size) {
      continue;
    }
    const args = ["release", "upload", tag, asset.filePath, "--repo", repository];
    if (remote) {
      args.push("--clobber");
    }
    runGh(args, token, env);
  }

  release = await githubRequest("GET", `/repos/${repository}/releases/${release.id}`, token);
  validateDraftRelease(release, tag, channel);
  console.log(`Draft release assets ready: ${tag} ${release.html_url}`);
}

function createDraftMetadata(tag, channel) {
  return {
    tag_name: tag,
    name: tag.slice(1),
    body: "",
    draft: true,
    prerelease: channel === "preview",
  };
}

function validateDraftRelease(release, tag, channel) {
  if (release.tag_name !== tag || release.draft !== true) {
    throw new Error(`release ${tag} is not a draft`);
  }

  const expectedAssets = expectedAssetNames(tag, channel);
  const assets = new Map((release.assets || []).map((asset) => [asset.name, asset]));
  for (const name of expectedAssets) {
    const asset = assets.get(name);
    if (!asset || asset.state !== "uploaded" || Number(asset.size) <= 0) {
      throw new Error(`draft ${tag} is missing a complete ${name} asset`);
    }
  }
}

function expectedAssetNames(tag, channel) {
  const version = tag.slice(1);
  const updateInfo = channel === "preview" ? "beta-mac.yml" : "latest-mac.yml";
  return [
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

function runGh(args, token, env) {
  const result = spawnSync("gh", args, {
    cwd: root,
    env: { ...env, GH_TOKEN: token },
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`gh ${args.join(" ")} failed`);
  }
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

module.exports = { createDraftMetadata, expectedAssetNames, validateDraftRelease };
