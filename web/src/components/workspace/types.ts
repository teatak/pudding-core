export type WorkspaceSurface = "workspace" | "canvas" | "browser" | "project";

export type BrowserWorkspaceArtifact = {
  createdAt: string;
  faviconURL?: string;
  kind: "browser";
  resourceID: string;
  sessionID: string;
  title?: string;
  url?: string;
};

export type CanvasWorkspaceArtifact = {
  createdAt: string;
  kind: "canvas";
  resourceID: string;
  resourceKind: string;
  sessionID: string;
  title?: string;
};

export type WorkspaceArtifact = BrowserWorkspaceArtifact | CanvasWorkspaceArtifact;
