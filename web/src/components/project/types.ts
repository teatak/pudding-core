export type ProjectSelection = {
  rootID: string;
  path: string;
};

export type ProjectFileTab = ProjectSelection & {
  pinned: boolean;
};

export type ProjectGitDiffSelection = ProjectSelection & {
  originalPath?: string;
  staged: boolean;
  view: "git-diff";
};

export type ProjectGitDiffTab = ProjectGitDiffSelection & {
  pinned: boolean;
};

export type ProjectTab = ProjectFileTab | ProjectGitDiffTab;

export type ProjectEntryTarget = ProjectSelection & {
  name: string;
  type: "dir" | "file";
};

export type ProjectCreateTarget = ProjectSelection & {
  type: "dir" | "file";
};

export function isProjectGitDiffTab(tab: ProjectTab): tab is ProjectGitDiffTab {
  return "view" in tab && tab.view === "git-diff";
}
