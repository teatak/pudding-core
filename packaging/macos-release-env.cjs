const { spawnSync } = require("node:child_process");

const defaultNotaryProfile = "pudding-notary";

function buildMacPackagingEnvironment(env, identity = resolveSigningIdentity(env)) {
  const result = {
    ...env,
    PUDDING_MAC_IDENTITY: identity,
  };
  delete result.PUDDING_UPDATE_MODE;

  const methods = notarizationMethods(result);
  if (methods.length > 1) {
    throw new Error(`multiple notarization methods configured: ${methods.join(", ")}`);
  }
  if (methods.length === 0) {
    result.APPLE_KEYCHAIN_PROFILE = defaultNotaryProfile;
  }
  return result;
}

function resolveSigningIdentity(env) {
  const configured = String(env.PUDDING_MAC_IDENTITY || "").trim();
  if (configured) {
    return configured;
  }
  const identities = parseDeveloperIdentities(
    capture("security", ["find-identity", "-v", "-p", "codesigning"]),
  );
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

function notarizationMethods(env) {
  const methods = [];
  if (String(env.APPLE_KEYCHAIN_PROFILE || "").trim()) {
    methods.push("keychain profile");
  }
  if (completeGroup(env, ["APPLE_ID", "APPLE_APP_SPECIFIC_PASSWORD", "APPLE_TEAM_ID"], "Apple ID")) {
    methods.push("Apple ID");
  }
  if (completeGroup(env, ["APPLE_API_KEY", "APPLE_API_KEY_ID", "APPLE_API_ISSUER"], "App Store Connect API")) {
    methods.push("App Store Connect API");
  }
  return methods;
}

function completeGroup(env, keys, label) {
  const present = keys.filter((key) => String(env[key] || "").trim());
  if (present.length > 0 && present.length !== keys.length) {
    const missing = keys.filter((key) => !String(env[key] || "").trim());
    throw new Error(`${label} notarization credentials are incomplete; missing ${missing.join(", ")}`);
  }
  return present.length === keys.length;
}

function capture(command, args, env = process.env) {
  const result = spawnSync(command, args, { env, encoding: "utf8" });
  if (result.status !== 0) {
    const detail = String(result.stderr || result.error?.message || "").trim();
    throw new Error(`${command} ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
  }
  return String(result.stdout || "");
}

module.exports = {
  buildMacPackagingEnvironment,
  defaultNotaryProfile,
  parseDeveloperIdentities,
  resolveSigningIdentity,
  validateNotaryCredentials,
};
