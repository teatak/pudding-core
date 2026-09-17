import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import { useI18n } from "@/i18n";

type SteppedSliderProps = {
  options: string[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  className?: string;
};

export function SteppedSlider({ options, value, onChange, disabled = false, className }: SteppedSliderProps) {
  const { t } = useI18n();
  const sliderRef = useRef<HTMLSpanElement>(null);
  const pointerRef = useRef<{ target: HTMLElement; id: number } | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const activeValue = preview ?? value;
  const activeIndex = options.indexOf(activeValue);
  const label = t("composer.reasoning");
  const valueText = t(`provider.reasoningEffort.${activeValue}`);

  const cancelPreview = useCallback(() => {
    const pointer = pointerRef.current;
    pointerRef.current = null;
    setPreview(null);
    if (pointer?.target.hasPointerCapture(pointer.id)) {
      pointer.target.releasePointerCapture(pointer.id);
    }
  }, []);

  // External state remains authoritative, including changes during a drag.
  useEffect(cancelPreview, [value, disabled, cancelPreview]);
  useEffect(() => {
    window.addEventListener("blur", cancelPreview);
    return () => window.removeEventListener("blur", cancelPreview);
  }, [cancelPreview]);

  // The official wrapper exposes Root props only; its Thumb owns the slider role.
  useLayoutEffect(() => {
    const thumb = sliderRef.current?.querySelector('[data-slot="slider-thumb"]');
    thumb?.setAttribute("aria-label", label);
    thumb?.setAttribute("aria-valuetext", valueText);
  }, [label, valueText]);

  const commit = (next: string) => {
    setPreview(null);
    if (!disabled && next !== value) onChange(next);
  };

  return (
    <div
      className={cn("w-full select-none pt-1 pb-0.5", className)}
      onPointerUp={cancelPreview}
      onPointerCancel={cancelPreview}
      onLostPointerCapture={cancelPreview}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) cancelPreview();
      }}
    >
      <div className="rounded-full bg-muted/80 p-1">
        <Slider
          ref={sliderRef}
          className="h-6 cursor-grab active:cursor-grabbing [&_[data-slot=slider-track]]:h-6 [&_[data-slot=slider-track]]:bg-transparent [&_[data-slot=slider-range]]:rounded-full [&_[data-slot=slider-range]]:bg-[var(--brand-accent)] [&_[data-slot=slider-thumb]]:size-6 [&_[data-slot=slider-thumb]]:border-0 [&_[data-slot=slider-thumb]]:shadow-md"
          min={0}
          max={options.length - 1}
          step={1}
          value={[activeIndex]}
          disabled={disabled}
          onPointerDownCapture={(event) => {
            if (disabled || event.button !== 0) {
              event.preventDefault();
              return;
            }
            pointerRef.current = { target: event.target as HTMLElement, id: event.pointerId };
          }}
          onValueChange={([index]) => {
            // Radix commits keyboard changes before notifying onValueChange.
            // Only a pointer interaction needs a temporary, uncommitted value.
            if (pointerRef.current) setPreview(options[index]);
          }}
          onValueCommit={([index]) => commit(options[index])}
        />
      </div>
      <div
        className="mt-1.5 grid w-full items-start gap-0.5"
        style={{ gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` }}
      >
        {options.map((option) => (
          <button
            key={option}
            type="button"
            disabled={disabled}
            aria-pressed={option === activeValue}
            className={cn(
              "min-w-0 cursor-default rounded-sm px-0.5 text-center text-[11px] leading-tight whitespace-normal [overflow-wrap:anywhere] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50",
              option === activeValue
                ? "font-medium text-foreground"
                : "text-muted-foreground/80 hover:text-foreground",
            )}
            onClick={() => commit(option)}
          >
            {t(`provider.reasoningEffort.${option}`)}
          </button>
        ))}
      </div>
    </div>
  );
}
