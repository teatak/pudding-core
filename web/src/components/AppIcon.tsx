import { useEffect, useState, type CSSProperties } from "react";

import { type AppIconSpec } from "@/contracts/api";
import { IdentityIcon, type IdentityIconSize } from "@/components/IdentityIcon";
import { cn } from "@/lib/utils";

export type { AppIconSpec };

const appIconSVGInflight = new Map<string, Promise<string>>();

export function AppIcon({
  className,
  icon,
  size = "md",
  src,
}: {
  className?: string;
  icon?: AppIconSpec;
  size?: IdentityIconSize;
  src?: string;
}) {
  const hasIconColor = Boolean(icon?.color?.light || icon?.color?.dark);
  const hasIconBackground = Boolean(icon?.background?.light || icon?.background?.dark);
  const iconStyle = appIconCSSVariables(icon);
  const [failed, setFailed] = useState(false);
  const [svgText, setSvgText] = useState<{ src: string; text: string } | null>(null);

  useEffect(() => {
    setFailed(false);
    setSvgText(null);
  }, [src]);

  useEffect(() => {
    if (!src || !hasIconColor || failed) {
      return;
    }
    let cancelled = false;
    fetchThemedAppIconSVG(src)
      .then((text) => {
        if (!cancelled) {
          setSvgText({ src, text });
        }
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [failed, hasIconColor, src]);

  const themedIcon =
    src && !failed && hasIconColor ? (
      svgText?.src === src ? (
        <span
          aria-hidden="true"
          className="block size-full [&_svg]:block [&_svg]:size-full"
          dangerouslySetInnerHTML={{ __html: svgText.text }}
        />
      ) : (
        <span aria-hidden="true" className="block size-full" />
      )
    ) : undefined;

  return (
    <IdentityIcon
      className={cn("pudding-app-icon", className)}
      contentClassName={cn(icon ? "object-contain" : "object-cover")}
      data-has-background={hasIconBackground ? "true" : undefined}
      data-has-color={hasIconColor ? "true" : undefined}
      data-light-background={needsAppIconLightBorder(icon) ? "true" : undefined}
      fallback="app"
      fit={icon ? "contain" : "cover"}
      size={size}
      src={src && !hasIconColor ? src : undefined}
      style={iconStyle}
    >
      {themedIcon}
    </IdentityIcon>
  );
}

function fetchThemedAppIconSVG(src: string) {
  const existing = appIconSVGInflight.get(src);
  if (existing) {
    return existing;
  }
  const request = fetch(src).then((response) => (response.ok ? response.text() : Promise.reject(new Error("icon fetch failed"))));
  appIconSVGInflight.set(src, request);
  request.then(
    () => {
      if (appIconSVGInflight.get(src) === request) {
        appIconSVGInflight.delete(src);
      }
    },
    () => {
      if (appIconSVGInflight.get(src) === request) {
        appIconSVGInflight.delete(src);
      }
    },
  );
  return request;
}

export function mergeAppIconSpec(primary: AppIconSpec | undefined, fallback: AppIconSpec | undefined): AppIconSpec | undefined {
  if (!primary) {
    return fallback;
  }
  if (!fallback) {
    return primary;
  }
  return {
    ...fallback,
    ...primary,
    background: hasThemeValue(primary.background) ? primary.background : fallback.background,
    color: hasThemeValue(primary.color) ? primary.color : fallback.color,
  };
}

function hasThemeValue(value: AppIconSpec["color"] | AppIconSpec["background"]): boolean {
  return Boolean(value?.light || value?.dark);
}

function appIconCSSVariables(icon: AppIconSpec | undefined): CSSProperties | undefined {
  const style: CSSProperties & Record<string, string> = {};
  const colorLight = icon?.color?.light || icon?.color?.dark || "";
  const colorDark = icon?.color?.dark || icon?.color?.light || "";
  const backgroundLight = icon?.background?.light || icon?.background?.dark || "";
  const backgroundDark = icon?.background?.dark || icon?.background?.light || "";
  if (colorLight) {
    style["--app-icon-color"] = colorLight;
    style["--app-icon-color-dark"] = colorDark || colorLight;
  }
  if (backgroundLight) {
    style["--app-icon-background"] = backgroundLight;
    style["--app-icon-background-dark"] = backgroundDark || backgroundLight;
  }
  return Object.keys(style).length > 0 ? style : undefined;
}

function needsAppIconLightBorder(icon: AppIconSpec | undefined): boolean {
  return isLightCSSColor(icon?.background?.light || icon?.background?.dark);
}

function isLightCSSColor(value: string | undefined): boolean {
  const normalized = value?.trim().toLowerCase().replace(/\s+/g, "");
  if (!normalized) {
    return false;
  }
  if (normalized === "white") {
    return true;
  }
  const rgb = parseCSSRGB(normalized);
  if (!rgb) {
    return false;
  }
  const [red, green, blue, alpha] = rgb;
  const composited = [red, green, blue].map((channel) => channel * alpha + 255 * (1 - alpha));
  const luminance = composited.reduce((sum, channel, index) => {
    const normalizedChannel = channel / 255;
    const linear = normalizedChannel <= 0.04045
      ? normalizedChannel / 12.92
      : ((normalizedChannel + 0.055) / 1.055) ** 2.4;
    return sum + linear * [0.2126, 0.7152, 0.0722][index];
  }, 0);
  return luminance >= 0.82;
}

function parseCSSRGB(value: string): [number, number, number, number] | null {
  const hex = value.match(/^#([\da-f]{3,4}|[\da-f]{6}|[\da-f]{8})$/i)?.[1];
  if (hex) {
    const expanded = hex.length <= 4 ? [...hex].map((character) => character + character).join("") : hex;
    return [
      Number.parseInt(expanded.slice(0, 2), 16),
      Number.parseInt(expanded.slice(2, 4), 16),
      Number.parseInt(expanded.slice(4, 6), 16),
      expanded.length === 8 ? Number.parseInt(expanded.slice(6, 8), 16) / 255 : 1,
    ];
  }
  const rgb = value.match(/^rgba?\((\d+(?:\.\d+)?),(\d+(?:\.\d+)?),(\d+(?:\.\d+)?)(?:,(\d*(?:\.\d+)?))?\)$/);
  if (!rgb) {
    return null;
  }
  return [
    Math.min(255, Number(rgb[1])),
    Math.min(255, Number(rgb[2])),
    Math.min(255, Number(rgb[3])),
    rgb[4] === undefined || rgb[4] === "" ? 1 : Math.min(1, Number(rgb[4])),
  ];
}
