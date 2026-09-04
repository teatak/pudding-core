import { Check, ChevronRight, TextCursorInput, X } from "@/components/icons";
import { useEffect, useMemo, useState, type KeyboardEvent, type ReactNode } from "react";

import { ChoiceMenu, type ChoiceMenuItem } from "@/components/ChoiceMenu";
import { ComposerFloatingPanel } from "@/components/ComposerFloatingPanel";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { dismissInputFlow, type InputFlowRequest } from "@/state/inputFlowStore";
import type { ContentPart } from "@/api/client";

type FormInputFlowSchema = {
  description?: string;
  steps: InputFlowStep[];
  title: string;
  type: "form";
};
type RepeatInputFlowSchema = {
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
type InputFlowSchema = FormInputFlowSchema | RepeatInputFlowSchema;
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
  type:
    | "single_select"
    | "multi_select"
    | "quick_number"
    | "number_input"
    | "text_input"
    | "phone_input"
    | "date_input"
    | "confirm";
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
type Translate = ReturnType<typeof useI18n>["t"];
export type InputFlowSubmission = {
  formResult: Extract<ContentPart, { type: "form_result" }>;
  request: InputFlowRequest;
  result: Record<string, unknown>;
  text: string;
};

function noop() {}

export function InputFlowPanel({ request, onSubmit }: { request: InputFlowRequest; onSubmit: (submission: InputFlowSubmission) => void }) {
  return <InputFlowContent request={request} onSubmit={onSubmit} />;
}

function InputFlowContent({ request, onSubmit }: { request: InputFlowRequest; onSubmit: (submission: InputFlowSubmission) => void }) {
  const flow = useMemo(() => normalizeInputFlow(request.args), [request.args]);

  if (!flow) {
    return null;
  }
  if (flow.schema.type === "form") {
    return <FormInputFlowContent request={request} schema={flow.schema} onSubmit={onSubmit} />;
  }
  return <RepeatInputFlowContent request={request} schema={flow.schema} onSubmit={onSubmit} />;
}

function RepeatInputFlowContent({
  request,
  schema,
  onSubmit,
}: {
  request: InputFlowRequest;
  schema: RepeatInputFlowSchema;
  onSubmit: (submission: InputFlowSubmission) => void;
}) {
  const { t } = useI18n();
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
  const [multiSelectedKeys, setMultiSelectedKeys] = useState<string[]>([]);

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
    const values = { ...nextValues, [step.id]: value };
    setNextValues(values);
    setTextValue("");
    setMultiSelectedKeys([]);
    const nextIndex = nextStepIndex + 1;
    if (nextIndex >= schema.nextSteps.length) {
      submit(items, values);
      return;
    }
    setNextStepIndex(nextIndex);
  }

  function submit(finalItems: Array<Record<string, unknown>>, values: Record<string, unknown>) {
    const result = resultPayload(schema, finalItems, values);
    dismissInputFlow(request);
    onSubmit({ formResult: formResultPart(schema, result), request, result, text: inputFlowSubmissionText(schema, result, t) });
  }

  return (
    <FloatingUIPanel
      cancelLabel={t("common.cancel")}
      description={schema.description}
      title={schema.title}
      onCancel={() => dismissInputFlow(request)}
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
            multiSelectedKeys={multiSelectedKeys}
            step={nextStep}
            textValue={textValue}
            onCustomOpen={() => setCustomOpen(true)}
            onCustomValue={setCustomValue}
            onMultiSelectedKeys={setMultiSelectedKeys}
            onMultiSubmit={(value) => commitNextValue(nextStep, value)}
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
            onCancel={() => dismissInputFlow(request)}
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

function FormInputFlowContent({
  request,
  schema,
  onSubmit,
}: {
  request: InputFlowRequest;
  schema: FormInputFlowSchema;
  onSubmit: (submission: InputFlowSubmission) => void;
}) {
  const { t } = useI18n();
  const [stepIndex, setStepIndex] = useState(0);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [rawSelections, setRawSelections] = useState<Record<string, unknown>>({});
  const [customOpen, setCustomOpen] = useState(false);
  const [customValue, setCustomValue] = useState("");
  const [textValue, setTextValue] = useState("");
  const [multiSelectedKeys, setMultiSelectedKeys] = useState<string[]>([]);
  const activeStep = schema.steps[stepIndex];

  function resetStepState() {
    setCustomOpen(false);
    setCustomValue("");
    setTextValue("");
    setMultiSelectedKeys([]);
  }

  function commitValue(step: InputFlowStep, value: unknown, rawValue: unknown = value) {
    const nextValues = value === undefined ? values : { ...values, [step.id]: value };
    setValues(nextValues);
    if (rawValue !== undefined) {
      setRawSelections((previous) => ({ ...previous, [step.id]: rawValue }));
    }
    resetStepState();
    if (stepIndex + 1 >= schema.steps.length) {
      const result = resultPayload(schema, [], nextValues);
      dismissInputFlow(request);
      onSubmit({ formResult: formResultPart(schema, result), request, result, text: inputFlowSubmissionText(schema, result, t) });
      return;
    }
    setStepIndex((index) => index + 1);
  }

  function selectOption(step: InputFlowStep, option: InputFlowOption) {
    const label = stringValue(option.title) || stringValue(option.label);
    commitValue(step, option.value ?? option.data ?? label, option);
  }

  if (!activeStep) {
    return null;
  }

  return (
    <FloatingUIPanel
      cancelLabel={t("common.cancel")}
      description={schema.description}
      title={schema.title}
      onCancel={() => dismissInputFlow(request)}
    >
      <div className="space-y-3 text-sm text-foreground">
        <ActiveStep
          customOpen={customOpen}
          customValue={customValue}
          max={numberLimit(activeStep, rawSelections)}
          multiSelectedKeys={multiSelectedKeys}
          step={activeStep}
          textValue={textValue}
          onConfirm={() => commitValue(activeStep, true)}
          onCustomOpen={() => setCustomOpen(true)}
          onCustomValue={setCustomValue}
          onMultiSelectedKeys={setMultiSelectedKeys}
          onMultiSubmit={(selected) => commitValue(activeStep, selected, selected)}
          onNumberSelect={(value) => commitValue(activeStep, value)}
          onOptionSelect={(option) => selectOption(activeStep, option)}
          onTextChange={setTextValue}
          onTextSubmit={(value) => commitValue(activeStep, value)}
          confirmValues={values}
        />
        {activeStep.required === false && activeStep.type !== "confirm" ? (
          <Button className="h-7 px-2 text-xs text-muted-foreground" size="sm" type="button" variant="ghost" onClick={() => commitValue(activeStep, undefined)}>
            {t("inputFlow.skip")}
          </Button>
        ) : null}
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
    <ComposerFloatingPanel
      className="flex flex-col gap-2 overflow-hidden"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="flex min-w-0 shrink-0 items-start gap-2">
        <TextCursorInput className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{title}</div>
          {description ? <div className="mt-0.5 line-clamp-2 text-xs leading-5 text-muted-foreground">{description}</div> : null}
        </div>
        <Button
          aria-label={cancelLabel}
          className="size-7 shrink-0 rounded-full"
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onCancel}
        >
          <X className="size-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </ComposerFloatingPanel>
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
  multiSelectedKeys = [],
  step,
  textValue = "",
  onConfirm,
  onCustomOpen,
  onCustomValue,
  onMultiSelectedKeys,
  onMultiSubmit,
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
  multiSelectedKeys?: string[];
  step: InputFlowStep;
  textValue?: string;
  onConfirm?: () => void;
  onCustomOpen: () => void;
  onCustomValue: (value: string) => void;
  onMultiSelectedKeys?: (keys: string[]) => void;
  onMultiSubmit?: (values: unknown[]) => void;
  onNumberSelect: (value: number) => void;
  onOptionSelect: (option: InputFlowOption) => void;
  onTextChange?: (value: string) => void;
  onTextSubmit?: (value: unknown) => void;
}) {
  return (
    <div className="space-y-2">
      <StepTitle step={step} />
      {step.type === "single_select" ? (
        <OptionList step={step} onSelect={onOptionSelect} />
      ) : step.type === "multi_select" ? (
        <MultiSelect
          selectedKeys={multiSelectedKeys}
          step={step}
          onSelectedKeys={onMultiSelectedKeys || noop}
          onSubmit={onMultiSubmit || noop}
        />
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
      ) : step.type === "text_input" || step.type === "phone_input" || step.type === "number_input" || step.type === "date_input" ? (
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
  schema: RepeatInputFlowSchema;
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

function MultiSelect({
  selectedKeys,
  step,
  onSelectedKeys,
  onSubmit,
}: {
  selectedKeys: string[];
  step: InputFlowStep;
  onSelectedKeys: (keys: string[]) => void;
  onSubmit: (values: unknown[]) => void;
}) {
  const { t } = useI18n();
  const options = (step.options || []).map(normalizeOption).filter(Boolean) as InputFlowOption[];
  const entries = options.map((option, index) => ({ key: optionKey(option, index), option }));
  const selected = new Set(selectedKeys);
  const min = typeof step.min === "number" ? Math.max(0, Math.floor(step.min)) : step.required === false ? 0 : 1;
  const max = typeof step.max === "number" && step.max > 0 ? Math.floor(step.max) : undefined;
  const maxReached = max !== undefined && selected.size >= max;

  function toggle(key: string) {
    if (selected.has(key)) {
      onSelectedKeys(selectedKeys.filter((item) => item !== key));
      return;
    }
    if (!maxReached) {
      onSelectedKeys([...selectedKeys, key]);
    }
  }

  return (
    <div className="space-y-2">
      <ChoiceMenu
        items={entries.map(({ key, option }) => {
          const title = stringValue(option.title) || stringValue(option.label) || String(option.value ?? "");
          const checked = selected.has(key);
          return {
            id: key,
            label: title,
            description: option.description,
            disabled: maxReached && !checked,
            value: key,
            render: () => (
              <div className="flex min-w-0 items-start gap-2">
                <span className={cn("mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border", checked ? "border-primary bg-primary text-primary-foreground" : "border-border")}>
                  {checked ? <Check className="size-3" data-icon-weight="strong" /> : null}
                </span>
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{title}</span>
                  {option.description ? <span className="mt-0.5 block truncate text-xs text-muted-foreground">{option.description}</span> : null}
                </span>
              </div>
            ),
          };
        })}
        onSelect={toggle}
      />
      <Button
        className="h-7 px-3 text-xs"
        size="sm"
        type="button"
        disabled={selected.size < min}
        onClick={() =>
          onSubmit(
            entries
              .filter(({ key }) => selected.has(key))
              .map(({ option }) => option.value ?? option.data ?? option.title ?? option.label),
          )
        }
      >
        {t("inputFlow.confirm")}
      </Button>
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
  const menuItems: Array<ChoiceMenuItem<{ type: "number"; value: number } | { type: "custom" }>> = [
    ...options.map((value) => ({ id: String(value), label: String(value), value: { type: "number" as const, value } })),
    {
      id: "custom",
      label: step.customLabel || t("inputFlow.custom"),
      noActiveStyle: customOpen,
      value: { type: "custom" as const },
      render: () =>
        customOpen ? (
          <div className="flex min-w-0 items-center gap-2">
            <input
              autoFocus
              className={cn(
                "h-6 min-w-0 flex-1 rounded-md border border-border bg-transparent px-2 text-sm outline-none focus-visible:border-ring",
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
            <Button className="h-6 px-2 text-xs" size="sm" type="button" disabled={!customValid} onMouseDown={(event) => event.preventDefault()} onClick={() => onSelect(customNumber)}>
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
  onSubmit: (value: unknown) => void;
}) {
  const { t } = useI18n();
  const text = value.trim();
  const required = step.required !== false;
  const numericValue = Number(text);
  const numberValid =
    step.type !== "number_input" ||
    (text.length > 0 &&
      Number.isFinite(numericValue) &&
      (typeof step.min !== "number" || numericValue >= step.min) &&
      (typeof step.max !== "number" || numericValue <= step.max));
  const valid = (!required || text.length > 0) && (text.length === 0 || numberValid);
  function handleSubmit() {
    if (valid) {
      onSubmit(step.type === "number_input" && text ? numericValue : text);
    }
  }
  const inputType = step.type === "phone_input" ? "tel" : step.type === "number_input" ? "number" : step.type === "date_input" ? "date" : "text";
  return (
    <div className="flex items-center gap-2">
      <input
        autoFocus
        className={cn(
          "h-8 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-sm outline-none focus-visible:border-ring",
          value && !valid ? "border-destructive" : "",
        )}
        inputMode={step.type === "phone_input" ? "tel" : step.type === "number_input" ? "decimal" : "text"}
        min={step.type === "number_input" ? step.min : undefined}
        max={step.type === "number_input" ? step.max : undefined}
        placeholder={step.placeholder || step.title}
        type={inputType}
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
        {items.length > 0 ? (
          <>
            <div className="font-medium text-foreground">{resultKey}</div>
            {items.map((item, index) => (
              <div key={index} className="truncate">
                {index + 1}. {itemSummary(item)}
              </div>
            ))}
          </>
        ) : null}
        {Object.keys(values).length > 0 ? (
          <div className={cn(items.length > 0 && "border-t border-border/60 pt-1")}>
            <span className="text-foreground">{t("inputFlow.details")}</span> {itemSummary(values)}
          </div>
        ) : null}
      </div>
      <ChoiceMenu items={[{ id: "confirm", label: t("inputFlow.confirm"), value: true }]} onSelect={onConfirm} />
    </div>
  );
}

function normalizeInputFlow(value: unknown): { raw: Record<string, unknown>; schema: InputFlowSchema } | null {
  const raw = parseRecord(value);
  if (!raw || (raw.type !== "form" && raw.type !== "repeat")) {
    return null;
  }
  const title = stringValue(raw.title);
  if (!title) {
    return null;
  }
  if (raw.type === "form") {
    const steps = Array.isArray(raw.steps) ? (raw.steps.map(normalizeStep).filter(Boolean) as InputFlowStep[]) : [];
    if (steps.length === 0) {
      return null;
    }
    return {
      raw,
      schema: {
        description: stringValue(raw.description),
        steps,
        title,
        type: "form",
      },
    };
  }
  if (!Array.isArray(raw.repeatSteps)) {
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
    record.type === "single_select" ||
    record.type === "multi_select" ||
    record.type === "quick_number" ||
    record.type === "number_input" ||
    record.type === "text_input" ||
    record.type === "phone_input" ||
    record.type === "date_input" ||
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

function optionKey(option: InputFlowOption, index: number) {
  const value = option.value ?? option.title ?? option.label ?? option.data;
  try {
    return `${index}:${JSON.stringify(value)}`;
  } catch {
    return `${index}:${String(value ?? "")}`;
  }
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
  if (schema.type === "form") {
    return {
      type: "user_input_result",
      title: schema.title,
      ...compactRecord(values),
    };
  }
  const resultKey = schema.resultKey || "items";
  return {
    type: "user_input_result",
    title: schema.title,
    [resultKey]: items,
    ...compactRecord(values),
  };
}

function formResultPart(
  schema: InputFlowSchema,
  result: Record<string, unknown>,
): Extract<ContentPart, { type: "form_result" }> {
  return {
    type: "form_result",
    title: schema.title,
    schema: schema as unknown as Record<string, unknown>,
    result,
  };
}

function inputFlowSubmissionText(schema: InputFlowSchema, result: Record<string, unknown>, t: Translate) {
  const details: string[] = [];
  if (schema.type === "form") {
    for (const step of schema.steps) {
      if (!Object.prototype.hasOwnProperty.call(result, step.id)) {
        continue;
      }
      details.push(`${step.title}: ${formatStepSubmissionValue(step, result[step.id], t)}`);
    }
  } else {
    const resultKey = schema.resultKey || "items";
    const items = result[resultKey];
    if (Array.isArray(items) && items.length > 0) {
      details.push(`${resultKey}: ${formatSubmissionValue(items)}`);
    }
    for (const step of schema.nextSteps) {
      if (!Object.prototype.hasOwnProperty.call(result, step.id)) {
        continue;
      }
      details.push(`${step.title}: ${formatStepSubmissionValue(step, result[step.id], t)}`);
    }
  }
  return t("inputFlow.response")
    .replace("{title}", schema.title)
    .replace("{values}", details.join("；") || t("inputFlow.confirmed"));
}

function formatStepSubmissionValue(step: InputFlowStep, value: unknown, t: Translate) {
  if (step.type === "confirm" && value === true) {
    return t("inputFlow.confirmed");
  }
  if (step.type === "single_select" || step.type === "multi_select") {
    const values = Array.isArray(value) ? value : [value];
    return values
      .map((item) => {
        const raw = formatSubmissionValue(item);
        const label = selectedOptionLabel(step, item);
        return label && label !== raw ? `${label}（${raw}）` : label || raw;
      })
      .join("、");
  }
  return formatSubmissionValue(value);
}

function selectedOptionLabel(step: InputFlowStep, value: unknown) {
  const options = (step.options || []).map(normalizeOption).filter(Boolean) as InputFlowOption[];
  const target = comparableValue(value);
  for (const option of options) {
    const optionValue = option.value ?? option.data ?? option.title ?? option.label;
    if (comparableValue(optionValue) === target) {
      return stringValue(option.title) || stringValue(option.label) || String(option.value ?? "");
    }
  }
  return "";
}

function comparableValue(value: unknown) {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function formatSubmissionValue(value: unknown): string {
  if (value === undefined || value === null) {
    return "";
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.map(formatSubmissionValue).filter(Boolean).join("；");
  }
  const record = parseRecord(value);
  if (record) {
    return Object.entries(record)
      .filter(([, item]) => item !== undefined && item !== "")
      .map(([key, item]) => `${key}: ${formatSubmissionValue(item)}`)
      .join("，");
  }
  return String(value);
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
