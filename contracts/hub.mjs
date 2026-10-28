export const HUB_INDEX_URL = 'https://teatak.github.io/pudding-hub/index.json';
export const OFFICIAL_HUB_ID = 'teatak/pudding-hub';
const base = 'https://teatak.github.io/pudding-hub/';
export function httpsURL(value) {
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.username || url.password || url.hash) throw new Error('Hub URLs must use HTTPS without credentials or fragments');
  return url;
}
// A persistent source reference contains the index URL and stable source ID,
// never the current catalog path. This lets the catalog move independently.
export function hubSourceReference(indexURL, sourceID) {
  const url = httpsURL(indexURL);
  url.searchParams.set('source', sourceID);
  return url.href;
}
export function readHubSourceReference(value) {
  const url = httpsURL(value);
  const sourceID = url.searchParams.get('source');
  if (!sourceID) return null;
  url.searchParams.delete('source');
  return {indexURL:url.href, sourceID};
}
export function officialCatalogIdentity(type) {
  return `${base}${type === 'plugin' ? 'plugins' : 'widgets'}/registry.json`;
}
export function catalogSourceIdentity(type, value) {
  const url = new URL(value).href;
  const folder = type === 'plugin' ? 'plugins' : 'widgets';
  const aliases = [officialCatalogIdentity(type), `${base}extensions/${folder}/registry.json`, `${base}${folder}/registry-v2.json`, hubSourceReference(HUB_INDEX_URL, OFFICIAL_HUB_ID)];
  const reference = new URL(url);
  const officialReference = reference.origin + reference.pathname === HUB_INDEX_URL &&
    [...reference.searchParams.keys()].length === 1 && reference.searchParams.get('source') === OFFICIAL_HUB_ID;
  return aliases.includes(url) || officialReference ? officialCatalogIdentity(type) : url;
}
export function catalogSourceReference(type, value) {
  return catalogSourceIdentity(type, value) === officialCatalogIdentity(type)
    ? hubSourceReference(HUB_INDEX_URL, OFFICIAL_HUB_ID) : value;
}
export function parseHubIndex(value) {
  if (!value || value.kind !== 'pudding.hub.index' || value.schemaVersion !== 1 || !Array.isArray(value.sources)) throw new Error('Unsupported Hub index');
  const ids = new Set();
  const sources = value.sources.map(source => {
    if (!source || typeof source.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_.\/-]{0,199}$/.test(source.id) || ids.has(source.id)) throw new Error('Invalid or duplicate Hub source ID');
    ids.add(source.id);
    if (!source.title || typeof source.title !== 'object' || Array.isArray(source.title) || !Object.values(source.title).length || Object.values(source.title).some(v => typeof v !== 'string' || !v.trim())) throw new Error('Hub source needs a localized title');
    const repository = httpsURL(source.repository).href;
    if (!Array.isArray(source.catalogs) || !source.catalogs.length) throw new Error('Hub source needs a catalog');
    const types = new Set();
    const catalogs = source.catalogs.map(catalog => {
      if (!catalog || !['widget','plugin'].includes(catalog.type) || types.has(catalog.type) || !Number.isSafeInteger(catalog.schemaVersion) || catalog.schemaVersion < 1) throw new Error('Each Hub source may have only one current catalog per type');
      types.add(catalog.type);
      return {type:catalog.type, schemaVersion:catalog.schemaVersion, url:httpsURL(catalog.url).href};
    });
    return {id:source.id,title:{...source.title},repository,catalogs};
  });
  return {kind:'pudding.hub.index',schemaVersion:1,sources};
}
