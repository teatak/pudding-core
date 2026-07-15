#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");
const { readReleaseNotes } = require("./release-metadata.cjs");

const root = path.resolve(__dirname, "..");

if (require.main === module) {
  main(process.argv.slice(2), process.env).catch((error) => {
    console.error(`Release gate failed: ${error.message}`);
    process.exitCode = 1;
  });
}

async function main(argv, env) {
  const [command, ...args] = argv;
  if (command === "channel") {
    const tag = args[0] || env.GITHUB_REF_NAME || "";
    console.log(inferReleaseChannelFromTag(tag));
    return;
  }

  if (command !== "prepare" && command !== "check" && command !== "tag") {
    throw new Error("usage: release-gate.cjs <prepare|check|tag|channel> [--channel stable|preview]");
  }

  const channel = readOption(args, "--channel") || env.PUDDING_RELEASE_CHANNEL || "stable";
  const metadata = readReleaseMetadata(root, channel, env.PUDDING_APP_VERSION);
  readReleaseNotes(root, metadata.tag);
  assertCleanWorktree(root);

  if (command === "check") {
    assertReleaseTag(root, metadata.tag);
    console.log(
      `Release tag verified: tag=${metadata.tag} channel=${metadata.releaseChannel.channel}`,
    );
    return;
  }

  assertPublishCheckout(root);
  await assertPublicVersionUnused(metadata.tag, env);
  if (command === "prepare") {
    console.log(
      `Release checkout ready: tag=${metadata.tag} channel=${metadata.releaseChannel.channel}`,
    );
    return;
  }

  createAndPushReleaseTag(root, metadata.tag);
  console.log(
    `Release started: tag=${metadata.tag} channel=${metadata.releaseChannel.channel}`,
  );
}

