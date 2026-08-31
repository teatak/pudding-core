#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const {
  buildMacPackagingEnvironment,
  validateNotaryCredentials,
} = require("../packaging/macos-release-env.cjs");
const { validateManifestVersions } = require("./release-gate.cjs");

const root = path.resolve(__dirname, "..");

try {
  main(process.argv.slice(2), process.env);
} catch (error) {
  console.error(`Desktop packaging failed: ${error.message}`);
  process.exitCode = 1;
}

function main(argv, env) {
  if (process.platform !== "darwin") {
    throw new Error("desktop packaging currently supports macOS only");
  }
  if (env.PUDDING_PACKAGING_PIPELINE !== "1") {
    throw new Error("run make desktop-bundle or make desktop-preview-bundle");
  }
  const verifyOnly = argv.length === 1 && argv[0] === "--verify-only";
  if (argv.length > (verifyOnly ? 1 : 0)) {
    throw new Error("usage: package-desktop.cjs [--verify-only]");
  }

  const packageMetadata = readJSON(path.join(root, "package.json"));
  const version = validateManifestVersions(
    packageMetadata,
    readJSON(path.join(root, "package-lock.json")),
  );

  const releaseEnv = {
    ...buildMacPackagingEnvironment(env),
    PUDDING_PACKAGING_PIPELINE: "1",
  };
  if (verifyOnly) {
    run(process.execPath, ["scripts/verify-desktop-release.cjs"], releaseEnv);
    return;
  }
  validateNotaryCredentials(releaseEnv);
  run(process.execPath, ["scripts/generate-third-party-notices.cjs"], releaseEnv);

  const builder = path.join(root, "node_modules", ".bin", "electron-builder");
  requireExecutable(builder, "electron-builder is missing; run npm install");
  for (const arch of ["arm64", "x64"]) {
    const runtime = path.join(root, "dist", "runtime", arch);
    requireExecutable(path.join(runtime, "puddingd"), `${arch} release daemon is missing`);
    requireExecutable(path.join(runtime, "language-servers", "gopls"), `${arch} gopls is missing`);
    requireFile(
      path.join(runtime, "language-servers", "gopls.LICENSE"),
      `${arch} gopls license is missing`,
    );
  }

  const output = path.join(root, "dist", "release");
  fs.rmSync(output, { force: true, recursive: true });
  console.log(
    `Packaging desktop release: version=${releaseEnv.PUDDING_APP_VERSION || version} ` +
      `channel=${releaseEnv.PUDDING_RELEASE_CHANNEL || "stable"}`,
  );
  run(
    builder,
    [
      "--config",
      "packaging/electron-builder.config.cjs",
      "--arm64",
      "--x64",
      "--publish",
      "never",
    ],
    releaseEnv,
  );
  run(process.execPath, ["scripts/verify-desktop-release.cjs"], releaseEnv);
}

function requireExecutable(filePath, message) {
  requireFile(filePath, message);
  fs.accessSync(filePath, fs.constants.X_OK);
}

function requireFile(filePath, message) {
  if (!fs.statSync(filePath, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`${message}: ${filePath}`);
  }
}

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function run(command, args, env) {
  const result = spawnSync(command, args, { cwd: root, env, stdio: "inherit" });
  if (result.status !== 0) {
    throw new Error(`${path.basename(command)} ${args.join(" ")} failed`);
  }
}
