// Independent from the model's waitSeconds. Pure helpers shared by the panel
// and timing regressions; UI activity never revives a completed model wait.
export const INPUT_PANEL_IDLE_MS = 60_000;
export function inputPanelRemaining(lastInteraction: number, now: number) {
  return Math.max(0, Math.ceil((lastInteraction + INPUT_PANEL_IDLE_MS - now) / 1000));
}
export function inputPanelCountdown(lastInteraction: number, now: number) {
  const seconds = inputPanelRemaining(lastInteraction, now);
  return seconds > 0 && seconds <= 10 ? seconds : null;
}
export function inputWaitCountdown(deadline: string | undefined, waitSeconds: number, now: number) {
  if (!deadline || waitSeconds <= 0) return null;
  const seconds = Math.ceil((Date.parse(deadline) - now) / 1000);
  return seconds > 0 && seconds <= Math.min(10, waitSeconds / 2) ? seconds : null;
}
