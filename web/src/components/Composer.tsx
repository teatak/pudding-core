import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, Check, Code2, FolderOpen, Loader2, MessageCircle, Plus, SearchCheck, ShieldCheck, X, type LucideIcon } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FocusEvent,
  type KeyboardEvent,
} from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import { APIError, approveApproval, cancelTurn, compactSession, denyApproval, getTurn, submitMessage, updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { ContextUsageRing } from "@/components/ContextUsageRing";
import { Mascot, type MascotGaze, type MascotGazePoint, type MascotMood } from "@/components/Mascot";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { type ResolvedModelSelection } from "@/components/ModelPicker";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { useI18n } from "@/i18n";
import { newClientID } from "@/lib/id";
import type { AppSearch } from "@/lib/route";
import { getSubmitFailure } from "@/lib/submitFailure";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { useOverlayStore, type AssistantOverlay, type AssistantOverlayPart, type TurnPhaseState } from "@/state/overlayStore";

const composerSchema = z.object({
  text: z.string(),
});

const MASCOT_INPUT_PITCH_BIAS = 0.65;

type ComposerProps = {
  token: string;
  session: Session;
  onSubmitError?: (message: string | null) => void;
};

type ComposerApproval = Extract<AssistantOverlayPart, { type: "approval" }>;
type SlashCommand = {
  command: string;
  description: string;
  hasArgs: boolean;
  id: "clear" | "compact" | "rename" | "summary";
  label: string;
};
type SlashSubmitCommand =
  | { id: "clear" }
  | { id: "compact"; hint: string }
  | { id: "rename"; title: string }
  | { id: "summary"; hint: string };

export function Composer({ token, session, onSubmitError }: ComposerProps) {
  const sessionID = session.id;
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const acceptSubmittingTurn = useOverlayStore((state) => state.acceptSubmittingTurn);
  const clearSubmittingTurn = useOverlayStore((state) => state.clearSubmittingTurn);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const startSubmittingTurn = useOverlayStore((state) => state.startSubmittingTurn);
  // 停止态双源:overlay 的 runningTurns(本地实时)|| session 快照的 running
  // (后端 turns 表派生)。中途刷新走 SSE tail 不回放 turn.started,若此时
  // provider 暂无 delta,overlay 不知道有 turn 在跑——session.running 兜底,
  // 保证停止按钮不丢(cancel 按 sessionID 取消,无需 turnID)。
  const overlayRunning = useOverlayStore((state) => Boolean(state.runningTurns[sessionID]));
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const pendingApproval = useOverlayStore((state) => selectPendingApproval(state.assistants, sessionID, state.runningTurns[sessionID]));
  const running = overlayRunning || session.running;
  const currentMode = session.modeLease === "session" ? session.activeMode : "chat";
  const [resolvedModel, setResolvedModel] = useState<ResolvedModelSelection | null>(null);
  const [mascotGaze, setMascotGaze] = useState<MascotGaze>({ type: "pointer" });
  const [mascotErrorMessage, setMascotErrorMessage] = useState<string | null>(null);
  const [mascotErrorSignal, setMascotErrorSignal] = useState(0);
  const [textFocused, setTextFocused] = useState(false);
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0);
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(newClientID());
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotErrorTimerRef = useRef<number | null>(null);
  const mascotGazeRafRef = useRef(0);
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });
  // 空消息(含纯空白)不可发:禁用发送按钮 + Enter 不触发提交,从源头避免
  // 弹出 zod min(1) 的校验错误。watch 让按钮态随输入实时更新。
  const draftText = form.watch("text");
  const trimmedDraftText = draftText.trim();
  const draftSlashCommand = parseSlashSubmitCommand(trimmedDraftText);
  const canSend = Boolean(trimmedDraftText);
  const textField = form.register("text");
  const slashCommands: SlashCommand[] = [
    {
      command: "/compact",
      description: t("composer.commandCompactDesc"),
      hasArgs: true,
      id: "compact",
      label: t("composer.commandCompact"),
    },
    {
      command: "/clear",
      description: t("composer.commandClearDesc"),
      hasArgs: false,
      id: "clear",
      label: t("composer.commandClear"),
    },
    {
      command: "/rename",
      description: t("composer.commandRenameDesc"),
      hasArgs: true,
      id: "rename",
      label: t("composer.commandRename"),
    },
    {
      command: "/summary",
      description: t("composer.commandSummaryDesc"),
      hasArgs: true,
      id: "summary",
      label: t("composer.commandSummary"),
    },
  ];
  const slashQuery = slashCommandQuery(trimmedDraftText, slashCommands);
  const visibleSlashCommands =
    textFocused && slashQuery !== null
      ? slashCommands.filter((command) => command.command.startsWith("/" + slashQuery))
      : [];
  const slashMenuOpen = visibleSlashCommands.length > 0;
  const reasoningOptions = useMemo(() => reasoningEffortOptionsForSelection(resolvedModel), [resolvedModel]);
  const resolvedModelKey = resolvedModel ? `${resolvedModel.provider}:${resolvedModel.model}` : "";
  const reasoningEffort = resolvedModelKey && session.reasoningModelKey === resolvedModelKey ? session.reasoningEffort || "" : "";
  const setSessionReasoningEffort = useCallback(
    (value: string) => {
      if (!resolvedModelKey) {
        return;
      }
      void updateSession(token, sessionID, { reasoningEffort: value })
        .then((updated) => {
          queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
            if (!previous) {
              return previous;
            }
            return {
              sessions: previous.sessions.map((item) => (item.id === updated.id ? updated : item)),
            };
          });
        })
        .catch((error) => {
          console.warn("failed to update reasoning effort", error);
        })
        .finally(() => {
          void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
        });
    },
    [queryClient, resolvedModelKey, sessionID, token],
  );

  useEffect(() => {
    if (!slashMenuOpen) {
      setSlashSelectedIndex(0);
      return;
    }
    setSlashSelectedIndex((index) => Math.min(index, visibleSlashCommands.length - 1));
  }, [slashMenuOpen, visibleSlashCommands.length]);

  useEffect(() => {
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setSessionReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setSessionReasoningEffort]);

  const clearMascotError = useCallback(() => {
    if (mascotErrorTimerRef.current) {
      window.clearTimeout(mascotErrorTimerRef.current);
      mascotErrorTimerRef.current = null;
    }
    setMascotErrorMessage(null);
  }, []);
  // Prefer this mascot feedback for composer-local validation and command errors.
  const showMascotError = useCallback(
    (message: string) => {
      onSubmitError?.(null);
      setMascotErrorMessage(message);
      setMascotErrorSignal((signal) => signal + 1);
      if (mascotErrorTimerRef.current) {
        window.clearTimeout(mascotErrorTimerRef.current);
      }
      mascotErrorTimerRef.current = window.setTimeout(() => {
        mascotErrorTimerRef.current = null;
        setMascotErrorMessage(null);
      }, 3600);
    },
    [onSubmitError],
  );
  const submitMutation = useMutation({
    mutationFn: async (value: z.infer<typeof composerSchema>) => {
      const clientMessageID = draftIDRef.current;
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      addPendingUser({
        sessionID,
        clientMessageID,
        status: "submitting",
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      const result = await submitMessage(token, sessionID, {
        clientMessageID,
        text: value.text,
      });
      if (result.queued || !result.turnID) {
        clearSubmittingTurn(sessionID, clientMessageID);
      } else {
        acceptSubmittingTurn(sessionID, clientMessageID, result.turnID);
      }
      return result;
    },
    onSuccess: async (result) => {
      clearMascotError();
      onSubmitError?.(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
      // 标题自动生成由后端 titler 负责(provisional + LLM,session.titled
      // 事件回推),前端不写标题
      if (result.duplicate && result.turnID) {
        try {
          const turn = await getTurn(token, sessionID, result.turnID);
          queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) =>
            upsertTurnIntoPages(previous, turn),
          );
        } catch (error) {
          console.warn("failed to sync duplicate turn", error);
        }
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      // 提交未被接受,canonical 不会出现这条消息:pending 气泡必须撤掉,
      // 文本留在 composer 里供重试(同一 draft ID)
      clearSubmittingTurn(sessionID, draftIDRef.current);
      removePendingUser(sessionID, draftIDRef.current);
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      showMascotError(failure.message);
    },
  });
  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });
  const compactMutation = useMutation({
    mutationFn: ({ hint }: { hint: string }) => compactSession(token, sessionID, { hint }),
    onMutate: () => {
      clearMascotError();
      onSubmitError?.(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
    },
    onSuccess: async () => {
      clearMascotError();
      onSubmitError?.(null);
      toast.success(t("composer.compactDone"));
      await queryClient.invalidateQueries({ queryKey: queryKeys.turns(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      showMascotError(compactErrorMessage(error, t));
    },
  });
  const systemSubmitMutation = useMutation({
    mutationFn: async ({ clientMessageID, text }: { clientMessageID: string; text: string }) => {
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      return submitMessage(token, sessionID, { clientMessageID, kind: "system", text });
    },
    onMutate: () => {
      clearMascotError();
      onSubmitError?.(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
    },
    onSuccess: async () => {
      clearMascotError();
      onSubmitError?.(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.summaryFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      showMascotError(failure.message);
    },
  });
  const renameMutation = useMutation({
    mutationFn: ({ title }: { title: string }) => updateSession(token, sessionID, { title }),
    onSuccess: async () => {
      clearMascotError();
      onSubmitError?.(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: () => {
      showMascotError(t("composer.renameFailed"));
    },
  });
  const sendEnabled =
    canSend &&
    !slashMenuOpen &&
    !submitMutation.isPending &&
    !compactMutation.isPending &&
    !systemSubmitMutation.isPending &&
    !renameMutation.isPending &&
    (Boolean(resolvedModel) || Boolean(draftSlashCommand && draftSlashCommand.id !== "summary"));
  const stopEnabled = running && !cancelMutation.isPending;
  const showStopButton = running || cancelMutation.isPending;
  const showSendButton =
    !showStopButton ||
    (canSend && !submitMutation.isPending && !compactMutation.isPending && !systemSubmitMutation.isPending && !renameMutation.isPending);

  const runClearCommand = useCallback(() => {
    clearMascotError();
    onSubmitError?.(null);
    draftIDRef.current = newClientID();
    form.reset({ text: "" });
    setTextFocused(false);
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1" };
        delete next.session;
        return next;
      },
    });
  }, [clearMascotError, form, navigate, onSubmitError]);

  const submitDraft = (value: z.infer<typeof composerSchema>) => {
    const text = value.text.trim();
    if (!text || submitMutation.isPending || compactMutation.isPending || systemSubmitMutation.isPending || renameMutation.isPending) {
      return;
    }
    const slashCommand = parseSlashSubmitCommand(text);
    if (slashCommand?.id === "clear") {
      runClearCommand();
      return;
    }
    if (slashCommand?.id === "compact") {
      compactMutation.mutate({ hint: slashCommand.hint });
      return;
    }
    if (slashCommand?.id === "rename") {
      if (!slashCommand.title) {
        showMascotError(t("composer.renameMissing"));
        return;
      }
      clearMascotError();
      onSubmitError?.(null);
      renameMutation.mutate({ title: slashCommand.title });
      return;
    }
    if (slashCommand?.id === "summary") {
      systemSubmitMutation.mutate({
        clientMessageID: draftIDRef.current,
        text: summaryPrompt(slashCommand.hint, t),
      });
      return;
    }
    clearMascotError();
    onSubmitError?.(null);
    if (!running) {
      startSubmittingTurn(sessionID, draftIDRef.current);
    }
    submitMutation.mutate({ text });
  };

  const handleResolvedModelChange = useCallback((next: ResolvedModelSelection | null) => {
    setResolvedModel((current) => {
      if (!next) {
        return current ? null : current;
      }
      if (
        current?.provider === next.provider &&
        current.model === next.model &&
        current.providerProtocol === next.providerProtocol &&
        current.providerBrand === next.providerBrand &&
        current.modelConfig === next.modelConfig
      ) {
        return current;
      }
      return next;
    });
  }, []);
  const setMascotPointerGaze = useCallback(() => {
    setMascotGaze((current) => (current.type === "pointer" ? current : { type: "pointer" }));
  }, []);
  const setMascotInputGaze = useCallback((target: MascotGazePoint | null) => {
    setMascotGaze(target ? { type: "input", target } : { type: "pointer" });
  }, []);
  const updateMascotInputGaze = useCallback(() => {
    const textArea = textAreaRef.current;
    if (!textArea || document.activeElement !== textArea) {
      return;
    }
    setMascotInputGaze(getTextAreaCaretClientPoint(textArea));
  }, [setMascotInputGaze]);
  const scheduleMascotInputGaze = useCallback(() => {
    if (mascotGazeRafRef.current) {
      window.cancelAnimationFrame(mascotGazeRafRef.current);
    }
    mascotGazeRafRef.current = window.requestAnimationFrame(() => {
      mascotGazeRafRef.current = 0;
      updateMascotInputGaze();
    });
  }, [updateMascotInputGaze]);
  const focusTextarea = useCallback(() => {
    window.requestAnimationFrame(() => {
      textAreaRef.current?.focus({ preventScroll: true });
      scheduleMascotInputGaze();
    });
  }, [scheduleMascotInputGaze]);
  const ime = useImeCompositionGuard({ onCompositionEnd: scheduleMascotInputGaze });
  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };
  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    setTextFocused(false);
    setMascotInputGaze(null);
  };
  const handleTextFocus = () => {
    setTextFocused(true);
    scheduleMascotInputGaze();
  };
  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    void textField.onChange(event);
    if (mascotErrorMessage) {
      clearMascotError();
    }
    setTextFocused(true);
    scheduleMascotInputGaze();
  };
  const selectSlashCommand = (command: SlashCommand) => {
    if (!command.hasArgs) {
      runClearCommand();
      return;
    }
    form.setValue("text", command.command + " ");
    window.requestAnimationFrame(() => {
      textAreaRef.current?.focus();
      scheduleMascotInputGaze();
    });
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (slashMenuOpen) {
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index + 1) % visibleSlashCommands.length);
          return;
        case "ArrowUp":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index - 1 + visibleSlashCommands.length) % visibleSlashCommands.length);
          return;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const command = visibleSlashCommands[slashSelectedIndex] ?? visibleSlashCommands[0];
          if (command) {
            selectSlashCommand(command);
          }
          return;
        }
        case "Escape":
          event.preventDefault();
          setTextFocused(false);
          return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey) {
      if (ime.isComposing(event)) {
        scheduleMascotInputGaze();
        return;
      }
      event.preventDefault();
      if (sendEnabled) {
        void form.handleSubmit(submitDraft)();
      }
    }
    scheduleMascotInputGaze();
  };

  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
      if (mascotErrorTimerRef.current) {
        window.clearTimeout(mascotErrorTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    setMascotGaze({ type: "pointer" });
  }, [sessionID]);

  return (
    <form
      className={`relative shrink-0 pb-4 ${pendingApproval ? "pt-36" : "pt-2"}`}
      onSubmit={form.handleSubmit(submitDraft)}
    >
      {/* 底部遮罩:滚动内容贴近输入区时淡出,随 composer 定位、宽度走 ChatColumn。
          文字边缘外漏不归遮罩管——那是 WKWebView 字形渲染溢出,Transcript overflow-hidden 裁掉 */}
      <div className="pointer-events-none absolute inset-x-0 -top-10">
        <ChatColumn>
          <div className="h-10 bg-gradient-to-t from-background to-transparent" />
        </ChatColumn>
      </div>
      <ChatColumn>
        <div className="relative">
          <ComposerApprovalBar approval={pendingApproval} token={token} />
          {slashMenuOpen ? (
            <SlashCommandMenu
              commands={visibleSlashCommands}
              selectedIndex={slashSelectedIndex}
              onHover={setSlashSelectedIndex}
              onSelect={selectSlashCommand}
            />
          ) : null}
          <div className="relative z-10 rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
            <div className="px-4 pt-4 pb-2">
              <Textarea
                className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-base leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
                placeholder={t("composer.messagePlaceholder")}
                rows={1}
                name={textField.name}
                ref={setTextAreaRef}
                onBlur={handleTextBlur}
                onChange={handleTextChange}
                onCompositionEnd={ime.onCompositionEnd}
                onCompositionStart={ime.onCompositionStart}
                onClick={scheduleMascotInputGaze}
                onFocus={handleTextFocus}
                onKeyDown={handleTextKeyDown}
                onKeyUp={scheduleMascotInputGaze}
                onMouseUp={scheduleMascotInputGaze}
                onSelect={scheduleMascotInputGaze}
              />
            </div>
            <div className="flex items-center gap-1 px-2 pb-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={t("composer.attach")}
                    className="rounded-full border-0 bg-transparent text-muted-foreground hover:text-foreground"
                    size="icon"
                    type="button"
                    variant="ghost"
                  >
                    <Plus className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("composer.attach")}</TooltipContent>
              </Tooltip>
              <CapabilityBadge mode={currentMode} />
              {compactMutation.isPending ? (
                <span
                  aria-live="polite"
                  className="min-w-0 max-w-40 truncate px-1 text-xs text-muted-foreground"
                  role="status"
                >
                  {t("composer.compacting")}
                </span>
              ) : null}
              <div className="ml-auto flex items-center gap-1">
                <ContextUsageRing token={token} sessionID={sessionID} />
                <ModelReasoningPicker
                  token={token}
                  session={session}
                  reasoningValue={reasoningEffort}
                  onAfterClose={focusTextarea}
                  onReasoningChange={(value) => {
                    setSessionReasoningEffort(value);
                  }}
                  onResolvedChange={handleResolvedModelChange}
                />
              </div>
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
                      onClick={() => {
                        if (!stopEnabled) {
                          return;
                        }
                        cancelMutation.mutate();
                      }}
                    >
                      {cancelMutation.isPending ? (
                        <Loader2 className="animate-spin" />
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
                      aria-label={t("composer.send")}
                      className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
                      disabled={!sendEnabled}
                      size="icon"
                      type="submit"
                      variant={sendEnabled ? "default" : "secondary"}
                    >
                      {submitMutation.isPending || compactMutation.isPending || systemSubmitMutation.isPending || renameMutation.isPending ? (
                        <Loader2 className="animate-spin" />
                      ) : (
                        <ArrowUp />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t("composer.send")}</TooltipContent>
                </Tooltip>
              ) : null}
            </div>
          </div>
          <span className="absolute z-30 size-12 overflow-visible" style={{ left: 6, top: -36 }}>
            <Mascot
              className="size-full overflow-visible"
              gaze={mascotErrorMessage ? { type: "center" } : mascotGaze}
              inputPitchBias={MASCOT_INPUT_PITCH_BIAS}
              mood={mascotErrorMessage ? "error" : mascotMoodFromPhase(turnPhase, running)}
              headShakeSignal={mascotErrorSignal}
              onPointerGaze={setMascotPointerGaze}
            />
          </span>
          {mascotErrorMessage ? (
            <span
              aria-live="polite"
              className="pointer-events-none absolute z-30 max-w-[min(28rem,calc(100%-4rem))] truncate px-1 text-xs font-semibold text-destructive"
              role="status"
              style={{ left: 56, top: -16 }}
            >
              {mascotErrorMessage}
            </span>
          ) : null}
        </div>
      </ChatColumn>
    </form>
  );
}

