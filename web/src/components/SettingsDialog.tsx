import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Settings } from "lucide-react";
import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { getSettings, putSettings } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const settingsFormSchema = z.object({
  text: z.string(),
});

type SettingsDialogProps = {
  token: string;
};

export function SettingsDialog({ token }: SettingsDialogProps) {
  const queryClient = useQueryClient();
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
  });

  const form = useForm<z.infer<typeof settingsFormSchema>>({
    resolver: zodResolver(settingsFormSchema),
    defaultValues: { text: "" },
  });

  useEffect(() => {
    if (settingsQuery.data) {
      form.reset({ text: stringifySettings(settingsQuery.data.settings) });
    }
  }, [form, settingsQuery.data]);

  const saveMutation = useMutation({
    mutationFn: (value: z.infer<typeof settingsFormSchema>) => putSettings(token, parseSettings(value.text)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.settings() }),
  });

  return (
    <Dialog>
      <Tooltip>
        <TooltipTrigger asChild>
          <DialogTrigger asChild>
            <Button aria-label="Settings" size="icon" variant="ghost">
              <Settings />
            </Button>
          </DialogTrigger>
        </TooltipTrigger>
        <TooltipContent>Settings</TooltipContent>
      </Tooltip>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Settings</DialogTitle>
          <DialogDescription>Key-value settings</DialogDescription>
        </DialogHeader>
        <form className="grid gap-4" onSubmit={form.handleSubmit((value) => saveMutation.mutate(value))}>
          <div className="grid gap-2">
            <Label htmlFor="settings-text">Entries</Label>
            <Textarea
              id="settings-text"
              className="min-h-56 font-mono text-sm"
              placeholder={"provider.baseURL=https://...\nprovider.apiKey=..."}
              {...form.register("text")}
            />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={saveMutation.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function stringifySettings(settings: Record<string, string>) {
  return Object.entries(settings)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function parseSettings(text: string) {
  const out: Record<string, string> = {};
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const eq = line.indexOf("=");
    if (eq <= 0) {
      continue;
    }
    out[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return out;
}
