export type AppReleaseVersion = {
  version: string;
  preview?: boolean;
  package_sha256?: string;
};

export type InstalledAppVersion = {
  version?: string;
  packageSHA256?: string;
};

type ParsedVersion = {
  core: number[];
  prerelease: string[] | null;
};

export function compareAppVersions(a: string, b: string) {
  const parsedA = parseAppVersion(a);
  const parsedB = parseAppVersion(b);
  if (!parsedA || !parsedB) {
    return a.localeCompare(b, undefined, { numeric: true });
  }
  for (let index = 0; index < 3; index += 1) {
    const difference = parsedA.core[index] - parsedB.core[index];
    if (difference !== 0) {
      return difference;
    }
  }
  if (!parsedA.prerelease && !parsedB.prerelease) {
    return 0;
  }
  if (!parsedA.prerelease) {
    return 1;
  }
  if (!parsedB.prerelease) {
    return -1;
  }
  const length = Math.max(parsedA.prerelease.length, parsedB.prerelease.length);
  for (let index = 0; index < length; index += 1) {
    const partA = parsedA.prerelease[index];
    const partB = parsedB.prerelease[index];
    if (partA === undefined) {
      return -1;
    }
    if (partB === undefined) {
      return 1;
    }
    if (partA === partB) {
      continue;
    }
    const numericA = /^\d+$/.test(partA);
    const numericB = /^\d+$/.test(partB);
    if (numericA && numericB) {
      return Number(partA) - Number(partB);
    }
    if (numericA !== numericB) {
      return numericA ? -1 : 1;
    }
    return partA.localeCompare(partB);
  }
  return 0;
}

export function isPreviewRelease(release: AppReleaseVersion) {
  return release.preview === true;
}

export function selectAppInstallRelease<T extends AppReleaseVersion>(
  releases: T[],
  includePreview: boolean,
  installed?: InstalledAppVersion,
): T | undefined {
  const ordered = [...releases].sort((a, b) => compareAppVersions(b.version, a.version));
  const stable = ordered.find((release) => !isPreviewRelease(release));
  if (!includePreview) {
    return stable;
  }
  const baselineVersion = installed?.version || stable?.version;
  const newerPreview = ordered.find(
    (release) =>
      isPreviewRelease(release) &&
      (!baselineVersion || compareAppVersions(release.version, baselineVersion) > 0),
  );
  return newerPreview || stable || ordered[0];
}

export function installedMatchesRelease(
  installed: InstalledAppVersion | undefined,
  release: AppReleaseVersion,
) {
  if (!installed || installed.version !== release.version) {
    return false;
  }
  const expectedSHA = release.package_sha256?.toLowerCase();
  return !expectedSHA || installed.packageSHA256?.toLowerCase() === expectedSHA;
}

export function needsAppUpgrade(installed: InstalledAppVersion, release: AppReleaseVersion) {
  if (!installed.version) {
    return false;
  }
  const comparison = compareAppVersions(release.version, installed.version);
  if (comparison !== 0) {
    return comparison > 0;
  }
  const expectedSHA = release.package_sha256?.toLowerCase();
  return Boolean(expectedSHA && installed.packageSHA256?.toLowerCase() !== expectedSHA);
}

function parseAppVersion(value: string): ParsedVersion | null {
  const match = value.trim().match(/^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/);
  if (!match) {
    return null;
  }
  return {
    core: [Number(match[1]), Number(match[2]), Number(match[3])],
    prerelease: match[4] ? match[4].split(".") : null,
  };
}
