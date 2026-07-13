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
    if (error.code.startsWith("git_") || error.code === "not_git_repository" || error.code === "repository_outside_project") {
      return t("project.gitLoadFailed");
    }
  }
  return t("project.browserLoadFailed");
}
