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
        d="M18 42h100M47 42v48"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
      <path
        className="stroke-foreground"
        d="M28 32h1m8 0h1m8 0h1"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationDetailStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth + 1}
      />
      <path
        className="stroke-foreground"
        d="M27 54h11M27 65h7"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationDetailStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
      <rect
        className="fill-background stroke-foreground"
        height="44"
        rx="10"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
        width="68"
        x="58"
        y="52"
      />
      <path
        className="stroke-foreground"
        d="M69 65h45M69 77h33M69 87h21"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationDetailStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
    </EmptyStateIllustration>
  );
}
