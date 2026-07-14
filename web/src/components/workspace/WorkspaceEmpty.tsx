import { Compass, SquareTerminal } from "lucide-react";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";

export function WorkspaceEmpty({
  disabled,
  creatingBrowser,
  creatingTerminal,
  onCreateBrowser,
  onCreateTerminal,
}: {
  disabled: boolean;
  creatingBrowser: boolean;
  creatingTerminal: boolean;
  onCreateBrowser: () => void;
  onCreateTerminal: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="flex h-full items-center justify-center p-6">
      <div className="w-52 space-y-1 text-muted-foreground">
        <Button
          className="w-full justify-start rounded-md bg-muted/60 px-2 font-normal hover:bg-muted hover:text-foreground"
          disabled={disabled || creatingBrowser || creatingTerminal}
          type="button"
          variant="ghost"
          onClick={onCreateBrowser}
        >
          {creatingBrowser ? <Spinner /> : <Compass />}
          {t("browser.create")}
        </Button>
        <Button
          className="w-full justify-start rounded-md bg-muted/60 px-2 font-normal hover:bg-muted hover:text-foreground"
          disabled={disabled || creatingBrowser || creatingTerminal}
          type="button"
          variant="ghost"
          onClick={onCreateTerminal}
        >
          {creatingTerminal ? <Spinner /> : <SquareTerminal />}
          {t("terminal.create")}
        </Button>
      </div>
    </div>
  );
}
