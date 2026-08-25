import { Captions, Check, ChevronDown, ChevronUp, Clock3, CornerDownLeft, FileText, FolderOpen, Mic, Pause, Play, Pencil, Trash2, X } from "@/components/icons";
import { memo, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";

import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { revealDesktopPath } from "@/api/client";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { projectReferenceRangeLabel } from "@/lib/projectReferences";
import { cn } from "@/lib/utils";

import { InterruptedBadge, MessageMeta } from "./MessageMeta";
import { FormResultCard, formResultFromContentParts } from "./FormResultCard";
import { uiContextFromContentParts, type TurnDisclosureState, type UserInputVM } from "./types";

const COLLAPSED_USER_TEXT_HEIGHT = 240;

export const UserInput = memo(function UserInput({
  disclosure,
  disclosureKey,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSteer,
  onQueuedSave,
  token,
  user,
}: {
  disclosure?: TurnDisclosureState;
  disclosureKey: string;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSteer?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  token: string;
  user: UserInputVM;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState(user.text);
  const [editing, setEditing] = useState(false);
  const [imagePreviewIndex, setImagePreviewIndex] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const clientMessageID = user.clientMessageID;
  const attachments = user.attachments || [];
  const voiceAudioAttachments = attachments.filter((attachment) => isAudioAttachment(attachment.mime, attachment.name) && isVoiceAudioAttachment(attachment));
  const contentAttachments = attachments.filter((attachment) => !isVoiceAudioAttachment(attachment));
  const imageAttachments = contentAttachments.filter((attachment) => isImageAttachment(attachment.mime, attachment.name));
  const localFolders = user.localFolders || [];
  const projectReferences = user.projectReferences || [];
  const orderedItems = orderedUserInputItems(contentAttachments, localFolders, projectReferences, user.parts);
  const formResult = formResultFromContentParts(user.parts);
  const uiContext = uiContextFromContentParts(user.parts);
  const imagePreviewItems: ImageLightboxItem[] = imageAttachments.map((attachment) => ({
    id: attachment.id,
    name: attachment.name,
    size: attachment.size,
    url: attachmentResourceURL(attachment, token),
  }));
  const imagePreviewIndexByID = new Map(imagePreviewItems.map((item, index) => [item.id, index]));
  const metaText =
    user.text ||
    contentAttachments
      .map((attachment) => attachment.name)
      .concat(localFolders.map((folder) => folder.path), projectReferences.map((reference) => reference.path))
      .join("\n");
  const canManageQueued =
    Boolean(clientMessageID && user.pending && (user.status === "queued" || user.status === "editing")) &&
    Boolean(onQueuedEditStart && onQueuedSave && onQueuedCancel);
  const voiceAudioAttachment = !editing ? voiceAudioAttachments[0] : undefined;
  const rawInput = isRawVoiceClientMessageID(clientMessageID) || voiceAudioAttachment?.origin === VOICE_AUDIO_ORIGIN;
  const asrInput = isASRClientMessageID(clientMessageID) || rawInput;
  const canEditQueued = canManageQueued && !rawInput && !formResult;
  const canSteerQueued = Boolean(clientMessageID && user.pending && user.status === "queued" && onQueuedSteer);

  useEffect(() => {
    if (!editing) {
      setDraft(user.text);
    }
  }, [editing, user.text]);
  useEffect(() => {
    if (imagePreviewIndex !== null && imagePreviewIndex >= imagePreviewItems.length) {
      setImagePreviewIndex(null);
    }
  }, [imagePreviewIndex, imagePreviewItems.length]);

  async function startEdit() {
    if (!clientMessageID || !onQueuedEditStart) {
      return;
    }
    setDraft(user.text);
    setEditing(true);
    setSaving(true);
    try {
      await onQueuedEditStart(clientMessageID);
    } catch {
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  async function saveEdit(text: string) {
    const next = text.trim();
    if (!clientMessageID || !onQueuedSave || !next) {
      return;
    }
    setSaving(true);
    try {
      await onQueuedSave(clientMessageID, next);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  async function discardEdit() {
    if (!clientMessageID || !onQueuedSave) {
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      await onQueuedSave(clientMessageID, user.text);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  async function cancelQueued() {
    if (!clientMessageID || !onQueuedCancel) {
      return;
    }
    setSaving(true);
    try {
      await onQueuedCancel(clientMessageID);
    } finally {
      setSaving(false);
    }
  }

  async function steerQueued() {
    if (!clientMessageID || !onQueuedSteer) {
      return;
    }
    setSaving(true);
    try {
      await onQueuedSteer(clientMessageID);
    } finally {
      setSaving(false);
    }
  }

  function revealLocalPath(path: string) {
    if (!path.trim()) {
      return;
    }
    void revealDesktopPath(token, path).catch(() => {});
  }

  const actions = !editing ? (
    <>
      {voiceAudioAttachment ? <VoiceAudioPlaybackButton attachment={voiceAudioAttachment} token={token} /> : null}
      {canManageQueued ? (
        <>
          {canSteerQueued ? (
            <MetaIconButton label={t("transcript.guideQueued")} disabled={saving} onClick={steerQueued}>
              {saving ? <Spinner /> : <CornerDownLeft />}
            </MetaIconButton>
          ) : null}
          {canEditQueued ? (
            <MetaIconButton label={t("transcript.editQueued")} disabled={saving} onClick={startEdit}>
              <Pencil />
            </MetaIconButton>
          ) : null}
          <MetaIconButton label={t("transcript.cancelQueued")} disabled={saving} onClick={cancelQueued}>
            {saving ? <Spinner /> : <Trash2 />}
          </MetaIconButton>
        </>
      ) : null}
    </>
  ) : null;
  const queuedStatus =
    user.pending && (user.status === "queued" || user.status === "editing") ? (
      <span className="inline-flex shrink-0 items-center gap-1">
        <Clock3 className="size-3.5" aria-hidden="true" />
        <span>{t("transcript.queued")}</span>
      </span>
    ) : null;
  const showASRIndicator = asrInput || Boolean(voiceAudioAttachment);
  const userInputItemCards = editing
    ? []
    : orderedItems.map((item) => {
        if (item.type === "local_folder") {
          return (
            <LocalFolderCard
              key={`folder:${item.item.id}`}
              label={t("composer.folderLabel")}
              name={item.item.name}
              path={item.item.path}
              onReveal={() => revealLocalPath(item.item.path)}
            />
          );
        }
        if (item.type === "project_reference") {
          return (
            <ProjectReferenceCard
              key={`project-reference:${item.item.id}`}
              reference={item.item}
              fileLabel={t("composer.projectFileLabel")}
              folderLabel={t("composer.projectFolderLabel")}
            />
          );
        }
        const attachment = item.item;
        if (isImageAttachment(attachment.mime, attachment.name)) {
          const imageIndex = imagePreviewIndexByID.get(attachment.id);
          if (imageIndex === undefined) {
            return null;
          }
          const image = imagePreviewItems[imageIndex];
          return image ? (
            <ImageAttachmentButton
              key={`attachment:${attachment.id}`}
              image={image}
              onOpen={() => setImagePreviewIndex(imageIndex)}
            />
          ) : null;
        }
        if (isAudioAttachment(attachment.mime, attachment.name)) {
          return <AudioAttachmentCard key={`attachment:${attachment.id}`} attachment={attachment} token={token} />;
        }
        const content = (
          <>
            <FileText className="size-3 shrink-0" />
            <span className="min-w-0 truncate">{attachment.name}</span>
            {attachment.size > 0 ? (
              <span className="shrink-0 text-muted-foreground/70">{formatAttachmentSize(attachment.size)}</span>
            ) : null}
          </>
        );
        const className =
          "inline-flex max-w-full items-center gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1 text-xs leading-5 no-underline hover:bg-muted";
        if (attachment.sourcePath) {
          return (
            <button
              key={`attachment:${attachment.id}`}
              className={className}
              type="button"
              onClick={() => revealLocalPath(attachment.sourcePath || "")}
            >
              {content}
            </button>
          );
        }
        return (
          <a
            key={`attachment:${attachment.id}`}
            className={className}
            href={attachmentResourceURL(attachment, token)}
            rel="noreferrer"
            target="_blank"
          >
            {content}
          </a>
        );
      });
  const showUserBubble = editing || Boolean(formResult || user.text || showASRIndicator || user.interrupted);

  return (
    <>
      <div className="group flex min-w-0 flex-col items-end" data-transcript-message-role="user">
        {userInputItemCards.length > 0 ? (
          <div className="flex max-w-[min(82%,42rem)] flex-wrap justify-end gap-1.5">{userInputItemCards}</div>
        ) : null}
        {showUserBubble ? (
          <div className={cn("flex w-full min-w-0 items-start justify-end gap-1", userInputItemCards.length > 0 && "mt-2")}>
            <div
              className={cn(
                "pudding-user-message selectable-text min-w-0 overflow-hidden rounded-[14px] rounded-br-[5px] border-0 px-3.5 py-2 text-left text-sm leading-6 break-words whitespace-pre-wrap shadow-none [overflow-wrap:anywhere]",
                formResult ? "w-[min(82%,42rem)]" : "max-w-[min(82%,42rem)]",
              )}
            >
              {editing ? (
                <div className="grid gap-2">
                  <Textarea
                    className="min-h-20 resize-y border-0 bg-transparent p-0 text-sm leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                  />
                  <div className="flex justify-end gap-1">
                    <MetaIconButton label={t("common.cancel")} disabled={saving} onClick={discardEdit}>
                      <X />
                    </MetaIconButton>
                    <MetaIconButton label={t("common.save")} disabled={saving || !draft.trim()} onClick={() => saveEdit(draft)}>
                      {saving ? <Spinner /> : <Check className="text-success" />}
                    </MetaIconButton>
                  </div>
                </div>
              ) : (
                <div className="grid min-w-0 max-w-full gap-2">
                  {formResult ? <FormResultCard part={formResult} /> : null}
                  {(!formResult && user.text) || showASRIndicator ? (
                    <CollapsibleUserText
                      key={disclosureKey}
                      disclosure={disclosure}
                      disclosureKey={disclosureKey}
                      messageID={user.messageID}
                    >
                      {showASRIndicator ? <ASRIndicator rawInput={rawInput} /> : null}
                      {!formResult ? user.text : null}
                    </CollapsibleUserText>
                  ) : null}
                  {user.interrupted ? <InterruptedBadge /> : null}
                </div>
              )}
            </div>
          </div>
        ) : null}
        {user.createdAt ? (
          <div className="mt-1">
            <MessageMeta
              actions={actions}
              align="end"
              createdAt={user.createdAt}
              hideStandardDetails={Boolean(queuedStatus)}
              persistentStatus={queuedStatus}
              text={metaText}
              uiContext={uiContext}
            />
          </div>
        ) : null}
      </div>
      <ImageLightbox images={imagePreviewItems} openIndex={imagePreviewIndex} onOpenIndexChange={setImagePreviewIndex} />
    </>
  );
});

function CollapsibleUserText({
  children,
  disclosure,
  disclosureKey,
  messageID,
}: {
  children: ReactNode;
  disclosure?: TurnDisclosureState;
  disclosureKey: string;
  messageID?: string;
}) {
  const { t } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const collapseAnchorRef = useRef<{
    bottom: number;
    scrollElement: HTMLElement;
  } | null>(null);
  const [collapsible, setCollapsible] = useState(false);
  const [expanded, setExpanded] = useState(() => disclosure?.isOpen(disclosureKey) || false);

  useLayoutEffect(() => {
    const content = contentRef.current;
    if (!content) {
      return;
    }
    const update = () => setCollapsible(content.getBoundingClientRect().height > COLLAPSED_USER_TEXT_HEIGHT + 1);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(content);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const anchor = collapseAnchorRef.current;
    const root = rootRef.current;
    if (!anchor || !root || expanded) {
      return;
    }

    anchor.scrollElement.scrollTop += root.getBoundingClientRect().bottom - anchor.bottom;
    collapseAnchorRef.current = null;
  }, [expanded]);

  function setOpen(open: boolean) {
    if (!open) {
      const root = rootRef.current;
      const scrollElement = root?.closest<HTMLElement>("[data-transcript-viewport]");
      if (root && scrollElement) {
        collapseAnchorRef.current = {
          bottom: root.getBoundingClientRect().bottom,
          scrollElement,
        };
      }
    }
    setExpanded(open);
    disclosure?.setOpen(disclosureKey, open);
  }

  const collapsed = collapsible && !expanded;
  return (
    <div
      ref={rootRef}
      className="relative min-w-0 max-w-full [overflow-wrap:anywhere]"
      data-transcript-message-id={messageID}
    >
      <div
        className={cn(
          "min-w-0 max-w-full",
          collapsed &&
            "overflow-hidden [mask-image:linear-gradient(to_bottom,#000_0,#000_calc(100%-2rem),transparent_100%)]",
        )}
        style={collapsed ? { maxHeight: COLLAPSED_USER_TEXT_HEIGHT } : undefined}
      >
        <div ref={contentRef}>{children}</div>
      </div>
      {collapsed ? (
        <button
          aria-expanded="false"
          aria-label={t("transcript.userMessageExpand")}
          className="absolute inset-0 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-ring/70"
          type="button"
          onClick={() => setOpen(true)}
        >
          <ChevronDown
            className="absolute bottom-0 right-0 size-4 text-muted-foreground/70"
            aria-hidden="true"
          />
        </button>
      ) : collapsible ? (
        <button
          aria-expanded="true"
          aria-label={t("transcript.userMessageCollapse")}
          className="ml-auto flex size-5 items-center justify-center rounded text-muted-foreground/70 outline-none hover:bg-foreground/5 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/70"
          type="button"
          onClick={() => setOpen(false)}
        >
          <ChevronUp className="size-4" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}

function isASRClientMessageID(clientMessageID: string | undefined) {
  return Boolean(clientMessageID?.startsWith("audmsg"));
}

function isRawVoiceClientMessageID(clientMessageID: string | undefined) {
  return Boolean(clientMessageID?.startsWith("voicemsg"));
}

type UserAttachment = NonNullable<UserInputVM["attachments"]>[number];
type UserLocalFolder = NonNullable<UserInputVM["localFolders"]>[number];
type UserProjectReference = NonNullable<UserInputVM["projectReferences"]>[number];
type OrderedUserItem =
  | { type: "attachment"; item: UserAttachment }
  | { type: "local_folder"; item: UserLocalFolder }
  | { type: "project_reference"; item: UserProjectReference };

const ASR_AUDIO_ORIGIN = "asr_audio";
const VOICE_AUDIO_ORIGIN = "voice_audio";

function isVoiceAudioAttachment(attachment: UserAttachment) {
  return attachment.origin === ASR_AUDIO_ORIGIN || attachment.origin === VOICE_AUDIO_ORIGIN;
}

function orderedUserInputItems(
  attachments: UserAttachment[],
  localFolders: UserLocalFolder[],
  projectReferences: UserProjectReference[],
  parts: UserInputVM["parts"],
): OrderedUserItem[] {
  if (!parts || parts.length === 0) {
    return [
      ...attachments.map((item) => ({ type: "attachment" as const, item })),
      ...localFolders.map((item) => ({ type: "local_folder" as const, item })),
      ...projectReferences.map((item) => ({ type: "project_reference" as const, item })),
    ];
  }
  const attachmentsByID = new Map(attachments.map((item) => [item.id, item]));
  const foldersByID = new Map(localFolders.map((item) => [item.id, item]));
  const projectReferencesByID = new Map(projectReferences.map((item) => [item.id, item]));
  const seenAttachments = new Set<string>();
  const seenFolders = new Set<string>();
  const seenProjectReferences = new Set<string>();
  const out: OrderedUserItem[] = [];
  for (const part of parts) {
    if (part.type === "attachment") {
      const attachment = attachmentsByID.get(part.id);
      if (attachment && !seenAttachments.has(attachment.id)) {
        seenAttachments.add(attachment.id);
        out.push({ type: "attachment", item: attachment });
      }
      continue;
    }
    if (part.type === "local_folder") {
      const folder = foldersByID.get(part.id);
      if (folder && !seenFolders.has(folder.id)) {
        seenFolders.add(folder.id);
        out.push({ type: "local_folder", item: folder });
      }
      continue;
    }
    if (part.type === "project_reference") {
      const reference = projectReferencesByID.get(part.id);
      if (reference && !seenProjectReferences.has(reference.id)) {
        seenProjectReferences.add(reference.id);
        out.push({ type: "project_reference", item: reference });
      }
    }
  }
  for (const item of attachments) {
    if (!seenAttachments.has(item.id)) {
      out.push({ type: "attachment", item });
    }
  }
  for (const item of localFolders) {
    if (!seenFolders.has(item.id)) {
      out.push({ type: "local_folder", item });
    }
  }
  for (const item of projectReferences) {
    if (!seenProjectReferences.has(item.id)) {
      out.push({ type: "project_reference", item });
    }
  }
  return out;
}

function ProjectReferenceCard({
  reference,
  fileLabel,
  folderLabel,
}: {
  reference: UserProjectReference;
  fileLabel: string;
  folderLabel: string;
}) {
  const Icon = reference.kind === "dir" ? FolderOpen : FileText;
  const range = projectReferenceRangeLabel(reference);
  return (
    <div
      className="inline-flex h-8 max-w-full items-center gap-1.5 rounded-md border border-border/70 bg-background/70 px-2 text-xs leading-5 whitespace-nowrap"

    >
      <Icon className="size-3 shrink-0 text-muted-foreground" data-icon-weight="subtle" />
      <span className="min-w-0 truncate whitespace-nowrap">{reference.name}</span>
      <span className="shrink-0 whitespace-nowrap text-muted-foreground/70">
        {range || (reference.kind === "dir" ? folderLabel : fileLabel)}
      </span>
    </div>
  );
}

function LocalFolderCard({ label, name, path, onReveal }: { label: string; name: string; path: string; onReveal: () => void }) {
  return (
    <button
      className="inline-flex h-8 max-w-full items-center gap-1.5 rounded-md border border-border/70 bg-background/70 px-2 text-xs leading-5 whitespace-nowrap hover:bg-muted"

      type="button"
      onClick={onReveal}
    >
      <FolderOpen className="size-3 shrink-0 text-muted-foreground" data-icon-weight="subtle" />
      <span className="min-w-0 truncate whitespace-nowrap">{name}</span>
      <span className="shrink-0 whitespace-nowrap text-muted-foreground/70">{label}</span>
    </button>
  );
}

function AudioAttachmentCard({ attachment, token }: { attachment: UserAttachment; token: string }) {
  const src = attachmentResourceURL(attachment, token);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  return (
    <div
      className="inline-flex h-16 min-w-44 max-w-full items-center gap-2 rounded-lg border border-border/70 bg-card px-2.5 text-sm shadow-sm"

    >
      <button
        aria-label={attachment.name}
        className="grid size-10 shrink-0 place-items-center rounded-md bg-muted/70 text-muted-foreground hover:bg-muted hover:text-foreground"
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
        {playing ? <Pause className="size-5" data-icon-weight="subtle" fill="currentColor" /> : <Play className="size-5" data-icon-weight="subtle" fill="currentColor" />}
      </button>
      <span className="flex min-w-0 max-w-52 flex-1 flex-col justify-center">
        <span className="truncate font-medium leading-5 text-foreground">{attachment.name}</span>
        {attachment.size > 0 ? (
          <span className="truncate text-xs text-muted-foreground">{formatAttachmentSize(attachment.size)}</span>
        ) : null}
      </span>
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
    </div>
  );
}

function ASRIndicator({ rawInput }: { rawInput: boolean }) {
  const { t } = useI18n();
  const label = rawInput ? t("transcript.rawTranscript") : t("transcript.asrInput");
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span aria-label={label} className="mr-1 inline-flex align-[-0.15em] text-muted-foreground" role="img">
          {rawInput ? (
            <Mic aria-hidden="true" className="size-3.5" data-icon-weight="subtle" />
          ) : (
            <Captions aria-hidden="true" className="size-3.5" data-icon-weight="subtle" />
          )}
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function VoiceAudioPlaybackButton({ attachment, token }: { attachment: UserAttachment; token: string }) {
  const { t } = useI18n();
  const src = attachmentResourceURL(attachment, token);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  const label = playing ? t("transcript.pauseOriginalAudio") : t("transcript.playOriginalAudio");
  return (
    <>
      <MetaIconButton
        disabled={!src}
        label={label}
        onClick={() => {
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
        {playing ? <Pause fill="currentColor" /> : <Play fill="currentColor" />}
      </MetaIconButton>
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

function ImageAttachmentButton({ image, onOpen }: { image: ImageLightboxItem; onOpen: () => void }) {
  return (
    <button
      className="block h-20 w-24 overflow-hidden rounded-md border border-border/70 bg-muted/40"

      type="button"
      onClick={onOpen}
    >
      <img alt={image.name} className="h-full w-full object-contain" src={image.url} />
    </button>
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

function isImageAttachment(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("image/") && cleaned !== "image/svg+xml") {
    return true;
  }
  return /\.(png|jpe?g|gif|webp)$/i.test(name);
}

function isAudioAttachment(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("audio/")) {
    return true;
  }
  return /\.(wav|mp3|m4a|aac|ogg|oga|flac|webm)$/i.test(name);
}

function MetaIconButton({
  children,
  disabled,
  label,
  onClick,
}: {
  children: ReactNode;
  disabled?: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={label}
          className="size-6 bg-transparent hover:bg-muted dark:hover:bg-muted/50 active:translate-y-0 [&>[data-slot=spinner]]:size-3"
          disabled={disabled}
          size="icon-xs"
          type="button"
          variant="ghost"
          onClick={onClick}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
