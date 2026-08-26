import {
  Camera,
  FolderOpen,
  Paperclip,
  ScanLine,
  WandSparkles,
} from "@/components/icons";
import { useEffect, useRef, type ReactNode } from "react";

import { AppIcon } from "@/components/AppIcon";
import { BuiltinAppIcon } from "@/components/AppIdentity";
import { IdentityIcon } from "@/components/IdentityIcon";
import { composerSuggestionPanelClassName } from "@/components/composerControlStyles";
import { type ComposerMentionReference } from "@/components/composerMentionReferences";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

type MentionSection = {
  id: string;
  label: string;
  entries: Array<{ index: number; reference: ComposerMentionReference }>;
};

export function ComposerMentionMenu({
  references,
  query,
  selectedIndex,
  onHover,
  onSelect,
}: {
  references: ComposerMentionReference[];
  query: string;
  selectedIndex: number;
  onHover: (index: number) => void;
  onSelect: (reference: ComposerMentionReference) => void;
}) {
  const { t } = useI18n();
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  const pointerSelectedIndexRef = useRef<number | null>(null);
  const sections = buildMentionSections(references, t);

  useEffect(() => {
    if (pointerSelectedIndexRef.current === selectedIndex) {
      return;
    }
    pointerSelectedIndexRef.current = null;
    scrollActiveIntoList(selectedRef.current, listRef.current);
  }, [references, selectedIndex]);

  return (
    <div
      className={cn(
        "absolute bottom-full z-40 w-[min(16rem,calc(100%-2rem))] text-sm",
        composerSuggestionPanelClassName,
      )}
      role="listbox"
      onContextMenu={(event) => event.preventDefault()}
    >
      {references.length === 0 ? (
        <div className="flex h-9 items-center px-3 text-xs text-muted-foreground">{t("composer.mentionNoMatch")}</div>
      ) : (
        <div
          ref={listRef}
          className="grid max-h-[20.25rem] gap-1 overflow-y-auto p-1"
        >
          {sections.map((section) => (
            <div key={section.id} className="contents">
              {section.label ? (
                <div className="flex h-9 items-center rounded-md px-2 text-[11px] font-medium text-muted-foreground">
                  {section.label}
                </div>
              ) : null}
              {section.entries.map(({ index, reference }) => (
                <button
                  key={`${reference.kind}:${reference.id}`}
                  ref={index === selectedIndex ? selectedRef : undefined}
                  aria-selected={index === selectedIndex}
                  className={cn(
                    "flex h-9 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left hover:bg-interactive-hover active:bg-interactive-pressed",
                    index === selectedIndex &&
                      "bg-interactive-selected text-foreground hover:bg-interactive-selected",
                  )}
                  role="option"
                  type="button"
                  onMouseEnter={() => {
                    if (index !== selectedIndex) {
                      pointerSelectedIndexRef.current = index;
                    }
                    onHover(index);
                  }}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    if (event.button !== 0) {
                      return;
                    }
                    onSelect(reference);
                  }}
                >
                  <MentionIcon reference={reference} />
                  <span className="min-w-0 flex-1 truncate font-medium">{reference.label}</span>
                </button>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function buildMentionSections(references: ComposerMentionReference[], t: (key: string) => string): MentionSection[] {
  const entries = references.map((reference, index) => ({ index, reference }));
  const actionEntries = entries.filter(({ reference }) => reference.kind === "action");
  const groupedSections = [
    { id: "app", label: t("composer.mentionAppLabel"), entries: entries.filter(({ reference }) => reference.kind === "app") },
    { id: "skill", label: t("composer.mentionSkillLabel"), entries: entries.filter(({ reference }) => reference.kind === "skill") },
  ].filter((section) => section.entries.length > 0);
  if (actionEntries.length === 0) {
    return groupedSections;
  }
  return [{ id: "actions", label: "", entries: actionEntries }, ...groupedSections];
}

function MentionIcon({ reference }: { reference: ComposerMentionReference }) {
  if (reference.appSource === "builtin" && reference.appID) {
    return <BuiltinAppIcon appID={reference.appID} size="xs" />;
  }
  if (reference.kind === "app") {
    return <AppIcon icon={reference.appIcon} size="xs" src={reference.iconURL} />;
  }
  if (reference.iconURL && reference.appIcon) {
    return <AppIcon icon={reference.appIcon} size="xs" src={reference.iconURL} />;
  }
  if (reference.iconURL) {
    return <IdentityIcon fallback={reference.kind === "skill" ? "skill" : "app"} fit="contain" size="xs" src={reference.iconURL} />;
  }
  if (reference.kind === "skill") {
    return <ColoredMentionIcon tone="info" icon={<WandSparkles className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "folder") {
    return <ColoredMentionIcon tone="amber" icon={<FolderOpen className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "screenshot") {
    return <ColoredMentionIcon tone="info" icon={<ScanLine className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "photo") {
    return <ColoredMentionIcon tone="emerald" icon={<Camera className="size-3.5" />} />;
  }
  return <ColoredMentionIcon tone="sky" icon={<Paperclip className="size-3.5" />} />;
}

function ColoredMentionIcon({ icon, tone }: { icon: ReactNode; tone: "amber" | "emerald" | "info" | "sky" }) {
  const toneClass = {
    amber: "bg-amber-600 text-white",
    emerald: "bg-emerald-600 text-white",
    info: "bg-info text-info-foreground",
    sky: "bg-sky-600 text-white",
  }[tone];
  return <span className={cn("grid size-5 shrink-0 place-items-center rounded", toneClass)}>{icon}</span>;
}

function scrollActiveIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeRect = active.getBoundingClientRect();
  const listRect = list.getBoundingClientRect();
  const padding = 4;
  if (activeRect.top < listRect.top + padding) {
    list.scrollTop -= listRect.top + padding - activeRect.top;
  } else if (activeRect.bottom > listRect.bottom - padding) {
    list.scrollTop += activeRect.bottom - (listRect.bottom - padding);
  }
}
