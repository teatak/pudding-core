import { Check, ChevronRight, TextCursorInput, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { cancelInputFlow, completeInputFlow, confirmUIRequest, type InputFlowRequest } from "@/state/inputFlowStore";

type InputFlowSchema = {
  afterItem?: InputFlowAfterItem;
  description?: string;
  maxItems?: number;
  minItems?: number;
  nextSteps: InputFlowStep[];
  repeatSteps: InputFlowStep[];
  resultKey?: string;
  title: string;
  type: "repeat";
};
type InputFlowStep = {
  customLabel?: string;
  description?: string;
  id: string;
  max?: number;
  maxFrom?: string;
  min?: number;
  options?: unknown[];
  placeholder?: string;
  required?: boolean;
  title: string;
  type: "option_list" | "quick_number" | "text_input" | "phone_input" | "confirm";
};
type InputFlowAfterItem = {
  actions?: Array<{ id: "continue" | "done"; label: string }>;
  title?: string;
};
type InputFlowOption = {
  data?: Record<string, unknown>;
  description?: string;
  label?: string;
  title?: string;
  value?: unknown;
};
type ChoiceMenuItem<T> = {
  description?: string;
  disabled?: boolean;
  id: string;
  label: string;
  render?: (active: boolean) => ReactNode;
  value: T;
};
type UIConfirmSchema = {
  cancelLabel?: string;
  confirmLabel?: string;
  description?: string;
  destructive?: boolean;
  rows: UIConfirmRow[];
  title: string;
};
type UIConfirmRow = {
  description?: string;
  label: string;
  value: unknown;
};

function noop() {}

export function InputFlowPanel({ request }: { request: InputFlowRequest }) {
  if (request.kind === "confirm") {
    return <UIConfirmPanel request={request} />;
  }
  return <InputFlowContent request={request} />;
}

function InputFlowContent({ request }: { request: InputFlowRequest }) {
  const { t } = useI18n();
  const flow = useMemo(() => normalizeInputFlow(request.args), [request.args]);
  const [items, setItems] = useState<Array<Record<string, unknown>>>([]);
  const [stepIndex, setStepIndex] = useState(0);
  const [current, setCurrent] = useState<Record<string, unknown>>({});
  const [rawSelections, setRawSelections] = useState<Record<string, unknown>>({});
  const [customOpen, setCustomOpen] = useState(false);
  const [customValue, setCustomValue] = useState("");
  const [collectingNext, setCollectingNext] = useState(false);
  const [nextStepIndex, setNextStepIndex] = useState(0);
  const [nextValues, setNextValues] = useState<Record<string, unknown>>({});
  const [textValue, setTextValue] = useState("");

  if (!flow) {
    return null;
  }

  const schema = flow.schema;
  const steps = schema.repeatSteps;
  const activeStep = steps[stepIndex];
  const nextStep = schema.nextSteps[nextStepIndex];
  const currentReady = !collectingNext && stepIndex >= steps.length;
  const minItems = Math.max(1, Math.floor(schema.minItems || 1));
  const maxItems = schema.maxItems && schema.maxItems > 0 ? Math.floor(schema.maxItems) : undefined;
  const canAddMore = !maxItems || items.length + 1 < maxItems;
  const doneDisabled = items.length + (currentReady ? 1 : 0) < minItems;

  function advance() {
    setCustomOpen(false);
    setCustomValue("");
    setStepIndex((index) => Math.min(index + 1, steps.length));
  }

  function selectOption(step: InputFlowStep, option: InputFlowOption) {
    const next = { ...current };
    const label = stringValue(option.title) || stringValue(option.label);
    if (option.data && typeof option.data === "object") {
      Object.assign(next, option.data);
    } else {
      next[step.id] = option.value ?? label;
    }
    if (label) {
      next[`${step.id}Label`] = label;
    }
    setCurrent(next);
    setRawSelections((previous) => ({ ...previous, [step.id]: option }));
    advance();
  }

  function selectNumber(step: InputFlowStep, value: number) {
    setCurrent((previous) => ({ ...previous, [step.id]: value }));
    setRawSelections((previous) => ({ ...previous, [step.id]: value }));
    advance();
  }

  function commitCurrent() {
    const nextItem = compactRecord(current);
    if (Object.keys(nextItem).length === 0) {
      return items;
    }
    const nextItems = [...items, nextItem];
    setItems(nextItems);
    setCurrent({});
    setRawSelections({});
    setStepIndex(0);
    return nextItems;
  }

  function startNextSteps(finalItems: Array<Record<string, unknown>>) {
    if (schema.nextSteps.length === 0) {
      submit(finalItems, nextValues);
      return;
    }
    setItems(finalItems);
    setCollectingNext(true);
    setNextStepIndex(0);
    setTextValue("");
  }

  function commitNextValue(step: InputFlowStep, value: unknown) {
    const values = step.type === "confirm" ? nextValues : { ...nextValues, [step.id]: value };
    setNextValues(values);
    setTextValue("");
    const nextIndex = nextStepIndex + 1;
    if (nextIndex >= schema.nextSteps.length) {
      submit(items, values);
      return;
    }
    setNextStepIndex(nextIndex);
  }

  function submit(finalItems: Array<Record<string, unknown>>, values: Record<string, unknown>) {
    completeInputFlow(request, resultPayload(schema, finalItems, values));
  }

  return (
    <FloatingUIPanel
      cancelLabel={t("common.cancel")}
      description={schema.description}
      title={schema.title}
      onCancel={() => cancelInputFlow(request)}
    >
      <div className="space-y-3 text-sm text-foreground">
        {items.length > 0 ? <SelectedItems items={items} /> : null}
        {!collectingNext && activeStep ? (
          <ActiveStep
            customOpen={customOpen}
            customValue={customValue}
            max={numberLimit(activeStep, rawSelections)}
            step={activeStep}
            onCustomOpen={() => setCustomOpen(true)}
            onCustomValue={setCustomValue}
            onOptionSelect={(option) => selectOption(activeStep, option)}
            onNumberSelect={(value) => selectNumber(activeStep, value)}
          />
        ) : null}
        {collectingNext && nextStep ? (
          <ActiveStep
            customOpen={customOpen}
            customValue={customValue}
            max={numberLimit(nextStep, nextValues)}
            step={nextStep}
            textValue={textValue}
            onCustomOpen={() => setCustomOpen(true)}
            onCustomValue={setCustomValue}
            onOptionSelect={(option) => commitNextValue(nextStep, option.value ?? option.title ?? option.label)}
            onTextChange={setTextValue}
            onTextSubmit={(value) => commitNextValue(nextStep, value)}
            onNumberSelect={(value) => commitNextValue(nextStep, value)}
            onConfirm={() => commitNextValue(nextStep, true)}
            confirmItems={items}
            confirmResultKey={schema.resultKey || "items"}
            confirmValues={nextValues}
          />
        ) : null}
        {currentReady ? (
          <AfterItemMenu
            canAddMore={canAddMore}
            current={current}
            doneDisabled={doneDisabled}
            schema={schema}
            onCancel={() => cancelInputFlow(request)}
            onContinue={commitCurrent}
            onDone={() => {
              const finalItems = commitCurrent();
              startNextSteps(finalItems);
            }}
          />
        ) : null}
      </div>
    </FloatingUIPanel>
  );
}

function UIConfirmPanel({ request }: { request: InputFlowRequest }) {
  const { t } = useI18n();
  const schema = useMemo(() => normalizeUIConfirm(request.args), [request.args]);

  if (!schema) {
    return null;
  }

  return (
    <FloatingUIPanel
      cancelLabel={schema.cancelLabel || t("common.cancel")}
      description={schema.description}
      title={schema.title}
      onCancel={() => cancelInputFlow(request)}
    >
      <div className="space-y-3 text-sm text-foreground">
        {schema.rows.length > 0 ? <ConfirmRows rows={schema.rows} /> : null}
        <ChoiceMenu
          maxHeightClassName="max-h-28"
          items={[
            {
              id: "confirm",
              label: schema.confirmLabel || t("uiConfirm.confirm"),
              value: "confirm" as const,
            },
            {
              id: "cancel",
              label: schema.cancelLabel || t("common.cancel"),
              value: "cancel" as const,
            },
          ]}
          onSelect={(action) => {
            if (action === "confirm") {
              confirmUIRequest(request, { type: "ui_confirm_result", confirmed: true });
              return;
            }
            cancelInputFlow(request);
          }}
        />
      </div>
    </FloatingUIPanel>
  );
}

function FloatingUIPanel({
  cancelLabel,
  children,
  description,
  title,
  onCancel,
}: {
  cancelLabel: string;
  children: ReactNode;
  description?: string;
  title: string;
  onCancel: () => void;
}) {
  return (
    <div
      className="absolute bottom-full left-4 z-20 flex max-h-[min(20rem,calc(100vh-12rem))] w-[min(34rem,calc(100%-2rem))] flex-col overflow-hidden rounded-t-lg border border-border/70 bg-popover/95 text-popover-foreground shadow-sm backdrop-blur sm:left-16 sm:w-[min(34rem,calc(100%-5rem))]"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="flex min-w-0 shrink-0 items-start gap-2 px-3 py-2">
        <TextCursorInput className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{title}</div>
          {description ? <div className="mt-0.5 line-clamp-2 text-xs leading-5 text-muted-foreground">{description}</div> : null}
        </div>
        <Button
          aria-label={cancelLabel}
          className="-mr-1 -mt-1 size-7 shrink-0 rounded-full"
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onCancel}
        >
          <X className="size-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-2">{children}</div>
    </div>
  );
}

