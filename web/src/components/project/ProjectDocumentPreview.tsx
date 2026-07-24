import { ChevronRight } from "lucide-react";
import { useMemo } from "react";
import { parse as parseYAML } from "yaml";

import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import type { ProjectDocumentPreviewKind } from "./projectPreviewKinds";

const MAX_TABLE_ROWS = 500;
const MAX_TABLE_COLUMNS = 100;
const MAX_STRUCTURED_NODES = 5_000;
const MAX_STRUCTURED_DEPTH = 50;

type PreviewPrimitive = boolean | null | number | string;
type PreviewValue = PreviewPrimitive | PreviewValue[] | { [key: string]: PreviewValue };

export function ProjectDocumentPreview({
  expandedPaths,
  kind,
  path,
  value,
  onExpandedPathChange,
}: {
  expandedPaths: ReadonlySet<string>;
  kind: ProjectDocumentPreviewKind;
  path: string;
  value: string;
  onExpandedPathChange: (path: string, expanded: boolean) => void;
}) {
  if (kind === "table") {
    return <TablePreview delimiter={/\.tsv$/i.test(path) ? "\t" : ","} value={value} />;
  }
  return (
    <StructuredPreview
      expandedPaths={expandedPaths}
      path={path}
      value={value}
      onExpandedPathChange={onExpandedPathChange}
    />
  );
}

