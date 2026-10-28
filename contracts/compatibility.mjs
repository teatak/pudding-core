// API requirements are minimum stable versions within one supported major.
// "1" and the plugin range "^1.0.0" are the original legacy declarations.
function parseVersion(value) {
  if (value === '1') value = '1.0.0';
  if (typeof value !== 'string' || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(value)) return null;
  const parts = value.split('.').map(Number);
  return parts.every(Number.isSafeInteger) ? parts : null;
}
function supportsVersion(required, provided) {
  const wanted = parseVersion(required), host = parseVersion(provided);
  return !!wanted && !!host && wanted[0] === host[0] &&
    (wanted[1] < host[1] || (wanted[1] === host[1] && wanted[2] <= host[2]));
}
function supportsPluginAPI(required, provided) {
  if (required === undefined || required === '') required = '1.0.0';
  if (typeof required !== 'string') return false;
  const caret = required.startsWith('^');
  const version = caret ? required.slice(1) : required;
  if (!supportsVersion(version, provided)) return false;
  if (!caret) return true;
  const wanted = parseVersion(version), host = parseVersion(provided);
  return wanted[0] !== 0 || (wanted[1] === host[1] && (wanted[1] !== 0 || wanted[2] === host[2]));
}
export { parseVersion, supportsVersion, supportsPluginAPI };
