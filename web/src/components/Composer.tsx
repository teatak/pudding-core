import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Send, Square } from "lucide-react";
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
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });

  const submitMutation = useMutation({
    mutationFn: async (value: z.infer<typeof composerSchema>) => {
      const clientMessageID = crypto.randomUUID();
      addPendingUser({
        sessionID,
        clientMessageID,
        text: value.text,
        createdAt: new Date().toISOString(),
      });
      return submitMessage(token, sessionID, { clientMessageID, text: value.text });
    },
    onSuccess: async () => {
      form.reset({ text: "" });
      await queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
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

  return (
    <form
      className="border-t bg-card p-3"
      onSubmit={form.handleSubmit((value) => submitMutation.mutate(value))}
    >
      <div className="flex items-end gap-2">
        <Textarea
          className="max-h-40 min-h-20 resize-none"
          placeholder="Message"
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void form.handleSubmit((value) => submitMutation.mutate(value))();
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
                variant="outline"
                onClick={() => cancelMutation.mutate()}
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