function SelectedItems({ items }: { items: Array<Record<string, unknown>> }) {
  return (
    <div className="space-y-1">
      {items.map((item, index) => (
        <div key={index} className="flex items-center gap-2 rounded-md bg-background/60 px-2 py-1.5 text-xs">
          <Check className="size-3.5 text-primary" />
          <span className="min-w-0 truncate">{itemSummary(item)}</span>
        </div>
      ))}
    </div>
  );
}

function ActiveStep({
  confirmItems = [],
  confirmResultKey = "items",
  confirmValues = {},
  customOpen,
  customValue,
  max,
  step,
  textValue = "",
  onConfirm,
  onCustomOpen,
  onCustomValue,
  onNumberSelect,
  onOptionSelect,
  onTextChange,
  onTextSubmit,
}: {
  confirmItems?: Array<Record<string, unknown>>;
  confirmResultKey?: string;
  confirmValues?: Record<string, unknown>;
  customOpen: boolean;
  customValue: string;
  max?: number;
  step: InputFlowStep;
  textValue?: string;
  onConfirm?: () => void;
  onCustomOpen: () => void;
  onCustomValue: (value: string) => void;
  onNumberSelect: (value: number) => void;
  onOptionSelect: (option: InputFlowOption) => void;
  onTextChange?: (value: string) => void;
  onTextSubmit?: (value: string) => void;
}) {
  return (
    <div className="space-y-2">
      <StepTitle step={step} />
      {step.type === "option_list" ? (
        <OptionList step={step} onSelect={onOptionSelect} />
      ) : step.type === "quick_number" ? (
        <QuickNumber
          customOpen={customOpen}
          customValue={customValue}
          max={max}
          step={step}
          onCustomOpen={onCustomOpen}
          onCustomValue={onCustomValue}
          onSelect={onNumberSelect}
        />
      ) : step.type === "text_input" || step.type === "phone_input" ? (
        <TextInputStep step={step} value={textValue} onChange={onTextChange || noop} onSubmit={onTextSubmit || noop} />
      ) : step.type === "confirm" ? (
        <ConfirmStep items={confirmItems} resultKey={confirmResultKey} values={confirmValues} onConfirm={onConfirm || noop} />
      ) : null}
    </div>
  );
}

