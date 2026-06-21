import { Check, Loader2, Pencil, Trash2, X } from "lucide-react";
import { memo, useEffect, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { InterruptedBadge, MessageMeta } from "./MessageMeta";
import type { UserInputVM } from "./types";

export const UserInput = memo(function UserInput({
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  user,
}: {
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  user: UserInputVM;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState(user.text);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const clientMessageID = user.clientMessageID;
  const canEditQueued =
    Boolean(clientMessageID && user.pending && (user.status === "queued" || user.status === "editing")) &&
    Boolean(onQueuedEditStart && onQueuedSave && onQueuedCancel);

  useEffect(() => {
    if (!editing) {
      setDraft(user.text);
    }
  }, [editing, user.text]);

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
          <>
            {user.text}
            {user.interrupted ? <InterruptedBadge /> : null}
          </>
        )}
      </div>
      {user.createdAt ? <MessageMeta actions={actions} align="end" createdAt={user.createdAt} text={user.text} /> : null}
    </div>
  );
});

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
