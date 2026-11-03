import { z } from "zod";

// Literal colors only: no URLs, CSS variables or declarations from packages.
const colorLiteral = /^(?:#[\da-fA-F]{3,4}|#[\da-fA-F]{6}|#[\da-fA-F]{8}|[a-zA-Z]+|(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch|color)\([a-zA-Z\d\s.,%+\-/]+\))$/i;
export const iconColor = z.string().trim().max(128).refine(value => !value || colorLiteral.test(value), "Invalid icon color");
export const iconThemeColor = z.object({ light: iconColor.optional(), dark: iconColor.optional() });
export const iconSpec = z.object({
  svg: z.string().trim().min(1).optional(),
  color: iconThemeColor.optional(),
  background: iconThemeColor.optional(),
});
export type IconSpec = z.infer<typeof iconSpec>;
export type IconThemeColor = z.infer<typeof iconThemeColor>;
