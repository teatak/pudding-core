import { SquareMousePointer } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import type { UIContextPart } from "@/state/uiContextStore";

export function UIContextControl({
  context,
  enabled,
  onEnabledChange,
}: {
  context: UIContextPart;
  enabled: boolean;
  onEnabledChange: (enabled: boolean) => void;
}) {
  const { t } = useI18n();
  const contextLabel = uiContextLabel(context, t);
  const stateLabel = enabled
    ? t("uiContext.included").replace("{context}", contextLabel)
    : t("uiContext.disabled");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={enabled ? t("uiContext.disable") : t("uiContext.enable")}
          aria-pressed={enabled}
          className={cn(
            "relative rounded-full",
            enabled ? "text-muted-foreground" : "text-muted-foreground/45",
          )}
          size="icon"
          type="button"
          variant="ghost"
          onClick={() => onEnabledChange(!enabled)}
        >
          <SquareMousePointer
            className={cn(
              "size-4",
              enabled && "[&_path:first-child]:fill-primary [&_path:first-child]:stroke-primary",
            )}
          />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{stateLabel}</TooltipContent>
    </Tooltip>
  );
}

export function uiContextLabel(context: UIContextPart, t: (key: string) => string) {
  const detail = context.name || fileName(context.path) || context.url;
  return [surfaceLabel(context.surface, t), detail].filter(Boolean).join(" · ");
}

function surfaceLabel(surface: UIContextPart["surface"], t: (key: string) => string) {
  switch (surface) {
    case "project":
      return t("uiContext.project");
    case "canvas":
      return t("uiContext.canvas");
    case "browser":
      return t("uiContext.browser");
    case "terminal":
      return t("uiContext.terminal");
    case "file_preview":
      return t("uiContext.filePreview");
  }
}

function fileName(path?: string) {
  return path?.split(/[\\/]/).filter(Boolean).at(-1);
}
