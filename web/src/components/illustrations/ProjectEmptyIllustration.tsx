import {
  EmptyStateIllustration,
  emptyStateIllustrationDetailStrokeOpacity,
  emptyStateIllustrationStrokeOpacity,
  emptyStateIllustrationStrokeWidth,
} from "@/components/illustrations/EmptyStateIllustration";

export function ProjectEmptyIllustration() {
  return (
    <EmptyStateIllustration>
      <path
        d="M18 36.5c0-7.2 5.8-13 13-13h26.5l7 8H100c7.2 0 13 5.8 13 13v42c0 7.2-5.8 13-13 13H31c-7.2 0-13-5.8-13-13v-50Z"
        className="fill-muted/50 stroke-foreground"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
      <path
        d="M24 47h83"
        className="stroke-foreground"
        strokeLinecap="round"
        strokeOpacity={emptyStateIllustrationStrokeOpacity}
        strokeWidth={emptyStateIllustrationStrokeWidth}
      />
      <g transform="translate(-5 12)">
        <path
          d="M83 44h33c9.4 0 17 6.7 17 15v17c0 8.3-7.6 15-17 15H98l-19 13 4-13c-9.4 0-17-6.7-17-15V59c0-8.3 7.6-15 17-15Z"
          className="fill-background stroke-foreground"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeOpacity={emptyStateIllustrationStrokeOpacity}
          strokeWidth={emptyStateIllustrationStrokeWidth}
        />
        <path
          d="M87 61h32M87 72h24"
          className="stroke-foreground"
          strokeLinecap="round"
          strokeOpacity={emptyStateIllustrationDetailStrokeOpacity}
          strokeWidth={emptyStateIllustrationStrokeWidth}
        />
      </g>
    </EmptyStateIllustration>
  );
}
