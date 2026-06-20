import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, Loader2, Plus } from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FocusEvent,
  type KeyboardEvent,
} from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import { APIError, cancelTurn, submitMessage, updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { Mascot, type MascotGaze, type MascotGazePoint, type MascotMood } from "@/components/Mascot";
import { ModelPicker } from "@/components/ModelPicker";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { getSubmitFailure } from "@/lib/submitFailure";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { useOverlayStore, type TurnPhaseState } from "@/state/overlayStore";

const composerSchema = z.object({
  text: z.string(),
});

const MASCOT_INPUT_PITCH_BIAS = 0.45;

type ComposerProps = {
  token: string;
  session: Session;
  onSubmitError?: (message: string | null) => void;
};

export function Composer({ token, session, onSubmitError }: ComposerProps) {
  const sessionID = session.id;
  const queryClient = useQueryClient();
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
  const running = overlayRunning || session.running;
  const [cancelLocked, setCancelLocked] = useState(false);
  const [resolvedModel, setResolvedModel] = useState<{ provider: string; model: string } | null>(null);
  const [mascotGaze, setMascotGaze] = useState<MascotGaze>({ type: "pointer" });
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(crypto.randomUUID());
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotGazeRafRef = useRef(0);
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });
  // 空消息(含纯空白)不可发:禁用发送按钮 + Enter 不触发提交,从源头避免
  // 弹出 zod min(1) 的校验错误。watch 让按钮态随输入实时更新。
  const canSend = Boolean(form.watch("text").trim());
  const textField = form.register("text");

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
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      const result = await submitMessage(token, sessionID, { clientMessageID, text: value.text });
      acceptSubmittingTurn(sessionID, clientMessageID, result.turnID);
      return result;
    },
    onSuccess: async () => {
      onSubmitError?.(null);
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
      clearSubmittingTurn(sessionID, draftIDRef.current);
      removePendingUser(sessionID, draftIDRef.current);
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      if (failure.surface === "conversation") {
        onSubmitError?.(failure.message);
        return;
      }
      onSubmitError?.(null);
      toast.error(failure.message);
    },
  });
  const sendEnabled = canSend && Boolean(resolvedModel) && !submitMutation.isPending;

  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });

  const submitDraft = (value: z.infer<typeof composerSchema>) => {
    const text = value.text.trim();
    if (!text || submitMutation.isPending) {
      return;
    }
    onSubmitError?.(null);
    setCancelLocked(true);
    startSubmittingTurn(sessionID, draftIDRef.current);
    submitMutation.mutate({ text });
  };

  const handleResolvedModelChange = useCallback((next: { provider: string; model: string } | null) => {
    setResolvedModel((current) => {
      if (!next) {
        return current ? null : current;
      }
      if (current?.provider === next.provider && current.model === next.model) {
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
  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };
  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    setMascotInputGaze(null);
  };
  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    void textField.onChange(event);
    scheduleMascotInputGaze();
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      if (sendEnabled) {
        void form.handleSubmit(submitDraft)();
      }
    }
    scheduleMascotInputGaze();
  };

  useEffect(() => {
    if (!cancelLocked) {
      return;
    }
    const timer = window.setTimeout(() => setCancelLocked(false), 500);
    return () => window.clearTimeout(timer);
  }, [cancelLocked]);

  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
    };
  }, []);

  useEffect(() => {
    setMascotGaze({ type: "pointer" });
  }, [sessionID]);

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
        <div className="relative">
          <div className="relative z-0 rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
            <div className="px-4 pt-4 pb-2">
              <Textarea
                className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-sm leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
                placeholder={t("composer.messagePlaceholder")}
                rows={1}
                name={textField.name}
                ref={setTextAreaRef}
                onBlur={handleTextBlur}
                onChange={handleTextChange}
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
                session={session}
                onResolvedChange={handleResolvedModelChange}
              />
              {running ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      aria-label={t("composer.stop")}
                      className="rounded-full !bg-foreground !text-background shadow-sm hover:!bg-foreground/90 hover:!text-background dark:hover:!bg-foreground/90"
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
                        <span aria-hidden="true" className="size-2.5 rounded-[2px] bg-current" />
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
          <span className="absolute z-30 size-12 overflow-visible" style={{ left: 6, top: -36 }}>
            <Mascot
              className="size-full overflow-visible"
              gaze={mascotGaze}
              inputPitchBias={MASCOT_INPUT_PITCH_BIAS}
              mood={mascotMoodFromPhase(turnPhase, running)}
              onPointerGaze={setMascotPointerGaze}
            />
          </span>
        </div>
      </ChatColumn>
    </form>
  );
}

function mascotMoodFromPhase(phase: TurnPhaseState | undefined, running: boolean): MascotMood {
  if (phase?.phase === "error") {
    return "error";
  }
  if (phase?.phase === "streaming_text") {
    return "ready";
  }
  if (running || phase?.phase === "submitting" || phase?.phase === "awaiting_model") {
    return "thinking";
  }
  return "idle";
}
