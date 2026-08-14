import type { Project, Session } from "@/api/client";
import { FolderClosed } from "@/components/icons";
import { useRailOverlayHold } from "@/components/session-rail/overlayHold";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useI18n } from "@/i18n";

export function SessionProjectPickerDialog({
  open,
  pending,
  projects,
  session,
  onOpenChange,
  onSelect,
}: {
  open: boolean;
  pending: boolean;
  projects: Project[];
  session: Session | undefined;
  onOpenChange: (open: boolean) => void;
  onSelect: (projectID: string) => Promise<void>;
}) {
  const { t } = useI18n();
  useRailOverlayHold(open);

  return (
    <CommandDialog
      className="sm:max-w-md"
      description={t("session.projectPickerDescription")}
      open={open}
      showCloseButton
      title={t("session.projectPickerTitle")}
      onOpenChange={onOpenChange}
    >
      <Command>
        <CommandInput placeholder={t("session.projectSearchPlaceholder")} />
        <CommandList>
          <CommandEmpty>{t("session.projectSearchEmpty")}</CommandEmpty>
          <CommandGroup>
            {projects.map((project) => (
              <CommandItem
                key={project.id}
                data-checked={project.id === session?.projectID}
                disabled={pending}
                value={`${project.name} ${project.id}`}
                onSelect={() => {
                  if (!session || project.id === session.projectID) {
                    onOpenChange(false);
                    return;
                  }
                  void onSelect(project.id)
                    .then(() => onOpenChange(false))
                    .catch(() => undefined);
                }}
              >
                <FolderClosed />
                <span className="min-w-0 flex-1 truncate">{project.name}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
