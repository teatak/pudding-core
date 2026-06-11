import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Send, Square } from "lucide-react";
import { useRef } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { APIError, cancelTurn, submitMessage } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useOverlayStore } from "@/state/overlayStore";

const composerSchema = z.object({
  text: z.string().trim().min(1),
});

type ComposerProps = {
  token: string;
  sessionID: string;
};

export function Composer({ token, sessionID }: ComposerProps) {
  const queryClient = useQueryClient();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const lastSubmitAtRef = useRef(0);
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
        form.setError("text", { message: "The session is already streaming." });
        return;
      }
      form.setError("text", { message: error instanceof Error ? error.message : "Submit failed" });
    },
  });

  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });

  const submitDraft = (value: z.infer<typeof composerSchema>) => {
    lastSubmitAtRef.current = Date.now();
    submitMutation.mutate(value);
  };

  const cancelTurnIfIntentional = () => {
    if (Date.now() - lastSubmitAtRef.current < 500) {
      return;
    }
    cancelMutation.mutate();
  };

  return (
    <form
      className="border-t bg-card p-3"
      onSubmit={form.handleSubmit(submitDraft)}
    >
      <div className="flex items-end gap-2">
        <Textarea
          className="max-h-40 min-h-20 resize-none"
          placeholder="Message"
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void form.handleSubmit(submitDraft)();
            }
          }}
          {...form.register("text")}
        />
        {runningTurnID ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label="Stop"
                disabled={cancelMutation.isPending}
                size="icon"
                type="button"
                variant="outline"
                onClick={cancelTurnIfIntentional}
              >
                {cancelMutation.isPending ? <Loader2 className="animate-spin" /> : <Square />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>Stop</TooltipContent>
          </Tooltip>
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button aria-label="Send" disabled={submitMutation.isPending} size="icon" type="submit">
                {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <Send />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>Send</TooltipContent>
          </Tooltip>
        )}
      </div>
      {form.formState.errors.text ? (
        <div className="mt-2 text-xs text-destructive">{form.formState.errors.text.message}</div>
      ) : null}
    </form>
  );
}
