import { ArrowUp } from "@/components/icons";
import type { ReactNode } from "react";

import type { AudioBindings, Session } from "@/api/client";
import { BackgroundProcessControl } from "@/components/BackgroundProcessControl";
import { ComposerAddButton } from "@/components/ComposerAddMenu";
import { ContextUsageRing } from "@/components/ContextUsageRing";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { ProjectComposerControls } from "@/components/ProjectComposerControls";
import { SessionAudioControls } from "@/components/SessionAudioControls";
import { Spinner } from "@/components/Spinner";
import { UIContextControl } from "@/components/UIContextControl";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import type { ResolvedModelSelection } from "@/lib/modelSelection";
import type { UIContextPart } from "@/state/uiContextStore";

type ComposerToolbarProps = {
  addBusy: boolean;
  audioBindings?: AudioBindings;
  audioInputSupported?: boolean;
  cancelPending: boolean;
  compacting: boolean;
  context?: UIContextPart;
  inputSlot?: ReactNode;
  mentionMenuOpen: boolean;
  presentation?: "default" | "floating";
  projectID: string;
  reasoningEffort: string;
  sendEnabled: boolean;
  session: Session;
  showSendButton: boolean;
  showStopButton: boolean;
  steering: boolean;
  stopEnabled: boolean;
  submitPending: boolean;
  token: string;
  uiContextEnabled: boolean;
  onAddClick: () => void;
  onCancel: () => void;
  onModelPickerClose: () => void;
  onReasoningChange: (value: string) => void;
  onResolvedModelChange: (value: ResolvedModelSelection | null) => void;
  onUIContextEnabledChange: (enabled: boolean) => void;
};

export function ComposerToolbar({
  addBusy,
  audioBindings,
  audioInputSupported,
  cancelPending,
  compacting,
  context,
  inputSlot,
  mentionMenuOpen,
  presentation = "default",
  projectID,
  reasoningEffort,
  sendEnabled,
  session,
  showSendButton,
  showStopButton,
  steering,
  stopEnabled,
  submitPending,
  token,
  uiContextEnabled,
  onAddClick,
  onCancel,
  onModelPickerClose,
  onReasoningChange,
  onResolvedModelChange,
  onUIContextEnabledChange,
}: ComposerToolbarProps) {
  const { t } = useI18n();
  const floating = presentation === "floating";

  return (
    <div className={floating ? "flex min-w-0 items-center gap-1 p-2" : "flex min-w-0 items-center gap-1 px-2 pb-2"}>
      <ComposerAddButton
        active={mentionMenuOpen}
        busy={addBusy}
        label={t("composer.addMenuTitle")}
        onClick={onAddClick}
      />
      {inputSlot}
      {floating ? null : <ProjectComposerControls projectID={projectID} token={token} />}
      {!floating && context ? (
        <UIContextControl
          context={context}
          enabled={uiContextEnabled}
          onEnabledChange={onUIContextEnabledChange}
        />
      ) : null}
      {floating ? null : <BackgroundProcessControl sessionID={session.id} token={token} />}
      {compacting ? (
        <span
          aria-live="polite"
          className="min-w-0 max-w-40 truncate px-1 text-xs text-muted-foreground"
          role="status"
        >
          {t("composer.compacting")}
        </span>
      ) : null}
      <div className="ml-auto flex min-w-0 items-center gap-1">
        {floating ? null : <ContextUsageRing token={token} sessionID={session.id} />}
        <ModelReasoningPicker
          className="min-w-0"
          iconOnly={floating}
          token={token}
          session={session}
          reasoningValue={reasoningEffort}
          onAfterClose={onModelPickerClose}
          onReasoningChange={onReasoningChange}
          onResolvedChange={onResolvedModelChange}
        />
      </div>
      <SessionAudioControls
        audioInputSupported={audioInputSupported}
        bindings={audioBindings}
        token={token}
        sessionID={session.id}
      />
      {showStopButton ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t("composer.stop")}
              className="rounded-full !bg-foreground !text-background shadow-sm hover:!bg-foreground/90 hover:!text-background dark:hover:!bg-foreground/90"
              disabled={!stopEnabled}
              size="icon"
              type="button"
              variant="ghost"
              onClick={onCancel}
            >
              {cancelPending ? (
                <Spinner />
              ) : (
                <span aria-hidden="true" className="size-2.5 rounded-[2px] bg-current" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("composer.stop")}</TooltipContent>
        </Tooltip>
      ) : null}
      {showSendButton ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t(steering ? "composer.queue" : "composer.send")}
              className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
              disabled={!sendEnabled}
              size="icon"
              type="submit"
              variant={sendEnabled ? "default" : "secondary"}
            >
              {submitPending ? <Spinner /> : <ArrowUp />}
            </Button>
          </TooltipTrigger>
          <TooltipContent className="grid gap-1">
            <span className="flex items-center gap-2">
              <kbd className="font-mono text-[11px]">Enter</kbd>
              <span>{t("composer.send")}</span>
            </span>
            {steering ? (
              <span className="flex items-center gap-2">
                <kbd className="font-mono text-[11px]">⌘ Enter</kbd>
                <span>{t("composer.steerShortcut")}</span>
              </span>
            ) : null}
          </TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  );
}
