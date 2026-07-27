import type { LucideIcon, LucideProps } from "lucide-react";

export type PuddingIconWeight = "subtle" | "regular" | "strong";

export type PuddingIconProps = Omit<LucideProps, "strokeWidth"> & {
  icon: LucideIcon;
  weight?: PuddingIconWeight;
};

export function PuddingIcon({ icon: Icon, weight = "regular", ...props }: PuddingIconProps) {
  return <Icon data-icon-weight={weight} {...props} />;
}
