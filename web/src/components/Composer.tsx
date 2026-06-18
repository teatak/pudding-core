import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, Loader2, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { APIError, cancelTurn, submitMessage, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
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
  // 停止态双源:overlay 的 runningTurns(本地实时)|| session 快照的 running
  // (后端 turns 表派生)。中途刷新走 SSE tail 不回放 turn.started,若此时
  // provider 暂无 delta,overlay 不知道有 turn 在跑——session.running 兜底,
  // 保证停止按钮不丢(cancel 按 sessionID 取消,无需 turnID)。
  const overlayRunning = useOverlayStore((state) => Boolean(state.runningTurns[sessionID]));
  const running = overlayRunning || session.running;
  const [cancelLocked, setCancelLocked] = useState(false);
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(crypto.randomUUID());
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });
  // 空消息(含纯空白)不可发:禁用发送按钮 + Enter 不触发提交,从源头避免
  // 弹出 zod min(1) 的校验错误。watch 让按钮态随输入实时更新。
  const canSend = Boolean(form.watch("text").trim());

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
      // 标题自动生成由后端 titler 负责(provisional + LLM,session.titled
      // 事件回推),前端不写标题
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
      if (error instanceof APIError && error.code === "no_model") {
        form.setError("text", { message: t("composer.noModel") });
        return;
      }
      form.setError("text", { message: error instanceof Error ? error.message : t("composer.submitFailed") });
    },
  });
  const sendEnabled = canSend && !submitMutation.isPending;

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
      className="relative shrink-0 pt-2 pb-4"
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
        <div className="relative rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
          <div className="px-4 pt-4 pb-2">
            <Textarea
              className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-sm leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
              placeholder={t("composer.messagePlaceholder")}
              rows={1}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  if (sendEnabled) {
                    void form.handleSubmit(submitDraft)();
                  }
                }
              }}
              {...form.register("text")}
            />
          </div>
          <div className="flex items-center justify-between gap-2 px-2 pb-2">
            <ModelPicker token={token} session={session} />
            {running ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={t("composer.stop")}
                    className="rounded-full bg-foreground text-background shadow-sm hover:bg-foreground/90 hover:text-background"
                    size="icon"
                    type="button"
                    variant="ghost"
                    onClick={() => {
                      if (cancelLocked) {
                        return;
                      }
                      cancelMutation.mutate();
                    }}
                  >
                    {cancelMutation.isPending ? (
                      <Loader2 className="animate-spin" />
                    ) : (
                      <Square className="size-3 fill-current stroke-current" />
                    )}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("composer.stop")}</TooltipContent>
              </Tooltip>
            ) : (
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
                    {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <ArrowUp />}
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
      </ChatColumn>
    </form>
  );
}
