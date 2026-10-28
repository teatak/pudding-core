export type HubCatalogType = 'widget' | 'plugin';
export interface HubCatalog { type: HubCatalogType; schemaVersion: number; url: string }
export interface HubSource { id: string; title: Record<string,string>; repository: string; catalogs: HubCatalog[] }
export interface HubIndex { kind: 'pudding.hub.index'; schemaVersion: 1; sources: HubSource[] }
export const HUB_INDEX_URL: string;
export const OFFICIAL_HUB_ID: string;
export function httpsURL(value: string): URL;
export function hubSourceReference(indexURL: string, sourceID: string): string;
export function readHubSourceReference(value: string): {indexURL: string; sourceID: string} | null;
export function officialCatalogIdentity(type: HubCatalogType): string;
export function catalogSourceIdentity(type: HubCatalogType, value: string): string;
export function catalogSourceReference(type: HubCatalogType, value: string): string;
export function parseHubIndex(value: unknown): HubIndex;
