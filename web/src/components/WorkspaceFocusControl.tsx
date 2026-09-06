import { Minimize2, Maximize2, PanelRight } from "@/components/icons";
import { AppTooltip } from "@/components/AppTooltip";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { setWorkspacePresentation, useWorkspacePresentation } from "@/state/workspaceStore";

export function WorkspaceFocusControl({ sessionID }: { sessionID: string }) {
  const { t } = useI18n();
  const presentation = useWorkspacePresentation(sessionID);
  const focused = presentation === "focused";
  const hidden = presentation === "hidden";
  const label = t(focused ? "workspace.exitFocus" : "workspace.focus");
  const toggleLabel = t(hidden ? "workspace.open" : "workspace.collapse");
  const Icon = focused ? Minimize2 : Maximize2;
  return (
    <div className="pudding-workspace-focus-control flex items-center gap-1">
      {!hidden && <AppTooltip content={label}>
        <Button
          aria-label={label}
          aria-pressed={focused}
          className="pudding-toolbar-icon-button p-0 transition-colors aria-pressed:bg-muted/60 active:not-aria-[haspopup]:translate-y-0"
          variant="ghost"
          onClick={() => setWorkspacePresentation(sessionID, focused ? "standard" : "focused")}
        >
          <Icon className="size-4" />
        </Button>
      </AppTooltip>}
      <AppTooltip content={toggleLabel}>
        <Button
          aria-label={toggleLabel}
          aria-expanded={!hidden}
          className="pudding-toolbar-icon-button p-0 transition-colors active:not-aria-[haspopup]:translate-y-0"
          variant="ghost"
          onClick={() => setWorkspacePresentation(sessionID, hidden ? "standard" : "hidden")}
        >
          <PanelRight className="size-4" />
        </Button>
      </AppTooltip>
    </div>
  );
}
