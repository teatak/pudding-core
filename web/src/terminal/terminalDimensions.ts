export type TerminalDimensions = {
  columns: number;
  rows: number;
};

export const DEFAULT_TERMINAL_DIMENSIONS: TerminalDimensions = {
  columns: 80,
  rows: 24,
};

export function normalizeTerminalDimensions(value?: Partial<TerminalDimensions>): TerminalDimensions {
  return {
    columns: normalizeDimension(value?.columns, DEFAULT_TERMINAL_DIMENSIONS.columns),
    rows: normalizeDimension(value?.rows, DEFAULT_TERMINAL_DIMENSIONS.rows),
  };
}

function normalizeDimension(value: number | undefined, fallback: number) {
  if (!Number.isFinite(value) || !value || value <= 0) {
    return fallback;
  }
  return Math.min(1_000, Math.floor(value));
}
