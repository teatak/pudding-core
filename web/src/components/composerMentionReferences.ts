export type ComposerMentionActionID = "files" | "folder";
export type ComposerMentionKind = "action" | "app" | "skill";
export type ComposerMentionAppIcon = {
  svg?: string;
  color?: {
    light?: string;
    dark?: string;
  };
  background?: {
    light?: string;
    dark?: string;
  };
};

export type ComposerMentionReference = {
  kind: ComposerMentionKind;
  id: string;
  label: string;
  description: string;
  insertText: string;
  actionID?: ComposerMentionActionID;
  appIcon?: ComposerMentionAppIcon;
  iconURL?: string;
  keepOpen?: boolean;
};

export type ComposerMentionTrigger = {
  start: number;
  end: number;
  query: string;
};

export function findComposerMentionTrigger(text: string, cursor: number): ComposerMentionTrigger | null {
  const end = Math.max(0, Math.min(cursor, text.length));
  let start = end;
  while (start > 0 && !/\s/.test(text[start - 1] ?? "")) {
    start -= 1;
  }
  const token = text.slice(start, end);
  if (!token.startsWith("@")) {
    return null;
  }
  if (token.slice(1).includes("@")) {
    return null;
  }
  return { start, end, query: token.slice(1) };
}

export function filterComposerMentionReferences(
  references: ComposerMentionReference[],
  query: string,
): ComposerMentionReference[] {
  const raw = query.trim().toLowerCase();
  if (raw === "") {
    return references;
  }

  const slash = raw.indexOf("/");
  if (slash >= 0) {
    const namespace = raw.slice(0, slash);
    const term = raw.slice(slash + 1);
    return references.filter((reference) => {
      if (reference.kind !== namespace) {
        return false;
      }
      if (term === "") {
        return true;
      }
      return (
        reference.id.toLowerCase().includes(term) ||
        reference.label.toLowerCase().includes(term) ||
        reference.description.toLowerCase().includes(term)
      );
    });
  }

  if (raw === "app" || raw === "skill") {
    return references.filter((reference) => reference.kind === raw);
  }

  return references.filter((reference) => referenceMatchesQuery(reference, raw));
}

function referenceMatchesQuery(reference: ComposerMentionReference, query: string) {
  return (
    reference.id.toLowerCase().includes(query) ||
    reference.label.toLowerCase().includes(query) ||
    reference.description.toLowerCase().includes(query) ||
    reference.kind.toLowerCase().includes(query)
  );
}
