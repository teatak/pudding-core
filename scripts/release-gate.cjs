#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

const root = path.resolve(__dirname, "..");

if (require.main === module) {
  try {
    main(process.argv.slice(2), process.env);
  } catch (error) {
    console.error(`Release gate failed: ${error.message}`);
    process.exitCode = 1;
  }
}

function main(argv, env) {
  const [command, ...args] = argv;
  if (command === "channel") {
    const tag = args[0] || env.GITHUB_REF_NAME || "";
    console.log(inferReleaseChannelFromTag(tag));
    return;
  }

  if (command !== "check" && command !== "tag") {
    throw new Error("usage: release-gate.cjs <check|tag|channel> [--channel stable|preview]");
  }

  const channel = readOption(args, "--channel") || env.PUDDING_RELEASE_CHANNEL || "stable";
  const metadata = readReleaseMetadata(root, channel, env.PUDDING_APP_VERSION);
  assertCleanWorktree(root);

  if (command === "check") {
    assertReleaseTag(root, metadata.tag, env);
    console.log(
      `Release tag verified: tag=${metadata.tag} channel=${metadata.releaseChannel.channel}`,
    );
    return;
  }

  assertPublishCheckout(root);
  createAndPushReleaseTag(root, metadata.tag);
  console.log(
    `Release started: tag=${metadata.tag} channel=${metadata.releaseChannel.channel}`,
  );
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

function assertReleaseTag(projectRoot, tag, env) {
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
  if (env.GITHUB_REF_TYPE && env.GITHUB_REF_TYPE !== "tag") {
    throw new Error("release workflow must run from a tag ref");
  }
  if (env.GITHUB_REF_NAME && env.GITHUB_REF_NAME !== tag) {
    throw new Error(`workflow tag ${env.GITHUB_REF_NAME} does not match ${tag}`);
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
    assertReleaseTag(projectRoot, tag, {});
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
  expectedTagForVersion,
  inferReleaseChannelFromTag,
  readReleaseMetadata,
  validateManifestVersions,
};
