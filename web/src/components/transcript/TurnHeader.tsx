import { memo } from "react";

import { useI18n } from "@/i18n";

import { formatDurationBetween, useElapsedDuration } from "./time";
import type { TurnHeaderVM } from "./types";

export const TurnHeader = memo(function TurnHeader({ header }: { header: TurnHeaderVM }) {
  const { locale, t } = useI18n();
  const elapsed = useElapsedDuration(header.startedAt, locale, header.status === "running");
  const duration = header.startedAt && header.endedAt
    ? formatDurationBetween(header.startedAt, header.endedAt, locale)
    : elapsed;
  const label = header.status === "running" ? "transcript.turnElapsed"
    : header.status === "cancelled" ? "transcript.turnCancelledDuration"
    : header.status === "failed" ? "transcript.turnFailedDuration"
    : "transcript.turnDuration";
  return (
    // Keep the same row through submit, streaming, steering and reconciliation.
    // Only this small component ticks; transcript content does not rerender.
    <div className="flex h-6 min-w-0 items-start border-b border-border/70 text-sm font-medium leading-5 text-muted-foreground/70" data-turn-header>
      <span className="shrink-0 whitespace-nowrap tabular-nums" data-turn-duration>
        {t(label).replace("{duration}", duration || "…")}
      </span>
    </div>
  );
});
