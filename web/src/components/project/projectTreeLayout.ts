const projectTreeBaseInset = 8;
const projectTreeLevelIndent = 16;
const projectTreeTwistieCenter = 8;
const projectTreeFolderLabelOffset = 20;

export function projectTreeNodeInset(depth: number) {
  return projectTreeBaseInset + Math.max(0, depth) * projectTreeLevelIndent;
}

export function projectTreeGuideInset(depth: number) {
  return projectTreeNodeInset(depth) + projectTreeTwistieCenter;
}

export function projectTreeFolderLabelInset(depth: number) {
  return projectTreeNodeInset(depth) + projectTreeFolderLabelOffset;
}
