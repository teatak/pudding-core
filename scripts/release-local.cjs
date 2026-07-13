#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");
const {
  buildMacPackagingEnvironment,
  parseDeveloperIdentities,
  resolveSigningIdentity,
  validateNotaryCredentials,
} = require("../packaging/macos-release-env.cjs");

const root = path.resolve(__dirname, "..");

if (require.main === module) {
  try {
    main(process.argv.slice(2), process.env);
  } catch (error) {
    console.error(`Local release failed: ${error.message}`);
    process.exitCode = 1;
  }
}

function main(argv, env) {
  const [command] = argv;
  if (command !== "start" && command !== "resume" && command !== "upload") {
    throw new Error("usage: release-local.cjs <start|resume|upload>");
  }
  if (process.platform !== "darwin") {
    throw new Error("desktop releases must run on macOS");
  }

  const release = resolveReleaseChannel(env.PUDDING_RELEASE_CHANNEL, packageMetadata.version);
  const identity = resolveSigningIdentity(env);
  const token = resolveGitHubToken(env);
  const releaseEnv = buildReleaseEnvironment(env, release.channel, identity, token);
  const tag = `v${packageMetadata.version}`;

  console.log(`Preparing local release: tag=${tag} channel=${release.channel}`);
  validateGitHubAccess(releaseEnv);
  validateNotaryCredentials(releaseEnv);
  for (const [step, args] of buildReleaseSteps(command, release.channel, tag)) {
    run(step, args, releaseEnv);
  }
  console.log("Draft is ready. Publish it with: make desktop-release-finalize");
}

function buildReleaseEnvironment(env, channel, identity, token) {
  return buildMacPackagingEnvironment({
    ...env,
    GH_TOKEN: token,
    PUDDING_RELEASE_CHANNEL: channel,
  }, identity);
}

function buildReleaseSteps(command, channel, tag) {
  const bundleTarget = channel === "preview" ? "desktop-preview-bundle" : "desktop-bundle";
  const verifyTarget = channel === "preview" ? "desktop-preview-verify" : "desktop-verify";
  if (command === "upload") {
    return [
      ["node", ["scripts/release-gate.cjs", "check"]],
      ["make", [verifyTarget]],
      ["node", ["scripts/release-draft.cjs", "create", tag]],
      ["node", ["scripts/release-draft.cjs", "status", tag]],
    ];
  }
  const steps = [
    ["node", ["scripts/release-gate.cjs", command === "start" ? "prepare" : "check"]],
    ["make", ["test"]],
    ["npm", ["run", "test:electron"]],
    ["make", [bundleTarget]],
  ];
  if (command === "start") {
    steps.push(["node", ["scripts/release-gate.cjs", "tag"]]);
  }
  steps.push(
    ["node", ["scripts/release-draft.cjs", "create", tag]],
    ["node", ["scripts/release-draft.cjs", "status", tag]],
  );
  return steps;
}

function resolveGitHubToken(env) {
  const configured = String(env.GH_TOKEN || "").trim();
  if (configured) {
    return configured;
  }
  const token = capture("gh", ["auth", "token"]).trim();
  if (!token) {
    throw new Error("GH_TOKEN or an authenticated gh CLI is required");
  }
  return token;
}

function validateGitHubAccess(env) {
  const canPush = capture(
    "gh",
    ["api", "repos/teatak/pudding", "--jq", ".permissions.push"],
    env,
  ).trim();
  if (canPush !== "true") {
    throw new Error("the current GitHub token cannot publish to teatak/pudding");
  }
}

function capture(command, args, env = process.env) {
  const result = spawnSync(command, args, { cwd: root, env, encoding: "utf8" });
  if (result.status !== 0) {
    const detail = String(result.stderr || result.error?.message || "").trim();
    throw new Error(`${command} ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
  }
  return String(result.stdout || "");
}

function run(command, args, env) {
  const result = spawnSync(command, args, { cwd: root, env, stdio: "inherit" });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed`);
  }
}

module.exports = { buildReleaseEnvironment, buildReleaseSteps, parseDeveloperIdentities };
