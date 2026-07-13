import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

import type { ProjectEntryTarget } from "./types";

const nameSchema = z.object({
  name: z.string().trim().min(1).refine((name) => name !== "." && name !== ".." && !/[\\/\0]/.test(name)),
});

export function ProjectNameDialog({
  initialName = "",
  mode,
  open,
  pending,
  onOpenChange,
  onSubmit,
}: {
  initialName?: string;
  mode: "newFile" | "newFolder" | "rename";
  open: boolean;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (name: string) => void;
}) {
  const { t } = useI18n();
  const form = useForm<z.infer<typeof nameSchema>>({
    defaultValues: { name: initialName },
    resolver: zodResolver(nameSchema),
  });
  useEffect(() => {
    if (open) {
      form.reset({ name: initialName });
    }
  }, [form, initialName, open]);
  const title = mode === "newFile"
    ? t("project.browserNewFile")
    : mode === "newFolder" ? t("project.browserNewFolder") : t("project.browserRename");
  return (
    <Dialog open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{t("project.browserNameHint")}</DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={form.handleSubmit((values) => onSubmit(values.name.trim()))}>
          <Input
            autoFocus
            aria-label={t("project.browserEntryName")}
            disabled={pending}
            {...form.register("name")}
          />
          {form.formState.errors.name ? <p className="text-xs text-destructive">{t("project.browserInvalidName")}</p> : null}
          <DialogFooter>
            <Button disabled={pending} type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
            <Button disabled={pending} type="submit">{pending ? <Spinner /> : null}{t("common.confirm")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectDeleteDialog({
  target,
  pending,
  onCancel,
  onConfirm,
}: {
  target?: ProjectEntryTarget;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  return (
    <AlertDialog open={Boolean(target)} onOpenChange={(open) => !open && !pending && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("project.browserDeleteTitle")}</AlertDialogTitle>
          <AlertDialogDescription>{t("project.browserDeleteDescription").replace("{name}", target?.name || "")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction disabled={pending} variant="destructive" onClick={onConfirm}>
            {pending ? <Spinner /> : null}{t("common.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function ProjectUnsavedCloseDialog({
  count,
  open,
  onCancel,
  onDiscard,
}: {
  count: number;
  open: boolean;
  onCancel: () => void;
  onDiscard: () => void;
}) {
  const { t } = useI18n();
  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("project.browserUnsavedCloseTitle")}</AlertDialogTitle>
          <AlertDialogDescription>{t("project.browserUnsavedCloseDescription").replace("{count}", String(count))}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onDiscard}>{t("project.browserDiscardAndClose")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
