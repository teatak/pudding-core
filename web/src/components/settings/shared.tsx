import type { ReactNode } from "react";

import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

export const SETTINGS_CONTENT_CLASS = "@container mx-auto grid min-w-0 w-full max-w-4xl gap-5";
export const SETTINGS_NARROW_CONTENT_CLASS = "@container mx-auto grid min-w-0 w-full max-w-3xl gap-5";
export const SETTINGS_COMPACT_SELECT_CLASS = "w-36 max-w-full";

export function SettingsControlRow({
  children,
  description,
  disabled,
  id,
  label,
}: {
  children: ReactNode;
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
}) {
  return (
    <div
      className={cn(
        "grid gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] sm:items-center",
        disabled && "opacity-60",
      )}
    >
      <label className="grid min-w-0 gap-1" htmlFor={id}>
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs leading-5 text-muted-foreground">{description}</span>
      </label>
      <div className="flex min-w-0 justify-start sm:justify-end">{children}</div>
    </div>
  );
}

export function SettingsActionRow({
  children,
  description,
  label,
}: {
  children: ReactNode;
  description: string;
  label: string;
}) {
  return (
    <div className="grid gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] sm:items-center">
      <span className="grid min-w-0 gap-1">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs leading-5 text-muted-foreground">{description}</span>
      </span>
      <div className="flex min-w-0 justify-start sm:justify-end">{children}</div>
    </div>
  );
}

export function SettingsNumberField({
  description,
  disabled,
  id,
  label,
  max,
  min,
  onBlur,
  onChange,
  step,
  suffix,
  value,
}: {
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
  max: number;
  min: number;
  onBlur?: () => void;
  onChange: (value: string) => void;
  step?: number | string;
  suffix?: string;
  value: string;
}) {
  return (
    <SettingsControlRow description={description} disabled={disabled} id={id} label={label}>
      <div className="flex items-center gap-2 sm:justify-end">
        <Input
          className="w-28 flex-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          disabled={disabled}
          id={id}
          inputMode="numeric"
          max={max}
          min={min}
          step={step}
          type="number"
          value={value}
          onBlur={onBlur}
          onChange={(event) => onChange(event.target.value)}
          onWheel={(event) => {
            if (document.activeElement === event.currentTarget) {
              event.currentTarget.blur();
            }
          }}
        />
        {suffix ? <span className="shrink-0 text-sm text-muted-foreground">{suffix}</span> : null}
      </div>
    </SettingsControlRow>
  );
}

export function SettingsToggleRow({
  checked,
  description,
  disabled,
  id,
  label,
  onChange,
}: {
  checked: boolean;
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  const labelID = `${id}-label`;
  const descriptionID = `${id}-description`;
  return (
    <div className={cn("flex items-center justify-between gap-4 px-3 py-3", disabled && "opacity-60")}>
      <span className="grid min-w-0 gap-1">
        <span id={labelID} className="text-sm font-medium">
          {label}
        </span>
        <span id={descriptionID} className="text-xs leading-5 text-muted-foreground">
          {description}
        </span>
      </span>
      <Switch
        aria-describedby={descriptionID}
        aria-labelledby={labelID}
        checked={checked}
        disabled={disabled}
        id={id}
        onCheckedChange={onChange}
      />
    </div>
  );
}


export function SettingsPanel({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: ReactNode;
}) {
  return (
    <section className="overflow-hidden rounded-xl border bg-card">
      <div className="pudding-settings-panel-header flex h-11 items-center justify-between gap-3 border-b px-4">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children ? <div className="grid gap-3 p-4">{children}</div> : null}
    </section>
  );
}

export function SettingsSection({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: string;
}) {
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex min-h-8 items-center justify-between gap-3">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}