function AfterItemMenu({
  canAddMore,
  current,
  doneDisabled,
  schema,
  onCancel,
  onContinue,
  onDone,
}: {
  canAddMore: boolean;
  current: Record<string, unknown>;
  doneDisabled: boolean;
  schema: InputFlowSchema;
  onCancel: () => void;
  onContinue: () => void;
  onDone: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-3">
      {schema.afterItem?.title ? <div className="text-xs font-medium text-muted-foreground">{schema.afterItem.title}</div> : null}
      <div className="px-2 py-1 text-xs text-muted-foreground">{itemSummary(current)}</div>
      <ChoiceMenu
        items={[
          ...(canAddMore
            ? [
                {
                  id: "continue",
                  label: actionLabel(schema.afterItem, "continue", t("inputFlow.continue")),
                  value: "continue" as const,
                },
              ]
            : []),
          {
            id: "done",
            label: actionLabel(schema.afterItem, "done", t("inputFlow.done")),
            value: "done" as const,
            disabled: doneDisabled,
          },
          { id: "cancel", label: t("common.cancel"), value: "cancel" as const },
        ]}
        onSelect={(action) => {
          if (action === "continue") {
            onContinue();
            return;
          }
          if (action === "done") {
            onDone();
            return;
          }
          onCancel();
        }}
      />
    </div>
  );
}

