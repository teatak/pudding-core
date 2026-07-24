import { ArrowUp, ChevronDown, Clock3, CornerDownRight } from "lucide-react";

import type { AudioBindings, Session } from "@/api/client";
import { AppDropdownMenuRadioItem as DropdownMenuRadioItem } from "@/components/AppMenu";
import { BackgroundProcessControl } from "@/components/BackgroundProcessControl";
import { ComposerAddButton } from "@/components/ComposerAddMenu";
import { ContextUsageRing } from "@/components/ContextUsageRing";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { ProjectComposerControls } from "@/components/ProjectComposerControls";
import { SessionAudioControls } from "@/components/SessionAudioControls";
import { Spinner } from "@/components/Spinner";
import { UIContextControl } from "@/components/UIContextControl";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
  mentionMenuOpen: boolean;
  projectID: string;
  reasoningEffort: string;
  sendEnabled: boolean;
  session: Session;
  showSendButton: boolean;
  showStopButton: boolean;
  runningDeliveryMode: "steer" | "queue";
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
  onRunningDeliveryModeChange: (value: "steer" | "queue") => void;
  onUIContextEnabledChange: (enabled: boolean) => void;
};

export function ComposerToolbar({
  addBusy,
  audioBindings,
  audioInputSupported,
  cancelPending,
  compacting,
  context,
  mentionMenuOpen,
  projectID,
  reasoningEffort,
  sendEnabled,
  session,
  showSendButton,
  showStopButton,
  runningDeliveryMode,
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
  onRunningDeliveryModeChange,
  onUIContextEnabledChange,
}: ComposerToolbarProps) {
  const { t } = useI18n();

  return (
    <div className="flex min-w-0 items-center gap-1 px-2 pb-2">
      <ComposerAddButton
        active={mentionMenuOpen}
        busy={addBusy}
        label={t("composer.addMenuTitle")}
        onClick={onAddClick}
      />
      <ProjectComposerControls projectID={projectID} token={token} />
      {context ? (
        <UIContextControl
          context={context}
          enabled={uiContextEnabled}
          onEnabledChange={onUIContextEnabledChange}
        />
      ) : null}
      <BackgroundProcessControl sessionID={session.id} token={token} />
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
        <ContextUsageRing mode={session.activeMode} token={token} sessionID={session.id} />
        <ModelReasoningPicker
          className="min-w-0"
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
        steering ? (
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    aria-label={t("composer.deliveryMode")}
                    className="size-8 rounded-full"
                    size="icon"
                    type="button"
                    variant="ghost"
                  >
                    <ChevronDown />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>{t("composer.deliveryMode")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" side="top" className="w-48">
              <DropdownMenuRadioGroup
                value={runningDeliveryMode}
                onValueChange={(value) => onRunningDeliveryModeChange(value as "steer" | "queue")}
              >
                <DropdownMenuRadioItem value="steer">
                  <CornerDownRight />
                  {t("composer.steer")}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="queue">
                  <Clock3 />
                  {t("composer.queue")}
                </DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null
      ) : null}
      {showSendButton ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t(steering && runningDeliveryMode === "steer" ? "composer.steer" : steering ? "composer.queue" : "composer.send")}
              className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
              disabled={!sendEnabled}
              size="icon"
              type="submit"
              variant={sendEnabled ? "default" : "secondary"}
            >
              {submitPending ? <Spinner /> : <ArrowUp />}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {t(steering && runningDeliveryMode === "steer" ? "composer.steer" : steering ? "composer.queue" : "composer.send")}
          </TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  );
}
