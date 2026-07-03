import { Check, ChevronRight, TextCursorInput, X } from "lucide-react";
import { useMemo, useState, type KeyboardEvent } from "react";

import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { cancelInputFlow, completeInputFlow, type InputFlowRequest } from "@/state/inputFlowStore";

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

export function InputFlowPanel({ request }: { request: InputFlowRequest }) {
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
    <div className="absolute right-4 bottom-full left-4 z-20 mb-1 max-h-[min(24rem,calc(100vh-12rem))] overflow-y-auto rounded-t-xl border border-border/70 bg-popover/95 px-3 py-2 text-popover-foreground shadow-sm backdrop-blur sm:right-8 sm:left-16">
      <div className="flex min-w-0 items-start gap-2">
        <TextCursorInput className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{schema.title}</div>
          {schema.description ? <div className="mt-0.5 line-clamp-2 text-xs leading-5 text-muted-foreground">{schema.description}</div> : null}
        </div>
        <Button
          aria-label={t("common.cancel")}
          className="-mr-1 -mt-1 size-7 shrink-0 rounded-full"
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => cancelInputFlow(request)}
        >
          <X className="size-4" />
        </Button>
      </div>
      <div className="mt-2 text-sm text-foreground">
          {items.length > 0 ? (
            <div className="mt-3 space-y-1">
              {items.map((item, index) => (
                <div key={index} className="flex items-center gap-2 rounded-md bg-background/60 px-2 py-1.5 text-xs">
                  <Check className="size-3.5 text-primary" />
                  <span className="min-w-0 truncate">{itemSummary(item)}</span>
                </div>
              ))}
            </div>
          ) : null}
          {!collectingNext && activeStep ? (
            <div className="mt-3 space-y-2">
              <StepTitle step={activeStep} />
              {activeStep.type === "option_list" ? (
                <OptionList step={activeStep} onSelect={(option) => selectOption(activeStep, option)} />
              ) : activeStep.type === "quick_number" ? (
                <QuickNumber
                  customOpen={customOpen}
                  customValue={customValue}
                  max={numberLimit(activeStep, rawSelections)}
                  step={activeStep}
                  onCustomOpen={() => setCustomOpen(true)}
                  onCustomValue={setCustomValue}
                  onSelect={(value) => selectNumber(activeStep, value)}
                />
              ) : null}
            </div>
          ) : null}
          {collectingNext && nextStep ? (
            <div className="mt-3 space-y-2">
              <StepTitle step={nextStep} />
              {nextStep.type === "text_input" || nextStep.type === "phone_input" ? (
                <TextInputStep
                  step={nextStep}
                  value={textValue}
                  onChange={setTextValue}
                  onSubmit={(value) => commitNextValue(nextStep, value)}
                />
              ) : nextStep.type === "confirm" ? (
                <ConfirmStep
                  items={items}
                  resultKey={schema.resultKey || "items"}
                  values={nextValues}
                  onConfirm={() => commitNextValue(nextStep, true)}
                />
              ) : nextStep.type === "option_list" ? (
                <OptionList
                  step={nextStep}
                  onSelect={(option) => commitNextValue(nextStep, option.value ?? option.title ?? option.label)}
                />
              ) : nextStep.type === "quick_number" ? (
                <QuickNumber
                  customOpen={customOpen}
                  customValue={customValue}
                  max={numberLimit(nextStep, nextValues)}
                  step={nextStep}
                  onCustomOpen={() => setCustomOpen(true)}
                  onCustomValue={setCustomValue}
                  onSelect={(value) => commitNextValue(nextStep, value)}
                />
              ) : null}
            </div>
          ) : null}
          {currentReady ? (
            <div className="mt-3 space-y-3">
              {schema.afterItem?.title ? <div className="text-xs font-medium text-muted-foreground">{schema.afterItem.title}</div> : null}
              <div className="rounded-md border border-border/60 bg-background/60 px-2 py-1.5 text-xs text-muted-foreground">
                {itemSummary(current)}
              </div>
              <div className="flex flex-wrap items-center gap-2">
                {canAddMore ? (
                  <Button variant="outline" size="sm" type="button" onClick={commitCurrent}>
                    {actionLabel(schema.afterItem, "continue", t("inputFlow.continue"))}
                  </Button>
                ) : null}
                <Button
                  size="sm"
                  type="button"
                  disabled={doneDisabled}
                  onClick={() => {
                    const finalItems = commitCurrent();
                    startNextSteps(finalItems);
                  }}
                >
                  {actionLabel(schema.afterItem, "done", t("inputFlow.done"))}
                </Button>
                <Button size="sm" type="button" variant="ghost" onClick={() => cancelInputFlow(request)}>
                  {t("common.cancel")}
                </Button>
              </div>
            </div>
          ) : null}
      </div>
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
    <div className="grid max-h-56 gap-2 overflow-y-auto pr-1">
      {options.map((option, index) => {
        const title = stringValue(option.title) || stringValue(option.label) || String(option.value ?? "");
        return (
          <button
            key={`${title}:${index}`}
            className="min-w-0 rounded-md border border-border/60 bg-background/70 px-3 py-2 text-left transition hover:bg-muted"
            type="button"
            onClick={() => onSelect(option)}
          >
            <div className="truncate text-sm font-medium">{title}</div>
            {option.description ? <div className="mt-0.5 truncate text-xs text-muted-foreground">{option.description}</div> : null}
          </button>
        );
      })}
    </div>
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
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {options.map((value) => (
          <Button key={value} variant="outline" size="sm" type="button" onClick={() => onSelect(value)}>
            {value}
          </Button>
        ))}
        <Button variant="outline" size="sm" type="button" onClick={onCustomOpen}>
          {step.customLabel || t("inputFlow.custom")}
        </Button>
      </div>
      {customOpen ? (
        <div className="flex items-center gap-2">
          <input
            className={cn(
              "h-8 w-24 rounded-md border border-border bg-background px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40",
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
              if (event.key === "Enter" && customValid) {
                event.preventDefault();
                onSelect(customNumber);
              }
            }}
          />
          <Button size="sm" type="button" disabled={!customValid} onClick={() => onSelect(customNumber)}>
            {t("inputFlow.confirm")}
          </Button>
        </div>
      ) : null}
    </div>
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
      <Button size="sm" type="button" onClick={onConfirm}>
        {t("inputFlow.confirm")}
      </Button>
    </div>
  );
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
  const entries = Object.entries(item).filter(([, value]) => value !== undefined && value !== "");
  if (entries.length === 0) {
    return "";
  }
  return entries
    .slice(0, 4)
    .map(([key, value]) => `${key}: ${String(value)}`)
    .join(" · ");
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
