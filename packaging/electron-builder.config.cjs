const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("./release-channel.cjs");

const root = path.resolve(__dirname, "..");
const requestedSigningIdentity = String(process.env.PUDDING_MAC_IDENTITY || "-").trim() || "-";
const signingIdentity = normalizeSigningIdentity(requestedSigningIdentity);
const requestedVersion = String(process.env.PUDDING_APP_VERSION || "").trim();
const releaseVersion = requestedVersion || packageMetadata.version;
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, releaseVersion);
const requestedUpdateMode = String(process.env.PUDDING_UPDATE_MODE || "").trim().toLowerCase();
if (requestedUpdateMode && requestedUpdateMode !== "manual" && requestedUpdateMode !== "automatic") {
  throw new Error("PUDDING_UPDATE_MODE must be manual or automatic");
}
const updateMode = requestedUpdateMode || "automatic";
const notarizationConfigured =
  Boolean(String(process.env.APPLE_KEYCHAIN_PROFILE || "").trim()) ||
  Boolean(process.env.APPLE_ID && process.env.APPLE_APP_SPECIFIC_PASSWORD && process.env.APPLE_TEAM_ID) ||
  Boolean(process.env.APPLE_API_KEY && process.env.APPLE_API_KEY_ID && process.env.APPLE_API_ISSUER);
if (updateMode === "automatic" && signingIdentity === "-") {
  throw new Error("PUDDING_MAC_IDENTITY is required for automatic macOS updates");
}
if (signingIdentity !== "-" && !notarizationConfigured) {
  throw new Error("Developer ID builds require Apple notarization credentials");
}

const extraMetadata = {
  puddingReleaseChannel: releaseChannel.channel,
  puddingUpdateMode: updateMode,
};
if (requestedVersion) {
  extraMetadata.version = requestedVersion;
}

module.exports = {
  appId: "com.teatak.pudding",
  productName: "Pudding",
  electronVersion: "43.0.0",
  forceCodeSigning: signingIdentity !== "-",
  asar: true,
  artifactName: "${productName}-${version}-${arch}.${ext}",
  extraMetadata,
  directories: {
    output: "dist/release",
  },
  files: [
    "electron/**/*.cjs",
    "!electron/test{,/**/*}",
    "package.json",
  ],
  extraResources: [
    { from: "bin/puddingd", to: "app/bin/puddingd" },
    { from: "bin/language-servers", to: "app/language-servers" },
    { from: "assets/macos/TrayTemplate.png", to: "TrayTemplate.png" },
    { from: "packaging/macos/zh-Hans.lproj", to: "zh-Hans.lproj" },
    { from: "packaging/macos/zh-Hant.lproj", to: "zh-Hant.lproj" },
  ],
  afterPack: path.join(root, "packaging", "electron-builder-after-pack.cjs"),
  mac: {
    category: "public.app-category.productivity",
    icon: "assets/macos/AppIcon.icns",
    identity: signingIdentity,
    hardenedRuntime: signingIdentity !== "-",
    target: ["dmg", "zip"],
    extendInfo: {
      LSHasLocalizedDisplayName: true,
      NSCameraUsageDescription: "Pudding uses the camera when you choose to capture a photo.",
      NSMicrophoneUsageDescription: "Pudding uses the microphone for local dictation.",
    },
  },
  protocols: [
    {
      name: "Pudding",
      schemes: ["pudding"],
    },
  ],
  dmg: {
    background: "assets/macos/dmg-background.tiff",
    contents: [
      { x: 165, y: 210, type: "file" },
      { x: 475, y: 210, type: "link", path: "/Applications" },
    ],
    iconSize: 92,
    iconTextSize: 13,
    title: "Pudding ${version}",
    window: {
      width: 640,
      height: 400,
    },
  },
  publish: [
    {
      provider: "github",
      owner: "teatak",
      repo: "pudding",
      channel: releaseChannel.updateChannel,
      releaseType: releaseChannel.releaseType,
    },
  ],
};

function normalizeSigningIdentity(identity) {
  if (identity === "-") {
    return identity;
  }
  return identity.replace(/^Developer ID Application:\s*/i, "");
}
