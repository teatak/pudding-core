import type { ProjectReference } from "@/api/client";

export const PROJECT_REFERENCE_DRAG_TYPE = "application/x-pudding-project-reference";

export type ProjectReferenceInput = Omit<ProjectReference, "id">;

export function writeProjectReferenceDrag(dataTransfer: DataTransfer, reference: ProjectReferenceInput) {
  dataTransfer.effectAllowed = "copy";
  dataTransfer.setData(PROJECT_REFERENCE_DRAG_TYPE, JSON.stringify(reference));
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
    return {
      name: reference.name,
      path: reference.path,
      sourcePath: reference.sourcePath,
      rootID: reference.rootID,
      kind: reference.kind,
    };
  } catch {
    return undefined;
  }
}

export function dataTransferHasProjectReference(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes(PROJECT_REFERENCE_DRAG_TYPE);
}