function SlashCommandMenu({
  commands,
  selectedIndex,
  onHover,
  onSelect,
}: {
  commands: SlashCommand[];
  selectedIndex: number;
  onHover: (index: number) => void;
  onSelect: (command: SlashCommand) => void;
}) {
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  const menuLayout = slashCommandMenuLayout(commands);

  useEffect(() => {
    scrollActiveSlashCommandIntoList(selectedRef.current, listRef.current);
  }, [selectedIndex]);

  return (
    <div
      ref={listRef}
      className="absolute bottom-full left-16 z-20 max-h-64 overflow-y-auto rounded-t-lg border border-border/70 bg-popover/95 p-1 text-sm text-popover-foreground shadow-sm backdrop-blur"
      role="listbox"
      style={{ width: `min(${menuLayout.width}px, calc(100% - 6rem))` }}
    >
      {commands.map((command, index) => (
        <button
          key={command.id}
          ref={index === selectedIndex ? selectedRef : undefined}
          aria-selected={index === selectedIndex}
          aria-label={`${command.command} ${command.description}`}
          className={cn(
            "grid h-8 w-full items-center gap-3 rounded-md px-2 text-left text-[12px] hover:bg-muted",
            index === selectedIndex && "bg-muted text-foreground",
          )}
          role="option"
          style={{ gridTemplateColumns: `${menuLayout.commandColumnWidth}px minmax(0, 1fr)` }}
          type="button"
          onMouseEnter={() => onHover(index)}
          onMouseDown={(event) => {
            event.preventDefault();
            onSelect(command);
          }}
        >
          <span className="font-mono font-medium">{command.command.slice(1)}</span>
          <span className="min-w-0 truncate text-muted-foreground">{command.description}</span>
        </button>
      ))}
    </div>
  );
}

function scrollActiveSlashCommandIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeTop = active.offsetTop;
  const activeBottom = activeTop + active.offsetHeight;
  const visibleTop = list.scrollTop;
  const visibleBottom = visibleTop + list.clientHeight;
  if (activeTop < visibleTop) {
    list.scrollTop = activeTop;
  } else if (activeBottom > visibleBottom) {
    list.scrollTop = activeBottom - list.clientHeight;
  }
}

function slashCommandMenuLayout(commands: SlashCommand[]) {
  const commandColumnWidth = Math.ceil(
    Math.max(48, ...commands.map((command) => estimateInlineTextWidth(command.command.slice(1)))),
  );
  const descriptionWidth = Math.ceil(
    Math.max(0, ...commands.map((command) => estimateInlineTextWidth(command.description))),
  );
  return {
    commandColumnWidth,
    width: clampNumber(28 + commandColumnWidth + 12 + descriptionWidth, 260, 360),
  };
}

function estimateInlineTextWidth(value: string) {
  let width = 0;
  for (const char of value) {
    if (/\s/.test(char)) {
      width += 4;
    } else if (/[\u2E80-\u9FFF\uF900-\uFAFF]/.test(char)) {
      width += 12;
    } else if (/[A-Z]/.test(char)) {
      width += 8;
    } else if (/[.,;:!?()[\]{}'"`-]/.test(char)) {
      width += 4;
    } else {
      width += 7;
    }
  }
  return width;
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function parseSlashSubmitCommand(text: string): SlashSubmitCommand | null {
  if (text === "/clear") {
    return { id: "clear" };
  }
  const compactMatch = text.match(/^\/compact(?:\s+([\s\S]*))?$/);
  if (compactMatch) {
    return { id: "compact", hint: (compactMatch[1] || "").trim() };
  }
  const renameMatch = text.match(/^\/rename(?:\s+([\s\S]*))?$/);
  if (renameMatch) {
    return { id: "rename", title: (renameMatch[1] || "").trim() };
  }
  const summaryMatch = text.match(/^\/summary(?:\s+([\s\S]*))?$/);
  if (summaryMatch) {
    return { id: "summary", hint: (summaryMatch[1] || "").trim() };
  }
  return null;
}

function summaryPrompt(hint: string, t: (key: string) => string) {
  if (hint) {
    return t("composer.summaryPromptWithHint").replace("{hint}", hint);
  }
  return t("composer.summaryPrompt");
}

function slashCommandQuery(text: string, commands: SlashCommand[]): string | null {
  if (!text.startsWith("/") || /\s/.test(text)) {
    return null;
  }
  const query = text.slice(1);
  if (commands.some((command) => command.command === text)) {
    return null;
  }
  return query;
}

function compactErrorMessage(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError) {
    switch (error.code) {
      case "compact_empty":
        return t("composer.compactEmpty");
      case "compact_running":
        return t("composer.compactRunning");
      case "turn_running":
        return t("composer.turnRunning");
      case "no_model":
        return t("composer.noModel");
      case "provider_config":
        return t("composer.providerConfig");
    }
  }
  return t("composer.compactFailed");
}

function CapabilityBadge({ mode }: { mode: Session["activeMode"] }) {
  const { t } = useI18n();
  const label = t(`mode.${mode}`);
  const title = t("mode.current").replace("{mode}", label);
  const iconByMode = {
    chat: { Icon: MessageCircle, className: "text-muted-foreground/80" },
    research: { Icon: SearchCheck, className: "text-blue-600/80 dark:text-blue-300/80" },
    workspace: { Icon: Code2, className: "text-emerald-600/80 dark:text-emerald-300/80" },
  } satisfies Record<Session["activeMode"], { Icon: LucideIcon; className: string }>;
  const { Icon, className } = iconByMode[mode];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          aria-label={title}
          className={`inline-flex size-5 shrink-0 items-center justify-center rounded-full ${className}`}
          role="img"
        >
          <Icon aria-hidden="true" className="size-3.5" strokeWidth={2.2} />
        </span>
      </TooltipTrigger>
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  );
}

