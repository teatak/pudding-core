import { BookmarkCheck, Box, Package } from "@/components/icons";
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

const appFallbackSizeClassByToken: Record<IdentityIconSize, string> = {
  xs: "size-3.5",
  sm: "size-4",
  md: "size-5",
  lg: "size-6",
  xl: "size-8",
  "2xl": "size-9",
  hero: "size-10",
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
  fallbackClassName,
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
  fallbackClassName?: string;
  fit?: "contain" | "cover";
  radius?: IdentityIconRadius;
  shape?: IdentityIconShape;
  size?: IdentityIconSize;
  src?: string;
}) {
  const [failed, setFailed] = useState(false);
  const fallbackVisible = !children && (!src || failed);

  useEffect(() => {
    setFailed(false);
  }, [src]);

  return (
    <span
      {...spanProps}
      className={cn(
        "inline-grid aspect-square shrink-0 place-items-center overflow-hidden bg-muted text-muted-foreground",
        fallbackVisible &&
          fallback === "skill" &&
          "bg-primary/10 text-primary shadow-none ring-1 ring-inset ring-primary/15 dark:bg-primary/15 dark:text-primary dark:ring-primary/20",
        sizeClassByToken[size],
        shape === "circle" ? "rounded-full" : identityIconRadiusClass(size, radius),
        className,
        fallbackVisible && fallbackClassName,
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
  switch (fallback) {
    case "app":
      return <Package className={appFallbackSizeClassByToken[size]} />;
    case "skill":
      return <BookmarkCheck className={fallbackSizeClassByToken[size]} data-icon-weight="strong" />;
    case "brand":
    default:
      return <Box className={fallbackSizeClassByToken[size]} />;
  }
}
