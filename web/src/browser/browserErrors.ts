import { APIError } from "@/api/client";

export type BrowserOpenAttempt = {
  url: string;
};

export function browserOpenErrorDescription(attempt: BrowserOpenAttempt | null, error: unknown): string {
  const lines: string[] = [];
  if (attempt?.url) {
    lines.push(`URL: ${attempt.url}`);
  }
  const message =
    error instanceof APIError
      ? `${error.status} ${error.code}`
      : error instanceof Error
        ? error.message
        : typeof error === "string"
          ? error
          : "";
  if (message) {
    lines.push(`Error: ${message}`);
  }
  return lines.join("\n");
}
