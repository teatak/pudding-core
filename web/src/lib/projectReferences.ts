import type { ProjectReference } from "@/api/client";

export const PROJECT_REFERENCE_DRAG_TYPE = "application/x-pudding-project-reference";
export const PROJECT_ENTRY_DRAG_TYPE = "application/x-pudding-project-entry";

export type ProjectReferenceInput = Omit<ProjectReference, "id">;
export type ProjectReferenceRange = Required<Pick<ProjectReference, "startLine" | "startColumn" | "endLine" | "endColumn">>;

export function writeProjectReferenceDrag(dataTransfer: DataTransfer, reference: ProjectReferenceInput) {
  const serialized = JSON.stringify(reference);
  dataTransfer.effectAllowed = "copyMove";
  dataTransfer.setData(PROJECT_REFERENCE_DRAG_TYPE, serialized);
  dataTransfer.setData(PROJECT_ENTRY_DRAG_TYPE, serialized);
}

export function readProjectEntryDrag(dataTransfer: DataTransfer): ProjectReferenceInput | undefined {
  const raw = dataTransfer.getData(PROJECT_ENTRY_DRAG_TYPE);
  if (!raw) return undefined;
  try {
    const value = JSON.parse(raw) as Partial<ProjectReferenceInput>;
    if (
      typeof value.name !== "string" ||
      typeof value.path !== "string" ||
      typeof value.rootID !== "string" ||
      (value.kind !== "file" && value.kind !== "dir")
    ) return undefined;
    return {
      kind: value.kind,
      name: value.name,
      path: value.path,
      rootID: value.rootID,
      sourcePath: typeof value.sourcePath === "string" ? value.sourcePath : "",
    };
  } catch {
    return undefined;
  }
}

export function dataTransferHasProjectEntry(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes(PROJECT_ENTRY_DRAG_TYPE);
}

export function readProjectReferenceDrag(dataTransfer: DataTransfer): ProjectReferenceInput | undefined {
  const raw = dataTransfer.getData(PROJECT_REFERENCE_DRAG_TYPE);
  if (!raw) {
    return undefined;
  }
  try {
    const reference = JSON.parse(raw) as Partial<ProjectReferenceInput>;
    if (
      typeof reference.name !== "string" ||
      typeof reference.path !== "string" ||
      typeof reference.sourcePath !== "string" ||
      typeof reference.rootID !== "string" ||
      (reference.kind !== "file" && reference.kind !== "dir")
    ) {
      return undefined;
    }
    const range = readProjectReferenceRange(reference);
    if ((hasAnyProjectReferenceRangeValue(reference) && !range) || (range && reference.kind !== "file")) {
      return undefined;
    }
    return {
      name: reference.name,
      path: reference.path,
      sourcePath: reference.sourcePath,
      rootID: reference.rootID,
      kind: reference.kind,
      ...range,
    };
  } catch {
    return undefined;
  }
}

export function dataTransferHasProjectReference(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes(PROJECT_REFERENCE_DRAG_TYPE);
}

export function projectReferenceRange(reference: ProjectReference): ProjectReferenceRange | undefined {
  return readProjectReferenceRange(reference);
}

export function projectReferenceRangeLabel(reference: ProjectReference) {
  const range = projectReferenceRange(reference);
  if (!range) return "";
  if (range.startLine === range.endLine) {
    return `L${range.startLine}:${range.startColumn}–${range.endColumn}`;
  }
  return `L${range.startLine}:${range.startColumn}–${range.endLine}:${range.endColumn}`;
}

function readProjectReferenceRange(reference: Partial<ProjectReference>): ProjectReferenceRange | undefined {
  const values = [reference.startLine, reference.startColumn, reference.endLine, reference.endColumn];
  if (values.every((value) => value === undefined)) return undefined;
  if (!values.every((value) => Number.isInteger(value) && Number(value) > 0)) return undefined;
  const range = {
    startLine: reference.startLine!,
    startColumn: reference.startColumn!,
    endLine: reference.endLine!,
    endColumn: reference.endColumn!,
  };
  if (range.endLine < range.startLine || (range.endLine === range.startLine && range.endColumn < range.startColumn)) {
    return undefined;
  }
  return range;
}

function hasAnyProjectReferenceRangeValue(reference: Partial<ProjectReference>) {
  return [reference.startLine, reference.startColumn, reference.endLine, reference.endColumn]
    .some((value) => value !== undefined);
}