function ConfirmRows({ rows }: { rows: UIConfirmRow[] }) {
  return (
    <div className="max-h-36 overflow-y-auto rounded-md border border-border/60 text-xs">
      {rows.map((row, index) => (
        <div key={`${row.label}:${index}`} className="grid grid-cols-[7rem_minmax(0,1fr)] gap-2 border-b border-border/50 px-2 py-1.5 last:border-b-0">
          <div className="truncate font-medium text-muted-foreground">{row.label}</div>
          <div className="min-w-0">
            <div className="truncate text-foreground">{formatConfirmValue(row.value)}</div>
            {row.description ? <div className="mt-0.5 truncate text-muted-foreground">{row.description}</div> : null}
          </div>
        </div>
      ))}
    </div>
  );
}

function StepTitle({ step }: { step: InputFlowStep }) {
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
        <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{step.title}</span>
      </div>
      {step.description ? <div className="mt-1 text-xs text-muted-foreground">{step.description}</div> : null}
    </div>
  );
}

function OptionList({ step, onSelect }: { step: InputFlowStep; onSelect: (option: InputFlowOption) => void }) {
  const options = (step.options || []).map(normalizeOption).filter(Boolean) as InputFlowOption[];
  return (
    <ChoiceMenu
      items={options.map((option, index) => {
        const title = stringValue(option.title) || stringValue(option.label) || String(option.value ?? "");
        return {
          id: `${title}:${index}`,
          label: title,
          description: option.description,
          value: option,
        };
      })}
      onSelect={onSelect}
    />
  );
}

function QuickNumber({
  customOpen,
  customValue,
  max,
  step,
  onCustomOpen,
  onCustomValue,
  onSelect,
}: {
  customOpen: boolean;
  customValue: string;
  max?: number;
  step: InputFlowStep;
  onCustomOpen: () => void;
  onCustomValue: (value: string) => void;
  onSelect: (value: number) => void;
}) {
  const { t } = useI18n();
  const min = typeof step.min === "number" ? Math.max(0, Math.floor(step.min)) : 1;
  const options = (step.options || [1, 2])
    .map((value) => (typeof value === "number" ? value : Number(value)))
    .filter((value) => Number.isFinite(value) && value >= min && (!max || value <= max));
  const customNumber = Number(customValue);
  const customValid = Number.isInteger(customNumber) && customNumber >= min && (!max || customNumber <= max);
  const menuItems: Array<ChoiceMenuItem<{ type: "number"; value: number } | { type: "custom" }>> = [
    ...options.map((value) => ({ id: String(value), label: String(value), value: { type: "number" as const, value } })),
    {
      id: "custom",
      label: step.customLabel || t("inputFlow.custom"),
      value: { type: "custom" as const },
      render: () =>
        customOpen ? (
          <div className="flex min-w-0 items-center gap-2 bg-transparent">
            <input
              autoFocus
              className={cn(
                "h-7 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40",
                customValue && !customValid ? "border-destructive" : "",
              )}
              inputMode="numeric"
              min={min}
              max={max}
              placeholder={t("inputFlow.customPlaceholder")}
              type="number"
              value={customValue}
              onChange={(event) => onCustomValue(event.target.value)}
              onKeyDown={(event) => {
                event.stopPropagation();
                if (event.key === "Enter" && customValid) {
                  event.preventDefault();
                  onSelect(customNumber);
                }
              }}
            />
            <Button size="sm" type="button" disabled={!customValid} onMouseDown={(event) => event.preventDefault()} onClick={() => onSelect(customNumber)}>
              {t("inputFlow.confirm")}
            </Button>
          </div>
        ) : (
          <div className="truncate text-sm font-medium">{step.customLabel || t("inputFlow.custom")}</div>
        ),
    },
  ];
  return (
    <ChoiceMenu
      items={menuItems}
      maxHeightClassName="max-h-36"
      onSelect={(item) => {
        if (item.type === "number") {
          onSelect(item.value);
          return;
        }
        onCustomOpen();
      }}
    />
  );
}

