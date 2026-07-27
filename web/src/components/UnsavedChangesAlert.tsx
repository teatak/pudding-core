import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ConfirmationDialog";
import { useI18n } from "@/i18n";

export function UnsavedChangesAlert({
  open,
  onDiscard,
  onOpenChange,
}: {
  open: boolean;
  onDiscard: () => void;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("common.unsavedChanges.title")}</AlertDialogTitle>
          <AlertDialogDescription>{t("common.unsavedChanges.description")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.unsavedChanges.keepEditing")}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onDiscard}>
            {t("common.unsavedChanges.discard")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
