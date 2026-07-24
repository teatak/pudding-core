import { Briefcase, FolderClosed, MessageSquareText } from "lucide-react";

import type { Session } from "@/api/client";

export function SessionModeIcon({ className = "size-4", mode }: {
  className?: string;
  mode: Session["activeMode"];
}) {
  if (mode === "code") {
    return <FolderClosed aria-hidden="true" className={className} />;
  }
  if (mode === "work") {
    return <Briefcase aria-hidden="true" className={className} />;
  }
  return <MessageSquareText aria-hidden="true" className={className} />;
}
