import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Captions, Mic, Volume2 } from "@/components/icons";
import { toast } from "sonner";

import {
  bindAudioInput,
  bindAudioOutput,
  cancelAudioRuntimeInstall,
  getAudioRuntime,
  startAudioRuntimeInstall,
  APIError,
  type AudioBindings,
  type AudioInputMode,
  type AudioRuntimeStatus,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { composerControlStateClassName } from "@/components/composerControlStyles";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export function SessionAudioControls({
  audioInputSupported,
  bindings,
  token,
  sessionID,
}: {
  audioInputSupported?: boolean;
  bindings?: AudioBindings;
  token: string;
  sessionID: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [runtimeDialogOpen, setRuntimeDialogOpen] = useState(false);
  const [runtimeInstallMode, setRuntimeInstallMode] = useState<AudioInputMode>("transcribe");
  const [checkingRuntime, setCheckingRuntime] = useState(false);
  const [selectedInputMode, setSelectedInputMode] = useState<AudioInputMode>("transcribe");
  const inputActive = bindings?.inputOwner === sessionID;
  const activeInputMode: AudioInputMode = bindings?.inputMode === "raw" ? "raw" : "transcribe";
  const outputActive = bindings?.outputOwner === sessionID;
  const inputLevel = inputActive ? bindings?.inputLevel ?? 0 : 0;
  const invalidateAudioBindings = () => queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
  const setBindings = (next: AudioBindings) => {
    queryClient.setQueryData(queryKeys.audioBindings(), { bindings: next });
    void invalidateAudioBindings();
  };
  const inputMutation = useMutation({
    mutationFn: ({ enabled, mode }: { enabled: boolean; mode: AudioInputMode }) => bindAudioInput(token, sessionID, enabled, mode),
    onMutate: async ({ enabled, mode }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", inputMode: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          inputOwner: enabled ? sessionID : current.inputOwner === sessionID ? "" : current.inputOwner,
          inputMode: enabled ? mode : current.inputOwner === sessionID ? "" : current.inputMode,
          inputLevel: enabled ? 0 : current.inputOwner === sessionID ? 0 : current.inputLevel,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (error, _change, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(audioAPIErrorMessage(error, t("voice.inputFailed"), t));
    },
  });
  const outputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioOutput(token, sessionID, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", inputMode: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          outputOwner: enabled ? sessionID : current.outputOwner === sessionID ? "" : current.outputOwner,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(audioAPIErrorMessage(error, t("voice.outputFailed"), t));
    },
  });

  useEffect(() => {
    if (!inputActive || !bindings?.inputMode || inputMutation.isPending) {
      return;
    }
    setSelectedInputMode(activeInputMode);
  }, [activeInputMode, bindings?.inputMode, inputActive, inputMutation.isPending]);

  useEffect(() => {
    if (audioInputSupported !== false || selectedInputMode === "transcribe") {
      return;
    }
    setSelectedInputMode("transcribe");
  }, [audioInputSupported, selectedInputMode]);

  useEffect(() => {
    if (!inputActive || activeInputMode !== "raw" || audioInputSupported !== false || inputMutation.isPending) {
      return;
    }
    inputMutation.mutate({ enabled: true, mode: "transcribe" });
  }, [activeInputMode, audioInputSupported, inputActive, inputMutation]);

  const openInput = async (mode: AudioInputMode) => {
    if (inputActive) {
      inputMutation.mutate({ enabled: true, mode });
      return;
    }
    setCheckingRuntime(true);
    try {
      const status = await queryClient.fetchQuery({
        queryKey: queryKeys.audioRuntime(),
        queryFn: () => getAudioRuntime(token),
        staleTime: 0,
      });
      queryClient.setQueryData(queryKeys.audioRuntime(), status);
      if (status.disabled) {
        toast.error(t("voice.runtimeDisabled"));
        return;
      }
      if (!status.installed) {
        setRuntimeInstallMode(mode);
        setRuntimeDialogOpen(true);
        return;
      }
      inputMutation.mutate({ enabled: true, mode });
    } catch (error) {
      toast.error(audioAPIErrorMessage(error, t("voice.inputFailed"), t));
    } finally {
      setCheckingRuntime(false);
    }
  };
  const handleInputModeClick = (mode: AudioInputMode) => {
    setSelectedInputMode(mode);
    if (inputActive && activeInputMode === mode) {
      inputMutation.mutate({ enabled: false, mode });
      return;
    }
    void openInput(mode);
  };
  const displayInputMode =
    inputMutation.isPending && inputMutation.variables?.enabled === true
      ? inputMutation.variables.mode
      : inputActive
        ? activeInputMode
        : selectedInputMode;
  const inputPending = checkingRuntime || (inputMutation.isPending && inputMutation.variables?.enabled === true);
  const inputPendingMode = checkingRuntime ? selectedInputMode : inputMutation.variables?.mode;

  return (
    <>
      <AudioControlButtons
        asrInputLabel={inputActive && displayInputMode === "transcribe" ? t("voice.inputASROn") : t("voice.inputASROff")}
        inputActive={inputActive}
        inputMode={displayInputMode}
        inputLevel={inputLevel}
        inputBusy={inputMutation.isPending || checkingRuntime}
        inputPending={inputPending}
        inputPendingMode={inputPendingMode}
        rawInputLabel={inputActive && displayInputMode === "raw" ? t("voice.inputRawOn") : t("voice.inputRawOff")}
        rawInputSupported={audioInputSupported === true}
        outputActive={outputActive}
        outputLabel={outputActive ? t("voice.outputOn") : t("voice.outputOff")}
        outputBusy={outputMutation.isPending}
        outputPending={outputMutation.isPending && outputMutation.variables === true}
        controlsLabel={t("voice.controls")}
        onInputModeClick={handleInputModeClick}
        onOutputClick={() => outputMutation.mutate(!outputActive)}
      />
      <AudioRuntimeInstallDialog
        open={runtimeDialogOpen}
        token={token}
        onOpenChange={setRuntimeDialogOpen}
        onInstalled={() => {
          setRuntimeDialogOpen(false);
          inputMutation.mutate({ enabled: true, mode: runtimeInstallMode });
        }}
      />
    </>
  );
}

export function AudioRuntimeInstallDialog({
  open,
  token,
  onOpenChange,
  onInstalled,
}: {
  open: boolean;
  token: string;
  onOpenChange: (open: boolean) => void;
  onInstalled: () => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [installRequested, setInstallRequested] = useState(false);
  const runtimeQuery = useQuery({
    queryKey: queryKeys.audioRuntime(),
    queryFn: () => getAudioRuntime(token),
    enabled: open,
    refetchInterval: open ? 750 : false,
  });
  const runtimeStatus = runtimeQuery.data;
  const runtimeRunning = runtimeStatus?.running ?? false;
  const startRuntimeMutation = useMutation({
    mutationFn: () => startAudioRuntimeInstall(token),
    onMutate: () => setInstallRequested(true),
    onSuccess: (status) => {
      queryClient.setQueryData(queryKeys.audioRuntime(), status);
      if (status.disabled || (!status.running && !status.installed)) {
        setInstallRequested(false);
      }
    },
    onError: (error) => {
      setInstallRequested(false);
      toast.error(audioAPIErrorMessage(error, t("voice.runtimeFailed"), t));
    },
  });
  const cancelRuntimeMutation = useMutation({
    mutationFn: () => cancelAudioRuntimeInstall(token),
    onSuccess: (status) => queryClient.setQueryData(queryKeys.audioRuntime(), status),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.audioRuntime() });
    },
  });
  const runtimeStarting = startRuntimeMutation.isPending;

  useEffect(() => {
    if (!installRequested || runtimeRunning || !runtimeStatus?.installed) {
      return;
    }
    setInstallRequested(false);
    onInstalled();
  }, [installRequested, onInstalled, runtimeRunning, runtimeStatus?.installed]);

  const closeDialog = () => {
    if (runtimeRunning) {
      cancelRuntimeMutation.mutate(undefined, {
        onSettled: () => {
          setInstallRequested(false);
          onOpenChange(false);
        },
      });
      return;
    }
    setInstallRequested(false);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (nextOpen) {
          onOpenChange(true);
        } else {
          closeDialog();
        }
      }}
    >
      <DialogContent
        className="sm:max-w-md"
        showCloseButton={!runtimeStarting}
        onEscapeKeyDown={(event) => {
          if (runtimeRunning || runtimeStarting) {
            event.preventDefault();
          }
        }}
        onPointerDownOutside={(event) => {
          if (runtimeRunning || runtimeStarting) {
            event.preventDefault();
          }
        }}
      >
        <DialogHeader>
          <DialogTitle>{runtimeRunning ? t("voice.runtimeDownloading") : t("voice.runtimeTitle")}</DialogTitle>
          <DialogDescription>{runtimeDialogDescription(runtimeStatus, t)}</DialogDescription>
        </DialogHeader>
        <RuntimeDownloadBody status={runtimeStatus} t={t} loading={runtimeQuery.isFetching && !runtimeStatus} />
        <DialogFooter>
          {runtimeRunning ? (
            <Button type="button" variant="destructive" onClick={closeDialog} disabled={cancelRuntimeMutation.isPending}>
              {cancelRuntimeMutation.isPending ? <Spinner className="size-4" /> : null}
              {t("voice.runtimeCancel")}
            </Button>
          ) : runtimeStatus?.installed ? (
            <Button type="button" onClick={closeDialog}>
              {t("voice.runtimeClose")}
            </Button>
          ) : (
            <>
              <Button type="button" variant="outline" onClick={closeDialog} disabled={runtimeStarting}>
                {t("common.cancel")}
              </Button>
              <Button type="button" onClick={() => startRuntimeMutation.mutate()} disabled={runtimeStarting}>
                {runtimeStarting ? <Spinner className="size-4" /> : null}
                {t("voice.runtimeDownload")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function audioAPIErrorMessage(error: unknown, fallback: string, t: (key: string) => string) {
  if (error instanceof APIError) {
    if (error.code === "audio_input_unavailable") {
      return t("voice.inputUnavailable");
    }
    if (error.code === "audio_input_route_unavailable") {
      return t("voice.inputRouteUnavailable");
    }
    if (error.code === "audio_input_unsupported") {
      return t("voice.inputRawUnsupported");
    }
    if (error.code === "audio_unavailable") {
      return t("voice.audioUnavailable");
    }
    if (error.code === "audio_binding_failed") {
      return fallback;
    }
    return fallback;
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

function runtimeDialogDescription(status: AudioRuntimeStatus | undefined, t: (key: string) => string) {
  if (status?.error) {
    return t("voice.runtimeFailed");
  }
  if (status?.installed) {
    return t("voice.runtimeReady");
  }
  return t("voice.runtimeMissing");
}

function RuntimeDownloadBody({ status, t, loading }: { status?: AudioRuntimeStatus; t: (key: string) => string; loading: boolean }) {
  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner className="size-4" />
        {t("common.loading")}
      </div>
    );
  }
  const progress = runtimeProgress(status);
  return (
    <div className="grid gap-3">
      {status?.running ? (
        <div className="grid gap-2">
          <div className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary" style={{ width: `${progress}%` }} />
          </div>
          <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
            <span className="min-w-0 truncate">
              {formatTemplate(t("voice.runtimeAssetProgress"), {
                index: status.assetIndex || 1,
                total: status.assetTotal || 1,
                asset: status.currentAsset || status.message || "",
              })}
            </span>
            <span className="shrink-0">
              {status.bytesTotal
                ? `${formatBytes(status.bytesDownloaded)} / ${formatBytes(status.bytesTotal)}`
                : status.bytesDownloaded
                  ? formatBytes(status.bytesDownloaded)
                  : t("voice.runtimeUnknownSize")}
            </span>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function runtimeProgress(status?: AudioRuntimeStatus) {
  if (!status?.running) {
    return 0;
  }
  if (!status.assetIndex || !status.assetTotal) {
    return 2;
  }
  const index = Math.max(1, status.assetIndex);
  const total = Math.max(index, status.assetTotal);
  let assetProgress = 0;
  if (["downloaded", "verifying", "extracting", "loading"].includes(status.state)) {
    assetProgress = 1;
  } else if (status.state === "downloading" && status.bytesTotal && status.bytesTotal > 0) {
    assetProgress = Math.max(0, Math.min(1, (status.bytesDownloaded ?? 0) / status.bytesTotal));
  }
  return Math.max(2, Math.min(100, ((index - 1 + assetProgress) / total) * 100));
}

function formatBytes(bytes?: number) {
  if (!bytes || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

function formatTemplate(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (_, key) => String(values[key] ?? ""));
}

export function AudioControlButtons({
  asrInputLabel,
  controlsLabel,
  inputActive,
  inputMode,
  inputLevel,
  inputBusy,
  inputPending,
  inputPendingMode,
  rawInputLabel,
  rawInputSupported,
  outputActive,
  outputLabel,
  outputBusy,
  outputPending,
  onInputModeClick,
  onOutputClick,
}: {
  asrInputLabel: string;
  controlsLabel: string;
  inputActive: boolean;
  inputMode: AudioInputMode;
  inputLevel: number;
  inputBusy?: boolean;
  inputPending: boolean;
  inputPendingMode?: AudioInputMode;
  rawInputLabel: string;
  rawInputSupported: boolean;
  outputActive: boolean;
  outputLabel: string;
  outputBusy?: boolean;
  outputPending: boolean;
  onInputModeClick: (mode: AudioInputMode) => void;
  onOutputClick: () => void;
}) {
  const inputDisabled = inputBusy ?? inputPending;
  const outputDisabled = outputBusy ?? outputPending;
  const asrButton = (
    <AudioInputButton
      active={inputActive && inputMode === "transcribe"}
      disabled={inputDisabled}
      grouped={rawInputSupported}
      icon={<Captions className="size-4" />}
      label={asrInputLabel}
      level={inputLevel}
      pending={inputPending && inputPendingMode !== "raw"}
      onClick={() => onInputModeClick("transcribe")}
    />
  );
  return (
    <div className="flex shrink-0 items-center gap-1" aria-label={controlsLabel}>
      {rawInputSupported ? (
        <ButtonGroup aria-label={controlsLabel}>
          {asrButton}
          <AudioInputButton
            active={inputActive && inputMode === "raw"}
            disabled={inputDisabled}
            grouped
            icon={<Mic className="size-4" />}
            label={rawInputLabel}
            level={inputLevel}
            pending={inputPending && inputPendingMode === "raw"}
            onClick={() => onInputModeClick("raw")}
          />
        </ButtonGroup>
      ) : (
        asrButton
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={outputDisabled}
            aria-label={outputLabel}
            aria-pressed={outputActive}
            className={cn(
              "rounded-full",
              !outputActive && composerControlStateClassName,
              !outputActive && "text-muted-foreground",
            )}
            size="icon-sm"
            type="button"
            variant={outputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!outputDisabled) {
                onOutputClick();
              }
            }}
          >
            {outputPending ? <Spinner className="size-4" /> : <Volume2 className="size-4" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{outputLabel}</TooltipContent>
      </Tooltip>
    </div>
  );
}

function AudioInputButton({
  active,
  disabled,
  grouped,
  icon,
  label,
  level,
  pending,
  onClick,
}: {
  active: boolean;
  disabled: boolean;
  grouped: boolean;
  icon: ReactNode;
  label: string;
  level: number;
  pending: boolean;
  onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-disabled={disabled}
          aria-label={label}
          aria-pressed={active}
          className={cn(
            "relative overflow-hidden",
            !grouped && "rounded-full",
            !active && composerControlStateClassName,
            !active && "text-muted-foreground",
          )}
          size="icon-sm"
          type="button"
          variant={active ? "default" : grouped ? "outline" : "ghost"}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => {
            if (!disabled) {
              onClick();
            }
          }}
        >
          {pending ? <Spinner className="size-4" /> : <MicButtonContent active={active} icon={icon} level={level} />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function MicButtonContent({ active, icon, level }: { active: boolean; icon: ReactNode; level: number }) {
  if (!active) {
    return icon;
  }
  const normalized = Math.max(0, Math.min(1, Math.sqrt(Math.max(0, level)) * 2.2));
  const profiles = [0.45, 0.75, 1, 0.72, 0.5];
  return (
    <span className="pointer-events-none flex h-5 w-5 items-center justify-center gap-0.5">
      {profiles.map((profile, index) => (
        <span
          key={index}
          className="w-0.5 rounded-full bg-primary-foreground"
          style={{ height: `${5 + normalized * profile * 14}px` }}
        />
      ))}
    </span>
  );
}
