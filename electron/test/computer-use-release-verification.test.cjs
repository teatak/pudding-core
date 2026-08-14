const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  assertStableComputerUseIdentity,
  computerUseHelperAppPath,
  parseCodeIdentity,
  verifyComputerUseHelper,
} = require("../../scripts/computer-use-release-verification.cjs");

const helperIdentity = {
  identifier: "com.teatak.pudding.computer-use-helper",
  teamIdentifier: "TEAM123",
  designatedRequirement:
    'identifier "com.teatak.pudding.computer-use-helper" and anchor apple generic and certificate leaf[subject.OU] = TEAM123',
};

test("parses and compares the full Computer Use code identity", () => {
  const parsed = parseCodeIdentity(`
Identifier=${helperIdentity.identifier}
TeamIdentifier=${helperIdentity.teamIdentifier}
designated => identifier "com.teatak.pudding.computer-use-helper"   and anchor apple generic and certificate leaf[subject.OU] = TEAM123
`);
  assert.deepEqual(parsed, helperIdentity);
  assert.doesNotThrow(() => assertStableComputerUseIdentity(helperIdentity, { ...helperIdentity }));
  assert.throws(
    () => assertStableComputerUseIdentity(helperIdentity, { ...helperIdentity, teamIdentifier: "OTHER" }),
    /teamIdentifier changed across the update/,
  );
  assert.throws(
    () => assertStableComputerUseIdentity(helperIdentity, { ...helperIdentity, designatedRequirement: "changed" }),
    /designatedRequirement changed across the update/,
  );
});

test("verifies the nested Helper identity, privacy metadata, version, and architecture", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-computer-use-release-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const appPath = path.join(root, "Pudding.app");
  const helperApp = computerUseHelperAppPath(appPath);
  for (const filePath of [
    path.join(appPath, "Contents", "MacOS", "Pudding"),
    path.join(appPath, "Contents", "Info.plist"),
    path.join(helperApp, "Contents", "MacOS", "PuddingComputerUseHelper"),
    path.join(helperApp, "Contents", "Info.plist"),
  ]) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, "fixture");
  }

  const checked = [];
  const commands = fixtureCommands(appPath, helperApp, checked);
  const result = verifyComputerUseHelper(appPath, {
    commands,
    expectedArch: "arm64",
    expectedIdentity: helperIdentity,
    label: "test app",
    signingAuthority: "Developer ID Application: Example (TEAM123)",
  });

  assert.deepEqual(result, { ...helperIdentity, architecture: "arm64" });
  assert.deepEqual(checked, [
    ["codesign", ["--verify", "--deep", "--strict", "--verbose=4", helperApp]],
  ]);
});

test("rejects a Helper whose team differs from the outer app", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-computer-use-release-team-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const appPath = path.join(root, "Pudding.app");
  const helperApp = computerUseHelperAppPath(appPath);
  for (const filePath of [
    path.join(appPath, "Contents", "MacOS", "Pudding"),
    path.join(appPath, "Contents", "Info.plist"),
    path.join(helperApp, "Contents", "MacOS", "PuddingComputerUseHelper"),
    path.join(helperApp, "Contents", "Info.plist"),
  ]) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, "fixture");
  }
  const commands = fixtureCommands(appPath, helperApp, []);
  const originalOutput = commands.output;
  commands.output = (command, args) => {
    if (command === "codesign" && args.at(-1) === appPath) {
      return codeDetails("com.teatak.pudding", "OTHER");
    }
    return originalOutput(command, args);
  };

  assert.throws(
    () => verifyComputerUseHelper(appPath, { commands, label: "test app" }),
    /Team ID differs/,
  );
});

test("rejects a Helper without a designated requirement", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-computer-use-release-requirement-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const appPath = path.join(root, "Pudding.app");
  const helperApp = computerUseHelperAppPath(appPath);
  for (const filePath of [
    path.join(appPath, "Contents", "MacOS", "Pudding"),
    path.join(appPath, "Contents", "Info.plist"),
    path.join(helperApp, "Contents", "MacOS", "PuddingComputerUseHelper"),
    path.join(helperApp, "Contents", "Info.plist"),
  ]) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, "fixture");
  }
  const commands = fixtureCommands(appPath, helperApp, []);
  const originalOutput = commands.output;
  commands.output = (command, args) => {
    if (command === "codesign" && args.at(-1) === helperApp) {
      return `Identifier=${helperIdentity.identifier}\nTeamIdentifier=${helperIdentity.teamIdentifier}`;
    }
    return originalOutput(command, args);
  };

  assert.throws(
    () => verifyComputerUseHelper(appPath, { commands, label: "test app" }),
    /does not have a designated requirement/,
  );
});

function fixtureCommands(appPath, helperApp, checked) {
  return {
    check(command, args) {
      checked.push([command, args]);
    },
    output(command, args) {
      if (command === "codesign") {
        return args.at(-1) === helperApp
          ? codeDetails(helperIdentity.identifier, helperIdentity.teamIdentifier)
          : codeDetails("com.teatak.pudding", helperIdentity.teamIdentifier);
      }
      if (command === "plutil") {
        const key = args[1];
        const plistPath = args[3];
        const helperValues = {
          CFBundleIdentifier: helperIdentity.identifier,
          CFBundleExecutable: "PuddingComputerUseHelper",
          LSUIElement: "true",
          NSScreenCaptureUsageDescription: "Capture a selected window.",
          CFBundleShortVersionString: "0.2.0",
          CFBundleVersion: "0.2.0",
        };
        const outerValues = {
          CFBundleShortVersionString: "0.2.0",
          CFBundleVersion: "0.2.0",
        };
        return plistPath.includes("Pudding Computer Use.app") ? helperValues[key] : outerValues[key];
      }
      if (command === "lipo") {
        return "arm64";
      }
      if (command === "otool") {
        return `${args.at(-1)}:\n\t/System/Library/Frameworks/AppKit.framework/AppKit`;
      }
      throw new Error(`unexpected command: ${command} ${args.join(" ")}`);
    },
  };
}

function codeDetails(identifier, teamIdentifier) {
  return `
Identifier=${identifier}
Authority=Developer ID Application: Example (TEAM123)
TeamIdentifier=${teamIdentifier}
designated => identifier "${identifier}" and anchor apple generic and certificate leaf[subject.OU] = ${teamIdentifier}
`;
}
