export type CanvasWindowState = {
  x: number;
  y: number;
  w: number;
  h: number;
  z: number;
  maximized?: boolean;
  restore?: CanvasWindowRestoreState;
};

export type CanvasWindowPosition = Pick<CanvasWindowState, "x" | "y">;
export type CanvasWindowGeometry = Pick<CanvasWindowState, "x" | "y" | "w" | "h">;
export type CanvasWindowRestoreState = Pick<CanvasWindowState, "x" | "y" | "w" | "h" | "z">;

export const MIN_CANVAS_WINDOW_WIDTH = 280;
export const MIN_CANVAS_WINDOW_HEIGHT = 180;
