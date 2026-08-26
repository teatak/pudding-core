export const composerShellClassName =
  "pudding-composer-shell relative rounded-[var(--pudding-composer-radius)] border border-border bg-card transition-shadow";

export const composerSendButtonClassName =
  "rounded-full !bg-foreground !text-background hover:!bg-foreground/85 disabled:!bg-control-disabled disabled:!text-background disabled:opacity-100 disabled:shadow-none";

export const composerMenuShadowClassName = "shadow-none";

export const composerSuggestionPanelClassName =
  `pudding-composer-suggestion left-4 mb-1 rounded-lg border bg-card text-card-foreground ${composerMenuShadowClassName}`;

export const composerAttachmentRemoveStateClassName =
  "transition-colors hover:bg-destructive hover:text-destructive-foreground active:bg-destructive/85";

export const composerImageAttachmentRemoveClassName =
  "absolute top-0.5 right-0.5 z-10 grid size-4 place-items-center rounded-full bg-foreground text-background shadow-md ring-1 ring-background/70 after:absolute after:-inset-1 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none";
