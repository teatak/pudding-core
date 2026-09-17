import type { ComponentProps } from "react";

import type { Session } from "@/api/client";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { useSessionModelSettings } from "@/hooks/useSessionModelSettings";
import { useI18n } from "@/i18n";

type SessionModelReasoningPickerProps = Pick<
  ComponentProps<typeof ModelReasoningPicker>,
  "token" | "onAfterClose" | "onResolvedChange" | "iconOnly" | "className"
> & { session: Session };

export function SessionModelReasoningPicker({ session, token, ...props }: SessionModelReasoningPickerProps) {
  const { t } = useI18n();
  const settings = useSessionModelSettings(token, session);

  return (
    <ModelReasoningPicker
      {...props}
      token={token}
      value={settings.value}
      reasoningValue={settings.reasoningValue}
      disabled={settings.isPending}
      error={settings.error ? t("provider.modelSaveFailed") : undefined}
      onChange={settings.selectModel}
      onReasoningChange={settings.selectReasoning}
    />
  );
}