function TextInputStep({
  step,
  value,
  onChange,
  onSubmit,
}: {
  step: InputFlowStep;
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: string) => void;
}) {
  const { t } = useI18n();
  const text = value.trim();
  const required = step.required !== false;
  const valid = !required || text.length > 0;
  function handleSubmit() {
    if (valid) {
      onSubmit(text);
    }
  }
  return (
    <div className="flex items-center gap-2">
      <input
        autoFocus
        className={cn(
          "h-8 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40",
          value && !valid ? "border-destructive" : "",
        )}
        inputMode={step.type === "phone_input" ? "tel" : "text"}
        placeholder={step.placeholder || step.title}
        type={step.type === "phone_input" ? "tel" : "text"}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
          if (event.key === "Enter") {
            event.preventDefault();
            handleSubmit();
          }
        }}
      />
      <Button size="sm" type="button" disabled={!valid} onClick={handleSubmit}>
        {t("inputFlow.confirm")}
      </Button>
    </div>
  );
}

function ConfirmStep({
  items,
  resultKey,
  values,
  onConfirm,
}: {
  items: Array<Record<string, unknown>>;
  resultKey: string;
  values: Record<string, unknown>;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-3">
      <div className="space-y-1 rounded-md border border-border/60 bg-background/60 p-2 text-xs text-muted-foreground">
        <div className="font-medium text-foreground">{resultKey}</div>
        {items.map((item, index) => (
          <div key={index} className="truncate">
            {index + 1}. {itemSummary(item)}
          </div>
        ))}
        {Object.keys(values).length > 0 ? (
          <div className="border-t border-border/60 pt-1">
            <span className="text-foreground">{t("inputFlow.details")}</span> {itemSummary(values)}
          </div>
        ) : null}
      </div>
      <ChoiceMenu items={[{ id: "confirm", label: t("inputFlow.confirm"), value: true }]} onSelect={onConfirm} />
    </div>
  );
}

function ChoiceMenu<T>({
  items,
  maxHeightClassName = "max-h-56",
  onSelect,
}: {
  items: Array<ChoiceMenuItem<T>>;
  maxHeightClassName?: string;
  onSelect: (value: T) => void;
}) {
  const listRef = useRef<HTMLDivElement | null>(null);
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const signature = items.map((item) => `${item.id}:${item.disabled ? "0" : "1"}`).join("|");
  const [selectedIndex, setSelectedIndex] = useState(() => firstEnabledIndex(items));

  useEffect(() => {
    setSelectedIndex(firstEnabledIndex(items));
  }, [signature]);

  useEffect(() => {
    listRef.current?.focus();
  }, [signature]);

  useEffect(() => {
    scrollActiveIntoList(selectedRef.current, listRef.current);
  }, [selectedIndex, signature]);

  function move(delta: number) {
    setSelectedIndex((current) => nextEnabledIndex(items, current, delta));
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        move(1);
        return;
      case "ArrowUp":
        event.preventDefault();
        move(-1);
        return;
      case "Home":
        event.preventDefault();
        setSelectedIndex(firstEnabledIndex(items));
        return;
      case "End":
        event.preventDefault();
        setSelectedIndex(lastEnabledIndex(items));
        return;
      case "Enter":
      case " ":
        event.preventDefault();
        if (items[selectedIndex] && !items[selectedIndex].disabled) {
          onSelect(items[selectedIndex].value);
        }
        return;
      default:
        return;
    }
  }

  return (
    <div
      ref={listRef}
      className={cn("grid gap-0.5 overflow-y-auto pr-1 outline-none", maxHeightClassName)}
      role="listbox"
      tabIndex={0}
      onKeyDown={handleKeyDown}
    >
      {items.map((item, index) => (
        <button
          key={item.id}
          ref={index === selectedIndex ? selectedRef : undefined}
          aria-selected={index === selectedIndex}
          className={cn(
            "min-w-0 rounded-md px-2.5 py-1.5 text-left transition hover:bg-muted disabled:opacity-50",
            index === selectedIndex && "bg-muted text-foreground",
          )}
          disabled={item.disabled}
          role="option"
          tabIndex={-1}
          type="button"
          onMouseEnter={() => {
            if (!item.disabled) {
              setSelectedIndex(index);
            }
          }}
          onMouseDown={(event) => {
            event.preventDefault();
            if (!item.disabled) {
              onSelect(item.value);
            }
          }}
        >
          {item.render ? (
            item.render(index === selectedIndex)
          ) : (
            <>
              <div className="truncate text-sm font-medium">{item.label}</div>
              {item.description ? <div className="mt-0.5 truncate text-xs text-muted-foreground">{item.description}</div> : null}
            </>
          )}
        </button>
      ))}
    </div>
  );
}

