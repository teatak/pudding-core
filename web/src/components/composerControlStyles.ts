export const composerControlStateClassName =
  "hover:bg-control-hover active:bg-control-active aria-expanded:bg-control-active data-[state=open]:bg-control-active dark:hover:bg-control-hover dark:active:bg-control-active dark:aria-expanded:bg-control-active dark:data-[state=open]:bg-control-active";

export const composerShellClassName =
  "pudding-composer-shell relative rounded-[var(--pudding-composer-radius)] border border-border bg-card transition-shadow";

export const composerSendButtonClassName =
  "rounded-full !bg-foreground !text-background hover:!bg-foreground/85 disabled:!bg-control-disabled disabled:!text-background disabled:opacity-100 disabled:shadow-none";

export const composerMenuShadowClassName = "shadow-none";

export const composerSuggestionPanelClassName =
  `pudding-composer-suggestion mb-1 rounded-lg border bg-card text-card-foreground ${composerMenuShadowClassName}`;

export const composerAttachmentRemoveStateClassName =
  "transition-colors hover:bg-destructive hover:text-destructive-foreground active:bg-destructive/85";
