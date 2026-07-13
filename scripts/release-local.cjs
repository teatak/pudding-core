#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

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
  if (command !== "start" && command !== "resume") {
    throw new Error("usage: release-local.cjs <start|resume>");
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
  run("node", ["scripts/check-public-release.cjs"], releaseEnv);
  run("make", ["test"], releaseEnv);
  run("npm", ["run", "test:electron"], releaseEnv);
  run("node", ["scripts/release-gate.cjs", command === "start" ? "tag" : "check"], releaseEnv);
  run("make", ["desktop-release", "language-servers"], releaseEnv);
  run("npm", ["run", "desktop:package"], releaseEnv);
  run("node", ["scripts/verify-desktop-release.cjs"], releaseEnv);
  run("node", ["scripts/release-draft.cjs", "create", tag], releaseEnv);
  run("node", ["scripts/release-draft.cjs", "status", tag], releaseEnv);
  console.log("Draft is ready. Publish it with: make desktop-release-finalize");
}

function buildReleaseEnvironment(env, channel, identity, token) {
  const result = {
    ...env,
    GH_TOKEN: token,
    PUDDING_MAC_IDENTITY: identity,
    PUDDING_RELEASE_CHANNEL: channel,
  };
  const hasAppleIDCredentials = Boolean(
    result.APPLE_ID && result.APPLE_APP_SPECIFIC_PASSWORD && result.APPLE_TEAM_ID,
  );
  if (!String(result.APPLE_KEYCHAIN_PROFILE || "").trim() && !hasAppleIDCredentials) {
    result.APPLE_KEYCHAIN_PROFILE = "pudding-notary";
  }
  return result;
}

function resolveSigningIdentity(env) {
  const configured = String(env.PUDDING_MAC_IDENTITY || "").trim();
  if (configured) {
    return configured;
  }
  const output = capture("security", ["find-identity", "-v", "-p", "codesigning"]);
  const identities = parseDeveloperIdentities(output);
  if (identities.length === 0) {
    throw new Error("no valid Developer ID Application identity was found");
  }
  if (identities.length > 1) {
    throw new Error("multiple Developer ID identities found; set PUDDING_MAC_IDENTITY explicitly");
  }
  return identities[0];
}

function parseDeveloperIdentities(output) {
  const identities = new Set();
  for (const match of String(output || "").matchAll(/"(Developer ID Application:[^"]+)"/g)) {
    identities.add(match[1].trim());
  }
  return [...identities];
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

function validateNotaryCredentials(env) {
  const profile = String(env.APPLE_KEYCHAIN_PROFILE || "").trim();
  if (!profile) {
    return;
  }
  capture(
    "xcrun",
    ["notarytool", "history", "--keychain-profile", profile, "--output-format", "json"],
    env,
  );
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

module.exports = { buildReleaseEnvironment, parseDeveloperIdentities };
