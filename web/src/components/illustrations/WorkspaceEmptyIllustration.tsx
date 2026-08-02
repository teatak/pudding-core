import {
  EmptyStateIllustration,
  emptyStateIllustrationDetailStrokeOpacity,
  emptyStateIllustrationStrokeOpacity,
  emptyStateIllustrationStrokeWidth,
} from "@/components/illustrations/EmptyStateIllustration";

export function WorkspaceEmptyIllustration() {
  return (
    <EmptyStateIllustration>
      <rect
        className="fill-muted/40 stroke-foreground"
        height="68"
        rx="13"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
        width="100"
        x="18"
        y="22"
      />
      <path
        className="stroke-foreground"
        d="M18 42h100M43 42v48"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
      <rect
        className="fill-foreground"
        fillOpacity="0.12"
        height="8"
        rx="4"
        width="15"
        x="23"
        y="50"
      />
      <rect
        className="fill-background stroke-foreground"
        height="46"
        rx="12"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
        width="64"
        x="62"
        y="51"
      />
      <path
        className="stroke-foreground"
        d="M73 66h40M73 79h25"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationDetailStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
    </EmptyStateIllustration>
  );
}
