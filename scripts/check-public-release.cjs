#!/usr/bin/env node

const packageMetadata = require("../package.json");

const repository = "teatak/pudding";

if (require.main === module) {
  main(process.env).catch((error) => {
    console.error(`Public release check failed: ${error.message}`);
    process.exitCode = 1;
  });
}

async function main(env) {
  const token = String(env.GH_TOKEN || "").trim();
  const headers = {
    Accept: "application/vnd.github+json",
    "User-Agent": "pudding-release-gate",
    "X-GitHub-Api-Version": "2022-11-28",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const tag = `v${packageMetadata.version}`;
  const encodedTag = encodeURIComponent(tag);
  const statuses = [];
  for (const [label, endpoint] of [
    ["release", `releases/tags/${encodedTag}`],
    ["tag", `git/ref/tags/${encodedTag}`],
  ]) {
    const response = await fetch(`https://api.github.com/repos/${repository}/${endpoint}`, {
      headers,
    });
    statuses.push({ label, status: response.status });
    await response.body?.cancel();
  }

  assertUnusedStatuses(tag, statuses);
  console.log(`Public version is unused: ${repository} ${tag}`);
}

function assertUnusedStatuses(tag, statuses) {
  for (const { label, status } of statuses) {
    if (status === 404) {
      continue;
    }
    if (status === 200) {
      throw new Error(`${label} ${tag} already exists in ${repository}`);
    }
    throw new Error(`could not verify ${label} ${tag}: GitHub returned HTTP ${status}`);
  }
}

module.exports = { assertUnusedStatuses };
