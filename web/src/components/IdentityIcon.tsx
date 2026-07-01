import { BookOpenText, Box, Cable } from "lucide-react";
import { useEffect, useState, type HTMLAttributes, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export type IdentityIconSize = "xs" | "sm" | "md" | "lg" | "xl" | "2xl" | "hero";
export type IdentityIconRadius = "auto" | "none" | "sm" | "md" | "lg" | "xl" | "2xl";
export type IdentityIconShape = "rounded" | "circle";
export type IdentityIconFallback = "brand" | "app" | "skill";

const sizeClassByToken: Record<IdentityIconSize, string> = {
  xs: "size-5",
  sm: "size-6",
  md: "size-8",
  lg: "size-9",
  xl: "size-12",
  "2xl": "size-14",
  hero: "size-16",
};

const radiusClassBySize: Record<IdentityIconSize, string> = {
  xs: "rounded",
  sm: "rounded-md",
  md: "rounded-md",
  lg: "rounded-lg",
  xl: "rounded-xl",
  "2xl": "rounded-xl",
  hero: "rounded-2xl",
};

const radiusClassByToken: Record<Exclude<IdentityIconRadius, "auto">, string> = {
  none: "rounded-none",
  sm: "rounded",
  md: "rounded-md",
  lg: "rounded-lg",
  xl: "rounded-xl",
  "2xl": "rounded-2xl",
};

const fallbackSizeClassByToken: Record<IdentityIconSize, string> = {
  xs: "size-3",
  sm: "size-3.5",
  md: "size-4",
  lg: "size-5",
  xl: "size-6",
  "2xl": "size-6",
  hero: "size-7",
};

export function identityIconSizeClass(size: IdentityIconSize) {
  return sizeClassByToken[size];
}

export function identityIconRadiusClass(size: IdentityIconSize, radius: IdentityIconRadius = "auto") {
  return radius === "auto" ? radiusClassBySize[size] : radiusClassByToken[radius];
}

export function IdentityIcon({
  alt = "",
  children,
  className,
  contentClassName,
  fallback = "brand",
  fit = "contain",
  radius = "auto",
  shape = "rounded",
  size = "md",
  src,
  style,
  ...spanProps
}: Omit<HTMLAttributes<HTMLSpanElement>, "children"> & {
  alt?: string;
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  fallback?: IdentityIconFallback;
  fit?: "contain" | "cover";
  radius?: IdentityIconRadius;
  shape?: IdentityIconShape;
  size?: IdentityIconSize;
  src?: string;
}) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [src]);

  return (
    <span
      {...spanProps}
      className={cn(
        "inline-grid aspect-square shrink-0 place-items-center overflow-hidden bg-muted text-muted-foreground",
        sizeClassByToken[size],
        shape === "circle" ? "rounded-full" : identityIconRadiusClass(size, radius),
        className,
      )}
      data-slot="identity-icon"
      style={style}
    >
      {children ? (
        children
      ) : src && !failed ? (
        <img
          alt={alt}
          className={cn("block size-full", fit === "cover" ? "object-cover" : "object-contain", contentClassName)}
          src={src}
          onError={() => setFailed(true)}
        />
      ) : (
        <IdentityIconFallbackView fallback={fallback} size={size} />
      )}
    </span>
  );
}

function IdentityIconFallbackView({ fallback, size }: { fallback: IdentityIconFallback; size: IdentityIconSize }) {
  const className = fallbackSizeClassByToken[size];
  switch (fallback) {
    case "app":
      return <Cable className={className} />;
    case "skill":
      return <BookOpenText className={className} strokeWidth={2.25} />;
    case "brand":
    default:
      return <Box className={className} strokeWidth={2.1} />;
  }
}