function firstEnabledIndex(items: Array<{ disabled?: boolean }>) {
  const index = items.findIndex((item) => !item.disabled);
  return index >= 0 ? index : 0;
}

function lastEnabledIndex(items: Array<{ disabled?: boolean }>) {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (!items[index]?.disabled) {
      return index;
    }
  }
  return 0;
}

function nextEnabledIndex(items: Array<{ disabled?: boolean }>, current: number, delta: number) {
  if (items.length === 0) {
    return 0;
  }
  let index = current;
  for (let count = 0; count < items.length; count += 1) {
    index = (index + delta + items.length) % items.length;
    if (!items[index]?.disabled) {
      return index;
    }
  }
  return current;
}

function scrollActiveIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeRect = active.getBoundingClientRect();
  const listRect = list.getBoundingClientRect();
  const padding = 4;
  if (activeRect.top < listRect.top + padding) {
    list.scrollTop -= listRect.top + padding - activeRect.top;
  } else if (activeRect.bottom > listRect.bottom - padding) {
    list.scrollTop += activeRect.bottom - (listRect.bottom - padding);
  }
}

function normalizeUIConfirm(value: unknown): UIConfirmSchema | null {
  const raw = parseRecord(value);
  if (!raw) {
    return null;
  }
  const title = stringValue(raw.title);
  if (!title) {
    return null;
  }
  const rows = Array.isArray(raw.rows)
    ? raw.rows
        .map((row) => {
          const record = parseRecord(row);
          const label = stringValue(record?.label);
          if (!record || !label) {
            return null;
          }
          return {
            description: stringValue(record.description),
            label,
            value: record.value,
          };
        })
        .filter(Boolean) as UIConfirmRow[]
    : [];
  return {
    cancelLabel: stringValue(raw.cancelLabel),
    confirmLabel: stringValue(raw.confirmLabel),
    description: stringValue(raw.description),
    destructive: raw.destructive === true,
    rows,
    title,
  };
}

function normalizeInputFlow(value: unknown): { raw: Record<string, unknown>; schema: InputFlowSchema } | null {
  const raw = parseRecord(value);
  if (!raw || raw.type !== "repeat" || !Array.isArray(raw.repeatSteps)) {
    return null;
  }
  const title = stringValue(raw.title);
  if (!title) {
    return null;
  }
  const repeatSteps = raw.repeatSteps.map(normalizeStep).filter(Boolean) as InputFlowStep[];
  if (repeatSteps.length === 0) {
    return null;
  }
  return {
    raw,
    schema: {
      afterItem: normalizeAfterItem(raw.afterItem),
      description: stringValue(raw.description),
      maxItems: numberValue(raw.maxItems),
      minItems: numberValue(raw.minItems),
      nextSteps: Array.isArray(raw.nextSteps) ? (raw.nextSteps.map(normalizeStep).filter(Boolean) as InputFlowStep[]) : [],
      repeatSteps,
      resultKey: stringValue(raw.resultKey) || "items",
      title,
      type: "repeat",
    },
  };
}

function normalizeStep(value: unknown): InputFlowStep | null {
  const record = parseRecord(value);
  if (!record) {
    return null;
  }
  const type =
    record.type === "option_list" ||
    record.type === "quick_number" ||
    record.type === "text_input" ||
    record.type === "phone_input" ||
    record.type === "confirm"
      ? record.type
      : "";
  const id = stringValue(record.id) || (type === "confirm" ? "confirm" : "");
  const title = stringValue(record.title);
  if (!id || !title || !type) {
    return null;
  }
  return {
    customLabel: stringValue(record.customLabel),
    description: stringValue(record.description),
    id,
    max: numberValue(record.max),
    maxFrom: stringValue(record.maxFrom),
    min: numberValue(record.min),
    options: Array.isArray(record.options) ? record.options : undefined,
    placeholder: stringValue(record.placeholder),
    required: record.required === false ? false : undefined,
    title,
    type,
  };
}