async function assertPublicVersionUnused(tag, env, fetchImpl = fetch) {
  const repository = "teatak/pudding";
  const token = String(env.GH_TOKEN || "").trim();
  const headers = {
    Accept: "application/vnd.github+json",
    "User-Agent": "pudding-release-gate",
    "X-GitHub-Api-Version": "2022-11-28",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const encodedTag = encodeURIComponent(tag);
  const statuses = [];
  for (const [label, endpoint] of [
    ["release", `releases/tags/${encodedTag}`],
    ["tag", `git/ref/tags/${encodedTag}`],
  ]) {
    const response = await fetchImpl(`https://api.github.com/repos/${repository}/${endpoint}`, {
      headers,
    });
    statuses.push({ label, status: response.status });
    await response.body?.cancel();
  }
  assertUnusedStatuses(tag, statuses, repository);
}

function assertUnusedStatuses(tag, statuses, repository = "teatak/pudding") {
  for (const { label, status } of statuses) {
    if (status === 404) {
      continue;
    }
    if (status === 200) {
      throw new Error(`${label} ${tag} already exists in ${repository}`);
    }
    throw new Error(`could not verify ${label} ${tag}: GitHub returned HTTP ${status}`);
  }
}

function readReleaseMetadata(projectRoot, channel, versionOverride) {
  if (String(versionOverride || "").trim()) {
    throw new Error("PUDDING_APP_VERSION is only allowed for local bundle tests");
  }

  const packageMetadata = readJSON(path.join(projectRoot, "package.json"));
  const packageLock = readJSON(path.join(projectRoot, "package-lock.json"));
  const version = validateManifestVersions(packageMetadata, packageLock);
  return {
    version,
    tag: expectedTagForVersion(version),
    releaseChannel: resolveReleaseChannel(channel, version),
  };
}

function validateManifestVersions(packageMetadata, packageLock) {
  const packageVersion = String(packageMetadata?.version || "").trim();
  const lockVersion = String(packageLock?.version || "").trim();
  const lockRootVersion = String(packageLock?.packages?.[""]?.version || "").trim();
  if (!packageVersion) {
    throw new Error("package.json version is empty");
  }
  if (packageVersion !== lockVersion || packageVersion !== lockRootVersion) {
    throw new Error(
      `package versions differ: package.json=${packageVersion || "<empty>"}, ` +
        `package-lock.json=${lockVersion || "<empty>"}, lock root=${lockRootVersion || "<empty>"}`,
    );
  }
  return packageVersion;
}

function expectedTagForVersion(version) {
  return `v${String(version || "").trim()}`;
}

function inferReleaseChannelFromTag(tag) {
  const normalized = String(tag || "").trim();
  if (!normalized.startsWith("v")) {
    throw new Error(`release tag must start with v: ${normalized || "<empty>"}`);
  }
  const version = normalized.slice(1);
  const channel = version.includes("-") ? "preview" : "stable";
  resolveReleaseChannel(channel, version);
  if (expectedTagForVersion(version) !== normalized) {
    throw new Error(`invalid release tag: ${normalized}`);
  }
  return channel;
}

function assertCleanWorktree(projectRoot) {
  const status = git(projectRoot, ["status", "--porcelain=v1", "--untracked-files=all"]);
  if (status) {
    throw new Error("release requires a clean worktree");
  }
}

function assertPublishCheckout(projectRoot) {
  const branch = git(projectRoot, ["symbolic-ref", "--quiet", "--short", "HEAD"], {
    allowFailure: true,
  });
  if (!branch.ok || !branch.output) {
    throw new Error("release tag must be created from a branch checkout");
  }

  const upstream = git(
    projectRoot,
    ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"],
    { allowFailure: true },
  );
  if (!upstream.ok || !upstream.output) {
    throw new Error(`branch ${branch.output} has no upstream`);
  }

  const remote = git(projectRoot, ["config", "--get", `branch.${branch.output}.remote`], {
    allowFailure: true,
  });
  const mergeRef = git(projectRoot, ["config", "--get", `branch.${branch.output}.merge`], {
    allowFailure: true,
  });
  if (!remote.ok || !remote.output || !mergeRef.ok || !mergeRef.output) {
    throw new Error(`cannot resolve remote upstream for branch ${branch.output}`);
  }

  const remoteBranch = git(projectRoot, ["ls-remote", remote.output, mergeRef.output]);
  const remoteHead = remoteBranch.split(/\s+/)[0] || "";
  const head = git(projectRoot, ["rev-parse", "HEAD"]);
  if (!remoteHead || remoteHead !== head) {
    throw new Error(
      `branch ${branch.output} must be pushed and match ${upstream.output} before release`,
    );
  }
}

function assertReleaseTag(projectRoot, tag) {
  const type = git(projectRoot, ["cat-file", "-t", `refs/tags/${tag}`], { allowFailure: true });
  if (!type.ok) {
    throw new Error(`missing release tag ${tag}`);
  }
  if (type.output !== "tag") {
    throw new Error(`release tag ${tag} must be annotated`);
  }

  const head = git(projectRoot, ["rev-parse", "HEAD"]);
  const taggedCommit = git(projectRoot, ["rev-list", "-n", "1", tag]);
  if (taggedCommit !== head) {
    throw new Error(`release tag ${tag} does not point to HEAD`);
  }
}

function createAndPushReleaseTag(projectRoot, tag) {
  const remoteTag = git(projectRoot, ["ls-remote", "--tags", "origin", `refs/tags/${tag}`]);
  if (remoteTag) {
    throw new Error(`remote tag ${tag} already exists; release tags are immutable`);
  }

  const localType = git(projectRoot, ["cat-file", "-t", `refs/tags/${tag}`], {
    allowFailure: true,
  });
  if (localType.ok) {
    assertReleaseTag(projectRoot, tag);
  } else {
    git(projectRoot, ["tag", "--annotate", tag, "--message", `Pudding ${tag}`]);
  }
  git(projectRoot, ["push", "origin", `refs/tags/${tag}`], { inherit: true });
}

function readOption(args, option) {
  const index = args.indexOf(option);
  if (index === -1) {
    return "";
  }
  const value = args[index + 1];
  if (!value || value.startsWith("--")) {
    throw new Error(`${option} requires a value`);
  }
  return value;
}

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function git(projectRoot, args, options = {}) {
  const result = spawnSync("git", args, {
    cwd: projectRoot,
    encoding: "utf8",
    stdio: options.inherit ? "inherit" : "pipe",
  });
  const output = String(result.stdout || "").trim();
  if (result.status !== 0) {
    if (options.allowFailure) {
      return { ok: false, output };
    }
    const detail = String(result.stderr || result.error?.message || "").trim();
    throw new Error(`git ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
  }
  return options.allowFailure ? { ok: true, output } : output;
}

module.exports = {
  assertPublicVersionUnused,
  assertUnusedStatuses,
  expectedTagForVersion,
  inferReleaseChannelFromTag,
  readReleaseMetadata,
  validateManifestVersions,
};
