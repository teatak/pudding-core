const path = require("node:path");

const root = path.resolve(__dirname, "..");
const signingIdentity = String(process.env.PUDDING_MAC_IDENTITY || "-").trim() || "-";
const requestedVersion = String(process.env.PUDDING_APP_VERSION || "").trim();
if (process.env.npm_lifecycle_event === "desktop:publish" && signingIdentity === "-") {
  throw new Error("PUDDING_MAC_IDENTITY is required for publishable macOS updates");
}

module.exports = {
  appId: "com.teatak.pudding",
  productName: "Pudding",
  electronVersion: "43.0.0",
  asar: true,
  artifactName: "${productName}-${version}-${arch}.${ext}",
  ...(requestedVersion ? { extraMetadata: { version: requestedVersion } } : {}),
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
  ],
  afterPack: path.join(root, "packaging", "electron-builder-after-pack.cjs"),
  mac: {
    category: "public.app-category.productivity",
    icon: "assets/macos/AppIcon.icns",
    identity: signingIdentity,
    hardenedRuntime: signingIdentity !== "-",
    target: ["dmg", "zip"],
    extendInfo: {
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
    contents: [
      { x: 150, y: 170, type: "file" },
      { x: 410, y: 170, type: "link", path: "/Applications" },
    ],
    window: {
      width: 560,
      height: 380,
    },
  },
  publish: [
    {
      provider: "github",
      owner: "teatak",
      repo: "pudding",
      releaseType: "release",
    },
  ],
};
