export type SettingsSectionID =
  | "dialogue"
  | "model"
  | "voice"
  | "skills"
  | "tools"
	| "permissions"
  | "browser"
  | "usage"
  | "archives"
  | "advanced"
  | "about";

export type SettingsDialogOpenDetail = {
  section?: SettingsSectionID;
  createProvider?: boolean;
};

export const SETTINGS_DIALOG_OPEN_EVENT = "pudding:settings-dialog-open";

export function openSettingsDialog(detail: SettingsDialogOpenDetail = {}) {
  window.dispatchEvent(new CustomEvent<SettingsDialogOpenDetail>(SETTINGS_DIALOG_OPEN_EVENT, { detail }));
}
