import { createContext } from "react";

export const TranscriptItemMeasureContext = createContext<(() => void) | null>(null);
