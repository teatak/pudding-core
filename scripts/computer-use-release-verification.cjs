const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const computerUseBundleID = "com.teatak.pudding.computer-use-helper";
const computerUseExecutableName = "PuddingComputerUseHelper";

function computerUseHelperAppPath(appPath) {
  return path.join(
    appPath,
    "Contents",
    "Resources",
    "app",
    "bin",
    "Pudding Computer Use.app",
  );
}

function hasComputerUseHelper(appPath) {
  return fs.statSync(computerUseHelperAppPath(appPath), { throwIfNoEntry: false })?.isDirectory() === true;
}

function verifyComputerUseHelper(appPath, options = {}) {
  const commands = options.commands || systemCommands;
  const label = String(options.label || "Pudding.app");
  const helperApp = computerUseHelperAppPath(appPath);
  const helperInfo = path.join(helperApp, "Contents", "Info.plist");
  const helperExecutable = path.join(helperApp, "Contents", "MacOS", computerUseExecutableName);
  const outerInfo = path.join(appPath, "Contents", "Info.plist");
  const outerExecutable = path.join(appPath, "Contents", "MacOS", "Pudding");
  for (const required of [helperApp, helperInfo, helperExecutable, outerInfo, outerExecutable]) {
    if (!fs.existsSync(required)) {
      throw new Error(`${label} is missing Computer Use release component: ${required}`);
    }
  }

  commands.check("codesign", ["--verify", "--deep", "--strict", "--verbose=4", helperApp]);
  const helperDetails = commands.output("codesign", ["-dv", "-r-", "--verbose=4", helperApp]);
  const outerDetails = commands.output("codesign", ["-dv", "-r-", "--verbose=4", appPath]);
  const identity = parseCodeIdentity(helperDetails);
  const outerIdentity = parseCodeIdentity(outerDetails);
  if (identity.identifier !== computerUseBundleID) {
    throw new Error(`${label} Computer Use identifier is ${identity.identifier || "<missing>"}`);
  }
  if (!identity.teamIdentifier || identity.teamIdentifier === "not set" || /Signature=adhoc/.test(helperDetails)) {
    throw new Error(`${label} Computer Use Helper does not have a Developer ID identity`);
  }
  if (!identity.designatedRequirement) {
    throw new Error(`${label} Computer Use Helper does not have a designated requirement`);
  }
  if (identity.teamIdentifier !== outerIdentity.teamIdentifier) {
    throw new Error(`${label} Computer Use Helper Team ID differs from Pudding.app`);
  }
  if (options.signingAuthority && !helperDetails.includes(`Authority=${options.signingAuthority}`)) {
    throw new Error(`${label} Computer Use Helper is not signed by ${options.signingAuthority}`);
  }

  const plistBundleID = plistValue(commands, helperInfo, "CFBundleIdentifier");
  const executableName = plistValue(commands, helperInfo, "CFBundleExecutable");
  const uiElement = plistValue(commands, helperInfo, "LSUIElement");
  const usageDescription = plistValue(commands, helperInfo, "NSScreenCaptureUsageDescription");
  const helperVersion = plistValue(commands, helperInfo, "CFBundleShortVersionString");
  const helperBuildVersion = plistValue(commands, helperInfo, "CFBundleVersion");
  const outerVersion = plistValue(commands, outerInfo, "CFBundleShortVersionString");
  const outerBuildVersion = plistValue(commands, outerInfo, "CFBundleVersion");
  if (plistBundleID !== computerUseBundleID || executableName !== computerUseExecutableName) {
    throw new Error(`${label} Computer Use Info.plist identity is invalid`);
  }
  if (uiElement !== "true") {
    throw new Error(`${label} Computer Use Helper must remain an LSUIElement background app`);
  }
  if (!usageDescription) {
    throw new Error(`${label} Computer Use Helper is missing NSScreenCaptureUsageDescription`);
  }
  if (helperVersion !== outerVersion || helperBuildVersion !== outerBuildVersion) {
    throw new Error(`${label} Computer Use Helper version differs from Pudding.app`);
  }

  const helperArch = singleArchitecture(commands.output("lipo", ["-archs", helperExecutable]), label);
  const outerArch = singleArchitecture(commands.output("lipo", ["-archs", outerExecutable]), label);
  if (helperArch !== outerArch || (options.expectedArch && helperArch !== options.expectedArch)) {
    throw new Error(`${label} Computer Use Helper architecture ${helperArch} differs from Pudding.app ${outerArch}`);
  }
  const dependencies = commands.output("otool", ["-L", helperExecutable]);
  if (/\s(?:\/Users\/|\/opt\/homebrew\/|\/usr\/local\/)/.test(dependencies)) {
    throw new Error(`${label} Computer Use Helper contains a non-portable dependency`);
  }

  if (options.expectedIdentity) {
    assertStableComputerUseIdentity(options.expectedIdentity, identity, label);
  }
  return { ...identity, architecture: helperArch };
}

function parseCodeIdentity(details) {
  const normalized = String(details || "");
  return {
    identifier: matchLine(normalized, /^Identifier=(.+)$/m),
    teamIdentifier: matchLine(normalized, /^TeamIdentifier=(.+)$/m),
    designatedRequirement: matchLine(normalized, /^designated => (.+)$/m).replace(/\s+/g, " "),
  };
}

function assertStableComputerUseIdentity(before, after, label = "Pudding.app") {
  for (const field of ["identifier", "teamIdentifier", "designatedRequirement"]) {
    if (!before?.[field] || !after?.[field] || before[field] !== after[field]) {
      throw new Error(`${label} Computer Use ${field} changed across the update`);
    }
  }
}

function plistValue(commands, plistPath, key) {
  return commands.output("plutil", ["-extract", key, "raw", plistPath]).trim();
}

function singleArchitecture(output, label) {
  const architectures = String(output || "").trim().split(/\s+/).filter(Boolean);
  if (architectures.length !== 1) {
    throw new Error(`${label} contains an unexpected architecture set: ${architectures.join(",")}`);
  }
  return architectures[0];
}

function matchLine(value, pattern) {
  return value.match(pattern)?.[1]?.trim() || "";
}

const systemCommands = {
  check(command, args) {
    const result = spawnSync(command, args, { encoding: "utf8" });
    if (result.status !== 0) {
      const detail = String(result.stderr || result.stdout || result.error?.message || "").trim();
      throw new Error(`${command} ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
    }
  },
  output(command, args) {
    const result = spawnSync(command, args, { encoding: "utf8" });
    if (result.status !== 0) {
      const detail = String(result.stderr || result.stdout || result.error?.message || "").trim();
      throw new Error(`${command} ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
    }
    return `${result.stdout || ""}\n${result.stderr || ""}`;
  },
};

module.exports = {
  assertStableComputerUseIdentity,
  computerUseBundleID,
  computerUseHelperAppPath,
  hasComputerUseHelper,
  parseCodeIdentity,
  verifyComputerUseHelper,
};