function ComposerApprovalBar({ approval, token }: { approval?: ComposerApproval; token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [pendingAction, setPendingAction] = useState<"turn" | "session" | "deny" | null>(null);
  const [selectedWorkspaceDirs, setSelectedWorkspaceDirs] = useState<string[]>([]);
  const [pickingWorkspaceDir, setPickingWorkspaceDir] = useState(false);
  useEffect(() => {
    setSelectedWorkspaceDirs([]);
  }, [approval?.approvalID]);
  if (!approval) {
    return null;
  }
  const current = approval;
  const targetMode = approvalTargetMode(current.payload);
  const title = approvalTitle(current, targetMode, t);
  const pending = pendingAction !== null;
  const isWorkspaceApproval = targetMode === "workspace";
  const payloadWorkspaceDirs = workspaceDirsFromPayload(current.payload);
  const hasPayloadWorkspaceDirs = payloadWorkspaceDirs.length > 0;
  const workspaceDirs = hasPayloadWorkspaceDirs ? payloadWorkspaceDirs : selectedWorkspaceDirs;
  const needsWorkspaceDir = isWorkspaceApproval && workspaceDirs.length === 0;
  const suggestedDirName = suggestedWorkspaceDirName(current.payload);

  async function approve(scope: "turn" | "session") {
    if (pending || needsWorkspaceDir) {
      return;
    }
    setPendingAction(scope);
    try {
      await approveApproval(token, current.sessionID, current.approvalID, scope, isWorkspaceApproval ? workspaceDirs : []);
      if (scope === "session") {
        await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      }
    } finally {
      setPendingAction(null);
    }
  }

  async function pickWorkspaceDirs() {
    if (pending || pickingWorkspaceDir || hasPayloadWorkspaceDirs) {
      return;
    }
    setPickingWorkspaceDir(true);
    try {
      const dirs = await pickWorkspaceDirectories(t);
      if (dirs.length > 0) {
        setSelectedWorkspaceDirs((prev) => dedupeStrings([...prev, ...dirs]));
      }
    } catch {
      toast.error(t("transcript.approvalWorkspaceDirPickFailed"));
    } finally {
      setPickingWorkspaceDir(false);
    }
  }

  function removeWorkspaceDir(dir: string) {
    setSelectedWorkspaceDirs((prev) => prev.filter((item) => item !== dir));
  }

  async function deny() {
    if (pending) {
      return;
    }
    setPendingAction("deny");
    try {
      await denyApproval(token, current.sessionID, current.approvalID);
    } finally {
      setPendingAction(null);
    }
  }

  return (
    <div className="absolute right-8 bottom-full left-16 z-0 grid gap-1 rounded-t-lg border border-border/70 bg-popover/95 px-3 py-2 text-xs text-popover-foreground shadow-sm backdrop-blur">
      <div className="flex min-w-0 items-center gap-1.5">
        <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium">{title}</span>
      </div>
      {current.reason ? <div className="line-clamp-2 leading-5 text-muted-foreground">{current.reason}</div> : null}
      {isWorkspaceApproval ? (
        <div className="grid gap-1">
          <div className="text-[11px] font-medium text-muted-foreground">
            {t("transcript.approvalWorkspaceDirs")}
          </div>
          {workspaceDirs.length > 0 ? (
            <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
              {workspaceDirs.map((dir) => (
                <div key={dir} className="flex min-w-0 items-center gap-1" title={dir}>
                  <span className="min-w-0 flex-1 truncate">{dir}</span>
                  {!hasPayloadWorkspaceDirs ? (
                    <button
                      aria-label={t("common.delete")}
                      className="grid size-4 shrink-0 place-items-center rounded-full text-muted-foreground hover:text-foreground"
                      type="button"
                      onClick={() => removeWorkspaceDir(dir)}
                    >
                      <X className="size-3" />
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          ) : null}
          {!hasPayloadWorkspaceDirs ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                className="h-6 gap-1 rounded-full px-2 text-[11px]"
                disabled={pending || pickingWorkspaceDir}
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => void pickWorkspaceDirs()}
              >
                {pickingWorkspaceDir ? <Loader2 className="size-3 animate-spin" /> : <FolderOpen className="size-3" />}
                {t("transcript.approvalWorkspaceDirChoose")}
              </Button>
              {suggestedDirName ? <span className="text-[11px] text-muted-foreground">{t("transcript.approvalWorkspaceDirsSuggested").replace("{name}", suggestedDirName)}</span> : null}
            </div>
          ) : null}
          {needsWorkspaceDir ? <div className="text-[11px] text-destructive">{t("transcript.approvalWorkspaceDirsRequired")}</div> : null}
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-1.5">
        <Button
          className="h-6 gap-1 rounded-full px-2 text-[11px]"
          disabled={pending || needsWorkspaceDir}
          size="sm"
          type="button"
          onClick={() => void approve("turn")}
        >
          {pendingAction === "turn" ? <Loader2 className="size-3 animate-spin" /> : <Check className="size-3" />}
          {t("transcript.approvalAllowTurn")}
        </Button>
        <Button
          className="h-6 gap-1 rounded-full px-2 text-[11px]"
          disabled={pending || needsWorkspaceDir}
          size="sm"
          type="button"
          variant="secondary"
          onClick={() => void approve("session")}
        >
          {pendingAction === "session" ? <Loader2 className="size-3 animate-spin" /> : <ShieldCheck className="size-3" />}
          {t("transcript.approvalAllowSession")}
        </Button>
        <Button
          className="h-6 gap-1 rounded-full px-1.5 text-[11px]"
          disabled={pending}
          size="sm"
          type="button"
          variant="ghost"
          onClick={() => void deny()}
        >
          {pendingAction === "deny" ? <Loader2 className="size-3 animate-spin" /> : <X className="size-3" />}
          {t("transcript.approvalDeny")}
        </Button>
      </div>
    </div>
  );
}

function selectPendingApproval(assistants: Record<string, AssistantOverlay>, sessionID: string, runningTurnID?: string): ComposerApproval | undefined {
  if (runningTurnID) {
    const running = assistants[runningTurnID];
    const approval = firstPendingApproval(running);
    if (approval) {
      return approval;
    }
  }
  for (const overlay of Object.values(assistants)) {
    if (overlay.turnID === runningTurnID || overlay.sessionID !== sessionID) {
      continue;
    }
    const approval = firstPendingApproval(overlay);
    if (approval) {
      return approval;
    }
  }
  return undefined;
}

function firstPendingApproval(overlay: AssistantOverlay | undefined): ComposerApproval | undefined {
  if (!overlay || overlay.status !== "streaming") {
    return undefined;
  }
  return overlay.parts.find(isPendingApprovalPart);
}

function isPendingApprovalPart(part: AssistantOverlayPart): part is ComposerApproval {
  return part.type === "approval" && !part.status;
}

function approvalTargetMode(payload: unknown) {
  if (payload && typeof payload === "object" && "targetMode" in payload && typeof payload.targetMode === "string") {
    return payload.targetMode;
  }
  return "";
}

function workspaceDirsFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object" || !("workspaceDirs" in payload) || !Array.isArray(payload.workspaceDirs)) {
    return [];
  }
  return dedupeStrings(payload.workspaceDirs.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean));
}

function suggestedWorkspaceDirName(payload: unknown) {
  if (payload && typeof payload === "object" && "suggestedDirName" in payload && typeof payload.suggestedDirName === "string") {
    return payload.suggestedDirName.trim();
  }
  return "";
}

function dedupeStrings(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index);
}

async function pickWorkspaceDirectories(t: (key: string) => string) {
  const { Dialogs } = await import("@wailsio/runtime");
  const result = await Dialogs.OpenFile({
    AllowsMultipleSelection: true,
    ButtonText: t("transcript.approvalWorkspaceDirChoose"),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    CanCreateDirectories: true,
    Message: t("transcript.approvalWorkspaceDirChooseMessage"),
    Title: t("transcript.approvalWorkspaceDirChooseTitle"),
  });
  const dirs = Array.isArray(result) ? result : result ? [result] : [];
  return dedupeStrings(dirs.map((dir) => dir.trim()).filter(Boolean));
}

function approvalTitle(approval: ComposerApproval, targetMode: string, t: (key: string) => string) {
  if (approval.approvalKind === "capability") {
    const mode = targetMode ? t(`mode.${targetMode}`) : "";
    if (mode) {
      return t("transcript.approvalCapabilityTitle").replace("{mode}", mode);
    }
  }
  return approval.title || t("transcript.approvalTitle");
}

function mascotMoodFromPhase(phase: TurnPhaseState | undefined, running: boolean): MascotMood {
  if (phase?.phase === "streaming_text") {
    return "ready";
  }
  if (running || phase?.phase === "submitting" || phase?.phase === "awaiting_model") {
    return "thinking";
  }
  return "idle";
}