function TablePreview({ delimiter, value }: { delimiter: string; value: string }) {
  const { t } = useI18n();
  const table = useMemo(() => parseDelimitedText(value, delimiter), [delimiter, value]);
  if (table.rows.length === 0) {
    return <PreviewMessage>{t("project.browserPreviewEmpty")}</PreviewMessage>;
  }
  const [header, ...body] = table.rows;
  return (
    <div className="h-full min-h-0 overflow-auto p-4">
      <table className="min-w-full border-separate border-spacing-0 overflow-hidden rounded-lg border border-border/70 text-left text-xs">
        <thead className="sticky top-0 z-[1] bg-muted/90 backdrop-blur">
          <tr>
            {header.map((cell, index) => (
              <th className="max-w-96 border-r border-b border-border/70 px-3 py-2 font-medium last:border-r-0" key={index} title={cell}>
                <span className="block truncate">{cell || String(index + 1)}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {body.map((row, rowIndex) => (
            <tr className="odd:bg-muted/20 hover:bg-muted/40" key={rowIndex}>
              {header.map((_, columnIndex) => (
                <td className="max-w-96 border-r border-b border-border/50 px-3 py-2 align-top last:border-r-0" key={columnIndex} title={row[columnIndex] || ""}>
                  <span className="block whitespace-pre-wrap break-words">{row[columnIndex] || ""}</span>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {table.truncated ? <p className="py-3 text-xs text-muted-foreground">{t("project.browserPreviewTruncated")}</p> : null}
    </div>
  );
}

function StructuredPreview({
  expandedPaths,
  path,
  value,
  onExpandedPathChange,
}: {
  expandedPaths: ReadonlySet<string>;
  path: string;
  value: string;
  onExpandedPathChange: (path: string, expanded: boolean) => void;
}) {
  const { t } = useI18n();
  const parsed = useMemo(() => {
    try {
      const raw = /\.json$/i.test(path) ? JSON.parse(value) : parseYAML(value, { maxAliasCount: 100 });
      const state = { remaining: MAX_STRUCTURED_NODES, truncated: false };
      return { data: normalizePreviewValue(raw, state, new WeakSet(), 0), error: false, truncated: state.truncated };
    } catch {
      return { data: null, error: true, truncated: false };
    }
  }, [path, value]);
  if (parsed.error) {
    return <PreviewMessage>{t("project.browserPreviewInvalid")}</PreviewMessage>;
  }
  return (
    <div className="h-full min-h-0 overflow-auto p-4 font-mono text-xs">
      <StructuredNode
        expandedPaths={expandedPaths}
        label=""
        nodePath="$"
        value={parsed.data}
        onExpandedPathChange={onExpandedPathChange}
      />
      {parsed.truncated ? <p className="pt-3 font-sans text-xs text-muted-foreground">{t("project.browserPreviewTruncated")}</p> : null}
    </div>
  );
}

function StructuredNode({
  expandedPaths,
  label,
  nodePath,
  value,
  onExpandedPathChange,
}: {
  expandedPaths: ReadonlySet<string>;
  label: string;
  nodePath: string;
  value: PreviewValue;
  onExpandedPathChange: (path: string, expanded: boolean) => void;
}) {
  if (!isPreviewCollection(value)) {
    return (
      <div className="flex min-h-7 items-start gap-2 rounded px-1.5 py-1 hover:bg-muted/35">
        {label ? <span className="shrink-0 text-muted-foreground">{label}:</span> : null}
        <PreviewScalar value={value} />
      </div>
    );
  }
  const open = expandedPaths.has(nodePath);
  const entries = Array.isArray(value) ? value.map((item, index) => [String(index), item] as const) : Object.entries(value);
  return (
    <div>
      <button
        className="flex h-7 w-full items-center gap-1 rounded px-1 text-left hover:bg-muted/35"
        type="button"
        onClick={() => onExpandedPathChange(nodePath, !open)}
      >
        <ChevronRight className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        {label ? <span>{label}</span> : <span className="text-muted-foreground">{Array.isArray(value) ? "Array" : "Object"}</span>}
        <span className="text-muted-foreground">({entries.length})</span>
      </button>
      {open ? (
        <div className="ml-2.5 border-l border-border/70 pl-2">
          {entries.map(([key, child]) => (
            <StructuredNode
              expandedPaths={expandedPaths}
              key={key}
              label={key}
              nodePath={`${nodePath}/${encodeURIComponent(key)}`}
              value={child}
              onExpandedPathChange={onExpandedPathChange}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function PreviewScalar({ value }: { value: PreviewPrimitive }) {
  if (value === null) {
    return <span className="text-muted-foreground">null</span>;
  }
  if (typeof value === "string") {
    return <span className="min-w-0 whitespace-pre-wrap break-words text-emerald-700 dark:text-emerald-300">{JSON.stringify(value)}</span>;
  }
  if (typeof value === "number") {
    return <span className="text-sky-700 dark:text-sky-300">{String(value)}</span>;
  }
  return <span className="text-violet-700 dark:text-violet-300">{String(value)}</span>;
}

function PreviewMessage({ children }: { children: string }) {
  return <div className="flex h-full min-h-64 items-center justify-center p-6 text-center text-sm text-muted-foreground">{children}</div>;
}

function isPreviewCollection(value: PreviewValue): value is PreviewValue[] | { [key: string]: PreviewValue } {
  return typeof value === "object" && value !== null;
}

function normalizePreviewValue(value: unknown, state: { remaining: number; truncated: boolean }, seen: WeakSet<object>, depth: number): PreviewValue {
  if (state.remaining <= 0 || depth > MAX_STRUCTURED_DEPTH) {
    state.truncated = true;
    return "…";
  }
  state.remaining -= 1;
  if (value === null || typeof value === "string" || typeof value === "boolean") {
    return value;
  }
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : String(value);
  }
  if (typeof value !== "object") {
    return String(value);
  }
  if (seen.has(value)) {
    return "[Circular]";
  }
  seen.add(value);
  if (Array.isArray(value)) {
    const items: PreviewValue[] = [];
    for (const item of value) {
      if (state.remaining <= 0) {
        state.truncated = true;
        break;
      }
      items.push(normalizePreviewValue(item, state, seen, depth + 1));
    }
    return items;
  }
  const record: { [key: string]: PreviewValue } = {};
  for (const [key, child] of Object.entries(value)) {
    if (state.remaining <= 0) {
      state.truncated = true;
      break;
    }
    record[key] = normalizePreviewValue(child, state, seen, depth + 1);
  }
  return record;
}

function parseDelimitedText(value: string, delimiter: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  let truncated = false;
  const pushCell = () => {
    if (row.length < MAX_TABLE_COLUMNS) row.push(cell);
    else truncated = true;
    cell = "";
  };
  const pushRow = () => {
    pushCell();
    if (rows.length < MAX_TABLE_ROWS + 1) rows.push(row);
    else truncated = true;
    row = [];
  };
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character === "\"") {
      if (quoted && value[index + 1] === "\"") {
        cell += "\"";
        index += 1;
      } else {
        quoted = !quoted;
      }
      continue;
    }
    if (!quoted && character === delimiter) {
      pushCell();
      continue;
    }
    if (!quoted && (character === "\n" || character === "\r")) {
      if (character === "\r" && value[index + 1] === "\n") index += 1;
      pushRow();
      continue;
    }
    cell += character;
  }
  if (cell || row.length > 0) pushRow();
  return { rows, truncated };
}
