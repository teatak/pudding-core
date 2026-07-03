import { Loader2, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function ComposerAddButton({
  active,
  busy,
  label,
  onClick,
}: {
  active: boolean;
  busy: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button
      aria-expanded={active}
      aria-label={label}
      className={cn(
        "rounded-full border-0 bg-transparent text-muted-foreground hover:text-foreground",
        active && "bg-muted text-foreground",
      )}
      size="icon"
      type="button"
      variant="ghost"
      onClick={onClick}
    >
      {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
    </Button>
  );
}
