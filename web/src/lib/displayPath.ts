export function displayUserPath(rawPath: string, rawHomeDirectory: string) {
  const path = rawPath.trim();
  const homeDirectory = trimTrailingSeparators(rawHomeDirectory.trim());
  if (!path || !homeDirectory) {
    return rawPath;
  }

  const normalizedPath = path.replaceAll("\\", "/");
  const normalizedHome = homeDirectory.replaceAll("\\", "/");
  const caseInsensitive = /^[A-Za-z]:\//.test(normalizedHome);
  const comparablePath = caseInsensitive ? normalizedPath.toLocaleLowerCase() : normalizedPath;
  const comparableHome = caseInsensitive ? normalizedHome.toLocaleLowerCase() : normalizedHome;

  if (comparablePath === comparableHome) {
    return "~";
  }
  if (!comparablePath.startsWith(`${comparableHome}/`)) {
    return rawPath;
  }
  return `~/${normalizedPath.slice(normalizedHome.length + 1)}`;
}

function trimTrailingSeparators(path: string) {
  if (/^[A-Za-z]:[\\/]$/.test(path) || path === "/") {
    return path;
  }
  return path.replace(/[\\/]+$/, "");
}
