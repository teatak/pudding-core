import { APIError } from "@/api/client";

export function projectBrowserError(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError) {
    if (error.code === "session_has_no_project") {
      return t("project.browserNoProject");
    }
    if (error.code === "project_file_not_text" || error.code === "project_file_not_regular") {
      return t("project.browserUnsupportedFile");
    }
    if (error.code === "project_file_too_large") {
      return t("project.browserFileTooLarge");
    }
    if (error.code === "project_entry_exists") {
      return t("project.browserEntryExists");
    }
    if (error.code === "project_entry_invalid_name") {
      return t("project.browserInvalidName");
    }
    if (error.code === "project_file_revision_conflict") {
      return t("project.browserExternalChange");
    }
    if ([
      "git_commit_failed",
      "git_commit_message_required",
      "git_conflicts",
      "git_discard_failed",
      "git_init_failed",
      "git_no_staged_changes",
      "git_no_upstream",
      "git_publish_failed",
      "git_remote_unavailable",
      "git_stage_failed",
      "git_sync_failed",
      "git_unstage_failed",
      "git_branch_create_failed",
      "git_branch_delete_failed",
      "git_branch_list_failed",
      "git_branch_rename_failed",
      "git_branch_switch_failed",
      "git_invalid_branch",
    ].includes(error.code)) {
      return t("project.gitOperationFailed");
    }
    if (error.code.startsWith("git_") || error.code === "not_git_repository" || error.code === "repository_outside_project") {
      return t("project.gitLoadFailed");
    }
  }
  return t("project.browserLoadFailed");
}

export function projectGitReadError(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError && error.code === "session_has_no_project") {
    return t("project.browserNoProject");
  }
  return t("project.gitLoadFailed");
}

export function projectGitOperationError(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError && error.code === "session_has_no_project") {
    return t("project.browserNoProject");
  }
  return t("project.gitOperationFailed");
}
