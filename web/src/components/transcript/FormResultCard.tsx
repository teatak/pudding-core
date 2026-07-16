import { Check, Eye, EyeOff, MessageSquareMore } from "lucide-react";
import { useState } from "react";

import type { ContentPart } from "@/api/client";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

type FormResultPart = Extract<ContentPart, { type: "form_result" }>;

type FormStep = {
  id: string;
  options?: unknown[];
  title: string;
  type: string;
};

type FormSchema = {
  nextSteps: FormStep[];
  repeatSteps: FormStep[];
  resultKey: string;
  steps: FormStep[];
  type: "form" | "repeat";
};

export function formResultFromContentParts(parts: ContentPart[] | undefined): FormResultPart | undefined {
  return parts?.find((part): part is FormResultPart => part.type === "form_result" && normalizeSchema(part.schema) !== null);
}

export function FormResultCard({ part }: { part: FormResultPart }) {
  const { t } = useI18n();
  const [revealed, setRevealed] = useState<Set<string>>(() => new Set());
  const schema = normalizeSchema(part.schema);

  if (!schema) {
    return null;
  }

  function toggleRevealed(key: string) {
    setRevealed((current) => {
      const next = new Set(current);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  const fields = schema.type === "form" ? schema.steps : schema.nextSteps;
  const repeatedItems = schema.type === "repeat" ? arrayRecords(part.result[schema.resultKey]) : [];

  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-2 border-b border-border/50 pb-2">
        <MessageSquareMore className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.8} />
        <div className="min-w-0 flex-1 truncate font-medium">{part.title}</div>
        <div className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
          <Check className="size-3.5 text-success" strokeWidth={2} />
          <span>{t("inputFlow.completed")}</span>
        </div>
      </div>

      {repeatedItems.length > 0 ? (
        <div className="grid gap-2 pt-3">
          {repeatedItems.map((item, index) => (
            <div key={index} className="rounded-lg border border-border/50 bg-background/35 p-2.5">
              <div className="mb-2 text-xs font-medium text-muted-foreground">{index + 1}</div>
              <div className="grid grid-cols-1 gap-x-5 gap-y-2 sm:grid-cols-2">
                {schema.repeatSteps.map((step) => (
                  <ResultField
                    key={step.id}
                    fieldKey={`${index}:${step.id}`}
                    revealed={revealed.has(`${index}:${step.id}`)}
                    step={step}
                    value={item[step.id] ?? item[`${step.id}Label`]}
                    onToggleRevealed={toggleRevealed}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      ) : null}

      {fields.length > 0 ? (
        <div className={cn("grid grid-cols-1 gap-x-6 gap-y-3 pt-3 sm:grid-cols-2", repeatedItems.length > 0 && "mt-1 border-t border-border/40")}>
          {fields.map((step) => (
            <ResultField
              key={step.id}
              fieldKey={step.id}
              revealed={revealed.has(step.id)}
              step={step}
              value={part.result[step.id]}
              onToggleRevealed={toggleRevealed}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ResultField({
  fieldKey,
  revealed,
  step,
  value,
  onToggleRevealed,
}: {
  fieldKey: string;
  revealed: boolean;
  step: FormStep;
  value: unknown;
  onToggleRevealed: (key: string) => void;
}) {
  const { t } = useI18n();
  if (value === undefined) {
    return null;
  }
  const sensitive = isSensitiveStep(step);
  const displayValue = sensitive && !revealed ? "••••••••" : formatStepValue(step, value, t("inputFlow.confirmed"));
  return (
    <div className={cn("min-w-0", isFullWidthStep(step, value) && "sm:col-span-2")}>
      <div className="truncate text-xs leading-5 text-muted-foreground">{step.title}</div>
      <div className="flex min-h-6 min-w-0 items-start gap-1">
        <div className={cn("min-w-0 flex-1 text-sm font-medium leading-6 break-words whitespace-pre-wrap [overflow-wrap:anywhere]", sensitive && !revealed && "tracking-widest")}>
          {displayValue}
        </div>
        {sensitive ? (
          <Button
            aria-label={revealed ? t("inputFlow.hideValue") : t("inputFlow.showValue")}
            className="-mr-1 size-6 shrink-0 text-muted-foreground"
            size="icon-sm"
            type="button"
            variant="ghost"
            onClick={() => onToggleRevealed(fieldKey)}
          >
            {revealed ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function normalizeSchema(value: Record<string, unknown>): FormSchema | null {
  const type = value.type === "form" || value.type === "repeat" ? value.type : null;
  if (!type) {
    return null;
  }
  const schema: FormSchema = {
    type,
    resultKey: stringValue(value.resultKey) || "items",
    steps: normalizeSteps(value.steps),
    repeatSteps: normalizeSteps(value.repeatSteps),
    nextSteps: normalizeSteps(value.nextSteps),
  };
  if ((schema.type === "form" && schema.steps.length === 0) || (schema.type === "repeat" && schema.repeatSteps.length === 0)) {
    return null;
  }
  return schema;
}

function normalizeSteps(value: unknown): FormStep[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    const record = objectValue(item);
    const id = stringValue(record?.id);
    const title = stringValue(record?.title);
    const type = stringValue(record?.type);
    if (!id || !title || !type) {
      return [];
    }
    return [{ id, title, type, options: Array.isArray(record?.options) ? record.options : undefined }];
  });
}

function formatStepValue(step: FormStep, value: unknown, confirmed: string): string {
  if (step.type === "confirm" && value === true) {
    return confirmed;
  }
  if (step.type === "single_select" || step.type === "multi_select") {
    const values = Array.isArray(value) ? value : [value];
    return values.map((item) => optionLabel(step.options, item) || formatValue(item)).join("、");
  }
  return formatValue(value);
}

function optionLabel(options: unknown[] | undefined, value: unknown) {
  const target = comparableValue(value);
  for (const option of options || []) {
    if (typeof option === "string" || typeof option === "number") {
      if (comparableValue(option) === target) {
        return String(option);
      }
      continue;
    }
    const record = objectValue(option);
    if (!record) {
      continue;
    }
    const optionValue = record.value ?? record.data ?? record.title ?? record.label;
    if (comparableValue(optionValue) === target) {
      return stringValue(record.title) || stringValue(record.label) || formatValue(optionValue);
    }
  }
  return "";
}

function formatValue(value: unknown): string {
  if (value === null) {
    return "—";
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.map(formatValue).join("、");
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function isSensitiveStep(step: FormStep) {
  return /password|passwd|passcode|secret|api[\s_-]*key|access[\s_-]*token|refresh[\s_-]*token|private[\s_-]*key|密码|口令|密钥|令牌/i.test(`${step.id} ${step.title}`);
}

function isFullWidthStep(step: FormStep, value: unknown) {
  return /sql|query|command|description|note|content|查询|命令|备注|内容/i.test(`${step.id} ${step.title}`) || formatValue(value).length > 56;
}

function arrayRecords(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value) ? value.map(objectValue).filter(Boolean) as Array<Record<string, unknown>> : [];
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function comparableValue(value: unknown) {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}
