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
import { useI18n } from "@/i18n";
import { applyProviderPreset, getOrderedProviderPresets } from "@/provider/presets";

const settingsFormSchema = z.object({
  text: z.string(),
});

type SettingsDialogProps = {
  token: string;
};

export function SettingsDialog({ token }: SettingsDialogProps) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const providerPresets = getOrderedProviderPresets(locale);
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

  function applyPreset(preset: (typeof providerPresets)[number]) {
    const currentSettings = parseSettings(form.getValues("text"));
    form.setValue("text", stringifySettings(applyProviderPreset(currentSettings, preset)), {
      shouldDirty: true,
      shouldTouch: true,
    });
  }

  return (
    <Dialog>
      <Tooltip>
        <TooltipTrigger asChild>
          <DialogTrigger asChild>
            <Button aria-label={t("settings.title")} size="icon" variant="ghost">
              <Settings />
            </Button>
          </DialogTrigger>
        </TooltipTrigger>
        <TooltipContent>{t("settings.title")}</TooltipContent>
      </Tooltip>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("settings.title")}</DialogTitle>
          <DialogDescription>{t("settings.description")}</DialogDescription>
        </DialogHeader>
        <form className="grid gap-4" onSubmit={form.handleSubmit((value) => saveMutation.mutate(value))}>
          <div className="grid gap-2">
            <Label>{t("settings.providerPresets")}</Label>
            <div className="grid grid-cols-2 gap-2">
              {providerPresets.map((preset) => (
                <Button
                  key={preset.id}
                  className="h-auto min-h-12 flex-col items-start gap-1 px-3 py-2 text-left"
                  type="button"
                  variant="outline"
                  onClick={() => applyPreset(preset)}
                >
                  <span>{preset.name}</span>
                  <span className="max-w-full truncate text-xs font-normal text-muted-foreground">
                    {preset.defaultModel}
                  </span>
                </Button>
              ))}
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="settings-text">{t("settings.entries")}</Label>
            <Textarea
              id="settings-text"
              className="min-h-56 font-mono text-sm"
              placeholder={t("settings.placeholder")}
              {...form.register("text")}
            />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={saveMutation.isPending}>
              {t("common.save")}
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
