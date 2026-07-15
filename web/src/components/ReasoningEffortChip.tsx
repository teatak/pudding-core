import { ChevronDown } from "lucide-react";

import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
} from "@/components/AppMenu";
import type { ResolvedModelSelection } from "@/lib/modelSelection";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";

export function ReasoningEffortChip({
  defaultValue,
  onAfterClose,
  options,
  value,
  onValueChange,
}: {
  defaultValue?: string;
  onAfterClose?: () => void;
  options: string[];
  value: string;
  onValueChange: (value: string) => void;
}) {
  const { t } = useI18n();
  const selectedValue = options.includes(value) ? value : "auto";
  const knownDefault = defaultValue && options.includes(defaultValue) ? defaultValue : undefined;
  const displayValue = selectedValue === "auto" && knownDefault ? knownDefault : selectedValue;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("composer.reasoning")}
          className="h-6 max-w-32 rounded-full border-0 bg-muted py-0 pr-1.5 pl-2 text-xs font-normal text-foreground/75 transition-none hover:bg-accent aria-expanded:bg-accent"
          size="sm"
          type="button"
          variant="ghost"
        >
          <span className="min-w-0 truncate">
            {t("composer.reasoning")}：{t(`provider.reasoningEffort.${displayValue}`)}
          </span>
          <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="min-w-24 w-28"
        side="top"
        sideOffset={8}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          onAfterClose?.();
        }}
      >
        <DropdownMenuRadioGroup value={displayValue} onValueChange={(next) => onValueChange(next === "auto" ? "" : next)}>
          {options.map((item) => (
            <DropdownMenuRadioItem key={item} className="h-7 text-xs" value={item}>
              {t(`provider.reasoningEffort.${item}`)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function reasoningEffortOptionsForSelection(selection: ResolvedModelSelection | null) {
  if (!selection) {
    return [];
  }
  const options = ["auto", "low", "medium", "high"];
  const openAIOptions = [...options, "xhigh"];
  if (supportsDeepSeekReasoning(selection)) {
    return selection.providerProtocol === "anthropic" ? ["auto", "high", "xhigh"] : openAIOptions;
  }
  if (supportsOpenAIReasoning(selection)) {
    return openAIOptions;
  }
  if (supportsGoogleThinking(selection)) {
    return options;
  }
  return [];
}

export function defaultReasoningEffortForSelection(selection: ResolvedModelSelection | null) {
  if (!selection) {
    return undefined;
  }
  if (supportsDeepSeekReasoning(selection)) {
    if (selection.providerProtocol === "anthropic") {
      return normalizeDeepSeekReasoningValue(deepSeekAnthropicEffort(selection.modelConfig?.providerOptions?.anthropic));
    }
    return normalizeDeepSeekReasoningValue(selection.modelConfig?.providerOptions?.openai?.reasoning_effort);
  }
  if (supportsOpenAIReasoning(selection)) {
    const value = selection.modelConfig?.providerOptions?.openai?.reasoning_effort;
    return typeof value === "string" ? value : undefined;
  }
  if (supportsGoogleThinking(selection)) {
    const thinking = selection.modelConfig?.providerOptions?.google?.thinking;
    if (!thinking || typeof thinking !== "object" || Array.isArray(thinking)) {
      return undefined;
    }
    const value = (thinking as Record<string, unknown>).level;
    return typeof value === "string" ? value : undefined;
  }
  return undefined;
}

function supportsOpenAIReasoning(selection: ResolvedModelSelection) {
  return selection.providerProtocol === "openai-compatible" || selection.providerProtocol === "openai-responses";
}

function supportsDeepSeekReasoning(selection: ResolvedModelSelection) {
  if (selection.providerBrand !== "deepseek") {
    return false;
  }
  return (selection.providerProtocol === "openai-compatible" || selection.providerProtocol === "anthropic") && /^deepseek-v4-/i.test(selection.model);
}

function deepSeekAnthropicEffort(options: Record<string, unknown> | undefined) {
  const outputConfig = options?.output_config;
  if (!outputConfig || typeof outputConfig !== "object" || Array.isArray(outputConfig)) {
    return undefined;
  }
  return (outputConfig as Record<string, unknown>).effort;
}

function normalizeDeepSeekReasoningValue(value: unknown) {
  if (value === "max") {
    return "xhigh";
  }
  return typeof value === "string" ? value : undefined;
}

function supportsGoogleThinking(selection: ResolvedModelSelection) {
  return selection.providerProtocol === "google" || selection.modelConfig?.providerOptions?.google?.thinking !== undefined;
}
