import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, CircleAlert, Loader2, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type FocusEvent, type KeyboardEvent } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import {
  APIError,
  createSession,
  deleteSession,
  listProviders,
  submitMessage,
  updateSession,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { Mascot, type MascotGaze, type MascotGazePoint } from "@/components/Mascot";
import { ModelPicker, type ResolvedModelSelection } from "@/components/ModelPicker";
import { ProviderProfileEditorDialog } from "@/components/ProviderProfileEditorDialog";
import { ProviderCustomCard, ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
import { defaultReasoningEffortForSelection, ReasoningEffortChip, reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { getOrderedProviderPresets, type ProviderPreset } from "@/provider/presets";
import { useDraftStore, type DraftModelValue } from "@/state/draftStore";
import { useOverlayStore } from "@/state/overlayStore";

const draftSchema = z.object({
  text: z.string(),
});

type DraftValue = z.infer<typeof draftSchema>;
type QuickSubmit = { id: number; text: string };

const suggestionKeys = ["draft.suggest.1", "draft.suggest.2", "draft.suggest.3"] as const;
const emptyDraftModel: DraftModelValue = {};

export function DraftConversation({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const [quickSubmit, setQuickSubmit] = useState<QuickSubmit | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const modelValue = useDraftStore((state) => state.model);
  const setModelValue = useDraftStore((state) => state.setModel);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const profiles = providersQuery.data?.providers || [];
  const hasConfiguredModel = profiles.some((profile) => profile.models.some((model) => model.id));
  const draftModelIsValid = providersQuery.isSuccess && isDraftModelAvailable(profiles, modelValue);
  const composerModelValue = draftModelIsValid ? modelValue : emptyDraftModel;
  const showPresetSetup = providersQuery.isSuccess && !hasConfiguredModel;
  const [mascotGaze, setMascotGaze] = useState<MascotGaze>({ type: "pointer" });
  const setMascotPointerGaze = useCallback(() => {
    setMascotGaze((current) => (current.type === "pointer" ? current : { type: "pointer" }));
  }, []);
  const setMascotInputGaze = useCallback((target: MascotGazePoint | null) => {
    setMascotGaze(target ? { type: "input", target } : { type: "pointer" });
  }, []);

  useEffect(() => {
    if (!providersQuery.isSuccess || (!modelValue.provider && !modelValue.model) || draftModelIsValid) {
      return;
    }
    setModelValue(emptyDraftModel);
  }, [draftModelIsValid, modelValue.model, modelValue.provider, providersQuery.isSuccess, setModelValue]);

  if (providersQuery.isPending) {
    return <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden" />;
  }

  return (
    <div className="pudding-draft-stage relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="pudding-draft-body">
        <div className="pudding-draft-title">
          <Mascot
            className="pudding-draft-mascot"
            gaze={mascotGaze}
            mood="idle"
            onPointerGaze={setMascotPointerGaze}
          />
          <h1 className="pudding-draft-heading">{t("draft.title")}</h1>
        </div>
        <div className={cn("pudding-draft-stack", showPresetSetup && "pudding-draft-stack-with-setup")}>
          {submitError ? (
            <ChatColumn className="mb-3">
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            </ChatColumn>
          ) : null}
          {showPresetSetup ? (
            <DraftPresetSetup
              className="mb-12"
              profiles={profiles}
              presets={getOrderedProviderPresets(locale)}
              token={token}
              onCreated={(profile, model) => {
                setModelValue({ provider: profile.id, model });
                setSubmitError(null);
              }}
            />
          ) : null}
          <DraftComposer
            quickSubmit={quickSubmit}
            token={token}
            modelReady={draftModelIsValid}
            modelValue={composerModelValue}
            onModelValueChange={setModelValue}
            onMascotInputGazeChange={setMascotInputGaze}
            onSubmitError={setSubmitError}
          />
          {!showPresetSetup ? (
            <ChatColumn className="mt-8 flex flex-wrap items-center justify-center gap-2 px-2">
              {suggestionKeys.map((key, index) => {
                const text = t(key);
                return (
                  <button
                    key={key}
                    className="rounded-full border border-input bg-background px-4 py-1.5 text-[13px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    type="button"
                    onClick={() => setQuickSubmit({ id: Date.now() + index, text })}
                  >
                    {text}
                  </button>
                );
              })}
            </ChatColumn>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function isRejectedSubmit(error: unknown) {
  return error instanceof APIError;
}

function DraftPresetSetup({
  className,
  profiles,
  presets,
  token,
  onCreated,
}: {
  className?: string;
  profiles: ProviderProfile[];
  presets: ProviderPreset[];
  token: string;
  onCreated: (profile: ProviderProfile, model: string) => void;
}) {
  const [selected, setSelected] = useState<ProviderPreset | null>(null);
  const [customOpen, setCustomOpen] = useState(false);

  return (
    <div className={cn("pudding-draft-preset-panel", className)}>
      <ProviderPresetGrid className="pudding-draft-preset-grid" presets={presets} onSelect={setSelected}>
        <ProviderCustomCard onSelect={() => setCustomOpen(true)} />
      </ProviderPresetGrid>
      <ProviderPresetCreateDialog
        open={Boolean(selected)}
        preset={selected}
        profiles={profiles}
        token={token}
        onCreated={(profile, variant) => {
          const model = profile.models[0]?.id || variant.models[0]?.id || "";
          if (model) {
            onCreated(profile, model);
          }
        }}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null);
          }
        }}
      />
      <ProviderProfileEditorDialog
        open={customOpen}
        profiles={profiles}
        token={token}
        onOpenChange={setCustomOpen}
        onSaved={(profile) => {
          const model = profile.models[0]?.id || "";
          if (model) {
            onCreated(profile, model);
          }
        }}
      />
    </div>
  );
}

function DraftComposer({
  token,
  quickSubmit,
  modelReady,
  modelValue,
  onModelValueChange,
  onMascotInputGazeChange,
  onSubmitError,
}: {
  token: string;
  quickSubmit: QuickSubmit | null;
  modelReady: boolean;
  modelValue: DraftModelValue;
  onModelValueChange: (model: DraftModelValue) => void;
  onMascotInputGazeChange: (target: MascotGazePoint | null) => void;
  onSubmitError: (message: string | null) => void;
}) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const acceptSubmittingTurn = useOverlayStore((state) => state.acceptSubmittingTurn);
  const clearSubmittingTurn = useOverlayStore((state) => state.clearSubmittingTurn);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const startSubmittingTurn = useOverlayStore((state) => state.startSubmittingTurn);
  const draftText = useDraftStore((state) => state.text);
  const setDraftText = useDraftStore((state) => state.setText);
  const clearDraft = useDraftStore((state) => state.clear);
  const [resolvedModel, setResolvedModel] = useState<ResolvedModelSelection | null>(null);
  const [draftReasoningEffort, setDraftReasoningEffortValue] = useState("");
  const [draftReasoningModelKey, setDraftReasoningModelKey] = useState("");
  const draftIDRef = useRef<string>(newClientID());
  const quickSubmitIDRef = useRef<number | null>(null);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotGazeRafRef = useRef(0);
  const form = useForm<DraftValue>({
    resolver: zodResolver(draftSchema),
    defaultValues: { text: draftText },
  });
  const canSend = Boolean(form.watch("text").trim());
  const sendEnabled = canSend && modelReady;
  const textField = form.register("text");
  const reasoningOptions = useMemo(() => reasoningEffortOptionsForSelection(resolvedModel), [resolvedModel]);
  const defaultReasoningEffort = useMemo(() => defaultReasoningEffortForSelection(resolvedModel), [resolvedModel]);
  const resolvedModelKey = resolvedModel ? `${resolvedModel.provider}:${resolvedModel.model}` : "";
  const reasoningEffort = resolvedModelKey && draftReasoningModelKey === resolvedModelKey ? draftReasoningEffort : "";
  const setDraftReasoningEffort = useCallback(
    (value: string) => {
      if (!resolvedModelKey) {
        return;
      }
      setDraftReasoningModelKey(resolvedModelKey);
      setDraftReasoningEffortValue(value);
    },
    [resolvedModelKey],
  );

  const updateMascotInputGaze = useCallback(() => {
    const textArea = textAreaRef.current;
    if (!textArea || document.activeElement !== textArea) {
      return;
    }
    onMascotInputGazeChange(mascotGazePointFromCaret(textArea));
  }, [onMascotInputGazeChange]);

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

  useEffect(() => {
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setDraftReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setDraftReasoningEffort]);

  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
      onMascotInputGazeChange(null);
    };
  }, [onMascotInputGazeChange]);

  const submitMutation = useMutation({
    mutationFn: async (value: DraftValue) => {
      const clientMessageID = draftIDRef.current;
      const activeReasoningEffort = reasoningEffort && reasoningOptions.includes(reasoningEffort) ? reasoningEffort : "";
      if (!modelValue.provider || !modelValue.model) {
        throw new APIError(400, "no_model");
      }
      let created = await createSession(token, {
        title: "",
        provider: modelValue.provider,
        model: modelValue.model,
      });
      if (activeReasoningEffort) {
        created = await updateSession(token, created.id, { reasoningEffort: activeReasoningEffort });
      }
      cacheCreatedSession(queryClient, created);
      await navigate({
        to: "/",
        search: (prev) => {
          const next = { ...(prev as AppSearch), session: created.id };
          delete next.draft;
          return next;
        },
        replace: true,
      });
      addPendingUser({
        sessionID: created.id,
        clientMessageID,
        status: "submitting",
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      startSubmittingTurn(created.id, clientMessageID);
      try {
        const result = await submitMessage(token, created.id, {
          clientMessageID,
          reasoningEffort: activeReasoningEffort || undefined,
          text: value.text,
        });
        if (result.queued || !result.turnID) {
          clearSubmittingTurn(created.id, clientMessageID);
        } else {
          acceptSubmittingTurn(created.id, clientMessageID, result.turnID);
        }
      } catch (error) {
        // 只有拿到后端明确拒绝的响应时才回滚 draft session。
        // 网络中断/响应解析失败可能发生在后端已经 accepted 之后,这类错误保留 session 交给 SSE/query 对账。
        if (isRejectedSubmit(error)) {
          clearSubmittingTurn(created.id, clientMessageID);
          removePendingUser(created.id, clientMessageID);
          try {
            await deleteSession(token, created.id);
          } catch {
            // 清理失败不覆盖真正的提交错误;下一次 sessions refetch 会对齐状态。
          }
          removeCachedSession(queryClient, created.id);
          await navigate({
            to: "/",
            search: (prev) => {
              const next = { ...(prev as AppSearch), draft: "1" };
              delete next.session;
              return next;
            },
            replace: true,
          });
        }
        throw error;
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.turns(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.messages(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      clearDraft();
      return created;
    },
    onSuccess: () => {
      onSubmitError(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
    },
    onError: (error) => {
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      if (failure.surface === "conversation") {
        onSubmitError(failure.message);
        return;
      }
      onSubmitError(null);
      toast.error(failure.message);
    },
  });

  const submitText = useCallback(
    (raw: string) => {
      const text = raw.trim();
      if (!text || !modelReady || submitMutation.isPending) {
        return;
      }
      onSubmitError(null);
      submitMutation.mutate({ text });
    },
    [modelReady, onSubmitError, submitMutation],
  );

  useEffect(() => {
    if (!quickSubmit) {
      return;
    }
    if (quickSubmitIDRef.current === quickSubmit.id) {
      return;
    }
    quickSubmitIDRef.current = quickSubmit.id;
    form.setValue("text", quickSubmit.text);
    setDraftText(quickSubmit.text);
    submitText(quickSubmit.text);
  }, [form, quickSubmit, setDraftText, submitText]);

  const submitDraft = (value: DraftValue) => submitText(value.text);
  const handleResolvedModelChange = useCallback((next: ResolvedModelSelection | null) => {
    setResolvedModel(next);
    if (next) {
      onModelValueChange({ provider: next.provider, model: next.model });
    }
  }, [onModelValueChange]);
  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };
  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    onMascotInputGazeChange(null);
  };
  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    void textField.onChange(event);
    setDraftText(event.currentTarget.value);
    scheduleMascotInputGaze();
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      if (ime.isComposing(event)) {
        scheduleMascotInputGaze();
        return;
      }
      event.preventDefault();
      if (sendEnabled && !submitMutation.isPending) {
        void form.handleSubmit(submitDraft)();
      }
    }
    scheduleMascotInputGaze();
  };

  return (
    <form className="relative shrink-0" onSubmit={form.handleSubmit(submitDraft)}>
      <ChatColumn>
        <div className="relative rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
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
              onFocus={scheduleMascotInputGaze}
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
            <ModelPicker
              className="ml-auto"
              token={token}
              value={modelValue}
              onChange={onModelValueChange}
              onAfterClose={focusTextarea}
              onResolvedChange={handleResolvedModelChange}
            />
            {reasoningOptions.length > 0 ? (
              <ReasoningEffortChip
                defaultValue={defaultReasoningEffort}
                options={reasoningOptions}
                value={reasoningEffort}
                onAfterClose={focusTextarea}
                onValueChange={setDraftReasoningEffort}
              />
            ) : null}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("composer.send")}
                  className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
                  disabled={!sendEnabled || submitMutation.isPending}
                  size="icon"
                  type="submit"
                  variant={sendEnabled ? "default" : "secondary"}
                >
                  {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <ArrowUp />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("composer.send")}</TooltipContent>
            </Tooltip>
          </div>
        </div>
      </ChatColumn>
    </form>
  );
}

function isDraftModelAvailable(profiles: ProviderProfile[], value: DraftModelValue): boolean {
  if (!value.provider || !value.model) {
    return false;
  }
  const profile = profiles.find((item) => item.id === value.provider);
  return Boolean(profile?.models.some((model) => model.id === value.model));
}

function cacheCreatedSession(queryClient: ReturnType<typeof useQueryClient>, created: Session) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return { sessions: [created] };
    }
    return { sessions: [created, ...previous.sessions.filter((session) => session.id !== created.id)] };
  });
}

function removeCachedSession(queryClient: ReturnType<typeof useQueryClient>, sessionID: string) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return previous;
    }
    return { sessions: previous.sessions.filter((session) => session.id !== sessionID) };
  });
}

function mascotGazePointFromCaret(textArea: HTMLTextAreaElement): MascotGazePoint {
  return getTextAreaCaretClientPoint(textArea);
}
