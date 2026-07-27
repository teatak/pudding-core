import { FileText, FolderOpen, Pause, Play, X } from "@/components/icons";
import { useRef, useState } from "react";

import type { ProjectReference } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import type { LocalFolderPath } from "@/lib/localFolders";
import { projectReferenceRangeLabel } from "@/lib/projectReferences";
import { orderedDraftItems, type DraftPartOrderItem } from "@/lib/submitParts";
import { cn } from "@/lib/utils";
import type { SessionDraftAttachment } from "@/state/sessionDraftStore";

type ComposerAttachment = SessionDraftAttachment;

type ComposerAttachmentsProps = {
  attachments: ComposerAttachment[];
  localFolders: LocalFolderPath[];
  partOrder: DraftPartOrderItem[];
  previewIndexByID: ReadonlyMap<string, number>;
  projectReferences: ProjectReference[];
  token: string;
  onPreview: (index: number) => void;
  onRemoveAttachment: (id: string) => void;
  onRemoveLocalFolder: (id: string) => void;
  onRemoveProjectReference: (id: string) => void;
  onRevealPath: (path: string) => void;
};

export function ComposerAttachments({
  attachments,
  localFolders,
  partOrder,
  previewIndexByID,
  projectReferences,
  token,
  onPreview,
  onRemoveAttachment,
  onRemoveLocalFolder,
  onRemoveProjectReference,
  onRevealPath,
}: ComposerAttachmentsProps) {
  const { t } = useI18n();
  if (attachments.length === 0 && localFolders.length === 0 && projectReferences.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap gap-2 px-3 pt-3">
      {orderedDraftItems(attachments, localFolders, partOrder, projectReferences).map((orderedItem) =>
        orderedItem.type === "attachment" ? (
          <ComposerAttachmentChip
            key={`attachment:${orderedItem.item.id}`}
            item={orderedItem.item}
            previewIndex={previewIndexByID.get(orderedItem.item.id)}
            removeLabel={t("composer.removeAttachment")}
            token={token}
            onPreview={onPreview}
            onRevealSource={onRevealPath}
            onRemove={() => onRemoveAttachment(orderedItem.item.id)}
          />
        ) : orderedItem.type === "local_folder" ? (
          <LocalFolderChip
            key={`folder:${orderedItem.item.id}`}
            folder={orderedItem.item}
            label={t("composer.folderLabel")}
            removeLabel={t("composer.removeFolder")}
            onReveal={() => onRevealPath(orderedItem.item.path)}
            onRemove={() => onRemoveLocalFolder(orderedItem.item.id)}
          />
        ) : (
          <ProjectReferenceChip
            key={`project-reference:${orderedItem.item.id}`}
            reference={orderedItem.item}
            fileLabel={t("composer.projectFileLabel")}
            folderLabel={t("composer.projectFolderLabel")}
            removeLabel={t("composer.removeProjectReference")}
            onRemove={() => onRemoveProjectReference(orderedItem.item.id)}
          />
        ),
      )}
    </div>
  );
}

