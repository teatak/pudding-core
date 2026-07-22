import {
  Camera,
  FolderOpen,
  Paperclip,
  ScanLine,
  WandSparkles,
} from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";

import { AppIcon } from "@/components/AppIcon";
import { BuiltinAppIcon } from "@/components/AppIdentity";
import { IdentityIcon } from "@/components/IdentityIcon";
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
  align = "default",
  selectedIndex,
  onHover,
  onSelect,
}: {
  references: ComposerMentionReference[];
  query: string;
  align?: "default" | "start";
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
        "pudding-composer-suggestion absolute bottom-[calc(100%-3px)] z-[5] w-[min(18rem,calc(100%-2rem))] overflow-hidden rounded-t-lg border border-b-0 bg-popover/95 text-sm text-popover-foreground backdrop-blur",
        align === "start" ? "left-4" : "left-16",
      )}
      role="listbox"
      onContextMenu={(event) => event.preventDefault()}
    >
      {references.length === 0 ? (
        <div className="px-3 py-2 text-xs text-muted-foreground">{t("composer.mentionNoMatch")}</div>
      ) : (
        <div ref={listRef} className="max-h-72 overflow-y-auto p-1">
          {sections.map((section) => (
            <div key={section.id} className="py-1 first:pt-0">
              {section.label ? <div className="px-2 py-1 text-[11px] font-medium text-muted-foreground">{section.label}</div> : null}
              {section.entries.map(({ index, reference }) => (
                <button
                  key={`${reference.kind}:${reference.id}`}
                  ref={index === selectedIndex ? selectedRef : undefined}
                  aria-selected={index === selectedIndex}
                  className={cn(
                    "flex h-9 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left hover:bg-muted",
                    index === selectedIndex && "bg-muted text-foreground",
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
    return <ColoredMentionIcon tone="violet" icon={<WandSparkles className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "folder") {
    return <ColoredMentionIcon tone="amber" icon={<FolderOpen className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "screenshot") {
    return <ColoredMentionIcon tone="indigo" icon={<ScanLine className="size-3.5" />} />;
  }
  if (reference.kind === "action" && reference.actionID === "photo") {
    return <ColoredMentionIcon tone="emerald" icon={<Camera className="size-3.5" />} />;
  }
  return <ColoredMentionIcon tone="sky" icon={<Paperclip className="size-3.5" />} />;
}

function ColoredMentionIcon({ icon, tone }: { icon: ReactNode; tone: "amber" | "emerald" | "indigo" | "rose" | "sky" | "slate" | "violet" }) {
  const toneClass = {
    amber: "bg-amber-600 text-white",
    emerald: "bg-emerald-600 text-white",
    indigo: "bg-indigo-600 text-white",
    rose: "bg-rose-600 text-white",
    sky: "bg-sky-600 text-white",
    slate: "bg-neutral-600 text-white",
    violet: "bg-violet-600 text-white",
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
