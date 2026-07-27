import { PanelLeft, PanelRight, PictureInPicture2 } from "@/components/icons";

import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
} from "@/components/AppMenu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import {
  setAgentConsoleMode,
  useAgentConsoleMode,
  type AgentConsoleMode,
} from "@/state/agentConsoleStore";

const icons = {
  floating: PictureInPicture2,
  "dock-left": PanelLeft,
  "dock-right": PanelRight,
} satisfies Record<AgentConsoleMode, typeof PanelLeft>;

export function AgentConsoleLayoutControl() {
  const { t } = useI18n();
  const mode = useAgentConsoleMode();
  const Icon = icons[mode];
  const options: Array<{ value: AgentConsoleMode; label: string }> = [
    { value: "floating", label: t("agentConsole.floating") },
    { value: "dock-left", label: t("agentConsole.dockLeft") },
    { value: "dock-right", label: t("agentConsole.dockRight") },
  ];

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              aria-label={t("agentConsole.layout")}
              className="pudding-toolbar-icon-button no-drag-region"
              size="icon-sm"
              tabIndex={-1}
              variant="ghost"
            >
              <Icon />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{t("agentConsole.layout")}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-44">
        <DropdownMenuRadioGroup
          value={mode}
          onValueChange={(value) => setAgentConsoleMode(value as AgentConsoleMode)}
        >
          {options.map(({ value, label }) => {
            const OptionIcon = icons[value];
            return (
              <DropdownMenuRadioItem key={value} value={value}>
                <OptionIcon />
                <span>{label}</span>
              </DropdownMenuRadioItem>
            );
          })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