function LocalFolderChip({
  folder,
  label,
  removeLabel,
  onReveal,
  onRemove,
}: {
  folder: LocalFolderPath;
  label: string;
  removeLabel: string;
  onReveal: () => void;
  onRemove: () => void;
}) {
  return (
    <div
      className="relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border border-border/70 bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm"

    >
      <button className="inline-flex min-w-0 items-center gap-1.5 text-left whitespace-nowrap" type="button" onClick={onReveal}>
        <FolderOpen className="size-4 shrink-0 text-muted-foreground" data-icon-weight="subtle" />
        <span className="min-w-0 truncate font-medium leading-5 text-foreground">{folder.name}</span>
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{label}</span>
      </button>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={onRemove}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function ProjectReferenceChip({
  reference,
  fileLabel,
  folderLabel,
  removeLabel,
  onRemove,
}: {
  reference: ProjectReference;
  fileLabel: string;
  folderLabel: string;
  removeLabel: string;
  onRemove: () => void;
}) {
  const Icon = reference.kind === "dir" ? FolderOpen : FileText;
  const range = projectReferenceRangeLabel(reference);
  return (
    <div
      className="relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border border-border/70 bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm"

    >
      <Icon className="size-4 shrink-0 text-muted-foreground" data-icon-weight="subtle" />
      <span className="min-w-0 truncate font-medium leading-5 text-foreground">{reference.name}</span>
      <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">
        {range || (reference.kind === "dir" ? folderLabel : fileLabel)}
      </span>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={onRemove}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function ComposerAttachmentChip({
  item,
  previewIndex,
  removeLabel,
  token,
  onPreview,
  onRevealSource,
  onRemove,
}: {
  item: ComposerAttachment;
  previewIndex?: number;
  removeLabel: string;
  token: string;
  onPreview: (index: number) => void;
  onRevealSource: (path: string) => void;
  onRemove: () => void;
}) {
  const src = composerAttachmentImageSource(item, token);
  const image = isImageAttachmentLike(item.attachment?.mime, item.name) && src;
  const audio = isAudioAttachmentLike(item.attachment?.mime, item.name);
  const busy = item.status === "uploading";
  if (image) {
    return (
      <div
        className={cn(
          "group relative h-16 w-16 shrink-0 overflow-hidden rounded-lg border bg-muted/40 shadow-sm",
          item.status === "error" ? "border-destructive/40" : "border-border/70",
        )}
      >
        <button
          aria-label={item.name}
          className="block h-full w-full"
          type="button"
          onClick={() => {
            if (previewIndex !== undefined) {
              onPreview(previewIndex);
            }
          }}
        >
          <img alt={item.name} className="h-full w-full object-cover" src={src} />
        </button>
        {busy ? (
          <span className="absolute inset-0 grid place-items-center bg-background/45">
            <Spinner className="size-4 text-foreground" />
          </span>
        ) : null}
        <button
          aria-label={removeLabel}
          className="absolute top-1.5 right-1.5 z-10 grid size-5 place-items-center rounded-full border border-black/10 bg-white text-black shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
          type="button"
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      </div>
    );
  }
  return (
    <div
      className={cn(
        "relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm",
        item.attachment?.sourcePath && "cursor-pointer",
        item.status === "error" ? "border-destructive/40 bg-destructive/10 text-destructive" : "border-border/70 text-muted-foreground",
      )}

      onClick={() => {
        if (item.attachment?.sourcePath) {
          onRevealSource(item.attachment.sourcePath);
        }
      }}
    >
      <span className="grid size-4 shrink-0 place-items-center text-muted-foreground">
        {busy ? (
          <Spinner className="size-4" />
        ) : audio ? (
          <AudioPreviewButton label={item.name} src={attachmentResourceURL(item.attachment, token)} />
        ) : (
          <FileText className="size-4" data-icon-weight="subtle" />
        )}
      </span>
      <span className="inline-flex min-w-0 items-center gap-1.5 whitespace-nowrap">
        <span className="truncate font-medium leading-5 text-foreground">{item.name}</span>
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{attachmentKindLabel(item.name, item.attachment?.mime)}</span>
      </span>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onRemove();
        }}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function AudioPreviewButton({ label, src }: { label: string; src: string }) {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  return (
    <>
      <button
        aria-label={label}
        className="grid size-full place-items-center rounded-sm hover:text-foreground"
        disabled={!src}
        type="button"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          const audio = audioRef.current;
          if (!audio) {
            return;
          }
          if (audio.paused) {
            void audio.play();
          } else {
            audio.pause();
          }
        }}
      >
        {playing ? <Pause className="size-4" data-icon-weight="subtle" fill="currentColor" /> : <Play className="size-4" data-icon-weight="subtle" fill="currentColor" />}
      </button>
      {src ? (
        <audio
          ref={audioRef}
          preload="none"
          src={src}
          onEnded={() => setPlaying(false)}
          onPause={() => setPlaying(false)}
          onPlay={() => setPlaying(true)}
        />
      ) : null}
    </>
  );
}

function formatAttachmentSize(size: number) {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${Math.round(size / 1024)} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function attachmentKindLabel(name: string, mime?: string) {
  const ext = name.split(".").pop()?.trim();
  if (ext && ext !== name) {
    return ext.toUpperCase();
  }
  const major = mime?.split("/")[0]?.trim();
  return major ? major.toUpperCase() : "FILE";
}

export function isImageAttachmentLike(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("image/") && cleaned !== "image/svg+xml") {
    return true;
  }
  return /\.(png|jpe?g|gif|webp)$/i.test(name);
}

function isAudioAttachmentLike(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("audio/")) {
    return true;
  }
  return /\.(wav|mp3|m4a|aac|ogg|oga|flac|webm)$/i.test(name);
}

export function composerAttachmentImageSource(item: ComposerAttachment, token: string) {
  return item.previewURL || attachmentResourceURL(item.attachment, token);
}

export function revokeAttachmentPreview(item: ComposerAttachment) {
  if (item.previewURL) {
    URL.revokeObjectURL(item.previewURL);
  }
}

