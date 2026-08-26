import { Plus } from "@/components/icons";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";

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
      className="rounded-full border-0 bg-transparent text-muted-foreground"
      size="icon"
      type="button"
      variant="ghost"
      onClick={onClick}
    >
      {busy ? <Spinner className="size-4" /> : <Plus className="size-4" />}
    </Button>
  );
}
