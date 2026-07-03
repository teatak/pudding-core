import { Check, Loader2, Paperclip, Pencil, Trash2, X } from "lucide-react";
import { memo, useEffect, useState, type ReactNode } from "react";

import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { cn } from "@/lib/utils";

import { InterruptedBadge, MessageMeta } from "./MessageMeta";
import type { UserInputVM } from "./types";

export const UserInput = memo(function UserInput({
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  token,
  user,
}: {
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
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
  const imageAttachments = attachments.filter((attachment) => isImageAttachment(attachment.mime, attachment.name));
  const fileAttachments = attachments.filter((attachment) => !isImageAttachment(attachment.mime, attachment.name));
  const imagePreviewItems: ImageLightboxItem[] = imageAttachments.map((attachment) => ({
    id: attachment.id,
    name: attachment.name,
    size: attachment.size,
    url: attachmentResourceURL(attachment, token),
  }));
  const metaText = user.text || attachments.map((attachment) => attachment.name).join("\n");
  const canEditQueued =
    Boolean(clientMessageID && user.pending && (user.status === "queued" || user.status === "editing")) &&
    Boolean(onQueuedEditStart && onQueuedSave && onQueuedCancel);

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

  const actions =
    canEditQueued && !editing ? (
      <>
        <MetaIconButton label={t("transcript.editQueued")} disabled={saving} onClick={startEdit}>
          <Pencil />
        </MetaIconButton>
        <MetaIconButton label={t("transcript.cancelQueued")} disabled={saving} onClick={cancelQueued}>
          {saving ? <Loader2 className="animate-spin" /> : <Trash2 />}
        </MetaIconButton>
      </>
    ) : null;

  return (
    <>
      <div className={cn("group flex flex-col items-end", user.pending && "opacity-70")}>
        <div className="pudding-user-message selectable-text min-w-0 max-w-[min(82%,42rem)] rounded-2xl rounded-br-md border border-border/60 px-3 py-2 text-left text-sm leading-6 break-words whitespace-pre-wrap shadow-sm">
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
                  {saving ? <Loader2 className="animate-spin" /> : <Check className="text-success" />}
                </MetaIconButton>
              </div>
            </div>
          ) : (
            <div className="grid gap-2">
              {user.text ? <div>{user.text}</div> : null}
              {imagePreviewItems.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {imagePreviewItems.map((image, index) => (
                    <ImageAttachmentButton
                      key={image.id}
                      image={image}
                      onOpen={() => setImagePreviewIndex(index)}
                    />
                  ))}
                </div>
              ) : null}
              {fileAttachments.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {fileAttachments.map((attachment) => (
                    <a
                      key={attachment.id}
                      className="inline-flex max-w-full items-center gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1 text-xs leading-5 no-underline hover:bg-muted"
                      href={attachmentResourceURL(attachment, token)}
                      rel="noreferrer"
                      target="_blank"
                    >
                      <Paperclip className="size-3 shrink-0" />
                      <span className="min-w-0 truncate">{attachment.name}</span>
                      {attachment.size > 0 ? (
                        <span className="shrink-0 text-muted-foreground/70">{formatAttachmentSize(attachment.size)}</span>
                      ) : null}
                    </a>
                  ))}
                </div>
              ) : null}
              {user.interrupted ? <InterruptedBadge /> : null}
            </div>
          )}
        </div>
        {user.createdAt ? <MessageMeta actions={actions} align="end" createdAt={user.createdAt} text={metaText} /> : null}
      </div>
      <ImageLightbox images={imagePreviewItems} openIndex={imagePreviewIndex} onOpenIndexChange={setImagePreviewIndex} />
    </>
  );
});

function ImageAttachmentButton({ image, onOpen }: { image: ImageLightboxItem; onOpen: () => void }) {
  return (
    <button
      className="block h-20 w-24 overflow-hidden rounded-md border border-border/70 bg-muted/40"
      title={`${image.name} ${image.size ? formatAttachmentSize(image.size) : ""}`}
      type="button"
      onClick={onOpen}
    >
      <img alt={image.name} className="h-full w-full object-cover" src={image.url} />
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
          className="size-6 bg-transparent transition-colors hover:bg-muted dark:hover:bg-muted/50 active:translate-y-0"
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
