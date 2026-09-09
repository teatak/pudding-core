import { Check, MessageCircleQuestionMark, Pencil, X } from "@/components/icons";
import { useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { ChoiceMenu, type ChoiceMenuItem } from "@/components/ChoiceMenu";
import { ComposerFloatingPanel } from "@/components/ComposerFloatingPanel";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { dismissInputFlow, type InputFlowRequest } from "@/state/inputFlowStore";
import type { ContentPart } from "@/api/client";

type FormInputFlowSchema = {
  steps: InputFlowStep[];
  title: string;
  type: "form";
};
type RepeatInputFlowSchema = {
  afterItem?: InputFlowAfterItem;
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
  allowCustom?: boolean;
  customLabel?: string;
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
  const completedItemCount = items.length + (currentReady && Object.keys(compactRecord(current)).length > 0 ? 1 : 0);
  const canAddMore = !maxItems || completedItemCount < maxItems;
  const doneDisabled = completedItemCount < minItems;

  function advance() {
    setTextValue("");
    setStepIndex((index) => Math.min(index + 1, steps.length));
  }

  function selectOption(step: InputFlowStep, option: InputFlowOption) {
    const next = { ...current };
    const label = stringValue(option.title) || stringValue(option.label);
    if (option.value === undefined && option.data && typeof option.data === "object") {
      Object.assign(next, option.data);
    } else {
      next[step.id] = option.value !== undefined ? option.value : label;
    }
    if (label) {
      next[`${step.id}Label`] = label;
    }
    setCurrent(next);
    setRawSelections((previous) => ({ ...previous, [step.id]: option }));
    advance();
  }

  function commitCurrentValue(step: InputFlowStep, value: unknown) {
    setCurrent((previous) => ({ ...previous, [step.id]: value }));
    setRawSelections((previous) => ({ ...previous, [step.id]: value }));
    advance();
  }

  function commitCurrent() {
    const nextItem = compactRecord(current);
    const nextItems = Object.keys(nextItem).length > 0 ? [...items, nextItem] : items;
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
    const values = value === undefined ? nextValues : { ...nextValues, [step.id]: value };
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
      title={schema.title}
      onCancel={() => dismissInputFlow(request)}
    >
      <div className="space-y-3 text-sm text-foreground">
        {items.length > 0 ? <SelectedItems items={items} /> : null}
        {!collectingNext && activeStep ? (
          <ActiveStep
            key={activeStep.id}
            max={numberLimit(activeStep, rawSelections)}
            step={activeStep}
            textValue={textValue}
            onTextChange={setTextValue}
            onTextSubmit={(value) => commitCurrentValue(activeStep, value)}
            onOptionSelect={(option) => selectOption(activeStep, option)}
            onSkip={activeStep.required === false ? advance : undefined}
          />
        ) : null}
        {collectingNext && nextStep ? (
          <ActiveStep
            key={nextStep.id}
            max={numberLimit(nextStep, nextValues)}
            multiSelectedKeys={multiSelectedKeys}
            step={nextStep}
            textValue={textValue}
            onMultiSelectedKeys={setMultiSelectedKeys}
            onMultiSubmit={(value) => commitNextValue(nextStep, value)}
            onOptionSelect={(option) => commitNextValue(nextStep, optionValue(option))}
            onTextChange={setTextValue}
            onTextSubmit={(value) => commitNextValue(nextStep, value)}
            onConfirm={() => commitNextValue(nextStep, true)}
            onSkip={nextStep.required === false && nextStep.type !== "confirm" ? () => commitNextValue(nextStep, undefined) : undefined}
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
  const [textValue, setTextValue] = useState("");
  const [multiSelectedKeys, setMultiSelectedKeys] = useState<string[]>([]);
  const activeStep = schema.steps[stepIndex];

  function resetStepState() {
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
    commitValue(step, optionValue(option), option);
  }

  if (!activeStep) {
    return null;
  }

  return (
    <FloatingUIPanel
      cancelLabel={t("common.cancel")}
      title={schema.title}
      progress={schema.steps.length > 1 ? `${stepIndex + 1} / ${schema.steps.length}` : undefined}
      onCancel={() => dismissInputFlow(request)}
    >
      <div className="space-y-3 text-sm text-foreground">
        <ActiveStep
          key={activeStep.id}
          max={numberLimit(activeStep, rawSelections)}
          multiSelectedKeys={multiSelectedKeys}
          step={activeStep}
          textValue={textValue}
          onConfirm={() => commitValue(activeStep, true)}
          onMultiSelectedKeys={setMultiSelectedKeys}
          onMultiSubmit={(selected) => commitValue(activeStep, selected, selected)}
          onOptionSelect={(option) => selectOption(activeStep, option)}
          onSkip={activeStep.required === false && activeStep.type !== "confirm" ? () => commitValue(activeStep, undefined) : undefined}
          onTextChange={setTextValue}
          onTextSubmit={(value) => commitValue(activeStep, value)}
          confirmValues={values}
        />
      </div>
    </FloatingUIPanel>
  );
}

function FloatingUIPanel({
  cancelLabel,
  children,
  progress,
  title,
  onCancel,
}: {
  cancelLabel: string;
  children: ReactNode;
  progress?: string;
  title: string;
  onCancel: () => void;
}) {
  return (
    <ComposerFloatingPanel
      className="flex max-h-[calc(100dvh-12rem)] flex-col gap-2 overflow-hidden bg-popover pb-1 backdrop-blur-none"
      data-input-flow-panel
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onCancel();
        }
      }}
    >
      <div data-input-flow-header className="flex min-w-0 shrink-0 items-start gap-2">
        <MessageCircleQuestionMark aria-hidden="true" className="mt-1.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="py-0.5 text-sm leading-6 text-muted-foreground">{title}</div>
        </div>
        {progress ? <span className="py-1 text-xs tabular-nums text-muted-foreground">{progress}</span> : null}
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
      <div data-input-flow-body className="-mx-2 min-h-0 flex-1 space-y-2 overflow-y-auto overscroll-contain [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
        {children}
      </div>
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
  max,
  multiSelectedKeys = [],
  step,
  textValue = "",
  onConfirm,
  onMultiSelectedKeys,
  onMultiSubmit,
  onOptionSelect,
  onSkip,
  onTextChange,
  onTextSubmit,
}: {
  confirmItems?: Array<Record<string, unknown>>;
  confirmResultKey?: string;
  confirmValues?: Record<string, unknown>;
  max?: number;
  multiSelectedKeys?: string[];
  step: InputFlowStep;
  textValue?: string;
  onConfirm?: () => void;
  onMultiSelectedKeys?: (keys: string[]) => void;
  onMultiSubmit?: (values: unknown[]) => void;
  onOptionSelect: (option: InputFlowOption) => void;
  onSkip?: () => void;
  onTextChange?: (value: string) => void;
  onTextSubmit?: (value: unknown) => void;
}) {
  const { t } = useI18n();
  const skipAction = onSkip ? (
    <Button className="px-2 text-xs text-muted-foreground" type="button" variant="ghost" onClick={onSkip}>
      {t("inputFlow.skip")}
    </Button>
  ) : null;
  return (
    <div className="space-y-2">
      <StepTitle step={step} />
      {step.type === "single_select" ? (
        <OptionList step={step} value={textValue} skipAction={skipAction} onChange={onTextChange || noop} onCustomSubmit={onTextSubmit || noop} onSelect={onOptionSelect} />
      ) : step.type === "multi_select" ? (
        <MultiSelect
          selectedKeys={multiSelectedKeys}
          skipAction={skipAction}
          step={step}
          onSelectedKeys={onMultiSelectedKeys || noop}
          onSubmit={onMultiSubmit || noop}
        />
      ) : step.type === "text_input" || step.type === "phone_input" || step.type === "number_input" || step.type === "date_input" ? (
        <TextInputStep max={max} skipAction={skipAction} step={step} value={textValue} onChange={onTextChange || noop} onSubmit={onTextSubmit || noop} />
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
        variant="question"
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
    <div data-input-flow-step-title className="min-w-0 px-2">
      <div className="whitespace-normal break-words text-sm font-medium leading-6">{step.title}</div>
    </div>
  );
}

function MultiSelect({
  selectedKeys,
  skipAction,
  step,
  onSelectedKeys,
  onSubmit,
}: {
  selectedKeys: string[];
  skipAction: ReactNode;
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
        variant="question"
        items={entries.map(({ key, option }, index) => {
          const title = stringValue(option.title) || stringValue(option.label) || String(option.value ?? "");
          const checked = selected.has(key);
          return {
            id: key,
            label: title,
            checked,
            disabled: maxReached && !checked,
            value: key,
            render: () => (
              <div className="flex min-w-0 items-start gap-2.5">
                <span aria-hidden="true" className={cn("mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-md border text-xs tabular-nums", checked ? "border-primary bg-primary text-primary-foreground" : "border-border text-muted-foreground")}>
                  {checked ? <Check className="size-3" data-icon-weight="strong" /> : index + 1}
                </span>
                <span className="min-w-0">
                  <span className="block whitespace-normal break-words text-sm font-medium leading-6">{title}</span>
                </span>
              </div>
            ),
          };
        })}
        onSelect={toggle}
      />
      <div className="flex items-center gap-2">
        {skipAction}
        <Button
          className="px-3 text-xs"
          type="button"
          disabled={selected.size < min}
          onClick={() =>
            onSubmit(
              entries
                .filter(({ key }) => selected.has(key))
                .map(({ option }) => optionValue(option)),
            )
          }
        >
          {t("inputFlow.confirm")}
        </Button>
      </div>
    </div>
  );
}

function OptionList({
  value,
  skipAction,
  step,
  onChange,
  onCustomSubmit,
  onSelect,
}: {
  value: string;
  skipAction: ReactNode;
  step: InputFlowStep;
  onChange: (value: string) => void;
  onCustomSubmit: (value: string) => void;
  onSelect: (option: InputFlowOption) => void;
}) {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const options = (step.options || []).map(normalizeOption).filter(Boolean) as InputFlowOption[];
  const answer = value.trim();
  const menuItems: Array<ChoiceMenuItem<{ type: "option"; option: InputFlowOption } | { type: "custom" }>> = options.map((option, index) => ({
    id: String(index),
    label: stringValue(option.title) || stringValue(option.label) || String(optionValue(option)),
    value: { type: "option", option },
  }));
  if (step.allowCustom) {
    menuItems.push({
      id: "custom",
      label: step.customLabel || t("inputFlow.custom"),
      noActiveStyle: true,
      className: "hover:bg-interactive-hover focus-within:bg-interactive-hover",
      value: { type: "custom" as const },
      render: () => (
        <InputEntryRow embedded>
          <input
            ref={inputRef}
            aria-label={step.title}
            className={entryInputClassName}
            placeholder={step.placeholder || step.customLabel || t("inputFlow.customPlaceholder")}
            type="text"
            value={value}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing && answer) {
                event.preventDefault();
                onCustomSubmit(answer);
              }
            }}
          />
          {skipAction}
          <Button type="button" disabled={!answer} onMouseDown={(event) => event.preventDefault()} onClick={() => onCustomSubmit(answer)}>
            {t("composer.send")}
          </Button>
        </InputEntryRow>
      ),
    });
  }
  return (
    <>
      <ChoiceMenu
        variant="question"
        items={menuItems}
        onSelect={(item) => {
          if (item.type === "option") {
            onSelect(item.option);
          } else {
            inputRef.current?.focus();
          }
        }}
      />
      {!step.allowCustom && skipAction}
    </>
  );
}

