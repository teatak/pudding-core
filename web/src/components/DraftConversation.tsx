import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, Loader2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { APIError, createSession, deleteSession, submitMessage, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { Mascot } from "@/components/Mascot";
import { ModelPicker } from "@/components/ModelPicker";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
import { useOverlayStore } from "@/state/overlayStore";

const draftSchema = z.object({
  text: z.string().trim().min(1),
});

type DraftValue = z.infer<typeof draftSchema>;
type QuickSubmit = { id: number; text: string };
type DraftModelValue = { provider?: string; model?: string };

const suggestionKeys = ["draft.suggest.1", "draft.suggest.2", "draft.suggest.3"] as const;

export function DraftConversation({ token }: { token: string }) {
  const { t } = useI18n();
  const [quickSubmit, setQuickSubmit] = useState<QuickSubmit | null>(null);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col justify-center px-4 pb-20">
        <div className="flex flex-col items-center justify-center">
          <Mascot className="size-32 overflow-visible" />
          <h1 className="mt-4 text-3xl font-medium text-foreground">{t("draft.title")}</h1>
        </div>
        <div className="mt-14 flex flex-col">
          <DraftComposer quickSubmit={quickSubmit} token={token} />
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
        </div>
      </div>
    </div>
  );
}

function DraftComposer({ token, quickSubmit }: { token: string; quickSubmit: QuickSubmit | null }) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const [modelValue, setModelValue] = useState<DraftModelValue>({});
  const draftIDRef = useRef<string>(crypto.randomUUID());
  const quickSubmitIDRef = useRef<number | null>(null);
  const form = useForm<DraftValue>({
    resolver: zodResolver(draftSchema),
    defaultValues: { text: "" },
  });
  const canSend = Boolean(form.watch("text").trim());

  const submitMutation = useMutation({
    mutationFn: async (value: DraftValue) => {
      const clientMessageID = draftIDRef.current;
      const created = await createSession(token, {
        title: "",
        provider: modelValue.provider,
        model: modelValue.model,
      });
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
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      try {
        await submitMessage(token, created.id, { clientMessageID, text: value.text });
      } catch (error) {
        removePendingUser(created.id, clientMessageID);
        if (error instanceof APIError) {
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
      await queryClient.invalidateQueries({ queryKey: queryKeys.messages(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      return created;
    },
    onSuccess: () => {
      draftIDRef.current = crypto.randomUUID();
      form.reset({ text: "" });
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "no_model") {
        form.setError("text", { message: t("composer.noModel") });
        return;
      }
      form.setError("text", { message: error instanceof Error ? error.message : t("composer.submitFailed") });
    },
  });

  const submitText = useCallback(
    (raw: string) => {
      const text = raw.trim();
      if (!text || submitMutation.isPending) {
        return;
      }
      submitMutation.mutate({ text });
    },
    [submitMutation],
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
    submitText(quickSubmit.text);
  }, [form, quickSubmit, submitText]);

  const submitDraft = (value: DraftValue) => submitText(value.text);

  return (
    <form className="relative shrink-0" onSubmit={form.handleSubmit(submitDraft)}>
      <ChatColumn>
        <div className="relative rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
          <div className="px-4 pt-4 pb-2">
            <Textarea
              className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-sm leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
              placeholder={t("composer.messagePlaceholder")}
              rows={1}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  if (canSend && !submitMutation.isPending) {
                    void form.handleSubmit(submitDraft)();
                  }
                }
              }}
              {...form.register("text")}
            />
          </div>
          <div className="flex items-center justify-between gap-2 px-2 pb-2">
            <ModelPicker token={token} value={modelValue} onChange={setModelValue} />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("composer.send")}
                  className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
                  disabled={!canSend || submitMutation.isPending}
                  size="icon"
                  type="submit"
                  variant={canSend ? "default" : "secondary"}
                >
                  {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <ArrowUp />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("composer.send")}</TooltipContent>
            </Tooltip>
          </div>
        </div>
        {form.formState.errors.text ? (
          <div className="mt-2 text-xs text-destructive">{form.formState.errors.text.message}</div>
        ) : null}
      </ChatColumn>
    </form>
  );
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