function normalizeAfterItem(value: unknown): InputFlowAfterItem | undefined {
  const record = parseRecord(value);
  if (!record) {
    return undefined;
  }
  return {
    title: stringValue(record.title),
    actions: Array.isArray(record.actions)
      ? record.actions
          .map((action) => {
            const actionRecord = parseRecord(action);
            const id = actionRecord?.id === "continue" || actionRecord?.id === "done" ? actionRecord.id : undefined;
            const label = stringValue(actionRecord?.label);
            return id && label ? { id, label } : null;
          })
          .filter(Boolean) as InputFlowAfterItem["actions"]
      : undefined,
  };
}

function normalizeOption(value: unknown): InputFlowOption | null {
  if (typeof value === "number" || typeof value === "string") {
    return { value, title: String(value) };
  }
  const record = parseRecord(value);
  if (!record) {
    return null;
  }
  return {
    data: parseRecord(record.data) || undefined,
    description: stringValue(record.description),
    label: stringValue(record.label),
    title: stringValue(record.title),
    value: record.value,
  };
}

function numberLimit(step: InputFlowStep, selections: Record<string, unknown>) {
  if (typeof step.max === "number") {
    return step.max;
  }
  if (!step.maxFrom) {
    return undefined;
  }
  const value = getPath(selections, step.maxFrom);
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function getPath(source: unknown, path: string) {
  return path.split(".").reduce<unknown>((current, key) => {
    const record = parseRecord(current);
    return record ? record[key] : undefined;
  }, source);
}

function actionLabel(afterItem: InputFlowAfterItem | undefined, id: "continue" | "done", fallback: string) {
  return afterItem?.actions?.find((action) => action.id === id)?.label || fallback;
}

function resultPayload(schema: InputFlowSchema, items: Array<Record<string, unknown>>, values: Record<string, unknown>) {
  const resultKey = schema.resultKey || "items";
  return {
    type: "input_flow_result",
    title: schema.title,
    [resultKey]: items,
    ...compactRecord(values),
  };
}

function itemSummary(item: Record<string, unknown>) {
  const friendly = friendlyItemSummary(item);
  if (friendly) {
    return friendly;
  }
  const entries = Object.entries(item).filter(([, value]) => value !== undefined && value !== "");
  if (entries.length === 0) {
    return "";
  }
  return entries
    .filter(([key]) => !technicalSummaryKey(key))
    .slice(0, 3)
    .map(([, value]) => String(value))
    .join(" · ");
}

function formatConfirmValue(value: unknown) {
  if (value === undefined || value === null) {
    return "";
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  const record = parseRecord(value);
  if (record) {
    return itemSummary(record) || JSON.stringify(record);
  }
  if (Array.isArray(value)) {
    return value
      .map((item) => {
        const itemRecord = parseRecord(item);
        return itemRecord ? itemSummary(itemRecord) || JSON.stringify(itemRecord) : String(item);
      })
      .join("、");
  }
  return String(value);
}

function friendlyItemSummary(item: Record<string, unknown>) {
  const title =
    stringValue(item.roomTypeLabel) ||
    stringValue(item.roomLabel) ||
    stringValue(item.name) ||
    stringValue(item.title) ||
    stringValue(item.label);
  const count = numberValue(item.count) ?? numberValue(item.quantity) ?? numberValue(item.num);
  const price = numberValue(item.price) ?? numberValue(item.realRate) ?? numberValue(item.rate);
  const parts = [title, priceSummary(price), count ? `${count}间` : ""].filter(Boolean);
  return parts.join(" · ");
}

function priceSummary(price: number | undefined) {
  if (price === undefined) {
    return "";
  }
  const amount = price >= 1000 && Number.isInteger(price) ? price / 100 : price;
  return `¥${Number.isInteger(amount) ? amount.toFixed(0) : amount.toFixed(2)}`;
}

function technicalSummaryKey(key: string) {
  const lower = key.toLowerCase();
  return lower.endsWith("id") || lower.endsWith("code") || lower === "availnum" || lower === "billingid";
}

function compactRecord(record: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(record).filter(([, value]) => value !== undefined && value !== ""));
}

function parseRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      return parseRecord(parsed);
    } catch {
      return null;
    }
  }
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