const entryInputClassName = "h-8 min-w-0 flex-1 border-0 bg-transparent p-0 text-sm outline-none placeholder:text-muted-foreground/60 aria-invalid:text-destructive";

function InputEntryRow({ children, embedded = false }: { children: ReactNode; embedded?: boolean }) {
  return (
    <div data-input-flow-entry className={cn("group/entry flex min-w-0 flex-1 items-center gap-2.5", !embedded && "min-h-11 rounded-md px-2 py-1.5 hover:bg-interactive-hover focus-within:bg-interactive-hover")}>
      <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-md border border-foreground/10 bg-foreground/5 text-muted-foreground group-focus-within/entry:text-foreground">
        <Pencil className="size-3.5" />
      </span>
      {children}
    </div>
  );
}

function TextInputStep({
  max,
  skipAction,
  step,
  value,
  onChange,
  onSubmit,
}: {
  max?: number;
  skipAction: ReactNode;
  step: InputFlowStep;
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: unknown) => void;
}) {
  const { t } = useI18n();
  const text = value.trim();
  const numericValue = Number(text);
  const numberValid =
    step.type !== "number_input" ||
    (text.length > 0 &&
      Number.isFinite(numericValue) &&
      (typeof step.min !== "number" || numericValue >= step.min) &&
      (typeof max !== "number" || numericValue <= max));
  const valid = text.length > 0 && numberValid;
  function handleSubmit() {
    if (valid) {
      onSubmit(step.type === "number_input" && text ? numericValue : text);
    }
  }
  const inputType = step.type === "phone_input" ? "tel" : step.type === "number_input" ? "number" : step.type === "date_input" ? "date" : "text";
  return (
    <InputEntryRow>
      <input
        autoFocus
        aria-label={step.title}
        aria-invalid={Boolean(value && !valid)}
        className={entryInputClassName}
        inputMode={step.type === "phone_input" ? "tel" : step.type === "number_input" ? "decimal" : "text"}
        min={step.type === "number_input" ? step.min : undefined}
        max={step.type === "number_input" ? max : undefined}
        placeholder={step.placeholder || step.title}
        type={inputType}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
          if (event.key === "Enter" && !event.nativeEvent.isComposing) {
            event.preventDefault();
            handleSubmit();
          }
        }}
      />
      {skipAction}
      <Button type="button" disabled={!valid} onClick={handleSubmit}>
        {t("composer.send")}
      </Button>
    </InputEntryRow>
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
      <ChoiceMenu variant="question" items={[{ id: "confirm", label: t("inputFlow.confirm"), value: true }]} onSelect={onConfirm} />
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
    allowCustom: record.allowCustom === true,
    customLabel: stringValue(record.customLabel),
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
    label: stringValue(record.label),
    title: stringValue(record.title),
    value: record.value,
  };
}

function optionKey(option: InputFlowOption, index: number) {
  const value = optionValue(option);
  try {
    return `${index}:${JSON.stringify(value)}`;
  } catch {
    return `${index}:${String(value ?? "")}`;
  }
}

function optionValue(option: InputFlowOption): unknown {
  return option.value !== undefined ? option.value : option.data ?? option.title ?? option.label;
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
    const values = step.type === "multi_select" && Array.isArray(value) ? value : [value];
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
    if (comparableValue(optionValue(option)) === target) {
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
  return Object.fromEntries(Object.entries(record).filter(([, value]) => value !== undefined));
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
