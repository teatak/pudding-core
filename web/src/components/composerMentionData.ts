import { appIconURL, skillIconURL, type AppDefinition, type Skill } from "@/api/client";
import { type ComposerMentionReference } from "@/components/composerMentionReferences";

export function buildComposerMentionReferences({
  apps,
  skills,
  t,
  token,
}: {
  apps: AppDefinition[];
  skills: Skill[];
  t: (key: string) => string;
  token: string;
}): ComposerMentionReference[] {
  return [
    {
      kind: "action",
      id: "files",
      label: t("composer.attach"),
      description: t("composer.mentionAttachDesc"),
      insertText: "",
      actionID: "files",
    },
    {
      kind: "action",
      id: "folder",
      label: t("composer.attachFolder"),
      description: t("composer.mentionFolderDesc"),
      insertText: "",
      actionID: "folder",
    },
    {
      kind: "action",
      id: "screenshot",
      label: t("composer.screenshot"),
      description: t("composer.mentionScreenshotDesc"),
      insertText: "",
      actionID: "screenshot",
    },
    {
      kind: "action",
      id: "photo",
      label: t("composer.photo"),
      description: t("composer.mentionPhotoDesc"),
      insertText: "",
      actionID: "photo",
    },
    ...apps.map((app) => appToMentionReference(app, token)),
    ...apps.flatMap((app) => appSkillMentionReferences(app, token)),
    ...skills.map((skill) => skillToMentionReference(skill, token)),
  ];
}

function appToMentionReference(app: AppDefinition, token: string): ComposerMentionReference {
  return {
    kind: "app",
    id: app.id,
    label: app.name || app.id,
    description: app.description || app.name,
    insertText: `@app/${app.id}`,
    appID: app.id,
    appSource: app.source,
    appIcon: app.icon,
    iconURL: appIconURL(token, app),
  };
}

function appSkillMentionReferences(app: AppDefinition, token: string): ComposerMentionReference[] {
  const iconURL = appIconURL(token, app);
  return (app.skills ?? []).map((skill) => {
    const id = skill.id || skill.name || skill.path;
    return {
      kind: "skill",
      id: `${app.id}/${id}`,
      label: `${app.name || app.id} · ${skill.name || id}`,
      description: skill.description || `${app.name} · ${skill.path}`,
      insertText: `@skill/${app.id}/${id}`,
      appID: app.id,
      appSource: app.source,
      appIcon: app.icon,
      iconURL,
    };
  });
}

function skillToMentionReference(skill: Skill, token: string): ComposerMentionReference {
  return {
    kind: "skill",
    id: skill.id,
    label: skill.name || skill.id,
    description: skill.description || skill.name,
    insertText: `@skill/${skill.id}`,
    iconURL: skillIconURL(token, skill),
  };
}
