import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Send, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { APIError, cancelTurn, submitMessage, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ModelPicker } from "@/components/ModelPicker";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { useOverlayStore } from "@/state/overlayStore";

const composerSchema = z.object({
  text: z.string().trim().min(1),
});

type ComposerProps = {
  token: string;
  session: Session;
};

export function Composer({ token, session }: ComposerProps) {
  const sessionID = session.id;
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const [cancelLocked, setCancelLocked] = useState(false);
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(crypto.randomUUID());
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });

  const submitMutation = useMutation({
    mutationFn: async (value: z.infer<typeof composerSchema>) => {
      const clientMessageID = draftIDRef.current;
      addPendingUser({
        sessionID,
        clientMessageID,
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      return submitMessage(token, sessionID, { clientMessageID, text: value.text });
    },
    onSuccess: async () => {
      draftIDRef.current = crypto.randomUUID();
      form.reset({ text: "" });
      await queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      // 提交未被接受,canonical 不会出现这条消息:pending 气泡必须撤掉,
      // 文本留在 composer 里供重试(同一 draft ID)
      removePendingUser(sessionID, draftIDRef.current);
      if (error instanceof APIError && error.code === "turn_running") {
        form.setError("text", { message: t("composer.turnRunning") });
        return;
      }
      form.setError("text", { message: error instanceof Error ? error.message : t("composer.submitFailed") });
    },
  });

  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });

  const submitDraft = (value: z.infer<typeof composerSchema>) => {
    setCancelLocked(true);
    submitMutation.mutate(value);
  };

  useEffect(() => {
    if (!cancelLocked) {
      return;
    }
    const timer = window.setTimeout(() => setCancelLocked(false), 500);
    return () => window.clearTimeout(timer);
  }, [cancelLocked]);

  return (
    <form
      className="relative px-5 pb-4"
      onSubmit={form.handleSubmit(submitDraft)}
    >
      {/* 与对话区同底无分割线,衔接用渐变遮罩:滚动内容在贴近输入区时淡出 */}
      <div className="pointer-events-none absolute inset-x-0 -top-10 h-10 bg-gradient-to-t from-background to-transparent" />
      <div className="mx-auto w-full max-w-3xl">
        <div className="rounded-xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
          <Textarea
            className="max-h-40 min-h-16 resize-none rounded-xl border-0 bg-transparent shadow-none focus-visible:ring-0 dark:bg-transparent"
            placeholder={t("composer.messagePlaceholder")}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void form.handleSubmit(submitDraft)();
              }
            }}
            {...form.register("text")}
          />
          <div className="flex items-center justify-between gap-2 px-2 pb-2">
            <ModelPicker token={token} session={session} />
            {runningTurnID ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={t("composer.stop")}
                    disabled={cancelMutation.isPending || cancelLocked}
                    size="icon"
                    type="button"
                    variant="outline"
                    onClick={() => cancelMutation.mutate()}
                  >
                    {cancelMutation.isPending ? <Loader2 className="animate-spin" /> : <Square />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{cancelLocked ? t("composer.stopPending") : t("composer.stop")}</TooltipContent>
              </Tooltip>
            ) : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button aria-label={t("composer.send")} disabled={submitMutation.isPending} size="icon" type="submit">
                    {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <Send />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("composer.send")}</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
        {form.formState.errors.text ? (
          <div className="mt-2 text-xs text-destructive">{form.formState.errors.text.message}</div>
        ) : null}
      </div>
    </form>
  );
}
