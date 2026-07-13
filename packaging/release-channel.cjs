const releaseChannels = Object.freeze({
  stable: "stable",
  preview: "preview",
});

function resolveReleaseChannel(rawValue, version) {
  const channel = String(rawValue || releaseChannels.stable).trim().toLowerCase();
  if (channel !== releaseChannels.stable && channel !== releaseChannels.preview) {
    throw new Error("PUDDING_RELEASE_CHANNEL must be stable or preview");
  }
  assertVersionForReleaseChannel(version, channel);
  return {
    channel,
    releaseType: channel === releaseChannels.preview ? "prerelease" : "release",
    updateChannel: channel === releaseChannels.preview ? "beta" : "latest",
    updateInfoFile: channel === releaseChannels.preview ? "beta-mac.yml" : "latest-mac.yml",
  };
}

function assertVersionForReleaseChannel(version, channel) {
  const normalized = String(version || "").trim();
  const pattern =
    channel === releaseChannels.preview
      ? /^\d+\.\d+\.\d+-beta\.[1-9]\d*$/
      : /^\d+\.\d+\.\d+$/;
  if (!pattern.test(normalized)) {
    const expected = channel === releaseChannels.preview ? "x.y.z-beta.n" : "x.y.z";
    throw new Error(`${channel} release version must match ${expected}`);
  }
}

module.exports = { releaseChannels, resolveReleaseChannel };
