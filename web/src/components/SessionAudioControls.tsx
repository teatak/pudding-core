import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Mic, Volume2 } from "lucide-react";
import { toast } from "sonner";

import { bindAudioInput, bindAudioOutput, type AudioBindings } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";

export function SessionAudioControls({ bindings, token, sessionID }: { bindings?: AudioBindings; token: string; sessionID: string }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const inputActive = bindings?.inputOwner === sessionID;
  const outputActive = bindings?.outputOwner === sessionID;
  const inputLevel = inputActive ? bindings?.inputLevel ?? 0 : 0;
  const invalidateAudioBindings = () => queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
  const setBindings = (next: AudioBindings) => {
    queryClient.setQueryData(queryKeys.audioBindings(), { bindings: next });
    void invalidateAudioBindings();
  };
  const inputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioInput(token, sessionID, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          inputOwner: enabled ? sessionID : current.inputOwner === sessionID ? "" : current.inputOwner,
          inputLevel: enabled ? 0 : current.inputOwner === sessionID ? 0 : current.inputLevel,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (_error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(t("voice.inputFailed"));
    },
  });
  const outputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioOutput(token, sessionID, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          outputOwner: enabled ? sessionID : current.outputOwner === sessionID ? "" : current.outputOwner,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (_error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(t("voice.outputFailed"));
    },
  });

  return (
    <AudioControlButtons
      inputActive={inputActive}
      inputLabel={inputActive ? t("voice.inputOn") : t("voice.inputOff")}
      inputLevel={inputLevel}
      inputBusy={inputMutation.isPending}
      inputPending={inputMutation.isPending && inputMutation.variables === true}
      outputActive={outputActive}
      outputLabel={outputActive ? t("voice.outputOn") : t("voice.outputOff")}
      outputBusy={outputMutation.isPending}
      outputPending={outputMutation.isPending && outputMutation.variables === true}
      controlsLabel={t("voice.controls")}
      onInputClick={() => inputMutation.mutate(!inputActive)}
      onOutputClick={() => outputMutation.mutate(!outputActive)}
    />
  );
}

export function AudioControlButtons({
  controlsLabel,
  inputActive,
  inputLabel,
  inputLevel,
  inputBusy,
  inputPending,
  outputActive,
  outputLabel,
  outputBusy,
  outputPending,
  onInputClick,
  onOutputClick,
}: {
  controlsLabel: string;
  inputActive: boolean;
  inputLabel: string;
  inputLevel: number;
  inputBusy?: boolean;
  inputPending: boolean;
  outputActive: boolean;
  outputLabel: string;
  outputBusy?: boolean;
  outputPending: boolean;
  onInputClick: () => void;
  onOutputClick: () => void;
}) {
  const inputDisabled = inputBusy ?? inputPending;
  const outputDisabled = outputBusy ?? outputPending;
  return (
    <div className="flex shrink-0 items-center gap-1" aria-label={controlsLabel}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={inputDisabled}
            aria-label={inputLabel}
            aria-pressed={inputActive}
            className="relative overflow-hidden rounded-full"
            size="icon"
            type="button"
            variant={inputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!inputDisabled) {
                onInputClick();
              }
            }}
          >
            {inputPending ? <Loader2 className="size-4 animate-spin" /> : <MicButtonContent active={inputActive} level={inputLevel} />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{inputLabel}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={outputDisabled}
            aria-label={outputLabel}
            aria-pressed={outputActive}
            className="rounded-full"
            size="icon"
            type="button"
            variant={outputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!outputDisabled) {
                onOutputClick();
              }
            }}
          >
            {outputPending ? <Loader2 className="size-4 animate-spin" /> : <Volume2 className="size-4" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{outputLabel}</TooltipContent>
      </Tooltip>
    </div>
  );
}

function MicButtonContent({ active, level }: { active: boolean; level: number }) {
  if (!active) {
    return <Mic className="size-4" />;
  }
  const normalized = Math.max(0, Math.min(1, Math.sqrt(Math.max(0, level)) * 2.2));
  const profiles = [0.45, 0.75, 1, 0.72, 0.5];
  return (
    <span className="pointer-events-none flex h-5 w-5 items-center justify-center gap-0.5">
      {profiles.map((profile, index) => (
        <span
          key={index}
          className="w-0.5 rounded-full bg-primary-foreground transition-all duration-100 ease-out"
          style={{ height: `${5 + normalized * profile * 14}px` }}
        />
      ))}
    </span>
  );
}
