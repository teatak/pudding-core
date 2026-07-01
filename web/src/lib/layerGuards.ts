let lastSelectOutsideInteractionAt = 0;

export function markSelectOutsideInteraction() {
  lastSelectOutsideInteractionAt = Date.now();
}

export function shouldKeepDialogOpenForSelectDismiss(target: EventTarget | null): boolean {
  const element = target instanceof Element ? target : null;
  return Boolean(
    element?.closest("[data-slot='select-content']") ||
      document.querySelector("[data-slot='select-content'][data-state='open']") ||
      Date.now() - lastSelectOutsideInteractionAt < 250,
  );
}
